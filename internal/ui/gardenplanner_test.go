package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/agentterminal"
	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/plan"
	gardensync "github.com/Tiago-0liveira/bonsai/internal/core/garden/sync"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
	agentfake "github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi/fake"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
	gardenfake "github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi/fake"
	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
)

// plannerAccount is an agent profile with its provider's read-only verdict.
func plannerAccount(id, provider, name string, ro agents.ReadOnlySupport) agentsapi.Account {
	a := wsAccount(id, provider, name, "logged_in")
	a.ReadOnly = ro
	return a
}

type plannerRig struct {
	garden *gardenfake.Backend
	agents *agentfake.Backend
}

// plannerEnv is the redesign shell with a connected Garden holding approved Issue #101 (the first Ready card)
// and the agents fake with one read-only capable account, one that cannot enforce it and a second capable one.
func plannerEnv(t *testing.T, w, h int) (Model, plannerRig) {
	t.Helper()
	fb := gardenFake(gardenapi.StateConnected)
	fb.WorkView.Snapshot.Items = []gardensync.Item{boardItem(t, 1, garden.StageReady, false), boardItem(t, 2, garden.StageReady, false)}
	ag := agentfake.New()
	def := agentsapi.Defaults{Fields: agentfake.SampleLaunchFields(), LaunchFields: agentfake.SampleLaunchFields(), Values: map[string]string{}}
	ag.Accounts.Items = []agentsapi.Account{
		plannerAccount("c1", "claude", "work", agents.ReadOnlySupport{Supported: true, Mechanism: "tool allowlist", Allows: []string{"read files"}}),
		plannerAccount("a1", "antigravity", "agy", agents.ReadOnlySupport{Reason: "agy has no tool allowlist"}),
		plannerAccount("x1", "codex", "personal", agents.ReadOnlySupport{Supported: true, Mechanism: "OS sandbox", Allows: []string{"read files"}}),
	}
	ag.Accounts.Infos = map[agents.AccountID]agents.AccountInfo{
		"c1": {Identity: "w@example.com", Options: map[string]any{"auth_status": "logged_in"}},
		"a1": {Identity: "a@example.com", Options: map[string]any{"auth_status": "logged_in"}},
		"x1": {Identity: "me@example.com", Options: map[string]any{"auth_status": "logged_in"}},
	}
	ag.Accounts.LaunchDefaults = map[agents.AccountID]agentsapi.Defaults{"c1": def, "a1": def, "x1": def}
	// The attempt names a session; the agents fake hosts it, as the holder does.
	fb.OnPlannerStart = func(a plan.Attempt) {
		ag.Sessions.Add(agentsapi.Session{Summary: agentterminal.Summary{ID: a.SessionID, Provider: a.Provider, AccountID: a.AccountID, Name: "Plan", State: "running"}})
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Cleanup(func() {
		theme.Current = theme.Resolve("bonsai", nil)
		applyTheme()
	})
	st, err := config.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	m := sized(t, New(t.TempDir(), &config.Config{}, st, WithLayout(LayoutRedesign), WithGarden(fb), WithAgents(ag.API())), w, h)
	m, _ = drainAll(t, m, m.Init())
	m = pressAll(t, m, "enter")
	return m, plannerRig{fb, ag}
}

// openPlanner opens the selected Issue and presses p.
func openPlanner(t *testing.T, m Model) Model {
	t.Helper()
	m = plannerPress(t, m, "enter", "p")
	if m.garden.planner.stage != plannerHub {
		t.Fatalf("p did not open the planner hub: stage %d\n%s", m.garden.planner.stage, view(m))
	}
	return m
}

// requestPlan walks Request AI plan to the account choice and returns it with the dialog loaded.
func requestPlan(t *testing.T, m Model) Model {
	t.Helper()
	m = plannerPress(t, m, "enter")
	if m.garden.planner.stage != plannerChoose {
		t.Fatalf("Request AI plan did not open the account choice: stage %d\n%s", m.garden.planner.stage, view(m))
	}
	return m
}

func TestPlannerHubIsOfferedForAnApprovedSpecificationAndStartsNothing(t *testing.T) {
	for _, sz := range draftSizes {
		m, rig := plannerEnv(t, sz[0], sz[1])
		m = openPlanner(t, m)
		out := view(m)
		mustContain(t, "hub", out, "LOCAL READ-ONLY PLANNER FOR #101", "Specification", "fingerprint", "Repository", "clean tree",
			"no worktree is created", "Request AI plan", "Only this button starts a planner")
		mustFit(t, "hub", m)
		mustContain(t, "crumbs", strings.Join(m.gardenCrumbs(), " > "), "Read-only planning")
		if rig.garden.Count("StartPlanner") != 0 || rig.garden.Writes() != 0 || len(rig.agents.Sessions.Starts()) != 0 {
			t.Fatalf("previewing started something: writes=%d starts=%d", rig.garden.Writes(), len(rig.agents.Sessions.Starts()))
		}
		// Back is not the primary action when a planner can be requested, and Enter on it changes nothing.
		m = plannerPress(t, m, "left", "enter")
		if m.garden.planner.stage != plannerNone || rig.garden.Count("StartPlanner") != 0 {
			t.Fatalf("Back did not leave the planner: stage %d", m.garden.planner.stage)
		}
	}
}

func TestPlannerIsUnavailableUntilGateOneIsVerified(t *testing.T) {
	m, rig := plannerEnv(t, 120, 35)
	// Strip the approval record: the label and metadata still claim readiness, which is not proof.
	items := rig.garden.WorkView.Snapshot.Items
	items[0].Verification.Approvals, items[0].Verification.ValidApprovals = nil, nil
	rig.garden.WorkView.Snapshot.Items = items
	m = openPlanner(t, m)
	out := view(m)
	mustContain(t, "unavailable", out, "NO PLANNER FOR #101 YET", "not approved", "Back")
	mustNotContain(t, "unavailable", out, "Request AI plan", "Let the planner read")
	m = plannerPress(t, m, "enter")
	if m.garden.planner.stage != plannerNone || rig.garden.Count("StartPlanner") != 0 {
		t.Fatalf("Enter on an unavailable planner must only go back: stage %d starts %d", m.garden.planner.stage, rig.garden.Count("StartPlanner"))
	}
}

func TestRequestingAPlanBindsTheAttemptAndShowsTheSameTerminalController(t *testing.T) {
	for _, sz := range draftSizes {
		m, rig := plannerEnv(t, sz[0], sz[1])
		m = requestPlan(t, openPlanner(t, m))
		out := view(m)
		mustContain(t, "choice", out, "Request AI plan", "read-only / no worktree", "Read-only: tool allowlist", "Accept read-only and request plan")
		mustFit(t, "choice", m)
		if rig.garden.Count("StartPlanner") != 0 {
			t.Fatal("opening the account choice started a planner")
		}
		// Ctrl+S is the explicit Request on the chosen (first, supported) account.
		m = plannerPress(t, m, "ctrl+s")
		if rig.garden.Count("StartPlanner") != 1 {
			t.Fatalf("StartPlanner calls = %d\n%s", rig.garden.Count("StartPlanner"), view(m))
		}
		req := rig.garden.PlannerStarted[0]
		if !req.AcceptReadOnly || req.AccountID != "c1" || req.IncludeDirty || req.Preview.Spec.RevisionHash == "" || req.Preview.Base.Commit == "" {
			t.Fatalf("request = %+v", req)
		}
		if st := m.garden.planner; st.stage != plannerSession || st.attempt == "" {
			t.Fatalf("stage %d attempt %q", st.stage, st.attempt)
		}
		// No second PTY client and no worktree: the TUI attached to the session the attempt names, as the writer.
		att := rig.agents.Sessions.Attaches()
		if len(att) != 1 || att[0].ID != "sess-fake-1" || att[0].Opts.ReadOnly || len(rig.agents.Sessions.Starts()) != 0 {
			t.Fatalf("attaches = %+v starts = %d", att, len(rig.agents.Sessions.Starts()))
		}
		if !m.plannerTerminalMode() {
			t.Fatalf("a fresh planner must take the keyboard")
		}
		mustContain(t, "session", view(m), "LOCAL READ-ONLY PLANNER / #101", "read-only / no worktree")
		mustFit(t, "session", m)
	}
}

func TestAProviderThatCannotEnforceReadOnlyCannotPlan(t *testing.T) {
	m, rig := plannerEnv(t, 120, 35)
	m = requestPlan(t, openPlanner(t, m))
	// The accounts sort by provider: antigravity first.
	m = plannerPress(t, m, "up")
	cur, _ := m.garden.planner.dlg.Current()
	if cur.Provider != "antigravity" {
		// Walk to it whatever the order is.
		for i := 0; i < 3 && cur.Provider != "antigravity"; i++ {
			m = plannerPress(t, m, "down")
			cur, _ = m.garden.planner.dlg.Current()
		}
	}
	mustContain(t, "unsupported", view(m), "Antigravity cannot plan: agy has no tool allowlist", "Cannot plan")
	m = plannerPress(t, m, "ctrl+s")
	if rig.garden.Count("StartPlanner") != 0 || m.garden.planner.stage != plannerChoose {
		t.Fatalf("an unsupported provider reached the backend: starts=%d stage=%d", rig.garden.Count("StartPlanner"), m.garden.planner.stage)
	}
	mustContain(t, "refusal", view(m), "Choose another account")
}

func TestUncommittedChangesNeedAnExplicitDecision(t *testing.T) {
	m, rig := plannerEnv(t, 120, 35)
	rig.garden.PlannerBase = plan.Base{Commit: "c0ffee1234567890", Branch: "main", Dirty: true, DirtyFiles: 2, DirtyDigest: "d1"}
	m = openPlanner(t, m)
	mustContain(t, "dirty", view(m), "2 uncommitted changes, not included", "[ ] Let the planner read the 2 uncommitted changes")
	m = plannerPress(t, m, "enter")
	if m.garden.planner.stage != plannerHub {
		t.Fatalf("a dirty checkout reached the account choice without a decision")
	}
	mustContain(t, "decision needed", view(m), "uncommitted changes. Press x")
	m = plannerPress(t, m, "x")
	mustContain(t, "included", view(m), "[x] Let the planner read", "2 uncommitted changes, included")
	m = requestPlan(t, m)
	m = plannerPress(t, m, "ctrl+s")
	if rig.garden.Count("StartPlanner") != 1 || !rig.garden.PlannerStarted[0].IncludeDirty {
		t.Fatalf("the decision did not reach the request: %+v", rig.garden.PlannerStarted)
	}
	a := rig.garden.PlannerStarted[0]
	if !a.Preview.Base.Dirty || a.Preview.Base.DirtyFiles != 2 {
		t.Fatalf("the dirty base was not carried: %+v", a.Preview.Base)
	}
}

func TestADirtyDecisionDoesNotSurviveAChangedCheckout(t *testing.T) {
	m, rig := plannerEnv(t, 120, 35)
	rig.garden.PlannerBase = plan.Base{Commit: "c0ffee1234567890", Dirty: true, DirtyFiles: 2, DirtyDigest: "d1"}
	m = plannerPress(t, openPlanner(t, m), "x")
	if !m.garden.planner.dirtyDecision() {
		t.Fatal("decision not recorded")
	}
	rig.garden.PlannerBase = plan.Base{Commit: "c0ffee1234567890", Dirty: true, DirtyFiles: 3, DirtyDigest: "d2"}
	m = plannerPress(t, m, "R")
	if m.garden.planner.dirtyDecision() || m.garden.planner.includeDirty {
		t.Fatal("a decision made for other content carried over")
	}
}

func TestFocusedPlannerTerminalKeepsEveryKeyFromTheGardenAndIsLeftWithCtrlBracket(t *testing.T) {
	m, rig := plannerEnv(t, 120, 35)
	m = plannerPress(t, requestPlan(t, openPlanner(t, m)), "ctrl+s")
	// Let the attach land: the stream exists and this client writes.
	if !m.plannerTerminalMode() || m.agents.view.stream == nil || m.agents.view.state != attachControlling {
		t.Fatalf("not focused on a writing stream: %+v", m.agents.view)
	}
	stream := rig.agents.Sessions.Streams("sess-fake-1")[0]
	// The holder's replay ends at its first status frame; until then input is dropped by design.
	m = plannerFrames(t, m, stream, out("planner ready\r\n"), status("running"))
	stage, workspace, sel := m.garden.planner.stage, m.workspace, m.garden.board.selection
	// Keys that quit, open help, switch workspace, go back or refresh everywhere else belong to the agent now.
	for _, k := range []string{"q", "?", "esc", "tab", "x", "R", "p", "c", "left"} {
		var cmd tea.Cmd
		m, cmd = send(t, m, keyFor(k))
		if cmd != nil {
			if msg := cmd(); msg != nil {
				if _, quit := msg.(tea.QuitMsg); quit {
					t.Fatalf("%q quit the program while the planner was focused", k)
				}
			}
		}
	}
	if m.garden.planner.stage != stage || m.workspace != workspace || m.garden.board.selection != sel || m.modal != nil {
		t.Fatalf("a focused key reached the Garden: stage %d workspace %d", m.garden.planner.stage, m.workspace)
	}
	if !m.plannerTerminalMode() {
		t.Fatal("focus was lost by an ordinary key")
	}
	// The emulator hands bytes to the input pump; drain it as the program loop would.
	m = plannerPump(t, m)
	got := strings.Join(inputsOf(stream), "")
	for _, want := range []string{"q", "?", "x", "R", "p", "c"} {
		if !strings.Contains(got, want) {
			t.Errorf("focused key %q never reached the agent (got %q)", want, got)
		}
	}
	// Ctrl+] is the one way out, and then keys are the Garden's again.
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyCtrlCloseBracket})
	if m.plannerTerminalMode() {
		t.Fatal("Ctrl+] did not unfocus the terminal")
	}
	m = plannerPress(t, m, "esc")
	if m.garden.planner.stage != plannerHub || m.agents.view != nil {
		t.Fatalf("Esc did not return to the attempts: stage %d", m.garden.planner.stage)
	}
}

