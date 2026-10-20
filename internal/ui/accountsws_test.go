package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi/fake"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/accounts"
	"github.com/Tiago-0liveira/bonsai/internal/ui/usagestore"
)

func wsAccount(id, provider, name, auth string) agentsapi.Account {
	a := agentsapi.Account{Account: agents.Account{ID: agents.AccountID(id), Provider: agents.ProviderID(provider), Name: name}}
	a.Info = agents.AccountInfo{Options: map[string]any{"auth_status": auth}}
	return a
}

// accountsFake is three profiles over two providers with real-looking usage.
func accountsFake() *fake.Accounts {
	five, week := 0.66, 0.39
	reset := time.Now().Add(3 * time.Hour)
	limits := agents.UsageHeadline{
		Primary:   &agents.UsageLimit{ID: "five_hour", Window: "5h", RemainingFraction: &five, ResetsAt: &reset},
		Secondary: &agents.UsageLimit{ID: "weekly", Window: "weekly", RemainingFraction: &week},
	}
	usage := agentsapi.Usage{Headline: limits, FetchedAt: time.Now()}
	def := agentsapi.Defaults{Fields: fake.SampleLaunchFields(), Values: map[string]string{}}
	return &fake.Accounts{
		Items: []agentsapi.Account{
			wsAccount("c1", "claude", "work", "logged_in"),
			wsAccount("x1", "codex", "personal", "logged_in"),
			wsAccount("x2", "codex", "old", "logged_out"),
		},
		Infos: map[agents.AccountID]agents.AccountInfo{
			"c1": {Identity: "w@example.com", Options: map[string]any{"auth_status": "logged_in"}},
			"x1": {Identity: "me@example.com", Options: map[string]any{"auth_status": "logged_in"}},
			"x2": {Options: map[string]any{"auth_status": "logged_out"}, Warnings: []string{"Not logged in"}},
		},
		Usages:         map[agents.AccountID]agentsapi.Usage{"c1": usage, "x1": usage, "x2": usage},
		LaunchDefaults: map[agents.AccountID]agentsapi.Defaults{"c1": def, "x1": def, "x2": def},
	}
}

// accountsModel builds the redesign shell with a fake backend; sessions is
// the fake Sessions (nil leaves the backend without any).
func accountsModel(t *testing.T, api agentsapi.Accounts, sessions agentsapi.Sessions, w, h int) Model {
	t.Helper()
	m := shellModel(t, config.Prefs{}, nil)
	WithAgents(agentsapi.Backend{Sessions: sessions, Accounts: api})(&m)
	m.acct = newAccountsState(m.agents.api.Accounts, false, m.agents.usage)
	return sized(t, m, w, h)
}

// drainAll runs cmd and feeds every message back, expanding batches. Commands
// are awaited up to a generous bound and a stall fails the test; the one
// cosmetic exception is the timer that clears "Saved", recognized by "Saved"
// being on screen. An ExecProcess command is returned as a message, not run.
func drainAll(t *testing.T, m Model, cmd tea.Cmd) (Model, []tea.Msg) {
	t.Helper()
	var seen []tea.Msg
	queue := []tea.Cmd{cmd}
	for len(queue) > 0 {
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		done := make(chan tea.Msg, 1)
		go func() { done <- c() }()
		var msg tea.Msg
		select {
		case msg = <-done:
		case <-time.After(2 * time.Second):
			if !strings.Contains(view(m), "Saved") {
				t.Fatalf("command stalled and is not the Saved timer:\n%s", view(m))
			}
			continue
		}
		if batch, ok := msg.(tea.BatchMsg); ok {
			queue = append(queue, batch...)
			continue
		}
		seen = append(seen, msg)
		var next tea.Cmd
		m, next = send(t, m, msg)
		queue = append(queue, next)
	}
	return m, seen
}

