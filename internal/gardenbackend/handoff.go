package gardenbackend

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/approve"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/connect"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/handoff"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/launch"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/planrev"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/reviewpass"
	domain "github.com/Tiago-0liveira/bonsai/internal/git"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
)

var _ gardenapi.HandoffBackend = (*Backend)(nil)

const maxHandoffHistory = 20

// Fixed phrases for sections that could not be read. They never carry provider or Git text.
const (
	reasonNoWorktreeSeam   = "Reading the worktree is unavailable in this build."
	reasonWorktreeCleaned  = "The retained worktree was cleaned."
	reasonWorktreeMissing  = "The retained worktree is missing or not owned by this attempt."
	reasonWorktreeUnread   = "The retained worktree could not be read."
	reasonAlreadyCompleted = "Already completed; merging happened outside Bonsai's control."
	reasonNoAttempt        = "There is no implementation attempt for this Issue."
	acknowledgeNoReview    = "no-ai-review"
	acknowledgeFixes       = "post-review-fixes-unreviewed"
	acknowledgeChecks      = "checks-missing-or-failed"
	acknowledgmentsPrefix  = "Acknowledged: "
	handoffUnknownNextStep = "Refresh, then decide again: Bonsai checks GitHub first and never records it twice."
)

func (b *Backend) now() time.Time {
	if b.deps.LaunchClock != nil {
		return b.deps.LaunchClock.Now().UTC()
	}
	return time.Now().UTC()
}

