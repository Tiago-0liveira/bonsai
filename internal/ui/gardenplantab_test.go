package ui

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/core/agentactivity"
	"github.com/Tiago-0liveira/bonsai/internal/core/agentterminal"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/plan"
	gardensync "github.com/Tiago-0liveira/bonsai/internal/core/garden/sync"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/overlay"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
)

var tabSizes = [][2]int{{80, 24}, {120, 35}, {160, 48}}

// tabNow is the pinned clock: elapsed time is measured from it at render, never stored.
var tabNow = time.Date(2026, 10, 20, 14, 30, 0, 0, time.UTC)

func ptKey(k string) tea.KeyMsg {
	switch k {
	case "ctrl+s":
		return tea.KeyMsg{Type: tea.KeyCtrlS}
	case "ctrl+y":
		return tea.KeyMsg{Type: tea.KeyCtrlY}
	case "ctrl+g":
		return tea.KeyMsg{Type: tea.KeyCtrlG}
	case "shift+tab":
		return tea.KeyMsg{Type: tea.KeyShiftTab}
	case "pgdown":
		return tea.KeyMsg{Type: tea.KeyPgDown}
	case " ":
		return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
	}
	return keyFor(k)
}

func ptPress(t *testing.T, m Model, keys ...string) Model {
	t.Helper()
	for _, k := range keys {
		var cmd tea.Cmd
		m, cmd = send(t, m, ptKey(k))
		m = plannerRun(t, m, cmd)
	}
	return m
}

// ptType types text into the focused box one key at a time.
func ptType(t *testing.T, m Model, s string) Model {
	t.Helper()
	for _, r := range s {
		m, _ = send(t, m, runeKey(string(r)))
	}
	return m
}

func pinClock(t *testing.T) {
	t.Helper()
	old := agentNow
	agentNow = func() time.Time { return tabNow }
	t.Cleanup(func() { agentNow = old })
}

// tabEnv is the planner environment with the Garden scripted by setup, read again and the Plan tab of Issue 1 open.
func tabEnv(t *testing.T, w, h int, setup func(*testing.T, plannerRig)) (Model, plannerRig) {
	return tabEnvGlyphs(t, w, h, false, setup)
}

func tabEnvGlyphs(t *testing.T, w, h int, ascii bool, setup func(*testing.T, plannerRig)) (Model, plannerRig) {
	t.Helper()
	pinClock(t)
	m, rig := plannerEnv(t, w, h)
	if ascii {
		m.work.AgentGlyphs = "ascii"
		for i := range rig.garden.WorkView.Snapshot.Items {
			rig.garden.WorkView.Snapshot.Items[i].Issue.Title = "ASCII title"
			rig.garden.WorkView.Snapshot.Items[i].Issue.Body = "ASCII body"
		}
	}
	if setup != nil {
		setup(t, rig)
	}
	m = plannerPress(t, m, "R")
	m.garden.board.selection.Focus(m.garden.board.collection, 1)
	m = plannerPress(t, m, "enter", "right", "right", "right")
	if m.garden.board.section != planTabSection || !m.gardenOverlayOpen() {
		t.Fatalf("the Plan tab is not open: section %d\n%s", m.garden.board.section, view(m))
	}
	return m, rig
}

func mutateRecord(m *Model, id garden.IssueID, fn func(*gardensync.Item)) {
	c := &m.garden.board.collection
	for col := range c.Columns {
		for i := range c.Columns[col] {
			if c.Columns[col][i].Item.Issue.ID == id {
				fn(&c.Columns[col][i].Item)
			}
		}
	}
}

func previewOf(t *testing.T, rig plannerRig) gardenapi.PlanRevisePreviewView {
	t.Helper()
	rv, err := rig.garden.PlanReview(context.Background(), 1)
	if err != nil || rv.Review.Current == nil {
		t.Fatalf("no current plan revision: %v", err)
	}
	return gardenapi.PlanRevisePreviewView{Revision: *rv.Review.Current, SettingsSource: "plan-fake-0",
		Settings: &gardenapi.PlanReviseSettings{AccountID: "c1", Model: "opus", Effort: "high"}, Base: plan.Base{Commit: planBase, Branch: "main"}}
}

func lastDecisionID(rig plannerRig) int64 {
	d := rig.garden.WorkView.Snapshot.Items[0].Verification.PlanDecisions
	return d[len(d)-1].CommentID
}

func localRun(live bool, state string, file plan.PlanFileState, act *agentactivity.Activity) map[uint64]gardenapi.IssueLocalWork {
	return map[uint64]gardenapi.IssueLocalWork{1: {Planner: &gardenapi.LocalRunSummary{AttemptID: "plan-fake-9", State: state, Live: live, Activity: act, PlanFile: file,
		Provider: "claude", AccountName: "work", Model: "opus", Effort: "high", RequestedAt: tabNow.Add(-(3*time.Minute + 12*time.Second)), Updated: tabNow.Add(-5 * time.Second)}}}
}

func working(detail string) *agentactivity.Activity {
	return &agentactivity.Activity{State: agentactivity.Working, Detail: detail, Since: tabNow.Add(-12 * time.Second), Source: agentactivity.SourceHook}
}

type tabCase struct {
	name   string
	mutate func(*Model)
	setup  func(*testing.T, plannerRig)
	kind   planTabKind
	want   []string
	not    []string
}

