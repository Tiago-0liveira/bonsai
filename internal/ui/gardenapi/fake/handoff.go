package fake

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/handoff"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/launch"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
)

// HandoffSample names one scripted handoff situation.
type HandoffSample string

const (
	// HandoffHumanReview: AI review completed with one fix after review, checks passed, draft PR not merged.
	HandoffHumanReview HandoffSample = "human-review"
	// HandoffNoReviewNoChecks: no AI review and no checks recorded.
	HandoffNoReviewNoChecks HandoffSample = "no-review-no-checks"
	// HandoffAcceptedAwaitingMerge: accepted, the pull request is open.
	HandoffAcceptedAwaitingMerge HandoffSample = "accepted-awaiting-merge"
	// HandoffDone: accepted and squash merged.
	HandoffDone HandoffSample = "done"
	// HandoffBlocked: the pull request was closed without merging.
	HandoffBlocked HandoffSample = "blocked"
	// HandoffStale: the head moved after the acceptance.
	HandoffStale HandoffSample = "stale"
	// HandoffOfflinePR: the pull request could not be read, so completion is unknown.
	HandoffOfflinePR HandoffSample = "offline-pr"
	// HandoffChangesRequested: changes requested and a rework is available.
	HandoffChangesRequested HandoffSample = "changes-requested"
	// HandoffNonCodeDone: work without a code deliverable, completed with recorded evidence.
	HandoffNonCodeDone HandoffSample = "non-code-done"
)

// HandoffSamples lists every sample, for tests that walk them all.
func HandoffSamples() []HandoffSample {
	return []HandoffSample{HandoffHumanReview, HandoffNoReviewNoChecks, HandoffAcceptedAwaitingMerge, HandoffDone, HandoffBlocked, HandoffStale,
		HandoffOfflinePR, HandoffChangesRequested, HandoffNonCodeDone}
}

const (
	sampleAttempt = "impl-sample01"
	sampleBase    = "1111111111111111111111111111111111111111"
	sampleHead    = "2222222222222222222222222222222222222222"
	sampleReviewd = "3333333333333333333333333333333333333333"
	sampleTree    = "4444444444444444444444444444444444444444"
	sampleIssue   = 42
)

var sampleTime = time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)

func allowedAction() gardenapi.HandoffAction { return gardenapi.HandoffAction{Allowed: true} }

func refused(msg string) gardenapi.HandoffAction {
	return gardenapi.HandoffAction{Code: gardenapi.CodeHandoffBlocked, Message: msg}
}

func intPtr(v int) *int { return &v }

func entryAt(id int64, d garden.HandoffDecision, text string) *gardenapi.HandoffEntryView {
	return &gardenapi.HandoffEntryView{CommentID: id, Decision: d, Author: SampleLogin, At: sampleTime.Add(time.Hour), Text: text}
}

func sampleCodeView() gardenapi.HandoffView {
	return gardenapi.HandoffView{
		Stamp: stamp(1, 1, SampleRepoID), IssueID: sampleIssue, AttemptID: sampleAttempt, FetchedAt: sampleTime,
		SpecLabel: "a1b2c3d4 (approval #3)", PlanLabel: "e5f6a7b8 (decision #5)", Outcome: "Implementation run 1 exited cleanly",
		Changes: gardenapi.HandoffChanges{Available: true, BaseSHA: sampleBase, FinalHead: sampleHead, FinalTree: sampleTree,
			Files:  []gardenapi.FileChange{{Path: "internal/counter/counter.go", Add: 42, Del: 7}, {Path: "internal/counter/counter_test.go", Add: 60}},
			Totals: gardenapi.ChangeTotals{Files: 2, Add: 102, Del: 7}},
		Review: gardenapi.HandoffReview{State: gardenapi.HandoffReviewCompleted, PassID: "rp-" + strings.Repeat("a", 32), PassRevision: 4,
			ReviewedHead: sampleReviewd, FinalHead: sampleHead, FixesAfterReview: true, ReportRef: "ev-" + strings.Repeat("b", 32)},
		Checks:         []gardenapi.HandoffCheck{{Name: "Go tests", Outcome: "passed", ExitCode: intPtr(0), EndedAt: &sampleTime}},
		ChecksRecorded: true,
		PR:             gardenapi.HandoffPR{Linked: true, Number: 7, URL: "https://github.com/acme/my-app/pull/7", HeadSHA: sampleHead, FetchedAt: sampleTime, ReadErr: handoff.ReadNone, Observed: &handoff.PRObservation{Number: 7, Open: true, HeadSHA: sampleHead, FetchedAt: sampleTime}},
		Decision:       gardenapi.HandoffDecisionState{State: string(handoff.DecisionNone)},
		Completion:     handoff.Completion{State: handoff.CompletionAwaitingDecision},
		InputsCurrent:  true,
		Actions: gardenapi.HandoffActions{Accept: allowedAction(), RequestChanges: allowedAction(), OpenPR: allowedAction(), Cleanup: allowedAction(),
			Revoke: refused("There is no current acceptance or completion to revoke."), CompleteNonCode: refused("The attempt changed files: accept the code instead of completing without it."),
			Rework: refused("Request changes on the current result first.")},
	}
}

