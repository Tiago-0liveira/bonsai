package ui

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/ui/components/modals"
	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
)

// configModel builds a palette model whose config edits hit a temp file, with
// the cwd moved out of any git repo so reloads resolve to that file.
func configModel(t *testing.T) Model {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	m := paletteModel("feat", false)
	m.repoDir = dir
	m.configFile = filepath.Join(dir, ".bonsai.yaml")
	return m
}

func settingByKey(key string) (configSetting, bool) {
	for _, s := range configSettings {
		if s.key == key {
			return s, true
		}
	}
	return configSetting{}, false
}

func TestConfigSettingsRegistry(t *testing.T) {
	seen := map[string]bool{}
	for _, s := range configSettings {
		if seen[s.label] {
			t.Errorf("duplicate config palette label %q", s.label)
		}
		seen[s.label] = true
		if s.key == "" || s.title == "" || s.desc == "" || s.example == "" {
			t.Errorf("setting %q missing metadata: %+v", s.key, s)
		}
		if !strings.HasPrefix(s.label, "Config: ") {
			t.Errorf("label %q should start with \"Config: \"", s.label)
		}
	}
}

func TestPaletteContainsEveryConfigSetting(t *testing.T) {
	m := paletteModel("feat", false)
	labels := map[string]bool{}
	for _, c := range m.paletteCommands() {
		labels[c.label] = true
	}
	for _, s := range configSettings {
		if !labels[s.label] {
			t.Errorf("palette is missing %q", s.label)
		}
	}
}

// submitConfig drives a KindConfigValue submit through the save cmd and the
// resulting configSavedMsg, returning the updated model.
func submitConfig(t *testing.T, m Model, value string) Model {
	t.Helper()
	nm, cmd := m.Update(modals.SubmitMsg{Kind: modals.KindConfigValue, Value: value})
	model := nm.(Model)
	if cmd == nil {
		t.Fatal("config submit returned no command")
	}
	msg, ok := cmd().(configSavedMsg)
	if !ok {
		t.Fatalf("config submit cmd returned %T", cmd())
	}
	if msg.err != nil {
		t.Fatalf("config write failed: %v", msg.err)
	}
	nm, _ = model.Update(msg)
	return nm.(Model)
}

func TestConfigUpstreamFlow(t *testing.T) {
	m := configModel(t)
	s, _ := settingByKey("upstream")
	nm, _ := m.openConfigSetting(s)
	model := nm.(Model)
	if model.modal == nil || model.modal.Kind() != modals.KindConfigValue {
		t.Fatal("upstream editor should open a value modal")
	}

	model = submitConfig(t, model, "origin/develop")
	if model.cfg.Upstream != "origin/develop" {
		t.Errorf("upstream after save = %q", model.cfg.Upstream)
	}
	if !strings.Contains(model.status, "upstream") {
		t.Errorf("status should mention the setting: %q", model.status)
	}
}

func TestConfigToggleFlow(t *testing.T) {
	m := configModel(t)
	s, _ := settingByKey("confirm_destructive")
	nm, _ := m.openConfigSetting(s)
	model := nm.(Model)
	if model.modal == nil || model.modal.Kind() != modals.KindConfigValue {
		t.Fatal("toggle editor should open a value modal")
	}

	model = submitConfig(t, model, "off")
	if model.cfg.ConfirmDestructive {
		t.Error("confirm_destructive should be false after choosing off")
	}
}

func TestConfigThemePresetAppliesLive(t *testing.T) {
	old := theme.Current
	t.Cleanup(func() { theme.Current = old })

	m := configModel(t)
	s, _ := settingByKey("theme.preset")
	nm, _ := m.openConfigSetting(s)
	model := submitConfig(t, nm.(Model), "nord")

	if model.cfg.Theme.Preset != "nord" {
		t.Fatalf("preset after save = %q", model.cfg.Theme.Preset)
	}
	want := theme.Resolve("nord", nil)
	if theme.Current.Accent != want.Accent {
		t.Errorf("live theme accent = %v, want nord %v", theme.Current.Accent, want.Accent)
	}
}

