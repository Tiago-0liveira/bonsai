package ui

import (
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/accounts"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/modals"
)

// Layout selects which TUI the root model runs. The redesign is an opt-in
// preview until its workspaces replace the legacy screens.
type Layout string

const (
	LayoutLegacy   Layout = "legacy"
	LayoutRedesign Layout = "redesign"
)

// ParseLayout validates a --tui-layout value.
func ParseLayout(s string) (Layout, error) {
	switch Layout(s) {
	case LayoutLegacy, LayoutRedesign:
		return Layout(s), nil
	}
	return "", fmt.Errorf("invalid --tui-layout %q: want %q or %q", s, LayoutLegacy, LayoutRedesign)
}

// WithLayout selects the TUI layout. The zero value keeps the legacy TUI.
func WithLayout(l Layout) Option {
	return func(m *Model) { m.layoutKind = l }
}

// workspace is one top-level destination of the redesigned shell.
type workspace int

const (
	wsGarden workspace = iota
	wsPlayground
	wsWorktrees
	wsAccounts
	wsSettings
	workspaceCount
)

// workspaceInfo carries the label and the unicode/ASCII icons of a workspace.
// ASCII icons are one cell like the unicode ones so tab widths do not change.
var workspaceInfo = [workspaceCount]struct{ name, icon, ascii string }{
	wsGarden:     {"Garden", "♧", "*"},
	wsPlayground: {"Playground", "▣", "#"},
	wsWorktrees:  {"Worktrees", "⌘", "%"},
	wsAccounts:   {"Accounts", "◎", "@"},
	wsSettings:   {"Settings", "⚙", "="},
}

func (w workspace) name() string { return workspaceInfo[w].name }

// key is the lowercase name persisted as a startup or remembered workspace.
func (w workspace) key() string { return strings.ToLower(workspaceInfo[w].name) }

// workspaceByKey resolves a persisted name; unknown or empty names report false.
func workspaceByKey(k string) (workspace, bool) {
	for i := workspace(0); i < workspaceCount; i++ {
		if i.key() == k {
			return i, true
		}
	}
	return 0, false
}

// startupWorkspace picks where the shell opens: the explicit startup choice,
// else the one remembered for this repository, else Garden. Stored names that
// no longer exist fall through to the next rule.
func (m Model) startupWorkspace() workspace {
	if ws, ok := workspaceByKey(m.work.StartWorkspace); ok {
		return ws
	}
	if !m.work.NoWorkspaceMemory {
		if ws, ok := workspaceByKey(m.state.LastWorkspace(m.repoDir)); ok {
			return ws
		}
	}
	return wsGarden
}

// workspaceSavedMsg reports a finished last-workspace write.
type workspaceSavedMsg struct {
	gen uint64
	err error
}

// selectWorkspace switches the tab, hands the keyboard back to the shell's own
// navigation, and persists the choice through a command when remembering is on.
func (m Model) selectWorkspace(ws workspace) (Model, tea.Cmd) {
	if ws == m.workspace {
		return m, nil
	}
	var detach tea.Cmd
	if m.workspace == wsWorktrees {
		m.worktreesRememberDocument()
		detach = m.terminalRelease()
		m.agents.overlay = nil
	}
	if m.workspace == wsPlayground && m.pg.searching {
		m.pg.searching = false // a half-typed search never outlives the focus that owned it
	}
	var release tea.Cmd
	if m.workspace == wsPlayground {
		// Leaving closes the observed or controlled attachment; the holder keeps running.
		release = m.pgRelease()
		m.pgReviewClose()
		m.pgChangesClose() // the last good data stays for the next visit
	}
	if m.workspace == wsGarden {
		// A read that lands after the user left must not reopen a step for them.
		m.garden.abandon()
		// Leaving closes the attachment to an attempt's terminal; the holder keeps running.
		detach = m.launchDetach()
	}
	if m.wt.returning && ws != m.wt.returnWS && ws != wsWorktrees {
		m.wt.returning = false
	}
	if m.workspace == wsSettings {
		m = m.settingsAutoReviewLeave()
	}
	m.workspace = ws
	m.shellFocus = shellNav
	m.set.editing, m.set.capturing = false, false
	m.set.assistOpen = false
	var cmds []tea.Cmd
	if detach != nil {
		cmds = append(cmds, detach)
	}
	if release != nil {
		cmds = append(cmds, release)
	}
	if ws == wsPlayground {
		// One workspace owns the live attachment: a Garden terminal left over is released first.
		if rel := m.pgTakeOver(); rel != nil {
			cmds = append(cmds, rel)
		}
	}
	if ws == wsAccounts && m.acct.present && !m.acct.started {
		m.acct.started = true
		cmds = append(cmds, m.acct.model.Init())
	}
	if ws == wsGarden {
		// The one network read of the Garden workspace: entering it, never a key.
		var load tea.Cmd
		m, load = m.gardenLoadStatus()
		cmds = append(cmds, load)
	}
	if ws == wsWorktrees {
		var read tea.Cmd
		m, read = m.worktreesEnter()
		cmds = append(cmds, read)
	}
	if ws == wsPlayground {
		var read tea.Cmd
		m, read = m.playgroundEnter()
		cmds = append(cmds, read)
	}
	if !m.work.NoWorkspaceMemory {
		m.wsGen++
		gen, st, dir, name := m.wsGen, m.state, m.repoDir, ws.key()
		cmds = append(cmds, func() tea.Msg {
			return workspaceSavedMsg{gen: gen, err: st.PersistLastWorkspace(gen, dir, name)}
		})
	}
	switch len(cmds) {
	case 0:
		return m, nil
	case 1:
		return m, cmds[0]
	}
	return m, tea.Batch(cmds...)
}