func accepted(v gardenapi.HandoffView) gardenapi.HandoffView {
	v.Decision = gardenapi.HandoffDecisionState{State: string(handoff.DecisionCurrent), Entry: entryAt(9001, garden.HandoffAccepted, "Acknowledged: nothing.")}
	v.History = []gardenapi.HandoffEntryView{*v.Decision.Entry}
	v.Completion = handoff.Completion{State: handoff.CompletionAccepted, Reasons: []handoff.Reason{handoff.ReasonPRNotMerged}, Evidence: handoff.Evidence{Source: "handoff-record", CommentID: 9001, VerifiedAt: sampleTime}}
	v.Actions.Accept = refused("This exact work is already accepted.")
	v.Actions.Revoke = allowedAction()
	return v
}

// SampleHandoff returns the view of one scripted situation.
func SampleHandoff(s HandoffSample) gardenapi.HandoffView {
	v := sampleCodeView()
	switch s {
	case HandoffNoReviewNoChecks:
		v.Review = gardenapi.HandoffReview{State: gardenapi.HandoffReviewNone}
		v.Checks, v.ChecksRecorded = nil, false
	case HandoffAcceptedAwaitingMerge:
		v = accepted(v)
	case HandoffDone:
		v = accepted(v)
		at := sampleTime.Add(2 * time.Hour)
		v.PR.Observed = &handoff.PRObservation{Number: 7, Merged: true, MergedAt: &at, MergeCommitSHA: strings.Repeat("5", 40), HeadSHA: sampleHead, FetchedAt: sampleTime}
		v.Completion = handoff.Completion{State: handoff.CompletionDone, Evidence: handoff.Evidence{Source: "github-pr", CommentID: 9001, PRNumber: 7, MergeCommitSHA: strings.Repeat("5", 40), MergedAt: &at, VerifiedAt: sampleTime}}
		// Merging happened outside Bonsai's control, so nothing about the decision can be redone; opening and cleanup stay.
		done := refused("Already completed; merging happened outside Bonsai's control.")
		v.Actions.Accept, v.Actions.RequestChanges, v.Actions.Revoke, v.Actions.Rework = done, done, done, done
	case HandoffBlocked:
		v = accepted(v)
		v.PR.Observed = &handoff.PRObservation{Number: 7, HeadSHA: sampleHead, FetchedAt: sampleTime}
		v.Completion = handoff.Completion{State: handoff.CompletionBlocked, Reasons: []handoff.Reason{handoff.ReasonPRClosedUnmerged}, Evidence: v.Completion.Evidence}
	case HandoffStale:
		v = accepted(v)
		v.Decision.State = string(handoff.DecisionStale)
		v.Decision.Stale = []handoff.StaleReason{handoff.StaleHead, handoff.StaleTree}
		v.Completion = handoff.Completion{State: handoff.CompletionStale, Reasons: []handoff.Reason{handoff.ReasonAcceptanceStale, "head-changed", "tree-changed"}}
		v.Actions.Revoke = refused("There is no current acceptance or completion to revoke.")
		v.Actions.Accept = allowedAction()
	case HandoffOfflinePR:
		v = accepted(v)
		v.PR.Observed, v.PR.ReadErr = nil, handoff.ReadOffline
		v.Completion = handoff.Completion{State: handoff.CompletionUnknown, Reasons: []handoff.Reason{handoff.ReasonPROffline}, Evidence: v.Completion.Evidence}
	case HandoffChangesRequested:
		v.Decision = gardenapi.HandoffDecisionState{State: string(handoff.DecisionCurrent), Entry: entryAt(9002, garden.HandoffChangesRequested, "Rename the counter and cover the overflow case.")}
		v.History = []gardenapi.HandoffEntryView{*v.Decision.Entry}
		v.Completion = handoff.Completion{State: handoff.CompletionChangesRequested, Evidence: handoff.Evidence{Source: "handoff-record", CommentID: 9002, VerifiedAt: sampleTime}}
		v.Actions.Rework = allowedAction()
	case HandoffNonCodeDone:
		v.AttemptID, v.Outcome = "", "No implementation run"
		v.Changes = gardenapi.HandoffChanges{Reason: "There is no implementation attempt for this Issue."}
		v.Review, v.Checks, v.ChecksRecorded = gardenapi.HandoffReview{State: gardenapi.HandoffReviewNone}, nil, false
		v.PR = gardenapi.HandoffPR{}
		v.Decision = gardenapi.HandoffDecisionState{State: string(handoff.DecisionCurrent), Entry: entryAt(9003, garden.HandoffCompleted, "Decision recorded in the design doc, linked from the Issue.")}
		v.History = []gardenapi.HandoffEntryView{*v.Decision.Entry}
		v.Completion = handoff.Completion{State: handoff.CompletionDone, Evidence: handoff.Evidence{Source: "handoff-record", CommentID: 9003, VerifiedAt: sampleTime}}
		v.Actions = gardenapi.HandoffActions{Accept: refused("No implementation attempt to decide."), RequestChanges: refused("No implementation attempt to decide."),
			Revoke: refused("Already completed; merging happened outside Bonsai's control."), CompleteNonCode: refused("This work is already done."), Rework: refused("There is no implementation attempt for this Issue."),
			OpenPR: refused("No pull request is linked to this attempt."), Cleanup: refused("Cleanup is not available for this work.")}
	}
	return v
}

