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
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi/fake"
)

var t0 = time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)

// agent builds a session on dir; created orders sessions by start time.
func (e *agentsEnv) agent(id, provider, state, dir, name string, created int) agentsapi.Session {
	s := e.session(id, provider, state, dir)
	s.Name = name
	s.ProfileName = "work"
	s.AccountID = agents.AccountID(provider + ":work")
	s.CreatedAt = t0.Add(time.Duration(created) * time.Minute)
	return s
}

func (e *agentsEnv) key(t *testing.T, k string) {
	t.Helper()
	var msg tea.KeyMsg
	switch k {
	case "tab":
		msg = tea.KeyMsg{Type: tea.KeyTab}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
	}
	nm, cmd := e.m.Update(msg)
	e.m = nm.(Model)
	e.lastCmd = cmd
}

func (e *agentsEnv) selectWT(path string) {
	e.m.list.SelectByPath(path)
}

func (e *agentsEnv) openTab(t *testing.T) {
	t.Helper()
	e.selectWT(e.wtA)
	e.key(t, "g")
}

func (e *agentsEnv) pane(w, h int) string { return ansi.Strip(e.m.renderAgentsPane(w, h)) }

func assertBox(t *testing.T, out string, w, h int) {
	t.Helper()
	lines := strings.Split(out, "\n")
	if len(lines) != h {
		t.Errorf("rows = %d, want %d", len(lines), h)
	}
	for _, l := range lines {
		if got := ansi.StringWidth(l); got > w {
			t.Errorf("line %d cells > %d: %q", got, w, l)
		}
	}
}

func TestAgentsLeftRight(t *testing.T) {
	tests := []struct {
		name        string
		left, right string
		width       int
		want        string
	}{
		{name: "both fit", left: "abc", right: "xy", width: 8, want: "abc   xy"},
		{name: "left truncated", left: "abcdefgh", right: "xy", width: 7, want: "abc… xy"},
		{name: "minimum right room", left: "abcdef", right: "xyz", width: 5, want: "… xyz"},
		{name: "right cannot fit", left: "abcdef", right: "xyz", width: 4, want: "abc…"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := leftRight(tc.left, tc.right, tc.width); got != tc.want {
				t.Errorf("leftRight(%q, %q, %d) = %q, want %q", tc.left, tc.right, tc.width, got, tc.want)
			}
		})
	}
}

func manySessions(e *agentsEnv, n int) {
	for i := 1; i <= n; i++ {
		p := []string{"claude", "codex", "antigravity"}[i%3]
		e.fb.Sessions.Add(e.agent(fmt.Sprintf("s%d", i), p, "running", e.wtA, fmt.Sprintf("Task number %d long name", i), i))
	}
}

func TestAgentsTabVisibility(t *testing.T) {
	t.Run("visible with zero agents", func(t *testing.T) {
		e := newAgentsEnv(t)
		e.selectWT(e.wtA)
		found := false
		for _, tb := range e.m.visibleTabs() {
			found = found || tb == tabAgents
		}
		if !found {
			t.Error("Agents tab missing with a backend and no sessions")
		}
		if got := tabLabel(tabAgents, false); !strings.Contains(got, "Agents (g)") {
			t.Errorf("label = %q", got)
		}
	})
	t.Run("absent without backend", func(t *testing.T) {
		e := newAgentsEnv(t, func(*Model) {})
		for _, tb := range e.m.visibleTabs() {
			if tb == tabAgents {
				t.Error("Agents tab must not exist without a backend")
			}
		}
		e.key(t, "g")
		if e.m.rightTab == tabAgents {
			t.Error("g must not open the tab without a backend")
		}
	})
	t.Run("unsupported host explains itself", func(t *testing.T) {
		fb := fake.New()
		fb.Sessions.Supports = false
		e := newAgentsEnv(t, WithAgents(fb.API()))
		e.selectWT(e.wtA)
		e.key(t, "g")
		if e.m.rightTab != tabAgents {
			t.Fatal("tab should exist on an unsupported host")
		}
		if !strings.Contains(e.pane(70, 20), "need a Unix host") {
			t.Errorf("missing notice:\n%s", e.pane(70, 20))
		}
		if !strings.Contains(ansi.Strip(e.m.View()), "need a Unix host") {
			t.Error("View lacks the notice")
		}
	})
}

