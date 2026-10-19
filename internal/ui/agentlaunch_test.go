package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/agentterminal"
	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	git "github.com/Tiago-0liveira/bonsai/internal/git/local"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/accounts"
)

func acct(provider, name string, running int) agentsapi.Account {
	return agentsapi.Account{
		Account: agents.Account{ID: agents.AccountID(provider + ":" + name), Provider: agents.ProviderID(provider), Name: name},
		Running: running,
	}
}

// accountsEnv is an agents env whose backend has accounts, listed in a mixed
// order so the grouping is visible.
func accountsEnv(t *testing.T, opts ...Option) *agentsEnv {
	t.Helper()
	e := newAgentsEnv(t, opts...)
	e.setAccounts(acct("codex", "work", 1), acct("claude", "work", 0), acct("claude", "personal", 0))
	e.openTab(t)
	return e
}

func (e *agentsEnv) setAccounts(items ...agentsapi.Account) {
	e.fb.Accounts.Items = items
	for _, a := range items {
		e.fb.Accounts.LaunchDefaults[a.ID] = agentsapi.Defaults{Values: map[string]string{"permissions": "auto"}}
	}
}

// openLaunch opens the dialog with n and lets its backend calls finish.
func (e *agentsEnv) openLaunch(t *testing.T) {
	t.Helper()
	e.key(t, "n")
	if e.m.agents.overlay == nil || e.m.agents.overlay.launch == nil {
		t.Fatal("n must open the launch dialog")
	}
	e.run(t, e.lastCmd)
}

func (e *agentsEnv) dialog() launchDialog { return *e.m.agents.overlay.launch }

func (e *agentsEnv) overlayView() string { return ansi.Strip(e.m.View()) }

func (e *agentsEnv) sendKey(t *testing.T, typ tea.KeyType) {
	t.Helper()
	e.send(t, tea.KeyMsg{Type: typ})
	e.run(t, e.lastCmd)
}

func TestLaunchDialogListsAccountsInATableWithoutBoxes(t *testing.T) {
	for _, size := range [][2]int{{160, 40}, {80, 24}} {
		e := accountsEnv(t)
		e.m.width, e.m.height = size[0], size[1]
		e.openLaunch(t)
		view := e.overlayView()
		claude, codex := strings.Index(view, "Claude"), strings.Index(view, "Codex")
		if claude < 0 || codex < claude {
			t.Fatalf("%v: providers must be grouped Claude then Codex:\n%s", size, view)
		}
		for _, want := range []string{"New session", "feat-a", "ACCOUNT", "PROVIDER", "Session options", "Save as account defaults", "Start session", "Ctrl+S Start"} {
			if !strings.Contains(view, want) && !(size[0] < 100 && want == "5H LEFT") {
				t.Errorf("%v: missing %q:\n%s", size, want, view)
			}
		}
		if strings.Contains(view, "Add account") || strings.Contains(view, "NEW AGENT") {
			t.Errorf("%v: old sidebar chrome left:\n%s", size, view)
		}
		if lines := strings.Split(view, "\n"); len(lines) > size[1] {
			t.Errorf("%v: %d rows exceed the screen", size, len(lines))
		}
		for _, l := range strings.Split(view, "\n") {
			if ansi.StringWidth(l) > size[0] {
				t.Errorf("%v: row wider than the screen: %q", size, l)
			}
		}
	}
}