// HandoffBackend replays scripted handoff views and records every call. It implements gardenapi.HandoffBackend
// without touching GitHub, Git or the disk. Fill the exported fields before use; an error field makes that call
// fail. Nothing it does merges, closes, cleans or pushes.
type HandoffBackend struct {
	View    gardenapi.HandoffView
	ViewErr error
	// PreviewErr, DecideErr, ReworkPreviewErr, StartReworkErr and OpenErr make that call fail.
	PreviewErr, DecideErr, ReworkPreviewErr, StartReworkErr, OpenErr error
	// DecideView is what DecideHandoff reports, restamped per call.
	DecideView gardenapi.HandoffDecisionView
	// ReworkPreviewView is what PreviewRework reports; ReworkResult is what StartRework reports.
	ReworkPreviewView gardenapi.ReworkPreview
	ReworkResult      gardenapi.LaunchAttemptView
	// Acknowledgments are listed by every previewed Accept.
	Acknowledgments []string

	// Requests, Decided, Reworked and Opened record what each call received, in order.
	Requests []gardenapi.HandoffRequest
	Previews []gardenapi.HandoffDecisionRequest
	Decided  []gardenapi.HandoffDecisionPreview
	Reworked []gardenapi.ReworkStartRequest
	Opened   []string

	mu    sync.Mutex
	gen   uint64
	calls []string
}

var _ gardenapi.HandoffBackend = (*HandoffBackend)(nil)

// NewHandoff returns a backend that serves the sample.
func NewHandoff(s HandoffSample) *HandoffBackend {
	v := SampleHandoff(s)
	return &HandoffBackend{
		View:       v,
		DecideView: gardenapi.HandoffDecisionView{IssueID: v.IssueID, AttemptID: v.AttemptID, State: gardenapi.PlanWriteDone, CommentID: 9100},
		ReworkPreviewView: gardenapi.ReworkPreview{IssueID: v.IssueID, AttemptID: v.AttemptID, FeedbackCommentID: 9002, Feedback: "Rename the counter and cover the overflow case.",
			AccountName: "personal", Provider: "codex", Native: launch.Options{Sandbox: "workspace-write", Approval: "on-request"}},
		ReworkResult: gardenapi.LaunchAttemptView{IssueID: v.IssueID},
	}
}

// Calls lists the methods called so far, in order.
func (b *HandoffBackend) Calls() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.calls...)
}

