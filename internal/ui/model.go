// Package ui is the Bubble Tea presentation layer. It holds no os/exec, file I/O
// or raw git logic — every side effect is delegated to internal/core via tea.Cmd.
package ui

import (
	"github.com/charmbracelet/bubbles/help"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	coreexec "github.com/Tiago-0liveira/bonsai/internal/core/exec"
	"github.com/Tiago-0liveira/bonsai/internal/core/gh"
	"github.com/Tiago-0liveira/bonsai/internal/core/git"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/modals"
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
	procs   *coreexec.Manager

	// Components.
	keys keyMap
	help help.Model
	list worktreelist.Model
	term terminal.Model

	// modal is non-nil while an overlay is active.
	modal *modals.Model

	// Data.
	worktrees []git.Worktree
	metrics   map[string]git.Metrics
	fileIndex []string
	prs       []gh.PR
	// prByBranch maps a branch (PR head ref) to its PR (any state), for row badges.
	prByBranch map[string]gh.PR
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

	// activeProc maps a worktree path to the process ID shown in the terminal.
	activeProc map[string]int
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

	// scriptRun maps a script name to its full shell command for the last-opened
	// scripts modal.
	scriptRun map[string]string

	// yankTargets maps a yank-menu label to the text it copies.
	yankTargets map[string]string

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
	// Resolve the color palette from config and push it to every styled component.
	theme.Current = theme.Resolve(cfg.Theme.Preset, cfg.Theme.Overrides)
	applyTheme()
	worktreelist.SetTheme(theme.Current)
	modals.SetTheme(theme.Current)

	m := Model{
		repoDir:         repoDir,
		cfg:             cfg,
		state:           state,
		procs:           coreexec.NewManager(),
		keys:            newKeyMap(cfg.Keys),
		help:            help.New(),
		list:            worktreelist.New(),
		term:            terminal.New(),
		focus:           focusList,
		metrics:         map[string]git.Metrics{},
		activeProc:      map[string]int{},
		prByBranch:      map[string]gh.PR{},
		prDetail:        map[int]gh.PRDetail{},
		prChecks:        map[int][]gh.Check{},
		checkRollup:     map[string]string{},
		statuses:        map[string]git.StatusSummary{},
		lastCommit:      map[string]int64{},
		seenProcStatus:  map[string]string{},
		diffFileContent: map[string]string{},
		yankTargets:     map[string]string{},
	}
	m.list.Focus()
	m.term.SetTitle("Git Log")
	if cols := keyCollisions(cfg.Keys); len(cols) > 0 {
		m.status = "key conflicts ignored: " + cols[0]
	}
	return m
}

// Init kicks off the first data load.
func (m Model) Init() tea.Cmd {
	return tea.Batch(loadWorktrees(m.repoDir), indexFiles(m.repoDir), tickProc(), tickPRs())
}

// selectedWorktree returns the highlighted worktree, if any.
func (m Model) selectedWorktree() (git.Worktree, bool) {
	it, ok := m.list.Selected()
	if !ok {
		return git.Worktree{}, false
	}
	return it.WT, true
}
