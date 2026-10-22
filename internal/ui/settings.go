package ui

import (
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/modals"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/worktreelist"
)

// settingsLevel is how deep the cursor is: category row, section list, fields.
type settingsLevel int

const (
	levelCategory settingsLevel = iota
	levelSection
	levelField
)

// repoWrite is a .bonsai.yaml change waiting for the user's confirmation.
type repoWrite struct{ key, value string }

// settingsState is the Settings workspace's cursor and transient messages.
type settingsState struct {
	level            settingsLevel
	cat, sec, field  int
	editing          bool
	capturing        bool // the next key press is the new binding
	input            textinput.Model
	editErr, saveErr string
	pending          *repoWrite
	assist           *assistOptions
	assistOpen       bool
	// rev is what the Garden review policy section knows about accounts; revPending is the policy a consent
	// dialog is asking about. Nothing is saved until the user confirms it.
	rev        settingsReview
	revPending *config.GardenReviewPrefs
	// inflight counts persists not yet answered; pendingRollback means the
	// newest one failed and the rollback waits for the older ones to land, so
	// disk and UI cannot end up disagreeing.
	inflight        int
	pendingRollback bool
}

// settingsSavedMsg reports a finished personal-preference write.
type settingsSavedMsg struct {
	gen uint64
	err error
}

func (m Model) curCategory() settingCategory { return settingCategories[m.set.cat] }

func (m Model) curSection() settingSection {
	c := m.curCategory()
	return c.sections[min(m.set.sec, len(c.sections)-1)]
}

func (m Model) curFields() []settingField { return m.visibleFields(m.curSection()) }

func (m Model) curField() (settingField, bool) {
	fs := m.curFields()
	if len(fs) == 0 {
		return settingField{}, false
	}
	return fs[min(m.set.field, len(fs)-1)], true
}

// settingsHints are the keys that work at the current level.
func (m Model) settingsHints() []shellHint {
	if m.set.assistOpen {
		return []shellHint{{"tab", "next"}, {"ctrl+s", "save defaults"}, {"esc", "cancel"}}
	}
	tab := m.keys.Tab.Help().Key
	switch m.set.level {
	case levelCategory:
		return []shellHint{{"←/→", "category"}, {"enter", "sections"}, {"esc", "back"}}
	case levelSection:
		return []shellHint{{"↑/↓", "section"}, {"←/→", "category"}, {tab, "field"}, {"esc", "parent"}}
	}
	hints := []shellHint{{"↑/↓", "field"}}
	if f, ok := m.curField(); ok {
		switch f.kind {
		case kindChoice, kindToggle:
			hints = append(hints, shellHint{"←/→", "change"}, shellHint{"enter", "choose"})
		case kindText:
			hints = append(hints, shellHint{"enter", "edit"})
		case kindKey:
			hints = append(hints, shellHint{"enter", "rebind"})
		}
	}
	return append(hints, shellHint{tab, "sections"}, shellHint{"esc", "parent"})
}