func (b *Backend) handoffService() (*handoff.Service, error) {
	gate, err := b.approveService()
	if err != nil {
		return nil, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.handoffs != nil {
		return b.handoffs, nil
	}
	svc, err := handoff.New(handoff.Deps{Reader: gate, Writer: b.deps.Writer, Now: b.now})
	if err != nil {
		return nil, err
	}
	b.handoffs = svc
	return svc, nil
}

// handoffTarget resolves the repository and the viewer. A decision needs Write access; reading needs Read.
func (b *Backend) handoffTarget(ctx context.Context, issue uint64, write bool) (approve.Target, connect.Target, error) {
	ct, id, err := b.target(ctx, write)
	if err != nil {
		return approve.Target{}, connect.Target{}, err
	}
	if issue == 0 {
		return approve.Target{}, connect.Target{}, apiError(approve.ErrWrongIssue)
	}
	return approve.Target{Repo: ct.Repository.ID, Issue: garden.IssueID(issue), Viewer: id.ID, Login: id.Login, ReadOnly: ct.Repository.Access < connect.AccessWrite}, ct, nil
}

// handoffRead is one consistent observation of everything a handoff view or decision depends on. Each source
// that failed is marked, so nothing downstream can mistake it for success.
type handoffRead struct {
	tgt     approve.Target
	conn    connect.Target
	attempt *launch.Attempt

	work       WorkIdentity
	workReason string // empty when the worktree was read
	dirty      bool
	zero       bool // the attempt changed nothing, so it can only be completed as non-code work
	// verified is the accepted record whose commits the linked pull request still proves while the worktree is
	// unreadable (cleaned or gone). It only ever feeds the identity; no decision is allowed from it.
	verified *garden.HandoffRecord

	passes    []reviewpass.Pass
	passesErr bool

	log      garden.HandoffLog
	logOK    bool
	live     launch.Inputs
	liveOK   bool
	identity handoff.Identity
	// linkedBase is the branch the attempt's pull request was published against, derived as publish does.
	linkedBase string

	pr         gardenapi.HandoffPR
	decision   handoff.DecisionStatus
	completion handoff.Completion
	review     gardenapi.HandoffReview
	checks     []gardenapi.HandoffCheck
	// checksAllPassed is true only when every configured check has a recorded pass.
	checksAllPassed bool
}

func (r *handoffRead) workOK() bool { return r.workReason == "" }

func (r *handoffRead) inputsCurrent() bool {
	if !r.liveOK {
		return false
	}
	if r.attempt == nil {
		return true
	}
	in := r.attempt.Inputs
	return in.Spec.RevisionHash == r.live.Spec.RevisionHash && in.SpecApprovalID == r.live.SpecApprovalID &&
		in.Plan.RevisionHash == r.live.Plan.RevisionHash && in.Plan.DecisionID == r.live.Plan.DecisionID
}

// handoffAttempt finds the named attempt, or the Issue's newest one. nil means the Issue has none.
func (b *Backend) handoffAttempt(ctx context.Context, svc *launch.Service, issue uint64, id string, repo garden.RepoID) (*launch.Attempt, error) {
	if id != "" {
		a, err := b.localAttempt(ctx, svc, id)
		if err != nil {
			return nil, apiError(err)
		}
		if a.Issue != garden.IssueID(issue) || a.Repo != repo {
			return nil, apiError(launch.ErrOwnership)
		}
		return &a, nil
	}
	list, err := b.deps.LaunchStore.List(ctx, b.local, garden.IssueID(issue))
	if err != nil {
		return nil, apiError(err)
	}
	for i := len(list) - 1; i >= 0; i-- {
		if list[i].Repo == repo {
			return &list[i], nil
		}
	}
	return nil, nil
}

func (b *Backend) readHandoff(ctx context.Context, issue uint64, attemptID string, write bool) (*handoffRead, error) {
	svc, err := b.launchService()
	if err != nil {
		return nil, apiError(err)
	}
	tgt, ct, err := b.handoffTarget(ctx, issue, write)
	if err != nil {
		return nil, err
	}
	r := &handoffRead{tgt: tgt, conn: ct}
	if r.attempt, err = b.handoffAttempt(ctx, svc, issue, attemptID, tgt.Repo); err != nil {
		return nil, err
	}
	if r.attempt != nil {
		b.readWork(ctx, r)
		b.readPasses(ctx, r)
	}
	b.readLog(ctx, r)
	r.verifyWithoutWorktree()
	b.deriveIdentity(r)
	if r.attempt != nil && r.attempt.PR.State == launch.PRLinked && r.attempt.PR.Number > 0 {
		b.readPR(ctx, r)
		// The linked head, not the observed one: a push after acceptance must surface as a changed head
		// rather than as a different decision.
		r.identity.PRNumber, r.identity.PRHeadSHA = int64(r.pr.Number), r.pr.HeadSHA
		r.linkedBase = linkedBase(r.attempt.Proposal.BaseRef, r.attempt.Proposal.RemoteName)
	}
	r.review, r.checks, r.checksAllPassed = reviewSection(r.passes, r.passesErr, r.work.Head)
	r.derive(b.now())
	return r, nil
}

// readWork inspects the retained worktree read-only. Anything short of a proven, owned worktree leaves a reason
// and no head, so the work counts as not ready.
func (b *Backend) readWork(ctx context.Context, r *handoffRead) {
	a := r.attempt
	switch {
	case a.State == launch.StateCleaned:
		r.workReason = reasonWorktreeCleaned
		return
	case b.deps.LaunchWorktrees == nil || b.deps.HandoffWork == nil:
		r.workReason = reasonNoWorktreeSeam
		return
	}
	check, err := b.deps.LaunchWorktrees.InspectIsolation(ctx, a.Proposal)
	switch {
	case err != nil:
		r.workReason = reasonWorktreeUnread
		return
	case !check.Exists || !check.Owned:
		r.workReason = reasonWorktreeMissing
		return
	}
	w, err := b.deps.HandoffWork.Inspect(ctx, a.Proposal.Path, a.Proposal.BaseSHA)
	if err != nil {
		r.workReason = reasonWorktreeUnread
		return
	}
	r.work = w
	r.dirty = w.Dirty || !check.Clean
	r.zero = !r.dirty && w.Head == a.Proposal.BaseSHA
}

func (b *Backend) readPasses(ctx context.Context, r *handoffRead) {
	store, err := b.launchReviewStore()
	if err != nil {
		r.passesErr = true
		return
	}
	list, err := store.ForAttempt(ctx, r.attempt.ID)
	if err != nil {
		r.passesErr = true
		return
	}
	for _, p := range list {
		s := p.Snapshot
		if s.Local == b.local && s.Repo == r.attempt.Repo && s.Issue == r.attempt.Issue {
			r.passes = append(r.passes, p)
		}
	}
}

// readLog reads and verifies the Issue's comment log and the approved pair. A log that is not read completely
// leaves logOK false: decisions and completion are then unknown, never absent.
func (b *Backend) readLog(ctx context.Context, r *handoffRead) {
	gate, err := b.approveService()
	if err != nil {
		return
	}
	log, err := gate.Read(ctx, r.tgt)
	if err != nil || !log.Verification.Complete || r.tgt.Viewer == "" {
		return
	}
	r.log = garden.VerifyHandoffs(garden.IssueIdentity{Repo: r.tgt.Repo, Issue: r.tgt.Issue}, log.Comments,
		garden.ApprovalPolicy{AllowedActorIDs: []string{r.tgt.Viewer}, ReadOnly: r.tgt.ReadOnly})
	r.logOK = true
	r.live, r.liveOK = launch.CurrentInputs(planrev.Derive(log, r.tgt.Viewer))
}

// deriveIdentity builds the identity a decision would be about. With no attempt, or an attempt that changed
// nothing, the work is non-code; otherwise it is the attempt's exact inputs, head, tree and newest review.
func (b *Backend) deriveIdentity(r *handoffRead) {
	a := r.attempt
	id := handoff.Identity{Deliverable: garden.DeliverableNonCode}
	if a == nil {
		if r.liveOK {
			id.SpecRevision, id.SpecApprovalID = r.live.Spec.RevisionHash, r.live.SpecApprovalID
			id.PlanRevision, id.PlanDecisionID = r.live.Plan.RevisionHash, r.live.Plan.DecisionID
		}
		r.identity = id
		return
	}
	id.AttemptID = a.ID
	id.SpecRevision, id.SpecApprovalID = a.Inputs.Spec.RevisionHash, a.Inputs.SpecApprovalID
	id.PlanRevision, id.PlanDecisionID = a.Inputs.Plan.RevisionHash, a.Inputs.Plan.DecisionID
	id.BaseSHA, id.FinalHead, id.FinalTree = a.Proposal.BaseSHA, r.work.Head, r.work.Tree
	if r.verified != nil {
		id.BaseSHA, id.FinalHead, id.FinalTree = r.verified.BaseSHA, r.verified.FinalHead, r.verified.FinalTree
	}
	if !r.zero {
		id.Deliverable = garden.DeliverableCode
	}
	if n := len(r.passes); n > 0 {
		p := r.passes[n-1]
		id.ReviewPass, id.ReviewPassRevision, id.ReportRef = p.ID, p.Revision, p.Report
	}
	r.identity = id
}

// verifyWithoutWorktree lets an accepted result survive cleanup. When the worktree cannot be read but the linked
// pull request was published at exactly the head the current acceptance covers, the acceptance's own commits stand
// in for the missing worktree; Derive still needs the fresh merged pull request to call it done.
func (r *handoffRead) verifyWithoutWorktree() {
	a := r.attempt
	if a == nil || r.workOK() || !r.logOK || a.PR.State != launch.PRLinked || a.PR.Number <= 0 || a.PR.HeadSHA == "" {
		return
	}
	var latest *garden.HandoffEntry
	for i := range r.log.Entries {
		e := &r.log.Entries[i]
		if e.Record.Decision != garden.HandoffRevoked && e.Record.AttemptID == a.ID {
			latest = e
		}
	}
	if latest == nil || latest.Revoked || latest.Record.Decision != garden.HandoffAccepted || latest.Record.FinalHead != a.PR.HeadSHA ||
		latest.Record.PRHeadSHA != "" && latest.Record.PRHeadSHA != a.PR.HeadSHA {
		return
	}
	rec := latest.Record
	r.verified = &rec
}

// linkedBase mirrors the base publish targets: a plain branch, never a detached commit.
func linkedBase(ref, remote string) string {
	if ref == "" || isCommitID(ref) {
		return ""
	}
	return config.BranchOnRemote(ref, remote)
}

func isCommitID(s string) bool {
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

func (b *Backend) readPR(ctx context.Context, r *handoffRead) {
	a := r.attempt
	r.pr = gardenapi.HandoffPR{Linked: true, Number: a.PR.Number, URL: a.PR.URL, HeadSHA: a.PR.HeadSHA, FetchedAt: b.now()}
	if b.deps.HandoffPRs == nil || !validRepoFullName(r.conn.FullName) {
		r.pr.ReadErr = handoff.ReadUnavailable
		return
	}
	d, err := b.deps.HandoffPRs.PullRequest(ctx, r.conn.FullName, a.PR.Number)
	if err != nil {
		r.pr.ReadErr = prReadError(err)
		return
	}
	r.pr.ReadErr = handoff.ReadNone
	r.pr.Observed = &handoff.PRObservation{
		Number: int64(d.Number), Open: d.State == "open" && !d.Merged, Merged: d.Merged || d.State == "merged", MergedAt: d.MergedAt,
		MergeCommitSHA: d.MergeCommitSHA, HeadSHA: d.HeadSHA, BaseRef: d.Base, FetchedAt: r.pr.FetchedAt,
	}
}

// prReadError classifies a failed read of the pull request without keeping any of its text.
func prReadError(err error) handoff.ReadError {
	var de *domain.Error
	var ne net.Error
	var ue *url.Error
	switch {
	case errors.As(err, &de) && de.Code == "not_found", errors.Is(err, garden.ErrNotFound):
		return handoff.ReadNotFound
	case errors.Is(err, connect.ErrOffline), errors.Is(err, garden.ErrOffline), errors.Is(err, context.DeadlineExceeded), errors.As(err, &ne), errors.As(err, &ue):
		return handoff.ReadOffline
	}
	return handoff.ReadUnavailable
}

func (r *handoffRead) derive(now time.Time) {
	if !r.logOK {
		r.decision = handoff.DecisionStatus{}
		r.completion = handoff.Completion{State: handoff.CompletionUnknown, Reasons: []handoff.Reason{handoff.ReasonLogUnreadable}}
		return
	}
	d := handoff.Current(r.log, r.identity)
	if d.State == handoff.DecisionCurrent && r.attempt != nil && !r.inputsCurrent() {
		// The attempt ran against a pair that is no longer the approved one, whatever the record says.
		// With no approved pair at all the zero live inputs differ from the attempt's, so both
		// are reported: a vanished approval must not leave the acceptance current.
		var why []handoff.StaleReason
		in := r.attempt.Inputs
		if in.Spec.RevisionHash != r.live.Spec.RevisionHash || in.SpecApprovalID != r.live.SpecApprovalID {
			why = append(why, handoff.StaleSpec)
		}
		if in.Plan.RevisionHash != r.live.Plan.RevisionHash || in.Plan.DecisionID != r.live.Plan.DecisionID {
			why = append(why, handoff.StalePlan)
		}
		d = handoff.DecisionStatus{State: handoff.DecisionStale, Entry: d.Entry, Stale: why}
	}
	r.decision = d
	in := handoff.Input{Identity: r.identity, Dirty: r.dirty, Decision: d, Now: now}
	if r.attempt != nil {
		in.AttemptState = string(r.attempt.State)
		if r.pr.Linked {
			in.PR = &handoff.LinkedPR{Number: int64(r.pr.Number), HeadSHA: r.pr.HeadSHA, BaseRef: r.linkedBase}
			in.Observed, in.ReadErr = r.pr.Observed, r.pr.ReadErr
		}
	}
	r.completion = handoff.Derive(in)
}

func reviewSection(passes []reviewpass.Pass, unknown bool, head string) (gardenapi.HandoffReview, []gardenapi.HandoffCheck, bool) {
	if unknown {
		return gardenapi.HandoffReview{State: gardenapi.HandoffReviewUnknown, ReviewUnknown: true}, nil, false
	}
	if len(passes) == 0 {
		return gardenapi.HandoffReview{State: gardenapi.HandoffReviewNone}, nil, false
	}
	p := passes[len(passes)-1]
	rv := gardenapi.HandoffReview{PassID: p.ID, PassRevision: p.Revision, Code: p.Code, ReviewedHead: p.ReviewedHead, FinalHead: p.FinalHead, ReportRef: p.Report}
	switch p.State {
	case reviewpass.StatePending:
		rv.State = gardenapi.HandoffReviewAuthorizedNotRun
	case reviewpass.StateStarting, reviewpass.StateStartUnknown, reviewpass.StateRunning:
		rv.State = gardenapi.HandoffReviewRunning
	case reviewpass.StateChecking:
		rv.State = gardenapi.HandoffReviewChecking
	case reviewpass.StateFailed:
		rv.State = gardenapi.HandoffReviewFailed
	case reviewpass.StateStopped:
		rv.State = gardenapi.HandoffReviewStopped
	case reviewpass.StateBlocked:
		rv.State = gardenapi.HandoffReviewBlocked
	case reviewpass.StateAwaitingHumanReview:
		rv.State = gardenapi.HandoffReviewCompleted
	default:
		rv.State = gardenapi.HandoffReviewUnknown
		rv.ReviewUnknown = true
	}
	rv.FixesAfterReview = p.ReviewedHead != "" && p.FinalHead != "" && p.FinalHead != p.ReviewedHead
	rv.NoSecondReview = rv.State == gardenapi.HandoffReviewCompleted && head != "" && p.ReviewedHead != "" && head != p.ReviewedHead
	byID := make(map[string]reviewpass.CheckResult, len(p.Checks))
	for _, c := range p.Checks {
		byID[c.CheckID] = c
	}
	var checks []gardenapi.HandoffCheck
	allPassed := len(p.Snapshot.Policy.Checks) > 0
	for _, c := range p.Snapshot.Policy.Checks {
		hc := gardenapi.HandoffCheck{Name: displayText(c.Name, 80), Outcome: string(reviewpass.OutcomeUnknown)}
		if res, ok := byID[c.ID]; ok {
			hc.Outcome, hc.ExitCode, hc.EndedAt = string(res.Outcome), res.ExitCode, res.EndedAt
		}
		allPassed = allPassed && hc.Outcome == string(reviewpass.OutcomePassed)
		checks = append(checks, hc)
	}
	return rv, checks, allPassed && rv.State == gardenapi.HandoffReviewCompleted
}

// displayText removes what could steer a terminal from text a person or a provider wrote. Newlines survive.
func displayText(s string, limit int) string {
	s = strings.ToValidUTF8(s, "�")
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\n':
			b.WriteRune(r)
		case r == '\t':
			b.WriteString("    ")
		case unicode.IsControl(r), unicode.Is(unicode.Cf, r):
		default:
			b.WriteRune(r)
		}
	}
	out := b.String()
	if limit > 0 {
		if rs := []rune(out); len(rs) > limit {
			out = string(rs[:limit-1]) + "…"
		}
	}
	return out
}

