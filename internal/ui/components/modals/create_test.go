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

func TestNameCheckShowsReasonAndBlocksEnter(t *testing.T) {
	check := func(name string) (string, string) {
		if strings.Contains(name, " ") {
			return "spaces are not allowed", strings.ReplaceAll(name, " ", "-")
		}
		return "", ""
	}
	m := NewInput(KindCreateNew, "New branch", "branch")
	m.SetSize(100, 30)
	m.SetCreateOptions(false, false, key.NewBinding(key.WithKeys("ctrl+a")), key.NewBinding(key.WithKeys("tab")), "main", func(name, base string) string { return "PREVIEW " + name })
	m.SetNameCheck(check)

	// Empty: nothing to show, nothing to submit.
	if _, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter}); cmd != nil {
		t.Fatal("enter on an empty name must do nothing")
	}

	m.SetInitial("my test")
	view := m.View()
	if !strings.Contains(view, "spaces are not allowed") || !strings.Contains(view, "try my-test") || strings.Contains(view, "PREVIEW") {
		t.Fatalf("invalid name must show the reason and no preview:\n%s", view)
	}
	if _, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter}); cmd != nil {
		t.Fatal("enter on an invalid name must do nothing")
	}

	m.SetInitial("my-test")
	if view := m.View(); strings.Contains(view, "spaces are not allowed") || !strings.Contains(view, "PREVIEW my-test") {
		t.Fatalf("valid name must show the preview:\n%s", view)
	}
	if got := submitValue(t, m); got != "my-test" {
		t.Fatalf("submit = %q", got)
	}
}
