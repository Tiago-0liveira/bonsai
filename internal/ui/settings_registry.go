package ui

import (
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/assist"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/accounts"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/prefs"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/worktreelist"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
)

// fieldScope says where a setting lives, so the UI never implies a repository
// write is a personal preference or the other way round.
type fieldScope int

const (
	scopeMachine fieldScope = iota // personal, state.json
	scopeRepo                      // the repository's .bonsai.yaml
	scopeNone                      // not available yet
)

type fieldKind int

const (
	kindChoice fieldKind = iota
	kindToggle
	kindText
	kindReadonly
	// kindKey is a key binding edited by pressing the new key.
	kindKey
	// kindLink opens another workspace, where the setting really lives.
	kindLink
	// kindUnavailable fields render dim with no control. A later plan flips a
	// field by giving it a real kind, getter and writer.
	kindUnavailable
)

// settingChoice is one selectable value: label is shown, value is stored.
type settingChoice struct {
	label, value string
	// off is why the choice cannot be picked; it stays listed so the reason can be shown.
	off string
}

// settingField is one row of a section. Machine fields write through set;
// repository fields name the .bonsai.yaml key in repoKey and are written only
// after a confirmation.
type settingField struct {
	id, label, help string
	scope           fieldScope
	kind            fieldKind
	choices         []settingChoice
	get             func(Model) string // stored value: a choice value or the text
	set             func(*config.Prefs, string)
	repoKey         string
	action          string             // kindKey: the keymap action name
	display         func(Model) string // chip text when it differs from the choice label
	origin          func(Model) string // where the effective value comes from
	show            func(Model) bool   // nil means always
	validate        func(string) error
	open            func(Model) (Model, tea.Cmd) // kindLink: switches to where the setting lives
	note            string                       // unavailable: what will deliver it
	// apply replaces the plain write for a choice or toggle that needs more than a store: a consent, a refusal.
	apply func(Model, string) (tea.Model, tea.Cmd)
	// dyn lists the choices when they depend on state read at run time; helpFn is help that does too.
	dyn    func(Model) []settingChoice
	helpFn func(Model) string
}

type settingSection struct {
	id, name string
	fields   []settingField
	// extra renders read-only lines under the fields, e.g. the swatch strip.
	extra func(m Model, width int) []string
}

type settingCategory struct {
	id, name string
	sections []settingSection
}

// settingCategories is the registry, in display order. It is filled in init
// because the field closures reach back into Model, which reads the registry.
var settingCategories []settingCategory

func init() {
	settingCategories = []settingCategory{
		{id: "general", name: "General", sections: []settingSection{generalStartup(), generalEditor(), generalRepository()}},
		{id: "appearance", name: "Appearance", sections: appearanceSections()},
		keysCategory(),
		{id: "garden", name: "Garden", sections: gardenSections()},
		{id: "agents", name: "Agents", sections: agentSections()},
	}
}

// visibleFields are the section's fields that apply right now.
func (m Model) visibleFields(sec settingSection) []settingField {
	out := make([]settingField, 0, len(sec.fields))
	for _, f := range sec.fields {
		if f.show == nil || f.show(m) {
			out = append(out, f)
		}
	}
	return out
}

// categoryScope summarizes where a category's available fields write.
func (m Model) categoryScope(c settingCategory) string {
	var machine, repo bool
	for _, sec := range c.sections {
		for _, f := range m.visibleFields(sec) {
			switch f.scope {
			case scopeMachine:
				machine = true
			case scopeRepo:
				repo = true
			}
		}
	}
	switch {
	case machine && repo:
		return "Mixed"
	case machine:
		return "This machine"
	case repo:
		return "Repository"
	}
	return "None yet"
}

// configSettingByKey reuses the repo-config descriptions instead of copying.
func configSettingByKey(key string) configSetting {
	for _, s := range configSettings {
		if s.key == key {
			return s
		}
	}
	return configSetting{key: key}
}

