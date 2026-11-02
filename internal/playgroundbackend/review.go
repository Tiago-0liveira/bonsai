package playgroundbackend

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/launch"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/review"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/playgroundapi"
)

const (
	reviewReadTimeout   = 25 * time.Second
	reviewActionTimeout = 90 * time.Second
	reviewPreviewBudget = 60 * time.Second
)

// ReviewStore is the durable review record (review.FileStore).
type ReviewStore interface {
	Load(ctx context.Context, local garden.LocalRepoID) (review.Snapshot, error)
	RecordRun(ctx context.Context, local garden.LocalRepoID, in review.RunInput) (review.Report, bool, error)
	MarkReady(ctx context.Context, local garden.LocalRepoID, id string, revision int, body review.Body) (review.Report, error)
	MarkFailed(ctx context.Context, local garden.LocalRepoID, id string, revision int, code review.FailCode) (review.Report, error)
	MarkInvalid(ctx context.Context, local garden.LocalRepoID, id string, revision int) (review.Report, error)
	MarkBlocked(ctx context.Context, local garden.LocalRepoID, id string, revision int, code review.FailCode) (review.Report, error)
	Dismiss(ctx context.Context, local garden.LocalRepoID, in review.Dismissal) (review.Dismissal, bool, error)
	Accept(ctx context.Context, local garden.LocalRepoID, p review.Preview, currentHead string, acks []string, confirmed bool) (review.Decision, bool, error)
}

var _ ReviewStore = (*review.FileStore)(nil)

// AttemptLister lists the Garden launch attempts of one clone (Issue 0 is all).
type AttemptLister interface {
	List(ctx context.Context, local garden.LocalRepoID, issue garden.IssueID) ([]launch.Attempt, error)
}

// ReviewAccounts lists agent accounts with their read-only verdicts.
type ReviewAccounts interface {
	List(ctx context.Context) ([]agentsapi.Account, error)
}

// ReviewHost starts and queries agent sessions by durable identity and stops
// one. It is the agent runtime's operation sessions, nothing wider.
type ReviewHost interface {
	agentsapi.OperationSessions
	Supported() bool
	Stop(ctx context.Context, id string) error
}

type reviewHost struct {
	agentsapi.OperationSessions
	sessions agentsapi.Sessions
}

func (h reviewHost) Supported() bool                           { return h.sessions.Supported() }
func (h reviewHost) Stop(ctx context.Context, id string) error { return h.sessions.Stop(ctx, id) }

func loadReviewPrefs() (*config.GardenReviewPrefs, error) {
	st, err := config.LoadState()
	if err != nil {
		return nil, err
	}
	return st.Prefs.GardenReview, nil
}

// policy re-reads the local policy. Only the user's state file can enable
// anything: an unreadable file is the off policy, reported beside it.
func (b *Backend) policy() (review.Policy, config.GardenReviewPrefs, *playgroundapi.Error) {
	if b.deps.Prefs == nil {
		return review.Policy{Trigger: review.TriggerManual}, (*config.GardenReviewPrefs)(nil).Effective(), fail(playgroundapi.CodeUnavailable, "The review policy cannot be read.")
	}
	p, err := b.deps.Prefs()
	if err != nil {
		return review.Policy{Trigger: review.TriggerManual}, (*config.GardenReviewPrefs)(nil).Effective(), fail(playgroundapi.CodeUnavailable, "The review policy could not be read.")
	}
	return review.PolicyFromPrefs(p), p.Effective(), nil
}

func (b *Backend) reviewAccount(ctx context.Context, id string) (agentsapi.Account, bool, error) {
	if b.deps.Accounts == nil {
		return agentsapi.Account{}, false, errors.New("no accounts")
	}
	list, err := b.deps.Accounts.List(ctx)
	if err != nil {
		return agentsapi.Account{}, false, err
	}
	for _, a := range list {
		if string(a.ID) == id {
			return a, true, nil
		}
	}
	return agentsapi.Account{}, false, nil
}

func (b *Backend) hosting() bool { return b.deps.Host != nil && b.deps.Host.Supported() }