func TestLaunchDialogStartsWithTheWebIdentities(t *testing.T) {
	e := accountsEnv(t)
	e.fb.Accounts.LaunchDefaults["claude:work"] = agentsapi.Defaults{Values: map[string]string{"permissions": "auto"}}
	e.openLaunch(t)
	if got := e.dialog().Selected(); got != "claude:work" {
		t.Fatalf("first claude account expected selected, got %q", got)
	}
	// Move to the Name field, replace the default, go on to the form and pick
	// the next permissions option.
	e.sendKey(t, tea.KeyTab)
	if got := e.dialog().name.Value(); got != "work" {
		t.Errorf("default name = %q, want the profile name", got)
	}
	e.send(t, tea.KeyMsg{Type: tea.KeyCtrlU})
	e.key(t, "Auth fix")
	e.sendKey(t, tea.KeyTab)
	e.sendKey(t, tea.KeyDown) // effort
	e.sendKey(t, tea.KeyDown) // permissions
	e.sendKey(t, tea.KeyRight)

	wantCols, wantRows := e.m.agentAreaSize()
	e.sendKey(t, tea.KeyCtrlS)

	starts := e.fb.Sessions.Starts()
	if len(starts) != 1 {
		t.Fatalf("starts = %d, want 1", len(starts))
	}
	req := starts[0]
	if req.ProjectID != config.ProjectID(e.repo) || req.WorktreeID != git.ID("local", e.wtA) || req.WorkDir != e.wtA {
		t.Errorf("identities = %q %q %q", req.ProjectID, req.WorktreeID, req.WorkDir)
	}
	if req.AccountID != "claude:work" || req.Name != "Auth fix" {
		t.Errorf("account/name = %q %q", req.AccountID, req.Name)
	}
	if req.Cols != wantCols || req.Rows != wantRows || req.Cols <= 0 || req.Rows <= 0 {
		t.Errorf("size = %dx%d, want %dx%d", req.Cols, req.Rows, wantCols, wantRows)
	}
	if req.Launch.Sandbox != "danger-full-access" || req.Launch.Approval != "never" || req.Launch.FullAccess == nil || *req.Launch.FullAccess {
		t.Errorf("launch options must come from the form: %+v", req.Launch)
	}
	if len(e.fb.Accounts.Updates()) != 0 {
		t.Error("defaults must not be saved unless asked")
	}
}

func TestLaunchDialogErrorStaysInline(t *testing.T) {
	for name, cause := range map[string]error{
		"account busy":  fmt.Errorf("start: %w", agents.ErrAccountBusy),
		"provider busy": agents.ErrProviderBusy,
		"validation":    fmt.Errorf("%w: effort \x1b[31mbogus\x1b[0m", agents.ErrInvalidLaunch),
	} {
		t.Run(name, func(t *testing.T) {
			e := accountsEnv(t)
			e.fb.Sessions.StartErr = cause
			e.openLaunch(t)
			e.sendKey(t, tea.KeyCtrlS)
			if e.m.agents.overlay == nil || e.m.agents.overlay.launch == nil {
				t.Fatal("the dialog must stay open on an error")
			}
			d := e.dialog()
			if d.starting || d.err == "" || strings.ContainsRune(d.err, 0x1b) {
				t.Errorf("starting=%v err=%q", d.starting, d.err)
			}
			if view := e.overlayView(); !strings.Contains(view, d.err[:min(len(d.err), 20)]) {
				t.Errorf("the error must be drawn:\n%s", view)
			}
			// The user can fix and retry.
			e.fb.Sessions.StartErr = nil
			e.sendKey(t, tea.KeyCtrlS)
			if e.m.agents.overlay != nil {
				t.Error("a successful retry closes the dialog")
			}
		})
	}
}

func TestLaunchSuccessSelectsAndEntersTerminalMode(t *testing.T) {
	e := accountsEnv(t)
	e.openLaunch(t)
	e.sendKey(t, tea.KeyCtrlS)

	if e.m.agents.overlay != nil {
		t.Fatal("success closes the dialog")
	}
	v := e.m.agents.view
	if v == nil || v.sessionID != "fake-1" {
		t.Fatalf("the new session must be the shown one: %+v", v)
	}
	if !e.m.agentTerminalMode() || v.state != attachControlling {
		t.Errorf("terminal mode=%v state=%v, want controlling", e.m.agentTerminalMode(), v.state)
	}
	calls := e.fb.Sessions.Attaches()
	if last := calls[len(calls)-1]; last.ID != "fake-1" || last.Opts.ReadOnly {
		t.Errorf("the new session must attach for control: %+v", last)
	}
	if e.m.agents.enterAfter != "" {
		t.Error("the enter wish must be consumed")
	}
}