func refuse(code gardenapi.Code, msg string) gardenapi.HandoffAction {
	return gardenapi.HandoffAction{Code: code, Message: msg}
}

var allowed = gardenapi.HandoffAction{Allowed: true}

// decisionGate is why no decision can be recorded at all right now, or the zero action when none blocks.
func (r *handoffRead) decisionGate() (gardenapi.HandoffAction, bool) {
	switch {
	case r.tgt.ReadOnly:
		return refuse(gardenapi.CodeReadOnly, "Recording a decision needs Write access to the repository."), true
	case !r.logOK:
		return refuse(gardenapi.CodeHandoffUnknown, "GitHub's comment log could not be read completely."), true
	case !r.liveOK || !r.inputsCurrent():
		return refuse(gardenapi.CodeHandoffBlocked, "The approved specification or plan changed since this attempt ran, or is not approved."), true
	}
	return gardenapi.HandoffAction{}, false
}

// codeGate is why a code decision cannot be recorded about this attempt right now.
func (r *handoffRead) codeGate() (gardenapi.HandoffAction, bool) {
	if a, blocked := r.decisionGate(); blocked {
		return a, true
	}
	switch {
	case r.attempt == nil:
		return refuse(gardenapi.CodeHandoffBlocked, reasonNoAttempt), true
	case r.completion.State == handoff.CompletionDone:
		return refuse(gardenapi.CodeHandoffBlocked, "This work is already done."), true
	case r.attempt.State != launch.StateAwaitingReview:
		return refuse(gardenapi.CodeHandoffBlocked, "The attempt has not finished and awaits no human review."), true
	case !r.workOK():
		return refuse(gardenapi.CodeHandoffBlocked, r.workReason), true
	case r.zero:
		return refuse(gardenapi.CodeHandoffBlocked, "The attempt changed no files, so there is nothing to accept or send back."), true
	case r.dirty:
		return refuse(gardenapi.CodeHandoffBlocked, "The worktree has uncommitted changes. Commit or discard them first."), true
	}
	return gardenapi.HandoffAction{}, false
}

