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
)

// focusArea identifies which pane owns keyboard focus.
type focusArea int

const (
	focusList focusArea = iota
	focusTerminal
)

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
	// prByBranch maps a branch (PR head ref) to its open PR, for row badges.
	prByBranch map[string]gh.PR

	// activeProc maps a worktree path to the process ID shown in the terminal.
	activeProc map[string]int

	// pendingAlias holds a new alias name between the name and command prompts.
	pendingAlias string

	// Layout / status.
	width, height int
	focus         focusArea
	status        string
	err           error
	ready         bool
}

// New constructs the root model with its core-layer dependencies injected.
func New(repoDir string, cfg *config.Config, state *config.State) Model {
	m := Model{
		repoDir: repoDir,
		cfg:     cfg,
		state:   state,
		procs:   coreexec.NewManager(),
		keys:    newKeyMap(),
		help:    help.New(),
		list:    worktreelist.New(),
		term:    terminal.New(),
		focus:      focusList,
		metrics:    map[string]git.Metrics{},
		activeProc: map[string]int{},
		prByBranch: map[string]gh.PR{},
	}
	m.list.Focus()
	m.term.SetTitle("Git Log")
	return m
}

// Init kicks off the first data load.
func (m Model) Init() tea.Cmd {
	return tea.Batch(loadWorktrees(m.repoDir), indexFiles(m.repoDir), tickProc())
}

// selectedWorktree returns the highlighted worktree, if any.
func (m Model) selectedWorktree() (git.Worktree, bool) {
	it, ok := m.list.Selected()
	if !ok {
		return git.Worktree{}, false
	}
	return it.WT, true
}
