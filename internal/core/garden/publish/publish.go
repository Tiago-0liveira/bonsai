// Package publish turns one private draft into a GitHub Issue with a verified,
// approved specification. It composes the canonical protocols and invents none:
// capture creates the Issue at Draft, edit appends the specification revision,
// approve records and projects the approval. Every step is persisted before it
// runs and verified after it, so a crash or an ambiguous answer is resumed by
// reusing what exists, never by repeating an effect. No planner, worktree,
// agent or pull request starts here.
package publish

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/approve"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/capture"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/drafts"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/edit"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/ops"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/spec"
	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
)

// Steps name where a publication stands; they are stored in the operation record.
const (
	StepIssue    = "issue"
	StepSpec     = "spec"
	StepApproval = "approval"
	StepVerify   = "verify"
)

// Drafts is the part of the private draft store publication needs.
type Drafts interface {
	Read(context.Context, drafts.Scope, drafts.ID) (drafts.Draft, error)
	Save(context.Context, drafts.Scope, drafts.Draft, uint64) (drafts.Draft, error)
}

// Deps are the seams of Service. Every one but Now is required.
type Deps struct {
	Drafts  Drafts
	Ops     *ops.Store
	Capture *capture.Service
	Edit    *edit.Service
	Approve *approve.Service
	Now     func() time.Time
}

// Service publishes drafts. It is safe for concurrent use.
type Service struct {
	d Deps

	mu       sync.Mutex
	inflight map[string]bool
}

// New builds a Service.
func New(d Deps) (*Service, error) {
	if d.Drafts == nil || d.Ops == nil || d.Capture == nil || d.Edit == nil || d.Approve == nil {
		return nil, errors.New("publish needs drafts, an operation store and the capture, edit and approve services")
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	return &Service{d: d, inflight: map[string]bool{}}, nil
}

// Request names a draft and the target and account that would publish it. The caller has just verified
// the connection and Write access; Viewer is the numeric account ID, Login a display label.
type Request struct {
	Local        garden.LocalRepoID
	Repo         garden.RepoID
	RepoFullName string
	Viewer       string
	Login        string
	DraftID      drafts.ID
}

func (r Request) scope() drafts.Scope { return drafts.Scope{Local: r.Local, Repo: r.Repo} }

// Preview is exactly what Publish would do. Hand it back to Publish unchanged.
type Preview struct {
	Request
	DraftRevision uint64
	Title         string
	Type          garden.ItemType
	Description   string
	// SpecText is the rendered specification that becomes the first revision.
	SpecText string
	Labels   []string
	// ContentHash binds repository ID, account, draft revision and every published text. The full name is
	// display and link text only: a renamed repository must not strand a publication that already started.
	ContentHash string
	// OperationID is deterministic, so confirming the same preview twice is the same operation.
	OperationID string
}

func contentHash(r Request, rev uint64, t garden.ItemType, title, desc, specText string) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s\x00%d\x00%s\x00%s\x00%d\x00%s\x00%s\x00%s\x00%s", r.Local, r.Repo, r.Viewer, r.DraftID, rev, t, strings.TrimSpace(title), desc, specText)
	return hex.EncodeToString(h.Sum(nil))
}

func operationID(id drafts.ID, rev uint64, hash string) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%d\x00%s", id, rev, hash)))
	return "publish-" + hex.EncodeToString(sum[:])[:24]
}

func previewOf(r Request, d drafts.Draft, rev uint64) Preview {
	specText := spec.Render(d.Spec)
	hash := contentHash(r, rev, d.Type, d.Title, d.Description, specText)
	return Preview{Request: r, DraftRevision: rev, Title: strings.TrimSpace(d.Title), Type: d.Type, Description: d.Description,
		SpecText: specText, Labels: capture.Labels(d.Type), ContentHash: hash, OperationID: operationID(d.ID, rev, hash)}
}

// State is where a publication stands after a call.
type State string

