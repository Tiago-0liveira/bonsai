package ui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/modals"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/prefs"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/worktreelist"
	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
)

// cfgKind is the editor style a config setting needs.
type cfgKind int

const (
	cfgInput  cfgKind = iota // free-text value
	cfgToggle                // on/off
	cfgSelect                // pick from options
	cfgMap                   // nested map: pick an entry, then set or remove it
	cfgList                  // string list: add or remove entries
)

// configSetting describes one editable .bonsai.yaml setting: its YAML path,
// how it reads in the palette, and what its editor modal explains.
type configSetting struct {
	key     string // dotted YAML path, e.g. "notifications.ci"
	label   string // palette label
	title   string // modal title
	desc    string // what the setting does (modal body)
	example string // example value (modal body + input placeholder)
	kind    cfgKind
	options []string // for cfgSelect
}

// configSettings is the full registry of editable repo-config settings, in
// palette order. Aliases are intentionally absent — the aliases menu covers
// them (user level, state.json).
var configSettings = []configSetting{
	{key: "upstream", label: "Config: upstream", title: "Set upstream",
		desc:    "Base ref that ahead/behind metrics compare against.",
		example: "origin/main", kind: cfgInput},
	{key: "editor", label: "Config: editor", title: "Set editor",
		desc:    "Command for the \"open editor\" action (\"e\"). Empty falls back to $VISUAL/$EDITOR, then vi. A personal override in preferences wins over this.",
		example: "code -w", kind: cfgInput},
	{key: "confirm_destructive", label: "Config: confirm_destructive", title: "confirm_destructive",
		desc:    "Prompt for confirmation before destructive operations.",
		example: "true / false", kind: cfgToggle},
	{key: "notifications.process", label: "Config: notifications.process", title: "notifications.process",
		desc:    "Desktop notification when a background process finishes.",
		example: "true / false", kind: cfgToggle},
	{key: "notifications.ci", label: "Config: notifications.ci", title: "notifications.ci",
		desc:    "Desktop notification when a PR's CI rollup changes.",
		example: "true / false", kind: cfgToggle},
	{key: "worktree.root", label: "Config: worktree.root", title: "Set worktree.root",
		desc:    "Directory new worktrees are created under (default: next to the repo).",
		example: "../worktrees", kind: cfgInput},
	{key: "worktree.path_template", label: "Config: worktree.path_template", title: "Set worktree.path_template",
		desc:    "Worktree directory name; {repo} and {branch} are replaced.",
		example: "{repo}-{branch}", kind: cfgInput},
	{key: "pkgmgr.search_depth", label: "Config: pkgmgr.search_depth", title: "Set pkgmgr.search_depth",
		desc:    "Maximum child-directory depth searched for project manifests. 0 disables downward discovery.",
		example: "2", kind: cfgInput},
	{key: "theme.preset", label: "Config: theme.preset", title: "theme.preset",
		desc:    "Built-in color palette for the whole UI.",
		example: "sakura", kind: cfgSelect, options: theme.Presets()},
	{key: "theme.overrides", label: "Config: theme.overrides", title: "theme.overrides",
		desc:    "Per-role color overrides on top of the preset.",
		example: "accent: \"#ff0000\"", kind: cfgMap},
	{key: "keys", label: "Config: keys", title: "keys",
		desc:    "Repo-level keybinding overrides (personal overrides in preferences win).",
		example: "prune: \"D\"", kind: cfgMap},
	{key: "hooks.on_worktree_create", label: "Config: hooks.on_worktree_create", title: "hooks.on_worktree_create",
		desc:    "Commands run in a new worktree after it is created.",
		example: "npm ci", kind: cfgList},
	{key: "hooks.on_worktree_delete", label: "Config: hooks.on_worktree_delete", title: "hooks.on_worktree_delete",
		desc:    "Commands run in a worktree before it is pruned.",
		example: "docker compose down", kind: cfgList},
}

// themeRoles lists the color roles theme.overrides accepts, matching the
// theme package's applyOverride.
var themeRoles = []string{
	"accent", "border_focus", "border", "dim", "text",
	"success", "danger", "warning", "pr_badge",
}