// shellFocus says who owns keyboard input in the redesigned shell. In
// shellNav the shell's tab keys apply. In shellBody the workspace body gets
// each key first, but the quit, palette and help bindings still work because
// they are not text input. In shellInput (a text field, later key capture) the
// body owns every key except ctrl+c, so the shell can never steal keystrokes.
type shellFocus int

const (
	shellNav shellFocus = iota
	shellBody
	shellDialog
	shellInput
)

func (f shellFocus) String() string {
	switch f {
	case shellBody:
		return "Body"
	case shellDialog:
		return "Dialog"
	case shellInput:
		return "Input"
	default:
		return "Navigation"
	}
}

// currentShellFocus resolves the focus, letting an open overlay win.
func (m Model) currentShellFocus() shellFocus {
	if m.modal != nil {
		return shellDialog
	}
	// Terminal input and workspace text fields own their keys before shell navigation.
	if m.pgTerminalMode() || (m.worktreesTerminalShown() && !m.agentOverlayOpen() && m.agentTerminalMode()) || m.worktreesCapturing() {
		return shellInput
	}
	// A text field, dropdown or confirmation inside Accounts takes every key,
	// so typing a name can never quit or jump workspaces.
	if m.workspace == wsAccounts && m.shellFocus == shellBody && m.acct.present && m.acct.model.Capturing() {
		return shellInput
	}
	return m.shellFocus
}

// Outer-region sizes of the shell. Submenu and detail rows are hooks for later
// workspaces and stay zero until one needs them.
const (
	shellHeaderRows = 3
	shellFooterRows = 3
	shellMinWidth   = 64
	shellMinHeight  = 10
	// shellCompactBelow is the width under which tabs drop their gaps.
	shellCompactBelow = 72
)

// shellRegions is the single geometry calculation: exact rows per outer region,
// reserved before the body is sized.
type shellRegions struct {
	width, header, submenu, body, detail, footer int
}

// shellGeometry reserves the fixed regions and gives the body the rest. ok is
// false when the viewport is too small for the chrome.
func shellGeometry(width, height, submenu, detail int) (shellRegions, bool) {
	r := shellRegions{width: width, header: shellHeaderRows, submenu: submenu, detail: detail, footer: shellFooterRows}
	r.body = height - r.header - r.submenu - r.detail - r.footer
	return r, width >= shellMinWidth && height >= shellMinHeight && r.body >= 1
}

// shellUpdate routes the shell and workspace results, then reconciles the one
// live terminal attachment with the workspace that owns it.
func (m Model) shellUpdate(msg tea.Msg) (tea.Model, tea.Cmd) {
	nm, cmd := m.shellUpdateMsg(msg)
	// Like the legacy Agents tab, the Playground reconciles its observed session after every update, so
	// selection, list refresh and focus changes need no per-key attach code.
	if mm, ok := nm.(Model); ok && mm.workspace == wsWorktrees {
		cmd = mm.syncAgentView(cmd)
		return mm, cmd
	}
	if mm, ok := nm.(Model); ok && (mm.workspace == wsPlayground || mm.pg.own) {
		sync := mm.pgSync()
		return mm, tea.Batch(cmd, sync)
	}
	return nm, cmd
}

