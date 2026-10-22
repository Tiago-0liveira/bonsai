package ui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/Tiago-0liveira/bonsai/internal/ui/components/modals"
	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
)

// keyFieldPos finds an action's section and field index in the Keys category.
func keyFieldPos(t *testing.T, action string) (sec, field int) {
	t.Helper()
	for si, s := range keymapSections {
		for fi, a := range s.actions {
			if a == action {
				return si, fi
			}
		}
	}
	t.Fatalf("no action %q", action)
	return 0, 0
}

func openKeyField(t *testing.T, m Model, action string) Model {
	t.Helper()
	sec, field := keyFieldPos(t, action)
	return openSettings(t, m, catKeys, sec, field)
}

func sendMsg(t *testing.T, m Model, msg tea.KeyMsg) Model {
	t.Helper()
	m, cmd := send(t, m, msg)
	return settle(t, m, cmd)
}

var (
	keyCtrlY = tea.KeyMsg{Type: tea.KeyCtrlY}
	keyCtrlU = tea.KeyMsg{Type: tea.KeyCtrlU}
	keyCtrlP = tea.KeyMsg{Type: tea.KeyCtrlP}
	keyCtrlK = tea.KeyMsg{Type: tea.KeyCtrlK}
	keyCtrlO = tea.KeyMsg{Type: tea.KeyCtrlO}
	keyBksp  = tea.KeyMsg{Type: tea.KeyBackspace}
	keyTabK  = tea.KeyMsg{Type: tea.KeyTab}
)

func TestKeysCategoryLayout(t *testing.T) {
	e := newSettingsEnv(t)
	m := openKeyField(t, e.model(t, 120, 35), "palette")
	var names []string
	for _, s := range settingCategories[catKeys].sections {
		names = append(names, s.name)
	}
	want := []string{"Navigation", "Worktree", "Tabs", "Files & clipboard", "Run", "Git", "Agents tab", "Processes tab", "Garden", "Terminal"}
	if strings.Join(names, "|") != strings.Join(want, "|") {
		t.Errorf("sections = %v", names)
	}
	view := ansi.Strip(m.View())
	for _, w := range []string{"Scope: This machine", "[ctrl+k]", "built-in default", "Settings / Keys / Navigation"} {
		if !strings.Contains(view, w) {
			t.Errorf("Keys view lacks %q:\n%s", w, view)
		}
	}
}

func TestKeyCaptureCancelLeavesEverything(t *testing.T) {
	e := newSettingsEnv(t)
	m := openKeyField(t, e.model(t, 120, 35), "pull")
	m = press(t, m, "enter")
	if !m.set.capturing || m.shellFocus != shellInput {
		t.Fatal("enter did not start capture")
	}
	if !strings.Contains(ansi.Strip(m.View()), "Press a new key for pull") {
		t.Errorf("no capture prompt:\n%s", ansi.Strip(m.View()))
	}
	m = press(t, m, "esc")
	if m.set.capturing || m.shellFocus != shellBody || m.set.level != levelField || m.prefsGen != 0 || len(m.work.Keys) != 0 {
		t.Errorf("cancel changed state: %+v", m.set)
	}
}

func TestKeyCaptureBindResetAndDefault(t *testing.T) {
	e := newSettingsEnv(t)
	m := openKeyField(t, e.model(t, 120, 35), "pull")

	m = sendMsg(t, press(t, m, "enter"), keyCtrlY)
	if m.work.Keys["pull"] != "ctrl+y" || diskPrefs(t).Keys["pull"] != "ctrl+y" || m.keys.Pull.Help().Key != "ctrl+y" || m.status != "Saved" {
		t.Fatalf("bind failed: work=%v disk=%v key=%s status=%q", m.work.Keys, diskPrefs(t).Keys, m.keys.Pull.Help().Key, m.status)
	}
	if !strings.Contains(ansi.Strip(m.View()), "[ctrl+y]") {
		t.Error("chip does not show the new key")
	}
	// A fresh session reloads it.
	if m2 := e.model(t, 80, 24); m2.keys.Pull.Help().Key != "ctrl+y" {
		t.Error("override not reloaded from state.json")
	}

	// Backspace resets; the map empties to nil so omitempty drops it.
	m = sendMsg(t, press(t, m, "enter"), keyBksp)
	if m.work.Keys != nil || diskPrefs(t).Keys != nil || m.keys.Pull.Help().Key != "ctrl+p" {
		t.Errorf("reset failed: work=%v disk=%v", m.work.Keys, diskPrefs(t).Keys)
	}
	// Reset at default says so and writes nothing.
	gen := m.prefsGen
	m = sendMsg(t, press(t, m, "enter"), keyBksp)
	if m.prefsGen != gen || !strings.Contains(m.status, "already at its default") {
		t.Errorf("reset at default: gen %d->%d status %q", gen, m.prefsGen, m.status)
	}

	// Pressing the default restores it.
	m = sendMsg(t, press(t, m, "enter"), keyCtrlY)
	m = sendMsg(t, press(t, m, "enter"), keyCtrlP)
	if m.work.Keys != nil || m.keys.Pull.Help().Key != "ctrl+p" || m.shellFocus != shellBody {
		t.Errorf("default press did not restore: %v", m.work.Keys)
	}
}