func keyFor(k string) tea.KeyMsg {
	switch k {
	case "esc", "tab", "left", "right", "up", "down", "enter":
		return keyOf(k)
	case "ctrl+s":
		return tea.KeyMsg{Type: tea.KeyCtrlS}
	}
	return runeKey(k)
}

// plannerRun feeds a command's messages back through Update until nothing is left. The terminal's blocking waits
// are skipped (see runCmds); the input pump is plannerPump.
func plannerRun(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	for i := 0; i < 30; i++ {
		msgs := runCmds(cmd)
		if len(msgs) == 0 {
			return m
		}
		var next []tea.Cmd
		for _, msg := range msgs {
			var c tea.Cmd
			m, c = send(t, m, msg)
			next = append(next, c)
		}
		cmd = tea.Batch(next...)
	}
	t.Fatal("commands kept producing messages")
	return m
}

// plannerPress sends keys one at a time and runs what each returns.
func plannerPress(t *testing.T, m Model, keys ...string) Model {
	t.Helper()
	for _, k := range keys {
		var cmd tea.Cmd
		m, cmd = send(t, m, keyFor(k))
		m = plannerRun(t, m, cmd)
	}
	return m
}

// plannerPump moves what the focused terminal queued for the agent into Update, as the wait command would.
func plannerPump(t *testing.T, m Model) Model {
	t.Helper()
	for m.agents.view != nil && m.agents.view.term != nil {
		term := m.agents.view.term
		select {
		case d := <-term.Input():
			var cmd tea.Cmd
			m, cmd = send(t, m, agentInputMsg{term: term, data: d})
			m = plannerRun(t, m, cmd)
		case <-time.After(40 * time.Millisecond):
			return m
		}
	}
	return m
}