// Picker-entry sentinels for map/list settings.
const (
	cfgRemovePrefix = "✕ remove "
	cfgAddSentinel  = "+"
)

func (s configSetting) addLabel() string {
	switch s.key {
	case "theme.overrides":
		return "＋ override a role…"
	case "keys":
		return "＋ override an action…"
	default:
		return "＋ add command…"
	}
}

// --- Modal bodies ---

// configCurrentValue renders the live value of a setting for a modal body.
func (m Model) configCurrentValue(s configSetting) string {
	switch s.key {
	case "upstream":
		return m.cfg.Upstream
	case "editor":
		if m.cfg.Editor == "" {
			return "(default: $VISUAL/$EDITOR or vi)"
		}
		return m.cfg.Editor
	case "confirm_destructive":
		return fmt.Sprintf("%v", m.cfg.ConfirmDestructive)
	case "notifications.process":
		return fmt.Sprintf("%v", m.cfg.Notifications.Process)
	case "notifications.ci":
		return fmt.Sprintf("%v", m.cfg.Notifications.CI)
	case "worktree.root":
		if m.cfg.Worktree.Root == "" {
			return "(default: next to the repo)"
		}
		return m.cfg.Worktree.Root
	case "worktree.path_template":
		if m.cfg.Worktree.PathTemplate == "" {
			return "(default: {repo}-{branch})"
		}
		return m.cfg.Worktree.PathTemplate
	case "pkgmgr.search_depth":
		return strconv.Itoa(m.cfg.PkgMgr.SearchDepth)
	case "theme.preset":
		if m.cfg.Theme.Preset == "" {
			return "(default: bonsai)"
		}
		return m.cfg.Theme.Preset
	case "theme.overrides":
		if len(m.cfg.Theme.Overrides) == 0 {
			return "(none)"
		}
		roles := make([]string, 0, len(m.cfg.Theme.Overrides))
		for r, c := range m.cfg.Theme.Overrides {
			roles = append(roles, r+": "+c)
		}
		sort.Strings(roles)
		return strings.Join(roles, ", ")
	case "keys":
		if len(m.cfg.Keys) == 0 {
			return "(none)"
		}
		ks := make([]string, 0, len(m.cfg.Keys))
		for a, k := range m.cfg.Keys {
			ks = append(ks, a+": "+k)
		}
		sort.Strings(ks)
		return strings.Join(ks, ", ")
	case "hooks.on_worktree_create":
		return hookSummary(m.cfg.CreateHooks())
	case "hooks.on_worktree_delete":
		return hookSummary(m.cfg.DeleteHooks())
	}
	return ""
}

func hookSummary(hooks []string) string {
	if len(hooks) == 0 {
		return "(none)"
	}
	return fmt.Sprintf("%d command(s)", len(hooks))
}

// configBody builds the styled current/description/example block shown above
// an editor's input or list.
func (m Model) configBody(s configSetting) string {
	dim := lipgloss.NewStyle().Foreground(theme.Current.Dim)
	acc := lipgloss.NewStyle().Foreground(theme.Current.Accent)
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s\n", dim.Render("current:"), acc.Render(m.configCurrentValue(s)))
	b.WriteString(s.desc + "\n")
	fmt.Fprintf(&b, "%s %s", dim.Render("example:"), dim.Render(s.example))
	return b.String()
}

func (m *Model) configModal(modal modals.Model) {
	modal.SetSize(m.width, m.height)
	m.modal = &modal
}

// --- Openers ---

// openConfigSetting starts the edit flow for one setting.
func (m Model) openConfigSetting(s configSetting) (tea.Model, tea.Cmd) {
	m.pendingCfg = &s
	m.pendingCfgSub = ""

	switch s.kind {
	case cfgInput:
		modal := modals.NewInput(modals.KindConfigValue, s.title, s.example)
		modal.SetBody(m.configBody(s))
		m.configModal(modal)
	case cfgToggle:
		modal := modals.NewSelect(modals.KindConfigValue, s.title, []string{"on", "off"})
		modal.SetBody(m.configBody(s))
		m.configModal(modal)
	case cfgSelect:
		modal := modals.NewSelect(modals.KindConfigValue, s.title, s.options)
		modal.SetBody(m.configBody(s))
		m.configModal(modal)
	case cfgMap:
		return m.openConfigMap(s)
	case cfgList:
		return m.openConfigList(s)
	}
	return m, nil
}