func TestLaunchSaveAsDefaultsSendsChangesAfterStart(t *testing.T) {
	e := accountsEnv(t)
	e.openLaunch(t)
	e.sendKey(t, tea.KeyTab)  // name
	e.sendKey(t, tea.KeyTab)  // model
	e.sendKey(t, tea.KeyDown) // effort
	e.sendKey(t, tea.KeyDown) // permissions
	e.sendKey(t, tea.KeyRight)
	e.sendKey(t, tea.KeyTab) // full access
	e.sendKey(t, tea.KeyTab) // save checkbox
	e.key(t, " ")
	if !e.dialog().save {
		t.Fatal("space must tick the box")
	}
	e.sendKey(t, tea.KeyCtrlS)

	ups := e.fb.Accounts.Updates()
	if len(ups) != 1 || ups[0].ID != "claude:work" {
		t.Fatalf("updates = %+v", ups)
	}
	if v := ups[0].Values["permissions"]; v == nil || *v != "full-access" {
		t.Errorf("changes must carry the edited field: %+v", ups[0].Values)
	}
	if len(ups[0].Values) != 1 {
		t.Errorf("only changed fields are saved: %+v", ups[0].Values)
	}
}

func TestLaunchSaveAsDefaultsSkippedWhenStartFails(t *testing.T) {
	e := accountsEnv(t)
	e.fb.Sessions.StartErr = errors.New("boom")
	e.openLaunch(t)
	e.sendKey(t, tea.KeyTab)
	e.sendKey(t, tea.KeyTab)
	e.sendKey(t, tea.KeyDown) // effort
	e.sendKey(t, tea.KeyDown) // permissions
	e.sendKey(t, tea.KeyRight)
	e.sendKey(t, tea.KeyTab) // full access
	e.sendKey(t, tea.KeyTab) // save checkbox
	e.key(t, " ")
	e.sendKey(t, tea.KeyCtrlS)
	if len(e.fb.Accounts.Updates()) != 0 {
		t.Error("a failed start must not change the account's defaults")
	}
}

func TestLaunchDialogSwitchingAccountReloadsForm(t *testing.T) {
	e := accountsEnv(t)
	e.fb.Accounts.LaunchDefaults["claude:personal"] = agentsapi.Defaults{Values: map[string]string{"permissions": "read-only"}}
	e.openLaunch(t)
	e.key(t, "j")
	e.run(t, e.lastCmd)
	d := e.dialog()
	if d.Selected() != "claude:personal" || d.form.Values()["permissions"] != "read-only" {
		t.Errorf("selected=%q values=%v", d.Selected(), d.form.Values())
	}
	if d.name.Value() != "personal" {
		t.Errorf("untouched name follows the account, got %q", d.name.Value())
	}
}

func TestLaunchDialogSignInFirst(t *testing.T) {
	e := accountsEnv(t)
	e.fb.Accounts.Infos["claude:work"] = agents.AccountInfo{Options: map[string]any{"auth_status": "logged_out"}}
	e.openLaunch(t)
	if view := e.overlayView(); !strings.Contains(view, "[ Sign in first ]") {
		t.Fatalf("an account without a login offers sign-in:\n%s", view)
	}
	e.send(t, tea.KeyMsg{Type: tea.KeyCtrlS})
	msgs := flatten(e.lastCmd)
	if len(msgs) != 1 {
		t.Fatalf("msgs = %v", msgs)
	}
	exec, ok := msgs[0].(accounts.ExecMsg)
	if !ok || !strings.Contains(strings.Join(exec.Cmd.Args, " "), "login claude:work") {
		t.Fatalf("expected the login command, got %#v", msgs[0])
	}
	if len(e.fb.Sessions.Starts()) != 0 {
		t.Error("nothing starts before signing in")
	}
}

