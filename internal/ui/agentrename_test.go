package ui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi/fake"
)

func renameEnv(t *testing.T) *agentsEnv {
	t.Helper()
	e := newAgentsEnv(t)
	e.fb.Sessions.Add(e.agent("r1", "codex", "running", e.wtA, "Redesign agents", 2))
	e.openPicker(t, 100, 30)
	return e
}

func TestPickerRenameSavesPrefilledNameAndRelists(t *testing.T) {
	e := renameEnv(t)
	e.key(t, "R")
	if p := e.m.agents.overlay.sessions; !p.renaming {
		t.Fatal("R must open the name input in the picker")
	}
	if view := e.pickerView(); !strings.Contains(view, "Name: Redesign agents") || !strings.Contains(view, "Enter Save name") {
		t.Fatalf("rename row:\n%s", view)
	}
	e.key(t, "!")
	e.send(t, tea.KeyMsg{Type: tea.KeyEnter})
	// Step by hand: a success status clears itself on a timer that run would wait for.
	var after tea.Cmd
	for _, step := range []int{0, 1} {
		msgs := runCmds(e.lastCmd)
		if len(msgs) != 1 {
			t.Fatalf("step %d: %d messages", step, len(msgs))
		}
		nm, cmd := e.m.Update(msgs[0])
		e.m, e.lastCmd, after = nm.(Model), cmd, cmd
	}
	calls := e.fb.Sessions.Renames()
	if len(calls) != 1 || calls[0] != (fake.RenameCall{ID: "r1", Name: "Redesign agents!"}) {
		t.Fatalf("renames = %+v", calls)
	}
	if e.m.agents.overlay.sessions.renaming {
		t.Fatal("input stays open after saving")
	}
	if !strings.Contains(e.m.status, "renamed to Redesign agents!") || e.m.statusErr {
		t.Fatalf("status = %q err=%v", e.m.status, e.m.statusErr)
	}
	e.run(t, after)
	if got := e.m.agents.sessions[0].Name; got != "Redesign agents!" {
		t.Fatalf("list not refreshed: %q", got)
	}
}

func TestPickerRenameEscCancelsAndBlankDoesNotSave(t *testing.T) {
	e := renameEnv(t)
	e.key(t, "R")
	e.sendKey(t, tea.KeyEsc)
	if e.m.agents.overlay == nil || e.m.agents.overlay.sessions.renaming {
		t.Fatal("esc must close only the input")
	}
	e.key(t, "R")
	for range "Redesign agents" {
		e.sendKey(t, tea.KeyBackspace)
	}
	e.sendKey(t, tea.KeyEnter)
	if !e.m.agents.overlay.sessions.renaming || len(e.fb.Sessions.Renames()) != 0 {
		t.Fatal("a blank name must keep the input open without saving")
	}
}

func TestPickerRenameFailureShowsErrorStatus(t *testing.T) {
	e := renameEnv(t)
	e.fb.Sessions.RenameErr = errors.New("agent session unavailable")
	e.key(t, "R")
	e.sendKey(t, tea.KeyEnter)
	if !e.m.statusErr || !strings.Contains(e.m.status, "could not rename: agent session unavailable") {
		t.Fatalf("status = %q err=%v", e.m.status, e.m.statusErr)
	}
}

func TestAgentsTabRenameKeyOpensPickerInputOnSelectedSession(t *testing.T) {
	e := newAgentsEnv(t)
	e.fb.Sessions.Add(e.agent("r1", "codex", "running", e.wtA, "Old name", 2))
	e.relist(t)
	e.openTab(t)
	e.key(t, "R")
	o := e.m.agents.overlay
	if o == nil || o.sessions == nil || !o.sessions.renaming || o.sessions.renameID != "r1" {
		t.Fatalf("overlay = %+v", o)
	}
}

func TestRenameHintFollowsTheBinding(t *testing.T) {
	e := renameEnv(t)
	p := e.m.agents.overlay.sessions
	p.keys = newKeyMap(map[string]string{"agent_rename": "ctrl+r"})
	if f := p.footer(); !strings.Contains(f, "ctrl+r Rename") {
		t.Fatalf("footer = %q", f)
	}
}