func pressAll(t *testing.T, m Model, keys ...string) Model {
	t.Helper()
	for _, k := range keys {
		var msg tea.KeyMsg
		switch k {
		case "enter", "esc", "tab", "left", "right", "up", "down":
			msg = keyOf(k)
		case "ctrl+s":
			msg = tea.KeyMsg{Type: tea.KeyCtrlS}
		case "shift+tab":
			msg = tea.KeyMsg{Type: tea.KeyShiftTab}
		default:
			msg = runeKey(k)
		}
		var cmd tea.Cmd
		m, cmd = send(t, m, msg)
		m, _ = drainAll(t, m, cmd)
	}
	return m
}

func goAccounts(t *testing.T, m Model) Model {
	t.Helper()
	m, cmd := m.selectWorkspace(wsAccounts)
	m, _ = drainAll(t, m, cmd)
	return pressAll(t, m, "enter")
}

func view(m Model) string { return ansi.Strip(m.View()) }

func TestAccountsWorkspaceShowsEverySelectableProfile(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {120, 35}} {
		m := goAccounts(t, accountsModel(t, accountsFake(), nil, size[0], size[1]))
		out := view(m)
		for _, want := range []string{"Claude", "Codex", "work", "personal", "old"} {
			if !strings.Contains(out, want) {
				t.Errorf("%dx%d lacks %q:\n%s", size[0], size[1], want, out)
			}
		}
		for i, line := range strings.Split(m.View(), "\n") {
			if w := ansi.StringWidth(line); w != size[0] {
				t.Errorf("%dx%d line %d width %d", size[0], size[1], i, w)
			}
		}
		if n := strings.Count(m.View(), "\n") + 1; n != size[1] {
			t.Errorf("%dx%d height %d", size[0], size[1], n)
		}
		seen := map[agents.AccountID]bool{m.acct.model.Selected(): true}
		for range 2 {
			m = pressAll(t, m, "down")
			seen[m.acct.model.Selected()] = true
		}
		if len(seen) != 3 {
			t.Errorf("%dx%d: down reached only %v", size[0], size[1], seen)
		}
	}
}

func TestAccountsAvailableWithoutSessionsSupport(t *testing.T) {
	for name, sessions := range map[string]agentsapi.Sessions{
		"nil sessions":         nil,
		"unsupported sessions": &fake.Sessions{Supports: false},
	} {
		m := goAccounts(t, accountsModel(t, accountsFake(), sessions, 100, 30))
		if !m.acct.present || m.agents.api.Accounts == nil {
			t.Fatalf("%s: accounts dropped", name)
		}
		if out := view(m); !strings.Contains(out, "personal") || strings.Contains(out, "unavailable") {
			t.Errorf("%s: accounts not usable:\n%s", name, out)
		}
	}
}

func TestAccountsWithoutBackendExplains(t *testing.T) {
	m := goAccounts(t, accountsModel(t, nil, nil, 80, 24))
	if out := view(m); !strings.Contains(out, "Accounts are unavailable") {
		t.Errorf("no reason shown:\n%s", out)
	}
	if m = pressAll(t, m, "esc"); m.shellFocus != shellNav {
		t.Errorf("esc did not return to navigation: %s", m.shellFocus)
	}
}

func TestAccountsFocusAndEscReturn(t *testing.T) {
	m := goAccounts(t, accountsModel(t, accountsFake(), nil, 100, 30))
	if m.shellFocus != shellBody || !m.acct.model.InProfiles() {
		t.Fatalf("enter did not open the profile list: %s", m.shellFocus)
	}
	if out := view(m); !strings.Contains(out, "FOCUS: Accounts / Profiles") {
		t.Errorf("focus label:\n%s", out)
	}
	m = pressAll(t, m, "enter")
	if m.acct.model.InProfiles() || !strings.Contains(view(m), "FOCUS: Accounts / Overview") {
		t.Errorf("enter did not move into the inspector:\n%s", view(m))
	}
	m = pressAll(t, m, "tab")
	if m.acct.model.Section() != "Usage" {
		t.Errorf("tab section = %s", m.acct.model.Section())
	}
	m = pressAll(t, m, "esc")
	if !m.acct.model.InProfiles() || m.shellFocus != shellBody {
		t.Errorf("esc from the inspector must return to the list")
	}
	m = pressAll(t, m, "esc")
	if m.shellFocus != shellNav || m.workspace != wsAccounts {
		t.Errorf("esc from the list must return to workspace navigation, got %s in %s", m.shellFocus, m.workspace.name())
	}
}

