package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/core/agentactivity"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
)

func act(state agentactivity.State, reason agentactivity.Reason, detail string) *agentactivity.Activity {
	return &agentactivity.Activity{State: state, Reason: reason, Detail: detail, Since: t0}
}

// withActivity returns s running with the given activity.
func withActivity(s agentsapi.Session, a *agentactivity.Activity) agentsapi.Session {
	s.Activity = a
	return s
}

func allIdx(n int) []int {
	idx := make([]int, n)
	for i := range idx {
		idx[i] = i
	}
	return idx
}

func TestAgentBadgeActivity(t *testing.T) {
	waiting := act(agentactivity.Waiting, agentactivity.ReasonPermission, "Bash")
	working := act(agentactivity.Working, "", "")
	idle := act(agentactivity.Idle, agentactivity.ReasonTurnComplete, "")
	tests := []struct {
		name     string
		sessions func(e *agentsEnv) []agentsapi.Session
		ascii    bool
		want     string
	}{
		{"working only", func(e *agentsEnv) []agentsapi.Session {
			return []agentsapi.Session{withActivity(e.session("a", "claude", "running", e.wtA), working)}
		}, false, "✹"},
		{"no activity is never marked", func(e *agentsEnv) []agentsapi.Session {
			return []agentsapi.Session{e.session("a", "claude", "running", e.wtA)}
		}, false, "✹"},
		{"idle only", func(e *agentsEnv) []agentsapi.Session {
			return []agentsapi.Session{withActivity(e.session("a", "claude", "running", e.wtA), idle)}
		}, false, "✹"},
		{"waiting adds one question mark", func(e *agentsEnv) []agentsapi.Session {
			return []agentsapi.Session{withActivity(e.session("a", "claude", "running", e.wtA), waiting)}
		}, false, "✹?"},
		{"waiting ascii", func(e *agentsEnv) []agentsapi.Session {
			return []agentsapi.Session{withActivity(e.session("a", "claude", "running", e.wtA), waiting)}
		}, true, "C?"},
		{"two waiting sessions still one mark", func(e *agentsEnv) []agentsapi.Session {
			return []agentsapi.Session{
				withActivity(e.session("a", "claude", "running", e.wtA), waiting),
				withActivity(e.session("b", "codex", "running", e.wtA), waiting),
			}
		}, false, "✹ ❃?"},
		{"mixed providers", func(e *agentsEnv) []agentsapi.Session {
			return []agentsapi.Session{
				withActivity(e.session("a", "claude", "running", e.wtA), idle),
				withActivity(e.session("b", "claude", "running", e.wtA), waiting),
				withActivity(e.session("c", "codex", "running", e.wtA), working),
			}
		}, false, "✹2 ❃?"},
		{"failed stays last", func(e *agentsEnv) []agentsapi.Session {
			return []agentsapi.Session{
				withActivity(e.session("a", "claude", "running", e.wtA), waiting),
				e.session("f", "codex", "failed", e.wtA),
			}
		}, false, "✹? !"},
		{"failed alone", func(e *agentsEnv) []agentsapi.Session {
			return []agentsapi.Session{e.session("f", "codex", "failed", e.wtA)}
		}, false, "!"},
		{"stale activity on an ended session is ignored", func(e *agentsEnv) []agentsapi.Session {
			return []agentsapi.Session{withActivity(e.session("a", "claude", "exited", e.wtA), waiting)}
		}, false, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newAgentsEnv(t)
			e.fb.Sessions.Add(tc.sessions(e)...)
			e.relist(t)
			got := ansi.Strip(e.m.agentBadge(allIdx(len(e.m.agents.sessions)), tc.ascii))
			if got != tc.want {
				t.Errorf("badge = %q, want %q", got, tc.want)
			}
		})
	}
}

// The glyph takes the style of its most urgent session; "?" and the waiting
// glyph share the attention style and idle-only glyphs are dimmed.
func TestAgentBadgeStyles(t *testing.T) {
	useTrueColor(t)
	th := theme.Current
	e := newAgentsEnv(t)
	e.fb.Sessions.Add(
		withActivity(e.session("a", "claude", "running", e.wtA), act(agentactivity.Idle, agentactivity.ReasonReady, "")),
		withActivity(e.session("b", "claude", "running", e.wtA), act(agentactivity.Waiting, agentactivity.ReasonQuestion, "")),
		withActivity(e.session("c", "codex", "running", e.wtA), act(agentactivity.Idle, agentactivity.ReasonReady, "")),
		withActivity(e.session("d", "antigravity", "running", e.wtA), act(agentactivity.Working, "", "")),
	)
	e.relist(t)
	got := e.m.agentBadge(allIdx(len(e.m.agents.sessions)), false)
	attention := lipgloss.NewStyle().Foreground(th.Warning).Bold(true)
	want := strings.Join([]string{
		attention.Render("✹2"),
		lipgloss.NewStyle().Foreground(th.Dim).Render("❃"),
		theme.ProviderStyle("antigravity").Render("✦") + attention.Render("?"),
	}, " ")
	if got != want {
		t.Errorf("badge styles:\n got %q\nwant %q", got, want)
	}
}

