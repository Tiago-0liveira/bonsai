package planrev

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/approve"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/plan"
)

// Target names one Issue and the authenticated account that acts on it.
type Target = approve.Target

// Log reads and verifies the Issue's comment log (gate one's own reader, so both gates trust the same viewer).
type Log interface {
	Read(context.Context, Target) (approve.Log, error)
}

// Appender is the only write the service needs: one comment.
type Appender interface {
	AppendComment(context.Context, garden.RepoID, garden.IssueID, string) (garden.IssueComment, error)
}

// Attempts looks up a planner attempt by ID.
type Attempts interface {
	Get(context.Context, string) (plan.Attempt, error)
}

// Deps are the seams of Service; all are required except Now.
type Deps struct {
	Log      Log
	Writer   Appender
	Attempts Attempts
	Local    garden.LocalRepoID
	Now      func() time.Time
}

// Service reads plan reviews, publishes revisions and records decisions. Writes are serialized so two
// publications in this process cannot both pass the replay check and append twice.
type Service struct {
	d  Deps
	mu sync.Mutex
}

// New builds a Service.
func New(d Deps) (*Service, error) {
	if d.Log == nil || d.Writer == nil || d.Attempts == nil || d.Local == "" {
		return nil, errors.New("planrev needs a log reader, a writer, an attempt lookup and a local ID")
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	return &Service{d: d}, nil
}

func (s *Service) read(ctx context.Context, t Target) (Review, approve.Log, error) {
	if t.Viewer == "" {
		return Review{}, approve.Log{}, ErrNoActor
	}
	log, err := s.d.Log.Read(ctx, t)
	if err != nil {
		return Review{}, approve.Log{}, err
	}
	rv := Derive(log, t.Viewer)
	rv.VerifiedAt = s.d.Now().UTC()
	return rv, log, nil
}

// Review verifies the log now and reports the plan revisions of the Issue. It writes nothing and starts nothing.
func (s *Service) Review(ctx context.Context, t Target) (Review, error) {
	rv, _, err := s.read(ctx, t)
	return rv, err
}

// PublishPreview is exactly what Publish would append. Hand it back to Publish unchanged.
type PublishPreview struct {
	Target Target
	// AttemptID is the planner attempt the plan came from; its record supplies the inspected base and the
	// specification the plan answers.
	AttemptID string
	Spec      plan.SpecRef
	Base      plan.Base
	Content   string
	Hash      string
	Sections  []Section
	// Parents are the plan heads the new revision supersedes (none for the first).
	Parents []string
	// Ordinal is what the revision will be numbered.
	Ordinal int
	// Against is the revision the new one is compared with in review: the current head, else the latest.
	Against *Revision
	// Blockers say why publishing is not offered.
	Blockers []string
	// Replay is the comment of this very revision when it is already in the log; publishing then writes nothing.
	Replay int64
}

// CanPublish reports whether Publish may run.
func (p PublishPreview) CanPublish() bool { return len(p.Blockers) == 0 }

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	m := map[string]int{}
	for _, x := range a {
		m[x]++
	}
	for _, x := range b {
		m[x]--
	}
	for _, n := range m {
		if n != 0 {
			return false
		}
	}
	return true
}

func hashesOf(revs []Revision) []string {
	out := make([]string, len(revs))
	for i, r := range revs {
		out[i] = r.Hash
	}
	return out
}

// preview loads the attempt and checks that it belongs to this clone and Issue, then prepares the publication.
func (s *Service) preview(ctx context.Context, t Target, attemptID, text string, rv Review) (PublishPreview, error) {
	a, err := s.d.Attempts.Get(ctx, attemptID)
	if err != nil {
		return PublishPreview{}, err
	}
	if a.Local != s.d.Local {
		return PublishPreview{}, ErrWrongAttempt
	}
	return Prepare(t, a, text, rv)
}

// Prepare checks everything that can refuse before a byte is written and composes the revision text. Only the
// user's explicit action reaches it, with text they chose: nothing is read from the planner's session by Bonsai.
func Prepare(t Target, a plan.Attempt, text string, rv Review) (PublishPreview, error) {
	if a.Repo != t.Repo || a.Issue != t.Issue {
		return PublishPreview{}, ErrWrongAttempt
	}
	if rv.Unavailable != "" {
		return PublishPreview{}, ErrNotApproved
	}
	if rv.Gate.Head.Hash != a.Spec.RevisionHash || rv.Gate.Fingerprint != a.Spec.Fingerprint {
		return PublishPreview{}, ErrSpecChanged
	}
	p := PublishPreview{Target: t, AttemptID: a.ID, Spec: a.Spec, Base: a.Base, Ordinal: len(rv.Revisions) + 1}
	if strings.TrimSpace(text) == "" {
		return p, ErrEmpty
	}
	p.Content = Compose(text, a.Spec, a.Base)
	if len(p.Content) > MaxBytes {
		return p, ErrTooLarge
	}
	p.Hash, p.Sections = garden.ContentHash(p.Content), Parse(p.Content)
	if missing := Missing(p.Sections); len(missing) > 0 {
		p.Blockers = append(p.Blockers, "Missing or empty: "+strings.Join(missing, ", ")+".")
	}
	p.Parents = hashesOf(rv.Heads())
	switch {
	case rv.Current != nil:
		p.Against = rv.Current
	case rv.Latest != nil:
		p.Against = rv.Latest
	}
	if existing, ok := rv.ByHash(p.Hash); ok {
		// The same text as a revision of ours that is still a head is the same publication, retried: it is found, not repeated.
		if !existing.Trusted || existing.Superseded {
			return p, ErrMatchesEarlier
		}
		p.Replay = existing.CommentID
	}
	return p, nil
}