// settingsKey handles a key while the Settings body owns the keyboard.
func (m Model) settingsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.set.assistOpen {
		return m.settingsAssistKey(msg)
	}
	if m.set.capturing {
		return m.captureKey(msg)
	}
	if m.set.editing {
		return m.editKey(msg)
	}
	m.status = ""
	m.set.editErr = ""
	k := msg.String()
	if k == "esc" {
		return m.settingsUp(), nil
	}
	isTab := key.Matches(msg, m.keys.Tab)
	switch m.set.level {
	case levelCategory:
		switch {
		case k == "left" || k == "h":
			return m.moveCategory(-1), nil
		case k == "right" || k == "l":
			return m.moveCategory(1), nil
		case k == "enter" || k == "down" || k == "j" || isTab:
			m.set.level = levelSection
		}
	case levelSection:
		switch {
		case k == "left" || k == "h":
			return m.moveCategory(-1), nil
		case k == "right" || k == "l":
			return m.moveCategory(1), nil
		case k == "up" || k == "k":
			m.set.sec = max(m.set.sec-1, 0)
			m.set.field = 0
		case k == "down" || k == "j":
			m.set.sec = min(m.set.sec+1, len(m.curCategory().sections)-1)
			m.set.field = 0
		case k == "enter" || isTab:
			if len(m.curFields()) > 0 {
				m.set.level = levelField
			}
		}
	case levelField:
		switch {
		case k == "up" || k == "k":
			m.set.field = max(m.set.field-1, 0)
		case k == "down" || k == "j":
			m.set.field = max(min(m.set.field+1, len(m.curFields())-1), 0)
		case isTab || key.Matches(msg, m.keys.ShiftTab):
			m.set.level = levelSection
		case k == "left" || k == "h":
			return m.cycleField(-1)
		case k == "right" || k == "l":
			return m.cycleField(1)
		case k == "enter":
			return m.activateField()
		}
	}
	return m, nil
}

// settingsUp goes up exactly one level; from the category row it hands the
// keyboard back to the shell navigation.
func (m Model) settingsUp() Model {
	switch m.set.level {
	case levelField:
		m.set.level = levelSection
	case levelSection:
		m.set.level = levelCategory
	default:
		m.shellFocus = shellNav
	}
	return m
}

func (m Model) moveCategory(d int) Model {
	n := len(settingCategories)
	m.set.cat = (m.set.cat + d + n) % n
	m.set.sec, m.set.field = 0, 0
	return m
}

// cycleField steps a choice or toggle by d; other kinds ignore left/right.
func (m Model) cycleField(d int) (tea.Model, tea.Cmd) {
	f, ok := m.curField()
	if !ok || (f.kind != kindChoice && f.kind != kindToggle) {
		return m, nil
	}
	choices := f.choices
	if f.dyn != nil {
		choices = f.dyn(m)
	}
	cur, n := -1, len(choices)
	if n == 0 {
		if f.apply != nil {
			return f.apply(m, "")
		}
		return m, nil
	}
	for i, c := range choices {
		if c.value == f.get(m) {
			cur = i
		}
	}
	if cur < 0 && d < 0 {
		cur = 0
	}
	idx := cur
	for step := 0; step < n; step++ {
		idx = (idx + d + n) % n
		if choices[idx].off == "" {
			break
		}
		m.status = choices[idx].label + " cannot be chosen: " + choices[idx].off
	}
	if choices[idx].off != "" {
		return m, nil // every choice is disabled: the reason stays in the status
	}
	next := choices[idx].value
	if f.apply != nil {
		return f.apply(m, next)
	}
	if f.scope == scopeRepo {
		return m.openRepoConfirm(f.repoKey, next), nil
	}
	return m.editPrefs(func(p *config.Prefs) { f.set(p, next) })
}

// activateField is enter on a field: cycle, edit, or explain why not.
func (m Model) activateField() (tea.Model, tea.Cmd) {
	f, ok := m.curField()
	if !ok {
		return m, nil
	}
	switch f.kind {
	case kindChoice, kindToggle:
		return m.cycleField(1)
	case kindText:
		return m.beginEdit(f), nil
	case kindKey:
		m.set.capturing = true
		m.set.editErr = ""
		m.shellFocus = shellInput
		return m, nil
	case kindLink:
		return m.openLink(f)
	case kindReadonly:
		m.status = "Read-only: " + f.label
	default:
		m.status = "Not available yet"
	}
	return m, nil
}

// openLink follows a link field to the workspace where its setting lives.
func (m Model) openLink(f settingField) (tea.Model, tea.Cmd) {
	return f.open(m)
}