// The badge grows by one cell at most, so the narrow row layout keeps fitting.
func TestAgentBadgeWaitingAddsOneCell(t *testing.T) {
	e := newAgentsEnv(t)
	e.fb.Sessions.Add(e.session("a", "claude", "running", e.wtA), e.session("b", "codex", "running", e.wtA))
	e.relist(t)
	plain := ansi.StringWidth(e.m.agentBadge(allIdx(2), false))
	e.fb.Sessions.Set(withActivity(e.session("b", "codex", "running", e.wtA), act(agentactivity.Waiting, agentactivity.ReasonPlan, "")))
	e.relist(t)
	if got := ansi.StringWidth(e.m.agentBadge(allIdx(2), false)); got != plain+1 {
		t.Errorf("waiting badge width = %d, want %d", got, plain+1)
	}
	for _, w := range []int{30, 40, 80} {
		e.m.list.SetSize(w, 30)
		for _, l := range strings.Split(e.m.list.View(), "\n") {
			if got := ansi.StringWidth(l); got > w {
				t.Errorf("width %d: line is %d cells: %q", w, got, l)
			}
		}
		if row := e.rowOf("feat-a"); !strings.Contains(row, "✹ ❃?") {
			t.Errorf("width %d: waiting badge missing: %q", w, row)
		}
	}
}

func TestSessionStatusLabels(t *testing.T) {
	e := newAgentsEnv(t)
	tests := []struct {
		name       string
		s          agentsapi.Session
		key, label string
		header     string
	}{
		{"no activity", e.session("a", "claude", "running", ""), "running", "Running", "Running"},
		{"working", withActivity(e.session("a", "claude", "running", ""), act(agentactivity.Working, "", "")), "working", "Working", "Working"},
		{"stalled", withActivity(e.session("a", "claude", "running", ""), &agentactivity.Activity{State: agentactivity.Working, Stalled: true}), "stalled", "Stalled", "Stalled"},
		{"idle", withActivity(e.session("a", "claude", "running", ""), act(agentactivity.Idle, agentactivity.ReasonReady, "")), "idle", "Idle", "Idle"},
		{"permission", withActivity(e.session("a", "claude", "running", ""), act(agentactivity.Waiting, agentactivity.ReasonPermission, "Bash")), "waiting", "Needs input (permission)", "Needs permission: Bash"},
		{"question", withActivity(e.session("a", "claude", "running", ""), act(agentactivity.Waiting, agentactivity.ReasonQuestion, "")), "waiting", "Needs input (question)", "Asks a question"},
		{"no reason", withActivity(e.session("a", "claude", "running", ""), act(agentactivity.Waiting, "", "")), "waiting", "Needs input", "Needs input"},
		{"lifecycle wins", withActivity(e.session("a", "claude", "failed", ""), act(agentactivity.Waiting, agentactivity.ReasonPermission, "")), "failed", "Failed", "Failed"},
		{"starting", e.session("a", "claude", "starting", ""), "starting", "Starting", "Starting"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			key, label := sessionStatus(tc.s)
			if key != tc.key || label != tc.label {
				t.Errorf("sessionStatus = %q, %q; want %q, %q", key, label, tc.key, tc.label)
			}
			if _, h := headerStatus(tc.s); h != tc.header {
				t.Errorf("headerStatus = %q, want %q", h, tc.header)
			}
		})
	}
}

func TestAgentsHeaderShowsActivity(t *testing.T) {
	for _, size := range [][2]int{{120, 30}, {80, 24}} {
		e := newAgentsEnv(t)
		e.fb.Sessions.Add(withActivity(e.agent("r1", "claude", "running", e.wtA, "Fix auth", 1),
			act(agentactivity.Waiting, agentactivity.ReasonPermission, "Bash")))
		e.relist(t)
		e.sizeTo(t, size[0], size[1])
		e.openTab(t)
		pane := e.pane(size[0], size[1])
		if size[0] >= agentsWideMin {
			if !strings.Contains(pane, "Needs permission: Bash") {
				t.Errorf("%dx%d: header lacks the waiting reason:\n%s", size[0], size[1], pane)
			}
		}
		for _, l := range strings.Split(pane, "\n") {
			if got := ansi.StringWidth(l); got > size[0] {
				t.Errorf("%dx%d: line is %d cells: %q", size[0], size[1], got, l)
			}
		}
	}
}

func TestSessionsPickerShowsActivity(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {140, 40}} {
		e := newAgentsEnv(t)
		e.fb.Sessions.Add(
			withActivity(e.agent("r1", "claude", "running", e.wtA, "Fix authentication", 4), act(agentactivity.Waiting, agentactivity.ReasonPermission, "Bash")),
			withActivity(e.agent("r2", "codex", "running", e.wtA, "Run integration tests", 3), act(agentactivity.Working, "", "")),
			withActivity(e.agent("r3", "codex", "running", e.wtA, "Draft notes", 2), act(agentactivity.Idle, agentactivity.ReasonTurnComplete, "")),
		)
		e.openPicker(t, size[0], size[1])
		view := checkPickerBounds(t, e, "Fix authentication")
		for _, want := range []string{"● Waiting", "● Working", "● Idle"} {
			if !strings.Contains(view, want) {
				t.Errorf("%dx%d: picker lacks %q:\n%s", size[0], size[1], want, view)
			}
		}
		if size[0] >= 140 && !strings.Contains(view, "Needs input (permission)") {
			t.Errorf("%dx%d: inspector lacks the full waiting label:\n%s", size[0], size[1], view)
		}
	}
}