func (m Model) shellUpdateMsg(msg tea.Msg) (tea.Model, tea.Cmd) {
	if read, ok := msg.(wtReadMsg); ok {
		if (read.prompt && (m.workspace != wsWorktrees || m.currentPath() != read.path)) || m.wt.generations[read.key] != read.gen {
			return m, nil
		}
		msg = read.msg
	}
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height, m.ready = msg.Width, msg.Height, true
		if m.wt.started {
			m.layout()
		}
		m.gardenBoardVisible()
		m.playgroundReconcile()
		m.gardenClampDocument()
		m.gardenClampDraftDocument()
		if m.modal != nil {
			m.modal.SetSize(msg.Width, msg.Height)
		}
		if d := m.garden.planner.dlg; d != nil {
			d.SetSize(m.width, m.gardenBoardRows())
		}
		m.gardenSizePlanEditor()
		m.planClamp()
		// A confirmation that froze other terminal dimensions is read again, so it never starts under old ones.
		m, launchCmd := m.gardenLaunchResize()
		if m.terminalShown() && m.agents.view.term != nil {
			// The terminal keeps the body's size: its viewport now, the agent's own size once the resize settles.
			w, rows := m.agentAreaSize()
			m.agents.view.term.SetViewport(w, rows)
			return m, tea.Batch(launchCmd, m.agentResizeDebounce())
		}
		return m, launchCmd
	case modals.CancelMsg:
		m.pendingConfirm, m.pendingCfg = nil, nil
		m.pendingCfgSub = ""
		m.modal = nil
		m.set.pending = nil
		m.set.revPending = nil
		m.pg.rev.action = pgReviewAction{}
		m.pg.takeFor = ""
		if msg.Kind == modals.KindPlaygroundLinkIssue || msg.Kind == modals.KindPlaygroundLinkConfirm || msg.Kind == modals.KindPlaygroundLinkReplace {
			m.pg.link = pgLinkFlow{} // nothing was written; the screen stays as it was
		}
		return m, nil
	case modals.SubmitMsg:
		kind := msg.Kind
		if kind == modals.KindPalette {
			if _, ok := paletteTargetForLabel(msg.Value); !ok {
				return m.worktreesModalSubmit(msg)
			}
		}
		if m.worktreesModalKind(kind) {
			return m.worktreesModalSubmit(msg)
		}
		m.modal = nil
		switch kind {
		case modals.KindPalette:
			if t, ok := paletteTargetForLabel(msg.Value); ok {
				return m.goToPalette(t)
			}
		case modals.KindConfigConfirm:
			return m.confirmRepoWrite()
		case modals.KindGardenDraftQuit:
			return m.privQuitChoice(msg.Value)
		case modals.KindGardenAutoReviewRestore:
			return m.autoReviewRestore()
		case modals.KindGardenAssistRestore:
			return m.editPrefs(func(p *config.Prefs) { p.GardenAssist = nil })
		case modals.KindGardenReviewEnable:
			return m.reviewConsentConfirmed()
		case modals.KindPlaygroundReviewRetry, modals.KindPlaygroundReviewDismiss, modals.KindPlaygroundReviewDismissAsk, modals.KindPlaygroundReviewAcceptAsk:
			return m.pgReviewSubmit(kind, msg.Value)
		case modals.KindPlaygroundTakeover:
			return m.pgTakeConfirmed()
		case modals.KindPlaygroundLinkIssue:
			return m.pgLinkIssueEntered(msg.Value), nil
		case modals.KindPlaygroundLinkConfirm:
			return m.pgLinkConfirmed(false)
		case modals.KindPlaygroundLinkReplace:
			return m.pgLinkConfirmed(true)
		}
		return m, nil
	case settingsAutoReviewAccountsMsg:
		return m.onSettingsAutoReviewAccounts(msg), nil
	case settingsAutoReviewDefaultsMsg:
		return m.onSettingsAutoReviewDefaults(msg), nil
	case settingsAutoReviewResolvedMsg:
		return m.onSettingsAutoReviewResolved(msg), nil
	case settingsReviewAccountsMsg:
		return m.onSettingsReviewAccounts(msg), nil
	case settingsSavedMsg:
		rollback := m.set.pendingRollback || msg.gen == m.prefsGen && msg.err != nil
		m = m.onSettingsSaved(msg)
		if rollback && !m.set.pendingRollback && m.set.autoReview.accounts.asked {
			m, defaults := m.autoReviewLoadDefaults()
			m, resolve := m.autoReviewResolve()
			return m, tea.Batch(defaults, resolve)
		}
		return m, nil
	case workspaceSavedMsg:
		if msg.err != nil && !errors.Is(msg.err, config.ErrSuperseded) && msg.gen == m.wsGen {
			m.status = "Could not remember workspace: " + msg.err.Error()
		}
		return m, nil
	case configSavedMsg:
		if m.pendingCfg != nil {
			return m.onConfigSaved(msg)
		}
		return m.onRepoConfigSaved(msg), nil
	case tea.MouseMsg:
		if m.workspace == wsWorktrees {
			return m.worktreesMouse(msg)
		}
		if m.workspace == wsGarden {
			return m.gardenMouse(msg)
		}
	case tea.KeyMsg:
		// Typing into an agent terminal (the planner's or the Playground's) comes before everything else: every
		// key but Ctrl+] belongs to the agent, so quit, help, the palette and workspace keys cannot fire.
		if m.terminalTypingMode() {
			return m.agentTerminalKey(msg)
		}
		if m.agents.overlay != nil && m.workspace == wsWorktrees && m.modal == nil {
			return m.overlayUpdate(msg)
		}
		if m.modal != nil && m.modal.Kind() == modals.KindGardenDraftQuit && msg.String() == "ctrl+c" {
			m.modal = nil // a second ctrl+c while the unwritten-edits dialog is open quits without saving
			return m.privQuitChoice("Quit without saving")
		}
		if m.modal != nil {
			nm, cmd := m.modal.Update(msg)
			m.modal = &nm
			return m, cmd
		}
		return m.shellKey(msg)
	}
	if handled, nm, cmd := m.playgroundMsg(msg); handled {
		return nm, cmd
	}
	m, forwarded := m.gardenPlannerForward(msg)
	m, launchFwd := m.gardenLaunchForward(msg)
	m, assistFwd := m.gardenAssistForward(msg)
	forwarded = tea.Batch(forwarded, launchFwd, assistFwd)
	if handled, nm, cmd := m.gardenMsg(msg); handled {
		return nm, tea.Batch(forwarded, cmd)
	}
	if handled, nm, cmd := m.worktreesMsg(msg); handled {
		return nm, tea.Batch(forwarded, cmd)
	}
	_, nm, cmd := m.accountsMsg(msg)
	return nm, tea.Batch(forwarded, cmd)
}

