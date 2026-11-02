package approve

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
)

// Writer is the set of writes gate one needs: an approval comment and the stage label.
type Writer interface {
	AppendComment(context.Context, garden.RepoID, garden.IssueID, string) (garden.IssueComment, error)
	UpdateStage(context.Context, garden.RepoID, garden.IssueID, []string, garden.Stage, garden.Stage) error
}

// Deps are the seams of Service.
type Deps struct {
	Reader garden.IssueReader
	Writer Writer
	Now    func() time.Time
}

// Service previews and records specification approval. Writes are serialized so two validations in this
// process cannot both pass the replay check and append twice.
type Service struct {
	d  Deps
	mu sync.Mutex
}

// New builds a Service; every seam is required except Now.
func New(d Deps) (*Service, error) {
	if d.Reader == nil || d.Writer == nil {
		return nil, errors.New("approve needs a reader and a writer")
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	return &Service{d: d}, nil
}

// Target names one Issue and the authenticated account that acts on it. Viewer is the numeric account ID
// the approval policy trusts; Login is a display label only.
type Target struct {
	Repo   garden.RepoID
	Issue  garden.IssueID
	Viewer string
	Login  string
	// ReadOnly records live repository permission for gate-two readers.
	ReadOnly bool
}

// Step is one write of the Draft → Approved transition, in order.
type Step string

const (
	StepApproval Step = "approval" // append the approval comment
	StepLabel    Step = "label"    // move the stage label from draft to ready
)

// Preview is exactly what Validate would record. Hand it back to Validate unchanged.
type Preview struct {
	Target Target
	Number int64
	Title  string
	Type   garden.ItemType
	Assessment
	// Steps are the writes still needed; empty when the Issue is already approved or blocked.
	Steps  []Step
	Labels []string
}

type snapshot struct {
	issue    garden.Issue
	v        garden.Verification
	comments []garden.Comment
}

// Log is one verified read of an Issue: what gate one decided from, for readers (plan review) that must verify
// further records against the same viewer policy.
type Log struct {
	Issue        garden.Issue
	Verification garden.Verification
	// Comments is the ordered server log the verification was derived from, including prose Bonsai ignores.
	Comments []garden.Comment
}

// Read fetches the Issue and its whole comment log and verifies it for the viewer of t. It writes nothing.
func (s *Service) Read(ctx context.Context, t Target) (Log, error) {
	st, err := s.read(ctx, t)
	return Log{Issue: st.issue, Verification: st.v, Comments: st.comments}, err
}

// read fetches the Issue and its whole comment log and verifies it. A log that cannot be read completely is
// reported as an incomplete verification, so the Assessment blocks instead of guessing.
func (s *Service) read(ctx context.Context, t Target) (snapshot, error) {
	if t.Viewer == "" {
		return snapshot{}, ErrNoActor
	}
	issue, err := s.d.Reader.GetIssue(ctx, t.Repo, t.Issue)
	if err != nil {
		return snapshot{}, err
	}
	if issue.ID != t.Issue || issue.Repo != t.Repo {
		return snapshot{}, ErrWrongIssue
	}
	id := garden.IssueIdentity{Repo: t.Repo, Issue: t.Issue}
	policy := garden.ApprovalPolicy{AllowedActorIDs: []string{t.Viewer}}
	if t.ReadOnly {
		policy.ReadOnly = true
	}
	comments, err := s.d.Reader.ListComments(ctx, t.Repo, t.Issue)
	if errors.Is(err, garden.ErrTooManyComments) {
		return snapshot{issue: issue, v: garden.Verify(id, nil, policy, garden.VerifyOptions{Complete: false})}, nil
	}
	if err != nil {
		return snapshot{}, err
	}
	conv := make([]garden.Comment, 0, len(comments))
	for _, c := range comments {
		conv = append(conv, garden.Comment{ID: c.ID, Author: c.Author, AuthorID: c.AuthorID, Body: c.Body, CreatedAt: c.CreatedAt})
	}
	opt := garden.VerifyOptions{Complete: true}
	if m, merr := garden.DecodeMetadata(issue.Metadata); merr == nil {
		opt.Revocations = m.Revocations
	}
	return snapshot{issue: issue, v: garden.Verify(id, conv, policy, opt), comments: conv}, nil
}

func (s *Service) preview(t Target, st snapshot) Preview {
	a := Assess(st.issue, st.v)
	p := Preview{Target: t, Number: st.issue.Number, Title: st.issue.Title, Type: garden.Classify(st.issue.Labels).Type, Assessment: a, Labels: slices.Clone(st.issue.Labels)}
	if a.CanValidate() {
		p.Steps = stepsFor(a)
	}
	return p
}

// stepsFor lists the writes that remain for a validatable Issue.
func stepsFor(a Assessment) []Step {
	var steps []Step
	if a.Approval == nil {
		steps = append(steps, StepApproval)
	}
	if a.From != garden.StageReady {
		steps = append(steps, StepLabel)
	}
	return steps
}

// Preview verifies the log now and reports what validating would record. It writes nothing, and a blocked
// specification is a Preview with Blockers, not an error.
func (s *Service) Preview(ctx context.Context, t Target) (Preview, error) {
	st, err := s.read(ctx, t)
	if err != nil {
		return Preview{}, err
	}
	return s.preview(t, st), nil
}

// Result is what Validate recorded.
type Result struct {
	ApprovalCommentID int64
	RevisionHash      string
	Fingerprint       string
	// Done lists the steps this call performed; empty when the Issue was already approved.
	Done []Step
}

// Validate records the approval of exactly the previewed revision: it re-reads and verifies the log, refuses
// when the head is no longer the one previewed, appends the approval comment unless a verified one exists,
// verifies it, then projects the stage label. The append-only approval records acceptance. Every step is idempotent,
// so repeating a Validate that stopped halfway finishes it. Nothing is launched.
func (s *Service) Validate(ctx context.Context, p Preview) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, err := s.read(ctx, p.Target)
	if err != nil {
		return Result{}, err
	}
	cur := s.preview(p.Target, st)
	if cur.Head != nil && !sameHead(cur.Assessment, p.Assessment) {
		return Result{}, ErrStale
	}
	if cur.State == StateApproved {
		return Result{ApprovalCommentID: cur.Approval.CommentID, RevisionHash: cur.Head.Hash, Fingerprint: cur.Fingerprint}, nil
	}
	if !cur.CanValidate() {
		return Result{}, BlockedError{Blockers: cur.Blockers}
	}
	res := Result{RevisionHash: cur.Head.Hash, Fingerprint: cur.Fingerprint}
	if cur.Approval == nil {
		body, err := garden.NewApproval(garden.IssueIdentity{Repo: p.Target.Repo, Issue: p.Target.Issue}, *cur.Head, s.approvalText(p.Target, cur.Assessment))
		if err != nil {
			return res, err
		}
		comment, err := s.d.Writer.AppendComment(ctx, p.Target.Repo, p.Target.Issue, body)
		if err != nil {
			return res, fmt.Errorf("%w: %w", ErrAppendFailed, err)
		}
		res.Done = append(res.Done, StepApproval)
		// Verify with a context that outlives a cancellation: the comment exists now, so the outcome must be read.
		st, err = s.read(context.WithoutCancel(ctx), p.Target)
		if err != nil {
			return res, fmt.Errorf("%w: %w", ErrUnverified, err)
		}
		cur = s.preview(p.Target, st)
		if cur.Head == nil || !sameHead(cur.Assessment, p.Assessment) {
			res.ApprovalCommentID = comment.ID
			return res, ErrHeadMoved
		}
		if cur.Approval == nil || cur.Approval.CommentID != comment.ID {
			return res, ErrUnverified
		}
	}
	res.ApprovalCommentID = cur.Approval.CommentID
	for _, step := range stepsFor(cur.Assessment) {
		var err error
		switch step {
		case StepLabel:
			err = s.projectLabel(ctx, p.Target, st.issue, p.Labels, p.From)
		}
		if err != nil {
			return res, fmt.Errorf("%w: %w", ErrProjection, err)
		}
		res.Done = append(res.Done, step)
	}
	after, err := s.read(context.WithoutCancel(ctx), p.Target)
	if err != nil {
		return res, fmt.Errorf("%w: %w", ErrUnverified, err)
	}
	if got := Assess(after.issue, after.v); got.State != StateApproved || !sameHead(got, p.Assessment) {
		return res, ErrUnverified
	}
	return res, nil
}

