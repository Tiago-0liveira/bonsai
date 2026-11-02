package ui

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/core/agentactivity"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/approve"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/plan"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
)

func localAct(s agentactivity.State, r agentactivity.Reason) *agentactivity.Activity {
	return &agentactivity.Activity{State: s, Reason: r}
}

var localCases = []struct {
	name  string
	sum   gardenapi.LocalRunSummary
	badge string
	agent string // text of the detail line
	plan  string
}{
	{"working", gardenapi.LocalRunSummary{AttemptID: "plan-a", State: "running", Live: true, Activity: localAct(agentactivity.Working, "")}, "planner working", "agent working", "plan file none yet"},
	{"waiting", gardenapi.LocalRunSummary{AttemptID: "plan-a", State: "running", Live: true, Activity: localAct(agentactivity.Waiting, agentactivity.ReasonQuestion)}, "planner needs you", "agent waiting for you (question)", "plan file none yet"},
	{"idle", gardenapi.LocalRunSummary{AttemptID: "plan-a", State: "running", Live: true, Activity: localAct(agentactivity.Idle, "")}, "planner idle", "agent idle", "plan file none yet"},
	{"ready", gardenapi.LocalRunSummary{AttemptID: "plan-a", State: "ended", PlanFile: plan.PlanFileReady}, "plan ready", "agent not running", "plan file ready"},
	{"invalid", gardenapi.LocalRunSummary{AttemptID: "plan-a", State: "ended", PlanFile: plan.PlanFileInvalid}, "plan file invalid", "agent not running", "plan file invalid"},
	{"ended", gardenapi.LocalRunSummary{AttemptID: "plan-a", State: "ended"}, "planner ended", "agent not running", "plan file none yet"},
}

// withLocal scripts what LocalWork reports for Issue #101 (ID 1) and reads it as the refresh would.
func withLocal(t *testing.T, m Model, rig plannerRig, sum *gardenapi.LocalRunSummary) Model {
	t.Helper()
	rig.garden.LocalWorkIssues = nil
	if sum != nil {
		s := *sum
		if s.Updated.IsZero() {
			s.Updated = time.Date(2026, 10, 20, 13, 4, 0, 0, time.UTC)
		}
		rig.garden.LocalWorkIssues = map[uint64]gardenapi.IssueLocalWork{1: {Planner: &s}}
	}
	m, cmd := m.gardenLoadLocal()
	return plannerRun(t, m, cmd)
}

func TestLocalBadgeOnBoardAndTableForEveryState(t *testing.T) {
	// Literal expectations; the timestamp is formatted in the local zone.
	prev := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = prev })
	activity := map[string]string{
		"working": "Planner working · updated Oct 20 13:04",
		"waiting": "Planner needs you · updated Oct 20 13:04",
		"idle":    "Planner idle · updated Oct 20 13:04",
		"ready":   "Plan ready to publish · updated Oct 20 13:04",
		"invalid": "Planner exited · updated Oct 20 13:04",
		"ended":   "Planner exited · updated Oct 20 13:04",
	}
	sizes := [][2]int{{120, 35}, {165, 45}, {80, 24}}
	for _, c := range localCases {
		for _, sz := range sizes {
			for _, ascii := range []bool{false, true} {
				m, rig := plannerEnv(t, sz[0], sz[1])
				if ascii {
					m.work.AgentGlyphs = "ascii"
				}
				m = withLocal(t, m, rig, &c.sum)
				label := c.name + " " + string(rune('0'+sz[0]/100)) + "xx"
				out := view(m)
				r, _, ok := m.garden.board.collection.Find(1)
				if !ok {
					t.Fatal("local update lost Issue")
				}
				text := activity[c.name]
				if got := boardActivity(r, false, true); got != text {
					t.Errorf("activity %s = %q, want %q", c.name, got, text)
				}
				b := m.garden.board
				cols := min(gardenBoardColumns(sz[0], b.selection.Mode), len(b.collection.Lanes(b.selection.Mode)))
				w := boardColumnWidths(sz[0], cols)[int(b.selection.Column)-b.selection.Window]
				want := ansi.Truncate(" "+text, w, "…")
				want = m.settingsASCII([]string{want})[0]
				if !strings.Contains(out, strings.TrimSpace(want)) {
					t.Errorf("board %s missing activity %q:\n%s", label, want, out)
				}

				mustContain(t, "board "+label, out, "#101", "Task 1")
				mustFit(t, "board "+label, m)
				if ascii {
					if bad := fixtureFree(out); len(bad) > 0 {
						t.Errorf("board %s: non-ASCII %q", label, string(bad))
					}
				}
				m = pressAll(t, m, "v")
				if m.garden.board.layout != layoutTable {
					t.Fatalf("v did not open the table (layout %d)", m.garden.board.layout)
				}
				out = view(m)
				mustContain(t, "table "+label, out, "#101")
				// Compact tables omit SIGNAL and the strip; wider tables retain activity.
				if sz[0] >= 120 {
					mustContain(t, "table "+label, out, "SIGNAL", strings.Split(text, " · ")[0])
				}
				mustFit(t, "table "+label, m)
				if ascii {
					if bad := fixtureFree(out); len(bad) > 0 {
						t.Errorf("table %s: non-ASCII %q", label, string(bad))
					}
				}
			}
		}
	}
}