func TestAgentExecRunsProcessThenReloadsOverlay(t *testing.T) {
	e := accountsEnv(t)
	e.key(t, "m")
	e.run(t, e.lastCmd)
	if e.m.agents.overlay == nil || e.m.agents.overlay.accounts == nil {
		t.Fatal("m opens the accounts modal")
	}
	cmd := e.fb.Accounts.LoginCommand("claude:work")
	nm, run := e.m.Update(accounts.ExecMsg{Cmd: cmd})
	e.m = nm.(Model)
	if run == nil {
		t.Fatal("ExecMsg must run the process")
	}
	if name := fmt.Sprintf("%T", run()); !strings.Contains(strings.ToLower(name), "exec") {
		t.Errorf("ExecMsg must suspend the TUI via ExecProcess, got %s", name)
	}
	seq := e.m.agents.seq
	nm, after := e.m.Update(agentExecDoneMsg{})
	e.m = nm.(Model)
	if e.m.agents.seq != seq+1 {
		t.Error("sessions must be listed again after the flow")
	}
	if e.m.agents.overlay == nil || e.m.agents.overlay.accounts == nil || after == nil {
		t.Error("the modal stays open and reloads")
	}
}

func TestQuickUsageOpensAccountsOnUsageTab(t *testing.T) {
	e := accountsEnv(t)
	e.fb.Sessions.Add(e.agent("a", "claude", "running", e.wtA, "Auth fix", 1))
	e.relist(t)
	e.key(t, "u")
	if e.m.agents.overlay == nil || e.m.agents.overlay.quick == nil {
		t.Fatal("u opens the quick usage overlay")
	}
	e.run(t, e.lastCmd)
	e.key(t, "m")
	e.run(t, e.lastCmd)
	o := e.m.agents.overlay
	if o == nil || o.accounts == nil || o.quick != nil {
		t.Fatalf("m in the overlay swaps to the accounts modal: %+v", o)
	}
	if view := e.overlayView(); !strings.Contains(view, "REMAINING CAPACITY") {
		t.Errorf("the modal opens on the Usage tab:\n%s", view)
	}
}

func TestAccountsOpensOnTheSessionsAccount(t *testing.T) {
	e := accountsEnv(t)
	e.fb.Sessions.Add(e.agent("a", "codex", "running", e.wtA, "Tests", 1))
	e.relist(t)
	e.key(t, "m")
	e.run(t, e.lastCmd)
	if view := e.overlayView(); !strings.Contains(view, "Codex / work") {
		t.Errorf("accounts must start on the selected session's account:\n%s", view)
	}
}

// countingSessions counts Stop calls on top of the fake.
type countingSessions struct {
	agentsapi.Sessions
	stops atomic.Int32
}

func (c *countingSessions) Stop(ctx context.Context, id string) error {
	c.stops.Add(1)
	return c.Sessions.Stop(ctx, id)
}

func stopEnv(t *testing.T) (*agentsEnv, *countingSessions) {
	t.Helper()
	e := newAgentsEnv(t)
	cs := &countingSessions{Sessions: e.fb.Sessions}
	e = newAgentsEnvWith(t, e, cs)
	e.fb.Sessions.Add(e.agent("a", "claude", "running", e.wtA, "Auth fix", 1))
	e.relist(t)
	e.openTab(t)
	return e, cs
}

// newAgentsEnvWith rebuilds the model of e over a wrapped Sessions.
func newAgentsEnvWith(t *testing.T, e *agentsEnv, s agentsapi.Sessions) *agentsEnv {
	t.Helper()
	be := agentsapi.Backend{Sessions: s, Accounts: e.fb.Accounts}
	wts, w, h := e.m.worktrees, e.m.width, e.m.height
	e.m = New(e.repo, &config.Config{}, &config.State{}, WithAgents(be))
	e.m.width, e.m.height, e.m.ready, e.m.worktrees = w, h, true, wts
	e.m.rebuildItems()
	e.m.list.SetSize(60, 30)
	return e
}

