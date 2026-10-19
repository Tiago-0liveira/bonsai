package ui

import (
	"reflect"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/accounts"
)

// sharedUsageEnv has three accounts with distinct usage, a running session on
// claude:work and the pane started, with every clock pinned to t0.
func sharedUsageEnv(t *testing.T) *agentsEnv {
	t.Helper()
	e := paneEnv(t, 160, 40, paneAccount("claude:work", "claude", "work"), paneAccount("claude:personal", "claude", "personal"), paneAccount("codex:work", "codex", "work"))
	e.fb.Accounts.Usages["claude:work"] = paneUsage(10*time.Second, 0.72, 0.41)
	e.fb.Accounts.Usages["claude:personal"] = paneUsage(20*time.Second, 0.33, 0.22)
	e.fb.Accounts.Usages["codex:work"] = paneUsage(30*time.Second, 0.91, 0.5)
	e.setAccounts(e.fb.Accounts.Items...)
	e.fb.Sessions.Add(e.agent("a", "claude", "running", e.wtA, "Auth fix", 1))
	e.relist(t)
	e.startPane(t)
	e.openTab(t)
	e.run(t, e.lastCmd)
	return e
}

func (e *agentsEnv) closeOverlay(t *testing.T) {
	t.Helper()
	e.send(t, tea.KeyMsg{Type: tea.KeyEsc})
	e.run(t, e.lastCmd)
	if e.m.agents.overlay != nil {
		t.Fatal("esc must close the overlay")
	}
}

// Every view reads usage through the same Usage call and the same window:
// opening all of them for the same accounts reaches the provider once per
// account, and they show the same numbers.
func TestEveryUsageViewSharesOneSource(t *testing.T) {
	e := sharedUsageEnv(t)
	if got := len(e.fb.Accounts.Fetches()); got != 3 {
		t.Fatalf("pane + strip fetches = %v, want one per account", e.fb.Accounts.Fetches())
	}
	root := e.m.agents.usage

	e.key(t, "n")
	e.run(t, e.lastCmd)
	launch := e.m.agents.overlay.launch
	if launch == nil {
		t.Fatal("n must open the launch dialog")
	}
	launchUsage := launch.Usage()
	e.closeOverlay(t)

	e.key(t, "m")
	e.run(t, e.lastCmd)
	modal := e.m.agents.overlay.accounts
	if modal == nil {
		t.Fatal("m must open the profiles modal")
	}
	modalUsage := modal.Usage()
	e.closeOverlay(t)

	e.key(t, "u")
	e.run(t, e.lastCmd)
	quick := e.m.agents.overlay.quick
	if quick == nil {
		t.Fatal("u must open the quick usage overlay")
	}
	quickUsage := quick.Usage()
	e.closeOverlay(t)

	if got := e.fb.Accounts.Fetches(); len(got) != 3 {
		t.Errorf("provider fetches = %v, want exactly one per account across all views", got)
	}
	for _, c := range e.fb.Accounts.UsageCalls() {
		if c.Refresh {
			t.Errorf("%s was refreshed; only the manual key may bypass the window", c.ID)
		}
	}
	// The strip and the pane read the root store; the overlays hold their own
	// copy of it. All of them must agree on every account.
	for _, id := range []agents.AccountID{"claude:work", "claude:personal", "codex:work"} {
		want := root[id].Usage
		if !root[id].Have {
			t.Fatalf("root store lacks %s", id)
		}
		if got := e.m.agents.usage[id].Usage; !reflect.DeepEqual(got, want) {
			t.Errorf("%s: root store drifted: %+v vs %+v", id, got, want)
		}
		for name, got := range map[string]interface{}{
			"launch dialog": launchUsage[id].Usage,
			"profiles":      modalUsage[id].Usage,
			"quick":         quickUsage[id].Usage,
		} {
			if !reflect.DeepEqual(got, want) {
				t.Errorf("%s shows %+v for %s, the root store has %+v", name, got, id, want)
			}
		}
	}
}