// fixtureFree is the non-ASCII text of a view without the fixture titles' own 界 and é.
func fixtureFree(s string) []rune {
	var out []rune
	for _, r := range nonASCII(s) {
		if r != '界' && r != 'é' && r != '\u0301' {
			out = append(out, r)
		}
	}
	return out
}

func TestLocalBadgeNeverChangesTheStage(t *testing.T) {
	m, rig := plannerEnv(t, 120, 35)
	before := m.garden.board.collection.Columns
	m = withLocal(t, m, rig, &localCases[0].sum)
	if _, _, ok := m.garden.board.collection.Find(1); !ok {
		t.Fatal("issue lost")
	}
	if len(before) != len(m.garden.board.collection.Columns) {
		t.Fatal("badge changed the columns")
	}
	if rig.garden.Writes() != 0 {
		t.Fatal("reading local work wrote")
	}
}

func TestLocalFactOnTheOpenedIssue(t *testing.T) {
	for _, c := range localCases {
		for _, sz := range [][2]int{{120, 35}, {80, 24}} {
			m, rig := plannerEnv(t, sz[0], sz[1])
			m = withLocal(t, m, rig, &c.sum)
			m = plannerPress(t, m, "enter")
			// The panel wraps the document beside its summary rail, so read the document itself.
			bodyW, _ := m.gardenOverlayBody()
			out := flat(ansi.Strip(strings.Join(m.gardenDocument(bodyW), "\n")))
			mustContain(t, c.name, out, "Local planner: "+c.sum.State, c.agent, c.plan, "updated Oct 20", "Press p to open it")
			if c.sum.Live {
				mustContain(t, c.name, out, "Planner keeps running in the background")
			}
			if c.sum.PlanFile == plan.PlanFileReady {
				mustContain(t, c.name, out, "u to review and publish")
			}
			mustFit(t, c.name, m)
		}
	}
	// An Issue with nothing local has no line.
	m, _ := plannerEnv(t, 120, 35)
	mustNotContain(t, "none", flat(view(plannerPress(t, m, "enter"))), "Local planner")
}

func TestLocalWorkIsReadOnEnteringAndOnRefresh(t *testing.T) {
	m, rig := plannerEnv(t, 120, 35)
	if rig.garden.Count("LocalWork") == 0 {
		t.Fatal("entering the board never read the local work")
	}
	n := rig.garden.Count("LocalWork")
	rig.garden.LocalWorkIssues = map[uint64]gardenapi.IssueLocalWork{1: {Planner: &localCases[0].sum}}
	m, cmd := m.gardenLoadWork()
	m = plannerRun(t, m, cmd)
	if rig.garden.Count("LocalWork") <= n {
		t.Fatal("refresh did not read the local work")
	}
	mustContain(t, "refreshed", view(m), "planner working")
}

func TestLocalFailedReadKeepsLastGoodMarkedStale(t *testing.T) {
	m, rig := plannerEnv(t, 120, 35)
	m = withLocal(t, m, rig, &localCases[0].sum)
	rig.garden.LocalWorkErr = errors.New("boom")
	m, cmd := m.gardenLoadLocal()
	m = plannerRun(t, m, cmd)
	out := view(m)
	mustContain(t, "stale", out, "1 planner working (stale)")
	if !m.garden.local.stale {
		t.Fatal("not marked stale")
	}
	// Only a successful read, even an empty one, clears it.
	rig.garden.LocalWorkErr = nil
	m = withLocal(t, m, rig, nil)
	if m.garden.local.stale || len(m.garden.local.issues) != 0 {
		t.Fatalf("empty success did not clear: %+v", m.garden.local)
	}
	mustNotContain(t, "cleared", view(m), "planner working")
}

