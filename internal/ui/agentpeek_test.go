package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func peekEnv(t *testing.T) *agentsEnv {
	t.Helper()
	e := newAgentsEnv(t)
	e.fb.Sessions.Add(
		e.agent("r1", "codex", "running", e.wtA, "First", 4),
		e.agent("r2", "claude", "running", e.wtA, "Second", 3),
		e.agent("x1", "claude", "exited", e.wtA, "Done", 2),
	)
	e.fb.Sessions.SetPeek("r1", "one a", "one b")
	e.fb.Sessions.SetPeek("r2", "two a")
	return e
}

func TestPickerPeeksSelectedSessionWithoutAttach(t *testing.T) {
	e := peekEnv(t)
	e.openPicker(t, 120, 30)
	if view := e.pickerView(); !strings.Contains(view, "loading…") {
		t.Fatalf("no loading marker:\n%s", view)
	}
	e.run(t, e.lastCmd)
	if view := e.pickerView(); !strings.Contains(view, "Last screen:") || !strings.Contains(view, "one b") || strings.Contains(view, "loading…") {
		t.Fatalf("snapshot not shown:\n%s", view)
	}
	e.sendKey(t, tea.KeyDown)
	if view := e.pickerView(); !strings.Contains(view, "two a") {
		t.Fatalf("second session snapshot missing:\n%s", view)
	}
	if got := strings.Join(e.fb.Sessions.Peeks(), ","); got != "r1,r2" {
		t.Fatalf("peeks = %s", got)
	}
	if len(e.fb.Sessions.Attaches()) != 0 {
		t.Fatal("peek must not attach")
	}
	// An ended session has no screen to read.
	e.sendKey(t, tea.KeyDown)
	if got := strings.Join(e.fb.Sessions.Peeks(), ","); got != "r1,r2" {
		t.Fatalf("ended session was peeked: %s", got)
	}
}

func TestPickerPeekDropsLateAnswerAndCachesBriefly(t *testing.T) {
	e := peekEnv(t)
	e.openPicker(t, 120, 30)
	stale := e.lastCmd // the request for r1, not delivered yet
	e.send(t, tea.KeyMsg{Type: tea.KeyDown})
	e.run(t, e.lastCmd)
	e.run(t, stale)
	view := e.pickerView()
	if !strings.Contains(view, "two a") || strings.Contains(view, "one a") {
		t.Fatalf("late answer for the previous selection leaked:\n%s", view)
	}
	// Back on r1 its dropped entry is asked for again; r2 stays cached.
	e.sendKey(t, tea.KeyUp)
	e.sendKey(t, tea.KeyDown)
	if got := strings.Join(e.fb.Sessions.Peeks(), ","); got != "r2,r1,r1" {
		t.Fatalf("peeks = %s", got)
	}

	now := time.Now()
	agentNow = func() time.Time { return now.Add(peekTTL + time.Second) }
	t.Cleanup(func() { agentNow = time.Now })
	e.sendKey(t, tea.KeyUp)
	if got := strings.Join(e.fb.Sessions.Peeks(), ","); got != "r2,r1,r1,r1" {
		t.Fatalf("expired entry not refreshed: %s", got)
	}
}