func (m Model) shellKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// ctrl+c quits, except that an open modal owns it first (it cancels the
	// modal; a second ctrl+c then quits). Every other key yields to a body or
	// input that owns the keyboard, including the letters bound to quit, help
	// and the palette.
	if msg.String() == "ctrl+c" {
		return m.quit() // defaults to keeping processes and holders running
	}
	focus := m.currentShellFocus()
	if focus == shellDialog {
		return m, nil
	}
	if focus == shellInput {
		return m.bodyKey(msg)
	}
	// The Issue overlay is modal: quit, help, the palette and workspace keys wait until it is closed.
	if m.gardenOverlayOpen() {
		return m.gardenKey(msg)
	}
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m.quit()
	case key.Matches(msg, m.keys.Palette):
		return m.openShellPalette(), nil
	case key.Matches(msg, m.keys.Help):
		if m.workspace == wsWorktrees && m.shellFocus == shellBody {
			return m.openKeymap()
		}
		return m.openShellHelp(), nil
	case key.Matches(msg, m.keys.Prefs):
		return m.selectWorkspace(wsSettings)
	}
	if focus == shellBody {
		return m.bodyKey(msg)
	}
	if msg.String() == "esc" && m.wt.returning && m.workspace == m.wt.returnWS {
		return m.worktreesReturn()
	}
	next := m.workspace
	switch {
	case m.workspace == wsWorktrees && (msg.String() == "enter" || msg.String() == "down"):
		m.shellFocus = shellBody
		return m, nil
	case m.workspace == wsGarden && (msg.String() == "enter" || msg.String() == "down"):
		return m.gardenEnterBody()
	case m.workspace == wsSettings && (msg.String() == "enter" || msg.String() == "down"):
		m.shellFocus = shellBody
		m.set.level = levelCategory
		return m, nil
	case m.workspace == wsAccounts && (msg.String() == "enter" || msg.String() == "down"):
		m.shellFocus = shellBody
		return m, nil
	case m.workspace == wsPlayground && (msg.String() == "enter" || msg.String() == "down"):
		return m.playgroundEnterBody()
	case key.Matches(msg, m.keys.AgentAccounts):
		// The Agents tab's profiles shortcut lands on the same workspace.
		return m.openAccounts(accounts.TabOverview)
	case key.Matches(msg, m.keys.Tab), msg.String() == "right", msg.String() == "l":
		next = (m.workspace + 1) % workspaceCount
	case key.Matches(msg, m.keys.ShiftTab), msg.String() == "left", msg.String() == "h":
		next = (m.workspace + workspaceCount - 1) % workspaceCount
	default:
		next = altDigitWorkspace(msg, next)
	}
	return m.selectWorkspace(next)
}

