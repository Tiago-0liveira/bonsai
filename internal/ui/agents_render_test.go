package ui

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/agentterminal"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
)

var update = flag.Bool("update", false, "rewrite the golden files in testdata")

// The goldens are the layout spec's mockups (tui-agents-plan/D-integration.md)
// rendered with a fake live session. Regenerate with:
//
//	go test ./internal/ui -run AgentsGolden -update
//
// and review the diff against the mockups; deliberate deviations are listed in
// the PR description.

// agentScreen is what the fake agent has drawn: positioned with CUP, ending with the prompt on
// the emulator's last row as a real agent leaves it (the observer crop is
// bottom-aligned).
const agentScreen = "\x1b[2J\x1b[H$ claude\r\n\r\n  Welcome to Claude Code\r\n\r\n  > fix the auth redirect\r\n\r\n  Reading internal/auth/redirect.go\x1b[23;1H\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\u2500\x1b[24;1H> "

// sizeTo makes the Agents pane exactly w x h cells by choosing the terminal
// size that yields it, so the live view and the render agree on the area.
func (e *agentsEnv) sizeTo(t *testing.T, w, h int) {
	t.Helper()
	for width := w; width < w+400; width++ {
		e.m.width, e.m.height = width, h+4
		e.m.layout()
		if pw, ph := e.m.agentPaneSize(); pw == w && ph == h {
			return
		}
		e.m.height = h + 3
		e.m.layout()
		if pw, ph := e.m.agentPaneSize(); pw == w && ph == h {
			return
		}
	}
	t.Fatalf("no terminal size gives a %dx%d agents pane", w, h)
}

func withUsage(t *testing.T, e *agentsEnv) {
	t.Helper()
	now := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	old := agentNow
	agentNow = func() time.Time { return now }
	t.Cleanup(func() { agentNow = old })
	reset := now.Add(2*time.Hour + 14*time.Minute)
	e.fb.Accounts.Usages["claude:work"] = agentsapi.Usage{Headline: agents.UsageHeadline{
		Primary:   &agents.UsageLimit{Window: "5h", RemainingFraction: ptr(0.72)},
		Secondary: &agents.UsageLimit{Window: "weekly", RemainingFraction: ptr(0.41)},
		NextReset: &reset,
	}}
	e.fb.Accounts.Usages["codex:work"] = e.fb.Accounts.Usages["claude:work"]
}

// goldenLive opens the tab on a worktree with the given sessions, attaches the
// first selected session read-only and draws the fake screen on it.
func goldenLive(t *testing.T, w, h int, makeSessions func(*agentsEnv) []agentsapi.Session) *agentsEnv {
	t.Helper()
	e := newAgentsEnv(t)
	withUsage(t, e)
	var sessions []agentsapi.Session
	if makeSessions != nil {
		sessions = makeSessions(e)
	}
	e.fb.Sessions.Add(sessions...)
	e.relist(t)
	e.sizeTo(t, w, h)
	e.openTab(t)
	e.fetchUsage(t)
	if len(sessions) > 0 {
		e.attached(t)
		e.frames(t, agentterminal.Frame{Type: "output", Data: []byte(agentScreen)}, status("running"))
	}
	return e
}

func (e *agentsEnv) twoSessions() []agentsapi.Session {
	a := e.agent("a", "claude", "running", e.wtA, "Auth fix", 1)
	b := e.agent("b", "codex", "running", e.wtA, "Tests", 2)
	return []agentsapi.Session{a, b}
}

// enterTerminal takes terminal mode as the writer, as the user's enter does.
func (e *agentsEnv) enterTerminal(t *testing.T) { e.enterTerminalWith(t, agentScreen) }

func (e *agentsEnv) enterTerminalWith(t *testing.T, screen string) {
	t.Helper()
	e.send(t, tea.KeyMsg{Type: tea.KeyEnter})
	e.attached(t)
	e.run(t, e.lastCmd)
	e.frames(t, agentterminal.Frame{Type: "output", Data: []byte(screen)}, status("running"))
	if e.m.agents.view.state != attachControlling {
		t.Fatalf("state = %v, want controlling", e.m.agents.view.state)
	}
}

// goldenText is the pane plus the status bar, stripped of styling.
func (e *agentsEnv) goldenText(w, h int) string {
	pw, ph := e.m.agentPaneSize()
	if pw != w || ph != h {
		panic(fmt.Sprintf("pane is %dx%d, want %dx%d", pw, ph, w, h))
	}
	return ansi.Strip(e.m.renderAgentsPane(w, h)) + "\n" + strings.Repeat("=", w) + "\n" + ansi.Strip(e.m.contextHelp())
}

