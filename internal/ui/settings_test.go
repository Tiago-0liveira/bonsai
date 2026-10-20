package ui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/modals"
	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
)

// settingsEnv is a redesign model backed by a real state.json and repo dir in
// temp locations. The cwd is moved out of the bonsai checkout so config
// discovery never sees its .bonsai.yaml.
type settingsEnv struct {
	repo, xdg string
	st        *config.State
}

func newSettingsEnv(t *testing.T) settingsEnv {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	t.Chdir(t.TempDir())
	t.Cleanup(func() {
		theme.Current = theme.Resolve("bonsai", nil)
		applyTheme()
	})
	return settingsEnv{repo: t.TempDir(), xdg: xdg}
}

func (e *settingsEnv) model(t *testing.T, w, h int) Model {
	t.Helper()
	st, err := config.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	e.st = st
	cfg, err := config.LoadFor(e.repo)
	if err != nil {
		t.Fatal(err)
	}
	return sized(t, New(e.repo, cfg, st, WithLayout(LayoutRedesign)), w, h)
}

func (e settingsEnv) writeState(t *testing.T, body string) {
	t.Helper()
	dir := filepath.Join(e.xdg, "bonsai")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "state.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func diskPrefs(t *testing.T) config.Prefs {
	t.Helper()
	st, err := config.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	return st.PrefsSnapshot()
}

func keyOf(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	}
	return runeKey(s)
}

// settle runs a command and feeds its message back, until no command is left.
func settle(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	for cmd != nil {
		m, cmd = send(t, m, cmd())
	}
	return m
}

// press sends keys in order and settles every command they produce.
func press(t *testing.T, m Model, keys ...string) Model {
	t.Helper()
	for _, k := range keys {
		var cmd tea.Cmd
		m, cmd = send(t, m, keyOf(k))
		m = settle(t, m, cmd)
	}
	return m
}

func typeText(t *testing.T, m Model, s string) Model {
	t.Helper()
	for _, r := range s {
		m, _ = send(t, m, runeKey(string(r)))
	}
	return m
}

// openSettings selects Settings and walks the cursor to the given field.
func openSettings(t *testing.T, m Model, cat, sec, field int) Model {
	t.Helper()
	m, cmd := m.selectWorkspace(wsSettings)
	m = settle(t, m, cmd)
	keys := []string{"enter"}
	for i := 0; i < cat; i++ {
		keys = append(keys, "right")
	}
	keys = append(keys, "enter")
	for i := 0; i < sec; i++ {
		keys = append(keys, "down")
	}
	keys = append(keys, "tab")
	for i := 0; i < field; i++ {
		keys = append(keys, "down")
	}
	return press(t, m, keys...)
}

const (
	catGeneral    = 0
	catAppearance = 1
	catKeys       = 2
	catGarden     = 3
	catAgents     = 4
)

func TestSettingsNavigationLevelsAndEsc(t *testing.T) {
	e := newSettingsEnv(t)
	m := e.model(t, 120, 35)
	m, _ = m.selectWorkspace(wsSettings)
	if m.shellFocus != shellNav {
		t.Fatal("selecting Settings must keep focus on the tabs")
	}
	steps := []struct {
		key        string
		focus      shellFocus
		level      settingsLevel
		cat, sec   int
		field      int
		wantStatus string
	}{
		{"enter", shellBody, levelCategory, 0, 0, 0, ""},
		{"right", shellBody, levelCategory, 1, 0, 0, ""},
		{"left", shellBody, levelCategory, 0, 0, 0, ""},
		{"left", shellBody, levelCategory, 4, 0, 0, ""},
		{"right", shellBody, levelCategory, 0, 0, 0, ""},
		{"right", shellBody, levelCategory, 1, 0, 0, ""},
		{"down", shellBody, levelSection, 1, 0, 0, ""},
		{"down", shellBody, levelSection, 1, 1, 0, ""},
		{"right", shellBody, levelSection, 2, 0, 0, ""},
		{"left", shellBody, levelSection, 1, 0, 0, ""},
		{"tab", shellBody, levelField, 1, 0, 0, ""},
		{"tab", shellBody, levelSection, 1, 0, 0, ""},
		{"enter", shellBody, levelField, 1, 0, 0, ""},
		{"esc", shellBody, levelSection, 1, 0, 0, ""},
		{"esc", shellBody, levelCategory, 1, 0, 0, ""},
		{"esc", shellNav, levelCategory, 1, 0, 0, ""},
	}
	for i, s := range steps {
		m = press(t, m, s.key)
		if m.shellFocus != s.focus || m.set.level != s.level || m.set.cat != s.cat || m.set.sec != s.sec || m.set.field != s.field {
			t.Fatalf("step %d (%s): focus=%s level=%d cat=%d sec=%d field=%d", i, s.key, m.shellFocus, m.set.level, m.set.cat, m.set.sec, m.set.field)
		}
	}
	// Back on the tabs, the tab keys work again.
	if m = press(t, m, "right"); m.workspace != wsGarden {
		t.Errorf("nav right after leaving the body selected %s", m.workspace.name())
	}
}

