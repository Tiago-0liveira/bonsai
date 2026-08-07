package modals

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func runeKey(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}

// submitValue presses enter on m and extracts the submitted value.
func submitValue(t *testing.T, m Model) string {
	t.Helper()
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter returned no command")
	}
	msg, ok := cmd().(SubmitMsg)
	if !ok {
		t.Fatalf("enter returned %T, want SubmitMsg", msg)
	}
	return msg.Value
}

func TestPruneForceToggleAndSubmit(t *testing.T) {
	m := NewPrune(KindPrune, "Prune feat?", "merge PR first", []string{"delete worktree", "delete branch"}, true)

	if m.ForceEnabled() {
		t.Fatal("force should start off")
	}

	if got := submitValue(t, m); got != "confirm" {
		t.Errorf("no toggles: submit = %q, want confirm", got)
	}

	forced, _ := m.Update(runeKey('f'))
	if !forced.ForceEnabled() {
		t.Fatal("f should toggle force on")
	}
	if got := submitValue(t, forced); got != "force" {
		t.Errorf("force only: submit = %q, want force", got)
	}
	forcedOff, _ := forced.Update(runeKey('f'))
	if forcedOff.ForceEnabled() {
		t.Fatal("f again should toggle force off")
	}

	merged, _ := m.Update(runeKey('m'))
	if !merged.MergeEnabled() {
		t.Fatal("m should toggle merge on")
	}
	if got := submitValue(t, merged); got != "merge" {
		t.Errorf("merge only: submit = %q, want merge", got)
	}

	both, _ := merged.Update(runeKey('f'))
	if got := submitValue(t, both); got != "merge,force" {
		t.Errorf("merge+force: submit = %q, want merge,force", got)
	}
}

func TestPruneForceRendered(t *testing.T) {
	m := NewPrune(KindPrune, "Prune?", "", []string{"delete"}, false)
	if out := m.renderPrune(); !strings.Contains(out, "force") {
		t.Errorf("renderPrune should show the force option:\n%s", out)
	}
}