func (m Model) beginEdit(f settingField) Model {
	in := textinput.New()
	in.Prompt = ""
	in.CharLimit = 200
	if f.id == "assist_guidance" {
		// Let validation explain overlong pasted guidance instead of silently truncating it.
		in.CharLimit = 0
	}
	in.SetValue(f.get(m))
	in.CursorEnd()
	// The shell routes no blink ticks to the field, so a static cursor is the
	// only one that renders reliably.
	in.Cursor.SetMode(cursor.CursorStatic)
	in.Focus()
	m.set.input = in
	m.set.editing = true
	m.set.editErr = ""
	m.shellFocus = shellInput
	return m
}

// editKey drives the inline text edit. Esc cancels only the edit and keeps
// the previous value; the cursor stays on the field.
func (m Model) editKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.set.editing = false
		m.set.editErr = ""
		m.shellFocus = shellBody
		return m, nil
	case "enter":
		return m.commitEdit()
	}
	var cmd tea.Cmd
	m.set.input, cmd = m.set.input.Update(msg)
	return m, cmd
}

func (m Model) commitEdit() (tea.Model, tea.Cmd) {
	f, ok := m.curField()
	if !ok {
		return m, nil
	}
	v := strings.TrimSpace(m.set.input.Value())
	if f.validate != nil {
		if err := f.validate(v); err != nil {
			m.set.editErr = err.Error()
			return m, nil
		}
	}
	m.set.editing = false
	m.set.editErr = ""
	m.shellFocus = shellBody
	if v == f.get(m) {
		m.status = "Unchanged"
		return m, nil
	}
	if f.scope == scopeRepo {
		return m.openRepoConfirm(f.repoKey, v), nil
	}
	return m.editPrefs(func(p *config.Prefs) { f.set(p, v) })
}

// editPrefs applies a personal edit to the working copy, previews it at once,
// and persists it in the background. gen orders overlapping saves.
func (m Model) editPrefs(mutate func(*config.Prefs)) (tea.Model, tea.Cmd) {
	next := m.work.Clone()
	mutate(&next)
	m.work = next
	m.prefsGen++
	m.set.inflight++
	m.set.pendingRollback = false
	m.set.saveErr = ""
	m.applyPreview()
	gen, st, snap := m.prefsGen, m.state, next.Clone()
	return m, func() tea.Msg {
		return settingsSavedMsg{gen: gen, err: st.PersistPrefs(gen, snap)}
	}
}

// applyPreview makes the working preferences visible: palette, contrast,
// keys, sort and PR status. It is deliberately free of list rebuilds, which
// would be domain work.
func (m *Model) applyPreview() {
	m.applyConfigLive()
	worktreelist.SetPRStatusMode(m.work.PRStatus)
	m.sort = sortModeFromName(m.work.Sort)
}

// onSettingsSaved settles a persist. Only the newest generation decides
// success or failure: a late or superseded result must never replace newer
// edits. When the newest fails, the rollback to what is really on disk waits
// until no older save is still in flight, since one of those may yet land.
func (m Model) onSettingsSaved(msg settingsSavedMsg) Model {
	m.set.inflight = max(m.set.inflight-1, 0)
	superseded := errors.Is(msg.err, config.ErrSuperseded)
	if msg.gen == m.prefsGen && !superseded {
		if msg.err == nil {
			m.set.saveErr = ""
			m.status = "Saved"
			return m
		}
		m.set.pendingRollback = true
		m.set.saveErr = "Save failed: " + msg.err.Error()
		m.status = m.set.saveErr
	}
	if m.set.pendingRollback && m.set.inflight == 0 {
		m.set.pendingRollback = false
		m.work = m.state.PrefsSnapshot()
		m.applyPreview()
	}
	return m
}

// openRepoConfirm asks before writing the repository's .bonsai.yaml.
func (m Model) openRepoConfirm(key, value string) Model {
	m.set.pending = &repoWrite{key: key, value: value}
	modal := modals.NewConfirm(modals.KindConfigConfirm, fmt.Sprintf("Write %s = %s to .bonsai.yaml?", key, value))
	modal.SetBody(m.configFile)
	modal.SetSize(m.width, m.height)
	m.modal = &modal
	return m
}