func TestStopConfirmCallsStopOnce(t *testing.T) {
	e, cs := stopEnv(t)
	e.key(t, "s")
	if e.m.modal == nil || e.m.pendingConfirm == nil {
		t.Fatal("s asks for confirmation")
	}
	view := ansi.Strip(e.m.modal.View())
	if !strings.Contains(view, "Stop Auth fix?") || !strings.Contains(view, "The agent process ends.") {
		t.Errorf("confirm text:\n%s", view)
	}
	if cs.stops.Load() != 0 {
		t.Fatal("nothing stops before the answer")
	}
	e.sendKey(t, tea.KeyUp) // from "No" to "Yes"
	e.sendKey(t, tea.KeyEnter)
	if got := cs.stops.Load(); got != 1 {
		t.Fatalf("Stop calls = %d, want 1", got)
	}
	if e.m.modal != nil || e.m.pendingConfirm != nil {
		t.Error("the confirmation closes")
	}
	if got := e.m.agents.sessions[0].State; got != "exited" {
		t.Errorf("sessions are listed again after stopping: %q", got)
	}
}

func TestStopDeclinedDoesNothing(t *testing.T) {
	e, cs := stopEnv(t)
	e.key(t, "s")
	e.sendKey(t, tea.KeyEnter) // default answer is No
	e.key(t, "s")
	e.sendKey(t, tea.KeyEsc)
	if cs.stops.Load() != 0 || e.m.modal != nil || e.m.pendingConfirm != nil {
		t.Errorf("stops=%d modal=%v", cs.stops.Load(), e.m.modal != nil)
	}
}

func TestStopFromTerminalModeLeavesModeKeepsStream(t *testing.T) {
	e, st := terminalEnv(t)
	cs := &countingSessions{Sessions: e.fb.Sessions}
	e.m.agents.api.Sessions = cs
	e.typeKeys(t, ctrlBracket, runes("s"))
	if e.m.modal == nil || e.m.agentTerminalMode() {
		t.Fatal("ctrl+] s asks to stop and leaves terminal mode")
	}
	if st.Closed() {
		t.Error("the stream stays attached under the prompt")
	}
	e.sendKey(t, tea.KeyUp)
	e.sendKey(t, tea.KeyEnter)
	if cs.stops.Load() != 1 {
		t.Errorf("Stop calls = %d, want 1", cs.stops.Load())
	}
}

func TestStopWithoutRunningAgentSaysSo(t *testing.T) {
	e := accountsEnv(t)
	e.key(t, "s")
	if e.m.modal != nil || !strings.Contains(e.m.status, "no running agent") {
		t.Errorf("status=%q modal=%v", e.m.status, e.m.modal != nil)
	}
}

func TestPaletteOpensAgentOverlays(t *testing.T) {
	e := accountsEnv(t)
	byLabel := map[string]paletteCmd{}
	for _, c := range e.m.paletteCommands() {
		byLabel[c.label] = c
	}
	for label, check := range map[string]func(*agentOverlay) bool{
		"Agents: new agent": func(o *agentOverlay) bool { return o.launch != nil },
		"Agents: accounts":  func(o *agentOverlay) bool { return o.accounts != nil },
		"Agents: usage":     func(o *agentOverlay) bool { return o.quick != nil },
	} {
		c, ok := byLabel[label]
		if !ok {
			t.Fatalf("palette lacks %q", label)
		}
		nm, _ := c.run(e.m)
		m := nm.(Model)
		if m.agents.overlay == nil || !check(m.agents.overlay) {
			t.Errorf("%q: overlay = %+v", label, m.agents.overlay)
		}
	}
}