// altDigitWorkspace maps alt+digit to a workspace; bare digits belong to agent
// sessions, so they never select.
func altDigitWorkspace(msg tea.KeyMsg, fallback workspace) workspace {
	for i := workspace(0); i < workspaceCount; i++ {
		if msg.String() == fmt.Sprintf("alt+%d", i+1) {
			return i
		}
	}
	return fallback
}

// bodyKey routes a key to the focused workspace body.
func (m Model) bodyKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.workspace != wsSettings && m.workspace != wsAccounts && m.workspace != wsGarden && m.workspace != wsPlayground && m.workspace != wsWorktrees {
		return m, nil
	}
	if m.shellFocus == shellBody && m.currentShellFocus() == shellBody {
		if ws := altDigitWorkspace(msg, m.workspace); ws != m.workspace {
			return m.selectWorkspace(ws)
		}
	}
	switch m.workspace {
	case wsWorktrees:
		return m.worktreesKey(msg)
	case wsAccounts:
		return m.accountsKey(msg)
	case wsGarden:
		return m.gardenKey(msg)
	case wsPlayground:
		return m.playgroundKey(msg)
	}
	nm, cmd := m.settingsKey(msg)
	// Moving onto the Garden category is when Settings first needs its status.
	wm, load := nm.(Model).settingsWarm()
	return wm, tea.Batch(cmd, load)
}

const (
	paletteGoPrefix         = "Go to "
	paletteSettingsPrefix   = "Settings: "
	paletteAccountsPrefix   = "Accounts: "
	palettePlaygroundPrefix = "Playground: "
	paletteEditKeys         = paletteSettingsPrefix + "Edit key bindings"
)

// paletteTarget is where a palette entry leads: a workspace and, for Settings
// entries, one of its categories.
type paletteTarget struct {
	ws          workspace
	category    int
	hasCategory bool
	// section opens an Accounts section instead of the workspace as it was left.
	section      accounts.Tab
	hasSection   bool
	gardenAction string
	// playground opens a Playground submenu entry.
	playground    pgSub
	hasPlayground bool
	worktrees     wtSub
	hasWorktrees  bool
}

// paletteTargets lists every entry in palette order with its target.
func paletteTargets() (labels []string, targets map[string]paletteTarget) {
	targets = map[string]paletteTarget{}
	add := func(label string, t paletteTarget) {
		labels = append(labels, label)
		targets[label] = t
	}
	for i := workspace(0); i < workspaceCount; i++ {
		add(paletteGoPrefix+i.name(), paletteTarget{ws: i})
	}
	keys := 0
	for i, c := range settingCategories {
		add(paletteSettingsPrefix+c.name, paletteTarget{ws: wsSettings, category: i, hasCategory: true})
		if c.id == "keys" {
			keys = i
		}
	}
	add(paletteEditKeys, paletteTarget{ws: wsSettings, category: keys, hasCategory: true})
	add(paletteAccountsPrefix+"Usage", paletteTarget{ws: wsAccounts, section: accounts.TabUsage, hasSection: true})
	add(paletteAccountsPrefix+"Defaults", paletteTarget{ws: wsAccounts, section: accounts.TabDefaults, hasSection: true})
	for sub := pgSub(0); sub < pgSubCount; sub++ {
		add(palettePlaygroundPrefix+pgSubNames[sub], paletteTarget{ws: wsPlayground, playground: sub, hasPlayground: true})
	}
	for sub := wtSub(0); sub < wtSubCount; sub++ {
		add("Worktrees: "+wtSubNames[sub], paletteTarget{ws: wsWorktrees, worktrees: sub, hasWorktrees: true})
	}
	add("Garden: Board", paletteTarget{ws: wsGarden, gardenAction: "board"})
	add("Garden: Table", paletteTarget{ws: wsGarden, gardenAction: "table"})
	add("Garden: Needs you", paletteTarget{ws: wsGarden, gardenAction: "attention"})
	add("Garden: Search approved work", paletteTarget{ws: wsGarden, gardenAction: "search"})
	add("Garden: Refresh", paletteTarget{ws: wsGarden, gardenAction: "refresh"})
	return labels, targets
}