func (m Model) confirmRepoWrite() (tea.Model, tea.Cmd) {
	p := m.set.pending
	m.set.pending = nil
	if p == nil {
		return m, nil
	}
	m.status = "Writing .bonsai.yaml…"
	return m, saveConfigValue(m.configFile, p.key, p.value)
}

// onRepoConfigSaved reloads the repo config after a confirmed write and
// re-applies what it changes. Unlike the legacy handler it starts no domain
// refresh.
func (m Model) onRepoConfigSaved(msg configSavedMsg) Model {
	if msg.err != nil {
		m.set.saveErr = "Save failed: " + msg.err.Error()
		m.status = m.set.saveErr
		return m
	}
	m.set.saveErr = ""
	cfg, err := config.LoadFor(m.repoDir)
	if err != nil {
		m.set.saveErr = "Saved, but reloading .bonsai.yaml failed: " + err.Error()
		m.status = m.set.saveErr
		return m
	}
	before := len(m.curFields())
	m.cfg = cfg
	m.applyConfigLive()
	if len(m.curFields()) != before {
		m.set.field = 0
		if len(m.curFields()) == 0 && m.set.level == levelField {
			m.set.level = levelSection
		}
	}
	m.status = "Wrote " + msg.setting + " to .bonsai.yaml"
	return m
}

// captureKey takes the next key press as the binding for the current field.
// A rejection stays in capture so the user can try another key.
func (m Model) captureKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	f, ok := m.curField()
	if !ok {
		return m, nil
	}
	finish := func(m Model) Model {
		m.set.capturing = false
		m.set.editErr = ""
		m.shellFocus = shellBody
		return m
	}
	desc := defaultBindings[f.action].desc
	def := effectiveKey(f.action, m.cfg.Keys)
	_, personal := m.work.Keys[f.action]
	clear := func(p *config.Prefs) { setKeyOverride(p, f.action, "") }

	candidate := msg.String()
	if candidate != "esc" && candidate != "backspace" && candidate != "delete" &&
		(msg.Paste || (msg.Type == tea.KeyRunes && len(msg.Runes) > 1)) {
		m.set.editErr = "press a single key"
		return m, nil
	}
	restore := func(m Model) (tea.Model, tea.Cmd) {
		// The default may have been taken by another override since.
		if err := validateKeyScope(f.action, def, m.mergedKeyOverridesWithout(f.action)); err != nil {
			m.set.editErr = err.Error()
			return m, nil
		}
		m = finish(m)
		return m.editPrefs(clear)
	}
	switch {
	case candidate == "esc":
		return finish(m), nil
	case candidate == "backspace" || candidate == "delete":
		if !personal {
			m = finish(m)
			m.status = desc + " is already at its default (" + def + ")"
			return m, nil
		}
		return restore(m)
	case candidate == def:
		// Pressing the default restores it even where it is normally fixed or
		// shared (shell is enter, rebase and restart_proc both use r).
		if !personal {
			m = finish(m)
			m.status = desc + " is already at its default (" + def + ")"
			return m, nil
		}
		return restore(m)
	}
	if err := validateKeyBinding(f.action, candidate, m.mergedKeyOverrides()); err != nil {
		m.set.editErr = err.Error()
		return m, nil
	}
	m = finish(m)
	return m.editPrefs(func(p *config.Prefs) { setKeyOverride(p, f.action, candidate) })
}

// setKeyOverride sets or (for "") removes a personal key override, leaving a
// nil map when none remain so the field is omitted from state.json.
func setKeyOverride(p *config.Prefs, action, key string) {
	if key == "" {
		delete(p.Keys, action)
	} else {
		if p.Keys == nil {
			p.Keys = map[string]string{}
		}
		p.Keys[action] = key
	}
	if len(p.Keys) == 0 {
		p.Keys = nil
	}
}