// plannerFrames delivers frames on the planner's stream, as the wait command would batch them.
func plannerFrames(t *testing.T, m Model, st *agentfake.Stream, fs ...agentterminal.Frame) Model {
	t.Helper()
	for _, f := range fs {
		if !st.Push(f) {
			t.Fatal("stream refused a frame")
		}
	}
	v := m.agents.view
	var cmd tea.Cmd
	m, cmd = send(t, m, agentFramesMsg{token: v.token, frames: fs})
	return plannerRun(t, m, cmd)
}

func attemptOf(id, state string, n int) plan.Attempt {
	st := plan.State(state)
	a := plan.Attempt{ID: id, Local: gardenfake.Clone, Repo: 48120394, Issue: 1, Number: int64(100 + n), Role: plan.RolePlanner,
		Spec: plan.SpecRef{RevisionHash: "h", Ordinal: 1, Fingerprint: "f"}, Base: plan.Base{Commit: "c0ffee1234567890"},
		Isolation: plan.Isolation{Mechanism: "tool allowlist", Allows: []string{"read files"}}, Provider: "claude", AccountID: "c1", AccountName: "work",
		SessionID: "sess-" + id, State: st, RequestedAt: time.Date(2026, 10, 20, 10, n, 0, 0, time.UTC), UpdatedAt: time.Date(2026, 10, 20, 10, n, 0, 0, time.UTC)}
	if !st.Live() {
		end := a.UpdatedAt.Add(time.Minute)
		a.EndedAt = &end
	}
	if st == plan.StateFailed {
		a.Reason = "session failed"
	}
	return a
}