func sameHead(a, b Assessment) bool {
	return a.Head != nil && b.Head != nil && a.Head.Hash == b.Head.Hash && a.Head.CommentID == b.Head.CommentID && a.Fingerprint == b.Fingerprint
}

// approvalText is the human-readable part of the approval comment. It repeats what the envelope binds so the
// record reads sensibly on GitHub or a phone; only the author GitHub attaches to the comment is evidence.
func (s *Service) approvalText(t Target, a Assessment) string {
	who := t.Login
	if who == "" {
		who = "account " + t.Viewer
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Specification s%d approved by %s.\n\n", a.Ordinal, who)
	fmt.Fprintf(&b, "- Revision: `%s` (comment %d)\n- Source fingerprint: `%s`\n- Recorded: %s\n\n", a.Head.Hash, a.Head.CommentID, a.Fingerprint, s.d.Now().UTC().Format(time.RFC3339))
	b.WriteString("This approves the specification only. It starts no planner, agent, worktree or pull request.")
	return b.String()
}

// projectLabel changes only the expected stage and refuses concurrent managed-label edits.
func (s *Service) projectLabel(ctx context.Context, t Target, issue garden.Issue, previous []string, from garden.Stage) error {
	if !slices.Equal(garden.ManagedLabels(issue.Labels), garden.ManagedLabels(previous)) {
		return garden.ErrMetadataChanged
	}
	return s.d.Writer.UpdateStage(ctx, t.Repo, t.Issue, previous, from, garden.StageReady)
}