func TestAccountsStateSurvivesWorkspaceSwitch(t *testing.T) {
	api := accountsFake()
	m := goAccounts(t, accountsModel(t, api, nil, 100, 30))
	m = pressAll(t, m, "down", "2") // second account, Usage section
	id, section := m.acct.model.Selected(), m.acct.model.Section()
	if id == "" || section != "Usage" {
		t.Fatalf("setup: %q %s", id, section)
	}
	m = pressAll(t, m, "esc")
	m, cmd := m.selectWorkspace(wsGarden)
	m, _ = drainAll(t, m, cmd)
	m, cmd = m.selectWorkspace(wsAccounts)
	m, _ = drainAll(t, m, cmd)
	if got := m.acct.model.Selected(); got != id {
		t.Errorf("selection %q after round trip, want %q", got, id)
	}
	if got := m.acct.model.Section(); got != section {
		t.Errorf("section %s after round trip, want %s", got, section)
	}
	m = pressAll(t, m, "r")
	m = pressAll(t, m, "esc")
	if got := m.acct.model.Selected(); got != id {
		t.Errorf("reload moved the selection to %q", got)
	}
}

func TestAccountsReloadKeepsSelection(t *testing.T) {
	m := goAccounts(t, accountsModel(t, accountsFake(), nil, 100, 30))
	m = pressAll(t, m, "down", "down")
	want := m.acct.model.Selected()
	m, cmd := send(t, m, accounts.ReloadMsg{})
	m, _ = drainAll(t, m, cmd)
	if got := m.acct.model.Selected(); got != want {
		t.Errorf("selection %q after reload, want %q", got, want)
	}
}

func TestAccountsListErrorKeepsLastGoodAndOffersRetry(t *testing.T) {
	api := accountsFake()
	m := goAccounts(t, accountsModel(t, api, nil, 100, 30))
	api.ListErr = errors.New("boom")
	m, cmd := send(t, m, accounts.ReloadMsg{})
	m, _ = drainAll(t, m, cmd)
	out := view(m)
	if !strings.Contains(out, "Couldn't refresh accounts") || !strings.Contains(out, "personal") {
		t.Errorf("failed refresh must keep the last list and say so:\n%s", out)
	}

	empty := &fake.Accounts{ListErr: errors.New("boom")}
	m = goAccounts(t, accountsModel(t, empty, nil, 80, 24))
	if out := view(m); !strings.Contains(out, "Couldn't load accounts") || !strings.Contains(out, "r") {
		t.Errorf("failed first read:\n%s", out)
	}
	empty.ListErr = nil
	m = pressAll(t, m, "r")
	if out := view(m); !strings.Contains(out, "No accounts yet") {
		t.Errorf("retry result:\n%s", out)
	}
}

func TestAccountsEmptyShowsReasonNotAddForm(t *testing.T) {
	m := goAccounts(t, accountsModel(t, &fake.Accounts{}, nil, 80, 24))
	out := view(m)
	if !strings.Contains(out, "No accounts yet") || strings.Contains(out, "ADD ACCOUNT") {
		t.Errorf("empty state:\n%s", out)
	}
	m = pressAll(t, m, "n")
	if !strings.Contains(view(m), "ADD ACCOUNT") {
		t.Error("n must open the add form")
	}
	if m = pressAll(t, m, "esc"); !strings.Contains(view(m), "No accounts yet") || m.shellFocus != shellBody {
		t.Errorf("esc from the add form must return to the empty state:\n%s", view(m))
	}
}