// A result an overlay gets back reaches the root store, so the pane and the
// strip follow without a second fetch.
func TestOverlayResultsAreReportedToTheRootStore(t *testing.T) {
	e := sharedUsageEnv(t)
	fetches := len(e.fb.Accounts.Fetches())
	session, _, _ := e.m.selectedSession(cleanPath(e.wtA))
	stripBefore := e.m.usageText(session, true)

	e.key(t, "m")
	e.run(t, e.lastCmd)
	e.key(t, "2") // Usage tab
	newer := paneUsage(-time.Minute, 0.05, 0.04)
	e.fb.Accounts.Usages["claude:work"] = newer
	e.key(t, "r")
	e.run(t, e.lastCmd)

	if got := len(e.fb.Accounts.Fetches()); got != fetches+1 {
		t.Fatalf("fetches = %d, want %d: one manual refresh", got, fetches+1)
	}
	got := e.m.agents.usage["claude:work"]
	if !got.Usage.FetchedAt.Equal(newer.FetchedAt) {
		t.Fatalf("root store FetchedAt = %v, want the refreshed %v", got.Usage.FetchedAt, newer.FetchedAt)
	}
	for _, row := range e.m.paneAccounts() {
		if row.Name == "work" && row.Provider == "claude" {
			if f, _ := agents.RemainingFraction(row.Primary); f != 0.05 {
				t.Errorf("pane shows %v, want the refreshed 0.05", f)
			}
		}
	}
	if text := e.m.usageText(session, true); text == stripBefore {
		t.Errorf("strip still shows %q after the refresh", text)
	}
}

// The new-agent dialog loads accounts, selection, describe info, defaults and
// usage through the same Browser as the profiles modal.
func TestLaunchDialogLoadsLikeTheProfilesModal(t *testing.T) {
	e := sharedUsageEnv(t)
	for _, a := range e.fb.Accounts.Items {
		e.fb.Accounts.Infos[a.ID] = agents.AccountInfo{Identity: string(a.ID) + "@example.com", Plan: "Max"}
	}
	e.key(t, "n")
	e.run(t, e.lastCmd)
	dlg := e.dialog()

	api := e.m.agents.api.Accounts
	modal := accounts.New(api, accounts.Options{Initial: dlg.Selected(), Usage: e.m.agents.usage})
	cmd := modal.Init()
	for i := 0; i < 20 && cmd != nil; i++ {
		var next []tea.Cmd
		for _, msg := range runCmds(cmd) {
			var c tea.Cmd
			modal, c = modal.Update(msg)
			next = append(next, c)
		}
		cmd = tea.Batch(next...)
	}

	var launchOrder, modalOrder []agents.AccountID
	for _, a := range dlg.Ordered() {
		launchOrder = append(launchOrder, a.ID)
	}
	for _, a := range modal.Ordered() {
		modalOrder = append(modalOrder, a.ID)
	}
	if !reflect.DeepEqual(launchOrder, modalOrder) || len(launchOrder) != 3 {
		t.Fatalf("account order: launch %v, profiles %v", launchOrder, modalOrder)
	}
	if dlg.Selected() != modal.Selected() {
		t.Errorf("selected: launch %q, profiles %q", dlg.Selected(), modal.Selected())
	}
	la, _ := dlg.Current()
	ma, _ := modal.Current()
	if !reflect.DeepEqual(la.Info, ma.Info) || la.Info.Identity == "" {
		t.Errorf("describe info: launch %+v, profiles %+v", la.Info, ma.Info)
	}
	if !reflect.DeepEqual(dlg.Defaults(), modal.Defaults()) {
		t.Errorf("defaults differ: %+v vs %+v", dlg.Defaults(), modal.Defaults())
	}
	if !reflect.DeepEqual(dlg.Usage()[dlg.Selected()].Usage, modal.Usage()[modal.Selected()].Usage) {
		t.Error("selected account usage differs between the dialog and the modal")
	}
}