// capability is the verdict on the configured account. It never starts anything.
func (b *Backend) capability(acct agentsapi.Account, found bool) playgroundapi.ReviewCapability {
	c := playgroundapi.ReviewCapability{AccountFound: found, HostingSupported: b.hosting()}
	if !found {
		c.Reason = "The review account was not found."
		return c
	}
	c.ReadOnly, c.PlanFile = acct.ReadOnly.Supported, acct.ReadOnly.PlanFile
	c.Supported = c.ReadOnly && c.PlanFile && c.HostingSupported
	switch {
	case !c.ReadOnly:
		c.Reason = clean(acct.ReadOnly.Reason, 200)
		if c.Reason == "" {
			c.Reason = "The provider has no enforced read-only mode."
		}
	case !c.PlanFile:
		c.Reason = "The provider cannot limit writes to the single report file."
	case !c.HostingSupported:
		c.Reason = "Agent sessions cannot be hosted here."
	}
	return c
}

// ReviewPolicy implements playgroundapi.ReviewBackend.
func (b *Backend) ReviewPolicy(ctx context.Context) (playgroundapi.ReviewPolicyView, error) {
	ctx, cancel := context.WithTimeout(ctx, reviewReadTimeout)
	defer cancel()
	gen := b.genPolicy.Add(1)
	pol, eff, perr := b.policy()
	v := playgroundapi.ReviewPolicyView{Stamp: b.stamp(gen), Enabled: pol.Enabled, AccountID: pol.AccountID, Role: playgroundapi.ReviewRole,
		Trigger: pol.Trigger, Scope: eff.Scope, MaxAutoRuns: pol.MaxAutoRuns, RequireCI: pol.RequireCI, BlockOnHigh: pol.BlockOnHigh, PrefsErr: perr}
	if pol.AccountID == "" {
		v.Capability = playgroundapi.ReviewCapability{HostingSupported: b.hosting(), Reason: "No review account is named."}
		return v, nil
	}
	acct, found, err := b.reviewAccount(ctx, pol.AccountID)
	if err != nil {
		if e := ctxErr(ctx); e != nil && !errors.Is(e, context.DeadlineExceeded) {
			return playgroundapi.ReviewPolicyView{}, e
		}
		v.AccountErr = fail(playgroundapi.CodeUnavailable, "The accounts could not be read.")
		v.Capability = playgroundapi.ReviewCapability{HostingSupported: b.hosting(), Reason: "The review account could not be checked."}
		return v, nil
	}
	if found {
		v.AccountName, v.Provider = clean(acct.Name, 80), string(acct.Provider)
	}
	v.Capability = b.capability(acct, found)
	return v, nil
}

// reviewTarget is a session proven to be a Garden implementation attempt of this
// clone, in this clone's worktree.
type reviewTarget struct {
	sc  scoped
	att launch.Attempt
}

// reviewTarget resolves the session by the Changes scope rule, then to its launch
// attempt (verified against the launch store like Changes does). A session that
// is not a reviewable attempt is a fact (reason), not an error: manual sessions
// are not reviewed by policy.
func (b *Backend) reviewTarget(ctx context.Context, id string) (reviewTarget, string, error) {
	sc, err := b.scope(ctx, id)
	if err != nil {
		return reviewTarget{}, "", err
	}
	t := sc.session.Task
	switch {
	case t == nil || t.AttemptID == "":
		return reviewTarget{}, "Manual sessions are not reviewed by policy.", nil
	case t.Role == "reviewer":
		return reviewTarget{}, "This is a reviewer session. Open the implementation session to review it.", nil
	case t.Role != "" && t.Role != "implementation":
		return reviewTarget{}, "Only implementation sessions are reviewed.", nil
	}
	a, ok := b.attempt(ctx, sc.session)
	if !ok {
		if e := ctxErr(ctx); e != nil {
			return reviewTarget{}, "", e
		}
		return reviewTarget{}, "The task attempt could not be verified.", nil
	}
	if !sc.missing && resolve(a.Proposal.Path) != resolve(sc.tree.Path) {
		return reviewTarget{}, "The session does not run in the attempt's worktree.", nil
	}
	return reviewTarget{sc: sc, att: a}, "", nil
}

// readHead reads the worktree's HEAD; ok is false when it is gone or unreadable.
func (b *Backend) readHead(ctx context.Context, t reviewTarget) (head string, err *playgroundapi.Error, cancelled error) {
	if t.sc.missing {
		return "", nil, nil
	}
	c, e := b.headCommit(ctx, t.sc.tree.Path)
	if e != nil {
		if ce := ctxErr(ctx); ce != nil && !errors.Is(ce, context.DeadlineExceeded) {
			return "", nil, ce
		}
		return "", fail(playgroundapi.CodeUnavailable, "The worktree's HEAD could not be read."), nil
	}
	return c.SHA, nil, nil
}