func tabCases() []tabCase {
	awaiting := func(t *testing.T, rig plannerRig) {
		scriptPlan(t, rig, longPlan("first"))
		scriptPlan(t, rig, longPlan("second"))
		rig.garden.PlanRevisePreview = previewOf(t, rig)
	}
	pending := func(att *plan.Attempt) func(*testing.T, plannerRig) {
		return func(t *testing.T, rig plannerRig) {
			rev := scriptPlan(t, rig, longPlan("first"))
			rig.garden.AddPlanDecision(1, rev, garden.DecisionChangesRequested, "Tighten the risks section.")
			pv := previewOf(t, rig)
			pv.Pending = &gardenapi.PendingRevise{DecisionCommentID: lastDecisionID(rig), Reason: "Tighten the risks section.", Attempt: att}
			rig.garden.PlanRevisePreview = pv
		}
	}
	return []tabCase{
		{name: "none", kind: tabNone, want: []string{"NO PLAN YET", "Request plan", "Reading this tab starts nothing"}},
		{name: "unavailable", kind: tabUnavailable, mutate: func(m *Model) {
			// A label and a claim are not proof: without the verified approval the gate reads as not approved.
			mutateRecord(m, 1, func(it *gardensync.Item) { it.Verification.Approvals, it.Verification.ValidApprovals = nil, nil })
		}, want: []string{"PLANNING UNAVAILABLE", "not approved"}, not: []string{"Approve plan"}},
		{name: "awaiting", kind: tabAwaiting, setup: awaiting,
			want: []string{"PLAN r2 / NEEDS YOUR APPROVAL", "Latest r2", "Current r2", "Approved none", "CHANGED SINCE r1: 1 added, 1 removed", "- 3. Step number 3", "+ 3. Step number 3", "RISKS", "TESTS", "Approve plan r2|Approve", "Request changes|Changes", "Open review|Review", "r1  awaiting decision, superseded"}},
		{name: "first revision", kind: tabAwaiting, setup: func(t *testing.T, rig plannerRig) {
			scriptPlan(t, rig, longPlan("first"))
			rig.garden.PlanRevisePreview = previewOf(t, rig)
		}, want: []string{"PLAN r1 / NEEDS YOUR APPROVAL", "FIRST REVISION", "Nothing to compare it with"}},
		{name: "live working", kind: tabLive, setup: func(t *testing.T, rig plannerRig) {
			rig.garden.LocalWorkIssues = localRun(true, "running", plan.PlanFileWriting, working("Read main.go"))
		}, want: []string{"PLANNER WORKING", "claude / work", "high", "Elapsed: 3m 12s", "Activity: Working · Read main.go · 12s ago", "Plan file: writing", "read-only", "Leave this overlay and planning keeps running.", "Open planner", "Stop attempt"},
			not: []string{"%", "step 1", "Step 1 of"}},
		{name: "live waiting", kind: tabLive, setup: func(t *testing.T, rig plannerRig) {
			rig.garden.LocalWorkIssues = localRun(true, "running", plan.PlanFileNone,
				&agentactivity.Activity{State: agentactivity.Waiting, Reason: agentactivity.ReasonPermission, Since: tabNow.Add(-40 * time.Second)})
		}, want: []string{"PLANNER NEEDS YOU", "Needs input (permission)", "waiting for you", "Plan file: none yet"}},
		{name: "live idle", kind: tabLive, setup: func(t *testing.T, rig plannerRig) {
			rig.garden.LocalWorkIssues = localRun(true, "running", plan.PlanFileNone, &agentactivity.Activity{State: agentactivity.Idle, Since: tabNow.Add(-time.Minute)})
		}, want: []string{"PLANNER IDLE (NOT FINISHED)", "not a finished plan", "Plan file: none yet"}, not: []string{"READY"}},
		{name: "live unreported", kind: tabLive, setup: func(t *testing.T, rig plannerRig) {
			rig.garden.LocalWorkIssues = localRun(true, "starting", plan.PlanFileNone, nil)
		}, want: []string{"PLANNER RUNNING", "Activity: not reported"}},
		{name: "publish", kind: tabPublish, setup: func(t *testing.T, rig plannerRig) {
			rig.garden.LocalWorkIssues = localRun(false, "ended", plan.PlanFileReady, nil)
		}, want: []string{"PLAN FILE READY TO PUBLISH", "claude / work ended", "not a plan revision until you publish", "Publish plan", "Open planner"}},
		{name: "pending", kind: tabPending, setup: pending(nil),
			want: []string{"CHANGES REQUESTED ON r1", "Feedback recorded on r1. No revision attempt is running.", "Tighten the risks section.", "Start revision attempt", "Revise with claude / work · high · opus"}},
		{name: "pending failed", kind: tabPending, setup: pending(&plan.Attempt{ID: "plan-fake-3", State: plan.StateFailed, Reason: "start failed"}),
			want: []string{"The revision attempt did not finish (failed (start failed))", "No revision attempt is running", "Start revision attempt"}},
		{name: "pending answered", kind: tabPending, setup: pending(&plan.Attempt{ID: "plan-fake-3", State: plan.StateEnded}),
			want: []string{"A revision attempt exists for it (ended)", "cannot be started again"}},
		{name: "approved", kind: tabApproved, setup: func(t *testing.T, rig plannerRig) {
			rev := scriptPlan(t, rig, longPlan("first"))
			rig.garden.AddPlanDecision(1, rev, garden.DecisionApproved, "")
			rig.garden.PlanRevisePreview = previewOf(t, rig)
		}, want: []string{"APPROVED r1 against s1", "Ready; coding starts only when you choose.", "Open review"}},
		{name: "stale approval", kind: tabAwaiting, setup: func(t *testing.T, rig plannerRig) {
			rev := scriptPlan(t, rig, longPlan("first"))
			rig.garden.AddPlanDecision(1, rev, garden.DecisionApproved, "")
			scriptPlan(t, rig, longPlan("second"))
			rig.garden.PlanRevisePreview = previewOf(t, rig)
		}, want: []string{"PLAN r2 / NEEDS YOUR APPROVAL", "earlier approval of r1 does not carry over", "Approved r1", "r1  approved"}},
	}
}

// assertRows checks every row of the drawn overlay panel: exact width, nothing wider than the screen.
func assertRows(t *testing.T, m Model, label string) {
	t.Helper()
	_, _, w, h := overlay.Center(m.width, m.height)
	rows, _ := m.issueOverlayRows(w, h)
	if len(rows) != h {
		t.Fatalf("%s: panel has %d rows, want %d", label, len(rows), h)
	}
	for i, r := range rows {
		if got := ansi.StringWidth(r); got != w {
			t.Fatalf("%s: panel row %d is %d cells, want %d: %q", label, i, got, w, ansi.Strip(r))
		}
	}
	assertOverlayFrame(t, m.View(), m.width, m.height)
	for i, r := range m.gardenDocument(max(1, m.docWidth())) {
		if got := lipgloss.Width(r); got > m.docWidth() {
			t.Fatalf("%s: document row %d is %d cells, body is %d", label, i, got, m.docWidth())
		}
	}
}

func (m Model) docWidth() int { w, _ := m.gardenOverlayBody(); return w }

func TestPlanTabRendersEveryStateAtEverySizeWithoutOverflow(t *testing.T) {
	for _, tc := range tabCases() {
		for _, sz := range tabSizes {
			for _, ascii := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%dx%d/ascii=%v", tc.name, sz[0], sz[1], ascii), func(t *testing.T) {
					m, rig := tabEnvGlyphs(t, sz[0], sz[1], ascii, tc.setup)
					if tc.mutate != nil {
						tc.mutate(&m)
					}
					data, ok := m.planTabData()
					if !ok || data.kind != tc.kind {
						t.Fatalf("state %d, want %d\n%s", data.kind, tc.kind, view(m))
					}
					assertRows(t, m, tc.name)
					// The whole document is reachable by scrolling.
					doc := ansi.Strip(strings.Join(m.gardenDocument(m.docWidth()), "\n"))
					for _, want := range tc.want {
						// Pinned actions are on the panel (a short label when the bar is narrow), the rest in the document.
						found := false
						for _, alt := range strings.Split(want, "|") {
							if ascii {
								alt = asciiRunes.Replace(alt)
							}
							found = found || strings.Contains(flat(doc), flat(alt)) || strings.Contains(flat(view(m)), flat(alt))
						}
						if !found {
							t.Fatalf("missing %q in\n%s\n--- panel\n%s", want, doc, panelRows(m))
						}
					}
					mustNotContain(t, tc.name, doc, tc.not...)
					if ascii {
						for _, r := range ansi.Strip(m.View()) {
							if r > 127 {
								t.Fatalf("non-ASCII %q in ascii mode", r)
							}
						}
					}
					if rig.garden.Writes() != 0 || rig.garden.Count("RevisePlan") != 0 || rig.garden.Count("StartPlanRevise") != 0 || rig.garden.Count("StartPlanner") != 0 {
						t.Fatal("showing the tab wrote or started something")
					}
					// Scrolling through the whole body keeps every row inside the panel.
					for range 6 {
						m = ptPress(t, m, "pgdown")
						assertRows(t, m, tc.name+" scrolled")
					}
				})
			}
		}
	}
}