// openConfigMap lists a map setting's entries (edit / remove) plus an add
// entry.
func (m Model) openConfigMap(s configSetting) (tea.Model, tea.Cmd) {
	var items []string
	switch s.key {
	case "theme.overrides":
		roles := make([]string, 0, len(m.cfg.Theme.Overrides))
		for r := range m.cfg.Theme.Overrides {
			roles = append(roles, r)
		}
		sort.Strings(roles)
		for _, r := range roles {
			items = append(items, r+" = "+m.cfg.Theme.Overrides[r], cfgRemovePrefix+r)
		}
	case "keys":
		actions := make([]string, 0, len(m.cfg.Keys))
		for a := range m.cfg.Keys {
			actions = append(actions, a)
		}
		sort.Strings(actions)
		for _, a := range actions {
			items = append(items, a+" = "+m.cfg.Keys[a], cfgRemovePrefix+a)
		}
	}
	items = append(items, s.addLabel())

	modal := modals.NewSelect(modals.KindConfigChoice, s.title, items)
	modal.SetBody(m.configBody(s))
	m.configModal(modal)
	return m, nil
}

// openConfigList lists a list setting's entries (select one to remove it)
// plus an add entry.
func (m Model) openConfigList(s configSetting) (tea.Model, tea.Cmd) {
	items := append([]string{}, m.configListValues(s)...)
	items = append(items, s.addLabel())

	modal := modals.NewSelect(modals.KindConfigChoice, s.title, items)
	modal.SetBody(m.configBody(s))
	m.configModal(modal)
	return m, nil
}

func (m Model) configListValues(s configSetting) []string {
	switch s.key {
	case "hooks.on_worktree_create":
		return m.cfg.CreateHooks()
	case "hooks.on_worktree_delete":
		return m.cfg.DeleteHooks()
	}
	return nil
}

// --- Submit handlers ---

// onConfigChoice routes a pick from a map/list setting's entry list.
func (m Model) onConfigChoice(value string) (tea.Model, tea.Cmd) {
	if m.pendingCfg == nil {
		return m, nil
	}
	s := *m.pendingCfg

	// Removal of an existing entry (map settings).
	if rest, ok := strings.CutPrefix(value, cfgRemovePrefix); ok {
		m.pendingCfgSub = rest
		modal := modals.NewConfirm(modals.KindConfigConfirm, "Remove "+rest+" from "+s.key+"?")
		m.configModal(modal)
		return m, nil
	}

	// The add entry: pick what to add, then (for maps) its value.
	if value == s.addLabel() {
		m.pendingCfgSub = cfgAddSentinel
		switch s.key {
		case "theme.overrides":
			modal := modals.NewSelect(modals.KindConfigChoice, "Override which role?", themeRoles)
			m.configModal(modal)
		case "keys":
			modal := modals.NewSelect(modals.KindConfigChoice, "Override which action?", keyActionNames())
			m.configModal(modal)
		default: // hooks
			modal := modals.NewInput(modals.KindConfigValue, "Add command to "+s.key, s.example)
			m.configModal(modal)
		}
		return m, nil
	}

	switch s.key {
	case "theme.overrides":
		// "role = color" — edit the color.
		role, color, _ := strings.Cut(value, " = ")
		m.pendingCfgSub = role
		modal := modals.NewInput(modals.KindConfigValue, "Color for "+role, "#ff0000 or 208")
		modal.SetBody(m.configBody(s))
		modal.SetInitial(color)
		m.configModal(modal)
	case "keys":
		// "action = key" — edit the key.
		action, k, _ := strings.Cut(value, " = ")
		m.pendingCfgSub = action
		modal := modals.NewInput(modals.KindConfigValue, "Key for "+action, "e.g. D, ctrl+e")
		modal.SetBody(m.configBody(s))
		modal.SetInitial(k)
		m.configModal(modal)
	default:
		// List settings: selecting an existing command removes it.
		m.pendingCfgSub = value
		modal := modals.NewConfirm(modals.KindConfigConfirm, "Remove this command from "+s.key+"?")
		modal.SetBody(value)
		m.configModal(modal)
	}
	return m, nil
}