func boolChoices() []settingChoice {
	return []settingChoice{{label: "On", value: "true"}, {label: "Off", value: "false"}}
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func titleCase(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func choicesOf(values []string) []settingChoice {
	out := make([]settingChoice, len(values))
	for i, v := range values {
		out[i] = settingChoice{label: titleCase(v), value: v}
	}
	return out
}

func unavailable(id, label, note string) settingField {
	return settingField{id: id, label: label, kind: kindUnavailable, scope: scopeNone, note: note}
}

// repoToggle is a boolean .bonsai.yaml setting; its help text is the existing
// config description.
func repoToggle(id, label, key string, get func(*config.Config) bool) settingField {
	return settingField{
		id: id, label: label, scope: scopeRepo, kind: kindToggle, repoKey: key,
		help:    configSettingByKey(key).desc,
		choices: boolChoices(),
		get:     func(m Model) string { return strconv.FormatBool(get(m.cfg)) },
		origin:  func(Model) string { return "repo .bonsai.yaml, else built-in default" },
	}
}

func generalStartup() settingSection {
	startup := []settingChoice{{label: "Last visited", value: ""}}
	for ws := workspace(0); ws < workspaceCount; ws++ {
		startup = append(startup, settingChoice{label: ws.name(), value: ws.key()})
	}
	return settingSection{id: "startup", name: "Startup", fields: []settingField{
		{
			id: "start_workspace", label: "Startup workspace", scope: scopeMachine, kind: kindChoice,
			help:    "Workspace shown when bonsai starts. Last visited reopens where you left off in each repository.",
			choices: startup,
			get:     func(m Model) string { return m.work.StartWorkspace },
			set:     func(p *config.Prefs, v string) { p.StartWorkspace = v },
			origin:  func(m Model) string { return personalOrDefault(m.work.StartWorkspace != "") },
		},
		{
			id: "remember", label: "Remember per repo", scope: scopeMachine, kind: kindToggle,
			help:    "Remember the last workspace separately for every local repository.",
			choices: boolChoices(),
			get:     func(m Model) string { return strconv.FormatBool(!m.work.NoWorkspaceMemory) },
			set:     func(p *config.Prefs, v string) { p.NoWorkspaceMemory = v == "false" },
			origin:  func(m Model) string { return personalOrDefault(m.work.NoWorkspaceMemory) },
		},
	}}
}

func personalOrDefault(personal bool) string {
	if personal {
		return "personal"
	}
	return "default"
}

func generalEditor() settingSection {
	return settingSection{id: "editor", name: "Editor & lists", fields: []settingField{
		{
			id: "editor", label: "Editor command", scope: scopeMachine, kind: kindText,
			help: "Command for the open-editor action. Empty inherits the repo setting, then $VISUAL/$EDITOR, then vi.",
			get:  func(m Model) string { return m.work.Editor },
			set:  func(p *config.Prefs, v string) { p.Editor = v },
			display: func(m Model) string {
				if ed := m.effectiveEditor(); ed != "" {
					return ed
				}
				return "inherit"
			},
			origin: func(m Model) string {
				switch {
				case m.work.Editor != "":
					return "personal"
				case m.cfg.Editor != "":
					return "repo .bonsai.yaml"
				}
				return "default $VISUAL/$EDITOR/vi"
			},
		},
		{
			id: "sort", label: "Default sort", scope: scopeMachine, kind: kindChoice,
			help:    "Order of the worktree list.",
			choices: choicesOf(prefs.SortModes),
			get:     func(m Model) string { return orDefault(m.work.Sort, prefs.SortModes[0]) },
			set:     func(p *config.Prefs, v string) { p.Sort = v },
			origin:  func(m Model) string { return personalOrDefault(m.work.Sort != "") },
		},
		{
			id: "pr_status", label: "PR status in list", scope: scopeMachine, kind: kindChoice,
			help:    "How much pull-request state each worktree row shows.",
			choices: choicesOf(worktreelist.PRStatusModes),
			get:     func(m Model) string { return orDefault(m.work.PRStatus, worktreelist.PRStatusModes[0]) },
			set:     func(p *config.Prefs, v string) { p.PRStatus = v },
			origin:  func(m Model) string { return personalOrDefault(m.work.PRStatus != "") },
		},
		{
			id: "prune_merge", label: "Merge PR when pruning", scope: scopeMachine, kind: kindToggle,
			help:    "Pre-select merging the pull request in the prune dialog.",
			choices: boolChoices(),
			get:     func(m Model) string { return strconv.FormatBool(m.work.PruneMerge) },
			set:     func(p *config.Prefs, v string) { p.PruneMerge = v == "true" },
			origin:  func(m Model) string { return personalOrDefault(m.work.PruneMerge) },
		},
	}}
}

func generalRepository() settingSection {
	return settingSection{id: "repository", name: "Repository", fields: []settingField{
		repoToggle("confirm_destructive", "Confirm destructive actions", "confirm_destructive",
			func(c *config.Config) bool { return c.ConfirmDestructive }),
		repoToggle("notify_process", "Notify when a process ends", "notifications.process",
			func(c *config.Config) bool { return c.Notifications.Process }),
		repoToggle("notify_ci", "Notify on CI change", "notifications.ci",
			func(c *config.Config) bool { return c.Notifications.CI }),
	}}
}

func appearanceSections() []settingSection {
	themeChoices := append([]settingChoice{{label: "Inherit", value: ""}}, choicesOf(theme.Presets())...)
	return []settingSection{
		{id: "theme", name: "Theme", fields: []settingField{
			{
				id: "theme", label: "Theme", scope: scopeMachine, kind: kindChoice,
				help:    "Color palette. Inherit follows the repository's theme.preset, then bonsai.",
				choices: themeChoices,
				get:     func(m Model) string { return m.work.Theme },
				set:     func(p *config.Prefs, v string) { p.Theme = v },
				display: func(m Model) string {
					if m.work.Theme == "" {
						return "Inherit (" + m.currentThemePreset() + ")"
					}
					return titleCase(m.work.Theme)
				},
				origin: func(m Model) string {
					switch {
					case m.work.Theme != "":
						return "personal"
					case m.cfg.Theme.Preset != "":
						return "repo .bonsai.yaml theme.preset"
					}
					return "default bonsai"
				},
			},
			{
				id: "theme_overrides", label: "Repo overrides", scope: scopeRepo, kind: kindReadonly,
				help: "Per-role colors from .bonsai.yaml theme.overrides apply on top of the chosen palette.",
				show: func(m Model) bool { return len(m.cfg.Theme.Overrides) > 0 },
				display: func(m Model) string {
					return "repo overrides apply on top: " + strconv.Itoa(len(m.cfg.Theme.Overrides)) + " roles"
				},
				origin: func(Model) string { return "repo .bonsai.yaml theme.overrides" },
			},
		}},
		{id: "surface", name: "Surface shades", extra: swatchStrip, fields: []settingField{{
			id: "contrast", label: "Surface contrast", scope: scopeMachine, kind: kindChoice,
			help:    "Layered keeps the theme's stepped neutrals; Flat draws every surface in the base color.",
			choices: []settingChoice{{label: "Layered neutrals", value: "layered"}, {label: "Flat", value: "flat"}},
			get:     func(m Model) string { return orDefault(m.work.Contrast, "layered") },
			set:     func(p *config.Prefs, v string) { p.Contrast = v },
			origin:  func(m Model) string { return personalOrDefault(m.work.Contrast != "") },
		}}},
		{id: "icons", name: "Icons and glyphs", fields: []settingField{{
			id: "icons", label: "Icons", scope: scopeMachine, kind: kindChoice,
			help:    "ASCII fallback replaces box-drawing and symbol glyphs for fonts or terminals that lack them.",
			choices: []settingChoice{{label: "Unicode", value: ""}, {label: "ASCII fallback", value: "ascii"}},
			get:     func(m Model) string { return m.work.AgentGlyphs },
			set:     func(p *config.Prefs, v string) { p.AgentGlyphs = v },
			origin:  func(m Model) string { return personalOrDefault(m.work.AgentGlyphs != "") },
		}}},
		{id: "density", name: "Density", fields: []settingField{{
			id: "density", label: "Density", scope: scopeMachine, kind: kindChoice,
			help:    "Compact removes the blank row between settings rows.",
			choices: []settingChoice{{label: "Comfortable", value: "comfortable"}, {label: "Compact", value: "compact"}},
			get:     func(m Model) string { return orDefault(m.work.Density, "comfortable") },
			set:     func(p *config.Prefs, v string) { p.Density = v },
			origin:  func(m Model) string { return personalOrDefault(m.work.Density != "") },
		}}},
	}
}

// keyLabels disambiguate actions whose help text is shared.
var keyLabels = map[string]string{"focus_next": "Focus next", "focus_prev": "Focus previous"}

// keysCategory builds one section per keymap section plus the fixed terminal
// key. Rows are personal overrides (state.json), so the scope is This machine.
func keysCategory() settingCategory {
	var secs []settingSection
	for _, sec := range keymapSections {
		var fields []settingField
		for _, name := range sec.actions {
			label := keyLabels[name]
			if label == "" {
				label = titleCase(defaultBindings[name].desc)
			}
			fields = append(fields, settingField{
				id: "key_" + name, label: label, scope: scopeMachine, kind: kindKey, action: name,
				help: fmt.Sprintf("Key for '%s' in %s. It may repeat a key used in a tab that is never active together with this one.", defaultBindings[name].desc, sec.name),
				get:  func(m Model) string { return effectiveKey(name, m.mergedKeyOverrides()) },
				origin: func(m Model) string {
					switch {
					case m.work.Keys[name] != "":
						return "personal override"
					case m.cfg.Keys[name] != "":
						return "repo .bonsai.yaml"
					}
					return "built-in default"
				},
			})
		}
		secs = append(secs, settingSection{id: "keys_" + sec.name, name: sec.name, fields: fields})
	}
	secs = append(secs, settingSection{id: "terminal", name: "Terminal", fields: []settingField{{
		id: "terminal_escape", label: "Leave agent input", scope: scopeNone, kind: kindReadonly,
		help:    "Returns from an agent's terminal to bonsai. Fixed so it can never be rebound away.",
		display: func(Model) string { return agentTerminalKeys[0].key + " (fixed)" },
		origin:  func(Model) string { return "fixed" },
	}}})
	return settingCategory{id: "keys", name: "Keys", sections: secs}
}

// gardenStatusField is a read-only row drawn from the status the Garden
// workspace reads. Settings never calls the backend a second time: until a
// status exists the row says why instead.
func gardenStatusField(id, label, help string, text func(gardenapi.StatusView) string) settingField {
	return settingField{
		id: id, label: label, scope: scopeNone, kind: kindReadonly, help: help,
		display: func(m Model) string {
			g := m.garden
			switch {
			case g.api == nil:
				return "unavailable: no Garden backend in this session"
			case g.hasStatus:
				return text(g.status)
			case g.err != nil:
				return g.err.Message
			case g.busy != busyNone:
				return "reading…"
			}
			return "not read yet: open Garden setup"
		},
		origin: func(Model) string { return "GitHub, read by Garden setup" },
	}
}

func gardenSections() []settingSection {
	return []settingSection{
		{id: "repository", name: "Repository", fields: []settingField{
			{
				id: "repo_name", label: "Repository", scope: scopeRepo, kind: kindReadonly,
				help:    "Local repository this bonsai session runs in.",
				display: func(m Model) string { return filepath.Base(m.repoDir) },
				origin:  func(Model) string { return "current directory" },
			},
			{
				id: "upstream", label: "Base ref", scope: scopeRepo, kind: kindText, repoKey: "upstream",
				help:   configSettingByKey("upstream").desc,
				get:    func(m Model) string { return m.cfg.Upstream },
				origin: func(Model) string { return "repo .bonsai.yaml, else origin/main" },
				validate: func(v string) error {
					if strings.TrimSpace(v) == "" {
						return errors.New("base ref must not be empty")
					}
					return nil
				},
			},
			{
				id: "remote", label: "Remote", scope: scopeRepo, kind: kindReadonly,
				help:    "Remote part of the base ref.",
				display: func(m Model) string { return config.RemoteOf(m.cfg.Upstream) },
				origin:  func(Model) string { return "derived from base ref" },
			},
			gardenStatusField("garden_repo", "GitHub repository", "The repository Garden tracks, found through your remotes.",
				func(v gardenapi.StatusView) string {
					switch {
					case v.Ambiguous:
						return "several repositories: choose in Garden setup"
					case v.Target.RepoID == 0:
						return "not detected"
					case len(v.Target.Aliases) > 0:
						return v.Target.FullName + " · " + strings.Join(v.Target.Aliases, ", ")
					}
					return v.Target.FullName
				}),
			gardenStatusField("garden_identity", "GitHub identity", "The GitHub account Garden uses. It is not a coding-agent account; those live in Accounts.",
				func(v gardenapi.StatusView) string {
					login := orDefault(v.Identity.Login, "unknown")
					return login + " · " + accessWord(v.Access)
				}),
			gardenStatusField("garden_state", "Garden", "Whether this clone has a Garden, and in which state.",
				func(v gardenapi.StatusView) string { return gardenStateLabel(v.State) }),
			{
				id: "garden_link", label: "Garden setup", scope: scopeNone, kind: kindLink,
				help:    "Set up, resume or turn off Garden for this repository. Open the Garden workspace.",
				display: func(Model) string { return "Open Garden setup ›" },
				origin:  func(Model) string { return "shared with the Garden workspace" },
				open:    func(m Model) (Model, tea.Cmd) { return m.openGarden() },
			},
		}},
		{id: "approval", name: "Approval gates", fields: []settingField{
			unavailable("approval_gates", "Approval before runs", "Unavailable — arrives with Garden planning (plan 08)"),
		}},
		{id: "issue_import", name: "Issue import", fields: []settingField{
			unavailable("issue_import", "Import issues", "Unavailable — Garden issues (plan 07)"),
		}},
		{id: "sync", name: "Sync", fields: []settingField{
			gardenStatusField("sync_state", "Sync state", "Connection state of this clone's Garden, as Garden setup reads it.",
				func(v gardenapi.StatusView) string { return orDefault(v.Sync, string(v.State)) }),
			gardenStatusField("sync_operation", "Operation", "Progress of the active or last interrupted setup.",
				func(v gardenapi.StatusView) string {
					if v.Operation == nil {
						return "none"
					}
					return fmt.Sprintf("%d of %d steps · %s", v.Operation.Done, v.Operation.Total, v.Operation.ID)
				}),
			gardenStatusField("sync_last", "Last sync", "When the Garden was last read from GitHub.",
				func(v gardenapi.StatusView) string {
					if v.LastSync.IsZero() {
						return "never"
					}
					return v.LastSync.Local().Format("2006-01-02 15:04")
				}),
		}},
		gardenAssistSection(),
	}
}

func agentSections() []settingSection {
	return []settingSection{
		{id: "accounts", name: "Accounts and launch", fields: []settingField{
			{
				// Launch defaults belong to each profile; Bonsai keeps no separate
				// default account, so this opens the one place they are edited.
				id: "agent_launch", label: "Account defaults", scope: scopeMachine, kind: kindLink,
				help:    "Launch defaults are stored per account. Open Accounts to edit them.",
				display: func(Model) string { return "Open Accounts ›" },
				origin:  func(Model) string { return "local profiles, shared by every worktree" },
				open:    func(m Model) (Model, tea.Cmd) { return m.openAccounts(accounts.TabDefaults) },
			},
		}},
		gardenReviewSection(),
		{id: "issue_setup", name: "Issue setup", fields: []settingField{
			unavailable("issue_setup", "Prepare agents from issues", "Unavailable — Garden issues (plan 07)"),
		}},
	}
}

func gardenAssistSection() settingSection {
	var choices []settingChoice
	for n := 1; n <= assist.MaxQuestions; n++ {
		v := strconv.Itoa(n)
		choices = append(choices, settingChoice{label: v, value: v})
	}
	return settingSection{id: "assist", name: "AI drafting", fields: []settingField{
		{
			id: "assist_account", label: "Assistant", scope: scopeMachine, kind: kindLink,
			help:    "Default account, model and effort for new AI drafting sessions. Empty model and effort inherit the account profile defaults.",
			display: func(m Model) string { return m.assistDefaultsSummary() },
			origin: func(m Model) string {
				p := m.work.GardenAssist
				return personalOrDefault(p != nil && (p.Account != "" || p.Model != "" || p.Effort != ""))
			},
			open: func(m Model) (Model, tea.Cmd) { return m.openAssistDefaults() },
		},
		{
			id: "assist_questions", label: "Questions per round", scope: scopeMachine, kind: kindChoice,
			help: "How many questions to ask per round (1–10). Default: 5.", choices: choices,
			get: func(m Model) string { return strconv.Itoa(m.assistDefaults().Questions) },
			set: func(p *config.Prefs, v string) { gardenAssistPrefs(p).Questions, _ = strconv.Atoi(v) },
			origin: func(m Model) string {
				return personalOrDefault(m.work.GardenAssist != nil && m.work.GardenAssist.Questions != 0)
			},
		},
		{
			id: "assist_guidance", label: "Prompt guidance", scope: scopeMachine, kind: kindText,
			help: "Small preferences such as 'focus on UX' or 'challenge scope'. Built-in rules and output format cannot be changed here.",
			get: func(m Model) string {
				if p := m.work.GardenAssist; p != nil {
					return p.Guidance
				}
				return ""
			},
			display: func(m Model) string {
				if p := m.work.GardenAssist; p != nil && p.Guidance != "" {
					return p.Guidance
				}
				return "none"
			},
			set: func(p *config.Prefs, v string) { gardenAssistPrefs(p).Guidance = assist.SanitizeGuidance(v) },
			validate: func(v string) error {
				if utf8.RuneCountInString(v) > assist.MaxGuidanceRunes {
					return errors.New("prompt guidance must be at most 300 runes")
				}
				return nil
			},
			origin: func(m Model) string {
				return personalOrDefault(m.work.GardenAssist != nil && m.work.GardenAssist.Guidance != "")
			},
		},
		{
			id: "assist_rules", label: "Built-in rules", scope: scopeNone, kind: kindReadonly,
			display: func(Model) string { return "Question quality, evidence and output rules are fixed in Bonsai." },
			origin:  func(Model) string { return "fixed" },
		},
		{
			id: "assist_restore", label: "Restore defaults", scope: scopeMachine, kind: kindLink,
			help:    "Clear all personal AI drafting defaults after confirmation.",
			display: func(Model) string { return "Restore defaults ›" },
			open:    func(m Model) (Model, tea.Cmd) { return m.confirmAssistRestore() },
		},
	}}
}