func paletteTargetForLabel(label string) (paletteTarget, bool) {
	_, targets := paletteTargets()
	t, ok := targets[label]
	return t, ok
}

// goToPalette selects a palette target. Going through selectWorkspace keeps the
// last-workspace memory; a Settings category entry lands on its category row.
func (m Model) goToPalette(t paletteTarget) (Model, tea.Cmd) {
	// Leaving the opened Issue's screens closes their terminal attachment; the holder is never stopped.
	var rel tea.Cmd
	if (t.gardenAction == "board" || t.gardenAction == "table" || t.gardenAction == "attention") && m.garden.board.opened != 0 {
		var r1, r2 tea.Cmd
		m, r1 = m.launchClose()
		if m.plannerActive() {
			m, r2 = m.gardenPlannerClose()
		}
		rel = tea.Batch(r1, r2)
	}
	nm, cmd := m.goToPaletteTarget(t)
	return nm, tea.Batch(rel, cmd)
}

func (m Model) goToPaletteTarget(t paletteTarget) (Model, tea.Cmd) {
	if t.hasSection {
		return m.openAccounts(t.section)
	}
	m, cmd := m.selectWorkspace(t.ws)
	if t.hasWorktrees {
		m.shellFocus = shellBody
		nm, read := m.worktreesSetSub(t.worktrees)
		return nm, tea.Batch(cmd, read)
	}
	if t.hasPlayground {
		m.shellFocus = shellBody
		return m.playgroundSetSub(t.playground), cmd
	}
	if t.gardenAction != "" {
		m.shellFocus = shellBody
		if m.garden.hasStatus && m.garden.status.Caps.Read {
			m.garden.sub = subBoard
			if b := &m.garden.board; b.opened != 0 && (t.gardenAction == "board" || t.gardenAction == "table" || t.gardenAction == "attention") {
				b.close() // the detail belongs to the old layout's parent
			}
			switch t.gardenAction {
			case "board":
				m.garden.board.setLayout(layoutBoard)
			case "table":
				m.garden.board.setLayout(layoutTable)
			case "attention":
				m.garden.board.setLayout(layoutAttention)
			}
			if t.gardenAction == "board" || t.gardenAction == "table" || t.gardenAction == "attention" {
				m.gardenBoardVisible()
				remember := m.gardenRememberView()
				var read tea.Cmd
				if !m.garden.board.loaded {
					m, read = m.gardenLoadWork()
				}
				return m, tea.Batch(cmd, remember, read)
			}
			if t.gardenAction == "search" {
				var read tea.Cmd
				if !m.garden.board.loaded {
					m, read = m.gardenLoadWork()
				}
				nm, extra := m.gardenStartSearch()
				return nm, tea.Batch(cmd, read, extra)
			}
			if t.gardenAction == "refresh" || !m.garden.board.loaded {
				return m.gardenLoadWork()
			}
		} else {
			m.garden.notice = "Connect a readable Garden before opening approved work."
			if t.gardenAction == "refresh" {
				return m.gardenLoadStatus()
			}
		}
		return m, cmd
	}
	if t.hasCategory {
		m.shellFocus = shellBody
		m = m.settingsAutoReviewLeave()
		m.set = settingsState{cat: t.category, level: levelCategory, saveErr: m.set.saveErr, autoReview: m.set.autoReview}
		m, load := m.settingsWarm()
		return m, tea.Batch(cmd, load)
	}
	return m, cmd
}

