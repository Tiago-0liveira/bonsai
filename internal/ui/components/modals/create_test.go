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
	m.SetCreateOptions(true, false, key.NewBinding(key.WithKeys(" "), key.WithHelp("space", "toggle")), key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "base")), "origin/main", func(name, base string) string { return "from " + base + " → /trees/" + name })
	m.SetInitial("feature")
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace})
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
