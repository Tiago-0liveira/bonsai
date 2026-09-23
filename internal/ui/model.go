// Package ui is the Bubble Tea presentation layer. It holds no os/exec, file I/O
// or raw git logic — every side effect is delegated to internal/core via tea.Cmd.
package ui

import (
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/core/gh"
	"github.com/Tiago-0liveira/bonsai/internal/core/git"
	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/modals"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/prefs"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/terminal"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/worktreelist"
	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
)

// focusArea identifies which pane owns keyboard focus.
type focusArea int

const (
	focusList focusArea = iota
	focusTerminal
)

// rightTab identifies which tab the right pane is showing.
type rightTab int

const (
	tabLog rightTab = iota
	tabProcs
	tabDiff
	tabPR
	tabInspect
	tabChecks
)

// sortMode orders the worktree list.
type sortMode int

const (
	sortName sortMode = iota
	sortAhead
	sortBehind
	sortPR
	sortActivity
	sortDirty
)

// label is the short suffix shown in the list title for a sort mode.
func (s sortMode) label() string {
	switch s {
	case sortAhead:
		return "↑ahead"
	case sortBehind:
		return "↓behind"
	case sortPR:
		return "PR"
	case sortActivity:
		return "activity"
	case sortDirty:
		return "dirty"
	default:
		return "name"
	}
}

// confirmState holds a deferred destructive action awaiting confirmation.
type confirmState struct {
	action tea.Cmd
}

// Model is the root Bubble Tea model.
type Model struct {
	// Dependencies (core layer handles).
	repoDir string
	cfg     *config.Config
	state   *config.State
	procs   *procView
	// ghCache memoizes gh PR/checks queries across refresh cycles.
	ghCache *gh.Cache

	// Components.
	keys keyMap
	list worktreelist.Model
	term terminal.Model

	// modal is non-nil while an overlay is active.
	modal *modals.Model
	// prefs is non-nil while the preferences overlay is open.
	prefs *prefs.Model

	// Data.
	worktrees []git.Worktree
	metrics   map[string]git.Metrics
	fileIndex []string
	prs       []gh.PR
	// prByBranch maps a branch (PR head ref) to its PR (any state), for row badges.
	prByBranch map[string]gh.PR
	// prByNumber maps a PR number to its PR, so "pr-N" worktree branches (whose
	// name never equals the PR head ref) still get state-aware badges.
	prByNumber map[int]gh.PR
	// prDetail caches the full detail of PRs whose tab has been opened, keyed by
	// PR number. prPaneErr holds the last PR-detail load error for display.
	prDetail  map[int]gh.PRDetail
	prPaneErr string
	// prChecks caches CI checks per PR number (for the PR tab checks section).
	prChecks map[int][]gh.Check
	// checkRollup maps a worktree path to its PR's CI rollup ("pass"/"fail"/
	// "pending"/""), for the row badge.
	checkRollup map[string]string
	// statuses maps a worktree path to its working-tree change counts, for the
	// row badge and dirty sorting.
	statuses map[string]git.StatusSummary
	// lastCommit maps a worktree path to its HEAD commit unix time (sortActivity).
	lastCommit map[string]int64

	// activeProc maps a worktree path to the process ID shown in the terminal
	// (the single-view selection; also the most-recently-touched id in multi-view).
	activeProc map[string]int
	// procMultiSel maps a worktree path to the ordered set of process IDs shown
	// together in the merged multi-view. nil/len<=1 means single-process view via
	// activeProc.
	procMultiSel map[string][]int
	// procMultiPickIDs is the ordered id list backing the currently open
	// multi-view picker modal; its MultiSubmitMsg indices refer into it.
	procMultiPickIDs []int
	// procLabelOverride maps a process id to a user-chosen short tag, used to
	// color-tag its lines in the merged multi-view log (see procTag).
	procLabelOverride map[int]string
	// policyModalID is the process id the open restart-policy picker applies to.
	policyModalID int
	// renameProcID is the process id the open tag/rename input applies to.
	renameProcID int
	// procSearch maps a worktree path to its active output search/filter query.
	procSearch map[string]string
	// procSearchActive is true while the inline process-output search box has
	// keyboard focus (Processes tab, "/"); it swallows all keys until esc/enter.
	procSearchActive bool
	procSearchInput  textinput.Model
	// seenProcStatus tracks the last observed status per process ("path#id") so a
	// running→done/failed transition can fire a notification once.
	seenProcStatus map[string]string

	// sort is the current worktree ordering.
	sort sortMode

	// Diff tab (file list) state.
	diffBase        string
	diffPath        string // worktree path the diff belongs to
	diffFiles       []git.DiffFile
	diffCursor      int
	diffFileContent map[string]string // file path -> colored diff (lazy)
	diffModalFile   string            // file whose diff the scroll modal is showing

	// Inspector tab state: cached detail data and the worktree it belongs to.
	inspectPath string
	inspect     inspectorData

	// Checks tab state: branch workflow runs, the last load error, and the
	// worktree path they belong to (staleness guard).
	ciPath string
	ciRuns []gh.Run
	ciErr  string

	// runningSig is a signature of per-worktree running-process counts, used to
	// skip list rebuilds on ticks where nothing changed.
	runningSig string

	// PR tab collapsible-section state.
	prExpandDesc    bool
	prExpandCommits bool
	// pendingConfirm holds a destructive action awaiting a yes/no confirmation.
	pendingConfirm *confirmState
	// pendingPRTitle carries a new PR's title between the title and body prompts.
	pendingPRTitle string

	// pendingAlias holds a new alias name between the name and command prompts.
	pendingAlias string

	// configFile is the .bonsai.yaml the config editors write to.
	configFile string
	// pendingCfg is the setting being edited; pendingCfgSub the selected
	// sub-entry for map/list settings (or cfgAddSentinel mid-flow).
	pendingCfg    *configSetting
	pendingCfgSub string

	// scriptRun maps a script name to its full shell command for the last-opened
	// scripts modal.
	scriptRun map[string]string

	// yankTargets maps a yank-menu label to the text it copies.
	yankTargets map[string]string

	// paletteByLabel maps a command-palette display label to its command for the
	// currently open palette.
	paletteByLabel map[string]paletteCmd

	// procModalRecs maps a process-modal row label to its record (for jump-to).
	procModalRecs map[string]*procstore.Record
	// quitRecs is the ordered list of running processes shown in the quit modal;
	// MultiSubmitMsg indices refer into it.
	quitRecs []*procstore.Record

	// mouseOff disables wheel scrolling for this session, handing the mouse back
	// to the terminal so text can be selected by dragging without holding shift
	// (see the "Mouse" palette command).
	mouseOff bool

	// Layout / status.
	width, height int
	focus         focusArea
	rightTab      rightTab
	logContent    string // last-loaded git log, shown on the Git Log tab
	status        string
	err           error
	ready         bool
}