func TestKeyCaptureDefaultRestoresEvenWhenFixed(t *testing.T) {
	e := newSettingsEnv(t)
	m := openKeyField(t, e.model(t, 120, 35), "shell") // default enter is a fixed key
	m = sendMsg(t, press(t, m, "enter"), keyCtrlY)
	if m.work.Keys["shell"] != "ctrl+y" {
		t.Fatalf("setup bind failed: %q", m.set.editErr)
	}
	m = sendMsg(t, press(t, m, "enter"), tea.KeyMsg{Type: tea.KeyEnter})
	if m.work.Keys != nil || m.set.capturing {
		t.Errorf("default enter not accepted: %v capturing=%v", m.work.Keys, m.set.capturing)
	}
}

func TestKeyCaptureRejections(t *testing.T) {
	tests := []struct {
		name   string
		action string
		key    tea.KeyMsg
		want   string
	}{
		{"reserved tab", "pull", keyTabK, "reserved"},
		{"reserved esc is cancel not bind", "", tea.KeyMsg{}, ""},
		{"same scope", "pull", keyCtrlU, "already used by 'push' (Git)"},
		{"global from scoped", "pull", keyCtrlK, "already used by 'commands' (Navigation)"},
		{"scoped from global", "palette", keyCtrlP, "already used by 'pull' (Git)"},
		{"workspace jump", "pull", tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("3"), Alt: true}, "workspaces"},
	}
	for _, tc := range tests {
		if tc.action == "" {
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			e := newSettingsEnv(t)
			m := openKeyField(t, e.model(t, 120, 35), tc.action)
			m = sendMsg(t, press(t, m, "enter"), tc.key)
			if !m.set.capturing || m.shellFocus != shellInput || !strings.Contains(m.set.editErr, tc.want) {
				t.Fatalf("capturing=%v err=%q, want %q", m.set.capturing, m.set.editErr, tc.want)
			}
			if m.prefsGen != 0 || len(m.work.Keys) != 0 {
				t.Error("a rejected key changed prefs")
			}
			if !strings.Contains(ansi.Strip(m.View()), tc.want) {
				t.Errorf("rejection not shown:\n%s", ansi.Strip(m.View()))
			}
			// A valid key afterwards still works in the same capture.
			m = sendMsg(t, m, keyCtrlY)
			if m.set.capturing || m.work.Keys[tc.action] != "ctrl+y" {
				t.Errorf("capture did not recover: %v", m.work.Keys)
			}
		})
	}
}

func TestKeyCrossScopeReuseAllowed(t *testing.T) {
	e := newSettingsEnv(t)
	m := openKeyField(t, e.model(t, 120, 35), "kill_proc")
	m = sendMsg(t, press(t, m, "enter"), keyCtrlU) // push uses it in the worktree scope
	if m.work.Keys["kill_proc"] != "ctrl+u" || diskPrefs(t).Keys["kill_proc"] != "ctrl+u" {
		t.Errorf("cross-scope reuse rejected: %q", m.set.editErr)
	}
}

