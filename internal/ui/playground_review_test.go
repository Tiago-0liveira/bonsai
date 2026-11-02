package ui

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/agentterminal"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/review"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/modals"
	"github.com/Tiago-0liveira/bonsai/internal/ui/playgroundapi"
	pgfake "github.com/Tiago-0liveira/bonsai/internal/ui/playgroundapi/fake"
)

type reviewUIEnv struct {
	*chgEnv
	review *pgfake.Review
}

func newReviewUIEnv(t *testing.T, w, h int) *reviewUIEnv {
	t.Helper()
	e := newChgEnv(t, w, h)
	head := strings.Repeat("a", 40)
	old := strings.Repeat("b", 40)
	r := &pgfake.Review{
		Policy: playgroundapi.ReviewPolicyView{AccountID: "acct", AccountName: "reviewer", Provider: "codex", Role: playgroundapi.ReviewRole, Trigger: review.TriggerManual, MaxAutoRuns: 3, Capability: playgroundapi.ReviewCapability{Supported: true}},
		ReviewsBySession: map[string]playgroundapi.ReviewsView{"s1": {Reviewable: true, AttemptID: "att1", Head: head, HeadRead: true, Reports: []playgroundapi.ReviewReport{
			{Revision: 2, Commit: head, Base: review.Base{Ref: "main", MergeBase: old}, State: review.StateReady, Summary: "Check boundary handling", Findings: []playgroundapi.ReviewFinding{
				{ID: "f1", Severity: review.SeverityHigh, Title: "Missing guard", Path: "guard.go", Line: 42, Evidence: "Input reaches the write", Recommendation: "Validate ownership first", PriorDismissal: "previously dismissed in r1"},
				{ID: "f2", Severity: review.SeverityLow, Title: "Extra lookup", Path: "read.go", Line: 7, Evidence: "Repeated read", Recommendation: "Reuse the result"},
			}},
			{Revision: 1, Commit: old, State: review.StateReady, Superseded: true, ForOlderCommit: true, Findings: []playgroundapi.ReviewFinding{{ID: "f1", Severity: review.SeverityHigh, Title: "Missing guard", Dismissed: &playgroundapi.FindingDismissal{Reason: "accepted risk", Revision: 1, Commit: old}}}},
		}, Decisions: []playgroundapi.ReviewDecision{{ReviewedHead: old, ReportRevision: 1, CI: review.CISnapshot{State: review.CIPassed}, DecidedAt: time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)}}}},
	}
	e.m.pg.api = e.pa.WithReview(r)
	e.key(t, "r")
	if !e.m.pg.rev.open {
		t.Fatal("review did not open")
	}
	return &reviewUIEnv{e, r}
}

func (e *reviewUIEnv) submit(t *testing.T, kind modals.Kind, value string) {
	t.Helper()
	var cmd tea.Cmd
	e.m, cmd = send(t, e.m, modals.SubmitMsg{Kind: kind, Value: value})
	e.m = pgDrain(t, e.m, cmd)
}

func TestPlaygroundReviewEvidenceHistoryAndSizing(t *testing.T) {
	for _, sz := range [][2]int{{80, 24}, {120, 35}, {160, 45}} {
		t.Run(fmt.Sprintf("%dx%d", sz[0], sz[1]), func(t *testing.T) {
			e := newReviewUIEnv(t, sz[0], sz[1])
			all := flat(ansi.Strip(strings.Join(e.m.pgReviewLines(sz[0], 200), "\n")))
			mustContain(t, "review evidence", all, "REPORT r2", "Missing guard", "guard.go:42", "Validate ownership first", "previously dismissed in r1", "Unresolved", "Report history", "Human acceptance", "FOR OLDER COMMIT")
			for _, line := range strings.Split(view(e.m), "\n") {
				if ansi.StringWidth(line) != sz[0] {
					t.Fatalf("width=%d", ansi.StringWidth(line))
				}
			}
			e.key(t, "]")
			mustContain(t, "old revision", strings.Join(e.m.pgReviewLines(sz[0], 200), "\n"), "REPORT r1", "accepted risk", "Superseded", "FOR OLDER COMMIT")
			e.key(t, "[", "n")
			if e.m.pg.rev.revision != 2 || e.m.pg.rev.finding != 1 {
				t.Fatal("report/finding navigation failed")
			}
			if len(e.review.StartRequests)+len(e.review.AcceptRequests)+len(e.review.DismissRequests) != 0 {
				t.Fatal("navigation mutated review")
			}
		})
	}
}