func TestAgentsEmptyState(t *testing.T) {
	e := newAgentsEnv(t)
	e.relist(t)
	e.openTab(t)
	for _, w := range []int{50, 80, 120} {
		out := e.pane(w, 20)
		assertBox(t, out, w, 20)
		for _, want := range []string{"No running agents", "No active agents", "[n] New agent", "[u] View account usage", "feat-a"} {
			if !strings.Contains(out, want) {
				t.Errorf("w=%d missing %q:\n%s", w, want, out)
			}
		}
	}
}

func TestAgentsStripOverflow(t *testing.T) {
	for _, w := range []int{80, 120, 200} {
		t.Run(fmt.Sprint(w), func(t *testing.T) {
			e := newAgentsEnv(t)
			manySessions(e, 9)
			e.relist(t)
			e.openTab(t)
			out := e.pane(w, 20)
			assertBox(t, out, w, 20)
			first := strings.Split(out, "\n")[0]
			if w <= 120 && !strings.Contains(first, "more]") {
				t.Errorf("expected overflow marker at %d: %q", w, first)
			}
			if w == 200 && strings.Contains(first, "more]") {
				t.Errorf("200 cells should fit 9 labels: %q", first)
			}
			// Selecting the last session must keep its label visible.
			e.key(t, "9")
			first = strings.Split(e.pane(w, 20), "\n")[0]
			if !strings.Contains(first, "[9 ") {
				t.Errorf("selected label scrolled out: %q", first)
			}
			if !strings.Contains(first, "a: All") {
				t.Errorf("hints dropped: %q", first)
			}
		})
	}
}

func TestAgentsCompactBreakpoints(t *testing.T) {
	e := newAgentsEnv(t)
	e.fb.Accounts.Usages["claude:work"] = agentsapi.Usage{Headline: agents.UsageHeadline{
		Primary:   &agents.UsageLimit{Window: "5h", RemainingFraction: ptr(0.72)},
		Secondary: &agents.UsageLimit{Window: "weekly", RemainingFraction: ptr(0.41)},
	}}
	e.fb.Sessions.Add(
		e.agent("a", "claude", "running", e.wtA, "Auth fix", 1),
		e.agent("b", "claude", "running", e.wtA, "Tests", 2),
		e.agent("c", "claude", "running", e.wtA, "API", 3),
	)
	e.relist(t)
	e.openTab(t)
	e.fetchUsage(t)

	tests := []struct {
		w       int
		want    []string
		notWant []string
	}{
		{120, []string{"Sessions: ", "n: New", "Claude · work", "│ ● Running", "5h ███████░░░ left", "Week 41% left", "u: Usage"}, nil},
		{70, []string{"[1 ✹ Auth fix]", "a: All", "work · 5h ███████░░░ · W 41%", "u: Usage"}, []string{"Sessions:", "n: New", "left"}},
		{50, []string{"Agents (g) │", "[3/3]", "a: Switch"}, []string{"u: Usage", "5h"}},
	}
	for _, tc := range tests {
		t.Run(fmt.Sprint(tc.w), func(t *testing.T) {
			out := e.pane(tc.w, 20)
			assertBox(t, out, tc.w, 20)
			for _, s := range tc.want {
				if !strings.Contains(out, s) {
					t.Errorf("missing %q:\n%s", s, out)
				}
			}
			for _, s := range tc.notWant {
				if strings.Contains(out, s) {
					t.Errorf("unexpected %q:\n%s", s, out)
				}
			}
		})
	}
	t.Run("too small", func(t *testing.T) {
		out := e.pane(120, 9) // 5 fixed rows leave 4 < 5
		if !strings.Contains(out, "Pane too small") {
			t.Errorf("want too-small notice:\n%s", out)
		}
		assertBox(t, out, 120, 9)
		if strings.Contains(e.pane(120, 10), "Pane too small") {
			t.Error("10 rows leave exactly 5 for the terminal")
		}
	})
}