func (b *HandoffBackend) begin(method string) gardenapi.Stamp {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.gen++
	b.calls = append(b.calls, method)
	return stamp(b.gen, 1, SampleRepoID)
}

func (b *HandoffBackend) Handoff(ctx context.Context, req gardenapi.HandoffRequest) (gardenapi.HandoffView, error) {
	st := b.begin("Handoff")
	b.mu.Lock()
	defer b.mu.Unlock()
	b.Requests = append(b.Requests, req)
	if b.ViewErr != nil {
		return gardenapi.HandoffView{}, b.ViewErr
	}
	v := b.View
	v.Stamp = st
	return v, errOr(nil, ctx)
}

func (b *HandoffBackend) PreviewHandoffDecision(ctx context.Context, req gardenapi.HandoffDecisionRequest) (gardenapi.HandoffDecisionPreview, error) {
	st := b.begin("PreviewHandoffDecision")
	b.mu.Lock()
	defer b.mu.Unlock()
	b.Previews = append(b.Previews, req)
	if b.PreviewErr != nil {
		return gardenapi.HandoffDecisionPreview{}, b.PreviewErr
	}
	p := gardenapi.HandoffDecisionPreview{Stamp: st, IssueID: req.IssueID, AttemptID: req.AttemptID, Actor: SampleLogin, ActorID: ViewerID,
		Decision: req.Decision, Text: strings.TrimSpace(req.Text), TargetCommentID: req.TargetCommentID, Summary: string(req.Decision) + " (fake)"}
	p.Identity.AttemptID = req.AttemptID
	if req.Decision == garden.HandoffAccepted {
		p.Acknowledgments = append([]string(nil), b.Acknowledgments...)
	}
	p.Body = p.Text
	if len(p.Acknowledgments) > 0 {
		p.Body = strings.TrimSpace(p.Body + "\n\nAcknowledged: " + strings.Join(p.Acknowledgments, ", ") + ".")
	}
	p.Digest = fmt.Sprintf("fake-%s-%s-%d", req.Decision, p.Body, req.TargetCommentID)
	return p, errOr(nil, ctx)
}

func (b *HandoffBackend) DecideHandoff(ctx context.Context, p gardenapi.HandoffDecisionPreview, confirmed bool) (gardenapi.HandoffDecisionView, error) {
	st := b.begin("DecideHandoff")
	b.mu.Lock()
	defer b.mu.Unlock()
	b.Decided = append(b.Decided, p)
	if b.DecideErr != nil {
		return gardenapi.HandoffDecisionView{}, b.DecideErr
	}
	if !confirmed {
		return gardenapi.HandoffDecisionView{}, &gardenapi.Error{Code: gardenapi.CodeHandoffBlocked, Message: "The decision was not confirmed."}
	}
	v := b.DecideView
	v.Stamp, v.IssueID, v.AttemptID, v.Decision = st, p.IssueID, p.AttemptID, p.Decision
	return v, errOr(nil, ctx)
}

func (b *HandoffBackend) PreviewRework(ctx context.Context, issue uint64, attemptID string) (gardenapi.ReworkPreview, error) {
	st := b.begin("PreviewRework")
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.ReworkPreviewErr != nil {
		return gardenapi.ReworkPreview{}, b.ReworkPreviewErr
	}
	p := b.ReworkPreviewView
	p.Stamp, p.IssueID, p.AttemptID = st, issue, attemptID
	return p, errOr(nil, ctx)
}

func (b *HandoffBackend) StartRework(ctx context.Context, req gardenapi.ReworkStartRequest) (gardenapi.LaunchAttemptView, error) {
	st := b.begin("StartRework")
	b.mu.Lock()
	defer b.mu.Unlock()
	b.Reworked = append(b.Reworked, req)
	if b.StartReworkErr != nil {
		return gardenapi.LaunchAttemptView{}, b.StartReworkErr
	}
	v := b.ReworkResult
	v.Stamp = st
	return v, errOr(nil, ctx)
}

func (b *HandoffBackend) OpenHandoffPR(ctx context.Context, issue uint64, attemptID string) error {
	b.begin("OpenHandoffPR")
	b.mu.Lock()
	defer b.mu.Unlock()
	b.Opened = append(b.Opened, fmt.Sprintf("%d/%s", issue, attemptID))
	if b.OpenErr != nil {
		return b.OpenErr
	}
	return errOr(nil, ctx)
}
