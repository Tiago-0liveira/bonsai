package prefs

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func testActions() []Action {
	return []Action{
		{Name: "new_worktree", Desc: "new worktree", Default: "n", Section: "Worktree"},
		{Name: "prune", Desc: "prune", Default: "x", Section: "Worktree"},
		{Name: "rebase", Desc: "rebase", Default: "r", Section: "Git"},
		{Name: "restart_proc", Desc: "restart proc", Default: "r", Section: "Processes tab"},
	}
}

func testModel() Model {
	return New("bonsai", "name", false, "", []string{"bonsai", "sakura"}, testActions(), false)
}

// keyMsg simulates pressing a rune key.
func keyMsg(s string) tea.Msg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func saveFrom(t *testing.T, cmd tea.Cmd) *SaveMsg {
	t.Helper()
	if cmd == nil {
		return nil
	}
	if sm, ok := cmd().(SaveMsg); ok {
		return &sm
	}
	return nil
}

// moveToAction places the cursor on the key row of an action.
func moveToAction(m *Model, action string) {
	for i, r := range m.rows {
		if r.kind == rowKey && r.action.Name == action {
			m.cursor = i
			return
		}
	}
}

// headerIndex returns the row index of a header by its display name.
func headerIndex(m Model, name string) int {
	for i, r := range m.rows {
		if r.kind == rowHeader && r.header == name {
			return i
		}
	}
	return -1
}

func contains(vis []int, i int) bool {
	for _, v := range vis {
		if v == i {
			return true
		}
	}
	return false
}

func TestKeybindingsCollapsedByDefault(t *testing.T) {
	m := testModel()
	// Any key row lives under the (collapsed) Keybindings header, so it must
	// not be in the visible set on open.
	var keyRow int = -1
	for i, r := range m.rows {
		if r.kind == rowKey {
			keyRow = i
			break
		}
	}
	if keyRow == -1 {
		t.Fatal("expected a key row")
	}
	if contains(m.visibleRows(), keyRow) {
		t.Error("key rows should be hidden while Keybindings is collapsed")
	}
	// The Keybindings header itself is visible.
	if !contains(m.visibleRows(), headerIndex(m, "Keybindings")) {
		t.Error("Keybindings header should be visible")
	}
}

func TestStartOnKeysExpandsKeybindings(t *testing.T) {
	m := New("bonsai", "name", false, "", []string{"bonsai"}, testActions(), true)
	if m.rows[m.cursor].kind != rowKey {
		t.Fatal("startOnKeys should land the cursor on a key row")
	}
	if !contains(m.visibleRows(), m.cursor) {
		t.Error("the key row under the cursor should be visible when arriving via startOnKeys")
	}
}

func TestHeaderEnterTogglesCollapse(t *testing.T) {
	m := testModel()
	kb := headerIndex(m, "Keybindings")
	m.cursor = kb
	if !m.collapsed[kb] {
		t.Fatal("Keybindings should start collapsed")
	}
	// Enter expands it; its sub-headers become visible.
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if nm.collapsed[kb] {
		t.Error("enter on a collapsed header should expand it")
	}
	sub := headerIndex(nm, "  Git")
	if sub == -1 || !contains(nm.visibleRows(), sub) {
		t.Error("sub-headers should be visible after expanding Keybindings")
	}
}

func TestHeaderArrowsCollapseExpand(t *testing.T) {
	m := testModel()
	appr := headerIndex(m, "Appearance")
	m.cursor = appr
	// left collapses.
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	if !nm.collapsed[appr] {
		t.Error("left should collapse the header")
	}
	// right expands.
	nm2, _ := nm.Update(tea.KeyMsg{Type: tea.KeyRight})
	if nm2.collapsed[appr] {
		t.Error("right should expand the header")
	}
}

func TestPresetChangeEmitsSave(t *testing.T) {
	m := testModel()
	m.cursor = 2 // sakura row
	nm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	snap := saveFrom(t, cmd)
	if snap == nil {
		t.Fatal("selecting a preset should emit a SaveMsg")
	}
	if snap.Theme != "sakura" {
		t.Errorf("snapshot theme = %q, want sakura", snap.Theme)
	}
	if nm.theme != "sakura" {
		t.Error("working theme should update immediately (live preview)")
	}
}

func TestRebindConflictingKeyRejected(t *testing.T) {
	m := testModel()
	moveToAction(&m, "prune")
	if m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}); m.capture != "prune" {
		t.Fatalf("capture = %q, want prune", m.capture)
	}
	// "n" is held by new_worktree.
	nm, cmd := m.Update(keyMsg("n"))
	if cmd != nil {
		t.Error("conflicting rebind should not save")
	}
	if nm.overrides["prune"] != "" {
		t.Error("conflicting rebind should not be recorded")
	}
	if nm.capture != "prune" {
		t.Error("capture should stay active after a conflict")
	}
}

func TestRebindOwnDefaultAllowedDespiteOverlap(t *testing.T) {
	m := testModel()
	m.overrides["rebase"] = "g" // user moved rebase away earlier
	moveToAction(&m, "rebase")
	if m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}); m.capture != "rebase" {
		t.Fatalf("capture = %q, want rebase", m.capture)
	}
	// "r" is restart_proc's key too, but it is rebase's own default.
	nm, cmd := m.Update(keyMsg("r"))
	snap := saveFrom(t, cmd)
	if snap == nil {
		t.Fatal("restoring the own default should save")
	}
	if _, still := snap.Keys["rebase"]; still {
		t.Error("restoring the default should clear the personal override")
	}
	if nm.capture != "" {
		t.Error("capture should end after a successful rebind")
	}
}

func TestReservedKeyRejected(t *testing.T) {
	m := testModel()
	moveToAction(&m, "prune")
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	nm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if cmd != nil {
		t.Error("reserved key should not save")
	}
	if nm.overrides["prune"] != "" {
		t.Error("reserved key should not be recorded")
	}
}

func TestBackspaceResetsOverride(t *testing.T) {
	m := testModel()
	m.overrides["prune"] = "z"
	moveToAction(&m, "prune")
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // capture
	nm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	snap := saveFrom(t, cmd)
	if snap == nil {
		t.Fatal("reset should emit a SaveMsg")
	}
	if _, still := snap.Keys["prune"]; still {
		t.Error("reset should drop the override from the snapshot")
	}
	if _, still := nm.overrides["prune"]; still {
		t.Error("reset should drop the override from the working copy")
	}
}

func TestEscapeCloses(t *testing.T) {
	m := testModel()
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("esc should emit a command")
	}
	if _, ok := cmd().(CloseMsg); !ok {
		t.Error("esc should emit CloseMsg")
	}
}

func TestSortCycle(t *testing.T) {
	m := testModel()
	for i, r := range m.rows {
		if r.kind == rowSort {
			m.cursor = i
		}
	}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRight})
	snap := saveFrom(t, cmd)
	if snap == nil || snap.Sort != "ahead" {
		t.Errorf("right on sort should cycle name→ahead, got %+v", snap)
	}
}

func TestPRStatusCycle(t *testing.T) {
	m := testModel()
	if m.prStatus != "full" {
		t.Fatalf("default PR status = %q, want full", m.prStatus)
	}
	for i, r := range m.rows {
		if r.kind == rowPRStatus {
			m.cursor = i
		}
	}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRight})
	snap := saveFrom(t, cmd)
	if snap == nil || snap.PRStatus != "compact" {
		t.Errorf("right on PR status should cycle full→compact, got %+v", snap)
	}
}