func TestLocalLateAndStaleRepliesAreIgnored(t *testing.T) {
	m, rig := plannerEnv(t, 120, 35)
	// Two reads in flight: the older one is read first, the newer second, and they land in reverse order.
	rig.garden.LocalWorkIssues = map[uint64]gardenapi.IssueLocalWork{1: {Planner: &localCases[0].sum}}
	m, c1 := m.gardenLoadLocal()
	old := runCmds(c1)
	rig.garden.LocalWorkIssues = map[uint64]gardenapi.IssueLocalWork{1: {Planner: &localCases[3].sum}}
	m, c2 := m.gardenLoadLocal()
	newer := runCmds(c2)
	for _, msg := range newer {
		m, _ = send(t, m, msg)
	}
	for _, msg := range old {
		m, _ = send(t, m, msg)
	}
	if s := m.garden.local.issues[1].Planner; s == nil || s.PlanFile != plan.PlanFileReady {
		t.Fatalf("a late older reply overwrote the newer one: %+v", s)
	}
	mustContain(t, "newer", view(m), "plan ready")
	// An older failure does not mark the newer success stale.
	m, c3 := m.gardenLoadLocal()
	good := runCmds(c3)
	for _, msg := range good {
		m, _ = send(t, m, msg)
	}
	m, _ = send(t, m, gardenLocalMsg{seq: m.garden.local.seq - 1, err: errors.New("late failure")})
	if m.garden.local.stale {
		t.Fatal("an older failure marked newer data stale")
	}
	// A reply for another repository is dropped.
	other := gardenapi.LocalWorkView{Stamp: gardenapi.Stamp{LocalRepoID: "x", GardenID: 1, Epoch: 9, Generation: 9999}}
	m, _ = send(t, m, gardenLocalMsg{seq: m.garden.local.seq, view: other})
	if len(m.garden.local.issues) == 0 {
		t.Fatal("a reply for another repository replaced the summaries")
	}
}

func TestLocalFooterTotalsFitNarrow(t *testing.T) {
	for _, sz := range [][2]int{{80, 24}, {120, 35}, {165, 45}} {
		m, rig := plannerEnv(t, sz[0], sz[1])
		rig.garden.LocalWorkIssues = map[uint64]gardenapi.IssueLocalWork{
			1: {Planner: &localCases[0].sum},
			2: {Planner: &localCases[3].sum},
		}
		m, cmd := m.gardenLoadLocal()
		m = plannerRun(t, m, cmd)
		out := view(m)
		if sz[0] >= 120 {
			mustContain(t, "footer", out, "1 planner working · 1 plan ready")
		} else {
			mustContain(t, "footer", out, "1 working · 1 ready")
		}
		mustFit(t, "footer", m)
	}
	m, _ := plannerEnv(t, 120, 35)
	mustNotContain(t, "none", view(m), "planner working", "plan ready")
}

// --- the planner hub ---

func readyAttempt(t *testing.T, rig plannerRig, live bool) plan.Attempt {
	t.Helper()
	it := rig.garden.WorkView.Snapshot.Items[0]
	a := approve.Assess(it.Issue, it.Verification)
	st := plan.StateEnded
	if live {
		st = plan.StateRunning
	}
	att := plan.Attempt{ID: "plan-fake-9", Local: "x", Repo: it.Issue.Repo, Issue: it.Issue.ID, Number: 101, Role: plan.RolePlanner, State: st, SessionID: "sess-9",
		Provider: "claude", AccountName: "work", Base: plan.Base{Commit: planBase, Branch: "main"},
		Spec:        plan.SpecRef{RevisionHash: a.Head.Hash, CommentID: a.Head.CommentID, Ordinal: a.Ordinal, Fingerprint: a.Fingerprint},
		RequestedAt: time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)}
	rig.garden.SetAttempt(att)
	return att
}