func TestAgentOverlayOwnsKeysAndStreamKeepsPumping(t *testing.T) {
	e := liveEnv(t)
	st := e.attached(t)
	e.fb.Accounts.Items = []agentsapi.Account{acct("claude", "work", 0)}
	e.fb.Accounts.LaunchDefaults["claude:work"] = agentsapi.Defaults{}
	e.key(t, "u")
	e.run(t, e.lastCmd)
	e.key(t, "q") // would quit the TUI if it reached the globals
	if e.m.agents.overlay == nil {
		t.Fatal("q must not close or quit through the overlay")
	}
	e.frames(t, out("behind the overlay"))
	if !strings.Contains(ansi.Strip(e.m.agents.view.term.View()), "behind the overlay") {
		t.Error("the live view keeps rendering under the overlay")
	}
	if st.Closed() {
		t.Error("the overlay must not touch the stream")
	}
	if view := e.overlayView(); strings.Contains(view, "behind the overlay") {
		t.Error("the agent dialog must cover the terminal area")
	}
	e.sendKey(t, tea.KeyEsc)
	if e.m.agents.overlay != nil || st.Closed() {
		t.Error("closing the overlay returns to the tab with the stream attached")
	}
}

func TestOverlaysWithoutAccountsBackendSayWhy(t *testing.T) {
	e := newAgentsEnv(t)
	e.m.agents.api.Accounts = nil
	e.openTab(t)
	for _, k := range []string{"n", "m", "u"} {
		e.m.status = ""
		e.key(t, k)
		if e.m.agents.overlay != nil || e.m.status != accountsUnavailable {
			t.Errorf("%s: overlay=%v status=%q", k, e.m.agents.overlay != nil, e.m.status)
		}
	}
}

func TestStartedElsewhereIsNotEntered(t *testing.T) {
	// A start answer arriving after the dialog was closed must not hijack the
	// view: the session just appears in the list.
	e := accountsEnv(t)
	e.openLaunch(t)
	id := e.dialog().id
	e.sendKey(t, tea.KeyEsc)
	sess := agentsapi.Session{Summary: agentterminal.Summary{ID: "late", State: "running"}, WorkDir: e.wtA}
	e.send(t, launchStartedMsg{dlg: id, session: sess, workDir: e.wtA})
	if e.m.agents.enterAfter != "" || e.m.agents.overlay != nil {
		t.Error("a start for a closed dialog must not enter terminal mode")
	}
}

// --- D4 follow-ups ---

func TestLargePasteIsSplitIntoInputLimitChunks(t *testing.T) {
	e, st := terminalEnv(t)
	big := strings.Repeat("x", 2*agentterminal.InputLimit+100)
	v := e.m.agents.view
	cmd, dropped := v.queueInput([]byte(big))
	if dropped {
		t.Fatal("a large paste fits the queue")
	}
	e.run(t, cmd)
	var total int
	for _, p := range st.Inputs() {
		if len(p) > agentterminal.InputLimit {
			t.Fatalf("chunk of %d bytes exceeds the holder's limit", len(p))
		}
		total += len(p)
	}
	if total != len(big) || len(st.Inputs()) != 3 {
		t.Errorf("sent %d bytes in %d chunks, want %d in 3", total, len(st.Inputs()), len(big))
	}
}

func TestInputBeyondTheQueueIsReported(t *testing.T) {
	e, st := terminalEnv(t)
	v := e.m.agents.view
	v.sending = true // a stalled write: nothing drains
	for i := 0; i < maxInputQueue; i++ {
		if _, dropped := v.queueInput([]byte("a")); dropped {
			t.Fatalf("chunk %d dropped early", i)
		}
	}
	nm, _ := e.m.Update(agentInputMsg{term: v.term, data: []byte("b")})
	e.m = nm.(Model)
	if e.m.status != inputDroppedNote {
		t.Errorf("status = %q, want the dropped-input note", e.m.status)
	}
	if len(st.Inputs()) != 0 {
		t.Error("nothing is sent while the write is stalled")
	}
}

func TestTerminalHintSaysConnectingWhileReattaching(t *testing.T) {
	e, _ := terminalEnv(t)
	v := e.m.agents.view
	e.m.reattachAgent(v, false)
	if !v.control || v.stream != nil {
		t.Fatal("setup: controlling with no stream")
	}
	if hint := ansi.Strip(e.m.terminalHint()); !strings.Contains(hint, "Connecting") {
		t.Errorf("hint = %q", hint)
	}
}