const (
	// Published means the Issue exists, its specification is approved and verified, and the draft is marked.
	Published State = "published"
	// Incomplete means the Issue exists but a later step is not verified; Resume continues it.
	Incomplete State = "incomplete"
	// Unknown means GitHub may hold the Issue but it could not be found yet; Resume looks again.
	Unknown State = "unknown"
	// Failed means nothing was created; the draft is back to private and may be previewed again.
	Failed State = "failed"
)

// Result is the outcome of a started publication.
type Result struct {
	State             State
	Step              string
	Issue             *drafts.IssueIdentity
	SpecCommentID     int64
	ApprovalCommentID int64
	// Err says why the publication stopped short of Published.
	Err error
}

// Preview reads the draft and reports what publishing would do. It writes nothing.
func (s *Service) Preview(ctx context.Context, r Request) (Preview, error) {
	d, err := s.d.Drafts.Read(ctx, r.scope(), r.DraftID)
	if err != nil {
		return Preview{}, err
	}
	switch {
	case !d.ArchivedAt.IsZero():
		return Preview{}, fmt.Errorf("%w: the draft is archived", ErrNotReady)
	case d.Publication.State == drafts.Published:
		return Preview{}, ErrAlreadyPublished
	case d.Publication.State == drafts.Publishing:
		return Preview{}, ErrPublishing
	}
	if err := ready(d, r); err != nil {
		return Preview{}, err
	}
	return previewOf(r, d, d.Revision), nil
}

// ready is every check that would otherwise surface as a refused Issue or a blocked approval, so a
// preview never promises what the approval would refuse.
func ready(d drafts.Draft, r Request) error {
	if !drafts.Assess(d).Ready() {
		return fmt.Errorf("%w: the draft is missing required parts or has open questions", ErrNotReady)
	}
	if err := capture.Validate(capture.Request{OpID: "x", Local: r.Local, Repo: r.Repo, Viewer: r.Viewer, Title: d.Title, Description: d.Description, Type: d.Type}); err != nil {
		return fmt.Errorf("%w: %w", ErrNotReady, err)
	}
	s := spec.Parse(spec.Render(d.Spec))
	for _, req := range []struct{ name, text string }{{"Summary", s.Summary}, {"Requirements", s.Requirements}, {"Acceptance criteria", s.AcceptanceCriteria}} {
		if strings.TrimSpace(req.text) == "" {
			return fmt.Errorf("%w: %s is empty", ErrNotReady, req.name)
		}
	}
	if q := approve.OpenQuestions(s.OpenQuestions); len(q) > 0 {
		return fmt.Errorf("%w: open question: %s", ErrNotReady, q[0])
	}
	return nil
}

// Publish confirms a preview. It is the only entry that starts a publication.
func (s *Service) Publish(ctx context.Context, p Preview) (Result, error) {
	if p.OperationID == "" || p.DraftRevision == 0 || p.DraftID == "" || p.Viewer == "" ||
		p.ContentHash != contentHash(p.Request, p.DraftRevision, p.Type, p.Title, p.Description, p.SpecText) ||
		p.OperationID != operationID(p.DraftID, p.DraftRevision, p.ContentHash) {
		return Result{}, ErrMismatch
	}
	unlock, err := s.lock(ctx, p.OperationID)
	if err != nil {
		return Result{}, err
	}
	defer unlock()
	d, err := s.d.Drafts.Read(ctx, p.scope(), p.DraftID)
	if err != nil {
		return Result{}, err
	}
	switch d.Publication.State {
	case drafts.Published:
		if d.Publication.OperationID != p.OperationID {
			return Result{}, ErrAlreadyPublished
		}
		return s.published(d), nil
	case drafts.Publishing:
		if d.Publication.OperationID != p.OperationID {
			return Result{}, ErrPublishing
		}
		// A crash or an earlier partial run: the reservation exists, continue it.
	default:
		if err := s.stale(d, p); err != nil {
			return Result{}, err
		}
		if err := s.reserve(ctx, p, &d); err != nil {
			return Result{}, err
		}
	}
	rec, err := s.d.Ops.Get(ctx, p.OperationID)
	if err != nil {
		// The draft is already publishing: report a resumable state, never "nothing started".
		return s.stopped(ops.Record{}, err), nil
	}
	return s.execute(ctx, p, rec, d)
}