// tabText is everything the Plan tab says: the whole document, however far it scrolls, and the panel.
func tabText(m Model) string {
	return flat(ansi.Strip(strings.Join(m.gardenDocument(m.docWidth()), "\n"))) + " " + flat(view(m))
}

func controlIDs(m Model) []string {
	var out []string
	for _, c := range m.issueControls() {
		out = append(out, c.id)
	}
	return out
}

func awaitingSetup(t *testing.T, rig plannerRig) {
	scriptPlan(t, rig, longPlan("first"))
	rig.garden.PlanRevisePreview = previewOf(t, rig)
}

func (r plannerRig) quiet(t *testing.T) {
	t.Helper()
	for _, name := range []string{"RevisePlan", "StartPlanRevise", "StartPlanner", "DecidePlan", "CancelPlanner", "PublishPlan"} {
		if n := r.garden.Count(name); n != 0 {
			t.Fatalf("%s was called %d times", name, n)
		}
	}
}

func TestPlanTabActionsAreContextualAndResolveThroughOneRoute(t *testing.T) {
	m, rig := tabEnv(t, 120, 35, awaitingSetup)
	if got, want := controlIDs(m), []string{"close", "approve", "changes", "review"}; !equalStrings(got, want) {
		t.Fatalf("controls %v, want %v", got, want)
	}
	// The other tabs keep the static plan and review actions.
	other := ptPress(t, m, "left")
	if got, want := controlIDs(other), []string{"close", "plan", "review"}; !equalStrings(got, want) {
		t.Fatalf("Questions controls %v, want %v", got, want)
	}
	var order []string
	for range 4 {
		m = ptPress(t, m, "tab")
		order = append(order, m.garden.board.overlayFocus)
	}
	if want := []string{"close", "approve", "changes", "review"}; !equalStrings(order, want) {
		t.Fatalf("tab order %v, want %v", order, want)
	}
	// Enter on the focused action, its shortcut and a click on it draw the same screen.
	for _, tc := range []struct{ id, key string }{{"changes", "c"}, {"review", "r"}, {"approve", "a"}} {
		base, _ := tabEnv(t, 120, 35, awaitingSetup)
		viaKey := ptPress(t, base, tc.key)
		var steps []string
		for i := 0; i <= slicesIndex(controlIDs(base), tc.id); i++ {
			steps = append(steps, "tab")
		}
		viaTab := ptPress(t, base, append(steps, "enter")...)
		x0, y0, w, h := overlay.Center(base.width, base.height)
		_, hits := base.issueOverlayRows(w, h)
		var viaClick Model
		for _, hit := range hits {
			if hit.action == tc.id {
				var cmd tea.Cmd
				viaClick, cmd = send(t, base, click(x0+hit.x0, y0+hit.y))
				viaClick = plannerRun(t, viaClick, cmd)
			}
		}
		if view(viaKey) != view(viaTab) || view(viaKey) != view(viaClick) {
			t.Fatalf("%s: key, Tab+Enter and click drew different screens", tc.id)
		}
	}
	rig.quiet(t)
}