func ptr(f float64) *float64 { return &f }

func TestAgentsFullViewWidths(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {120, 40}, {200, 50}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			e := newAgentsEnv(t)
			manySessions(e, 4)
			e.m.width, e.m.height = size[0], size[1]
			e.relist(t)
			e.openTab(t)
			out := e.m.View()
			lines := strings.Split(out, "\n")
			if len(lines) > size[1] {
				t.Errorf("%d rows > %d", len(lines), size[1])
			}
			for _, l := range lines {
				if ansi.StringWidth(l) > size[0] {
					t.Errorf("line wider than %d: %q", size[0], ansi.Strip(l))
				}
			}
			if !strings.Contains(ansi.Strip(out), "Agents (g)") {
				t.Error("tab label missing")
			}
		})
	}
}

func TestAgentsSelectionMemory(t *testing.T) {
	e := newAgentsEnv(t)
	e.fb.Sessions.Add(
		e.agent("a1", "claude", "running", e.wtA, "One", 1),
		e.agent("a2", "codex", "running", e.wtA, "Two", 2),
		e.agent("b1", "claude", "running", e.wtB, "Bee", 1),
	)
	e.relist(t)
	e.openTab(t)

	sel := func(path string) string { s, _, _ := e.m.selectedSession(path); return s.ID }
	if got := sel(e.wtA); got != "a2" {
		t.Fatalf("default should be the most recent active session, got %s", got)
	}
	e.key(t, "1")
	e.selectWT(e.wtB)
	e.key(t, "]") // wraps within wt-b's single session
	if got := sel(e.wtB); got != "b1" {
		t.Fatalf("wt-b = %s", got)
	}
	e.selectWT(e.wtA)
	if got := sel(e.wtA); got != "a1" {
		t.Errorf("selection forgotten across worktree switch: %s", got)
	}
	e.key(t, "]")
	if got := sel(e.wtA); got != "a2" {
		t.Errorf("] should advance, got %s", got)
	}
	e.key(t, "[")
	if got := sel(e.wtA); got != "a1" {
		t.Errorf("[ should go back, got %s", got)
	}
	e.key(t, "7") // no such session: unchanged
	if got := sel(e.wtA); got != "a1" {
		t.Errorf("out-of-range number changed selection: %s", got)
	}
}