// New constructs the root model with its core-layer dependencies injected.
func New(repoDir string, cfg *config.Config, state *config.State) Model {
	// Resolve the color palette: a personal in-app pick (state.json) overrides
	// the repo's .bonsai.yaml preset; per-role overrides stay repo-level.
	preset := cfg.Theme.Preset
	if state.Prefs.Theme != "" {
		preset = state.Prefs.Theme
	}
	theme.Current = theme.Resolve(preset, cfg.Theme.Overrides)
	applyTheme()
	worktreelist.SetTheme(theme.Current)
	worktreelist.SetPRStatusMode(state.Prefs.PRStatus)
	modals.SetTheme(theme.Current)
	prefs.SetTheme(theme.Current)

	// Personal key overrides (state.json) override repo-level ones.
	keys := make(map[string]string, len(cfg.Keys)+len(state.Prefs.Keys))
	for k, v := range cfg.Keys {
		keys[k] = v
	}
	for k, v := range state.Prefs.Keys {
		keys[k] = v
	}

	m := Model{
		repoDir:           repoDir,
		cfg:               cfg,
		state:             state,
		procs:             newProcView(repoDir),
		ghCache:           gh.NewCache(gh.DefaultCacheTTL),
		configFile:        config.FileFor(repoDir),
		keys:              newKeyMap(keys),
		list:              worktreelist.New(),
		term:              terminal.New(),
		focus:             focusList,
		sort:              sortModeFromName(state.Prefs.Sort),
		metrics:           map[string]git.Metrics{},
		activeProc:        map[string]int{},
		procMultiSel:      map[string][]int{},
		procLabelOverride: map[int]string{},
		procSearch:        map[string]string{},
		procSearchInput:   textinput.New(),
		prByBranch:        map[string]gh.PR{},
		prDetail:          map[int]gh.PRDetail{},
		prChecks:          map[int][]gh.Check{},
		checkRollup:       map[string]string{},
		statuses:          map[string]git.StatusSummary{},
		lastCommit:        map[string]int64{},
		seenProcStatus:    map[string]string{},
		diffFileContent:   map[string]string{},
		yankTargets:       map[string]string{},
	}
	m.list.Focus()
	m.procSearchInput.Placeholder = "search output…"
	m.term.SetTitle("Git Log")
	if cols := keyCollisions(keys); len(cols) > 0 {
		m.status = "key conflicts ignored: " + cols[0]
	}
	return m
}

// name returns the persisted preference name for a sort mode.
func (s sortMode) name() string {
	switch s {
	case sortAhead:
		return "ahead"
	case sortBehind:
		return "behind"
	case sortPR:
		return "pr"
	case sortActivity:
		return "activity"
	case sortDirty:
		return "dirty"
	default:
		return "name"
	}
}

// sortModeFromName parses a persisted sort preference; unknown names sort by
// name.
func sortModeFromName(name string) sortMode {
	switch name {
	case "ahead":
		return sortAhead
	case "behind":
		return sortBehind
	case "pr":
		return sortPR
	case "activity":
		return sortActivity
	case "dirty":
		return sortDirty
	default:
		return sortName
	}
}

// Init kicks off the first data load.
func (m Model) Init() tea.Cmd {
	return tea.Batch(loadWorktrees(m.repoDir, false), indexFiles(m.repoDir), tickProc(), tickPRs())
}

// selectedWorktree returns the highlighted worktree, if any.
func (m Model) selectedWorktree() (git.Worktree, bool) {
	it, ok := m.list.Selected()
	if !ok {
		return git.Worktree{}, false
	}
	return it.WT, true
}