func TestEnterWishClearedWhenUserMovesAway(t *testing.T) {
	for name, move := range map[string]func(e *agentsEnv){
		"worktree": func(e *agentsEnv) { e.selectWT(e.wtB) },
		"session":  func(e *agentsEnv) { e.m.agents.selected[cleanPath(e.wtA)] = "other" },
	} {
		t.Run(name, func(t *testing.T) {
			e := accountsEnv(t)
			e.fb.Sessions.Add(e.agent("other", "claude", "running", e.wtA, "Other", 1))
			e.openLaunch(t)
			e.sendKey(t, tea.KeyCtrlS)
			e.send(t, tea.WindowSizeMsg{Width: 100, Height: 30}) // any update
			if e.m.agents.overlay != nil {
				t.Fatal("setup: dialog should be closed")
			}
			// Re-arm as if the attach were still pending.
			e.m.agents.enterAfter, e.m.agents.enterPath = "fake-1", cleanPath(e.wtA)
			e.m.agents.selected[cleanPath(e.wtA)] = "fake-1"
			move(e)
			e.send(t, tea.WindowSizeMsg{Width: 100, Height: 30})
			if e.m.agents.enterAfter != "" {
				t.Error("enter wish must be cleared after moving away")
			}
		})
	}
}

func TestBusyHintRespectsConcurrentSameAccount(t *testing.T) {
	e := newAgentsEnv(t)
	a := acct("codex", "work", 1)
	a.ConcurrentSameAccount = true
	e.setAccounts(a)
	e.openTab(t)
	e.openLaunch(t)
	if strings.Contains(e.overlayView(), "busy") {
		t.Error("a provider allowing concurrent sessions is not busy")
	}
}

func TestLateStartShowsStatus(t *testing.T) {
	e := accountsEnv(t)
	e.send(t, launchStartedMsg{dlg: 99, session: agentsapi.Session{Summary: agentterminal.Summary{ID: "x", Name: "Late"}}, workDir: e.wtA})
	if e.m.status != "agents: started Late" {
		t.Errorf("status = %q", e.m.status)
	}
}

func TestLaunchModelAndEffortOverridesStayWithSession(t *testing.T) {
	e := accountsEnv(t)
	e.fb.Accounts.LaunchDefaults["claude:work"] = agentsapi.Defaults{
		Values: map[string]string{"model": "small", "effort": "low", "permissions": "auto"},
		Models: []agents.ModelOption{
			{ID: "small", Label: "Small", Efforts: []string{"low", "high"}},
			{ID: "large", Label: "Large", Efforts: []string{"low", "high"}},
		},
	}
	e.openLaunch(t)
	if view := e.overlayView(); !strings.Contains(view, "Model") || !strings.Contains(view, "Effort") {
		t.Fatalf("launch overrides must be visible:\n%s", view)
	}
	e.sendKey(t, tea.KeyTab) // name
	e.sendKey(t, tea.KeyTab) // model
	e.sendKey(t, tea.KeyRight)
	e.sendKey(t, tea.KeyTab) // effort, rather than leaving the form
	e.sendKey(t, tea.KeyEnter)
	e.sendKey(t, tea.KeyDown)
	e.sendKey(t, tea.KeyEnter)
	if got := e.dialog().form.Values(); got["model"] != "large" || got["effort"] != "high" {
		t.Fatalf("edited launch values = %v", got)
	}
	e.sendKey(t, tea.KeyCtrlS)
	starts := e.fb.Sessions.Starts()
	if len(starts) != 1 || starts[0].Launch.Model != "large" || starts[0].Launch.Effort != "high" {
		t.Fatalf("session overrides = %+v", starts)
	}
	if got := e.fb.Accounts.Updates(); len(got) != 0 {
		t.Fatalf("session overrides saved profile defaults: %+v", got)
	}
	defaults := e.fb.Accounts.LaunchDefaults["claude:work"].Values
	if defaults["model"] != "small" || defaults["effort"] != "low" {
		t.Fatalf("profile defaults changed: %v", defaults)
	}
}