var severityRank = map[review.Severity]int{review.SeverityHigh: 0, review.SeverityMedium: 1, review.SeverityLow: 2, review.SeverityInfo: 3}

func dismissalOf(ds []review.Dismissal, r review.Report, id string) *playgroundapi.FindingDismissal {
	for _, d := range ds {
		if d.AttemptID == r.AttemptID && d.ReportRevision == r.Revision && d.Commit == r.Commit && d.FindingID == id {
			return &playgroundapi.FindingDismissal{Reason: clean(d.Reason, review.MaxReasonRunes), At: d.At, Revision: d.ReportRevision, Commit: d.Commit}
		}
	}
	return nil
}

func findingOf(f review.Finding, r review.Report, ds []review.Dismissal) playgroundapi.ReviewFinding {
	out := playgroundapi.ReviewFinding{ID: f.ID, Severity: f.Severity, Title: clean(f.Title, review.MaxTitleRunes), Path: clean(f.Path, review.MaxPathRunes),
		Line: f.Line, Evidence: clean(f.Evidence, review.MaxEvidenceRunes), Recommendation: clean(f.Recommendation, review.MaxRecommendRunes)}
	if d := dismissalOf(ds, r, f.ID); d != nil {
		out.Dismissed = d
	} else if p, ok := review.PriorDismissal(ds, r.AttemptID, f.ID, r.Revision); ok {
		out.PriorDismissal = p.Label()
	}
	return out
}

func reportOf(r review.Report, all []review.Report, ds []review.Dismissal, head string) playgroundapi.ReviewReport {
	out := playgroundapi.ReviewReport{Revision: r.Revision, Commit: r.Commit, Short: shortSHA(r.Commit), Base: r.Base, State: r.State, FailCode: r.FailCode,
		Trigger: r.Trigger, Auto: r.Auto, Summary: clean(r.Summary, review.MaxSummaryRunes), CreatedAt: r.CreatedAt, ReadyAt: r.ReadyAt,
		Superseded: review.Superseded(all, r), ForOlderCommit: head != "" && r.Commit != head, RunSessionID: r.RunSession}
	for _, f := range r.Findings {
		out.Findings = append(out.Findings, findingOf(f, r, ds))
	}
	sort.SliceStable(out.Findings, func(i, j int) bool {
		return severityRank[out.Findings[i].Severity] < severityRank[out.Findings[j].Severity]
	})
	return out
}

func shortSHA(s string) string {
	if len(s) > 7 {
		return s[:7]
	}
	return s
}

func decisionOf(d review.Decision, head string) playgroundapi.ReviewDecision {
	return playgroundapi.ReviewDecision{ID: d.ID, ReportRevision: d.ReportRevision, ReportCommit: d.ReportCommit, ReviewedHead: d.ReviewedHead,
		Short: shortSHA(d.ReviewedHead), Unresolved: append([]review.FindingRef(nil), d.UnresolvedAtDecision...), Counts: d.UnresolvedCounts, CI: d.CI,
		Policy: d.PolicyFlags, Acknowledged: append([]string(nil), d.Acknowledged...), DecidedAt: d.DecidedAt, ForOlderCommit: head != "" && d.ReviewedHead != head}
}

// loadSettled reads the clone's records after settling the attempt's finished
// runs, so a result that arrived while nobody looked is shown.
func (b *Backend) loadSettled(ctx context.Context, attempt string) (review.Snapshot, error) {
	snap, err := b.deps.Review.Load(ctx, b.local)
	if err != nil {
		return review.Snapshot{}, err
	}
	changed := false
	for _, r := range snap.Reports {
		if r.AttemptID == attempt && r.State == review.StateRunning {
			c, serr := b.settleRun(ctx, r)
			if e := ctxErr(ctx); e != nil && !errors.Is(e, context.DeadlineExceeded) {
				return review.Snapshot{}, e
			}
			changed = changed || (c && serr == nil)
		}
	}
	if changed {
		return b.deps.Review.Load(ctx, b.local)
	}
	return snap, nil
}

