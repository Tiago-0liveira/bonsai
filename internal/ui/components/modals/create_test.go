package modals

import (
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"strings"
	"testing"
)

func TestCreateOptions(t *testing.T) {
	m := NewInput(KindCreateNew, "New branch", "branch")
	m.SetSize(100, 30)
	m.SetCreateOptions(true, false, key.NewBinding(key.WithKeys("ctrl+a"), key.WithHelp("space", "toggle")), key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "base")), "origin/main", func(name, base string) string { return "from " + base + " → /trees/" + name })
	m.SetInitial("feature")
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlA})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnd})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("-next")})
	if !strings.Contains(m.View(), "from origin/main-next → /trees/feature") {
		t.Fatal(m.View())
	}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := cmd().(SubmitMsg)
	if !got.StartAgent || got.Base != "origin/main-next" || got.Value != "feature" {
		t.Fatalf("%+v", got)
	}
}

func TestPrintableToggleDoesNotConsumeTyping(t *testing.T) {
	for _, binding := range []string{" ", "a"} {
		m := NewInput(KindCreateNew, "New branch", "branch")
		m.SetCreateOptions(true, false, key.NewBinding(key.WithKeys(binding)), key.NewBinding(key.WithKeys("tab")), "", nil)
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune(" ")})
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("b")})
		_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
		if got := cmd().(SubmitMsg); got.Value != "a b" || got.StartAgent {
			t.Fatalf("%+v", got)
		}
	}
}