// scriptHistory gives Issue #101 (ID 1) a past: a failed attempt and a cancelled one, with the current spec
// fingerprint so the planner may be requested again.
func scriptHistory(t *testing.T, m Model, rig plannerRig) Model {
	t.Helper()
	probe, err := rig.garden.PreviewPlanner(t.Context(), 1)
	if err != nil || !probe.CanRequest() {
		t.Fatalf("probe = %+v, %v", probe, err)
	}
	for i, st := range []string{"failed", "cancelled"} {
		a := attemptOf("plan-old"+st, st, i)
		a.Spec = probe.Spec
		rig.garden.SetAttempt(a)
	}
	return m
}

func TestPlannerHistoryKeepsFailedAndCancelledAttemptsAndRetryIsANewRequest(t *testing.T) {
	for _, sz := range draftSizes {
		m, rig := plannerEnv(t, sz[0], sz[1])
		m = scriptHistory(t, m, rig)
		m = openPlanner(t, m)
		out := view(m)
		mustContain(t, "history", out, "Attempts (newest first)", "cancelled", "failed (session failed)", "Claude / work")
		mustFit(t, "history", m)
		before := rig.garden.Count("StartPlanner")
		// Retry is the same explicit action again, and adds an attempt next to the old ones.
		m = plannerPress(t, requestPlan(t, m), "ctrl+s")
		if rig.garden.Count("StartPlanner") != before+1 {
			t.Fatalf("retry did not make exactly one new request")
		}
		m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyCtrlCloseBracket})
		m = plannerPress(t, m, "esc")
		out = view(m)
		mustContain(t, "after retry", out, "#3", "starting", "cancelled", "failed (session failed)")
		if len(m.garden.planner.preview.Attempts) != 3 {
			t.Fatalf("history = %d attempts", len(m.garden.planner.preview.Attempts))
		}
	}
}