// Reviews implements playgroundapi.ReviewBackend.
func (b *Backend) Reviews(ctx context.Context, req playgroundapi.ReviewsRequest) (playgroundapi.ReviewsView, error) {
	ctx, cancel := context.WithTimeout(ctx, reviewReadTimeout)
	defer cancel()
	gen := b.genReviews.Add(1)
	t, reason, err := b.reviewTarget(ctx, req.SessionID)
	if err != nil {
		return playgroundapi.ReviewsView{}, err
	}
	v := playgroundapi.ReviewsView{Stamp: b.stamp(gen), SessionID: req.SessionID}
	if reason != "" {
		v.Reason = reason
		return v, nil
	}
	v.Reviewable, v.AttemptID = true, t.att.ID
	head, herr, cerr := b.readHead(ctx, t)
	if cerr != nil {
		return playgroundapi.ReviewsView{}, cerr
	}
	v.Head, v.HeadRead, v.HeadErr = head, head != "", herr
	if b.deps.Review == nil {
		v.ReportsErr = fail(playgroundapi.CodeStore, "Review records are unavailable.")
		return v, nil
	}
	snap, err := b.loadSettled(ctx, t.att.ID)
	if err != nil {
		if e := ctxErr(ctx); e != nil && !errors.Is(e, context.DeadlineExceeded) {
			return playgroundapi.ReviewsView{}, e
		}
		v.ReportsErr = fail(playgroundapi.CodeStore, "The review records could not be read.")
		return v, nil
	}
	var mine []review.Report
	for _, r := range snap.Reports {
		if r.AttemptID == t.att.ID {
			mine = append(mine, r)
		}
	}
	for i := len(mine) - 1; i >= 0; i-- { // Load orders by revision; newest first here
		r := mine[i]
		v.RunInFlight = v.RunInFlight || r.State == review.StateRunning
		v.Reports = append(v.Reports, reportOf(r, mine, snap.Dismissals, head))
	}
	for i := len(snap.Decisions) - 1; i >= 0; i-- {
		if d := snap.Decisions[i]; d.AttemptID == t.att.ID {
			v.Decisions = append(v.Decisions, decisionOf(d, head))
		}
	}
	return v, nil
}

// reviewFail reduces a review package error to a fixed code.
func reviewFail(ctx context.Context, err error) error {
	if e := ctxErr(ctx); e != nil {
		return e
	}
	switch {
	case errors.Is(err, review.ErrStale):
		return fail(playgroundapi.CodeStale, "The attempt moved on since you looked. Review it again.")
	case errors.Is(err, review.ErrNeedsAcknowledgment):
		return fail(playgroundapi.CodeNeedsAcknowledgment, "Every listed acknowledgment must be given first.")
	case errors.Is(err, review.ErrNotConfirmed):
		return fail(playgroundapi.CodeNotConfirmed, "Confirm to continue.")
	case errors.Is(err, review.ErrAutoLimit):
		return fail(playgroundapi.CodeUnavailable, "The automatic review run limit was reached.")
	case errors.Is(err, review.ErrRunInFlight):
		return fail(playgroundapi.CodeReviewRunning, "A review is already running for this attempt.")
	case errors.Is(err, review.ErrNotFound), errors.Is(err, review.ErrConflict):
		return fail(playgroundapi.CodeStale, "That report or finding changed. Reload and try again.")
	case errors.Is(err, review.ErrInvalid):
		return fail(playgroundapi.CodeInvalidInput, "That input is not valid.")
	}
	return fail(playgroundapi.CodeStore, "The review records could not be read or saved.")
}

// reviewable resolves the session for a call that needs a reviewable attempt and
// the review store.
func (b *Backend) reviewable(ctx context.Context, session string) (reviewTarget, error) {
	t, reason, err := b.reviewTarget(ctx, session)
	if err != nil {
		return reviewTarget{}, err
	}
	if reason != "" {
		return reviewTarget{}, fail(playgroundapi.CodeNotReviewable, reason)
	}
	if b.deps.Review == nil {
		return reviewTarget{}, fail(playgroundapi.CodeStore, "Review records are unavailable.")
	}
	return t, nil
}

