package accounts

import (
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
)

// Footer hints per tab, as the UX mockups spell them.
const (
	footerOverview = "j/k Navigate  ·  Tab Section  ·  Enter Select  ·  Esc Close"
	footerUsage    = "j/k Account  ·  Tab Section  ·  r Refresh  ·  Esc Close"
	footerDefaults = "Tab Section  ·  Enter Edit  ·  Esc Back"
)

// reload delivers a ReloadMsg, as the parent does after a terminal flow ends.
func reload(m Model) Model {
	m, cmd := m.Update(ReloadMsg{})
	m, _ = drain(m, cmd)
	return m
}

// execOf returns the first ExecMsg among msgs.
func execOf(t *testing.T, msgs []tea.Msg) *exec.Cmd {
	t.Helper()
	for _, msg := range msgs {
		if e, ok := msg.(ExecMsg); ok {
			return e.Cmd
		}
	}
	t.Fatalf("no ExecMsg in %#v", msgs)
	return nil
}

func TestNavigationWalksProviderGroupsAndReachesAdd(t *testing.T) {
	m := opened(t, newFake(), 104, 34)
	// Display order is claude work, claude personal, codex work.
	for _, want := range []string{"Claude · personal", "Codex · work"} {
		m, _ = press(m, "j")
		if out := plain(m); !strings.Contains(out, want) {
			t.Fatalf("after j, view lacks %q:\n%s", want, out)
		}
	}
	m, _ = press(m, "k")
	if out := plain(m); !strings.Contains(out, "Claude · personal") {
		t.Fatalf("k did not step back:\n%s", out)
	}
	m, _ = press(m, "k", "k")
	if out := plain(m); !strings.Contains(out, "Claude · work") {
		t.Fatalf("k past the top must stay on the first account:\n%s", out)
	}

	m, _ = press(m, "j", "j", "j")
	if !m.add.active {
		t.Fatalf("j past the last account must reach + Add account:\n%s", plain(m))
	}
	out := plain(m)
	for _, want := range []string{"ADD ACCOUNT", "> + Add account"} {
		if !strings.Contains(out, want) {
			t.Fatalf("add view lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "> ✳ work") || strings.Contains(out, "> ◎ work") {
		t.Fatalf("an account keeps the selection marker on the add row:\n%s", out)
	}

	m, _ = press(m, "esc")
	if out := plain(m); m.add.active || !strings.Contains(out, "Codex · work") {
		t.Fatalf("esc from add must return to the last account:\n%s", out)
	}
}

func TestTabCyclingAndDirectKeys(t *testing.T) {
	m := opened(t, newFake(), 104, 34)
	steps := []struct{ key, want string }{
		{"tab", "[Usage]"},
		{"tab", "[Defaults]"},
		{"tab", "[Overview]"},
		{"shift+tab", "[Defaults]"},
		{"2", "[Usage]"},
		{"3", "[Defaults]"},
		{"1", "[Overview]"},
	}
	for _, s := range steps {
		m, _ = press(m, s.key)
		out := plain(m)
		if !strings.Contains(out, s.want) {
			t.Fatalf("after %s, view lacks %q:\n%s", s.key, s.want, out)
		}
		// Only the active tab is bracketed.
		if n := strings.Count(out, "[Overview]") + strings.Count(out, "[Usage]") + strings.Count(out, "[Defaults]"); n != 1 {
			t.Fatalf("after %s, %d tabs bracketed:\n%s", s.key, n, out)
		}
	}
}

func TestFooterMatchesMockupPerTab(t *testing.T) {
	for _, size := range [][2]int{{104, 34}, {80, 24}} {
		m := opened(t, newFake(), size[0], size[1])
		for _, c := range []struct{ key, footer string }{
			{"1", footerOverview},
			{"2", footerUsage},
			{"3", footerDefaults},
		} {
			m, _ = press(m, c.key)
			if out := plain(m); !strings.Contains(out, c.footer) {
				t.Fatalf("%dx%d tab %s: footer %q missing:\n%s", size[0], size[1], c.key, c.footer, out)
			}
		}
	}
}

func TestFooterAddStages(t *testing.T) {
	m := opened(t, newFake(), 104, 34)
	m, _ = press(m, "j", "j", "j")
	if out := plain(m); !strings.Contains(out, "j/k Provider  ·  Enter Continue  ·  Esc Back") {
		t.Fatalf("provider stage footer:\n%s", out)
	}
	m, _ = press(m, "enter")
	if out := plain(m); !strings.Contains(out, "Type a name  ·  Enter Continue  ·  Esc Back") {
		t.Fatalf("name stage footer:\n%s", out)
	}
	m, _ = press(m, "w", "enter")
	if out := plain(m); !strings.Contains(out, "Enter Continue in terminal  ·  Esc Back") {
		t.Fatalf("continue stage footer:\n%s", out)
	}
}

func TestSidebarUsageColumnNeedsWideSidebar(t *testing.T) {
	// 80x24 gives a 76-cell frame, whose sidebar is 26 cells wide.
	m := opened(t, newFake(), 80, 24)
	if out := plain(m); !strings.Contains(out, " 72%") {
		t.Fatalf("usage column missing at 26-cell sidebar:\n%s", out)
	}
	// 60x24 gives a 56-cell frame and a 24-cell sidebar.
	m = opened(t, newFake(), 60, 24)
	if out := plain(m); strings.Contains(out, "72%") {
		t.Fatalf("usage column shown at 24-cell sidebar:\n%s", out)
	}
}

func TestDescribeForUnselectedAccountIsIgnored(t *testing.T) {
	m := opened(t, newFake(), 104, 34)
	m, _ = press(m, "j") // c2 is selected now
	m, _ = m.Update(describeMsg{id: "c1", seq: m.latest.describe, info: agents.AccountInfo{Identity: "late@example.com"}})
	if out := plain(m); strings.Contains(out, "late@example.com") || !strings.Contains(out, "me@example.com") {
		t.Fatalf("describe for c1 leaked into c2:\n%s", out)
	}
}

func TestStaleSequenceResultsIgnored(t *testing.T) {
	m := opened(t, newFake(), 104, 34)
	m, _ = press(m, "j")
	old := m.latest.describe
	m, _ = press(m, "k", "j") // a newer describe for c2 has landed
	m, _ = m.Update(describeMsg{id: "c2", seq: old, info: agents.AccountInfo{Identity: "old@example.com"}})
	if out := plain(m); strings.Contains(out, "old@example.com") || !strings.Contains(out, "me@example.com") {
		t.Fatalf("stale describe overwrote newer state:\n%s", out)
	}

	m, _ = press(m, "2")
	oldUsage := m.latest.usage
	m, _ = press(m, "r")
	m, _ = m.Update(usageMsg{id: "c2", seq: oldUsage, usage: usageOf(0.05, false)})
	if out := plain(m); strings.Contains(out, "5% remaining") {
		t.Fatalf("stale usage overwrote the refresh:\n%s", out)
	}

	before := len(m.accounts)
	m, _ = m.Update(listMsg{seq: m.latest.list - 1, accounts: nil})
	if len(m.accounts) != before {
		t.Fatalf("stale list cleared the accounts")
	}
}

func TestUsageRefreshErrorKeepsLastGoodNumbers(t *testing.T) {
	api := newFake()
	m := opened(t, api, 104, 34)
	m, _ = press(m, "2")
	api.UsageErr = errors.New("provider returned 503 from https://api.example.com/usage/token-abc")
	m, _ = press(m, "r")

	out := plain(m)
	for _, want := range []string{"72% remaining", "Updated 35 seconds ago", "Showing last good data · provider returned 503"} {
		if !strings.Contains(out, want) {
			t.Fatalf("stale usage lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "token-abc") || strings.Contains(out, "https://") {
		t.Fatalf("error leaked a URL:\n%s", out)
	}
}

func TestUsageWithoutDataShowsErrorNotNumbers(t *testing.T) {
	api := newFake()
	api.UsageErr = errors.New("no usage yet")
	m := opened(t, api, 104, 34)
	m, _ = press(m, "2")
	out := plain(m)
	if !strings.Contains(out, "Usage unavailable") || !strings.Contains(out, "no usage yet") || strings.Contains(out, "remaining") {
		t.Fatalf("usage without data:\n%s", out)
	}
}

func TestAntigravityShowsModelPools(t *testing.T) {
	api := newFake()
	api.Items = append(api.Items, acct("a1", "antigravity", "personal", "logged_in"))
	low, high := 0.12, 0.8
	api.Usages["a1"] = agentsapi.Usage{
		Snapshot: agents.UsageSnapshot{Limits: []agents.UsageLimit{
			{ID: "pro", Group: "Gemini", Label: "Gemini Pro", RemainingFraction: &low},
			{ID: "flash", Group: "Gemini", Label: "Flash", RemainingFraction: &high},
		}},
		FetchedAt: testNow,
	}
	m := opened(t, api, 104, 34)
	m = selectByID(t, m, "a1")
	m, _ = press(m, "2")
	out := plain(m)
	for _, want := range []string{"Gemini", "Gemini Pro", "12%", "Flash", "80%"} {
		if !strings.Contains(out, want) {
			t.Fatalf("antigravity pools lack %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "5-hour window") {
		t.Fatalf("pools must not show the shared windows:\n%s", out)
	}
}

// selectByID moves the selection with j/k until id is selected.
func selectByID(t *testing.T, m Model, id agents.AccountID) Model {
	t.Helper()
	for range 10 {
		if m.selected == id {
			return m
		}
		m, _ = press(m, "j")
	}
	t.Fatalf("could not select %s; selected %s", id, m.selected)
	return m
}

func TestEscLeavesDropdownBeforeClosing(t *testing.T) {
	m := opened(t, newFake(), 104, 34)
	m, _ = press(m, "3", "enter")
	if !m.Capturing() {
		t.Fatal("open dropdown must capture keys")
	}
	m, msgs := press(m, "esc")
	if len(msgs) != 0 || m.Capturing() {
		t.Fatalf("esc in dropdown: msgs=%#v capturing=%v", msgs, m.Capturing())
	}
}

func TestDiscardDialogCanDiscardThenClose(t *testing.T) {
	api := newFake()
	m := opened(t, api, 104, 34)
	m, _ = press(m, "3", "enter", "down", "enter", "esc")
	if !strings.Contains(plain(m), "Discard unsaved defaults?") {
		t.Fatalf("no discard dialog:\n%s", plain(m))
	}
	m, msgs := press(m, "right", "enter")
	if len(msgs) != 1 || msgs[0] != (CloseMsg{}) {
		t.Fatalf("discard must close the modal, msgs=%#v", msgs)
	}
	if len(api.Updates()) != 0 {
		t.Fatalf("discard must not save: %+v", api.Updates())
	}
}

func TestSwitchingAccountWithUnsavedDefaultsAsks(t *testing.T) {
	m := opened(t, newFake(), 104, 34)
	m, _ = press(m, "3", "enter", "down", "enter", "j")
	out := plain(m)
	if !strings.Contains(out, "Discard unsaved defaults?") || !strings.Contains(out, "Claude · work") {
		t.Fatalf("switching with unsaved edits must ask:\n%s", out)
	}
	m, _ = press(m, "enter") // "Keep editing" is the safe default
	if out := plain(m); strings.Contains(out, "Discard unsaved defaults?") || !strings.Contains(out, "Claude · work") {
		t.Fatalf("keep editing must stay on the account:\n%s", out)
	}
}

func TestRemoveBlockedWhileRunning(t *testing.T) {
	api := newFake()
	api.Items[0].Running = 2
	m := opened(t, api, 104, 34)
	m, _ = press(m, "down", "enter")
	out := plain(m)
	if !strings.Contains(out, "Stop 2 running sessions first.") || m.dlg != nil {
		t.Fatalf("remove while running:\n%s", out)
	}
	if got := api.Removed(); len(got) != 0 {
		t.Fatalf("removed %v while running", got)
	}
}

func TestRemoveConfirmReloadsAndSelectsNeighbor(t *testing.T) {
	api := newFake()
	m := opened(t, api, 104, 34)
	m, _ = press(m, "down", "enter")
	if out := plain(m); !strings.Contains(out, "Remove ✳ work? Its Bonsai config dir is deleted.") {
		t.Fatalf("no confirm dialog:\n%s", out)
	}
	m, _ = press(m, "esc")
	if m.dlg != nil || len(api.Removed()) != 0 {
		t.Fatalf("esc must cancel the removal")
	}

	m, _ = press(m, "down", "enter", "right", "enter")
	if got := api.Removed(); len(got) != 1 || got[0] != "c1" {
		t.Fatalf("removed = %v", got)
	}
	out := plain(m)
	if strings.Contains(out, "Claude · work") || !strings.Contains(out, "Claude · personal") {
		t.Fatalf("after remove the neighbor must be selected:\n%s", out)
	}
}

func TestRemoveErrorShownInline(t *testing.T) {
	api := newFake()
	api.RemoveErr = errors.New("permission denied")
	m := opened(t, api, 104, 34)
	m, _ = press(m, "down", "enter", "right", "enter")
	if out := plain(m); !strings.Contains(out, "Couldn't remove: permission denied") {
		t.Fatalf("remove error not shown:\n%s", out)
	}
}

func TestLoginEmitsTerminalCommand(t *testing.T) {
	m := opened(t, newFake(), 104, 34)
	_, msgs := press(m, "enter")
	args := strings.Join(execOf(t, msgs).Args, " ")
	if !strings.HasSuffix(args, "agent account login c1") {
		t.Fatalf("login argv = %q", args)
	}
}

func TestAddNameValidation(t *testing.T) {
	cases := []struct{ name, want string }{
		{"", "Name is required"},
		{"   ", "Name is required"},
		{"a/b", "Name cannot contain /"},
		{"WORK", "Claude already has an account named WORK"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := opened(t, newFake(), 104, 34)
			m, _ = press(m, "j", "j", "j", "enter") // claude, name stage
			if c.name != "" {
				m, _ = press(m, c.name)
			}
			m, msgs := press(m, "enter")
			if len(msgs) != 0 {
				t.Fatalf("invalid name emitted %#v", msgs)
			}
			if out := plain(m); !strings.Contains(out, c.want) {
				t.Fatalf("want %q:\n%s", c.want, out)
			}
		})
	}
}

func TestAddFlowEmitsCommandAndSelectsCreated(t *testing.T) {
	api := newFake()
	m := opened(t, api, 104, 34)
	m, _ = press(m, "j", "j", "j") // + Add account
	m, _ = press(m, "j", "j")      // antigravity; "work" is free there
	m, _ = press(m, "enter", "work", "enter")
	if out := plain(m); !strings.Contains(out, "Enter Continue in terminal") {
		t.Fatalf("continue stage:\n%s", out)
	}
	m, msgs := press(m, "enter")
	args := strings.Join(execOf(t, msgs).Args, " ")
	if !strings.HasSuffix(args, "agent account add antigravity work") {
		t.Fatalf("add argv = %q", args)
	}
	if m.add.active {
		t.Fatal("add form must close when the terminal flow starts")
	}

	// The CLI created the account; the reload selects it.
	api.Items = append(api.Items, acct("a1", "antigravity", "work", "logged_in"))
	m = reload(m)
	if out := plain(m); !strings.Contains(out, "Antigravity · work") {
		t.Fatalf("created account not selected:\n%s", out)
	}
}

func TestAddEscBacksOutOneStage(t *testing.T) {
	m := opened(t, newFake(), 104, 34)
	m, _ = press(m, "j", "j", "j", "enter", "x", "enter")
	m, _ = press(m, "esc") // continue -> name
	if out := plain(m); !strings.Contains(out, "Type a name") {
		t.Fatalf("esc from continue must go back to the name:\n%s", out)
	}
	m, _ = press(m, "esc") // name -> provider
	if out := plain(m); !strings.Contains(out, "j/k Provider") {
		t.Fatalf("esc from name must go back to the provider:\n%s", out)
	}
	m, _ = press(m, "esc") // provider -> account
	if m.add.active {
		t.Fatalf("esc from provider must leave add:\n%s", plain(m))
	}
}

func TestReloadKeepsSelectionByID(t *testing.T) {
	api := newFake()
	m := opened(t, api, 104, 34)
	m, _ = press(m, "j", "j") // codex work
	// A new claude account sorts first in input order; the selection must
	// still follow the id, not the row index.
	api.Items = append([]agentsapi.Account{acct("c0", "claude", "new", "logged_in")}, api.Items...)
	m = reload(m)
	if out := plain(m); !strings.Contains(out, "Codex · work") {
		t.Fatalf("reload moved the selection:\n%s", out)
	}
}

func TestSavedNoticeClears(t *testing.T) {
	m := opened(t, newFake(), 104, 34)
	m, _ = press(m, "3", "enter", "down", "enter", "ctrl+s")
	if !strings.Contains(plain(m), "Saved") {
		t.Fatalf("no Saved notice:\n%s", plain(m))
	}
	m, _ = m.Update(savedClearMsg{id: "c1", seq: m.latest.save})
	if out := plain(m); strings.Contains(out, "Saved") {
		t.Fatalf("Saved did not clear:\n%s", out)
	}
}

func TestASCIIVariants(t *testing.T) {
	m := New(newFake(), Options{ASCII: true, Now: func() time.Time { return testNow }})
	m.SetSize(104, 34)
	m, _ = drain(m, m.Init())
	out := plain(m)
	for _, want := range []string{"C Claude · work", "> C work", "  C personal", "  X work", "  * "} {
		if !strings.Contains(out, want) {
			t.Fatalf("ascii overview lacks %q:\n%s", want, out)
		}
	}
	if strings.ContainsAny(out, "✳◎✦●▼") {
		t.Fatalf("unicode glyphs in ascii overview:\n%s", out)
	}

	m, _ = press(m, "3", "enter")
	if out := plain(m); !strings.Contains(out, "[v]") || strings.Contains(out, "▼") {
		t.Fatalf("ascii defaults arrow:\n%s", out)
	}
}

func TestASCIIUsageMeter(t *testing.T) {
	m := New(newFake(), Options{ASCII: true, Now: func() time.Time { return testNow }})
	m.SetSize(104, 34)
	m, _ = drain(m, m.Init())
	m, _ = press(m, "2")
	if out := plain(m); strings.ContainsAny(out, "█░") || !strings.Contains(out, "#") {
		t.Fatalf("ascii usage meter must use # and .:\n%s", out)
	}
}

func TestNoSecretsOrConfigPathsRendered(t *testing.T) {
	api := newFake()
	api.DescribeErr = errors.New("read /home/alice/.bonsai/agents/c1/CLAUDE_CONFIG_DIR/creds: denied")
	m := opened(t, api, 104, 34)
	forbidden := []string{"/home/", "/.claude", "/.codex", "CLAUDE_CONFIG_DIR", "CODEX_HOME", "Bearer ", "sk-"}
	for _, key := range []string{"1", "2", "3", "j", "k"} {
		m, _ = press(m, key)
		out := plain(m)
		for _, bad := range forbidden {
			if strings.Contains(out, bad) {
				t.Fatalf("key %s rendered %q:\n%s", key, bad, out)
			}
		}
		if key == "1" && !strings.Contains(out, "<path>") {
			t.Fatalf("describe error path not redacted:\n%s", out)
		}
	}
}

func TestSanitizeRedactsPathsAndControls(t *testing.T) {
	got := sanitize(errors.New("\x1b[31mfailed at /home/u/.claude/x\x1b[0m\n  now"))
	if strings.Contains(got, "/home") || strings.Contains(got, "\x1b") || strings.Contains(got, "\n") {
		t.Fatalf("sanitize = %q", got)
	}
	if long := sanitize(errors.New(strings.Repeat("word ", 60))); ansi.StringWidth(long) > maxErrLen {
		t.Fatalf("long error not capped: %d cells", ansi.StringWidth(long))
	}
	if sanitize(nil) != "" {
		t.Fatal("nil error must sanitize to empty")
	}
}