// stale compares the confirmed preview with the draft as it is now.
func (s *Service) stale(d drafts.Draft, p Preview) error {
	if d.Revision != p.DraftRevision || !d.ArchivedAt.IsZero() || previewOf(p.Request, d, d.Revision).ContentHash != p.ContentHash {
		return ErrStale
	}
	return nil
}

// reserve binds a durable operation to the exact draft, then marks the draft as publishing. The operation
// is written first: a crash between the two leaves a record nobody can mistake for a finished publication.
func (s *Service) reserve(ctx context.Context, p Preview, d *drafts.Draft) error {
	now := s.d.Now().UTC()
	_, err := s.d.Ops.Update(ctx, p.OperationID, func(cur ops.Record, found bool) (ops.Record, error) {
		if !found {
			return ops.Record{ID: p.OperationID, Kind: ops.KindPublish, Local: p.Local, Repo: p.Repo, State: ops.StatePending, Owner: p.Viewer,
				ContentHash: p.ContentHash, Title: p.Title, Type: p.Type, Draft: string(p.DraftID), DraftRevision: p.DraftRevision, CreatedAt: now, UpdatedAt: now}, nil
		}
		if cur.Kind != ops.KindPublish || cur.Local != p.Local || cur.Repo != p.Repo || cur.ContentHash != p.ContentHash || cur.Owner != p.Viewer ||
			cur.Draft != string(p.DraftID) || cur.DraftRevision != p.DraftRevision {
			return cur, ErrMismatch
		}
		return cur, nil
	})
	if err != nil {
		return err
	}
	next := *d
	next.Publication = drafts.Publication{State: drafts.Publishing, OperationID: p.OperationID}
	saved, err := s.d.Drafts.Save(ctx, p.scope(), next, d.Revision)
	if err != nil {
		s.finishOp(ctx, p.OperationID, ops.StateFailed, "")
		if errors.Is(err, drafts.ErrConflict) {
			return ErrStale
		}
		return err
	}
	*d = saved
	return nil
}

// Resume continues the publication the draft is stuck in, from its stored operation.
func (s *Service) Resume(ctx context.Context, r Request) (Result, error) {
	d, err := s.d.Drafts.Read(ctx, r.scope(), r.DraftID)
	if err != nil {
		return Result{}, err
	}
	if d.Publication.State == drafts.Unpublished {
		return Result{}, ErrStale
	}
	unlock, err := s.lock(ctx, d.Publication.OperationID)
	if err != nil {
		return Result{}, err
	}
	defer unlock()
	// The draft may have moved while we waited for the lock.
	if d, err = s.d.Drafts.Read(ctx, r.scope(), r.DraftID); err != nil {
		return Result{}, err
	}
	switch d.Publication.State {
	case drafts.Unpublished:
		return Result{}, ErrStale
	case drafts.Published:
		return s.published(d), nil
	}
	rec, err := s.d.Ops.Get(ctx, d.Publication.OperationID)
	if err != nil {
		return Result{}, fmt.Errorf("%w: %w", ErrMismatch, err)
	}
	// Content is frozen while publishing, so the stored revision reproduces the confirmed hash.
	p := previewOf(r, d, rec.DraftRevision)
	if rec.Kind != ops.KindPublish || rec.Draft != string(d.ID) || rec.Owner != r.Viewer || rec.ContentHash != p.ContentHash || rec.Repo != r.Repo || rec.Local != r.Local {
		return Result{}, ErrMismatch
	}
	return s.execute(ctx, p, rec, d)
}

func (s *Service) published(d drafts.Draft) Result {
	return Result{State: Published, Step: StepVerify, Issue: d.Publication.Issue}
}