func TestPlannerRefreshAndExternalEditsNeverStartOne(t *testing.T) {
	m, rig := plannerEnv(t, 120, 35)
	m = openPlanner(t, m)
	for i := 0; i < 3; i++ {
		m = plannerPress(t, m, "R")
	}
	// The Issue is edited elsewhere: the next read sees a different state, still without running anything.
	items := rig.garden.WorkView.Snapshot.Items
	items[0].Verification.Approvals, items[0].Verification.ValidApprovals = nil, nil
	rig.garden.WorkView.Snapshot.Items = items
	m = plannerPress(t, m, "R")
	mustContain(t, "edited", view(m), "NO PLANNER FOR #101 YET")
	if rig.garden.Count("StartPlanner") != 0 || len(rig.agents.Sessions.Starts()) != 0 || len(rig.agents.Sessions.Attaches()) != 0 {
		t.Fatal("reading started a planner")
	}
}

func TestPlannerCancelNeedsConfirmationAndKeepsTheAttempt(t *testing.T) {
	m, rig := plannerEnv(t, 120, 35)
	probe, _ := rig.garden.PreviewPlanner(t.Context(), 1)
	live := attemptOf("plan-live", "running", 5)
	live.Spec = probe.Spec
	rig.garden.SetAttempt(live)
	rig.agents.Sessions.Add(agentsapi.Session{Summary: agentterminal.Summary{ID: live.SessionID, State: "running"}})
	m = openPlanner(t, m)
	mustContain(t, "live", view(m), "running", "still running")
	mustNotContain(t, "live", view(m), "Request AI plan  ")
	// A second request while one runs is not offered.
	m = plannerPress(t, m, "enter")
	if m.garden.planner.stage == plannerChoose {
		t.Fatal("a second planner can be requested while one runs")
	}
	m = openPlanner(t, m)
	m = plannerPress(t, m, "c")
	mustContain(t, "confirm", view(m), "Cancel this attempt?")
	m = plannerPress(t, m, "n")
	if rig.garden.Count("CancelPlanner") != 0 {
		t.Fatal("declined confirmation cancelled the attempt")
	}
	m = plannerPress(t, m, "c", "y")
	if rig.garden.Count("CancelPlanner") != 1 {
		t.Fatalf("CancelPlanner calls = %d", rig.garden.Count("CancelPlanner"))
	}
	mustContain(t, "cancelled", view(m), "cancelled")
	if _, ok := m.garden.planner.attemptByID("plan-live"); !ok {
		t.Fatal("cancelling removed the attempt from the history")
	}
	mustFit(t, "cancelled", m)
}

