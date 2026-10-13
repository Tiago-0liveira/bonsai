package cli

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
)

func TestServeTUIFiltersLogs(t *testing.T) {
	m := serveTUIModel{
		group:    &procstore.ServeGroup{Processes: []procstore.ServeProcess{{Name: "api"}, {Name: "webhook"}}},
		selected: 1,
		search:   "denied",
		height:   40,
		lines: []string{
			"[10:00:00] api OUT ready",
			"[10:00:01] webhook ERR denied request",
			"[10:00:02] webhook OUT accepted",
		},
	}
	got := m.visibleLines()
	if len(got) != 1 || got[0] != "[10:00:01] webhook ERR denied request" {
		t.Fatalf("visible = %#v", got)
	}
}

func TestServeTUIGuardedStopNeedsASecondPress(t *testing.T) {
	stops := 0
	m := serveTUIModel{group: &procstore.ServeGroup{}, cancel: func() {}, guard: &serveStopGuard{
		warning: func() string { return "1 agent still running will be stopped." },
		stop:    func() error { stops++; return nil },
	}}
	press := func(m serveTUIModel, key string) (serveTUIModel, tea.Cmd) {
		next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
		return next.(serveTUIModel), cmd
	}
	m, cmd := press(m, "X")
	if cmd != nil || !strings.Contains(m.status, "Press X again") {
		t.Fatalf("first X: %q", m.status)
	}
	// Any other key disarms it.
	m, _ = press(m, "?")
	if m, cmd = press(m, "X"); cmd != nil {
		t.Fatal("X after another key stopped without a warning")
	}
	m, cmd = press(m, "X")
	if cmd == nil {
		t.Fatal("second X did not stop")
	}
	if msg, ok := cmd().(serveStoppedMsg); !ok || msg.err != nil || stops != 1 {
		t.Fatalf("stop ran %d times: %+v", stops, msg)
	}
}