func TestPlannerHubShowsLiveStatusAndAReadyPlan(t *testing.T) {
	for _, sz := range draftSizes {
		m, rig := plannerEnv(t, sz[0], sz[1])
		readyAttempt(t, rig, false)
		rig.garden.PlannerOverlay = map[string]gardenapi.PlannerLiveView{"plan-fake-9": {
			PlanFile: plan.PlanFileInfo{State: plan.PlanFileReady, Size: 2048, ModTime: time.Date(2026, 10, 20, 13, 5, 6, 0, time.UTC)}}}
		m = openPlanner(t, m)
		out := flat(view(m))
		mustContain(t, "hub", out, "plan ready", "Plan ready: press u to review and publish.")
		mustFit(t, "hub", m)
		if sz[0] >= 120 {
			mustContain(t, "hub detail", out, "plan file ready · 2.0 KB", "changed Oct 20")
		}
	}
}

func TestPlannerHubShowsWorkingAttemptAndTheKeepsRunningNote(t *testing.T) {
	for _, sz := range draftSizes {
		m, rig := plannerEnv(t, sz[0], sz[1])
		readyAttempt(t, rig, true)
		rig.garden.PlannerOverlay = map[string]gardenapi.PlannerLiveView{"plan-fake-9": {
			Activity: localAct(agentactivity.Working, ""), PlanFile: plan.PlanFileInfo{State: plan.PlanFileWriting, Size: 300}}}
		m = openPlanner(t, m)
		out := flat(view(m))
		mustContain(t, "hub", out, "working", "plan writing", "Planner keeps running in the background", "the badge on this Issue, or p")
		mustFit(t, "hub", m)
	}
}

func TestEscFromThePlannerSessionShowsTheKeepsRunningNote(t *testing.T) {
	m, rig := plannerEnv(t, 120, 35)
	m = requestPlan(t, openPlanner(t, m))
	m = plannerPress(t, m, "ctrl+s")
	if m.garden.planner.stage != plannerSession {
		t.Fatalf("start did not open the session: stage %d\n%s", m.garden.planner.stage, view(m))
	}
	_ = rig
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyCtrlCloseBracket}) // a fresh planner owns the keyboard until Ctrl+]
	m = plannerPress(t, m, "esc")
	if m.garden.planner.stage != plannerHub {
		t.Fatalf("Esc did not return to the attempts: stage %d", m.garden.planner.stage)
	}
	mustContain(t, "hub", flat(view(m)), "Planner keeps running in the background")
	// Leaving the planner entirely keeps the reminder on the Issue.
	m = plannerPress(t, m, "esc")
	if m.garden.planner.stage != plannerNone {
		t.Fatal("Esc did not leave the planner")
	}
	mustContain(t, "issue", flat(view(m)), "Planner keeps running in the background")
}

func TestHubPollRefreshesTheOverlayAndIgnoresALateOlderReply(t *testing.T) {
	m, rig := plannerEnv(t, 120, 35)
	readyAttempt(t, rig, true)
	rig.garden.PlannerOverlay = map[string]gardenapi.PlannerLiveView{"plan-fake-9": {Activity: localAct(agentactivity.Working, "")}}
	m = openPlanner(t, m)
	mustNotContain(t, "before", flat(view(m)), "Plan ready")

	// A first poll is slow, a second one lands first with the plan ready.
	slow := m.gardenPlannerPoll()
	slowMsgs := runCmds(slow)
	rig.garden.PlannerOverlay = map[string]gardenapi.PlannerLiveView{"plan-fake-9": {PlanFile: plan.PlanFileInfo{State: plan.PlanFileReady, Size: 10}}}
	fast := runCmds(m.gardenPlannerPoll())
	for _, msg := range fast {
		m, _ = send(t, m, msg)
	}
	for _, msg := range slowMsgs {
		m, _ = send(t, m, msg)
	}
	mustContain(t, "after", flat(view(m)), "Plan ready: press u")

	// The tick does it too, and a failed poll keeps what is shown.
	m, _ = send(t, m, gardenPlannerPollMsg{issue: 1, err: errors.New("boom")})
	mustContain(t, "failed poll", flat(view(m)), "Plan ready: press u")
}

func TestLocalTickRearmsOnlyWhileGardenIsInView(t *testing.T) {
	m, _ := plannerEnv(t, 120, 35)
	if !m.garden.local.tickOn {
		t.Fatal("entering the board did not arm the timer")
	}
	gen := m.garden.local.tickGen
	m, cmd := send(t, m, gardenLocalTickMsg{gen: gen})
	if cmd == nil || m.garden.local.tickGen != gen+1 {
		t.Fatal("tick did not re-arm in view")
	}
	// A superseded tick does nothing.
	if _, cmd := send(t, m, gardenLocalTickMsg{gen: gen}); cmd != nil {
		t.Fatal("a stale tick produced work")
	}
	m.workspace = wsSettings
	m, cmd = send(t, m, gardenLocalTickMsg{gen: m.garden.local.tickGen})
	if cmd != nil || m.garden.local.tickOn {
		t.Fatal("tick kept running outside Garden")
	}
}

