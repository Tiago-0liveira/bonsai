package ui

import (
	"bytes"
	"github.com/Tiago-0liveira/bonsai/internal/core/agentterminal"
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agentactivity"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
	tea "github.com/charmbracelet/bubbletea"
)

func TestAgentHintsUseOverrides(t *testing.T) {
	e := newAgentsEnv(t)
	e.m.keys = newKeyMap(map[string]string{"agents_tab": "ctrl+g", "agent_all": "ctrl+a", "agent_new": "ctrl+n", "agent_usage": "ctrl+u"})
	s := e.session("a", "claude", "running", e.wtA)
	if got := e.m.narrowRow(s, 0, 1); !strings.Contains(got, "Agents (ctrl+g)") || !strings.Contains(got, "ctrl+a: sessions") {
		t.Fatal(got)
	}
	if got := strings.Join(e.m.emptyArea("feat-a", 80, 20), "\n"); !strings.Contains(got, "[ctrl+n]") || !strings.Contains(got, "[ctrl+u]") {
		t.Fatal(got)
	}
	p := newSessionsPicker("test", false)
	p.keys = e.m.keys
	if !strings.Contains(p.footer(), "ctrl+n New") {
		t.Fatal(p.footer())
	}
}

func TestAgentEndedSummaryAndAcknowledgement(t *testing.T) {
	e := newAgentsEnv(t)
	e.fb.Sessions.Add(e.session("failed", "claude", "failed", e.wtA), e.session("done", "codex", "exited", e.wtA))
	e.relist(t)
	e.openTab(t)
	if got := e.pane(120, 30); !strings.Contains(got, "1 failed · 1 exited · a view · r retry") || strings.Contains(got, "No active agents") {
		t.Fatal(got)
	}
	if len(e.m.stripSessions(cleanPath(e.wtA))) != 2 {
		t.Fatal("ended sessions missing")
	}
	nm, _ := e.m.openAgentsPicker()
	e.m = nm.(Model)
	if !e.m.agents.ack["failed"] || !e.m.agents.ack["done"] {
		t.Fatal("history must acknowledge ended states")
	}
}

func TestAgentRepoPickerSearchAndJump(t *testing.T) {
	e := newAgentsEnv(t)
	e.fb.Sessions.Add(e.session("a", "claude", "running", e.wtA), e.session("b", "codex", "failed", e.wtB))
	e.relist(t)
	e.openTab(t)
	nm, _ := e.m.openRepoSessions()
	e.m = nm.(Model)
	p := e.m.agents.overlay.sessions
	if len(p.sessions) != 2 {
		t.Fatal("missing repository sessions")
	}
	p.query.SetValue("feat-b")
	if got := p.visible(); len(got) != 1 || got[0].ID != "b" {
		t.Fatal(got)
	}
	p.sync()
	_, cmd := p.Update(tea.KeyMsg{Type: tea.KeyEnter})
	picked := cmd().(sessionsPickMsg)
	nm, _ = e.m.onAgentsPick(picked.path, picked.id)
	e.m = nm.(Model)
	wt, _ := e.m.selectedWorktree()
	if wt.Path != e.wtB || e.m.agents.selected[cleanPath(e.wtB)] != "b" {
		t.Fatal("did not jump to worktree and session")
	}
}

func TestAgentAttentionCycles(t *testing.T) {
	e := newAgentsEnv(t)
	e.fb.Sessions.Add(
		withActivity(e.session("a", "claude", "running", e.wtA), act(agentactivity.Waiting, "", "")),
		withActivity(e.session("b", "codex", "running", e.wtB), act(agentactivity.Waiting, "", "")))
	e.relist(t)
	for _, want := range []string{"a", "b", "a"} {
		nm, _ := e.m.nextAgentAttention()
		e.m = nm.(Model)
		if e.m.agents.attentionID != want {
			t.Fatalf("got %s want %s", e.m.agents.attentionID, want)
		}
	}
	e.m.agents.sessions = nil
	e.m.agents.byTree = nil
	nm, _ := e.m.nextAgentAttention()
	if nm.(Model).status != "No sessions need attention" {
		t.Fatal("missing empty queue status")
	}
}

func TestAgentRetryPreservesRecordedOptions(t *testing.T) {
	e := newAgentsEnv(t)
	s := e.session("failed", "codex", "failed", e.wtA)
	s.Model = "test-model"
	s.Options = map[string]string{"effort": "high", "sandbox": "workspace-write", "approval": "on-request"}
	nm, cmd := e.m.retrySession(s)
	e.m = nm.(Model)
	_ = cmd()
	starts := e.fb.Sessions.Starts()
	if len(starts) != 1 {
		t.Fatal(starts)
	}
	got := starts[0]
	if got.WorkDir != s.WorkDir || got.AccountID != s.AccountID || got.Launch.Model != s.Model || got.Launch.Effort != "high" || got.Launch.Sandbox != "workspace-write" || got.Launch.Approval != "on-request" || got.Launch.Prompt != "" {
		t.Fatalf("%+v", got)
	}
}