func TestAccountsStatesHaveReasons(t *testing.T) {
	api := accountsFake()
	m := goAccounts(t, accountsModel(t, api, nil, 120, 35))
	m = pressAll(t, m, "down", "down", "enter") // x2: logged out
	out := view(m)
	for _, want := range []string{"Sign-in required", "Not logged in"} {
		if !strings.Contains(out, want) {
			t.Errorf("signed-out account lacks %q:\n%s", want, out)
		}
	}
	// Usage with nothing stored keeps the reason and a way out.
	failing := accountsFake()
	failing.UsageErr = errors.New("Not signed in")
	f := goAccounts(t, accountsModel(t, failing, nil, 120, 35))
	f = pressAll(t, f, "down", "down", "enter", "2")
	out = view(f)
	for _, want := range []string{"Usage unavailable", "Not signed in", "Sign in again from Overview"} {
		if !strings.Contains(out, want) {
			t.Errorf("usage failure lacks %q:\n%s", want, out)
		}
	}
	// Unknown sign-in state is named, never shown as connected or signed out.
	api.Infos["c1"] = agents.AccountInfo{}
	m = pressAll(t, m, "1", "esc", "up", "up", "enter")
	if out := view(m); !strings.Contains(out, "Sign-in state unknown") {
		t.Errorf("unknown auth:\n%s", out)
	}
}

