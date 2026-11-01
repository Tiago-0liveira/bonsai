package ui

import (
	"errors"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/ui/components/worktreelist"
	tea "github.com/charmbracelet/bubbletea"
)

func TestStatusExpiry(t *testing.T) {
	m := renderModel()
	m.list = worktreelist.New()
	m.status = "first"
	nm, cmd := m.Update(nil)
	m = nm.(Model)
	if cmd == nil || m.statusGen == 0 {
		t.Fatal("missing expiry command")
	}
	old := m.statusGen
	m.status = "second"
	nm, _ = m.Update(nil)
	m = nm.(Model)
	nm, _ = m.Update(statusClearMsg{old})
	m = nm.(Model)
	if m.status != "second" {
		t.Fatal("stale tick cleared new message")
	}
	m.err = errors.New("persistent error")
	nm, _ = m.Update(statusClearMsg{m.statusGen})
	m = nm.(Model)
	if m.status != "" || m.err == nil {
		t.Fatal("expiry must clear status and retain error")
	}
}

func TestStatusClearsOnActionKey(t *testing.T) {
	for _, tc := range []struct {
		name  string
		key   tea.KeyMsg
		clear bool
	}{
		{"unknown", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("~")}, false},
		{"focus", tea.KeyMsg{Type: tea.KeyTab}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := renderModel()
			m.list = worktreelist.New()
			m.status = "notice"
			m.err = errors.New("error")
			nm, _ := m.Update(tc.key)
			got := nm.(Model)
			if (got.status == "") != tc.clear || (got.err == nil) != tc.clear {
				t.Fatalf("status %q error %v", got.status, got.err)
			}
		})
	}
}

// Status expiry runs alongside mutations; these tests inspect only the mutation.
func statusPrimaryResult(cmd tea.Cmd) tea.Msg {
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		return batch[0]()
	}
	return msg
}