func TestKeyConflictUsesMergedView(t *testing.T) {
	e := newSettingsEnv(t)
	if err := os.WriteFile(filepath.Join(e.repo, ".bonsai.yaml"), []byte("keys:\n  push: ctrl+t\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := openKeyField(t, e.model(t, 120, 35), "pull")
	// ctrl+u was freed by the repo override; ctrl+t is taken by it.
	m = sendMsg(t, press(t, m, "enter"), tea.KeyMsg{Type: tea.KeyCtrlT})
	if !strings.Contains(m.set.editErr, "'push'") {
		t.Errorf("repo override ignored: %q", m.set.editErr)
	}
	m = sendMsg(t, m, keyCtrlU)
	if m.work.Keys["pull"] != "ctrl+u" {
		t.Errorf("freed key refused: %q", m.set.editErr)
	}
	// The reset target is the repo value, and the row says where it comes from.
	pm := openKeyField(t, m, "push")
	if !strings.Contains(ansi.Strip(pm.View()), "repo .bonsai.yaml") {
		t.Errorf("origin not shown:\n%s", ansi.Strip(pm.View()))
	}
}

func TestKeyCaptureSaveFailureRestores(t *testing.T) {
	e := newSettingsEnv(t)
	m := openKeyField(t, e.model(t, 120, 35), "pull")
	dir := filepath.Join(e.xdg, "bonsai")
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	m = sendMsg(t, press(t, m, "enter"), keyCtrlY)
	if m.work.Keys != nil || m.keys.Pull.Help().Key != "ctrl+p" || !strings.Contains(m.set.saveErr, "Save failed") || m.status == "Saved" {
		t.Errorf("failure not rolled back: keys=%v key=%s err=%q", m.work.Keys, m.keys.Pull.Help().Key, m.set.saveErr)
	}
}

func TestTerminalFixedRowIsNotEditable(t *testing.T) {
	e := newSettingsEnv(t)
	m := openSettings(t, e.model(t, 120, 35), catKeys, len(keymapSections), 0)
	if !strings.Contains(ansi.Strip(m.View()), "ctrl+] (fixed)") {
		t.Fatalf("fixed row missing:\n%s", ansi.Strip(m.View()))
	}
	for _, k := range []string{"enter", "left", "right"} {
		var cmd tea.Cmd
		m, cmd = send(t, m, keyOf(k))
		if cmd != nil || m.set.capturing || m.set.editing || m.shellFocus != shellBody || m.prefsGen != 0 {
			t.Errorf("%s changed the fixed row", k)
		}
	}
}

func TestKeyOverridesDriveTheShell(t *testing.T) {
	e := newSettingsEnv(t)
	e.writeState(t, `{"prefs":{"keys":{"palette":"ctrl+o","focus_next":"N"}}}`)
	m := e.model(t, 120, 35)
	if v := ansi.Strip(m.View()); !strings.Contains(v, "ctrl+o commands") || !strings.Contains(v, "N/shift+tab next/prev") {
		t.Errorf("footer lacks personal keys:\n%s", v)
	}
	if m2, _ := send(t, m, keyCtrlK); m2.modal != nil {
		t.Error("old palette key still works")
	}
	if m2, _ := send(t, m, keyCtrlO); m2.modal == nil {
		t.Error("new palette key does not open it")
	}
	if m2, _ := send(t, m, runeKey("N")); m2.workspace != wsPlayground {
		t.Error("Tab override does not cycle workspaces")
	}
	if m2, _ := send(t, m, keyTabK); m2.workspace != wsGarden {
		t.Error("old Tab still cycles")
	}
}

func TestKeyRebindTakesEffectLive(t *testing.T) {
	e := newSettingsEnv(t)
	m := openKeyField(t, e.model(t, 120, 35), "palette")
	m = sendMsg(t, press(t, m, "enter"), keyCtrlO)
	m = press(t, m, "esc", "esc", "esc") // back to the tabs
	if m.shellFocus != shellNav {
		t.Fatalf("focus = %s", m.shellFocus)
	}
	if m2, _ := send(t, m, keyCtrlK); m2.modal != nil {
		t.Error("old key opens the palette after rebinding")
	}
	if m2, _ := send(t, m, keyCtrlO); m2.modal == nil {
		t.Error("new key does not open the palette")
	}
	if !strings.Contains(ansi.Strip(m.View()), "ctrl+o commands") {
		t.Error("footer still shows the old key")
	}
}

func TestPaletteEntriesRouteToSettingsCategories(t *testing.T) {
	e := newSettingsEnv(t)
	base := e.model(t, 120, 35)
	opened, _ := send(t, base, keyCtrlK)
	view := ansi.Strip(opened.View())
	for _, w := range []string{"Go to Garden", "Settings: General", "Settings: Appearance", "Settings: Keys", "Settings: Garden", "Settings: Agents", "Settings: Edit key bindings"} {
		if !strings.Contains(view, w) {
			t.Errorf("palette lacks %q", w)
		}
	}
	for label, cat := range map[string]int{
		"Settings: General": catGeneral, "Settings: Appearance": catAppearance, "Settings: Keys": catKeys,
		"Settings: Garden": catGarden, "Settings: Agents": catAgents, "Settings: Edit key bindings": catKeys,
	} {
		m, _ := send(t, base, keyCtrlK)
		m, cmd := send(t, m, modals.SubmitMsg{Kind: modals.KindPalette, Value: label})
		if m.workspace != wsSettings || m.shellFocus != shellBody || m.set.level != levelCategory || m.set.cat != cat || m.modal != nil {
			t.Errorf("%s: ws=%s focus=%s level=%d cat=%d", label, m.workspace.name(), m.shellFocus, m.set.level, m.set.cat)
		}
		if cmd == nil {
			t.Fatalf("%s: workspace change was not persisted", label)
		}
		if _, ok := cmd().(workspaceSavedMsg); !ok {
			t.Errorf("%s: palette started domain work", label)
		}
		if got := e.st.LastWorkspace(e.repo); got != "settings" {
			t.Errorf("%s: remembered %q", label, got)
		}
	}
}

func TestPaletteFilterFindsSettingsKeys(t *testing.T) {
	e := newSettingsEnv(t)
	m := e.model(t, 120, 35)
	m, _ = send(t, m, keyCtrlK)
	for _, r := range "bindings" {
		m, _ = send(t, m, runeKey(string(r)))
	}
	if v := ansi.Strip(m.View()); !strings.Contains(v, "Edit key bindings") || strings.Contains(v, "Go to Garden") {
		t.Errorf("filter wrong:\n%s", v)
	}
	m, cmd := send(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m, _ = send(t, m, cmd())
	if m.workspace != wsSettings || m.set.cat != catKeys {
		t.Errorf("enter on filtered entry: ws=%s cat=%d", m.workspace.name(), m.set.cat)
	}
}

func TestSettingsSelectionStylesDiffer(t *testing.T) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(old) })

	e := newSettingsEnv(t)
	m := openSettings(t, e.model(t, 120, 35), catAppearance, 0, 0)
	field := m.fieldRow(90, m.curFields()[0], true, true)
	plainField := m.fieldRow(90, m.curFields()[0], false, true)

	m.set.level = levelCategory
	catActive := m.settingsSubmenu(120)[1]
	m.set.level = levelSection
	catParent := m.settingsSubmenu(120)[1]
	secActive := m.sectionPanel(settingsPanelW, 8, m.curCategory())[2]
	otherSec := m.sectionPanel(settingsPanelW, 8, m.curCategory())[4]

	distinct := map[string]string{
		"category (active)": catActive, "category (parent)": catParent,
		"section (selected)": secActive, "section (other)": otherSec,
		"field (selected)": field, "field (other)": plainField,
	}
	seen := map[string]string{}
	for name, row := range distinct {
		if !strings.Contains(row, "\x1b[") {
			t.Fatalf("%s rendered without colour", name)
		}
		// Compare styling only: strip the text so equal labels cannot mask it.
		style := strings.ReplaceAll(row, ansi.Strip(row), "")
		if prev, dup := seen[style]; dup {
			t.Errorf("%s and %s share the same styling", name, prev)
		}
		seen[style] = name
	}
}