func TestSettingsDownEntersFromNav(t *testing.T) {
	e := newSettingsEnv(t)
	m := e.model(t, 80, 24)
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("5"), Alt: true})
	if m.workspace != wsSettings {
		t.Fatalf("alt+5 selected %s", m.workspace.name())
	}
	if m = press(t, m, "down"); m.shellFocus != shellBody {
		t.Error("down on the Settings tab did not enter the body")
	}
}

func TestSettingsGlobalKeysInBodyAndInputOwnsKeys(t *testing.T) {
	e := newSettingsEnv(t)
	m := openSettings(t, e.model(t, 80, 24), catGeneral, 1, 0) // Editor command
	if _, cmd := send(t, m, runeKey("q")); cmd == nil || cmd() != tea.Quit() {
		t.Error("q must quit from the settings body")
	}
	if m2, _ := send(t, m, tea.KeyMsg{Type: tea.KeyCtrlK}); m2.modal == nil {
		t.Error("palette must open from the settings body")
	}
	if m2, _ := send(t, m, runeKey("?")); m2.modal == nil {
		t.Error("help must open from the settings body")
	}

	m = press(t, m, "enter")
	if m.shellFocus != shellInput || !m.set.editing {
		t.Fatal("enter on a text field did not start an edit")
	}
	for _, k := range []tea.KeyMsg{runeKey("q"), runeKey("?"), {Type: tea.KeyCtrlK}, {Type: tea.KeyRight}, {Type: tea.KeyTab}} {
		m2, cmd := send(t, m, k)
		if cmd != nil && k.Type != tea.KeyRunes {
			t.Errorf("%s produced a command during an edit", k.String())
		}
		if m2.modal != nil || m2.workspace != wsSettings {
			t.Errorf("%s leaked to the shell during an edit", k.String())
		}
	}
	if _, cmd := send(t, m, tea.KeyMsg{Type: tea.KeyCtrlC}); cmd == nil || cmd() != tea.Quit() {
		t.Error("ctrl+c must quit even during an edit")
	}
	if !strings.Contains(ansi.Strip(m.View()), "FOCUS: Settings / General / Input") {
		t.Errorf("footer lost the input focus:\n%s", ansi.Strip(m.View()))
	}
}

func TestSettingsEscDuringEditCancelsOnlyTheEdit(t *testing.T) {
	e := newSettingsEnv(t)
	e.writeState(t, `{"prefs":{"editor":"nvim"}}`)
	m := openSettings(t, e.model(t, 80, 24), catGeneral, 1, 0)
	m = typeText(t, press(t, m, "enter"), "XYZ")
	m = press(t, m, "esc")
	if m.set.editing || m.shellFocus != shellBody || m.set.level != levelField {
		t.Fatalf("esc did more than cancel the edit: editing=%v focus=%s level=%d", m.set.editing, m.shellFocus, m.set.level)
	}
	if m.work.Editor != "nvim" || diskPrefs(t).Editor != "nvim" || m.prefsGen != 0 {
		t.Error("a cancelled edit changed the value")
	}
	if m = press(t, m, "esc"); m.set.level != levelSection {
		t.Error("the next esc must go up one level")
	}
}