func TestAgentsPicker(t *testing.T) {
	e := newAgentsEnv(t)
	e.fb.Sessions.Add(
		e.agent("r1", "claude", "running", e.wtA, "Fix authentication", 3),
		e.agent("r2", "codex", "starting", e.wtA, "Run tests", 4),
		e.agent("x1", "codex", "exited", e.wtA, "Previous attempt", 1),
		e.agent("f1", "claude", "failed", e.wtA, "Broken run", 2),
	)
	e.relist(t)
	e.openTab(t)
	if row := e.rowOf("feat-a"); !strings.HasSuffix(strings.TrimRight(row, " "), "!") {
		t.Fatalf("setup: failed marker missing: %q", row)
	}

	e.m.width, e.m.height = 140, 40
	e.m.layout()
	e.key(t, "a")
	if e.m.agents.overlay == nil || e.m.agents.overlay.sessions == nil || e.m.modal != nil {
		t.Fatal("picker not open as an agent overlay")
	}
	view := e.pickerView()
	if !strings.Contains(view, "Sessions") || !strings.Contains(view, "feat-a") || !strings.Contains(view, "4 sessions") {
		t.Fatalf("picker lacks the title, branch and count:\n%s", view)
	}
	// Flat list, no section headers: running newest first, then ended newest
	// first.
	var order []string
	for _, s := range e.m.agents.overlay.sessions.visible() {
		order = append(order, s.ID)
	}
	if got := strings.Join(order, ","); got != "r2,r1,f1,x1" {
		t.Fatalf("order = %s, want r2,r1,f1,x1", got)
	}
	for _, want := range []string{"Run tests", "Codex · work", "● Starting", "● Running", "✗ Failed", "✓ Exited"} {
		if !strings.Contains(view, want) {
			t.Errorf("picker view missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "Recent") {
		t.Errorf("picker must not show sections:\n%s", view)
	}
	e.key(t, "/")
	e.key(t, "previous")
	if got := e.m.agents.overlay.sessions.visible(); len(got) != 1 || got[0].ID != "x1" {
		t.Errorf("search should keep only the match: %+v", got)
	}
	e.send(t, tea.KeyMsg{Type: tea.KeyEsc}) // clears the search, keeps the picker
	if e.m.agents.overlay == nil || len(e.m.agents.overlay.sessions.visible()) != 4 {
		t.Fatal("esc in search clears the query")
	}
	e.send(t, tea.KeyMsg{Type: tea.KeyDown})
	e.send(t, tea.KeyMsg{Type: tea.KeyDown})
	e.send(t, tea.KeyMsg{Type: tea.KeyEnter})
	e.run(t, e.lastCmd)
	if e.m.agents.overlay != nil {
		t.Error("picker should close")
	}
	if s, _, _ := e.m.selectedSession(cleanPath(e.wtA)); s.ID != "f1" {
		t.Errorf("selected = %s, want f1", s.ID)
	}
	if !e.m.agents.ack["f1"] {
		t.Error("picking a failed session acknowledges it")
	}
	if row := e.rowOf("feat-a"); strings.Contains(row, "!") {
		t.Errorf("! should clear after ack: %q", row)
	}
	if first := strings.Split(e.pane(120, 20), "\n")[0]; !strings.Contains(first, "Broken run") {
		t.Errorf("an ended pick should appear in the strip: %q", first)
	}
}

func TestAgentsUsageOrdering(t *testing.T) {
	e := newAgentsEnv(t)
	hl := func(f float64) agentsapi.Usage {
		return agentsapi.Usage{Headline: agents.UsageHeadline{Primary: &agents.UsageLimit{Window: "5h", RemainingFraction: ptr(f)}}}
	}
	e.fb.Accounts.Usages["claude:work"] = hl(0.72)
	e.fb.Accounts.Usages["codex:work"] = hl(0.33)
	e.fb.Sessions.Add(
		e.agent("a", "claude", "running", e.wtA, "One", 1),
		e.agent("b", "codex", "running", e.wtA, "Two", 2),
	)
	e.relist(t)
	e.openTab(t) // selects b (codex): request 1
	cmdB := e.lastCmd
	if cmdB == nil {
		t.Fatal("opening the tab should request usage")
	}
	e.key(t, "1") // switch to a (claude): request 2 supersedes
	cmdA := e.lastCmd

	lateB := usageMsgOf(t, cmdB) // answer for the account the user already left
	nm, _ := e.m.Update(lateB)
	e.m = nm.(Model)
	// The answer lands on its own account's entry and never on the one the
	// strip is showing.
	if !e.m.agents.usage["codex:work"].Have || e.m.agents.usage["claude:work"].Have {
		t.Error("late result must update only the account it was fetched for")
	}
	nm, _ = e.m.Update(usageMsgOf(t, cmdA))
	e.m = nm.(Model)
	if !strings.Contains(e.pane(120, 20), "5h ███████░░░ left") {
		t.Errorf("current account usage not shown:\n%s", e.pane(120, 20))
	}

	// A failed refresh keeps the last good data, dimmed and marked stale.
	e.fb.Accounts.UsageErr = errors.New("rate limited")
	e.key(t, "]")
	e.key(t, "[")
	nm, _ = e.m.Update(usageMsgOf(t, e.lastCmd))
	e.m = nm.(Model)
	out := e.pane(120, 20)
	if !strings.Contains(out, "5h ███████░░░ left (stale)") {
		t.Errorf("stale usage not shown:\n%s", out)
	}
}

func TestAgentsWorktreeMoveAcceptsNewAccountUsage(t *testing.T) {
	e := newAgentsEnv(t)
	e.fb.Accounts.Usages["claude:work"] = agentsapi.Usage{Headline: agents.UsageHeadline{
		Primary: &agents.UsageLimit{Window: "5h", RemainingFraction: ptr(0.72)},
	}}
	e.fb.Accounts.Usages["codex:work"] = agentsapi.Usage{Headline: agents.UsageHeadline{
		Primary: &agents.UsageLimit{Window: "5h", RemainingFraction: ptr(0.33)},
	}}
	e.fb.Sessions.Add(
		e.agent("a", "claude", "running", e.wtA, "One", 1),
		e.agent("b", "codex", "running", e.wtB, "Two", 1),
	)
	e.relist(t)
	e.openTab(t)
	e.key(t, "tab")
	before := e.usageSeq("codex:work")

	nm, cmd := e.m.Update(tea.KeyMsg{Type: tea.KeyDown})
	e.m = nm.(Model)
	if got := selectedPath(e.m.list); got != e.wtB {
		t.Fatalf("selected worktree = %q, want %q", got, e.wtB)
	}
	if e.m.agents.usageAccount != "codex:work" || e.usageSeq("codex:work") == before {
		t.Fatalf("usage request state = (%q, %d), want a new codex:work request", e.m.agents.usageAccount, e.usageSeq("codex:work"))
	}
	nm, _ = e.m.Update(usageMsgOf(t, cmd))
	e.m = nm.(Model)
	if out := e.pane(120, 20); !strings.Contains(out, "5h ███░░░░░░░ left") {
		t.Errorf("new account usage response was rejected:\n%s", out)
	}
}

func TestAgentsUsageTickOnlyWhenTabVisible(t *testing.T) {
	e := newAgentsEnv(t)
	e.fb.Sessions.Add(e.agent("a", "claude", "running", e.wtA, "One", 1))
	e.relist(t)
	e.selectWT(e.wtA)
	before := e.usageSeq("claude:work")
	nm, cmd := e.m.Update(agentUsageTickMsg{gen: e.m.agents.usageTickGen})
	e.m = nm.(Model)
	if e.usageSeq("claude:work") != before || cmd == nil {
		t.Error("hidden tab: no request, but the tick must re-arm")
	}
	e.openTab(t)
	before = e.usageSeq("claude:work")
	nm, _ = e.m.Update(agentUsageTickMsg{gen: e.m.agents.usageTickGen})
	e.m = nm.(Model)
	if e.usageSeq("claude:work") == before {
		t.Error("visible tab: tick must request usage")
	}
}

func TestAgentsUsageTickInitChainIsSingle(t *testing.T) {
	e := newAgentsEnv(t)
	e.fb.Accounts.Usages["claude:work"] = agentsapi.Usage{}
	e.fb.Sessions.Add(e.agent("a", "claude", "running", e.wtA, "One", 1))
	e.relist(t)
	e.openTab(t)
	if e.m.Init() == nil || e.m.Init() == nil {
		t.Fatal("Init must schedule agent work")
	}

	gen := e.m.agents.usageTickGen
	nm, cmd := e.m.Update(agentUsageTickMsg{gen: gen})
	e.m = nm.(Model)
	batch, ok := cmd().(tea.BatchMsg)
	if !ok || len(batch) != 2 {
		t.Fatalf("current tick command = %T with %d items, want request + re-arm", batch, len(batch))
	}
	if msg := batch[0](); msg == nil {
		t.Fatal("usage request returned no result")
	}

	callsBefore := len(e.fb.Accounts.UsageCalls())
	before := e.usageSeq("claude:work")
	nm, staleCmd := e.m.Update(agentUsageTickMsg{gen: gen})
	e.m = nm.(Model)
	if staleCmd != nil || e.usageSeq("claude:work") != before {
		t.Errorf("old generation re-armed or requested usage: cmd=%v seq=%d want=%d", staleCmd != nil, e.usageSeq("claude:work"), before)
	}
	nm, currentCmd := e.m.Update(agentUsageTickMsg{gen: e.m.agents.usageTickGen})
	e.m = nm.(Model)
	currentBatch, ok := currentCmd().(tea.BatchMsg)
	if !ok || len(currentBatch) != 2 {
		t.Fatalf("current generation must request usage and re-arm once, got %T with %d items", currentBatch, len(currentBatch))
	}
	currentBatch[0]()
	if calls := e.fb.Accounts.UsageCalls(); len(calls) != callsBefore+1 {
		t.Errorf("usage calls in the current period = %d, want one", len(calls)-callsBefore)
	}
}

func TestAgentsKeyPrecedence(t *testing.T) {
	t.Run("tab-local keys only when the Agents tab is focused", func(t *testing.T) {
		e := newAgentsEnv(t)
		e.fb.Sessions.Add(e.agent("a", "claude", "running", e.wtA, "One", 1))
		e.relist(t)
		e.openTab(t)
		kinds := map[string]func(*agentOverlay) bool{
			"n": func(o *agentOverlay) bool { return o.launch != nil },
			"m": func(o *agentOverlay) bool { return o.accounts != nil },
			"u": func(o *agentOverlay) bool { return o.quick != nil },
		}
		for _, k := range []string{"n", "m", "u"} {
			e.key(t, k)
			if e.m.agents.overlay == nil || !kinds[k](e.m.agents.overlay) || e.m.modal != nil {
				t.Fatalf("%s on the Agents tab: overlay=%+v modal=%v", k, e.m.agents.overlay, e.m.modal != nil)
			}
			e.send(t, tea.KeyMsg{Type: tea.KeyEsc})
			e.run(t, e.lastCmd)
			if e.m.agents.overlay != nil {
				t.Fatalf("esc must close the %s overlay", k)
			}
		}
	})
	t.Run("globals unchanged elsewhere", func(t *testing.T) {
		e := newAgentsEnv(t)
		e.selectWT(e.wtA)
		e.key(t, "n") // list focused: new worktree modal
		if e.m.modal == nil || e.m.agents.overlay != nil {
			t.Errorf("global n must still open the create flow (modal=%v status=%q)", e.m.modal != nil, e.m.status)
		}
	})
	t.Run("other tab keeps its own keys", func(t *testing.T) {
		e := newAgentsEnv(t)
		e.selectWT(e.wtA)
		e.m.rightTab, e.m.focus = tabLog, focusTerminal
		e.key(t, "n")
		if e.m.agents.overlay != nil {
			t.Error("n on the log tab must not reach the agents handler")
		}
	})
	t.Run("g opens and focuses the tab from the list", func(t *testing.T) {
		e := newAgentsEnv(t)
		e.selectWT(e.wtA)
		e.key(t, "g")
		if e.m.rightTab != tabAgents || e.m.focus != focusTerminal {
			t.Errorf("tab=%v focus=%v", e.m.rightTab, e.m.focus)
		}
	})
	t.Run("rebinding moves the tab-local key", func(t *testing.T) {
		e := newAgentsEnv(t)
		e.m.keys = newKeyMap(map[string]string{"agent_new": "N"})
		e.openTab(t)
		e.key(t, "N")
		if e.m.agents.overlay == nil || e.m.agents.overlay.launch == nil {
			t.Error("rebound key not honored")
		}
	})
}

func TestAgentsPaletteAndHelp(t *testing.T) {
	e := newAgentsEnv(t)
	labels := map[string]bool{}
	for _, c := range e.m.paletteCommands() {
		labels[c.label] = true
	}
	for _, want := range []string{"Agents: open tab", "Agents: new agent", "Agents: accounts", "Agents: usage"} {
		if !labels[want] {
			t.Errorf("palette missing %q", want)
		}
	}
	off := newAgentsEnv(t, func(*Model) {})
	for _, c := range off.m.paletteCommands() {
		if strings.HasPrefix(c.label, "Agents:") {
			t.Errorf("palette shows %q without a backend", c.label)
		}
	}
}

func TestAgentsWideLayoutRows(t *testing.T) {
	e := newAgentsEnv(t)
	e.fb.Sessions.Add(
		e.agent("a", "claude", "running", e.wtA, "Auth fix", 1),
		e.agent("b", "codex", "running", e.wtA, "Tests", 2),
	)
	e.relist(t)
	e.openTab(t)

	t.Run("title and blank row when the terminal keeps 20 rows", func(t *testing.T) {
		rows := strings.Split(e.pane(120, 27), "\n")
		assertBox(t, strings.Join(rows, "\n"), 120, 27)
		if !strings.HasPrefix(rows[0], "AGENTS · feat-a") || strings.TrimSpace(rows[1]) != "" {
			t.Errorf("title rows: %q / %q", rows[0], rows[1])
		}
		if !strings.HasPrefix(rows[2], "Sessions: ") || !strings.HasPrefix(rows[3], "──") {
			t.Errorf("strip then rule expected: %q / %q", rows[2], rows[3])
		}
		if !strings.Contains(rows[4], "Codex · work") || !strings.Contains(rows[4], "● Running") {
			t.Errorf("header 1: %q", rows[4])
		}
		if !strings.Contains(rows[5], "u: Usage") || !strings.HasPrefix(rows[6], "──") {
			t.Errorf("header 2 then rule expected: %q / %q", rows[5], rows[6])
		}
	})
	t.Run("title drops first when the terminal would get under 20 rows", func(t *testing.T) {
		rows := strings.Split(e.pane(120, 26), "\n")
		if !strings.HasPrefix(rows[0], "Sessions: ") || strings.Contains(strings.Join(rows, "\n"), "AGENTS ·") {
			t.Errorf("title should be gone:\n%s", strings.Join(rows, "\n"))
		}
		if !strings.HasPrefix(rows[1], "──") {
			t.Errorf("rule after the strip: %q", rows[1])
		}
	})
	t.Run("compact keeps one header row and no title", func(t *testing.T) {
		rows := strings.Split(e.pane(80, 40), "\n")
		if !strings.HasPrefix(rows[0], "[1 ") || !strings.HasPrefix(rows[2], "──") || strings.Contains(strings.Join(rows, "\n"), "AGENTS ·") {
			t.Errorf("compact rows:\n%s", strings.Join(rows[:4], "\n"))
		}
	})
	t.Run("narrow is a single summary row", func(t *testing.T) {
		rows := strings.Split(e.pane(50, 40), "\n")
		if !strings.HasPrefix(rows[0], "Agents (g) │") {
			t.Errorf("row 0: %q", rows[0])
		}
	})
}

func TestAgentsPickerEmptyAndActions(t *testing.T) {
	e := newAgentsEnv(t)
	e.relist(t)
	e.openTab(t)
	e.key(t, "a")
	if e.m.agents.overlay == nil || e.m.agents.overlay.sessions == nil {
		t.Fatal("the picker opens even with zero sessions")
	}
	view := e.pickerView()
	if !strings.Contains(view, "No sessions in this worktree yet") || !strings.Contains(view, "n New session") {
		t.Errorf("empty picker:\n%s", view)
	}
	// n inside the picker is an action, not search text.
	e.key(t, "n")
	e.run(t, e.lastCmd)
	if e.m.modal != nil || e.m.agents.overlay == nil || e.m.agents.overlay.launch == nil {
		t.Errorf("n in the picker must open the launch dialog: modal=%v overlay=%+v", e.m.modal != nil, e.m.agents.overlay)
	}
}

func TestAgentsStatusBar(t *testing.T) {
	e := newAgentsEnv(t)
	e.openTab(t)
	if got := ansi.Strip(e.m.contextHelp()); got != "a all · m profiles · u usage · n new · tab focus · ? keys" {
		t.Errorf("agents tab hints = %q", got)
	}
	e.m.rightTab = tabLog
	if strings.Contains(ansi.Strip(e.m.contextHelp()), "agents") {
		t.Error("other tabs keep their own hints")
	}
}
