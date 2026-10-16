package modals

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

const kindAgentsTest Kind = "agents"

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

func sectionFixture() SectionFilterFunc {
	all := []Section{
		{Name: "Git", Items: []string{"pull", "push", "commit"}},
		{Name: "Tabs", Items: []string{"log", "diff"}},
	}
	return func(q string) []Section {
		out := make([]Section, 0, len(all))
		for _, s := range all {
			var items []string
			for _, it := range s.Items {
				if q == "" || strings.Contains(it, q) {
					items = append(items, it)
				}
			}
			out = append(out, Section{Name: s.Name, Items: items})
		}
		return out
	}
}

func TestSectionedOpensCollapsed(t *testing.T) {
	f := sectionFixture()
	m := NewSectionedFuzzy(KindPalette, "Commands", f, f(""))
	out := m.renderSections()
	if !strings.Contains(out, "Git (3)") || !strings.Contains(out, "Tabs (2)") {
		t.Errorf("headers with counts should show:\n%s", out)
	}
	if strings.Contains(out, "pull") || strings.Contains(out, "log") {
		t.Errorf("items should be hidden while collapsed:\n%s", out)
	}
}

func TestSectionedToggleExpands(t *testing.T) {
	f := sectionFixture()
	m := NewSectionedFuzzy(KindPalette, "Commands", f, f(""))
	// Cursor starts on the first header (Git). Enter expands it.
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	out := m2.renderSections()
	if !strings.Contains(out, "pull") {
		t.Errorf("expanded Git should show items:\n%s", out)
	}
	if strings.Contains(out, "log") {
		t.Errorf("Tabs should stay collapsed:\n%s", out)
	}
}

func TestSectionedQueryAutoExpandsAndHides(t *testing.T) {
	f := sectionFixture()
	m := NewSectionedFuzzy(KindPalette, "Commands", f, f(""))
	// Type "l": matches "pull" (Git) and "log" (Tabs); Tabs keeps "log", Git keeps "pull".
	typed, _ := m.Update(runeKey('l'))
	out := typed.renderSections()
	if !strings.Contains(out, "pull") || !strings.Contains(out, "log") {
		t.Errorf("query should auto-expand matching items:\n%s", out)
	}
	if strings.Contains(out, "commit") || strings.Contains(out, "diff") {
		t.Errorf("non-matching items should be hidden:\n%s", out)
	}
	// Enter on the first item submits it.
	if got := submitValue(t, typed); got != "pull" {
		t.Errorf("enter on item: submit = %q, want pull", got)
	}
}

func TestSectionedQueryHidesEmptySections(t *testing.T) {
	f := sectionFixture()
	m := NewSectionedFuzzy(KindPalette, "Commands", f, f(""))
	typed, _ := m.Update(runeKey('d')) // only "diff" in Tabs matches
	out := typed.renderSections()
	if strings.Contains(out, "Git") {
		t.Errorf("empty Git section should be hidden under query:\n%s", out)
	}
	if !strings.Contains(out, "Tabs") || !strings.Contains(out, "diff") {
		t.Errorf("Tabs/diff should show:\n%s", out)
	}
}

func TestPruneForceRendered(t *testing.T) {
	m := NewPrune(KindPrune, "Prune?", "", []string{"delete"}, false)
	if out := m.renderPrune(); !strings.Contains(out, "force") {
		t.Errorf("renderPrune should show the force option:\n%s", out)
	}
}

func TestPickerKeys(t *testing.T) {
	all := []string{"alpha", "beta", "gamma"}
	filter := func(q string) []string {
		var out []string
		for _, s := range all {
			if strings.Contains(s, q) {
				out = append(out, s)
			}
		}
		return out
	}
	press := func(m Model, k tea.KeyMsg) (Model, tea.Msg) {
		m, cmd := m.Update(k)
		if cmd == nil {
			return m, nil
		}
		return m, cmd()
	}
	runes := func(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }
	m := NewPicker(kindAgentsTest, "T", filter, all, "FOOT", "nothing here", "n")

	t.Run("action key fires while search is closed", func(t *testing.T) {
		_, msg := press(m, runes("n"))
		if a, ok := msg.(ActionMsg); !ok || a.Key != "n" || a.Kind != kindAgentsTest {
			t.Errorf("msg = %#v", msg)
		}
	})
	t.Run("slash opens search and keys become text", func(t *testing.T) {
		// Updates are applied without running the returned commands: the text
		// field's are cursor blinks, which wait on a timer.
		m2, _ := m.Update(runes("/"))
		m2, _ = m2.Update(runes("n"))
		if m2.input.Value() != "n" {
			t.Errorf("n while searching must be text, got %q", m2.input.Value())
		}
		m2, _ = m2.Update(tea.KeyMsg{Type: tea.KeyBackspace})
		m2, _ = m2.Update(runes("b"))
		if got := m2.items; len(got) != 1 || got[0] != "beta" {
			t.Errorf("items = %v", got)
		}
		m2, _ = m2.Update(tea.KeyMsg{Type: tea.KeyEsc})
		if m2.searching || len(m2.items) != 3 {
			t.Errorf("esc closes search and clears the query: searching=%v items=%v", m2.searching, m2.items)
		}
		if _, msg := press(m2, tea.KeyMsg{Type: tea.KeyEsc}); msg == nil {
			t.Error("esc with search closed cancels")
		} else if _, ok := msg.(CancelMsg); !ok {
			t.Errorf("msg = %#v", msg)
		}
	})
	t.Run("enter submits the cursor row", func(t *testing.T) {
		m2, _ := press(m, tea.KeyMsg{Type: tea.KeyDown})
		_, msg := press(m2, tea.KeyMsg{Type: tea.KeyEnter})
		if s, ok := msg.(SubmitMsg); !ok || s.Value != "beta" {
			t.Errorf("msg = %#v", msg)
		}
	})
	t.Run("view shows footer and empty text", func(t *testing.T) {
		if v := ansi.Strip(m.View()); !strings.Contains(v, "FOOT") || strings.Contains(v, "type to filter") {
			t.Errorf("view:\n%s", v)
		}
		e := NewPicker(kindAgentsTest, "T", filter, nil, "FOOT", "nothing here")
		if v := ansi.Strip(e.View()); !strings.Contains(v, "nothing here") {
			t.Errorf("empty view:\n%s", v)
		}
	})
}
