package handoff

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/approve"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/planrev"
)

// MaxTextBytes bounds the human's own words in a decision.
const MaxTextBytes = 4 << 10

// Reader reads and verifies the Issue's comment log (gate one's reader, so every gate trusts the same viewer).
type Reader interface {
	Read(context.Context, approve.Target) (approve.Log, error)
}

// Writer is the only write the service needs: one comment.
type Writer interface {
	AppendComment(context.Context, garden.RepoID, garden.IssueID, string) (garden.IssueComment, error)
}

// Deps are the seams of Service; Reader and Writer are required.
type Deps struct {
	Reader Reader
	Writer Writer
	Now    func() time.Time
}

// Service reads handoff history and records decisions. Writes are serialized so two decisions in this process
// cannot both pass the replay check and append twice.
type Service struct {
	d  Deps
	mu sync.Mutex
}

// New builds a Service.
func New(d Deps) (*Service, error) {
	if d.Reader == nil || d.Writer == nil {
		return nil, errors.New("handoff needs a log reader and a writer")
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	return &Service{d: d}, nil
}

func (s *Service) read(ctx context.Context, t approve.Target) (approve.Log, garden.HandoffLog, error) {
	if t.Viewer == "" {
		return approve.Log{}, garden.HandoffLog{}, ErrNoActor
	}
	log, err := s.d.Reader.Read(ctx, t)
	if err != nil {
		return approve.Log{}, garden.HandoffLog{}, err
	}
	if !log.Verification.Complete {
		return log, garden.HandoffLog{}, ErrIncompleteLog
	}
	policy := garden.ApprovalPolicy{AllowedActorIDs: []string{t.Viewer}, ReadOnly: t.ReadOnly}
	return log, garden.VerifyHandoffs(garden.IssueIdentity{Repo: t.Repo, Issue: t.Issue}, log.Comments, policy), nil
}

// Read verifies the log now and returns the Issue's handoff history. It writes nothing.
func (s *Service) Read(ctx context.Context, t approve.Target) (garden.HandoffLog, error) {
	_, h, err := s.read(ctx, t)
	return h, err
}

// DecideRequest is one explicit human decision. Identity is what the human was shown.
type DecideRequest struct {
	Target   approve.Target
	Identity Identity
	Decision garden.HandoffDecision
	Text     string
	ActorID  string
	// TargetCommentID names the handoff comment to withdraw; revocation only.
	TargetCommentID int64
}

// DecideResult is what Decide recorded.
type DecideResult struct {
	CommentID int64
	// Replayed is set when the same decision was already recorded and nothing was appended.
	Replayed bool
}

// Decide records the decision after checking the log once more. It never starts a run or touches Git. The write
// happens only when the specification and plan are still the verified approved pair, and its outcome is read
// back: a record that does not verify afterwards is ErrUnverified, never success.
//
// Decide trusts the caller to build Identity from the launch attempt's actual state; it can check the approved
// pair but not the head, tree or pull request. The once-only guarantee is per process (the mutex); across
// processes or retries, replay detection on re-read returns the existing record instead of appending again.
func (s *Service) Decide(ctx context.Context, r DecideRequest) (DecideResult, error) {
	text := strings.TrimSpace(r.Text)
	switch {
	case !validDecision(r.Decision) || r.Decision == garden.HandoffRevoked && r.TargetCommentID <= 0 || r.Decision != garden.HandoffRevoked && r.TargetCommentID != 0:
		return DecideResult{}, ErrInvalidDecision
	case len(text) > MaxTextBytes || strings.Contains(text, "bonsai:garden/"):
		return DecideResult{}, ErrInvalidText
	case text == "" && (r.Decision == garden.HandoffChangesRequested || r.Decision == garden.HandoffCompleted):
		return DecideResult{}, ErrTextRequired
	}
	if r.ActorID == "" || r.ActorID != r.Target.Viewer {
		return DecideResult{}, ErrActorChanged
	}
	if r.Target.ReadOnly {
		return DecideResult{}, ErrReadOnly
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	log, h, err := s.read(ctx, r.Target)
	if err != nil {
		return DecideResult{}, err
	}
	if log.Verification.ApprovalReadOnly {
		return DecideResult{}, ErrReadOnly
	}
	rec := garden.HandoffRecord{Decision: garden.HandoffRevoked}
	if r.Decision == garden.HandoffRevoked {
		for _, e := range h.Entries {
			if e.CommentID != r.TargetCommentID || e.Record.Decision == garden.HandoffRevoked {
				continue
			}
			if e.Revoked {
				return DecideResult{CommentID: e.RevokedBy, Replayed: true}, nil
			}
			return s.append(ctx, r, rec, text, e.CommentID)
		}
		return DecideResult{}, ErrNoTarget
	}
	if !approvedPair(log, r.Target.Viewer, r.Identity) {
		return DecideResult{}, ErrInputsChanged
	}
	rec = r.Identity.Record(r.Decision)
	if cur := Current(h, r.Identity); cur.State == DecisionCurrent && cur.Entry.AuthorID == r.Target.Viewer &&
		cur.Entry.Record.Decision == r.Decision && cur.Entry.Detail() == text {
		return DecideResult{CommentID: cur.Entry.CommentID, Replayed: true}, nil
	}
	return s.append(ctx, r, rec, text, 0)
}

func validDecision(d garden.HandoffDecision) bool {
	return d == garden.HandoffAccepted || d == garden.HandoffChangesRequested || d == garden.HandoffCompleted || d == garden.HandoffRevoked
}

// approvedPair reports whether id names the verified approved specification and the accepted plan.
func approvedPair(log approve.Log, viewer string, id Identity) bool {
	rv := planrev.Derive(log, viewer)
	if !rv.Ready || rv.Gate.Head == nil || rv.Gate.Approval == nil || rv.Approved == nil || len(rv.Approved.Decisions) == 0 {
		return false
	}
	last := rv.Approved.Decisions[len(rv.Approved.Decisions)-1]
	return rv.Gate.Head.Hash == id.SpecRevision && rv.Gate.Approval.CommentID == id.SpecApprovalID &&
		rv.Approved.Hash == id.PlanRevision && last.CommentID == id.PlanDecisionID && last.Decision == garden.DecisionApproved && !last.Revoked
}

func (s *Service) append(ctx context.Context, r DecideRequest, rec garden.HandoffRecord, text string, target int64) (DecideResult, error) {
	body, err := garden.NewHandoff(garden.IssueIdentity{Repo: r.Target.Repo, Issue: r.Target.Issue}, rec, target, headerText(r, rec)+detail(text))
	if err != nil {
		return DecideResult{}, fmt.Errorf("%w: %w", ErrInvalidText, err)
	}
	c, err := s.d.Writer.AppendComment(ctx, r.Target.Repo, r.Target.Issue, body)
	if err != nil {
		return DecideResult{}, fmt.Errorf("%w: %w", ErrAppendFailed, err)
	}
	// The comment exists now, so its outcome must be read even when the caller gave up.
	_, after, err := s.read(context.WithoutCancel(ctx), r.Target)
	if err != nil {
		return DecideResult{CommentID: c.ID}, fmt.Errorf("%w: %w", ErrUnverified, err)
	}
	for _, e := range after.Entries {
		if e.CommentID == c.ID && e.Record.Decision == r.Decision {
			return DecideResult{CommentID: c.ID}, nil
		}
	}
	return DecideResult{CommentID: c.ID}, ErrUnverified
}

func detail(text string) string {
	if text == "" {
		return ""
	}
	return "\n\n" + text
}

// headerText is the one-line human summary; the envelope carries the facts and the record hash covers both.
func headerText(r DecideRequest, rec garden.HandoffRecord) string {
	who := r.Target.Login
	if who == "" {
		who = "account " + r.Target.Viewer
	}
	switch rec.Decision {
	case garden.HandoffAccepted:
		return fmt.Sprintf("Accepted attempt %s at `%s` by %s.", rec.AttemptID, rec.FinalHead, who)
	case garden.HandoffChangesRequested:
		return fmt.Sprintf("Changes requested on attempt %s at `%s` by %s.", rec.AttemptID, rec.FinalHead, who)
	case garden.HandoffCompleted:
		return fmt.Sprintf("Completed without code changes by %s.", who)
	}
	return fmt.Sprintf("Revoked handoff comment %d by %s.", r.TargetCommentID, who)
}