// PreviewPublish verifies the log now and reports what publishing text as a plan revision would append. text is
// the plan the user chose to publish from the planner attempt; it is composed with the attempt's inspected base
// and specification and checked for the required sections. It writes nothing.
func (s *Service) PreviewPublish(ctx context.Context, t Target, attemptID, text string) (PublishPreview, error) {
	rv, _, err := s.read(ctx, t)
	if err != nil {
		return PublishPreview{}, err
	}
	return s.preview(ctx, t, attemptID, text, rv)
}

// PublishResult is what Publish recorded.
type PublishResult struct {
	CommentID int64
	Hash      string
	Ordinal   int
	// Replayed is set when the revision was already in the log and nothing was appended.
	Replayed bool
}

// Publish appends the previewed plan revision after reading and verifying the log once more. The write happens
// only when the specification and the plan heads are exactly what the preview saw, and its outcome is read back:
// a revision that does not verify afterwards is reported as ErrUnverified, never as success.
func (s *Service) Publish(ctx context.Context, p PublishPreview) (PublishResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rv, _, err := s.read(ctx, p.Target)
	if err != nil {
		return PublishResult{}, err
	}
	cur, err := s.preview(ctx, p.Target, p.AttemptID, p.Content, rv)
	if err != nil {
		return PublishResult{}, err
	}
	if cur.Spec != p.Spec {
		return PublishResult{}, ErrSpecChanged
	}
	// Confirming a preview whose revision is already in the log (a retry after an unknown outcome) finds it, even
	// though the heads have moved on to it.
	if cur.Hash == p.Hash && cur.Replay != 0 {
		return PublishResult{CommentID: cur.Replay, Hash: cur.Hash, Ordinal: s.ordinalOf(rv, cur.Hash), Replayed: true}, nil
	}
	if cur.Hash != p.Hash || !sameSet(cur.Parents, p.Parents) {
		return PublishResult{}, ErrHeadsMoved
	}
	if !cur.CanPublish() {
		return PublishResult{}, IncompleteError{Missing: Missing(cur.Sections)}
	}
	body, err := garden.NewPlanRevision(garden.IssueIdentity{Repo: p.Target.Repo, Issue: p.Target.Issue}, cur.Content, cur.Parents, cur.Spec.Fingerprint)
	if err != nil {
		return PublishResult{}, err
	}
	c, err := s.d.Writer.AppendComment(ctx, p.Target.Repo, p.Target.Issue, body)
	if err != nil {
		return PublishResult{}, fmt.Errorf("%w: %w", ErrAppendFailed, err)
	}
	// The comment exists now, so its outcome must be read even when the caller gave up.
	after, _, err := s.read(context.WithoutCancel(ctx), p.Target)
	if err != nil {
		return PublishResult{CommentID: c.ID, Hash: cur.Hash}, fmt.Errorf("%w: %w", ErrUnverified, err)
	}
	for _, r := range after.Revisions {
		if r.CommentID == c.ID && r.Hash == cur.Hash && r.Trusted {
			return PublishResult{CommentID: c.ID, Hash: r.Hash, Ordinal: r.Ordinal}, nil
		}
	}
	return PublishResult{CommentID: c.ID, Hash: cur.Hash}, ErrUnverified
}

func (s *Service) ordinalOf(rv Review, hash string) int {
	if r, ok := rv.ByHash(hash); ok {
		return r.Ordinal
	}
	return 0
}

// MaxReasonBytes bounds a decision's reason.
const MaxReasonBytes = 4 << 10

// DecisionRequest is the user's decision about one exact plan revision. Fingerprint is the specification the
// reviewer saw the revision answer: a request naming another one is refused.
type DecisionRequest struct {
	Target       Target
	RevisionHash string
	Fingerprint  string
	Decision     garden.Decision
	Reason       string
	// Approval confirmation binds both heads and the authenticated actor shown in review.
	SpecHash string
	ActorID  string
}