func TestAccountsUsageLabelsRemainingAndNeverInverts(t *testing.T) {
	m := goAccounts(t, accountsModel(t, accountsFake(), nil, 120, 35))
	m = pressAll(t, m, "down", "enter", "2")
	out := view(m)
	for _, want := range []string{"REMAINING CAPACITY", "66% left", "39% left"} {
		if !strings.Contains(out, want) {
			t.Errorf("usage lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "used") || strings.Contains(out, "34%") {
		t.Errorf("usage must not show an inverted or unlabeled metric:\n%s", out)
	}
}

func TestAccountsUsageGoesThroughSharedStore(t *testing.T) {
	api := accountsFake()
	m := goAccounts(t, accountsModel(t, api, nil, 100, 30))
	if got := len(api.Fetches()); got != 1 {
		t.Errorf("opening fetched usage %d times, want only the selected account", got)
	}
	// The component reports applied results; the root store follows.
	if !m.agents.usage[m.acct.model.Selected()].Have {
		t.Error("shared usage store did not follow the workspace")
	}
	m = pressAll(t, m, "enter", "2", "r")
	if got := len(api.UsageCalls()); got != 2 || !api.UsageCalls()[1].Refresh {
		t.Errorf("usage calls %v, want a load then one explicit refresh", api.UsageCalls())
	}
}

func TestAccountsUsageStaleShowsNote(t *testing.T) {
	api := accountsFake()
	m := goAccounts(t, accountsModel(t, api, nil, 120, 35))
	m = pressAll(t, m, "enter", "2")
	api.UsageErr = errors.New("provider down")
	m, cmd := send(t, m, usagestore.Result{Account: m.acct.model.Selected(), Err: errors.New("provider down")})
	m, _ = drainAll(t, m, cmd)
	m = pressAll(t, m, "r")
	if out := view(m); !strings.Contains(out, "Stale") || !strings.Contains(out, "66% left") {
		t.Errorf("stale data must stay visible and labeled:\n%s", out)
	}
}

func TestAccountsDefaultsSaveOnlyChangesAndProtectDraft(t *testing.T) {
	api := accountsFake()
	m := goAccounts(t, accountsModel(t, api, nil, 120, 35))
	m = pressAll(t, m, "enter", "3")
	m = pressAll(t, m, "enter", "down", "enter") // open Permissions, pick the next option
	if !m.acct.model.Dirty() {
		t.Fatalf("edit did not make a draft:\n%s", view(m))
	}
	// Section switches keep the draft; a profile switch asks first.
	m = pressAll(t, m, "tab", "tab", "tab")
	if !m.acct.model.Dirty() {
		t.Error("section switch dropped the draft")
	}
	m = pressAll(t, m, "esc")
	first := m.acct.model.Selected()
	m = pressAll(t, m, "down")
	if m.acct.model.Selected() != first || !strings.Contains(view(m), "Discard") {
		t.Errorf("profile switch with a draft must ask:\n%s", view(m))
	}
	if m.currentShellFocus() != shellInput {
		t.Errorf("a confirmation must own the keyboard, got %s", m.currentShellFocus())
	}
	m = pressAll(t, m, "esc") // keep editing
	if !m.acct.model.Dirty() {
		t.Error("cancel dropped the draft")
	}
	// Leaving the workspace keeps the draft on the model.
	m = pressAll(t, m, "esc")
	m, _ = m.selectWorkspace(wsGarden)
	if !m.acct.model.Dirty() {
		t.Error("workspace switch dropped the draft")
	}
	m, cmd := m.openAccounts(accounts.TabDefaults)
	m, _ = drainAll(t, m, cmd)
	m = pressAll(t, m, "enter", "ctrl+s")
	ups := api.Updates()
	if len(ups) != 1 || ups[0].ID != first || len(ups[0].Values) != 1 {
		t.Errorf("save sent %+v, want one explicit change", ups)
	}
}

func TestAccountsDefaultsSaveFailureIsShown(t *testing.T) {
	api := accountsFake()
	api.UpdateErr = errors.New("disk full")
	m := goAccounts(t, accountsModel(t, api, nil, 120, 35))
	m = pressAll(t, m, "enter", "3", "enter", "down", "enter", "ctrl+s")
	if out := view(m); !strings.Contains(out, "Couldn't save") || !m.acct.model.Dirty() {
		t.Errorf("save failure must show and keep the draft:\n%s", out)
	}
}

func TestAccountsDefaultsNoFieldsExplains(t *testing.T) {
	api := accountsFake()
	api.LaunchDefaults["c1"] = agentsapi.Defaults{Values: map[string]string{}}
	m := goAccounts(t, accountsModel(t, api, nil, 100, 30))
	m = pressAll(t, m, "enter", "3")
	if out := view(m); !strings.Contains(out, "No editable defaults") {
		t.Errorf("provider without fields:\n%s", out)
	}
}

func TestAccountsLoginAndAddRunInTerminalThenReload(t *testing.T) {
	api := accountsFake()
	m := goAccounts(t, accountsModel(t, api, nil, 100, 30))
	m = pressAll(t, m, "enter") // Overview, Sign in again focused
	m, cmd := send(t, m, keyOf("enter"))
	var exec accounts.ExecMsg
	if c := cmd; c != nil {
		msg := c()
		var ok bool
		if exec, ok = msg.(accounts.ExecMsg); !ok {
			t.Fatalf("sign in produced %T", msg)
		}
	}
	nm, run := send(t, m, exec)
	if run == nil {
		t.Fatal("ExecMsg must suspend the TUI through a command")
	}
	_ = nm
	// When the terminal flow ends the list reloads and failures are surfaced.
	m, cmd = send(t, m, agentExecDoneMsg{err: errors.New("exit status 1")})
	if !strings.Contains(m.status, "accounts:") || cmd == nil {
		t.Errorf("status %q, cmd %v", m.status, cmd != nil)
	}
}

func TestAccountsRemoveConfirmsAndBlocksRunningSessions(t *testing.T) {
	api := accountsFake()
	api.Items[0].Running = 2
	m := goAccounts(t, accountsModel(t, api, nil, 100, 30))
	m = pressAll(t, m, "enter", "right", "enter") // Remove account
	if out := view(m); !strings.Contains(out, "Stop 2 running sessions first") || len(api.Removed()) != 0 {
		t.Errorf("running sessions must block removal:\n%s", out)
	}
	m = pressAll(t, m, "esc", "down", "enter", "right", "enter")
	if out := view(m); !strings.Contains(strings.ToLower(out), "remove account") || len(api.Removed()) != 0 {
		t.Errorf("removal must ask first:\n%s", out)
	}
	m = pressAll(t, m, "right", "enter")
	if got := api.Removed(); len(got) != 1 || got[0] != "x1" {
		t.Errorf("removed %v", got)
	}
}

func TestAccountsNeverStopSessions(t *testing.T) {
	sess := &fake.Sessions{Supports: true}
	var live agentsapi.Session
	live.ID, live.State = "s1", "running"
	sess.Add(live)
	m := goAccounts(t, accountsModel(t, accountsFake(), sess, 100, 30))
	m = pressAll(t, m, "esc", "right", "left")
	m, _ = m.selectWorkspace(wsSettings)
	if got, _ := sess.List(context.Background()); len(got) != 1 || got[0].State != "running" {
		t.Errorf("a live session changed: %+v", got)
	}
}

func TestSettingsAccountDefaultsOpensAccounts(t *testing.T) {
	m := accountsModel(t, accountsFake(), nil, 100, 30)
	m = openSettings(t, m, catAgents, 0, 0)
	if f, _ := m.curField(); f.kind != kindLink {
		t.Fatalf("field = %+v", f)
	}
	m, cmd := send(t, m, keyOf("enter"))
	m, _ = drainAll(t, m, cmd)
	if m.workspace != wsAccounts || m.shellFocus != shellBody || m.acct.model.Section() != "Defaults" {
		t.Errorf("ws=%s focus=%s section=%s", m.workspace.name(), m.shellFocus, m.acct.model.Section())
	}
	if !strings.Contains(view(m), "Account defaults") && !strings.Contains(view(m), "Provider default") {
		t.Errorf("defaults not shown:\n%s", view(m))
	}
}

func TestAgentAccountsShortcutOpensWorkspace(t *testing.T) {
	m := accountsModel(t, accountsFake(), nil, 100, 30)
	m, cmd := send(t, m, runeKey("m"))
	m, _ = drainAll(t, m, cmd)
	if m.workspace != wsAccounts || m.shellFocus != shellBody {
		t.Errorf("ws=%s focus=%s", m.workspace.name(), m.shellFocus)
	}
	if len(m.acct.model.Accounts()) != 3 {
		t.Error("the workspace did not load")
	}
}

func TestAccountsTypingNeverQuitsOrJumps(t *testing.T) {
	m := goAccounts(t, accountsModel(t, accountsFake(), nil, 100, 30))
	m = pressAll(t, m, "n", "enter") // add form, name stage
	if m.currentShellFocus() != shellInput {
		t.Fatalf("focus = %s", m.currentShellFocus())
	}
	for _, k := range []string{"q", "?", "m", ","} {
		var cmd tea.Cmd
		m, cmd = send(t, m, runeKey(k))
		if cmd != nil && fmt.Sprintf("%T", cmd()) == "tea.QuitMsg" {
			t.Errorf("%q quit while typing", k)
		}
		if m.modal != nil || m.workspace != wsAccounts {
			t.Errorf("%q left the input (modal=%v ws=%s)", k, m.modal != nil, m.workspace.name())
		}
	}
}

func TestAccountsHintsAndHelp(t *testing.T) {
	m := goAccounts(t, accountsModel(t, accountsFake(), nil, 120, 35))
	out := view(m)
	for _, want := range []string{"account", "inspect", "section", "add", "workspaces"} {
		if !strings.Contains(out, want) {
			t.Errorf("profile hints lack %q:\n%s", want, out)
		}
	}
	m = pressAll(t, m, "enter", "3")
	out = view(m)
	for _, want := range []string{"ctrl+s", "save", "profiles"} {
		if !strings.Contains(out, want) {
			t.Errorf("defaults hints lack %q:\n%s", want, out)
		}
	}
	m = pressAll(t, m, "esc", "esc")
	if out := view(m); !strings.Contains(out, "open accounts") {
		t.Errorf("nav hint:\n%s", out)
	}
	m, _ = send(t, m, keyCtrlK)
	if out := view(m); !strings.Contains(out, "Accounts: Defaults") {
		// The palette window may clip it; the target must still resolve.
		if _, ok := paletteTargetForLabel("Accounts: Defaults"); !ok {
			t.Error("palette lacks Accounts: Defaults")
		}
	}
}

func TestAccountsASCIIHasNoWideGlyphs(t *testing.T) {
	m := accountsModel(t, accountsFake(), nil, 100, 30)
	m.work.AgentGlyphs = "ascii"
	m.acct.model.SetASCII(true)
	m = goAccounts(t, m)
	for _, key := range []string{"", "2", "3"} {
		if key != "" {
			m = pressAll(t, m, "enter", key)
		}
		for _, r := range view(m) {
			if r > 127 {
				t.Errorf("non-ASCII %q in ASCII mode:\n%s", string(r), view(m))
				break
			}
		}
	}
}