func TestPlaygroundReviewFreshnessAndLateReads(t *testing.T) {
	e := newReviewUIEnv(t, 120, 35)
	original := e.m.pg.rev.reports.val
	newer := original
	newer.Head = strings.Repeat("c", 40)
	newer.ReportsErr = &playgroundapi.Error{Code: playgroundapi.CodeStore}
	e.review.ReviewsBySession["s1"] = newer
	e.key(t, "r")
	if !e.m.pg.rev.reports.stale() || e.m.pg.rev.head.val.Head != newer.Head || len(e.m.pg.rev.reports.val.Reports) != 2 {
		t.Fatal("partial failure lost independent last good data")
	}
	mustContain(t, "partial error", strings.Join(e.m.pgReviewLines(120, 200), "\n"), "STALE reports", "FOR OLDER COMMIT")
	e.review.ReviewsErr = errors.New("raw SECRET provider error")
	e.key(t, "r")
	if strings.Contains(view(e.m), "SECRET") {
		t.Fatal("raw backend error leaked")
	}
	e.review.ReviewsErr = nil
	// A successful empty report list really clears it.
	e.review.ReviewsBySession["s1"] = playgroundapi.ReviewsView{Reviewable: true, HeadRead: true, Head: newer.Head}
	e.key(t, "r")
	if len(e.m.pg.rev.reports.val.Reports) != 0 {
		t.Fatal("successful empty read did not clear reports")
	}
	oldGen := e.m.pg.rev.reports.gen
	e.key(t, "esc", "down", "r")
	before := e.m.pg.rev.reports.val
	e.m, _ = send(t, e.m, pgReviewReportsMsg{pgReadMsg[playgroundapi.ReviewsView]{key: "s1", gen: oldGen, v: original}, oldGen})
	if e.m.pg.rev.reports.val.SessionID != before.SessionID {
		t.Fatal("late reply replaced another session's reports")
	}
}

func TestPlaygroundReviewExplicitRetryAndDismissal(t *testing.T) {
	e := newReviewUIEnv(t, 120, 35)
	e.key(t, "t")
	if e.m.modal == nil || e.m.modal.Kind() != modals.KindPlaygroundReviewRetry {
		t.Fatal("retry did not confirm")
	}
	e.key(t, "enter") // cancel is selected first
	if len(e.review.StartRequests) != 0 {
		t.Fatal("cancel started reviewer")
	}
	e.key(t, "t", "up", "enter")
	if len(e.review.StartRequests) != 1 {
		t.Fatal("confirmed retry did not start once")
	}
	req := e.review.StartRequests[0]
	if !req.Confirmed || req.ExpectHead != strings.Repeat("a", 40) || req.RequestID == "" || req.SessionID != "s1" {
		t.Fatalf("retry=%+v", req)
	}
	e.key(t, "d")
	e.submit(t, modals.KindPlaygroundReviewDismiss, "  ")
	if len(e.review.DismissRequests) != 0 {
		t.Fatal("empty reason was submitted")
	}
	e.key(t, "d")
	e.submit(t, modals.KindPlaygroundReviewDismiss, "  accepted risk  ")
	if e.m.modal == nil || e.m.modal.Kind() != modals.KindPlaygroundReviewDismissAsk {
		t.Fatal("dismissal did not confirm")
	}
	e.key(t, "up", "enter")
	if len(e.review.DismissRequests) != 1 {
		t.Fatal("dismissal not recorded")
	}
	d := e.review.DismissRequests[0]
	if d.ReportRevision != 2 || d.FindingID != "f1" || d.Reason != "accepted risk" {
		t.Fatalf("dismiss=%+v", d)
	}
}