func (r *handoffRead) currentEntry() *garden.HandoffEntry {
	if r.decision.State == handoff.DecisionCurrent {
		return r.decision.Entry
	}
	return nil
}

func feedbackUsed(a *launch.Attempt, id int64) bool {
	for _, w := range a.Reworks {
		if w.FeedbackCommentID == id {
			return true
		}
	}
	return false
}

func (r *handoffRead) actions() gardenapi.HandoffActions {
	var out gardenapi.HandoffActions
	cur := r.currentEntry()
	if gate, blocked := r.codeGate(); blocked {
		out.Accept, out.RequestChanges = gate, gate
	} else {
		// Deciding the same thing again is allowed: the service finds the recorded one instead of writing twice.
		out.Accept, out.RequestChanges = allowed, allowed
	}
	out.Revoke = refuse(gardenapi.CodeHandoffBlocked, "There is no current acceptance or completion to revoke.")
	if gate, blocked := r.decisionGate(); blocked {
		out.Revoke = gate
	} else if r.completion.State == handoff.CompletionDone {
		out.Revoke = refuse(gardenapi.CodeHandoffBlocked, reasonAlreadyCompleted)
	} else if cur != nil && (cur.Record.Decision == garden.HandoffAccepted || cur.Record.Decision == garden.HandoffCompleted) {
		out.Revoke = allowed
	}
	out.CompleteNonCode = r.completeGate()
	out.Rework = r.reworkGate()
	out.OpenPR = refuse(gardenapi.CodeHandoffBlocked, "No pull request is linked to this attempt.")
	if r.pr.Linked && r.pr.URL != "" {
		out.OpenPR = allowed
	}
	out.Cleanup = refuse(gardenapi.CodeHandoffBlocked, "Cleanup is not available for this work.")
	return out
}