func slicesIndex(list []string, v string) int {
	for i, s := range list {
		if s == v {
			return i
		}
	}
	return -1
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestPlanTabDisabledActionsStayVisibleWithTheirReason(t *testing.T) {
	m, rig := tabEnv(t, 120, 35, awaitingSetup)
	m.garden.status.Access = gardenapi.AccessRead
	var approve issueControl
	for _, c := range m.issueControls() {
		if c.id == "approve" {
			approve = c
		}
	}
	if approve.enabled || !strings.Contains(approve.reason, "Write access") {
		t.Fatalf("approve = %+v", approve)
	}
	mustContain(t, "bar", flat(view(m)), "a Approve plan r1")
	for range 5 {
		m = ptPress(t, m, "tab")
		if id := m.garden.board.overlayFocus; id == "approve" || id == "changes" {
			t.Fatalf("Tab focused the disabled %s", id)
		}
	}
	reviews := rig.garden.Count("PlanReview")
	m = ptPress(t, m, "a")
	mustContain(t, "reason", flat(view(m)), "Write access")
	if rig.garden.Count("PlanReview") != reviews || m.garden.planrev.stage != planNone {
		t.Fatal("a disabled action read or opened something")
	}
	rig.quiet(t)
}

func TestPlanTabApproveConfirmsTheExactPairInsideTheOverlay(t *testing.T) {
	for _, sz := range tabSizes {
		m, rig := tabEnv(t, sz[0], sz[1], awaitingSetup)
		reviews := rig.garden.Count("PlanReview")
		m = ptPress(t, m, "a")
		if rig.garden.Count("PlanReview") != reviews+1 {
			t.Fatal("approving did not read the review again first")
		}
		if !m.planDecideInOverlay() || !m.gardenOverlayOpen() {
			t.Fatalf("the confirmation is not in the overlay: stage %d\n%s", m.garden.planrev.stage, view(m))
		}
		out := tabText(m)
		mustContain(t, "confirm", out, "Issue #101", "APPROVE PLAN r1?", "Plan revision:", "Source spec fingerprint:", "Specification head:", "Actor:", "No agent, worktree or PR starts")
		assertRows(t, m, "confirm")
		// Cancel is the safe default; nothing is written.
		m = ptPress(t, m, "enter")
		if m.garden.planrev.stage != planNone || rig.garden.Count("DecidePlan") != 0 || m.garden.board.section != planTabSection {
			t.Fatalf("Cancel decided or left the tab: stage %d", m.garden.planrev.stage)
		}
		// Approve names the plan and decides once, however many times Enter repeats.
		m = ptPress(t, m, "a", "tab")
		var cmd tea.Cmd
		m, cmd = send(t, m, ptKey("enter"))
		for range 3 {
			var again tea.Cmd
			m, again = send(t, m, ptKey("enter"))
			if again != nil {
				t.Fatal("a repeated Enter started another write")
			}
		}
		m = plannerRun(t, m, cmd)
		if rig.garden.Count("DecidePlan") != 1 || len(rig.garden.PlanDecided) != 1 || rig.garden.PlanDecided[0].Decision != garden.DecisionApproved {
			t.Fatalf("decisions: %d %+v", rig.garden.Count("DecidePlan"), rig.garden.PlanDecided)
		}
		mustContain(t, "approved", flat(view(m)), "Recorded on plan r1: approved. No run was started.")
		data, _ := m.planTabData()
		if data.kind != tabApproved || rig.garden.Count("StartPlanner") != 0 || rig.garden.Count("RevisePlan") != 0 {
			t.Fatalf("after approval: kind %d", data.kind)
		}
	}
}

func TestPlanTabApproveRefusesWhenTheHeadsMoved(t *testing.T) {
	m, rig := tabEnv(t, 120, 35, awaitingSetup)
	// Another client publishes r2 after the tab was drawn.
	scriptPlan(t, rig, longPlan("second"))
	m = ptPress(t, m, "a")
	mustContain(t, "refusal", tabText(m), "The plan under review changed")
	if rig.garden.Count("DecidePlan") != 0 || m.garden.planrev.stage != planNone {
		t.Fatal("a plan nobody looked at was offered for approval")
	}
	mustContain(t, "reloaded", tabText(m), "PLAN r2 / NEEDS YOUR APPROVAL")

	// Heads that move after the confirmation opened are refused by the write; nothing is recorded.
	m, rig = tabEnv(t, 120, 35, awaitingSetup)
	m = ptPress(t, m, "a")
	scriptPlan(t, rig, longPlan("second"))
	m = ptPress(t, m, "tab", "enter")
	if got := len(rig.garden.WorkView.Snapshot.Items[0].Verification.PlanDecisions); m.garden.planrev.stage != planNone || got != 0 {
		t.Fatalf("decided against moved heads: stage %d, %d decisions recorded", m.garden.planrev.stage, got)
	}
	mustContain(t, "write refusal", tabText(m), "Approval heads or actor changed")
	mustContain(t, "reloaded", tabText(m), "PLAN r2 / NEEDS YOUR APPROVAL")
}

func TestPlanTabFeedbackFormKeepsTextRefusesEmptyAndDropsItWhenTheRevisionChanges(t *testing.T) {
	for _, sz := range tabSizes {
		m, rig := tabEnv(t, sz[0], sz[1], awaitingSetup)
		m = ptPress(t, m, "c")
		f := m.planForm()
		if f == nil {
			t.Fatalf("the form did not open\n%s", view(m))
		}
		out := tabText(m)
		mustContain(t, "form", out, "Request changes to plan r1", "Feedback stays attached to this exact plan revision.", "Revise with claude / work · high · opus",
			"read-only", "The new attempt receives this feedback and plan r1.", "Starts one read-only revision attempt. Nothing starts coding.")
		assertRows(t, m, "form")
		mustContain(t, "hints", out, "ctrl+s send feedback & revise plan")

		// An empty reason is refused before anything is sent.
		m = ptPress(t, m, "ctrl+s", "ctrl+s")
		mustContain(t, "empty", flat(view(m)), "Feedback needs a reason")
		if rig.garden.Count("RevisePlan") != 0 || m.planForm() == nil {
			t.Fatal("an empty reason was sent or closed the form")
		}

		m = ptType(t, m, "Tighten the risks.")
		m = ptPress(t, m, "esc")
		if m.planForm() != nil || m.garden.board.section != planTabSection {
			t.Fatal("Esc did not leave the form for the Plan tab")
		}
		// Back, Esc and a tab switch keep what was typed for the same revision.
		m = ptPress(t, m, "left", "right", "c")
		if got := m.planForm().ta.Value(); got != "Tighten the risks." {
			t.Fatalf("typed text after a tab switch: %q", got)
		}
		m = ptPress(t, m, "esc")

		// The revision under review changes: the text is dropped, and the user is told.
		scriptPlan(t, rig, longPlan("second"))
		rig.garden.PlanRevisePreview = previewOf(t, rig)
		m = ptPress(t, m, "R")
		mustContain(t, "dropped", tabText(m), "Your unsent feedback was for plan r1, which is no longer the revision under review. It was dropped.")
		m = ptPress(t, m, "c")
		if f := m.planForm(); f == nil || f.ta.Value() != "" || f.ordinal != 2 {
			t.Fatalf("the form after the revision changed: %+v", f)
		}
		rig.quiet(t)
	}
}

func revisedAttempt(state plan.State) *plan.Attempt {
	return &plan.Attempt{ID: "plan-fake-5", Issue: 1, State: state, Provider: "claude", AccountID: "c1", AccountName: "work", Model: "opus", Effort: "high",
		SessionID: "sess-fake-5", RequestedAt: tabNow.Add(-20 * time.Second), UpdatedAt: tabNow, Revises: &plan.RevisionRef{RevisionHash: "x", Ordinal: 1, DecisionCommentID: 9001}}
}

func doneDecision() gardenapi.PlanDecisionView {
	return gardenapi.PlanDecisionView{State: gardenapi.PlanWriteDone, CommentID: 9001, Ordinal: 1, Decision: garden.DecisionChangesRequested}
}

func TestPlanTabFeedbackSendsOnceAndShowsTheLiveRevisionAttempt(t *testing.T) {
	for _, sz := range tabSizes {
		m, rig := tabEnv(t, sz[0], sz[1], awaitingSetup)
		m = ptPress(t, m, "c")
		m = ptType(t, m, "Tighten the risks.")
		rig.garden.PlanReviseResult = gardenapi.PlanReviseView{Decision: doneDecision(), Attempt: revisedAttempt(plan.StateRunning)}
		var cmd tea.Cmd
		m, cmd = send(t, m, ptKey("ctrl+s"))
		if cmd == nil || !m.garden.busy.writes() {
			t.Fatal("the submit did not start one write")
		}
		for _, k := range []string{"ctrl+s", "enter", "ctrl+s", "enter"} {
			var again tea.Cmd
			m, again = send(t, m, ptKey(k))
			if again != nil {
				t.Fatalf("%s while sending started another write", k)
			}
		}
		// The backend now holds the decision and the running attempt; the reads after the write find them.
		pv := previewOf(t, rig)
		pv.Pending = &gardenapi.PendingRevise{DecisionCommentID: 9001, Reason: "Tighten the risks.", Attempt: revisedAttempt(plan.StateRunning)}
		pv.Unavailable = "A planner attempt is running."
		rig.garden.PlanRevisePreview = pv
		m = plannerRun(t, m, cmd)
		if rig.garden.Count("RevisePlan") != 1 || len(rig.garden.PlanRevised) != 1 {
			t.Fatalf("RevisePlan ran %d times", rig.garden.Count("RevisePlan"))
		}
		req := rig.garden.PlanRevised[0]
		if req.IssueID != 1 || req.Reason != "Tighten the risks." || !req.AcceptReadOnly || req.IncludeDirty || req.RevisionHash != pv.Revision.Hash ||
			req.Settings != (gardenapi.PlanReviseSettings{AccountID: "c1", Model: "opus", Effort: "high"}) || req.Base.Commit != planBase ||
			req.SpecHash == "" || req.Fingerprint == "" || req.ActorID != "1001" || req.Cols <= 0 || req.Rows <= 0 {
			t.Fatalf("request: %+v", req)
		}
		mustContain(t, "result", tabText(m), "Feedback recorded on r1. A read-only revision attempt is running.")
		data, _ := m.planTabData()
		if data.kind != tabLive || m.planForm() != nil || rig.garden.Count("StartPlanRevise") != 0 || rig.garden.Count("StartPlanner") != 0 {
			t.Fatalf("after the send: kind %d", data.kind)
		}
		m = ptPress(t, m, "esc")
		m = plannerPress(t, m, "enter", "right", "right", "right")
		mustContain(t, "live", flat(view(m)), "Revises plan r1", "Elapsed: 20s", "Leave this overlay and planning keeps running.")
		assertRows(t, m, "live revision")
	}
}

func TestPlanTabPartialFailureShowsPendingAndRetriesOnlyOnPurpose(t *testing.T) {
	m, rig := tabEnv(t, 120, 35, awaitingSetup)
	m = ptPress(t, m, "c")
	m = ptType(t, m, "Tighten the risks.")
	rig.garden.PlanReviseResult = gardenapi.PlanReviseView{Decision: doneDecision(), StartErr: &gardenapi.Error{Code: gardenapi.CodeFailed, Message: "The account could not start.", Next: "Check the account."}}
	pv := previewOf(t, rig)
	pv.Pending = &gardenapi.PendingRevise{DecisionCommentID: 9001, Reason: "Tighten the risks."}
	rig.garden.PlanRevisePreview = pv
	m = ptPress(t, m, "ctrl+s")
	out := tabText(m)
	mustContain(t, "pending", out, "Feedback recorded on r1, but no revision attempt started", "CHANGES REQUESTED ON r1", "No revision attempt is running.",
		"The start failed: The account could not start. Check the account.", "Tighten the risks.", "Start revision attempt")
	if rig.garden.Count("RevisePlan") != 1 || rig.garden.Count("StartPlanRevise") != 0 {
		t.Fatalf("calls: %v", rig.garden.Calls())
	}
	// Nothing restarts on reading: reopening, rereading and tab switches never call a write.
	m = ptPress(t, m, "R", "esc", "enter", "right", "right", "right", "R", "left", "right")
	if rig.garden.Count("RevisePlan") != 1 || rig.garden.Count("StartPlanRevise") != 0 {
		t.Fatalf("reading started something: %v", rig.garden.Calls())
	}
	// The retry is its own confirmation and its own write.
	m = ptPress(t, m, "b")
	f := m.planForm()
	if f == nil || !f.retry {
		t.Fatalf("no retry confirmation\n%s", view(m))
	}
	mustContain(t, "retry", tabText(m), "START A REVISION ATTEMPT FOR r1", "The feedback is already recorded on the Issue.", "Tighten the risks.", "Revise with claude / work")
	if rig.garden.Count("StartPlanRevise") != 0 {
		t.Fatal("opening the confirmation started the attempt")
	}
	m = ptPress(t, m, "esc")
	if rig.garden.Count("StartPlanRevise") != 0 || m.planForm() != nil {
		t.Fatal("leaving the confirmation started the attempt")
	}
	rig.garden.PlanReviseResult = gardenapi.PlanReviseView{Decision: doneDecision(), Attempt: revisedAttempt(plan.StateRunning)}
	m = ptPress(t, m, "b", "ctrl+s")
	if rig.garden.Count("StartPlanRevise") != 1 || len(rig.garden.PlanReviseStarted) != 1 {
		t.Fatalf("retry calls: %v", rig.garden.Calls())
	}
	req := rig.garden.PlanReviseStarted[0]
	if req.DecisionCommentID != 9001 || req.RevisionHash != pv.Revision.Hash || !req.AcceptReadOnly || req.Settings.AccountID != "c1" || req.IssueID != 1 {
		t.Fatalf("retry request: %+v", req)
	}
	if rig.garden.Count("RevisePlan") != 1 {
		t.Fatal("the retry recorded the decision again")
	}
}

func TestPlanTabFailedAttemptAllowsRetryButARunningOneDoesNot(t *testing.T) {
	setup := func(att *plan.Attempt) func(*testing.T, plannerRig) {
		return func(t *testing.T, rig plannerRig) {
			rev := scriptPlan(t, rig, longPlan("first"))
			rig.garden.AddPlanDecision(1, rev, garden.DecisionChangesRequested, "Tighten the risks section.")
			pv := previewOf(t, rig)
			pv.Pending = &gardenapi.PendingRevise{DecisionCommentID: lastDecisionID(rig), Reason: "Tighten the risks section.", Attempt: att}
			rig.garden.PlanRevisePreview = pv
		}
	}
	m, _ := tabEnv(t, 120, 35, setup(revisedAttempt(plan.StateFailed)))
	if !slicesContains(controlIDs(m), "retry") || !controlEnabled(m, "retry") {
		t.Fatalf("a failed attempt must allow a retry: %v", controlIDs(m))
	}
	m, _ = tabEnv(t, 120, 35, setup(revisedAttempt(plan.StateCancelled)))
	if !controlEnabled(m, "retry") {
		t.Fatal("a cancelled attempt must allow a retry")
	}
	for _, st := range []plan.State{plan.StateEnded, plan.StateRunning, plan.StateStarting} {
		m, _ = tabEnv(t, 120, 35, setup(revisedAttempt(st)))
		if controlEnabled(m, "retry") {
			t.Fatalf("a %s attempt that already answers the feedback was offered again", st)
		}
	}
	m, _ = tabEnv(t, 120, 35, setup(revisedAttempt(plan.StateEnded)))
	for _, c := range m.issueControls() {
		if c.id == "retry" && c.reason != "A revision attempt already answers this feedback." {
			t.Fatalf("retry reason %q", c.reason)
		}
	}
}

func slicesContains(list []string, v string) bool { return slicesIndex(list, v) >= 0 }

func controlEnabled(m Model, id string) bool {
	for _, c := range m.issueControls() {
		if c.id == id {
			return c.enabled
		}
	}
	return false
}

func TestPlanTabUnknownDecisionStartsNothingAndKeepsTheText(t *testing.T) {
	m, rig := tabEnv(t, 120, 35, awaitingSetup)
	m = ptPress(t, m, "c")
	m = ptType(t, m, "Tighten the risks.")
	rig.garden.PlanReviseResult = gardenapi.PlanReviseView{Decision: gardenapi.PlanDecisionView{State: gardenapi.PlanWriteUnknown,
		Err: &gardenapi.Error{Code: gardenapi.CodeFailed, Message: "GitHub did not confirm the comment.", Next: "Reopen to reconcile."}}}
	previews := rig.garden.Count("PreviewPlanRevise")
	m = ptPress(t, m, "ctrl+s")
	mustContain(t, "unknown", tabText(m), "GitHub did not confirm the comment. Reopen to reconcile.")
	if rig.garden.Count("RevisePlan") != 1 || rig.garden.Count("StartPlanRevise") != 0 || rig.garden.Count("PreviewPlanRevise") <= previews {
		t.Fatalf("calls: %v", rig.garden.Calls())
	}
	m = ptPress(t, m, "c")
	if f := m.planForm(); f == nil || f.ta.Value() != "Tighten the risks." {
		t.Fatalf("the text was lost after an unknown outcome: %+v", f)
	}
}

func TestPlanTabExistingAttemptIsShownNotStartedAgain(t *testing.T) {
	m, rig := tabEnv(t, 120, 35, awaitingSetup)
	m = ptPress(t, m, "c")
	m = ptType(t, m, "Tighten the risks.")
	d := doneDecision()
	d.Replayed = true
	rig.garden.PlanReviseResult = gardenapi.PlanReviseView{Decision: d, Attempt: revisedAttempt(plan.StateRunning), Existing: true}
	m = ptPress(t, m, "ctrl+s")
	mustContain(t, "existing", tabText(m), "That feedback on r1 was already recorded; showing the revision attempt it started.")
	if rig.garden.Count("RevisePlan") != 1 {
		t.Fatalf("calls: %v", rig.garden.Calls())
	}
}

func TestPlanTabRefusalBeforeAnyWriteKeepsTheFormAndText(t *testing.T) {
	m, rig := tabEnv(t, 120, 35, awaitingSetup)
	m = ptPress(t, m, "c")
	m = ptType(t, m, "Tighten the risks.")
	rig.garden.RevisePlanErr = &gardenapi.Error{Code: gardenapi.CodeSpecChanged, Message: "The specification changed.", Next: "Read the plan again."}
	m = ptPress(t, m, "ctrl+s")
	if f := m.planForm(); f == nil || f.ta.Value() != "Tighten the risks." {
		t.Fatal("a refusal closed the form or lost the text")
	}
	mustContain(t, "refusal", tabText(m), "The specification changed. Read the plan again.")
	if len(rig.garden.PlanRevised) != 0 {
		t.Fatal("a refused call was recorded as sent")
	}
	mustContain(t, "not confirmed", tabText(m), "Not confirmed as recorded")
}

func TestPlanTabAnyErrorRereadsTheTabAndLocalWork(t *testing.T) {
	m, rig := tabEnv(t, 120, 35, awaitingSetup)
	m = ptPress(t, m, "c")
	m = ptType(t, m, "Tighten the risks.")
	previews, locals, works := rig.garden.Count("PreviewPlanRevise"), rig.garden.Count("LocalWork"), rig.garden.Count("Work")
	rig.garden.RevisePlanErr = fmt.Errorf("connection reset")
	m = ptPress(t, m, "ctrl+s")
	if rig.garden.Count("PreviewPlanRevise") <= previews || rig.garden.Count("LocalWork") <= locals || rig.garden.Count("Work") <= works {
		t.Fatalf("an error did not reread: %v", rig.garden.Calls())
	}
	if f := m.planForm(); f == nil || f.ta.Value() != "Tighten the risks." {
		t.Fatal("the text was lost")
	}
	// A repeat finds the earlier record: Existing, no second attempt of its own.
	rig.garden.RevisePlanErr = nil
	d := doneDecision()
	d.Replayed = true
	rig.garden.PlanReviseResult = gardenapi.PlanReviseView{Decision: d, Attempt: revisedAttempt(plan.StateRunning), Existing: true}
	m = ptPress(t, m, "ctrl+s")
	mustContain(t, "existing", tabText(m), "already recorded")
	// An unrelated live attempt is refused with a coded error: the form stays and says why.
	m, rig = tabEnv(t, 120, 35, awaitingSetup)
	m = ptType(t, ptPress(t, m, "c"), "Tighten the risks.")
	rig.garden.RevisePlanErr = &gardenapi.Error{Code: gardenapi.CodeAttemptActive, Message: "A planner attempt is still running for this Issue.", Next: "Stop it first."}
	m = ptPress(t, m, "ctrl+s")
	mustContain(t, "active", tabText(m), "A planner attempt is still running for this Issue. Stop it first.")
	if m.planForm() == nil || m.garden.busy.writes() {
		t.Fatal("the form was lost or the write stayed busy")
	}
}

func TestPlanTabDirtyCheckoutNeedsExplicitConsentAndAccountFallbackOpensTheChoice(t *testing.T) {
	m, rig := tabEnv(t, 120, 35, func(t *testing.T, rig plannerRig) {
		scriptPlan(t, rig, longPlan("first"))
		pv := previewOf(t, rig)
		pv.Base = plan.Base{Commit: planBase, Branch: "main", Dirty: true, DirtyFiles: 2, DirtyDigest: "d1"}
		rig.garden.PlanRevisePreview = pv
	})
	m = ptPress(t, m, "c")
	m = ptType(t, m, "Tighten the risks.")
	mustContain(t, "dirty", flat(view(m)), "[ ] Let the planner read uncommitted changes")
	m = ptPress(t, m, "ctrl+s")
	mustContain(t, "refused", flat(view(m)), "The checkout has uncommitted changes")
	if rig.garden.Count("RevisePlan") != 0 {
		t.Fatal("the dirty checkout was read without consent")
	}
	m = ptPress(t, m, "ctrl+y")
	mustContain(t, "consent", flat(view(m)), "[x] Let the planner read uncommitted changes")
	m = ptPress(t, m, "ctrl+s")
	if len(rig.garden.PlanRevised) != 1 || !rig.garden.PlanRevised[0].IncludeDirty || !rig.garden.PlanRevised[0].Base.Dirty {
		t.Fatalf("consent not carried: %+v", rig.garden.PlanRevised)
	}

	// Without retained settings the existing planner account choice is the way to pick one.
	m, rig = tabEnv(t, 120, 35, func(t *testing.T, rig plannerRig) {
		scriptPlan(t, rig, longPlan("first"))
		pv := previewOf(t, rig)
		pv.Settings, pv.SettingsUnavailable = nil, "The account that wrote this plan no longer exists."
		rig.garden.PlanRevisePreview = pv
	})
	m = ptPress(t, m, "c")
	mustContain(t, "unavailable", tabText(m), "no account chosen", "The account that wrote this plan no longer exists.")
	m = ptType(t, m, "Tighten the risks.")
	m = ptPress(t, m, "ctrl+s")
	mustContain(t, "choose", tabText(m), "Choose an account first")
	if rig.garden.Count("RevisePlan") != 0 {
		t.Fatal("feedback was sent without an account")
	}
	m = ptPress(t, m, "ctrl+g")
	if m.garden.planner.stage != plannerChoose || !m.garden.planner.revise {
		t.Fatalf("the account choice did not open: stage %d", m.garden.planner.stage)
	}
	mustContain(t, "choice", flat(view(m)), "Accept read-only and revise plan")
	// Backing out returns to the form with the text; nothing was sent.
	m = ptPress(t, m, "esc")
	if m.garden.planner.stage != plannerNone || m.planForm() == nil || m.planForm().ta.Value() != "Tighten the risks." || rig.garden.Count("RevisePlan") != 0 {
		t.Fatalf("Back did not return to the form: stage %d", m.garden.planner.stage)
	}
	// Choosing sends the feedback with that account, once.
	m = ptPress(t, m, "ctrl+g", "ctrl+s")
	if len(rig.garden.PlanRevised) != 1 || rig.garden.PlanRevised[0].Settings.AccountID != "c1" || !rig.garden.PlanRevised[0].AcceptReadOnly || rig.garden.Count("StartPlanner") != 0 {
		t.Fatalf("revise with the chosen account: %+v calls %v", rig.garden.PlanRevised, rig.garden.Calls())
	}
}

func TestPlanTabLateAndForeignRepliesAreIgnored(t *testing.T) {
	m, rig := tabEnv(t, 120, 35, awaitingSetup)
	good := m.planTabPreview()
	if good == nil {
		t.Fatal("no preview loaded")
	}
	seq := m.garden.plantab.seq
	stale := *good
	stale.Pending = &gardenapi.PendingRevise{DecisionCommentID: 1, Reason: "stale"}
	stale.Stamp.Generation = good.Stamp.Generation - 1 // older than what the tab already took in
	m, _ = send(t, m, gardenPlanRevisePreviewMsg{seq: seq, issue: 1, view: stale})
	other := *good
	other.IssueID, other.Pending = 2, &gardenapi.PendingRevise{DecisionCommentID: 2}
	other.Stamp.Generation += 50
	m, _ = send(t, m, gardenPlanRevisePreviewMsg{seq: seq, issue: 2, view: other})
	superseded := *good
	superseded.Pending, superseded.Stamp.Generation = &gardenapi.PendingRevise{DecisionCommentID: 3}, good.Stamp.Generation+60
	m, _ = send(t, m, gardenPlanRevisePreviewMsg{seq: seq - 1, issue: 1, view: superseded})
	wrongClone := *good
	wrongClone.Pending, wrongClone.Stamp = &gardenapi.PendingRevise{DecisionCommentID: 4}, gardenapi.Stamp{LocalRepoID: "another-clone", GardenID: good.Stamp.GardenID, Generation: good.Stamp.Generation + 70}
	m, _ = send(t, m, gardenPlanRevisePreviewMsg{seq: seq, issue: 1, view: wrongClone})
	if got := m.planTabPreview(); got == nil || got.Pending != nil || got.Stamp != good.Stamp {
		t.Fatalf("a late or foreign preview replaced the newest one: %+v", got)
	}
	// A failed read keeps the last good preview.
	m, _ = send(t, m, gardenPlanRevisePreviewMsg{seq: seq, issue: 1, err: fmt.Errorf("offline")})
	if m.planTabPreview() == nil || m.planTabPreview().Stamp != good.Stamp {
		t.Fatal("a failed read dropped the last good preview")
	}

	// A reply to a write that is no longer the one in flight changes nothing.
	m = ptPress(t, m, "c")
	m = ptType(t, m, "Tighten the risks.")
	var cmd tea.Cmd
	m, cmd = send(t, m, ptKey("ctrl+s"))
	busy := m.garden.busy
	late := gardenPlanReviseMsg{seq: m.garden.seq - 1, issue: 1, view: gardenapi.PlanReviseView{IssueID: 1, Decision: doneDecision(), Attempt: revisedAttempt(plan.StateRunning)}}
	m, _ = send(t, m, late)
	if m.garden.busy != busy || m.planForm() == nil {
		t.Fatal("a superseded write reply settled the request in flight")
	}
	// A reply for another Issue is not shown as this Issue's attempt; the state is read again instead.
	foreign := gardenPlanReviseMsg{seq: m.garden.seq, issue: 2, view: gardenapi.PlanReviseView{IssueID: 2, Stamp: good.Stamp, Decision: doneDecision(), Attempt: revisedAttempt(plan.StateRunning)}}
	m, _ = send(t, m, foreign)
	if data, _ := m.planTabData(); data.kind == tabLive {
		t.Fatal("another Issue's attempt was shown on this one")
	}
	mustContain(t, "foreign", tabText(m), "may have been recorded")
	_ = cmd
	_ = rig
}

func TestPlanTabReopeningAndRereadingNeverCallAWrite(t *testing.T) {
	m, rig := tabEnv(t, 80, 24, func(t *testing.T, rig plannerRig) {
		rev := scriptPlan(t, rig, longPlan("first"))
		rig.garden.AddPlanDecision(1, rev, garden.DecisionChangesRequested, "Tighten the risks section.")
		pv := previewOf(t, rig)
		pv.Pending = &gardenapi.PendingRevise{DecisionCommentID: lastDecisionID(rig), Reason: "Tighten the risks section."}
		rig.garden.PlanRevisePreview = pv
	})
	for range 3 {
		m = ptPress(t, m, "esc", "enter", "right", "right", "right", "R", "down", "pgdown", "home", "left", "right", "tab", "shift+tab")
		m, _ = send(t, m, gardenLocalTickMsg{gen: m.garden.local.tickGen})
	}
	for _, name := range []string{"RevisePlan", "StartPlanRevise", "StartPlanner", "DecidePlan", "CancelPlanner", "PublishPlan"} {
		if rig.garden.Count(name) != 0 {
			t.Fatalf("%s was called by reading: %v", name, rig.garden.Calls())
		}
	}
	if rig.garden.Writes() != 0 || len(rig.garden.PlanRevised)+len(rig.garden.PlanReviseStarted) != 0 {
		t.Fatal("reading wrote something")
	}
	if rig.garden.Count("PreviewPlanRevise") < 3 {
		t.Fatalf("the tab never read its preview: %v", rig.garden.Calls())
	}
}

func TestPlanTabElapsedAndActivityComeOnlyFromReportedData(t *testing.T) {
	m, _ := tabEnv(t, 120, 35, func(t *testing.T, rig plannerRig) {
		rig.garden.LocalWorkIssues = localRun(true, "running", plan.PlanFileWriting, working("Read main.go"))
	})
	mustContain(t, "elapsed", flat(view(m)), "Elapsed: 3m 12s")
	// Time passes at render only: no message arrives and the stored data is the same.
	later := tabNow.Add(time.Minute)
	agentNow = func() time.Time { return later }
	mustContain(t, "later", flat(view(m)), "Elapsed: 4m 12s", "Activity: Working · Read main.go · 1m 12s ago")

	m, _ = tabEnv(t, 120, 35, func(t *testing.T, rig plannerRig) {
		run := localRun(true, "running", plan.PlanFileNone, nil)
		s := run[1].Planner
		s.RequestedAt, s.Provider, s.AccountName, s.Model, s.Effort = time.Time{}, "", "", "", ""
		rig.garden.LocalWorkIssues = run
	})
	out := flat(ansi.Strip(strings.Join(m.gardenDocument(m.docWidth()), "\n")))
	mustContain(t, "unreported", out, "Elapsed: not reported", "Activity: not reported", "PLANNER RUNNING", "The planner")
	mustNotContain(t, "unreported", out, "working", "Working", "ready")
}

func TestPlanTabStopAttemptAsksFirst(t *testing.T) {
	m, rig := tabEnv(t, 120, 35, func(t *testing.T, rig plannerRig) {
		a := revisedAttempt(plan.StateRunning)
		a.ID, a.Revises = "plan-fake-9", nil
		rig.garden.SetAttempt(*a)
		rig.garden.LocalWorkIssues = localRun(true, "running", plan.PlanFileWriting, working("Read main.go"))
	})
	m = ptPress(t, m, "c")
	mustContain(t, "confirm", flat(view(m)), "STOP THE PLANNER ATTEMPT?", "y stops it. Any other key keeps it running.")
	assertRows(t, m, "stop")
	if rig.garden.Count("CancelPlanner") != 0 {
		t.Fatal("asking stopped the attempt")
	}
	m = ptPress(t, m, "x")
	if m.planStopAsked() || rig.garden.Count("CancelPlanner") != 0 {
		t.Fatal("another key stopped it or kept the question")
	}
	m = ptPress(t, m, "c", "y")
	if rig.garden.Count("CancelPlanner") != 1 {
		t.Fatalf("y did not stop the attempt: %v", rig.garden.Calls())
	}
	if m.garden.err != nil {
		t.Fatalf("stopping from the overlay failed: %+v", m.garden.err)
	}
}

func TestPlanTabOpenPlannerGoesToTheSessionAndEscReturnsWithScroll(t *testing.T) {
	for _, sz := range tabSizes {
		m, rig := tabEnv(t, sz[0], sz[1], func(t *testing.T, rig plannerRig) {
			a := revisedAttempt(plan.StateRunning)
			a.ID, a.Revises = "plan-fake-9", nil
			rig.garden.SetAttempt(*a)
			rig.agents.Sessions.Add(agentsapi.Session{Summary: agentterminal.Summary{ID: "sess-fake-5", Provider: "claude", AccountID: "c1", Name: "Plan", State: "running"}})
			rig.garden.LocalWorkIssues = localRun(true, "running", plan.PlanFileWriting, working("Read main.go"))
		})
		m = ptPress(t, m, "down")
		scroll := m.garden.board.docScroll
		m = ptPress(t, m, "p")
		if m.garden.planner.stage != plannerSession || !m.garden.planner.direct || m.gardenOverlayOpen() {
			t.Fatalf("p did not open the session: stage %d\n%s", m.garden.planner.stage, view(m))
		}
		m = ptPress(t, m, "esc")
		if m.garden.planner.stage != plannerNone || !m.gardenOverlayOpen() || m.garden.board.section != planTabSection || m.garden.board.docScroll != scroll {
			t.Fatalf("Esc did not return to the Plan tab (stage %d, section %d, scroll %d/%d)", m.garden.planner.stage, m.garden.board.section, m.garden.board.docScroll, scroll)
		}
		if m.agents.view != nil {
			t.Fatal("the terminal attachment was kept after leaving")
		}
		rig.quiet(t)
	}
}

func TestPlanTabPublishOpensTheBoxForTheReadyPlanAndEscReturns(t *testing.T) {
	m, rig := tabEnv(t, 120, 35, func(t *testing.T, rig plannerRig) {
		pv, err := rig.garden.PreviewPlanner(context.Background(), 1)
		if err != nil {
			t.Fatal(err)
		}
		a := revisedAttempt(plan.StateEnded)
		a.ID, a.Revises, a.Spec = "plan-fake-9", nil, pv.Spec
		rig.garden.SetAttempt(*a)
		rig.garden.PlannerOverlay = map[string]gardenapi.PlannerLiveView{"plan-fake-9": {PlanFile: plan.PlanFileInfo{State: plan.PlanFileReady}}}
		rig.garden.PlannerPlans = map[string]string{"plan-fake-9": longPlan("from the planner")}
		rig.garden.LocalWorkIssues = localRun(false, "ended", plan.PlanFileReady, nil)
	})
	m = ptPress(t, m, "u")
	if m.garden.planrev.stage != planCompose {
		t.Fatalf("u did not open the publish box: %d\n%s", m.garden.planrev.stage, view(m))
	}
	mustContain(t, "prefilled", view(m), "from the planner")
	if rig.garden.Count("PublishPlan") != 0 {
		t.Fatal("opening the box published")
	}
	m = ptPress(t, m, "esc")
	if m.garden.planrev.stage != planNone || m.garden.planner.stage != plannerNone || !m.gardenOverlayOpen() || m.garden.board.section != planTabSection {
		t.Fatalf("Esc did not return to the Plan tab: planrev %d planner %d", m.garden.planrev.stage, m.garden.planner.stage)
	}
}

func TestPlanTabOpenReviewReturnsWithScrollRestored(t *testing.T) {
	m, _ := tabEnv(t, 80, 24, func(t *testing.T, rig plannerRig) {
		scriptPlan(t, rig, longPlan("first"))
		scriptPlan(t, rig, longPlan("second"))
		rig.garden.PlanRevisePreview = previewOf(t, rig)
	})
	m = ptPress(t, m, "down", "down", "down")
	scroll := m.garden.board.docScroll
	if scroll != 3 {
		t.Fatalf("scroll %d", scroll)
	}
	m = ptPress(t, m, "r")
	if m.garden.planrev.stage != planReview || m.gardenOverlayOpen() {
		t.Fatal("r did not open the review")
	}
	m = ptPress(t, m, "esc")
	if m.garden.planrev.stage != planNone || !m.gardenOverlayOpen() || m.garden.board.section != planTabSection || m.garden.board.docScroll != scroll {
		t.Fatalf("Esc did not return (section %d scroll %d)", m.garden.board.section, m.garden.board.docScroll)
	}
}

func TestPlanTabKeepsNarrowScrollAndKeepsThePlainRequestPlanRoute(t *testing.T) {
	m, rig := tabEnv(t, 80, 24, awaitingSetup)
	if m.garden.board.overlayFocus != "" {
		t.Fatal("focus should start in the body")
	}
	m = ptPress(t, m, "pgdown")
	if m.garden.board.docScroll == 0 {
		t.Fatal("the body does not scroll at 80x24")
	}
	mustContain(t, "short", flat(view(m)), "a Approve", "c Changes", "r Review")
	// Request plan keeps working from every state: p opens the planner hub as before.
	m = ptPress(t, m, "p")
	if m.garden.planner.stage != plannerHub {
		t.Fatalf("p opened stage %d", m.garden.planner.stage)
	}
	if rig.garden.Count("StartPlanner") != 0 {
		t.Fatal("opening the planner started one")
	}
}

func TestPlanTabSubStatesFitEverySizeInUnicodeAndASCII(t *testing.T) {
	retry := func(t *testing.T, rig plannerRig) {
		rev := scriptPlan(t, rig, longPlan("first"))
		rig.garden.AddPlanDecision(1, rev, garden.DecisionChangesRequested, "Tighten the risks section.")
		pv := previewOf(t, rig)
		pv.Base = plan.Base{Commit: planBase, Branch: "main", Dirty: true, DirtyFiles: 2, DirtyDigest: "d1"}
		pv.Pending = &gardenapi.PendingRevise{DecisionCommentID: lastDecisionID(rig), Reason: "Tighten the risks section."}
		rig.garden.PlanRevisePreview = pv
	}
	live := func(t *testing.T, rig plannerRig) {
		a := revisedAttempt(plan.StateRunning)
		a.ID, a.Revises = "plan-fake-9", nil
		rig.garden.SetAttempt(*a)
		rig.garden.LocalWorkIssues = localRun(true, "running", plan.PlanFileWriting, working("Read main.go"))
	}
	for _, sz := range tabSizes {
		for _, ascii := range []bool{false, true} {
			for name, run := range map[string]func(*testing.T, Model) Model{
				"form": func(t *testing.T, m Model) Model {
					return ptType(t, ptPress(t, m, "c"), strings.Repeat("Tighten the risks section. ", 12))
				},
				"empty":   func(t *testing.T, m Model) Model { return ptPress(t, m, "c") },
				"approve": func(t *testing.T, m Model) Model { return ptPress(t, m, "a") },
			} {
				t.Run(fmt.Sprintf("%s/%dx%d/%v", name, sz[0], sz[1], ascii), func(t *testing.T) {
					m, _ := tabEnvGlyphs(t, sz[0], sz[1], ascii, awaitingSetup)
					m = run(t, m)
					assertRows(t, m, name)
					if ascii {
						for _, r := range ansi.Strip(m.View()) {
							if r > 127 {
								t.Fatalf("non-ASCII %q", r)
							}
						}
					}
					// Tab walks the form's focus without leaving the panel.
					for range 5 {
						m = ptPress(t, m, "tab")
						assertRows(t, m, name+" tab")
					}
				})
			}
			t.Run(fmt.Sprintf("retry/%dx%d/%v", sz[0], sz[1], ascii), func(t *testing.T) {
				m, _ := tabEnvGlyphs(t, sz[0], sz[1], ascii, retry)
				m = ptPress(t, m, "b")
				mustContain(t, "retry", tabText(m), "[ ] Let the planner read uncommitted changes", "START A REVISION ATTEMPT FOR r1")
				assertRows(t, m, "retry")
				for range 4 {
					m = ptPress(t, m, "tab")
					assertRows(t, m, "retry tab")
				}
			})
			t.Run(fmt.Sprintf("stop/%dx%d/%v", sz[0], sz[1], ascii), func(t *testing.T) {
				m, _ := tabEnvGlyphs(t, sz[0], sz[1], ascii, live)
				m = ptPress(t, m, "c")
				assertRows(t, m, "stop")
			})
		}
	}
}