func (s *Service) lock(ctx context.Context, id string) (func(), error) {
	s.mu.Lock()
	if s.inflight[id] {
		s.mu.Unlock()
		return nil, ErrInFlight
	}
	s.inflight[id] = true
	s.mu.Unlock()
	release := func() { s.mu.Lock(); delete(s.inflight, id); s.mu.Unlock() }
	l, err := s.d.Ops.TryOperationLock(ctx, id)
	if err != nil {
		release()
		if errors.Is(err, procstore.ErrLocked) {
			return nil, ErrInFlight
		}
		return nil, err
	}
	return func() { l.Unlock(); release() }, nil
}

// record stores progress with a context that survives cancellation: the effect already happened.
func (s *Service) record(ctx context.Context, id string, fn func(*ops.Record)) (ops.Record, error) {
	return s.d.Ops.Update(context.WithoutCancel(ctx), id, func(cur ops.Record, _ bool) (ops.Record, error) {
		fn(&cur)
		cur.UpdatedAt = s.d.Now().UTC()
		return cur, nil
	})
}

func (s *Service) finishOp(ctx context.Context, id string, state ops.State, step string) {
	_, _ = s.record(ctx, id, func(r *ops.Record) { r.State = state; r.Step = step })
}

// stopped is the result of an error after the reservation, when no Issue is known: GitHub may hold one,
// so it is Unknown and resuming looks before it creates.
func (s *Service) stopped(rec ops.Record, err error) Result {
	if rec.IssueID != 0 {
		return s.incomplete(Result{Issue: issueOf(rec)}, StepIssue, err)
	}
	return Result{State: Unknown, Step: StepIssue, Err: err}
}

func (s *Service) incomplete(res Result, step string, err error) Result {
	res.State, res.Step, res.Err = Incomplete, step, err
	return res
}

// execute runs the steps from wherever the record says the publication stands. The draft is Publishing and
// this process holds the operation lock.
func (s *Service) execute(ctx context.Context, p Preview, rec ops.Record, d drafts.Draft) (Result, error) {
	if rec.State == ops.StateDone {
		if rec.IssueID == 0 {
			return Result{}, ErrMismatch
		}
		res := Result{Issue: issueOf(rec), SpecCommentID: rec.SpecCommentID, ApprovalCommentID: rec.ApprovalCommentID}
		return s.complete(ctx, p, res), nil
	}
	if rec.State != ops.StatePending {
		var err error
		if rec, err = s.record(ctx, rec.ID, func(r *ops.Record) { r.State = ops.StatePending }); err != nil {
			return s.stopped(rec, err), nil
		}
	}
	if rec.IssueID == 0 {
		id, number, stop, err := s.createIssue(ctx, p, d)
		if stop != nil {
			return *stop, nil
		}
		if err != nil {
			return s.stopped(rec, err), nil
		}
		url := fmt.Sprintf("https://github.com/%s/issues/%d", p.RepoFullName, number)
		rec, err = s.record(ctx, rec.ID, func(r *ops.Record) { r.IssueID, r.Number, r.IssueURL, r.Step = id, number, url, StepSpec })
		if err != nil {
			// The Issue exists and the capture record holds it: a resume finds it there.
			return s.incomplete(Result{Issue: &drafts.IssueIdentity{ID: id, Number: number, URL: url}}, StepIssue, err), nil
		}
	}
	res := Result{Issue: issueOf(rec)}
	// The link is kept on the draft as soon as it exists, so a partial publication still shows its Issue.
	if d.Publication.Issue == nil {
		if _, err := s.savePublication(ctx, p, func(pub *drafts.Publication) { pub.Issue = res.Issue }); err != nil {
			return s.incomplete(res, StepIssue, err), nil
		}
	}
	if rec.SpecCommentID == 0 {
		var err error
		if rec, err = s.specStep(ctx, p, rec); err != nil {
			return s.incomplete(res, StepSpec, err), nil
		}
	}
	res.SpecCommentID = rec.SpecCommentID
	if rec.ApprovalCommentID == 0 {
		var err error
		rec, err = s.approvalStep(ctx, p, rec)
		res.ApprovalCommentID = rec.ApprovalCommentID
		if err != nil {
			return s.incomplete(res, StepApproval, err), nil
		}
	}
	if err := s.verify(ctx, p, rec); err != nil {
		return s.incomplete(res, StepVerify, err), nil
	}
	if _, err := s.record(ctx, rec.ID, func(r *ops.Record) { r.State, r.Step = ops.StateDone, "" }); err != nil {
		return s.incomplete(res, StepVerify, err), nil
	}
	return s.complete(ctx, p, res), nil
}