func TestSettingsMachineFieldsPersist(t *testing.T) {
	tests := []struct {
		name            string
		cat, sec, field int
		keys            []string
		check           func(config.Prefs) bool
	}{
		{"startup workspace", catGeneral, 0, 0, []string{"right"}, func(p config.Prefs) bool { return p.StartWorkspace == "garden" }},
		{"remember off", catGeneral, 0, 1, []string{"right"}, func(p config.Prefs) bool { return p.NoWorkspaceMemory }},
		{"editor", catGeneral, 1, 0, []string{"enter", "n", "v", "i", "m", "enter"}, func(p config.Prefs) bool { return p.Editor == "nvim" }},
		{"sort", catGeneral, 1, 1, []string{"right"}, func(p config.Prefs) bool { return p.Sort == "ahead" }},
		{"pr status", catGeneral, 1, 2, []string{"right"}, func(p config.Prefs) bool { return p.PRStatus == "compact" }},
		{"prune merge", catGeneral, 1, 3, []string{"enter"}, func(p config.Prefs) bool { return p.PruneMerge }},
		{"theme", catAppearance, 0, 0, []string{"right", "right"}, func(p config.Prefs) bool { return p.Theme == "sakura" }},
		{"contrast", catAppearance, 1, 0, []string{"right"}, func(p config.Prefs) bool { return p.Contrast == "flat" }},
		{"icons", catAppearance, 2, 0, []string{"right"}, func(p config.Prefs) bool { return p.AgentGlyphs == "ascii" }},
		{"density", catAppearance, 3, 0, []string{"right"}, func(p config.Prefs) bool { return p.Density == "compact" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newSettingsEnv(t)
			m := openSettings(t, e.model(t, 120, 35), tc.cat, tc.sec, tc.field)
			m = press(t, m, tc.keys...)
			if !tc.check(m.work) {
				t.Errorf("working copy did not change: %+v", m.work)
			}
			if got := diskPrefs(t); !tc.check(got) {
				t.Errorf("not persisted: %+v", got)
			}
			if m.status != "Saved" {
				t.Errorf("status = %q, want Saved", m.status)
			}
		})
	}
}

func TestSettingsPreviewIsImmediate(t *testing.T) {
	e := newSettingsEnv(t)
	m := openSettings(t, e.model(t, 120, 35), catAppearance, 0, 0)
	before := theme.Current.Accent
	m, cmd := send(t, m, keyOf("right")) // bonsai
	m, _ = send(t, m, keyOf("right"))    // sakura, before any save result arrives
	if cmd == nil || theme.Current.Accent == before || theme.Current.Accent != theme.Resolve("sakura", nil).Accent {
		t.Errorf("theme preview not applied before the save finished: %v", theme.Current.Accent)
	}
	m = openSettings(t, e.model(t, 120, 35), catAppearance, 1, 0)
	m, _ = send(t, m, keyOf("right"))
	if theme.Current.Surface != theme.Current.Background || theme.Current.Raised != theme.Current.Background {
		t.Error("flat contrast did not collapse the surfaces")
	}
	m = openSettings(t, e.model(t, 120, 35), catAppearance, 2, 0)
	m, _ = send(t, m, keyOf("right"))
	if !m.agentGlyphsASCII() {
		t.Error("ascii fallback not previewed")
	}
}