func TestKeysCaptureViewIsBounded(t *testing.T) {
	for _, ascii := range []bool{false, true} {
		for _, sz := range [][2]int{{80, 24}, {120, 35}, {160, 48}, {64, 14}} {
			e := newSettingsEnv(t)
			if ascii {
				e.writeState(t, `{"prefs":{"agent_glyphs":"ascii"}}`)
			}
			m := sized(t, openKeyField(t, e.model(t, 80, 24), "pull"), sz[0], sz[1])
			m = press(t, m, "enter")
			assertSettingsView(t, "capturing", m, sz[0], sz[1], ascii)
			m = sendMsg(t, m, keyCtrlU)
			assertSettingsView(t, "rejected", m, sz[0], sz[1], ascii)
		}
	}
}

func TestSettingsRollbackWaitsForOlderSaves(t *testing.T) {
	e := newSettingsEnv(t)
	m := openSettings(t, e.model(t, 120, 35), catAppearance, 0, 0)
	m, cmd1 := send(t, m, keyOf("right")) // gen 1: bonsai
	m, _ = send(t, m, keyOf("right"))     // gen 2: sakura
	// Gen 2 fails before gen 1 has run.
	m, _ = send(t, m, settingsSavedMsg{gen: 2, err: errors.New("disk full")})
	if m.work.Theme != "sakura" || !m.set.pendingRollback || !strings.Contains(m.set.saveErr, "disk full") {
		t.Fatalf("rollback must wait for the in-flight save: theme=%q pending=%v", m.work.Theme, m.set.pendingRollback)
	}
	// The older save lands afterwards and becomes the on-disk truth.
	m, _ = send(t, m, cmd1())
	if diskPrefs(t).Theme != "bonsai" {
		t.Fatalf("disk theme = %q", diskPrefs(t).Theme)
	}
	if m.work.Theme != "bonsai" || m.set.pendingRollback || theme.Current.Accent != theme.Resolve("bonsai", nil).Accent {
		t.Errorf("UI != disk after rollback: work=%q pending=%v", m.work.Theme, m.set.pendingRollback)
	}
}