func issueOf(rec ops.Record) *drafts.IssueIdentity {
	return &drafts.IssueIdentity{ID: rec.IssueID, Number: rec.Number, URL: rec.IssueURL}
}

// complete marks the draft published. Every step is verified, and content is frozen while publishing, so
// a revision conflict here only means another process touched the record: read again and retry once.
func (s *Service) complete(ctx context.Context, p Preview, res Result) Result {
	_, err := s.savePublication(ctx, p, func(pub *drafts.Publication) {
		*pub = drafts.Publication{State: drafts.Published, OperationID: p.OperationID, Issue: res.Issue, VerifiedAt: s.d.Now().UTC()}
	})
	if err != nil {
		return s.incomplete(res, StepVerify, err)
	}
	res.State, res.Step = Published, StepVerify
	return res
}

// savePublication rewrites only the publication field of the draft as stored now.
func (s *Service) savePublication(ctx context.Context, p Preview, mutate func(*drafts.Publication)) (drafts.Draft, error) {
	ctx = context.WithoutCancel(ctx)
	var err error
	for range 2 {
		var cur drafts.Draft
		if cur, err = s.d.Drafts.Read(ctx, p.scope(), p.DraftID); err != nil {
			return drafts.Draft{}, err
		}
		if cur.Publication.State == drafts.Published && cur.Publication.OperationID == p.OperationID {
			return cur, nil
		}
		mutate(&cur.Publication)
		var saved drafts.Draft
		if saved, err = s.d.Drafts.Save(ctx, p.scope(), cur, cur.Revision); !errors.Is(err, drafts.ErrConflict) {
			return saved, err
		}
	}
	return drafts.Draft{}, err
}

// createIssue creates the Issue through capture, reconciling once when the outcome is unknown. A non-nil
// stop is the final Result of the publication; an error means nothing about the Issue is known to have changed.
func (s *Service) createIssue(ctx context.Context, p Preview, d drafts.Draft) (garden.IssueID, int64, *Result, error) {
	opID := p.OperationID + "-issue"
	res, err := s.d.Capture.Create(ctx, capture.Request{OpID: opID, Local: p.Local, Repo: p.Repo, Viewer: p.Viewer, Title: p.Title, Description: p.Description, Type: p.Type})
	if errors.Is(err, capture.ErrNeedsReconcile) {
		if res, err = s.d.Capture.Reconcile(ctx, opID, p.Viewer); err != nil {
			s.finishOp(ctx, p.OperationID, ops.StateUnknown, StepIssue)
			return 0, 0, &Result{State: Unknown, Step: StepIssue, Err: err}, nil
		}
	}
	switch {
	case err == nil && res.Record.State == ops.StateDone && res.Record.IssueID != 0:
		return res.Record.IssueID, res.Record.Number, nil, nil
	case err == nil && res.Record.State != ops.StateFailed:
		s.finishOp(ctx, p.OperationID, ops.StateUnknown, StepIssue)
		return 0, 0, &Result{State: Unknown, Step: StepIssue}, nil
	case err != nil && res.Record.State != ops.StateFailed && !errors.Is(err, capture.ErrInvalid):
		if errors.Is(err, capture.ErrInFlight) {
			err = fmt.Errorf("%w: %w", ErrInFlight, err)
		}
		return 0, 0, nil, err
	}
	// Definitive: GitHub did not create the Issue. The draft returns to private so it can be retried.
	s.finishOp(ctx, p.OperationID, ops.StateFailed, StepIssue)
	if d.Publication.Issue == nil {
		if _, rerr := s.savePublication(ctx, p, func(pub *drafts.Publication) { *pub = drafts.Publication{State: drafts.Unpublished} }); rerr != nil {
			// The draft still reads Publishing, so "safe to try again" would be false: resuming re-runs the create.
			return 0, 0, &Result{State: Unknown, Step: StepIssue, Err: fmt.Errorf("%w: the draft could not be returned to private: %w", err, rerr)}, nil
		}
	}
	return 0, 0, &Result{State: Failed, Step: StepIssue, Err: err}, nil
}