func TestSettingsOldStateFileLoads(t *testing.T) {
	e := newSettingsEnv(t)
	e.writeState(t, `{"copy_counts":{"a":1},"aliases":[{"Name":"x","Command":"y"}],"prefs":{"theme":"nord","sort":"pr","keys":{"quit":"x"}}}`)
	m := e.model(t, 120, 35)
	if m.work.Theme != "nord" || m.work.Sort != "pr" || m.work.Contrast != "" || m.work.StartWorkspace != "" || m.work.NoWorkspaceMemory {
		t.Errorf("old prefs misread: %+v", m.work)
	}
	if m.workspace != wsGarden {
		t.Errorf("old state opened %s, want Garden", m.workspace.name())
	}
	m = openSettings(t, m, catAppearance, 0, 0)
	if !strings.Contains(ansi.Strip(m.View()), "[Nord v]") {
		t.Errorf("old theme not shown:\n%s", ansi.Strip(m.View()))
	}
	m = press(t, m, "right")
	data, err := os.ReadFile(filepath.Join(e.xdg, "bonsai", "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"copy_counts"`, `"Name": "x"`, `"quit": "x"`} {
		if !strings.Contains(string(data), want) {
			t.Errorf("unrelated state %s lost after a preference save:\n%s", want, data)
		}
	}
}

func TestSettingsSaveFailureRestoresLastPersisted(t *testing.T) {
	e := newSettingsEnv(t)
	e.writeState(t, `{"prefs":{"theme":"nord"}}`)
	m := openSettings(t, e.model(t, 120, 35), catAppearance, 0, 0)
	// Replace the state directory with a file so the next write must fail.
	dir := filepath.Join(e.xdg, "bonsai")
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	m = press(t, m, "right") // nord -> mono (previews), then the save fails
	if m.work.Theme != "nord" {
		t.Errorf("working copy = %q, want the last persisted nord", m.work.Theme)
	}
	if theme.Current.Accent != theme.Resolve("nord", nil).Accent {
		t.Error("preview was not rolled back")
	}
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "Save failed") {
		t.Errorf("failure not shown:\n%s", view)
	}
	if m.status == "Saved" || strings.Contains(view, "Saved ") {
		t.Errorf("failure reported as saved: %q", m.status)
	}
}

func TestSettingsLateSaveResultsNeverReplaceNewerEdits(t *testing.T) {
	e := newSettingsEnv(t)
	m := openSettings(t, e.model(t, 120, 35), catAppearance, 0, 0)
	m, cmd1 := send(t, m, keyOf("right")) // gen 1: bonsai
	m, cmd2 := send(t, m, keyOf("right")) // gen 2: sakura
	if m.prefsGen != 2 {
		t.Fatalf("prefsGen = %d", m.prefsGen)
	}
	// The newer write lands first, then the older one reports superseded.
	m, _ = send(t, m, cmd2())
	r1 := cmd1()
	if got := r1.(settingsSavedMsg); !errors.Is(got.err, config.ErrSuperseded) {
		t.Fatalf("older write error = %v, want ErrSuperseded", got.err)
	}
	m, _ = send(t, m, r1)
	if m.work.Theme != "sakura" || m.set.saveErr != "" || m.status != "Saved" {
		t.Errorf("late result disturbed state: theme=%q err=%q status=%q", m.work.Theme, m.set.saveErr, m.status)
	}
	if diskPrefs(t).Theme != "sakura" {
		t.Error("disk lost the newer edit")
	}
	// A stale failure is ignored outright, even though it is not ErrSuperseded.
	m, _ = send(t, m, settingsSavedMsg{gen: 1, err: errors.New("disk full")})
	if m.work.Theme != "sakura" || m.set.saveErr != "" {
		t.Errorf("stale failure applied: %q %q", m.work.Theme, m.set.saveErr)
	}
}

func TestSettingsRepoWriteNeedsConfirmation(t *testing.T) {
	e := newSettingsEnv(t)
	file := filepath.Join(e.repo, ".bonsai.yaml")
	original := "# keep me\nupstream: origin/dev\n"
	if err := os.WriteFile(file, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	m := openSettings(t, e.model(t, 120, 35), catGeneral, 2, 0) // Confirm destructive
	if !m.cfg.ConfirmDestructive {
		t.Fatal("fixture: confirm_destructive should default to true")
	}

	m, cmd := send(t, m, keyOf("right"))
	if cmd != nil || m.modal == nil || m.modal.Kind() != modals.KindConfigConfirm {
		t.Fatalf("a repo edit must open the confirm dialog first (cmd=%v)", cmd != nil)
	}
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "confirm_destructive = false") || !strings.Contains(view, ".bonsai.yaml") {
		t.Errorf("dialog does not name the write:\n%s", view)
	}

	// Cancel writes nothing.
	m, _ = send(t, m, modals.CancelMsg{Kind: modals.KindConfigConfirm})
	if got, _ := os.ReadFile(file); string(got) != original || !m.cfg.ConfirmDestructive || m.set.pending != nil {
		t.Errorf("cancel changed something: %q", got)
	}

	// Yes writes, reloads, and starts no domain work.
	m, _ = send(t, m, keyOf("right"))
	m, cmd = send(t, m, modals.SubmitMsg{Kind: modals.KindConfigConfirm, Value: "yes"})
	if cmd == nil {
		t.Fatal("confirming produced no write")
	}
	msg := cmd()
	if _, ok := msg.(configSavedMsg); !ok {
		t.Fatalf("write returned %T", msg)
	}
	m, cmd = send(t, m, msg)
	if cmd != nil {
		t.Error("config reload started domain work")
	}
	got, _ := os.ReadFile(file)
	if !strings.Contains(string(got), "# keep me") || !strings.Contains(string(got), "confirm_destructive: false") || !strings.Contains(string(got), "origin/dev") {
		t.Errorf("repo file after write:\n%s", got)
	}
	if m.cfg.ConfirmDestructive || !strings.Contains(m.status, "confirm_destructive = false") {
		t.Errorf("config not reloaded: cfg=%v status=%q", m.cfg.ConfirmDestructive, m.status)
	}
	if !strings.Contains(ansi.Strip(m.View()), "[Off v]") {
		t.Errorf("view does not show the new value:\n%s", ansi.Strip(m.View()))
	}
}

