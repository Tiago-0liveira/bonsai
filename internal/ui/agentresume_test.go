package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Tiago-0liveira/bonsai/internal/core/agenthost"
	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/agentterminal"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
)

func resumeEnv(t *testing.T, state, reason string) *agentsEnv {
	t.Helper()
	e := newAgentsEnv(t)
	s := e.agent("a", "claude", state, e.wtA, "Conversation", 1)
	s.Error = reason
	e.fb.Sessions.Add(s)
	e.relist(t)
	e.openTab(t)
	e.fb.Sessions.ResumeResult = s
	e.fb.Sessions.ResumeResult.State = "running"
	return e
}

func TestEnterResumesInterrupted(t *testing.T) {
	for _, picker := range []bool{false, true} {
		t.Run(fmt.Sprintf("picker=%v", picker), func(t *testing.T) {
			e := resumeEnv(t, "failed", agenthost.LostError)
			if picker {
				e.key(t, "a")
				e.send(t, tea.KeyMsg{Type: tea.KeyEnter})
				for _, msg := range flatten(e.lastCmd) {
					e.send(t, msg)
				}
			} else {
				e.send(t, tea.KeyMsg{Type: tea.KeyEnter})
			}
			if e.m.agents.overlay != nil {
				t.Fatal("picker remained open")
			}
			if v := e.m.agents.view; v == nil || v.banner != "resuming…" {
				t.Fatalf("view = %+v", v)
			}
			cols, rows := e.m.agentAreaSize()
			for _, msg := range flatten(e.lastCmd) {
				e.send(t, msg)
			}
			calls := e.fb.Sessions.Resumes()
			if len(calls) != 1 || calls[0].ID != "a" || calls[0].Cols != cols || calls[0].Rows != rows {
				t.Fatalf("resumes = %+v; want a at %dx%d", calls, cols, rows)
			}
			if len(e.fb.Sessions.Attaches()) != 0 {
				t.Fatal("attached before resume completed")
			}
			e.attached(t)
			attaches := e.fb.Sessions.Attaches()
			if len(attaches) != 1 || attaches[0].Opts.ReadOnly || attaches[0].Opts.Take {
				t.Fatalf("attaches = %+v", attaches)
			}
			if !e.m.agentTerminalMode() || e.m.status != "resumed after interruption" {
				t.Fatalf("control/status = %v / %q", e.m.agentTerminalMode(), e.m.status)
			}
		})
	}
}

func TestEnterOnExitedDoesNotResume(t *testing.T) {
	e := resumeEnv(t, "exited", "")
	e.attached(t)
	e.frames(t, status("exited"))
	e.send(t, tea.KeyMsg{Type: tea.KeyEnter})
	flatten(e.lastCmd)
	if len(e.fb.Sessions.Resumes()) != 0 || e.m.agents.view.control {
		t.Fatal("ended session resumed or controlled")
	}
}

func TestResumeKeyOnExited(t *testing.T) {
	for _, picker := range []bool{false, true} {
		t.Run(fmt.Sprintf("picker=%v", picker), func(t *testing.T) {
			e := resumeEnv(t, "exited", "")
			if picker {
				e.key(t, "a")
			}
			e.key(t, "r")
			if picker {
				for _, msg := range flatten(e.lastCmd) {
					e.send(t, msg)
				}
			}
			flatten(e.lastCmd)
			if calls := e.fb.Sessions.Resumes(); len(calls) != 1 || calls[0].ID != "a" {
				t.Fatalf("resumes = %+v", calls)
			}
		})
	}
}

func TestResumeKeyIgnoredWhenActive(t *testing.T) {
	e := resumeEnv(t, "running", "")
	e.attached(t)
	e.key(t, "r")
	flatten(e.lastCmd)
	if len(e.fb.Sessions.Resumes()) != 0 || e.m.modal != nil {
		t.Fatal("active session resumed or global action ran")
	}
}

func TestNotResumableBanner(t *testing.T) {
	for _, tc := range []struct{ reason, hint string }{
		{agents.ReasonNoConversation, " · F start fresh"},
		{agents.ReasonWorktreeGone, ""},
	} {
		t.Run(tc.reason, func(t *testing.T) {
			e := resumeEnv(t, "failed", agenthost.LostError)
			e.fb.Sessions.ResumeErr = fmt.Errorf("wrapped: %w", &agents.NotResumableError{Reason: tc.reason})
			e.send(t, tea.KeyMsg{Type: tea.KeyEnter})
			for _, msg := range flatten(e.lastCmd) {
				e.send(t, msg)
			}
			if got, want := e.m.agents.view.banner, "can't resume: "+tc.reason+tc.hint; got != want {
				t.Fatalf("banner = %q, want %q", got, want)
			}
			if len(e.fb.Sessions.Attaches()) != 0 {
				t.Fatal("attached after failed resume")
			}
		})
	}
}

func TestInterruptedLabel(t *testing.T) {
	e := resumeEnv(t, "failed", agenthost.LostError)
	e.key(t, "a")
	if view := e.pickerView(); !strings.Contains(view, "interrupted") {
		t.Fatalf("missing interrupted label:\n%s", view)
	}
}

func TestResumeKeyBindingNoConflict(t *testing.T) {
	e := resumeEnv(t, "exited", "")
	handled, _, cmd := e.m.agentsTabKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	if !handled || cmd == nil {
		t.Fatal("Agents tab did not handle r before globals")
	}
	if got := defaultBindings["agent_resume"]; len(got.keys) != 1 || got.keys[0] != "r" || got.desc != "resume session" {
		t.Fatalf("binding = %+v", got)
	}
	for _, sec := range keymapSections {
		if sec.name != "Agents tab" {
			continue
		}
		for _, action := range sec.actions {
			if action != "agent_resume" && defaultBindings[action].keys[0] == "r" {
				t.Fatalf("r conflicts with %s", action)
			}
		}
	}
}