func TestPlannerStaleRepliesNeverOverwriteNewerAttempts(t *testing.T) {
	m, rig := plannerEnv(t, 120, 35)
	rig.garden.SetAttempt(attemptOf("plan-a", "failed", 1))
	m = openPlanner(t, m)
	m, first := m.gardenPlannerRefresh()
	older := first()
	rig.garden.SetAttempt(attemptOf("plan-b", "ended", 2))
	m, second := m.gardenPlannerRefresh()
	newer := second()
	// The newer reply lands first, then the late one for the abandoned request.
	m, _ = send(t, m, newer)
	m, _ = send(t, m, older)
	if got := len(m.garden.planner.preview.Attempts); got != 2 {
		t.Fatalf("a late reply replaced newer history: %d attempts", got)
	}
}

func TestPlannerEndedSessionRefreshesItsAttempt(t *testing.T) {
	m, rig := plannerEnv(t, 120, 35)
	m = plannerPress(t, requestPlan(t, openPlanner(t, m)), "ctrl+s")
	stream := rig.agents.Sessions.Streams("sess-fake-1")[0]
	before := rig.garden.Count("PlannerAttempts")
	// The planner session ends: the attempt's record is read again so its state follows the session.
	rig.garden.SetAttempt(func() plan.Attempt {
		a, _ := m.garden.planner.attemptByID(m.garden.planner.attempt)
		a.State = plan.StateEnded
		return a
	}())
	exited := agentterminal.Frame{Type: "status", Status: &agentterminal.Summary{State: "exited"}}
	m = plannerFrames(t, m, stream, status("running"), exited)
	if rig.garden.Count("PlannerAttempts") <= before {
		t.Fatalf("an ended session did not refresh its attempt")
	}
	a, _ := m.garden.planner.attemptByID(m.garden.planner.attempt)
	if a.State != plan.StateEnded {
		t.Fatalf("attempt state = %s", a.State)
	}
	out := view(m)
	mustContain(t, "ended", out, "exited", "esc: attempts")
	mustNotContain(t, "ended", out, "n: new", "a: all")
	if m.plannerTerminalMode() {
		t.Fatal("an ended session keeps the keyboard")
	}
}