func TestSettingsRepoWriteFailureIsShown(t *testing.T) {
	e := newSettingsEnv(t)
	m := openSettings(t, e.model(t, 120, 35), catGeneral, 2, 1)
	m, _ = send(t, m, configSavedMsg{setting: "notifications.process = false", err: errors.New("read-only file system")})
	if !strings.Contains(ansi.Strip(m.View()), "Save failed: read-only file system") || !m.cfg.Notifications.Process {
		t.Errorf("failure not surfaced or config changed:\n%s", ansi.Strip(m.View()))
	}
}

func TestSettingsUpstreamEditValidatesThenConfirms(t *testing.T) {
	e := newSettingsEnv(t)
	m := openSettings(t, e.model(t, 120, 35), catGarden, 0, 1) // Base ref
	m = press(t, m, "enter")
	for range m.cfg.Upstream {
		m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyBackspace})
	}
	m = press(t, m, "enter")
	if !m.set.editing || m.modal != nil || !strings.Contains(ansi.Strip(m.View()), "must not be empty") {
		t.Fatalf("empty base ref was accepted (editing=%v)", m.set.editing)
	}
	m = press(t, typeText(t, m, "origin/trunk"), "enter")
	if m.modal == nil || m.modal.Kind() != modals.KindConfigConfirm || m.shellFocus != shellBody {
		t.Fatal("a valid base ref must go to the confirm dialog")
	}
	if _, err := os.Stat(filepath.Join(e.repo, ".bonsai.yaml")); err == nil {
		t.Error("repo file written before confirmation")
	}
	m = settleSubmit(t, m)
	if m.cfg.Upstream != "origin/trunk" || !strings.Contains(ansi.Strip(m.View()), "[origin/trunk]") {
		t.Errorf("upstream = %q", m.cfg.Upstream)
	}
}

func settleSubmit(t *testing.T, m Model) Model {
	t.Helper()
	m, cmd := send(t, m, modals.SubmitMsg{Kind: modals.KindConfigConfirm, Value: "yes"})
	return settle(t, m, cmd)
}

func TestSettingsUnavailableFieldsChangeNothing(t *testing.T) {
	cases := []struct{ cat, sec int }{{catGarden, 1}, {catGarden, 2}, {catAgents, 1}, {catAgents, 2}}
	for _, c := range cases {
		e := newSettingsEnv(t)
		m := openSettings(t, e.model(t, 120, 35), c.cat, c.sec, 0)
		before := m.work
		for _, k := range []string{"enter", "left", "right"} {
			var cmd tea.Cmd
			m, cmd = send(t, m, keyOf(k))
			if cmd != nil || m.modal != nil || m.set.editing || m.prefsGen != 0 {
				t.Errorf("cat %d sec %d: %s did something (cmd=%v modal=%v)", c.cat, c.sec, k, cmd != nil, m.modal != nil)
			}
			if k == "enter" && m.status != "Not available yet" {
				t.Errorf("cat %d sec %d: status %q", c.cat, c.sec, m.status)
			}
		}
		if fmt.Sprint(m.work) != fmt.Sprint(before) {
			t.Errorf("cat %d sec %d: prefs changed", c.cat, c.sec)
		}
		view := ansi.Strip(m.View())
		if strings.Contains(view, " v]") && c.cat != catGarden {
			t.Errorf("cat %d sec %d shows a control:\n%s", c.cat, c.sec, view)
		}
		if !strings.Contains(view, "Unavailable") && !strings.Contains(view, "Not editable") {
			t.Errorf("cat %d sec %d is not honest about availability:\n%s", c.cat, c.sec, view)
		}
	}
}