// --- publish prefill ---

func publishEnv(t *testing.T, ready bool) (Model, plannerRig) {
	t.Helper()
	m, rig := plannerEnv(t, 120, 35)
	readyAttempt(t, rig, false)
	if ready {
		rig.garden.PlannerOverlay = map[string]gardenapi.PlannerLiveView{"plan-fake-9": {PlanFile: plan.PlanFileInfo{State: plan.PlanFileReady, Size: 100}}}
		rig.garden.PlannerPlans = map[string]string{"plan-fake-9": longPlan("from the file")}
	}
	return openPlanner(t, m), rig
}

func TestUPrefillsThePublishBoxFromAReadyPlanFileAndPublishesNothing(t *testing.T) {
	m, rig := publishEnv(t, true)
	m = plannerPress(t, m, "u")
	if m.garden.planrev.stage != planCompose {
		t.Fatalf("u did not open the box: stage %d", m.garden.planrev.stage)
	}
	if got := m.garden.planrev.ta.Value(); got != longPlan("from the file") {
		t.Fatalf("box not prefilled: %q", got)
	}
	mustContain(t, "note", flat(view(m)), "from the plan file", "nothing is published yet")
	mustFit(t, "compose", m)
	m = plannerPress(t, m, "ctrl+s")
	if m.garden.planrev.stage != planConfirm || rig.garden.Count("PublishPlan") != 0 || len(rig.garden.PlanPublished) != 0 {
		t.Fatalf("preview published or did not show: stage %d", m.garden.planrev.stage)
	}
	// Only the confirmation publishes, with the text of the box.
	m = plannerPress(t, m, "enter")
	if len(rig.garden.PlanPublished) != 1 {
		t.Fatalf("confirmation did not publish: %d", len(rig.garden.PlanPublished))
	}
}

func TestPrefillNeverOverwritesWhatTheUserTypedOrKept(t *testing.T) {
	m, _ := publishEnv(t, true)
	m.garden.planrev.drafts = map[string]string{"plan-fake-9": "my own draft"}
	m = plannerPress(t, m, "u")
	if got := m.garden.planrev.ta.Value(); got != "my own draft" {
		t.Fatalf("kept draft was replaced: %q", got)
	}
}

func TestPrefillFallsBackToAnEmptyBoxWithTheError(t *testing.T) {
	m, rig := publishEnv(t, true)
	rig.garden.PlannerPlanErr = &gardenapi.Error{Code: gardenapi.CodePlanFileInvalid, Message: "The plan file is not acceptable.", Next: "Paste the plan instead."}
	m = plannerPress(t, m, "u")
	if m.garden.planrev.stage != planCompose || m.garden.planrev.ta.Value() != "" {
		t.Fatalf("fallback is not the empty box: %q", m.garden.planrev.ta.Value())
	}
	mustContain(t, "error", flat(view(m)), "The plan file is not acceptable.", "Paste the plan instead.")
	if rig.garden.Count("PublishPlan") != 0 {
		t.Fatal("published")
	}
}

func TestNotReadyPlanFileOpensTheEmptyBoxWithoutReadingIt(t *testing.T) {
	m, rig := publishEnv(t, false)
	m = plannerPress(t, m, "u")
	if m.garden.planrev.stage != planCompose || m.garden.planrev.ta.Value() != "" || rig.garden.Count("PlannerPlan") != 0 {
		t.Fatalf("not-ready flow wrong: %q, reads %d", m.garden.planrev.ta.Value(), rig.garden.Count("PlannerPlan"))
	}
}

func TestLatePlanReadAfterTheBoxClosedIsDropped(t *testing.T) {
	m, rig := publishEnv(t, true)
	m, cmd := m.gardenPlannerPublish()
	msgs := runCmds(cmd)
	m = plannerPress(t, m, "esc") // keeps the (empty) draft and leaves the box
	for _, msg := range msgs {
		m, _ = send(t, m, msg)
	}
	if m.garden.planrev.ta.Value() != "" {
		t.Fatal("a late plan read filled a closed box")
	}
	_ = rig
}