func TestPlannerSessionFollowsResize(t *testing.T) {
	m, rig := plannerEnv(t, 120, 35)
	m = plannerPress(t, requestPlan(t, openPlanner(t, m)), "ctrl+s")
	stream := rig.agents.Sessions.Streams("sess-fake-1")[0]
	m = plannerFrames(t, m, stream, status("running"))
	w, rows := m.agentAreaSize()
	if w != 120 || rows != 35-3-3-settingsSubmenuRows-plannerHeadRows-plannerTailRows {
		t.Fatalf("area = %dx%d", w, rows)
	}
	var cmd tea.Cmd
	m, cmd = send(t, m, tea.WindowSizeMsg{Width: 90, Height: 28})
	if cmd == nil {
		t.Fatal("a resize of the focused terminal did not schedule the PTY resize")
	}
	if w2, r2 := m.agentAreaSize(); w2 != 90 || r2 >= rows {
		t.Fatalf("area after resize = %dx%d", w2, r2)
	}
	mustFit(t, "resized", m)
}

func TestEscapingTheAccountChoiceStartsNothingAndReturnsToTheHub(t *testing.T) {
	m, rig := plannerEnv(t, 120, 35)
	m = requestPlan(t, openPlanner(t, m))
	if m.shellFocus != shellInput {
		t.Fatal("the account choice has a name field and must own the keyboard")
	}
	// Letters typed into the choice never reach the shell: q does not quit, ? does not open help.
	m = plannerPress(t, m, "q", "?")
	if m.garden.planner.stage != plannerChoose || m.modal != nil {
		t.Fatalf("a global key fired inside the choice: stage %d", m.garden.planner.stage)
	}
	m = plannerPress(t, m, "esc")
	if m.garden.planner.stage != plannerHub || m.garden.planner.dlg != nil || m.shellFocus != shellBody {
		t.Fatalf("esc: stage %d dlg %v focus %v", m.garden.planner.stage, m.garden.planner.dlg, m.shellFocus)
	}
	if rig.garden.Count("StartPlanner") != 0 {
		t.Fatal("backing out started a planner")
	}
}

func TestPlannerScreenGoesWhenTheIssueLeavesTheView(t *testing.T) {
	m, rig := plannerEnv(t, 120, 35)
	m = plannerPress(t, requestPlan(t, openPlanner(t, m)), "ctrl+s")
	if m.agents.view == nil {
		t.Fatal("no attachment")
	}
	// The next work read no longer holds the Issue.
	rig.garden.WorkView.Snapshot.Items = rig.garden.WorkView.Snapshot.Items[1:]
	m, cmd := m.gardenLoadWork()
	m = plannerRun(t, m, cmd)
	if m.garden.planner.stage != plannerNone || m.agents.view != nil {
		t.Fatalf("stage %d view %v", m.garden.planner.stage, m.agents.view)
	}
	if m.plannerTerminalMode() {
		t.Fatal("a vanished Issue keeps the keyboard")
	}
}