func TestSettingsRememberWorkspacePerRepo(t *testing.T) {
	e := newSettingsEnv(t)
	m := e.model(t, 80, 24)
	m, cmd := send(t, m, keyOf("right"))
	if cmd == nil {
		t.Fatal("changing workspace did not persist it")
	}
	m = settle(t, m, cmd)
	m = press(t, m, "right", "right") // Worktrees
	m = openSettings(t, m, 0, 0, 0)   // Settings
	if m.workspace != wsSettings {
		t.Fatalf("workspace = %s", m.workspace.name())
	}

	// A new session in the same repo restores it, without domain work.
	m2 := e.model(t, 80, 24)
	if m2.workspace != wsSettings || m2.Init() != nil {
		t.Errorf("restored %s, init=%v", m2.workspace.name(), m2.Init() != nil)
	}

	// The same repo through a symlink is the same identity.
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(e.repo, link); err != nil {
		t.Skip("symlinks unavailable: " + err.Error())
	}
	st, _ := config.LoadState()
	if got := New(link, &config.Config{}, st, WithLayout(LayoutRedesign)).workspace; got != wsSettings {
		t.Errorf("symlinked path opened %s", got.name())
	}
	// Another repo does not inherit it.
	if got := New(t.TempDir(), &config.Config{}, st, WithLayout(LayoutRedesign)).workspace; got != wsGarden {
		t.Errorf("other repo opened %s, want Garden", got.name())
	}
}