func orDefault(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

func (r *handoffRead) completeGate() gardenapi.HandoffAction {
	if gate, blocked := r.decisionGate(); blocked {
		return gate
	}
	switch {
	case r.attempt != nil && r.attempt.State != launch.StateAwaitingReview:
		return refuse(gardenapi.CodeHandoffBlocked, "The attempt has not finished and awaits no human review.")
	case r.attempt != nil && !r.workOK():
		return refuse(gardenapi.CodeHandoffBlocked, r.workReason)
	case r.attempt != nil && r.dirty:
		return refuse(gardenapi.CodeHandoffBlocked, "The worktree has uncommitted changes. Commit or discard them first.")
	case r.attempt != nil && !r.zero:
		return refuse(gardenapi.CodeHandoffBlocked, "The attempt changed files: accept the code instead of completing without it.")
	case r.completion.State == handoff.CompletionDone:
		return refuse(gardenapi.CodeHandoffBlocked, "This work is already done.")
	}
	return allowed
}

func (r *handoffRead) reworkGate() gardenapi.HandoffAction {
	cur := r.currentEntry()
	switch {
	case r.attempt == nil:
		return refuse(gardenapi.CodeHandoffBlocked, reasonNoAttempt)
	case r.completion.State == handoff.CompletionDone:
		return refuse(gardenapi.CodeHandoffBlocked, reasonAlreadyCompleted)
	case r.tgt.ReadOnly:
		return refuse(gardenapi.CodeReadOnly, "Starting a rework needs Write access to the repository.")
	case !r.logOK:
		return refuse(gardenapi.CodeHandoffUnknown, "GitHub's comment log could not be read completely.")
	case cur == nil || cur.Record.Decision != garden.HandoffChangesRequested:
		return refuse(gardenapi.CodeHandoffBlocked, "Request changes on the current result first.")
	case r.attempt.State != launch.StateAwaitingReview && !r.attempt.ReworkRefeedable(cur.CommentID):
		return refuse(gardenapi.CodeHandoffBlocked, "The attempt is not awaiting human review.")
	case feedbackUsed(r.attempt, cur.CommentID) && !r.attempt.ReworkRefeedable(cur.CommentID):
		return refuse(gardenapi.CodeHandoffBlocked, "This feedback already started a rework.")
	case launch.ValidateFeedback(cur.Detail()) != nil:
		return refuse(gardenapi.CodeHandoffBlocked, "The feedback contains characters Bonsai cannot pass to the agent; edit the GitHub comment or request changes again.")
	case !r.inputsCurrent():
		return refuse(gardenapi.CodeHandoffBlocked, "The approved specification or plan changed since this attempt ran.")
	}
	return allowed
}

func entryView(e garden.HandoffEntry) gardenapi.HandoffEntryView {
	return gardenapi.HandoffEntryView{CommentID: e.CommentID, Decision: e.Record.Decision, Author: displayText(e.Author, 80), At: e.CreatedAt,
		Text: displayText(e.Detail(), 4<<10), Revoked: e.Revoked}
}

func (r *handoffRead) history() []gardenapi.HandoffEntryView {
	var out []gardenapi.HandoffEntryView
	for _, e := range r.log.Entries {
		if e.Record.Decision == garden.HandoffRevoked || e.Record.AttemptID != r.identity.AttemptID {
			continue
		}
		if r.identity.AttemptID == "" && e.Record.Deliverable != garden.DeliverableNonCode {
			continue
		}
		out = append(out, entryView(e))
	}
	if len(out) > maxHandoffHistory {
		out = out[len(out)-maxHandoffHistory:]
	}
	return out
}

func (r *handoffRead) outcome() string {
	a := r.attempt
	if a == nil || len(a.Runs) == 0 {
		return "No implementation run"
	}
	run := a.Runs[len(a.Runs)-1]
	switch {
	case run.State == "exited" && run.ExitCode != nil && *run.ExitCode == 0:
		return fmt.Sprintf("Implementation run %d exited cleanly", run.Number)
	case run.State == "exited" && run.ExitCode != nil:
		return fmt.Sprintf("Implementation run %d exited with code %d", run.Number, *run.ExitCode)
	case run.State == "failed":
		return fmt.Sprintf("Implementation run %d failed", run.Number)
	}
	return fmt.Sprintf("Implementation run %d is %s", run.Number, displayText(run.State, 20))
}

func (r *handoffRead) view(stamp gardenapi.Stamp, now time.Time) gardenapi.HandoffView {
	v := gardenapi.HandoffView{
		Stamp: stamp, IssueID: uint64(r.tgt.Issue), FetchedAt: now, Outcome: r.outcome(), Review: r.review, Checks: r.checks,
		ChecksRecorded: r.hasChecks(), PR: r.pr, Completion: r.completion, Actions: r.actions(), InputsCurrent: r.inputsCurrent(),
		SpecLabel: label(r.identity.SpecRevision, r.identity.SpecApprovalID, "approval"), PlanLabel: label(r.identity.PlanRevision, r.identity.PlanDecisionID, "decision"),
	}
	if r.attempt != nil {
		v.AttemptID = r.attempt.ID
	}
	v.Changes = gardenapi.HandoffChanges{Available: r.workOK() && r.attempt != nil, Reason: r.workReason, BaseSHA: r.identity.BaseSHA, FinalHead: r.work.Head,
		FinalTree: r.work.Tree, Dirty: r.dirty, Files: r.work.Files, Totals: r.work.Totals, Truncated: r.work.Truncated}
	if r.attempt == nil {
		v.Changes.Reason = reasonNoAttempt
	}
	v.Decision.State = "unknown"
	if r.logOK {
		v.Decision.State = string(r.decision.State)
		v.Decision.Stale = r.decision.Stale
		if r.decision.Entry != nil {
			e := entryView(*r.decision.Entry)
			v.Decision.Entry = &e
		}
		v.History = r.history()
	}
	return v
}

func (r *handoffRead) hasChecks() bool {
	for _, c := range r.checks {
		if c.Outcome != string(reviewpass.OutcomeUnknown) {
			return true
		}
	}
	return false
}

func label(hash string, id int64, kind string) string {
	if hash == "" {
		return ""
	}
	return fmt.Sprintf("%s (%s #%d)", shortHash(hash), kind, id)
}

// Handoff implements gardenapi.HandoffBackend. It reads and writes nothing; the pull request is read fresh.
func (b *Backend) Handoff(ctx context.Context, req gardenapi.HandoffRequest) (gardenapi.HandoffView, error) {
	gen := b.gen.Add(1)
	ctx, cancel := context.WithTimeout(ctx, launchReadTimeout)
	defer cancel()
	b.invalidateAuth()
	r, err := b.readHandoff(ctx, req.IssueID, req.AttemptID, false)
	if err != nil {
		return gardenapi.HandoffView{}, err
	}
	v := r.view(b.stamp(gen, r.tgt.Repo), b.now())
	if r.attempt != nil {
		v.Actions.Cleanup = b.cleanupAvailability(ctx, r.attempt.ID)
	}
	return v, nil
}

func (b *Backend) cleanupAvailability(ctx context.Context, id string) gardenapi.HandoffAction {
	svc, err := b.launchService()
	if err != nil {
		return refuse(gardenapi.CodeHandoffBlocked, "Cleanup is unavailable in this build.")
	}
	p, err := svc.PreviewCleanup(ctx, id)
	switch {
	case err != nil:
		return refuse(gardenapi.CodeLaunchCleanupUnsafe, "Cleanup could not be checked.")
	case !p.Allowed:
		return refuse(gardenapi.CodeLaunchCleanupUnsafe, orDefault(p.Blocker, "The worktree cannot be removed safely."))
	}
	return allowed
}

// acknowledgments are what a person must accept to accept work that lacks full evidence.
func (r *handoffRead) acknowledgments() []string {
	var out []string
	switch {
	case r.review.State != gardenapi.HandoffReviewCompleted:
		out = append(out, acknowledgeNoReview)
	case r.review.NoSecondReview:
		out = append(out, acknowledgeFixes)
	}
	if !r.checksAllPassed {
		out = append(out, acknowledgeChecks)
	}
	return out
}

func (r *handoffRead) decisionAction(d garden.HandoffDecision) (gardenapi.HandoffAction, bool) {
	a := r.actions()
	switch d {
	case garden.HandoffAccepted:
		return a.Accept, true
	case garden.HandoffChangesRequested:
		return a.RequestChanges, true
	case garden.HandoffCompleted:
		return a.CompleteNonCode, true
	case garden.HandoffRevoked:
		return a.Revoke, true
	}
	return gardenapi.HandoffAction{}, false
}

// digestInput is every frozen fact of a decision preview. The generation is left out: it changes on each call.
type digestInput struct {
	Garden, Epoch, Issue uint64
	LocalID              string
	Attempt, ActorID     string
	Identity             handoff.Identity
	Decision             garden.HandoffDecision
	Text                 string
	Target               int64
	Acks                 []string
	Body                 string
	PRNumber             int64
	PRHeadSHA            string
	PROpen, PRMerged     bool
	PRBaseRef            string
}

func (b *Backend) previewHandoff(ctx context.Context, gen uint64, req gardenapi.HandoffDecisionRequest) (gardenapi.HandoffDecisionPreview, error) {
	r, err := b.readHandoff(ctx, req.IssueID, req.AttemptID, true)
	if err != nil {
		return gardenapi.HandoffDecisionPreview{}, err
	}
	act, ok := r.decisionAction(req.Decision)
	if !ok || req.Decision != garden.HandoffRevoked && req.TargetCommentID != 0 {
		return gardenapi.HandoffDecisionPreview{}, apiError(handoff.ErrInvalidDecision)
	}
	if !act.Allowed {
		return gardenapi.HandoffDecisionPreview{}, &gardenapi.Error{Code: act.Code, Message: act.Message}
	}
	text := strings.TrimSpace(req.Text)
	switch {
	case text == "" && (req.Decision == garden.HandoffChangesRequested || req.Decision == garden.HandoffCompleted):
		return gardenapi.HandoffDecisionPreview{}, apiError(handoff.ErrTextRequired)
	case len(text) > handoff.MaxTextBytes || strings.Contains(text, "bonsai:garden/"):
		return gardenapi.HandoffDecisionPreview{}, apiError(handoff.ErrInvalidText)
	}
	p := gardenapi.HandoffDecisionPreview{IssueID: req.IssueID, AttemptID: r.identity.AttemptID, Actor: r.tgt.Login, ActorID: r.tgt.Viewer,
		Identity: r.identity, Decision: req.Decision, Text: text, TargetCommentID: req.TargetCommentID}
	if req.Decision == garden.HandoffRevoked {
		cur := r.currentEntry()
		if cur == nil || cur.CommentID != req.TargetCommentID {
			return gardenapi.HandoffDecisionPreview{}, &gardenapi.Error{Code: gardenapi.CodeHandoffChanged, Message: "The record to revoke is no longer the current one.", Next: "Refresh the handoff."}
		}
	}
	if req.Decision == garden.HandoffAccepted {
		p.Acknowledgments = r.acknowledgments()
	}
	p.Body = text
	if len(p.Acknowledgments) > 0 {
		ack := acknowledgmentsPrefix + strings.Join(p.Acknowledgments, ", ") + "."
		if p.Body != "" {
			p.Body += "\n\n"
		}
		p.Body += ack
	}
	p.Summary = summaryOf(req.Decision, r.identity, req.TargetCommentID)
	if r.pr.Linked {
		p.PRNumber, p.PRHeadSHA = int64(r.pr.Number), r.pr.HeadSHA
		if r.pr.Observed != nil {
			o := r.pr.Observed
			p.PRHeadSHA, p.PROpen, p.PRMerged, p.PRBaseRef = o.HeadSHA, o.Open, o.Merged, o.BaseRef
		}
	}
	p.Stamp = b.stamp(gen, r.tgt.Repo)
	p.Digest = digestOf(p)
	return p, nil
}

func summaryOf(d garden.HandoffDecision, id handoff.Identity, target int64) string {
	switch d {
	case garden.HandoffAccepted:
		return fmt.Sprintf("Accept attempt %s at %s", id.AttemptID, shortHash(id.FinalHead))
	case garden.HandoffChangesRequested:
		return fmt.Sprintf("Request changes on attempt %s at %s", id.AttemptID, shortHash(id.FinalHead))
	case garden.HandoffCompleted:
		return "Complete without code changes"
	}
	return "Revoke handoff comment " + strconv.FormatInt(target, 10)
}

func digestOf(p gardenapi.HandoffDecisionPreview) string {
	acks := p.Acknowledgments
	if acks == nil {
		// nil and empty must digest alike: a round trip through JSON turns one into the other.
		acks = []string{}
	}
	in := digestInput{Garden: p.Stamp.GardenID, Epoch: p.Stamp.Epoch, Issue: p.IssueID, LocalID: p.Stamp.LocalRepoID, Attempt: p.AttemptID,
		ActorID: p.ActorID, Identity: p.Identity, Decision: p.Decision, Text: p.Text, Target: p.TargetCommentID, Acks: acks, Body: p.Body,
		PRNumber: p.PRNumber, PRHeadSHA: p.PRHeadSHA, PROpen: p.PROpen, PRMerged: p.PRMerged, PRBaseRef: p.PRBaseRef}
	raw, _ := json.Marshal(in)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// PreviewHandoffDecision implements gardenapi.HandoffBackend. It writes nothing.
func (b *Backend) PreviewHandoffDecision(ctx context.Context, req gardenapi.HandoffDecisionRequest) (gardenapi.HandoffDecisionPreview, error) {
	gen := b.gen.Add(1)
	ctx, cancel := context.WithTimeout(ctx, launchReadTimeout)
	defer cancel()
	b.invalidateAuth()
	return b.previewHandoff(ctx, gen, req)
}

// DecideHandoff implements gardenapi.HandoffBackend. It re-reads the work, the review, the pull request and the
// log, and writes only when the recomputed preview is exactly the one confirmed.
func (b *Backend) DecideHandoff(ctx context.Context, p gardenapi.HandoffDecisionPreview, confirmed bool) (gardenapi.HandoffDecisionView, error) {
	gen := b.gen.Add(1)
	if !confirmed {
		return gardenapi.HandoffDecisionView{}, &gardenapi.Error{Code: gardenapi.CodeHandoffBlocked, Message: "The decision was not confirmed.", Next: "Confirm exactly what is shown."}
	}
	svc, err := b.handoffService()
	if err != nil {
		return gardenapi.HandoffDecisionView{}, apiError(err)
	}
	if p.Stamp.LocalRepoID != b.local.String() || p.IssueID == 0 || p.Digest == "" || p.Digest != digestOf(p) {
		return gardenapi.HandoffDecisionView{}, apiError(launch.ErrOwnership)
	}
	ctx, cancel := context.WithTimeout(ctx, launchReadTimeout)
	defer cancel()
	// Confirmation must observe live permissions and the actual authenticated account.
	b.invalidateAuth()
	fresh, err := b.previewHandoff(ctx, gen, gardenapi.HandoffDecisionRequest{IssueID: p.IssueID, AttemptID: p.AttemptID, Decision: p.Decision, Text: p.Text, TargetCommentID: p.TargetCommentID})
	var ae *gardenapi.Error
	if errors.As(err, &ae) && ae.Code == gardenapi.CodeHandoffBlocked {
		err = &gardenapi.Error{Code: gardenapi.CodeHandoffChanged, Message: "That decision is no longer possible: " + ae.Message, Next: "Refresh the handoff."}
	}
	if err != nil {
		return gardenapi.HandoffDecisionView{}, err
	}
	if fresh.Digest != p.Digest {
		return gardenapi.HandoffDecisionView{}, &gardenapi.Error{Code: gardenapi.CodeHandoffChanged, Message: "The work, the review, the pull request or the account changed since the preview, so nothing was recorded.", Next: "Refresh the handoff and decide again."}
	}
	t, _, err := b.handoffTarget(ctx, p.IssueID, true)
	if err != nil {
		return gardenapi.HandoffDecisionView{}, err
	}
	res, err := svc.Decide(ctx, handoff.DecideRequest{Target: t, Identity: p.Identity, Decision: p.Decision, Text: p.Body, ActorID: p.ActorID, TargetCommentID: p.TargetCommentID})
	v := gardenapi.HandoffDecisionView{Stamp: fresh.Stamp, IssueID: p.IssueID, AttemptID: p.AttemptID, CommentID: res.CommentID, Decision: p.Decision, Replayed: res.Replayed}
	switch {
	case err == nil:
		v.State = gardenapi.PlanWriteDone
		return v, nil
	case errors.Is(err, handoff.ErrUnverified):
		v.State = gardenapi.PlanWriteUnknown
		v.Err = &gardenapi.Error{Code: gardenapi.CodeHandoffUnknown, Message: "Bonsai could not confirm the decision on GitHub.", Next: handoffUnknownNextStep}
		return v, nil
	case errors.Is(err, handoff.ErrAppendFailed) && !definitiveWrite(err):
		v.State = gardenapi.PlanWriteUnknown
		v.Err = &gardenapi.Error{Code: gardenapi.CodeHandoffUnknown, Message: "The decision may have been recorded.", Next: handoffUnknownNextStep}
		return v, nil
	}
	return gardenapi.HandoffDecisionView{}, apiError(err)
}

func launchOptionsOf(o launch.Options) agents.LaunchOptions {
	l := agents.LaunchOptions{Model: o.Model, Effort: o.Effort, Sandbox: o.Sandbox, Approval: o.Approval, PermissionMode: o.PermissionMode}
	if o.FullAccess != nil {
		v := *o.FullAccess
		l.FullAccess = &v
	}
	return l
}

func (b *Backend) reworkPreview(ctx context.Context, gen uint64, r *handoffRead) (gardenapi.ReworkPreview, error) {
	if g := r.reworkGate(); !g.Allowed {
		return gardenapi.ReworkPreview{}, &gardenapi.Error{Code: g.Code, Message: g.Message}
	}
	a, e := r.attempt, r.currentEntry()
	return gardenapi.ReworkPreview{
		Stamp: b.stamp(gen, a.Repo), IssueID: uint64(a.Issue), AttemptID: a.ID, FeedbackCommentID: e.CommentID, Feedback: e.Detail(),
		AccountName: displayText(a.AccountName, 80), Provider: a.Provider, Native: a.Options,
		Escalated: agents.EscalatedLaunch(a.Provider, launchOptionsOf(a.Options)), Review: b.resolveLaunchReview(ctx),
	}, nil
}

// PreviewRework implements gardenapi.HandoffBackend. It reads and verifies; nothing is reserved or started.
func (b *Backend) PreviewRework(ctx context.Context, issue uint64, attemptID string) (gardenapi.ReworkPreview, error) {
	gen := b.gen.Add(1)
	ctx, cancel := context.WithTimeout(ctx, launchReadTimeout)
	defer cancel()
	b.invalidateAuth()
	r, err := b.readHandoff(ctx, issue, attemptID, true)
	if err != nil {
		return gardenapi.ReworkPreview{}, err
	}
	if attemptID == "" && r.attempt == nil {
		return gardenapi.ReworkPreview{}, &gardenapi.Error{Code: gardenapi.CodeHandoffBlocked, Message: reasonNoAttempt}
	}
	return b.reworkPreview(ctx, gen, r)
}

// The run number keeps a refed start (after a definitively failed one) from reusing a pass already stopped as
// implementation-not-started.
func reworkReviewID(attemptID string, feedback int64, run int) string {
	sum := sha256.Sum256([]byte(attemptID + "\x00rework\x00" + strconv.FormatInt(feedback, 10) + "\x00" + strconv.Itoa(run)))
	return fmt.Sprintf("rp-%x", sum[:16])
}

// reserveReworkReview authorizes a new review pass for the run a rework is about to start. It never reuses the
// pass of an earlier run: the id is distinct per attempt and feedback, so a replay finds the same one.
func (b *Backend) reserveReworkReview(ctx context.Context, a launch.Attempt, feedback int64, review gardenapi.LaunchReview, actor string, escalation bool) (reviewpass.Pass, error) {
	snapshot := reviewpass.Snapshot{
		Local: b.local, Repo: a.Repo, Issue: a.Issue, Number: a.Number,
		Inputs: reviewpass.Inputs{SpecRevision: a.Inputs.Spec.RevisionHash, SpecFingerprint: a.Inputs.Spec.Fingerprint, SpecApprovalID: a.Inputs.SpecApprovalID,
			PlanRevision: a.Inputs.Plan.RevisionHash, PlanDecisionID: a.Inputs.Plan.DecisionID},
		AttemptID: a.ID, Worktree: reviewpass.Worktree{Path: a.Proposal.Path, Branch: a.Proposal.Branch, BaseSHA: a.Proposal.BaseSHA}, Policy: review.Policy, PolicyHash: review.Hash,
	}
	run := fmt.Sprintf("%s/start/%d", a.ID, len(a.Runs)+1)
	return b.reservePass(ctx, reworkReviewID(a.ID, feedback, len(a.Runs)+1), fmt.Sprintf("%s/rework/%d", a.ID, feedback), run, snapshot, actor, escalation)
}

// unstartedPasses lists the attempt's reserved-but-never-started passes, other than keepID. Review execution may not
// exist yet, so such a pass would otherwise block every rework forever. A pass whose reviewer started (or may have)
// still blocks. It changes nothing.
func (b *Backend) unstartedPasses(ctx context.Context, attemptID, keepID string) ([]string, error) {
	store, err := b.launchReviewStore()
	if err != nil {
		return nil, fmt.Errorf("open review passes: %w", err)
	}
	list, err := store.ForAttempt(ctx, attemptID)
	if err != nil {
		return nil, fmt.Errorf("read review passes: %w", err)
	}
	var unstarted []string
	for _, p := range list {
		switch {
		case !p.State.Active() || p.ID == keepID:
		case p.State == reviewpass.StatePending && p.Reviewer == nil:
			unstarted = append(unstarted, p.ID)
		default:
			return nil, reviewpass.ErrActive
		}
	}
	return unstarted, nil
}

// closeUnstartedPass moves a pending pass whose reviewer never started to stopped with a fixed code, so history is
// kept. A pass that moved since it was listed is not ours to close.
func (b *Backend) closeUnstartedPass(ctx context.Context, id, code string) error {
	store, err := b.launchReviewStore()
	if err != nil {
		return fmt.Errorf("open review passes: %w", err)
	}
	now := b.now()
	_, err = store.Update(ctx, id, func(p reviewpass.Pass) (reviewpass.Pass, bool, error) {
		if p.State != reviewpass.StatePending || p.Reviewer != nil {
			return p, false, reviewpass.ErrActive
		}
		end := now
		if end.Before(p.CreatedAt) {
			end = p.CreatedAt
		}
		p.State, p.Code, p.EndedAt = reviewpass.StateStopped, code, &end
		return p, true, nil
	})
	if err != nil {
		return fmt.Errorf("close review pass: %w", err)
	}
	return nil
}

// StartRework implements gardenapi.HandoffBackend. It starts one more run in the retained worktree for exactly
// the feedback that was previewed. The same feedback never starts a second run or a second review pass.
func (b *Backend) StartRework(ctx context.Context, req gardenapi.ReworkStartRequest) (gardenapi.LaunchAttemptView, error) {
	gen := b.gen.Add(1)
	svc, err := b.launchService()
	if err != nil {
		return gardenapi.LaunchAttemptView{}, apiError(err)
	}
	p := req.Preview
	if p.Stamp.LocalRepoID != b.local.String() || p.IssueID == 0 || p.AttemptID == "" || p.FeedbackCommentID <= 0 {
		return gardenapi.LaunchAttemptView{}, apiError(launch.ErrOwnership)
	}
	if !req.Confirmed {
		return gardenapi.LaunchAttemptView{}, apiError(launch.ErrNotAccepted)
	}
	ctx, cancel := context.WithTimeout(ctx, launchRunTimeout)
	defer cancel()
	known, err := b.localAttempt(ctx, svc, p.AttemptID)
	if err != nil {
		return gardenapi.LaunchAttemptView{}, apiError(err)
	}
	if uint64(known.Issue) != p.IssueID {
		return gardenapi.LaunchAttemptView{}, apiError(launch.ErrOwnership)
	}
	if feedbackUsed(&known, p.FeedbackCommentID) && !known.ReworkRefeedable(p.FeedbackCommentID) {
		// A replay shows the attempt, so it needs the same read access any view does.
		b.invalidateAuth()
		tgt, _, err := b.handoffTarget(ctx, p.IssueID, false)
		if err != nil {
			return gardenapi.LaunchAttemptView{}, err
		}
		if known.Repo != tgt.Repo {
			return gardenapi.LaunchAttemptView{}, apiError(launch.ErrOwnership)
		}
		return b.attemptView(ctx, gen, known), nil
	}
	b.invalidateAuth()
	r, err := b.readHandoff(ctx, p.IssueID, p.AttemptID, true)
	if err != nil {
		return gardenapi.LaunchAttemptView{}, err
	}
	fresh, err := b.reworkPreview(ctx, gen, r)
	if err != nil {
		return gardenapi.LaunchAttemptView{}, err
	}
	if fresh.FeedbackCommentID != p.FeedbackCommentID || fresh.Feedback != p.Feedback || fresh.Review.Hash != p.Review.Hash || fresh.Escalated != p.Escalated ||
		fresh.Stamp.GardenID != p.Stamp.GardenID || fresh.Stamp.Epoch != p.Stamp.Epoch {
		return gardenapi.LaunchAttemptView{}, &gardenapi.Error{Code: gardenapi.CodeLaunchChanged, Message: "The feedback, the automatic review policy or the repository changed since the preview.", Next: "Preview the rework again before confirming."}
	}
	if fresh.Escalated && !req.EscalationConfirmed {
		return gardenapi.LaunchAttemptView{}, apiError(launch.ErrEscalationNotAccepted)
	}
	if fresh.Review.Hash != "" && fresh.Review.Policy.Reviewer.Escalated && !req.ReviewEscalationConfirmed {
		return gardenapi.LaunchAttemptView{}, apiError(launch.ErrEscalationNotAccepted)
	}
	newID := ""
	if fresh.Review.Hash != "" {
		newID = reworkReviewID(r.attempt.ID, p.FeedbackCommentID, len(r.attempt.Runs)+1)
	}
	// Everything that can refuse is checked before anything is written, so a refusal leaves the old pass untouched.
	stale, err := b.unstartedPasses(ctx, r.attempt.ID, newID)
	if err != nil {
		return gardenapi.LaunchAttemptView{}, apiError(err)
	}
	rr := launch.ReworkRequest{AttemptID: p.AttemptID, FeedbackCommentID: p.FeedbackCommentID, Feedback: fresh.Feedback,
		Confirmed: true, EscalationConfirmed: req.EscalationConfirmed}
	if err := svc.CheckRework(ctx, rr); err != nil {
		return gardenapi.LaunchAttemptView{}, apiError(err)
	}
	// An unstarted pass is closed whatever the policy now says: nothing may stay active for work that was replaced.
	for _, id := range stale {
		if err := b.closeUnstartedPass(ctx, id, reviewpass.CodeSuperseded); err != nil {
			return gardenapi.LaunchAttemptView{}, apiError(err)
		}
	}
	var frozen *reviewpass.Pass
	if newID != "" {
		// Reserve first so a crash after the holder starts cannot lose the authorized review.
		pass, err := b.reserveReworkReview(ctx, *r.attempt, p.FeedbackCommentID, fresh.Review, r.tgt.Viewer, req.ReviewEscalationConfirmed)
		if err != nil {
			return gardenapi.LaunchAttemptView{}, apiError(err)
		}
		frozen = &pass
	}
	a, err := svc.Rework(ctx, rr)
	if err != nil && !errors.Is(err, launch.ErrOutcomeUnknown) && frozen != nil {
		// The run definitively did not start, so its pass must not stay authorized. An unknown outcome is left to
		// reconciliation by identity.
		if cerr := b.closeUnstartedPass(context.WithoutCancel(ctx), frozen.ID, reviewpass.CodeImplementationNotStarted); cerr != nil {
			err = fmt.Errorf("%w: %w", err, cerr)
		}
	}
	if a.ID == "" {
		if err == nil {
			err = &gardenapi.Error{Code: gardenapi.CodeLaunchUnknown, Message: "The rework returned no attempt.", Next: "Refresh the attempt."}
		}
		return gardenapi.LaunchAttemptView{}, apiError(err)
	}
	if err == nil && !feedbackUsed(&a, p.FeedbackCommentID) {
		err = launch.ErrHolderActive
	}
	view := b.attemptView(ctx, gen, a)
	if frozen != nil && err == nil {
		view.Review, view.ReviewUnknown = frozen, false
		// Narrow window: a concurrent caller may have stopped the reserved pass between reserve and start. The run
		// is already going and stays, but the view must show the pass's real state, not the reserved one.
		if store, serr := b.launchReviewStore(); serr == nil {
			if cur, gerr := store.Get(ctx, frozen.ID); gerr == nil && !cur.State.Active() {
				view.Review = &cur
			}
		}
	}
	return view, apiError(err)
}

// OpenHandoffPR implements gardenapi.HandoffBackend. It opens only the repository's own https pull request URL
// for the linked number, never a URL taken from the attempt as stored.
func (b *Backend) OpenHandoffPR(ctx context.Context, issue uint64, attemptID string) error {
	svc, err := b.launchService()
	if err != nil {
		return apiError(err)
	}
	ctx, cancel := context.WithTimeout(ctx, launchReadTimeout)
	defer cancel()
	t, ct, err := b.handoffTarget(ctx, issue, false)
	if err != nil {
		return err
	}
	a, err := b.handoffAttempt(ctx, svc, issue, attemptID, t.Repo)
	if err != nil {
		return err
	}
	if a == nil || a.PR.State != launch.PRLinked || a.PR.Number <= 0 {
		return &gardenapi.Error{Code: gardenapi.CodeHandoffBlocked, Message: "No pull request is linked to this attempt."}
	}
	want := fmt.Sprintf("https://github.com/%s/pull/%d", ct.FullName, a.PR.Number)
	if !validRepoFullName(ct.FullName) || !strings.EqualFold(a.PR.URL, want) {
		return &gardenapi.Error{Code: gardenapi.CodeHandoffBlocked, Message: "The recorded pull request link is not this repository's pull request URL.", Next: "Open the pull request from GitHub yourself."}
	}
	if err := b.deps.OpenURL(want); err != nil {
		return &gardenapi.Error{Code: gardenapi.CodeFailed, Message: "The local pull request link action failed."}
	}
	return nil
}