func TestAgentStopAllRequiresConfirmation(t *testing.T) {
	e := newAgentsEnv(t)
	e.fb.Sessions.Add(e.session("a", "claude", "running", e.wtA), e.session("b", "codex", "running", e.wtA))
	e.relist(t)
	e.openTab(t)
	nm, cmd := e.m.askStopAllAgents()
	e.m = nm.(Model)
	if cmd != nil || e.m.modal == nil || e.m.pendingConfirm == nil {
		t.Fatal("stop all must wait for confirmation")
	}
}

func TestAgentBellTransitions(t *testing.T) {
	e := newAgentsEnv(t)
	old := e.session("a", "claude", "running", e.wtA)
	cases := []struct {
		name    string
		state   string
		waiting bool
		want    bool
	}{
		{"still working", "running", false, false},
		{"waiting", "running", true, true},
		{"done", "exited", false, true},
		{"failed", "failed", false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			next := e.session("a", "claude", tc.state, e.wtA)
			if tc.waiting {
				next.Activity = act(agentactivity.Waiting, "", "")
			}
			if got := agentNeedsBell([]agentsapi.Session{old}, []agentsapi.Session{next}); got != tc.want {
				t.Fatalf("bell=%v want %v", got, tc.want)
			}
			if agentNeedsBell(nil, []agentsapi.Session{next}) {
				t.Fatal("initial snapshot must be silent")
			}
			if agentNeedsBell([]agentsapi.Session{next}, []agentsapi.Session{next}) {
				t.Fatal("repeated state must be silent")
			}
		})
	}
}

func TestAgentPeekUsesCachedScreen(t *testing.T) {
	e, _ := terminalEnv(t)
	e.m.agents.view.term.Write([]byte("cached session preview"))
	peek := e.m.agentPeek()
	if !strings.Contains(strings.Join(peek[e.m.agents.view.sessionID], "\n"), "cached session preview") {
		t.Fatal(peek)
	}
	if len(peek[e.m.agents.view.sessionID]) > 10 {
		t.Fatal("peek must be bounded")
	}
}

func TestAgentStripOutputAge(t *testing.T) {
	e, _ := terminalEnv(t)
	v := e.m.agents.view
	v.lastOutput = agentNow().Add(-2 * time.Minute)
	wt, _ := e.m.selectedWorktree()
	s, idx, ok := e.m.selectedSession(cleanPath(wt.Path))
	if !ok {
		t.Fatal("missing session")
	}
	got := ansi.Strip(e.m.sessionStrip([]agentsapi.Session{s}, idx, 120, "", ""))
	if !strings.Contains(got, "▶ 2m") {
		t.Fatal(got)
	}
	e.m.state.Prefs.AgentGlyphs = "ascii"
	got = ansi.Strip(e.m.sessionStrip([]agentsapi.Session{s}, 0, 120, "", ""))
	if !strings.Contains(got, "> 2m") {
		t.Fatal(got)
	}
}

func TestBellOutput(t *testing.T) {
	var out bytes.Buffer
	m := renderModel()
	WithOutput(&out)(&m)
	m.ringBell()()
	if out.String() != "\a" {
		t.Fatalf("got %q", out.String())
	}
}

func TestSessionAttentionAcknowledgement(t *testing.T) {
	for _, state := range []string{"running", "failed", "exited"} {
		for _, ack := range []bool{false, true} {
			e := newAgentsEnv(t)
			s := agentsapi.Session{Summary: agentterminal.Summary{ID: "s", State: state}}
			e.m.agents.sessions = []agentsapi.Session{s}
			e.m.agents.byTree = map[string][]int{e.wtA: {0}}
			e.m.agents.ack["s"] = ack
			want := state != "running" && !ack
			if e.m.sessionNeedsAttention(s) != want || (e.m.worktreeAttention(e.wtA) > 0) != want {
				t.Fatalf("%s ack %v", state, ack)
			}
		}
	}
}

func TestLegendTrackedGlyph(t *testing.T) {
	for _, ascii := range []bool{false, true} {
		m := newAgentsEnv(t).m
		want := "±3"
		if ascii {
			m.state.Prefs.AgentGlyphs = "ascii"
			want = "+/-3"
		}
		if got := m.legendContent(); !strings.Contains(got, want) || strings.Contains(got, "●3") {
			t.Fatal(got)
		}
	}
}