func TestSettingsNewerEditClearsPendingRollback(t *testing.T) {
	e := newSettingsEnv(t)
	m := openSettings(t, e.model(t, 120, 35), catAppearance, 0, 0)
	m, _ = send(t, m, keyOf("right"))
	m, _ = send(t, m, keyOf("right"))
	m, _ = send(t, m, settingsSavedMsg{gen: 2, err: errors.New("x")})
	m, cmd3 := send(t, m, keyOf("right")) // gen 3
	if m.set.pendingRollback {
		t.Fatal("a newer edit must clear the pending rollback")
	}
	m, _ = send(t, m, settingsSavedMsg{gen: 1})
	m, _ = send(t, m, settingsSavedMsg{gen: 2, err: errors.New("x")})
	if m.work.Theme != "dracula" {
		t.Errorf("stale results disturbed the newer edit: %q", m.work.Theme)
	}
	m, _ = send(t, m, cmd3())
	if m.status != "Saved" || diskPrefs(t).Theme != "dracula" {
		t.Errorf("status=%q disk=%q", m.status, diskPrefs(t).Theme)
	}
}

func TestKeyRestoreRechecksScopeConflicts(t *testing.T) {
	e := newSettingsEnv(t)
	// push moved off ctrl+u, then kill_proc... no: pull takes push's old key,
	// so restoring push to ctrl+u would collide in the worktree scope.
	e.writeState(t, `{"prefs":{"keys":{"push":"ctrl+t","pull":"ctrl+u"}}}`)
	m := openKeyField(t, e.model(t, 120, 35), "push")
	for _, k := range []tea.KeyMsg{keyBksp, keyCtrlU} {
		m = sendMsg(t, press(t, m, "enter"), k)
		if !m.set.capturing || !strings.Contains(m.set.editErr, "already used by 'pull'") || m.work.Keys["push"] != "ctrl+t" {
			t.Fatalf("restore over a conflict accepted: capturing=%v err=%q keys=%v", m.set.capturing, m.set.editErr, m.work.Keys)
		}
		m = press(t, m, "esc")
	}
}

func TestKeyRestoreOfReservedDefaultStillAllowed(t *testing.T) {
	e := newSettingsEnv(t)
	e.writeState(t, `{"prefs":{"keys":{"shell":"ctrl+t"}}}`)
	m := openKeyField(t, e.model(t, 120, 35), "shell")
	m = sendMsg(t, press(t, m, "enter"), keyBksp)
	if m.work.Keys != nil {
		t.Errorf("reserved default not restorable: %q", m.set.editErr)
	}
}

func TestKeyCaptureRejectsPastesAndMultiRune(t *testing.T) {
	e := newSettingsEnv(t)
	m := openKeyField(t, e.model(t, 120, 35), "pull")
	m = press(t, m, "enter")
	for _, k := range []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune("ab")},
		{Type: tea.KeyRunes, Runes: []rune("z"), Paste: true},
	} {
		m = sendMsg(t, m, k)
		if !m.set.capturing || m.set.editErr != "press a single key" || len(m.work.Keys) != 0 {
			t.Errorf("%v accepted: err=%q", k, m.set.editErr)
		}
	}
}

func TestSavedRepoConfigResetsCursorWhenFieldsChange(t *testing.T) {
	e := newSettingsEnv(t)
	m := openSettings(t, e.model(t, 120, 35), catAppearance, 0, 0)
	if len(m.curFields()) != 1 {
		t.Fatalf("fixture: %d fields", len(m.curFields()))
	}
	// The repo gains theme overrides, so the Theme section grows a row.
	if err := os.WriteFile(filepath.Join(e.repo, ".bonsai.yaml"), []byte("theme:\n  overrides:\n    accent: \"#ff0000\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.set.field = 5
	m, _ = send(t, m, configSavedMsg{setting: "theme.overrides.accent = #ff0000"})
	if len(m.curFields()) != 2 || m.set.field != 0 {
		t.Errorf("fields=%d cursor=%d", len(m.curFields()), m.set.field)
	}
}