func TestInterruptedBanner(t *testing.T) {
	e := resumeEnv(t, "failed", agenthost.LostError)
	e.attached(t)
	s := e.fb.Sessions.ResumeResult.Summary
	s.State, s.Error = "failed", agenthost.LostError
	applyAgentStatus(e.m.agents.view, s)
	if got := e.m.agents.view.banner; got != "interrupted · enter to resume" {
		t.Fatalf("banner = %q", got)
	}
	s.Error = "launch failed"
	applyAgentStatus(e.m.agents.view, s)
	if got := e.m.agents.view.banner; got != "failed: launch failed" {
		t.Fatalf("banner = %q", got)
	}
}

func TestResumeIgnoresStaleResult(t *testing.T) {
	e := resumeEnv(t, "failed", agenthost.LostError)
	e.send(t, tea.KeyMsg{Type: tea.KeyEnter})
	messages := flatten(e.lastCmd)
	e.selectWT(e.repo)
	e.send(t, tea.WindowSizeMsg{Width: 100, Height: 30})
	for _, msg := range messages {
		e.send(t, msg)
	}
	if e.m.agents.view != nil || len(e.fb.Sessions.Attaches()) != 0 || e.m.status == "resumed after interruption" {
		t.Fatal("stale result restored view or status")
	}
}

func TestResumeRepeatedKey(t *testing.T) {
	e := resumeEnv(t, "failed", agenthost.LostError)
	e.send(t, tea.KeyMsg{Type: tea.KeyEnter})
	cmd := e.lastCmd
	e.send(t, tea.KeyMsg{Type: tea.KeyEnter})
	flatten(e.lastCmd)
	flatten(cmd)
	if calls := e.fb.Sessions.Resumes(); len(calls) != 1 {
		t.Fatalf("resumes = %+v", calls)
	}
}

func TestResumeErrorSanitized(t *testing.T) {
	e := resumeEnv(t, "exited", "")
	e.fb.Sessions.ResumeErr = fmt.Errorf("\x1b[31mprovider\nfailed\x1b[0m")
	e.key(t, "r")
	for _, msg := range flatten(e.lastCmd) {
		e.send(t, msg)
	}
	if got := e.m.agents.view.banner; got != "can't resume: provider failed" {
		t.Fatalf("banner = %q", got)
	}
}

func TestResumeRejectsOldFramesAndLists(t *testing.T) {
	e := resumeEnv(t, "failed", agenthost.LostError)
	old := e.attached(t)
	oldToken := e.m.agents.view.token
	oldSession := e.m.agents.sessions[0]
	e.send(t, tea.KeyMsg{Type: tea.KeyEnter})
	for _, msg := range flatten(e.lastCmd) {
		e.send(t, msg)
	}
	if !old.Closed() {
		t.Fatal("old stream not released")
	}
	e.send(t, agentFramesMsg{token: oldToken, frames: []agentterminal.Frame{status("failed")}})
	e.send(t, agentsListMsg{seq: e.m.agents.seq, sessions: []agentsapi.Session{oldSession}})
	if ss, _, ok := e.m.selectedSession(cleanPath(e.wtA)); !ok || !ss.Active() {
		t.Fatalf("stale list restored session: %+v", ss)
	}
	if e.m.agents.view.state == attachEnded {
		t.Fatal("old frames ended resumed view")
	}
}

func TestResumePickerRoutesWorktree(t *testing.T) {
	e := resumeEnv(t, "exited", "")
	ss := e.agent("b", "codex", "failed", e.wtB, "Other conversation", 2)
	ss.Error = agenthost.LostError
	e.fb.Sessions.Add(ss)
	e.relist(t)
	e.key(t, "A")
	e.send(t, sessionsPickMsg{path: e.wtB, id: "b"})
	flatten(e.lastCmd)
	calls := e.fb.Sessions.Resumes()
	wt, _ := e.m.selectedWorktree()
	if len(calls) != 1 || calls[0].ID != "b" || cleanPath(wt.Path) != cleanPath(e.wtB) || e.m.agents.overlay != nil {
		t.Fatalf("routes: calls=%+v worktree=%s", calls, wt.Path)
	}
}

func TestResumeKeyIgnoredWhenActiveInPicker(t *testing.T) {
	e := resumeEnv(t, "running", "")
	e.key(t, "a")
	e.key(t, "r")
	flatten(e.lastCmd)
	if len(e.fb.Sessions.Resumes()) != 0 || e.m.agents.overlay == nil {
		t.Fatal("active picker row resumed or closed")
	}
}

func TestResumeRedesignResult(t *testing.T) {
	e := resumeEnv(t, "failed", agenthost.LostError)
	e.m.layoutKind = LayoutRedesign
	e.m.workspace = wsWorktrees
	e.m.shellFocus = shellBody
	e.m.focus = focusTerminal
	cmd := e.m.resumeSelectedAgent()
	for _, msg := range flatten(cmd) {
		e.send(t, msg)
	}
	if calls := e.fb.Sessions.Resumes(); len(calls) != 1 {
		t.Fatalf("resumes = %+v", calls)
	}
	if e.m.status != "resumed after interruption" || e.m.agents.view == nil || !e.m.agents.view.control {
		t.Fatalf("redesign did not handle resume result: status=%q view=%+v", e.m.status, e.m.agents.view)
	}
}