// filterItems is a case-insensitive substring filter over a fixed list.
func filterItems(items []string) modals.FilterFunc {
	return func(q string) []string {
		q = strings.ToLower(strings.TrimSpace(q))
		var out []string
		for _, it := range items {
			if q == "" || strings.Contains(strings.ToLower(it), q) {
				out = append(out, it)
			}
		}
		return out
	}
}

// openShellPalette combines workspace navigation with the existing scoped actions.
func (m Model) openShellPalette() Model {
	nm, _ := m.openPalette()
	return nm.(Model)
}

// shellHints are the navigation hints in the order the footer drops them
// when the width runs out, most important first. Keys are the effective ones,
// so scoped overrides show up.
func (m Model) shellHints() []shellHint {
	if m.worktreesTerminalShown() && !m.agentOverlayOpen() && m.agentTerminalMode() {
		return []shellHint{{"Ctrl+]", "unfocus"}}
	}
	if m.workspace == wsWorktrees && m.shellFocus != shellNav {
		return append(m.worktreesHints(), shellHint{m.keys.Palette.Help().Key, "commands"}, shellHint{m.keys.Help.Help().Key, "keys"})
	}
	if m.currentShellFocus() == shellInput && m.workspace == wsAccounts {
		return m.accountsHints()
	}
	if m.currentShellFocus() == shellInput && m.workspace == wsPlayground {
		return m.playgroundHints()
	}
	if m.currentShellFocus() == shellInput && m.workspace == wsGarden {
		if m.gardenScreen() == scrDrafts {
			return m.gardenDraftsHints()
		}
		if m.planActive() {
			return m.gardenPlanHints()
		}
		if m.launchActive() {
			return m.gardenLaunchHints()
		}
		if m.plannerActive() {
			return m.gardenPlannerHints()
		}
		if m.planForm() != nil {
			return m.planFormHints()
		}
		return []shellHint{{"enter", "apply search"}, {"esc", "cancel"}}
	}
	if m.currentShellFocus() == shellInput {
		if m.set.capturing {
			return []shellHint{{"new key", "bind"}, {"esc", "cancel"}, {"backspace", "reset"}}
		}
		return []shellHint{{"enter", "save"}, {"esc", "cancel"}}
	}
	h := []shellHint{}
	if m.currentShellFocus() == shellNav {
		h = append(h, shellHint{"←/→", "workspace"})
		switch m.workspace {
		case wsSettings:
			h = append(h, shellHint{"enter/↓", "open settings"})
		case wsAccounts:
			h = append(h, shellHint{"enter/↓", "open accounts"})
		case wsPlayground:
			h = append(h, shellHint{"enter/↓", "open sessions"})
		}
	}
	if m.currentShellFocus() == shellBody && m.workspace == wsSettings {
		h = append(h, m.settingsHints()...)
	}
	if m.currentShellFocus() == shellBody && m.workspace == wsAccounts {
		h = append(h, m.accountsHints()...)
	}
	if m.currentShellFocus() == shellBody && m.workspace == wsGarden {
		h = append(h, m.gardenHints()...)
	}
	if m.currentShellFocus() == shellBody && m.workspace == wsPlayground {
		h = append(h, m.playgroundHints()...)
	}
	h = append(h,
		shellHint{m.keys.Palette.Help().Key, "commands"},
		shellHint{m.keys.Help.Help().Key, "keys"},
		shellHint{m.keys.Quit.Help().Key, "quit"},
	)
	if m.currentShellFocus() == shellNav {
		h = append(h,
			shellHint{m.keys.Tab.Help().Key + "/" + m.keys.ShiftTab.Help().Key, "next/prev"},
			shellHint{"alt+1-5", "jump"},
		)
	}
	return h
}

type shellHint struct{ key, desc string }

// openShellHelp is the key reference for the shell: the keys that work here,
// not the legacy worktree bindings.
func (m Model) openShellHelp() Model {
	var items []string
	extra := []shellHint{{"h/l", "workspace"}, {m.keys.AgentAccounts.Help().Key, "open Accounts"}}
	for _, h := range append(m.shellHints(), extra...) {
		items = append(items, m.chrome(fmt.Sprintf("%-10s %s", h.key, h.desc)))
	}
	modal := modals.NewFuzzy(modals.KindKeymap, "Keybindings (redesign preview)", filterItems(items), items)
	modal.SetSize(m.width, m.height)
	m.modal = &modal
	return m
}