// keyActionNames lists every bindable action in keymap-section order.
func keyActionNames() []string {
	var out []string
	for _, sec := range keymapSections {
		out = append(out, sec.actions...)
	}
	return out
}

// onConfigValue applies the final value for the pending setting.
func (m Model) onConfigValue(value string) (tea.Model, tea.Cmd) {
	s := m.pendingCfg
	if s == nil {
		return m, nil
	}
	if s.kind == cfgToggle {
		if value == "on" {
			value = "true"
		} else {
			value = "false"
		}
	}
	if strings.TrimSpace(value) == "" {
		m.status = "config: empty value for " + s.key
		return m, nil
	}
	if s.key == "pkgmgr.search_depth" {
		depth, err := strconv.Atoi(value)
		if err != nil || depth < 0 {
			m.status = "config: pkgmgr.search_depth must be a non-negative integer"
			return m, nil
		}
		value = strconv.Itoa(depth)
	}

	if s.kind == cfgMap {
		return m, saveConfigValue(m.configFile, s.key+"."+m.pendingCfgSub, value)
	}
	return m, saveConfigValue(m.configFile, s.key, value)
}

// onConfigConfirm removes the pending sub-entry after confirmation.
func (m Model) onConfigConfirm() (tea.Model, tea.Cmd) {
	if m.pendingCfg == nil {
		return m, nil
	}
	s := *m.pendingCfg
	switch s.kind {
	case cfgMap:
		return m, unsetConfigValue(m.configFile, s.key+"."+m.pendingCfgSub)
	case cfgList:
		var kept []string
		for _, v := range m.configListValues(s) {
			if v != m.pendingCfgSub {
				kept = append(kept, v)
			}
		}
		return m, saveConfigList(m.configFile, s.key, kept)
	}
	return m, nil
}

// --- Saving + live apply ---

// configSavedMsg reports a finished config write.
type configSavedMsg struct {
	setting string // "key = value" or "removed key"
	err     error
}

func saveConfigValue(file, key, value string) tea.Cmd {
	return func() tea.Msg {
		return configSavedMsg{setting: key + " = " + value, err: config.Set(file, key, value)}
	}
}

func unsetConfigValue(file, key string) tea.Cmd {
	return func() tea.Msg {
		return configSavedMsg{setting: "removed " + key, err: config.Unset(file, key)}
	}
}

func saveConfigList(file, key string, values []string) tea.Cmd {
	return func() tea.Msg {
		return configSavedMsg{setting: key + " (" + fmt.Sprintf("%d", len(values)) + " entries)", err: config.ListSet(file, key, values)}
	}
}

// onConfigSaved reloads the config and applies it live: theme, keys, and
// anything the refresh cascade picks up (upstream metrics via loadWorktrees).
func (m Model) onConfigSaved(msg configSavedMsg) (tea.Model, tea.Cmd) {
	m.pendingCfg = nil
	m.pendingCfgSub = ""
	if msg.err != nil {
		m.status = "config: " + msg.err.Error()
		return m, nil
	}

	cfg, err := config.LoadFor(m.repoDir)
	if err != nil {
		m.status = "config: saved, but reload failed: " + err.Error()
		return m, nil
	}
	m.cfg = cfg
	m.applyConfigLive()

	m.status = "config: " + msg.setting
	if cols := keyCollisions(m.mergedKeyOverrides()); len(cols) > 0 {
		m.status += " (key conflict ignored: " + cols[0] + ")"
	}
	// Recompute metrics/status cheaply (gh results come from cache when fresh).
	return m, loadWorktrees(m.repoDir, false)
}

// applyConfigLive restyles every component and rebuilds keybindings from the
// freshly loaded config.
func (m *Model) applyConfigLive() {
	theme.Current = theme.Resolve(m.currentThemePreset(), m.cfg.Theme.Overrides)
	applyTheme()
	worktreelist.SetTheme(theme.Current)
	modals.SetTheme(theme.Current)
	prefs.SetTheme(theme.Current)
	m.keys = newKeyMap(m.mergedKeyOverrides())
}