func TestLaunchModelChangeRevalidatesEffort(t *testing.T) {
	for _, noEffort := range []bool{false, true} {
		t.Run(fmt.Sprintf("no-effort=%v", noEffort), func(t *testing.T) {
			e := accountsEnv(t)
			e.fb.Accounts.LaunchDefaults["claude:work"] = agentsapi.Defaults{
				Values: map[string]string{"model": "small", "effort": "high"},
				Models: []agents.ModelOption{
					{ID: "small", Efforts: []string{"low", "high"}},
					{ID: "large", Efforts: []string{"low"}, DefaultEffort: "low", NoEffort: noEffort},
				},
			}
			e.openLaunch(t)
			e.sendKey(t, tea.KeyTab)
			e.sendKey(t, tea.KeyTab)
			e.sendKey(t, tea.KeyRight)
			want := "low"
			if noEffort {
				want = ""
			}
			if got := e.dialog().form.Values()["effort"]; got != want {
				t.Fatalf("effort = %q, want %q", got, want)
			}
			if noEffort && strings.Contains(e.overlayView(), "Effort") {
				t.Fatal("models without effort must hide the effort control")
			}
		})
	}
}

func TestAgentDialogsStayInsideTabOnOpenAndResize(t *testing.T) {
	for _, action := range []string{"n", "m", "u", "a", "s"} {
		for _, size := range [][2]int{{240, 65}, {160, 40}, {100, 30}, {80, 24}, {58, 20}} {
			t.Run(fmt.Sprintf("%s-%dx%d", action, size[0], size[1]), func(t *testing.T) {
				e := accountsEnv(t)
				e.fb.Sessions.Add(e.agent("a", "claude", "running", e.wtA, "Auth fix", 1))
				e.relist(t)
				e.send(t, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
				before := strings.Split(ansi.Strip(e.m.View()), "\n")
				e.key(t, action)
				e.run(t, e.lastCmd)
				after := strings.Split(ansi.Strip(e.m.View()), "\n")
				leftW, _, _ := e.m.dims()
				if len(after) != size[1] {
					t.Fatalf("view has %d rows, want %d", len(after), size[1])
				}
				for i, line := range after {
					if ansi.StringWidth(line) > size[0] {
						t.Fatalf("row %d exceeds screen: %q", i, line)
					}
					if i < len(after)-1 && ansi.Cut(line, 0, leftW) != ansi.Cut(before[i], 0, leftW) {
						t.Fatalf("dialog replaced worktree pane at row %d:\n%s", i, e.m.View())
					}
				}
				if after[1] != before[1] {
					t.Fatalf("dialog replaced tab strip:\n%s", e.m.View())
				}
				w, h := e.m.agentPaneSize()
				paneView := ansi.Strip(e.m.renderAgentsPane(w, h))
				if strings.ContainsAny(paneView, "╭╮╰╯├┤┴") {
					t.Fatalf("agent view has modal borders:\n%s", paneView)
				}
				if action == "u" && !strings.Contains(strings.Split(paneView, "\n")[h-1], "Refresh") {
					t.Fatalf("usage view must reach the bottom of the pane:\n%s", paneView)
				}
				e.send(t, tea.WindowSizeMsg{Width: 80, Height: 24})
				w, h = e.m.agentPaneSize()
				pane := strings.Split(e.m.renderAgentsPane(w, h), "\n")
				if len(pane) != h {
					t.Fatalf("resized pane has %d rows, want %d", len(pane), h)
				}
				for _, row := range pane {
					if ansi.StringWidth(row) > w {
						t.Fatalf("resized dialog exceeds pane: %q", row)
					}
				}
			})
		}
	}
}