// DismissFinding implements playgroundapi.ReviewBackend.
func (b *Backend) DismissFinding(ctx context.Context, req playgroundapi.DismissRequest) (playgroundapi.DismissView, error) {
	ctx, cancel := context.WithTimeout(ctx, reviewReadTimeout)
	defer cancel()
	gen := b.genAction.Add(1)
	reason, rerr := review.CleanReason(req.Reason)
	if rerr != nil || req.ReportRevision < 1 || req.FindingID == "" || len(req.FindingID) > 64 {
		return playgroundapi.DismissView{}, fail(playgroundapi.CodeInvalidInput, "A reason of 1 to 500 characters is required.")
	}
	t, err := b.reviewable(ctx, req.SessionID)
	if err != nil {
		return playgroundapi.DismissView{}, err
	}
	snap, err := b.deps.Review.Load(ctx, b.local)
	if err != nil {
		return playgroundapi.DismissView{}, reviewFail(ctx, err)
	}
	var rep *review.Report
	for i := range snap.Reports {
		if r := &snap.Reports[i]; r.AttemptID == t.att.ID && r.Revision == req.ReportRevision {
			rep = r
		}
	}
	if rep == nil {
		return playgroundapi.DismissView{}, fail(playgroundapi.CodeStale, "That report no longer exists.")
	}
	d, created, err := b.deps.Review.Dismiss(ctx, b.local, review.Dismissal{AttemptID: t.att.ID, ReportRevision: rep.Revision, FindingID: req.FindingID, Commit: rep.Commit, Reason: reason})
	if err != nil {
		return playgroundapi.DismissView{}, reviewFail(ctx, err)
	}
	return playgroundapi.DismissView{Stamp: b.stamp(gen), FindingID: d.FindingID, Created: created,
		Dismissal: playgroundapi.FindingDismissal{Reason: clean(d.Reason, review.MaxReasonRunes), At: d.At, Revision: d.ReportRevision, Commit: d.Commit}}, nil
}

// ciSnapshot reads CI through the independent Checks path. A failed read is
// CIUnavailable, never passed.
func (b *Backend) ciSnapshot(ctx context.Context, session, head string) (review.CISnapshot, *playgroundapi.Error, error) {
	cv, err := b.Checks(ctx, playgroundapi.ChecksRequest{SessionID: session, HeadSHA: head})
	if err != nil {
		if e := ctxErr(ctx); e != nil && !errors.Is(e, context.DeadlineExceeded) {
			return review.CISnapshot{}, nil, e
		}
		return review.CISnapshot{State: review.CIUnavailable}, fail(playgroundapi.CodeUnavailable, "CI could not be read."), nil
	}
	snap := review.CISnapshot{HeadSHA: cv.HeadSHA, FetchedAt: cv.Stamp.FetchedAt}
	switch cv.State {
	case playgroundapi.ChecksPending:
		snap.State = review.CIPending
	case playgroundapi.ChecksPassed:
		snap.State = review.CIPassed
	case playgroundapi.ChecksFailed:
		snap.State = review.CIFailed
	case playgroundapi.ChecksNone:
		snap.State = review.CINone
	default:
		return review.CISnapshot{State: review.CIUnavailable}, fail(playgroundapi.CodeUnavailable, "CI could not be read."), nil
	}
	return snap, nil, nil
}

// acceptFacts are the fresh inputs of a preview.
type acceptFacts struct {
	t      reviewTarget
	head   string
	snap   review.Snapshot
	policy review.Policy
	report *review.Report
}

func (b *Backend) acceptFacts(ctx context.Context, session string) (acceptFacts, error) {
	t, err := b.reviewable(ctx, session)
	if err != nil {
		return acceptFacts{}, err
	}
	pol, _, perr := b.policy()
	if perr != nil {
		return acceptFacts{}, perr
	}
	head, herr, cerr := b.readHead(ctx, t)
	if cerr != nil {
		return acceptFacts{}, cerr
	}
	if herr != nil {
		return acceptFacts{}, herr
	}
	if head == "" {
		return acceptFacts{}, fail(playgroundapi.CodeUnavailable, "The worktree is gone, so there is nothing to accept.")
	}
	snap, err := b.loadSettled(ctx, t.att.ID)
	if err != nil {
		return acceptFacts{}, reviewFail(ctx, err)
	}
	f := acceptFacts{t: t, head: head, snap: snap, policy: pol}
	if r, ok := review.LatestReady(snap.Reports, t.att.ID); ok {
		f.report = &r
	}
	return f, nil
}