func compareGolden(t *testing.T, name, got string) {
	t.Helper()
	lines := strings.Split(got, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " ")
	}
	got = strings.Join(lines, "\n") + "\n"
	path := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("missing golden %s (run with -update): %v", path, err)
	}
	want := strings.ReplaceAll(string(raw), "\r\n", "\n")
	if runtime.GOOS == "windows" {
		got = strings.ReplaceAll(got, `\`, "/")
		want = strings.ReplaceAll(want, `\`, "/")
	}
	if want != got {
		t.Errorf("%s differs from the golden file (run with -update to accept):\n--- got\n%s--- want\n%s", name, got, want)
	}
}

func TestAgentsGolden(t *testing.T) {
	type scenario struct {
		name string
		w, h int
		make func(t *testing.T, w, h int) *agentsEnv
	}
	live := func(t *testing.T, w, h int) *agentsEnv {
		return goldenLive(t, w, h, (*agentsEnv).twoSessions)
	}
	terminal := func(t *testing.T, w, h int) *agentsEnv {
		e := live(t, w, h)
		e.enterTerminal(t)
		return e
	}
	tests := []scenario{
		{"agents-wide-navigation-160x40", 160, 40, live},
		{"agents-strip-overflow-100x30", 100, 30, func(t *testing.T, w, h int) *agentsEnv {
			e := newAgentsEnv(t)
			manySessions(e, 6)
			return goldenFrom(t, e, w, h)
		}},
		{"agents-empty-120x30", 120, 30, func(t *testing.T, w, h int) *agentsEnv { return goldenLive(t, w, h, nil) }},
		{"agents-terminal-mode-120x30", 120, 30, terminal},
		{"agents-observing-other-120x30", 120, 30, func(t *testing.T, w, h int) *agentsEnv {
			e := live(t, w, h)
			if _, err := e.fb.Sessions.Attach(t.Context(), "b", agentsapi.AttachOptions{}); err != nil { // another client writes
				t.Fatal(err)
			}
			e.send(t, tea.KeyMsg{Type: tea.KeyEnter})
			e.attached(t)
			e.frames(t, status("running"))
			return e
		}},
		{"agents-unfocused-120x30", 120, 30, func(t *testing.T, w, h int) *agentsEnv {
			e := terminal(t, w, h)
			e.send(t, tea.KeyMsg{Type: tea.KeyCtrlCloseBracket})
			return e
		}},
		{"agents-compact-navigation-80x24", 80, 24, live},
		{"agents-compact-terminal-80x24", 80, 24, terminal},
		{"agents-narrow-navigation-58x20", 58, 20, live},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := tc.make(t, tc.w, tc.h)
			compareGolden(t, tc.name, e.goldenText(tc.w, tc.h))
		})
	}
}

// goldenFrom is goldenLive for an env whose sessions are already added.
func goldenFrom(t *testing.T, e *agentsEnv, w, h int) *agentsEnv {
	t.Helper()
	withUsage(t, e)
	e.relist(t)
	e.sizeTo(t, w, h)
	e.openTab(t)
	e.fetchUsage(t)
	e.attached(t)
	e.frames(t, agentterminal.Frame{Type: "output", Data: []byte(agentScreen)}, status("running"))
	return e
}

func TestAgentsGoldenPicker(t *testing.T) {
	for _, size := range [][2]int{{120, 30}, {80, 24}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			e := newAgentsEnv(t)
			e.fb.Sessions.Add(
				e.agent("r1", "claude", "running", e.wtA, "Fix authentication", 3),
				e.agent("r2", "codex", "running", e.wtA, "Run integration tests", 4),
				e.agent("x1", "codex", "exited", e.wtA, "Previous attempt", 1),
				e.agent("f1", "claude", "failed", e.wtA, "Broken run", 2),
			)
			e.relist(t)
			e.sizeTo(t, size[0], size[1])
			e.openTab(t)
			e.key(t, "a")
			compareGolden(t, fmt.Sprintf("agents-picker-%dx%d", size[0], size[1]), e.pickerView())
		})
	}
	t.Run("empty", func(t *testing.T) {
		e := newAgentsEnv(t)
		e.relist(t)
		e.sizeTo(t, 100, 24)
		e.openTab(t)
		e.key(t, "a")
		compareGolden(t, "agents-picker-empty-100x24", e.pickerView())
	})
}