func TestPlaygroundAcceptancePreviewAcknowledgmentsAndLateResponse(t *testing.T) {
	e := newReviewUIEnv(t, 80, 24)
	e.review.PreviewBySession["s1"] = playgroundapi.AcceptPreviewView{AttemptID: "att1", HeadSHA: strings.Repeat("a", 40), ReportRevision: 2, ReportCommit: strings.Repeat("a", 40), AcceptAllowed: true,
		Unresolved: e.m.pg.rev.reports.val.Reports[0].Findings, Counts: map[review.Severity]int{review.SeverityHigh: 1, review.SeverityLow: 1}, CI: review.CISnapshot{State: review.CIFailed, HeadSHA: strings.Repeat("a", 40), FetchedAt: e.pa.Now()}, Required: []review.Ack{review.AckOpenHigh, review.AckCINotPassed}}
	e.key(t, "a")
	if e.m.modal != nil || e.m.pg.rev.action.preview == nil {
		t.Fatal("full scrollable preview not shown")
	}
	all := strings.Join(e.m.pgReviewPreviewLines(80, 200), "\n")
	mustContain(t, "preview", all, "UNRESOLVED FINDINGS: 2", "Missing guard", "Extra lookup", "CI: failed", "Required acknowledgment")
	e.key(t, "enter")
	for i := 0; i < 2; i++ {
		if len(e.review.AcceptRequests) != 0 {
			t.Fatal("accepted before conditions and final confirmation")
		}
		e.key(t, "up", "enter")
	}
	if len(e.review.AcceptRequests) != 0 {
		t.Fatal("accepted without final confirmation")
	}
	e.key(t, "up", "enter")
	if len(e.review.AcceptRequests) != 1 || len(e.review.AcceptRequests[0].Acknowledged) != 2 || !e.review.AcceptRequests[0].Confirmed {
		t.Fatal("acceptance lost acknowledgments")
	}
	// A preview that finishes after a workspace change must never open a dialog.
	nm, cmd := e.m.pgReviewKey(keyOf("a"))
	e.m = nm.(Model)
	preview := cmd()
	e.m, _ = e.m.selectWorkspace(wsSettings)
	e.m, _ = send(t, e.m, preview)
	if e.m.modal != nil || e.m.pg.rev.action.preview != nil {
		t.Fatal("late preview opened acceptance in another workspace")
	}
}

func TestPlaygroundReviewManualSessionAndReconcile(t *testing.T) {
	e := newReviewUIEnv(t, 120, 35)
	e.review.ReviewsBySession["s2"] = playgroundapi.ReviewsView{Reason: "Manual session: no verified implementation attempt"}
	e.key(t, "esc", "down", "r", "t", "d", "a", "f")
	if len(e.review.StartRequests)+len(e.review.DismissRequests)+len(e.review.AcceptRequests) != 0 || e.m.workspace != wsPlayground {
		t.Fatal("unlinked session acquired review actions")
	}
	mustContain(t, "manual", view(e.m), "Manual session")
	// Reconcile is driven by a single completion-scheduled loop in every workspace.
	e.m.workspace = wsSettings
	handled, m, cmd := e.m.pgReviewMsg(pgReviewTickMsg{})
	if !handled || cmd == nil || !m.pg.rev.reconcileBusy {
		t.Fatal("no reconciliation outside Playground")
	}
	_, m, duplicate := m.pgReviewMsg(pgReviewTickMsg{})
	if duplicate != nil {
		t.Fatal("overlapping reconciliation")
	}
	result := cmd()
	_, m, next := m.pgReviewMsg(result)
	if m.pg.rev.reconcileBusy || next == nil {
		t.Fatal("reconciliation loop did not resume")
	}
}

func TestPlaygroundRequestFixesUsesExistingForm(t *testing.T) {
	m, _, lb := fakeLaunchUIEnv(t, 120, 35)
	// Reuse the ready canonical Issue and account resolver from the launch tests.
	m.workspace = wsPlayground
	m.shellFocus = shellBody
	s := agentsapi.Session{Summary: agentterminal.Summary{ID: "s1", State: "running", Task: &agents.TaskRef{RepositoryID: uint64(lb.StatusView.Target.RepoID), IssueID: 1, AttemptID: "att1"}}}
	m.pg.recs = []pgRec{{Session: s}}
	m.pg.loaded = true
	m.pg.sel = s.ID
	m.pg.rev = pgReview{open: true, session: s.ID, reports: pgCall[playgroundapi.ReviewsView]{have: true, val: playgroundapi.ReviewsView{Reviewable: true, AttemptID: "att1"}}, head: pgCall[playgroundapi.ReviewsView]{have: true, val: playgroundapi.ReviewsView{HeadRead: true, Head: strings.Repeat("a", 40)}}}
	nm, cmd := m.pgReviewRequestFixes()
	m = pgDrain(t, nm.(Model), cmd)
	if m.workspace != wsGarden || m.garden.launch.stage != launchStageChoose || len(lb.starts) != 0 || lb.retries != 0 {
		t.Fatalf("fixes did not open the existing local form: ws=%v stage=%v starts=%d", m.workspace, m.garden.launch.stage, len(lb.starts))
	}
}