// specStep appends the draft's specification as the Issue's first revision, or finds the one an
// interrupted run already appended. Commit looks for an existing revision by this account with the same
// text and parents before it appends, so an ambiguous append is never repeated.
func (s *Service) specStep(ctx context.Context, p Preview, rec ops.Record) (ops.Record, error) {
	t := edit.Target{Repo: p.Repo, Issue: rec.IssueID, Viewer: p.Viewer}
	doc, err := s.d.Edit.Load(ctx, t)
	if err != nil {
		return rec, err
	}
	// The Issue ID comes from our own capture record; the author check guards identity only, not the body.
	if doc.Issue.AuthorID != p.Viewer {
		return rec, fmt.Errorf("%w: the Issue was not created by this account", ErrConflict)
	}
	hash := garden.ContentHash(p.SpecText)
	// The base is the state right after creation: no revision yet. Anything else surfaces as a conflict.
	out, outcome, err := s.d.Edit.Commit(ctx, edit.SavePreview{Target: t, Base: edit.Base{BodyHash: doc.Base.BodyHash}, Content: p.SpecText, NewHash: hash})
	switch {
	case outcome.Conflict != nil:
		return rec, ErrConflict
	case err != nil:
		return rec, err
	}
	return s.record(ctx, rec.ID, func(r *ops.Record) { r.SpecHash, r.SpecCommentID, r.Step = hash, out.CommentID, StepApproval })
}

// approvalStep records the approval of exactly our revision and projects the accepted stage. It refuses to
// approve any head that is not the revision this operation appended.
func (s *Service) approvalStep(ctx context.Context, p Preview, rec ops.Record) (ops.Record, error) {
	t := approve.Target{Repo: p.Repo, Issue: rec.IssueID, Viewer: p.Viewer, Login: p.Login}
	pv, err := s.d.Approve.Preview(ctx, t)
	if err != nil {
		return rec, err
	}
	if pv.Head == nil || pv.Head.CommentID != rec.SpecCommentID || pv.Head.Hash != rec.SpecHash {
		return rec, ErrConflict
	}
	// Assess counts an approval only when its author is the viewer (the verifier's trusted actor), so an
	// existing one is ours.
	var approval int64
	if pv.State == approve.StateApproved {
		approval = pv.Approval.CommentID
	} else {
		if !pv.CanValidate() {
			return rec, fmt.Errorf("%w: %w", ErrNotReady, approve.BlockedError{Blockers: pv.Blockers})
		}
		res, err := s.d.Approve.Validate(ctx, pv)
		approval = res.ApprovalCommentID
		if err != nil {
			// Not stored: the next run must pass through here again to finish the projection. It finds the
			// recorded approval on GitHub and cannot append another.
			rec.ApprovalCommentID = approval
			return rec, err
		}
	}
	return s.record(ctx, rec.ID, func(r *ops.Record) { r.ApprovalCommentID, r.Step = approval, StepVerify })
}

// verify reads the Issue back independently of the writes: our revision is the head, our approval counts
// and the stage has left Draft. Labels alone are never taken as acceptance.
func (s *Service) verify(ctx context.Context, p Preview, rec ops.Record) error {
	log, err := s.d.Approve.Read(ctx, approve.Target{Repo: p.Repo, Issue: rec.IssueID, Viewer: p.Viewer, Login: p.Login})
	if err != nil {
		return fmt.Errorf("%w: %w", ErrUnverified, err)
	}
	a := approve.Assess(log.Issue, log.Verification)
	if a.State != approve.StateApproved || a.Head == nil || a.Head.CommentID != rec.SpecCommentID || a.Head.Hash != rec.SpecHash ||
		a.Approval == nil || a.Approval.CommentID != rec.ApprovalCommentID {
		return ErrUnverified
	}
	return nil
}