func TestConfigThemeOverrideFlow(t *testing.T) {
	old := theme.Current
	t.Cleanup(func() { theme.Current = old })

	m := configModel(t)
	m.cfg.Theme.Overrides = map[string]string{"accent": "#00ff00"}
	s, _ := settingByKey("theme.overrides")

	// Entry picker shows the existing override plus add/remove entries.
	nm, _ := m.openConfigSetting(s)
	model := nm.(Model)
	if model.modal == nil || model.modal.Kind() != modals.KindConfigChoice {
		t.Fatal("overrides editor should open a choice modal")
	}

	nm, _ = model.Update(modals.SubmitMsg{Kind: modals.KindConfigChoice, Value: "＋ override a role…"})
	model = nm.(Model)
	nm, _ = model.Update(modals.SubmitMsg{Kind: modals.KindConfigChoice, Value: "danger"})
	model = nm.(Model)
	if model.modal == nil || model.modal.Kind() != modals.KindConfigValue {
		t.Fatal("picking a role should open the value input")
	}

	model = submitConfig(t, model, "#ff0000")
	if model.cfg.Theme.Overrides["danger"] != "#ff0000" {
		t.Errorf("overrides after save = %v", model.cfg.Theme.Overrides)
	}
	if theme.Current.Danger != "#ff0000" {
		t.Errorf("live theme danger = %v", theme.Current.Danger)
	}
}

func TestConfigHookRemoveFlow(t *testing.T) {
	m := configModel(t)
	m.cfg.Hooks.OnWorktreeCreate = []string{"npm ci", "echo hi"}
	s, _ := settingByKey("hooks.on_worktree_create")

	nm, _ := m.openConfigSetting(s)
	model := nm.(Model)
	if model.modal == nil || model.modal.Kind() != modals.KindConfigChoice {
		t.Fatal("hooks editor should open a choice modal")
	}

	// Selecting an existing command asks for confirmation.
	nm, _ = model.Update(modals.SubmitMsg{Kind: modals.KindConfigChoice, Value: "npm ci"})
	model = nm.(Model)
	if model.modal == nil || model.modal.Kind() != modals.KindConfigConfirm {
		t.Fatal("selecting a hook should open a confirm modal")
	}

	nm, cmd := model.Update(modals.SubmitMsg{Kind: modals.KindConfigConfirm, Value: "yes"})
	model = nm.(Model)
	if cmd == nil {
		t.Fatal("confirm returned no command")
	}
	msg, ok := cmd().(configSavedMsg)
	if !ok || msg.err != nil {
		t.Fatalf("hook removal failed: %T %v", msg, msg.err)
	}
	nm, _ = model.Update(msg)
	model = nm.(Model)

	got := model.cfg.CreateHooks()
	if len(got) != 1 || got[0] != "echo hi" {
		t.Errorf("hooks after removal = %v", got)
	}
}

func TestConfigEmptyValueRejected(t *testing.T) {
	m := configModel(t)
	s, _ := settingByKey("upstream")
	nm, _ := m.openConfigSetting(s)
	model := nm.(Model)

	nm, cmd := model.Update(modals.SubmitMsg{Kind: modals.KindConfigValue, Value: "   "})
	model = nm.(Model)
	if cmd != nil {
		t.Error("empty value should not save")
	}
	if !strings.Contains(model.status, "empty") {
		t.Errorf("status should explain the refusal: %q", model.status)
	}
}


func TestConfigPkgMgrSearchDepthFlow(t *testing.T) {
	m := configModel(t)
	s, ok := settingByKey("pkgmgr.search_depth")
	if !ok {
		t.Fatal("pkgmgr.search_depth missing from config registry")
	}
	nm, _ := m.openConfigSetting(s)
	model := submitConfig(t, nm.(Model), "3")
	if model.cfg.PkgMgr.SearchDepth != 3 {
		t.Fatalf("search depth after save = %d, want 3", model.cfg.PkgMgr.SearchDepth)
	}
}

func TestConfigPkgMgrSearchDepthRejectsInvalid(t *testing.T) {
	m := configModel(t)
	s, _ := settingByKey("pkgmgr.search_depth")
	nm, _ := m.openConfigSetting(s)
	model := nm.(Model)

	nm, cmd := model.Update(modals.SubmitMsg{Kind: modals.KindConfigValue, Value: "-1"})
	model = nm.(Model)
	if cmd != nil {
		t.Fatal("negative search depth should not save")
	}
	if !strings.Contains(model.status, "non-negative integer") {
		t.Fatalf("status = %q, want validation message", model.status)
	}
}