func TestSettingsStartupWorkspaceRules(t *testing.T) {
	tests := []struct {
		name  string
		state string
		want  workspace
	}{
		{"explicit wins over memory", `{"prefs":{"start_workspace":"worktrees"},"workspaces":{"REPO":"accounts"}}`, wsWorktrees},
		{"memory used when last visited", `{"workspaces":{"REPO":"accounts"}}`, wsAccounts},
		{"memory off ignores history", `{"prefs":{"no_workspace_memory":true},"workspaces":{"REPO":"accounts"}}`, wsGarden},
		{"memory off still honors explicit", `{"prefs":{"no_workspace_memory":true,"start_workspace":"settings"}}`, wsSettings},
		{"unknown explicit falls back to memory", `{"prefs":{"start_workspace":"bogus"},"workspaces":{"REPO":"playground"}}`, wsPlayground},
		{"unknown memory falls back to Garden", `{"workspaces":{"REPO":"nowhere"}}`, wsGarden},
		{"nothing stored", `{}`, wsGarden},
	}
	for _, tc := range tests {
		e := newSettingsEnv(t)
		e.writeState(t, strings.ReplaceAll(tc.state, "REPO", strings.ReplaceAll(config.RepoIdentity(e.repo), `\`, `\\`)))
		m := e.model(t, 80, 24)
		if m.workspace != tc.want {
			t.Errorf("%s: opened %s, want %s", tc.name, m.workspace.name(), tc.want.name())
		}
		if m.Init() != nil {
			t.Errorf("%s: restoring a workspace started work", tc.name)
		}
	}
}

func TestSettingsMemoryOffPersistsNothing(t *testing.T) {
	e := newSettingsEnv(t)
	e.writeState(t, `{"prefs":{"no_workspace_memory":true}}`)
	m := e.model(t, 80, 24)
	m, cmd := send(t, m, keyOf("right"))
	if cmd != nil || m.workspace != wsPlayground {
		t.Errorf("memory off still persisted (cmd=%v)", cmd != nil)
	}
}

func TestSettingsPrefsKeyRoutesToSettings(t *testing.T) {
	e := newSettingsEnv(t)
	m := e.model(t, 80, 24)
	m, _ = send(t, m, runeKey(","))
	if m.workspace != wsSettings || m.prefs != nil || m.modal != nil {
		t.Errorf("',' opened workspace=%s prefs=%v modal=%v", m.workspace.name(), m.prefs != nil, m.modal != nil)
	}
}

func TestSettingsHelpMatchesLevel(t *testing.T) {
	e := newSettingsEnv(t)
	m := openSettings(t, e.model(t, 120, 35), catAppearance, 0, 0)
	cases := []struct {
		level settingsLevel
		want  []string
	}{
		{levelCategory, []string{"category", "sections"}},
		{levelSection, []string{"section", "field"}},
		{levelField, []string{"change", "choose", "sections"}},
	}
	for _, c := range cases {
		m.set.level = c.level
		h, _ := send(t, m, runeKey("?"))
		view := ansi.Strip(h.View())
		for _, w := range c.want {
			if !strings.Contains(view, w) {
				t.Errorf("level %d help lacks %q:\n%s", c.level, w, view)
			}
		}
	}
}

func TestSettingsEffectiveValueOrigins(t *testing.T) {
	e := newSettingsEnv(t)
	if err := os.WriteFile(filepath.Join(e.repo, ".bonsai.yaml"), []byte("editor: hx\ntheme:\n  preset: nord\n  overrides:\n    accent: \"#ff0000\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := e.model(t, 120, 35)
	editor, _ := fieldByID(t, "editor")
	if got := editor.origin(m); got != "repo .bonsai.yaml" {
		t.Errorf("editor origin = %q", got)
	}
	m.work.Editor = "code -w"
	if got := editor.origin(m); got != "personal" || editor.display(m) != "code -w" {
		t.Errorf("personal editor origin = %q display = %q", got, editor.display(m))
	}
	m.work.Editor, m.cfg.Editor = "", ""
	if got := editor.origin(m); !strings.Contains(got, "$VISUAL/$EDITOR/vi") || editor.display(m) != "inherit" {
		t.Errorf("default editor origin = %q display = %q", got, editor.display(m))
	}
	th, _ := fieldByID(t, "theme")
	if got := th.origin(m); !strings.Contains(got, "repo .bonsai.yaml") {
		t.Errorf("theme origin = %q", got)
	}
	m = openSettings(t, m, catAppearance, 0, 0)
	if v := ansi.Strip(m.View()); !strings.Contains(v, "Inherit (nord)") || !strings.Contains(v, "Repo overrides") {
		t.Errorf("inherit/overrides not shown:\n%s", v)
	}
	if v := ansi.Strip(press(t, m, "down").View()); !strings.Contains(v, "repo overrides apply on top: 1 roles") {
		t.Errorf("overrides line missing:\n%s", v)
	}
}

func fieldByID(t *testing.T, id string) (settingField, bool) {
	t.Helper()
	for _, c := range settingCategories {
		for _, s := range c.sections {
			for _, f := range s.fields {
				if f.id == id {
					return f, true
				}
			}
		}
	}
	t.Fatalf("no field %q", id)
	return settingField{}, false
}

func TestSettingsScopeChipAndFooter(t *testing.T) {
	e := newSettingsEnv(t)
	m := openSettings(t, e.model(t, 120, 35), catGeneral, 0, 0)
	cases := map[int]string{catGeneral: "Scope: Mixed", catAppearance: "Scope: This machine", catGarden: "Scope: Mixed", catAgents: "Scope: This machine"}
	for cat, want := range cases {
		m.set.cat, m.set.sec = cat, 0
		view := ansi.Strip(m.View())
		if !strings.Contains(view, want) {
			t.Errorf("category %d lacks %q", cat, want)
		}
		if !strings.Contains(view, "Settings / "+settingCategories[cat].name+" / "+settingCategories[cat].sections[0].name) {
			t.Errorf("category %d footer breadcrumb missing", cat)
		}
		if strings.Contains(view, "preview · shell only") {
			t.Error("Settings still shows the shell-only status")
		}
	}
}

func TestSettingsRendersBoundedEverywhere(t *testing.T) {
	sizes := [][2]int{{80, 24}, {120, 35}, {160, 48}, {64, 10}, {64, 14}, {99, 24}, {100, 24}}
	themes := []string{"bonsai", "mono", "sakura", "dracula", "nord"}
	for _, ascii := range []bool{false, true} {
		for _, flat := range []bool{false, true} {
			for _, th := range themes {
				for _, density := range []string{"", "compact"} {
					prefs := config.Prefs{Theme: th, Density: density}
					if ascii {
						prefs.AgentGlyphs = "ascii"
					}
					if flat {
						prefs.Contrast = "flat"
					}
					e := newSettingsEnv(t)
					e.writeState(t, mustPrefsJSON(prefs))
					base := e.model(t, 80, 24)
					for _, sz := range sizes {
						for cat := range settingCategories {
							for sec := range settingCategories[cat].sections {
								for _, level := range []settingsLevel{levelCategory, levelSection, levelField} {
									m := sized(t, base, sz[0], sz[1])
									m.workspace, m.shellFocus = wsSettings, shellBody
									m.set = settingsState{cat: cat, sec: sec, level: level}
									nf := len(m.curFields())
									for f := 0; f < nf; f++ {
										m.set.field = f
										label := fmt.Sprintf("ascii=%v flat=%v %s %s %dx%d c%d s%d l%d f%d", ascii, flat, th, density, sz[0], sz[1], cat, sec, level, f)
										assertSettingsView(t, label, m, sz[0], sz[1], ascii)
										if level != levelField {
											break
										}
									}
								}
							}
						}
					}
				}
			}
		}
	}
}

func mustPrefsJSON(p config.Prefs) string {
	var parts []string
	add := func(k, v string) {
		if v != "" {
			parts = append(parts, fmt.Sprintf("%q:%q", k, v))
		}
	}
	add("theme", p.Theme)
	add("density", p.Density)
	add("contrast", p.Contrast)
	add("agent_glyphs", p.AgentGlyphs)
	return `{"prefs":{` + strings.Join(parts, ",") + `}}`
}

func assertSettingsView(t *testing.T, label string, m Model, w, h int, ascii bool) {
	t.Helper()
	view := m.View()
	lines := strings.Split(view, "\n")
	if len(lines) != h {
		t.Errorf("%s: %d lines, want %d", label, len(lines), h)
		return
	}
	for i, l := range lines {
		if got := lipgloss.Width(l); got != w {
			t.Errorf("%s: line %d is %d wide, want %d: %q", label, i, got, w, ansi.Strip(l))
			return
		}
	}
	if ascii {
		for _, r := range ansi.Strip(view) {
			if r > 127 {
				t.Errorf("%s: non-ASCII %q in ascii mode:\n%s", label, string(r), ansi.Strip(view))
				return
			}
		}
	}
}

func TestSettingsCursorStaysVisibleWhenBodyIsShort(t *testing.T) {
	e := newSettingsEnv(t)
	for _, sz := range [][2]int{{64, 14}, {80, 15}, {120, 17}} {
		m := sized(t, e.model(t, 80, 24), sz[0], sz[1])
		m.workspace, m.shellFocus = wsSettings, shellBody
		m.set = settingsState{cat: catGeneral, sec: 1, level: levelField}
		for f := range m.curFields() {
			m.set.field = f
			label := m.curFields()[f].label
			if !strings.Contains(ansi.Strip(m.View()), label) {
				t.Errorf("%dx%d: selected field %q scrolled out of view:\n%s", sz[0], sz[1], label, ansi.Strip(m.View()))
			}
		}
	}
}

func TestSettingsDensityRemovesSpacerRows(t *testing.T) {
	e := newSettingsEnv(t)
	rows := func(density string) int {
		m := sized(t, e.model(t, 120, 40), 120, 40)
		m.work.Density = density
		m.workspace, m.shellFocus = wsSettings, shellBody
		m.set = settingsState{cat: catGeneral, sec: 1}
		lines := strings.Split(ansi.Strip(m.View()), "\n")
		first, last := -1, -1
		for i, l := range lines {
			for _, label := range []string{"Editor command", "Merge PR when pruning"} {
				if strings.Contains(l, label) {
					if first < 0 {
						first = i
					}
					last = i
				}
			}
		}
		return last - first
	}
	if comfortable, compact := rows(""), rows("compact"); comfortable <= compact || compact != 3 {
		t.Errorf("comfortable spans %d rows, compact %d (want compact 3 and fewer than comfortable)", comfortable, compact)
	}
}