// DecisionResult is what Decide recorded.
type DecisionResult struct {
	CommentID int64
	Ordinal   int
	Decision  garden.Decision
	// Replayed is set when the same decision was already recorded and nothing was appended.
	Replayed bool
}

// Decide records a decision for exactly the named revision. It never starts a planner or any
// run. Approval rechecks the two heads and the actor and ends at Ready.
func (s *Service) Decide(ctx context.Context, r DecisionRequest) (DecisionResult, error) {
	switch r.Decision {
	case garden.DecisionChangesRequested, garden.DecisionRejected, garden.DecisionApproved:
	default:
		return DecisionResult{}, ErrNoRevision
	}
	reason := strings.TrimSpace(r.Reason)
	switch {
	case r.Decision == garden.DecisionChangesRequested && reason == "":
		return DecisionResult{}, ErrReasonRequired
	case len(reason) > MaxReasonBytes || strings.Contains(reason, "bonsai:garden/"):
		return DecisionResult{}, ErrInvalidReason
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	rv, _, err := s.read(ctx, r.Target)
	if err != nil {
		return DecisionResult{}, err
	}
	if r.Decision == garden.DecisionApproved {
		if r.ActorID == "" || r.ActorID != r.Target.Viewer {
			return DecisionResult{}, ErrActorChanged
		}
		if rv.Gate.Head == nil || rv.Gate.Head.Hash != r.SpecHash || rv.Gate.Fingerprint != r.Fingerprint {
			return DecisionResult{}, ErrHeadsMoved
		}
		if rv.Current == nil || rv.Current.Hash != r.RevisionHash || len(rv.Heads()) != 1 {
			return DecisionResult{}, ErrHeadsMoved
		}
	}
	if rv.Unavailable != "" {
		return DecisionResult{}, ErrNotApproved
	}
	rev, ok := rv.ByHash(r.RevisionHash)
	if !ok {
		return DecisionResult{}, ErrNoRevision
	}
	if rev.SourceSpecFingerprint != r.Fingerprint || rev.Stale {
		return DecisionResult{}, ErrSpecChanged
	}
	// Replays first: the decision is already recorded, so a head that moved on since must not turn it into a refusal.
	if n := len(rev.Decisions); n > 0 && !rev.Decisions[n-1].Revoked && rev.Decisions[n-1].Decision == r.Decision && ReasonOf(rev.Decisions[n-1]) == reason {
		return DecisionResult{CommentID: rev.Decisions[n-1].CommentID, Ordinal: rev.Ordinal, Decision: r.Decision, Replayed: true}, nil
	}
	if !rv.CanDecide(r.Decision) || rv.Current.Hash != rev.Hash {
		return DecisionResult{}, ErrNotCurrent
	}
	body, err := garden.NewPlanDecision(garden.IssueIdentity{Repo: r.Target.Repo, Issue: r.Target.Issue}, rev.Revision, r.Decision, s.decisionText(r.Target, rev, r.Decision, reason), "local")
	if err != nil {
		return DecisionResult{}, err
	}
	c, err := s.d.Writer.AppendComment(ctx, r.Target.Repo, r.Target.Issue, body)
	if err != nil {
		return DecisionResult{}, fmt.Errorf("%w: %w", ErrAppendFailed, err)
	}
	after, _, err := s.read(context.WithoutCancel(ctx), r.Target)
	if err != nil {
		return DecisionResult{CommentID: c.ID}, fmt.Errorf("%w: %w", ErrUnverified, err)
	}
	if r.Decision == garden.DecisionApproved && !after.Ready {
		return DecisionResult{CommentID: c.ID}, ErrUnverified
	}
	if got, ok := after.ByHash(rev.Hash); ok {
		for _, d := range got.Decisions {
			if d.CommentID == c.ID && d.Decision == r.Decision {
				return DecisionResult{CommentID: c.ID, Ordinal: rev.Ordinal, Decision: r.Decision}, nil
			}
		}
	}
	return DecisionResult{CommentID: c.ID}, ErrUnverified
}

// decisionText is the human-readable part of the decision comment. The envelope binds the revision and the
// specification; this text carries the reviewer's reason and is covered by the record hash.
func (s *Service) decisionText(t Target, rev Revision, d garden.Decision, reason string) string {
	who := t.Login
	if who == "" {
		who = "account " + t.Viewer
	}
	verb := "Changes requested on"
	if d == garden.DecisionApproved {
		verb = "Approved"
	}
	if d == garden.DecisionRejected {
		verb = "Rejected"
	}
	head := fmt.Sprintf("%s plan r%d by %s (revision `%s`, %s).", verb, rev.Ordinal, who, rev.Hash, s.d.Now().UTC().Format(time.RFC3339))
	if reason == "" {
		return head
	}
	return head + "\n\n" + reason
}
