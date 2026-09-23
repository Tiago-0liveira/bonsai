package ui

import (
	"errors"
	"github.com/Tiago-0liveira/bonsai/internal/core/updater"
	tea "github.com/charmbracelet/bubbletea"
	"strings"
	"testing"
)

func TestUpdatePrompt(t *testing.T) {
	m := Model{ready: true, height: 24}
	next, _ := m.Update(updateAvailableMsg{updater.Release{Tag: "v9.0.0"}})
	m = next.(Model)
	if !strings.Contains(m.View(), "[u] Update") {
		t.Fatal(m.View())
	}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'u'}})
	m = next.(Model)
	if cmd == nil || !m.updating {
		t.Fatal("update did not start")
	}
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'u'}})
	if cmd != nil {
		t.Fatal("started duplicate update")
	}
	next, _ = m.Update(updateInstalledMsg{err: errors.New("offline")})
	m = next.(Model)
	if m.updatePromptVisible() || m.updating || m.err == nil {
		t.Fatal("failure did not return to TUI")
	}
	m.availableUpdate.Tag = "v9.0.0"
	next, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	m = next.(Model)
	if cmd != nil || m.updatePromptVisible() {
		t.Fatal("Later did not dismiss prompt")
	}
}
