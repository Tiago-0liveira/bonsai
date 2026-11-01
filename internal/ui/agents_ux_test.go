package ui

import (
	"strings"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/agentactivity"
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