func (b *Backend) preview(f acceptFacts, ci review.CISnapshot) review.Preview {
	return review.AcceptPreview(review.PreviewInput{Policy: f.policy, AttemptID: f.t.att.ID, Report: f.report, HeadSHA: f.head, CI: ci,
		Dismissals: f.snap.Dismissals, Now: b.deps.Now()})
}

// PreviewAccept implements playgroundapi.ReviewBackend.
func (b *Backend) PreviewAccept(ctx context.Context, req playgroundapi.AcceptPreviewRequest) (playgroundapi.AcceptPreviewView, error) {
	ctx, cancel := context.WithTimeout(ctx, reviewPreviewBudget)
	defer cancel()
	gen := b.genPreview.Add(1)
	f, err := b.acceptFacts(ctx, req.SessionID)
	if err != nil {
		return playgroundapi.AcceptPreviewView{}, err
	}
	ci, cierr, err := b.ciSnapshot(ctx, req.SessionID, f.head)
	if err != nil {
		return playgroundapi.AcceptPreviewView{}, err
	}
	p := b.preview(f, ci)
	v := playgroundapi.AcceptPreviewView{Stamp: b.stamp(gen), SessionID: req.SessionID, AttemptID: p.AttemptID, HeadSHA: p.HeadSHA,
		ReportRevision: p.ReportRevision, ReportCommit: p.ReportCommit, ReportStale: p.ReportStale, Counts: p.Counts, CI: p.CI, CIErr: cierr,
		Policy: p.Policy, Required: p.Required, AcceptAllowed: p.AcceptAllowed}
	for _, u := range p.Unresolved {
		v.Unresolved = append(v.Unresolved, findingOf(u, *f.report, f.snap.Dismissals))
	}
	return v, nil
}

func unresolvedIDs(ids []string) string {
	sort.Strings(ids)
	return strings.Join(ids, ",")
}

// Accept implements playgroundapi.ReviewBackend. It records the decision and
// does nothing else: no merge, close, push, comment, label, session stop or
// worktree removal exists in this package's dependencies for it to call.
//
// The preview in the request is only a token. The requirements are recomputed
// from fresh reads and the call is refused as stale when HEAD, the latest ready
// report or the unresolved findings differ from what the human saw.
func (b *Backend) Accept(ctx context.Context, req playgroundapi.AcceptRequest) (playgroundapi.AcceptView, error) {
	ctx, cancel := context.WithTimeout(ctx, reviewReadTimeout)
	defer cancel()
	gen := b.genAction.Add(1)
	tok := req.Preview
	if !req.Confirmed {
		return playgroundapi.AcceptView{}, fail(playgroundapi.CodeNotConfirmed, "Confirm to accept.")
	}
	stale := fail(playgroundapi.CodeStale, "The attempt moved on since the preview. Preview it again.")
	f, err := b.acceptFacts(ctx, tok.SessionID)
	if err != nil {
		return playgroundapi.AcceptView{}, err
	}
	if f.t.att.ID != tok.AttemptID || f.head != tok.HeadSHA {
		return playgroundapi.AcceptView{}, stale
	}
	p := b.preview(f, tok.CI)
	var shown []string
	for _, u := range tok.Unresolved {
		shown = append(shown, u.ID)
	}
	var now []string
	for _, u := range p.Unresolved {
		now = append(now, u.ID)
	}
	if p.ReportRevision != tok.ReportRevision || p.ReportCommit != tok.ReportCommit || unresolvedIDs(shown) != unresolvedIDs(now) {
		return playgroundapi.AcceptView{}, stale
	}
	// The store checks HEAD again against the one read just before the write.
	head, herr, cerr := b.readHead(ctx, f.t)
	if cerr != nil {
		return playgroundapi.AcceptView{}, cerr
	}
	if herr != nil || head == "" {
		return playgroundapi.AcceptView{}, fail(playgroundapi.CodeUnavailable, "The worktree's HEAD could not be read.")
	}
	d, created, err := b.deps.Review.Accept(ctx, b.local, p, head, req.Acknowledged, true)
	if err != nil {
		return playgroundapi.AcceptView{}, reviewFail(ctx, err)
	}
	return playgroundapi.AcceptView{Stamp: b.stamp(gen), Decision: decisionOf(d, head), Created: created}, nil
}