// mergedKeyOverridesWithout is the merged overrides minus action's own personal
// override, i.e. the view in which that action is back at its default.
func (m Model) mergedKeyOverridesWithout(action string) map[string]string {
	out := m.mergedKeyOverrides()
	if _, ok := m.cfg.Keys[action]; ok {
		out[action] = m.cfg.Keys[action]
	} else {
		delete(out, action)
	}
	return out
}

// gardenAssistPrefs allocates the optional defaults only when a preference is edited.
func gardenAssistPrefs(p *config.Prefs) *config.GardenAssistPrefs {
	if p.GardenAssist == nil {
		p.GardenAssist = &config.GardenAssistPrefs{}
	}
	return p.GardenAssist
}

// settingsAssistWarm reads accounts through commands so the Assistant row can name its saved account.
func (m Model) settingsAssistWarm() (Model, tea.Cmd) {
	if m.workspace != wsSettings || m.curCategory().id != "garden" || m.set.assist != nil ||
		m.work.GardenAssist == nil || m.work.GardenAssist.Account == "" || m.agents.api.Accounts == nil {
		return m, nil
	}
	m.set.assist = newAssistOptions(m.agents.api.Accounts, m.agents.usage, m.agentGlyphsASCII(), m.assistDefaults())
	m.set.assist.defaultsMode = true
	return m, m.set.assist.Init()
}

func (m Model) openAssistDefaults() (Model, tea.Cmd) {
	if m.agents.api.Accounts == nil {
		m.status = "Agent accounts are unavailable here"
		return m, nil
	}
	m.set.assist = newAssistOptions(m.agents.api.Accounts, m.agents.usage, m.agentGlyphsASCII(), m.assistDefaults())
	m.set.assist.defaultsMode = true
	m.set.assistOpen = true
	m.shellFocus = shellInput
	return m, m.set.assist.Init()
}

func (m Model) settingsAssistKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	o := m.set.assist
	cmd, done := o.key(msg)
	if !done {
		return m, cmd
	}
	if o.submit {
		o.submit = false
		a, ok := o.Current()
		switch {
		case !ok:
			o.notice = "Choose an account first."
		case !assistSupported(a):
			o.notice = "Choose a signed-in account that supports structured calls."
		case !o.formLoaded:
			o.notice = "Model and effort options must load before saving."
		default:
			values := o.settings()
			m.set.assistOpen = false
			m.shellFocus = shellBody
			return m.editPrefs(func(p *config.Prefs) {
				def := gardenAssistPrefs(p)
				def.Account, def.Model, def.Effort = string(values.AccountID), values.Model, values.Effort
			})
		}
		return m, nil
	}
	m.set.assistOpen = false
	m.shellFocus = shellBody
	return m, cmd
}

func (m Model) assistDefaultsSummary() string {
	p := m.work.GardenAssist
	if p == nil || p.Account == "" {
		return "ask each time"
	}
	o := m.set.assist
	if m.agents.api.Accounts == nil {
		return "missing: choose again"
	}
	if o == nil || !o.Listed() {
		return "reading accounts…"
	}
	if o.ListErr() != "" {
		return "accounts unavailable: choose again"
	}
	for _, a := range o.Accounts() {
		if string(a.ID) == p.Account && assistSupported(a) {
			return boardTitle(a.Name) + " · " + orDefault(p.Model, "profile default") + " · " + orDefault(p.Effort, "profile default")
		}
	}
	return "missing: choose again"
}

func (m Model) confirmAssistRestore() (Model, tea.Cmd) {
	modal := modals.NewConfirm(modals.KindGardenAssistRestore, "Restore AI drafting defaults?")
	modal.SetBody("Clear the saved assistant, questions per round and prompt guidance.")
	modal.SetSize(m.width, m.height)
	m.modal = &modal
	return m, nil
}
