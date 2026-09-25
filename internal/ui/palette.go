package ui

import (
	"strconv"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	coreexec "github.com/Tiago-0liveira/bonsai/internal/core/exec"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/modals"
)

// paletteCmd is one executable entry in the command palette. scopeHint names the
// worktree/PR the command would act on ("" for global commands); available
// reports whether it can run right now, with a reason when it cannot.
type paletteCmd struct {
	label     string
	section   string      // group name for the sectioned palette
	binding   key.Binding // zero value when there is no direct keybinding
	scopeHint func(m Model) string
	available func(m Model) (bool, string)
	run       func(m Model) (tea.Model, tea.Cmd)
}

// tag stamps a section name onto a group of commands.
func tag(section string, cs []paletteCmd) []paletteCmd {
	for i := range cs {
		cs[i].section = section
	}
	return cs
}

// --- Scope hints ---

func wtScope(m Model) string {
	if wt, ok := m.selectedWorktree(); ok {
		return wt.Branch
	}
	return ""
}

func prScope(m Model) string {
	wt, ok := m.selectedWorktree()
	if !ok {
		return ""
	}
	if n, isPR := m.prForBranch(wt.Branch); isPR {
		return "PR #" + strconv.Itoa(n)
	}
	return ""
}

// --- Availability checks ---

func needWorktree(m Model) (bool, string) {
	if _, ok := m.selectedWorktree(); ok {
		return true, ""
	}
	return false, "no worktree selected"
}

func needFeatureWorktree(m Model) (bool, string) {
	wt, ok := m.selectedWorktree()
	if !ok {
		return false, "no worktree selected"
	}
	if wt.IsMain {
		return false, "main worktree"
	}
	return true, ""
}

func needBranch(m Model) (bool, string) {
	if ok, reason := needWorktree(m); !ok {
		return false, reason
	}
	wt, _ := m.selectedWorktree()
	if wt.Branch == "" || wt.Branch == "(detached)" {
		return false, "HEAD is detached"
	}
	return true, ""
}

func needPR(m Model) (bool, string) {
	if ok, reason := needWorktree(m); !ok {
		return false, reason
	}
	wt, _ := m.selectedWorktree()
	if _, isPR := m.prForBranch(wt.Branch); !isPR {
		return false, "no PR connected"
	}
	return true, ""
}

func needCreatePR(m Model) (bool, string) {
	if ok, reason := needFeatureWorktree(m); !ok {
		return false, reason
	}
	wt, _ := m.selectedWorktree()
	if _, isPR := m.prForBranch(wt.Branch); isPR {
		return false, "worktree already has a PR"
	}
	return true, ""
}

func needProcess(m Model) (bool, string) {
	if ok, reason := needWorktree(m); !ok {
		return false, reason
	}
	wt, _ := m.selectedWorktree()
	if _, ok := m.selectedProcID(wt.Path); !ok {
		return false, "no process in this worktree"
	}
	return true, ""
}

// mouseToggleLabel names what the mouse toggle would do next.
func mouseToggleLabel(m Model) string {
	if m.mouseOff {
		return "Mouse: enable wheel scrolling"
	}
	return "Mouse: disable (drag-select text without shift)"
}

// toggleMouse turns wheel scrolling on or off for this session. With mouse
// tracking on, most terminals need shift held to select text by dragging;
// turning it off gives plain drag-select back at the cost of the wheel.
func (m Model) toggleMouse() (tea.Model, tea.Cmd) {
	m.mouseOff = !m.mouseOff
	if m.mouseOff {
		m.status = "mouse off — drag to select text; wheel no longer scrolls"
		return m, tea.DisableMouse
	}
	m.status = "mouse on — wheel scrolls; hold shift to select text"
	return m, tea.EnableMouseCellMotion
}

// --- Run helpers ---

// runOnWorktree wraps an op that needs the selected worktree's path.
func runOnWorktree(f func(m Model, path string) (tea.Model, tea.Cmd)) func(Model) (tea.Model, tea.Cmd) {
	return func(m Model) (tea.Model, tea.Cmd) {
		wt, ok := m.selectedWorktree()
		if !ok {
			return m, nil
		}
		return f(m, wt.Path)
	}
}

// prNumber resolves the selected worktree's PR number (0 when absent).
func (m Model) prNumber() int {
	wt, ok := m.selectedWorktree()
	if !ok {
		return 0
	}
	n, _ := m.prForBranch(wt.Branch)
	return n
}

// paletteCommands enumerates every action the palette offers, built from live
// state so bindings, scope hints, and availability are current at open time.
func (m Model) paletteCommands() []paletteCmd {
	wt := func(label string, b key.Binding, run func(Model) (tea.Model, tea.Cmd)) paletteCmd {
		return paletteCmd{label: label, binding: b, scopeHint: wtScope, available: needWorktree, run: run}
	}
	feature := func(label string, b key.Binding, run func(Model) (tea.Model, tea.Cmd)) paletteCmd {
		return paletteCmd{label: label, binding: b, scopeHint: wtScope, available: needFeatureWorktree, run: run}
	}
	pr := func(label string, run func(Model) (tea.Model, tea.Cmd)) paletteCmd {
		return paletteCmd{label: label, scopeHint: prScope, available: needPR, run: run}
	}
	global := func(label string, b key.Binding, run func(Model) (tea.Model, tea.Cmd)) paletteCmd {
		return paletteCmd{label: label, binding: b, run: run}
	}

	cmds := tag("Worktree & git", []paletteCmd{
		global("New worktree", m.keys.Create, Model.openCreateSourceModal),
		global("Prune merged worktrees", m.keys.BulkPrune, Model.openBulkPruneModal),
		global("Refresh", m.keys.Refresh, func(m Model) (tea.Model, tea.Cmd) {
			return m, loadWorktrees(m.repoDir, true)
		}),
		wt("Open shell", m.keys.Enter, Model.openShell),
		wt("Open editor", m.keys.Editor, Model.openEditor),
		wt("Pull", m.keys.Pull, runOnWorktree(func(m Model, path string) (tea.Model, tea.Cmd) {
			m.status = "pulling…"
			return m, gitPull(path)
		})),
		wt("Push", m.keys.Push, runOnWorktree(func(m Model, path string) (tea.Model, tea.Cmd) {
			m.status = "pushing…"
			return m, gitPush(path)
		})),
		wt("Fetch", m.keys.Fetch, runOnWorktree(func(m Model, path string) (tea.Model, tea.Cmd) {
			m.status = "fetching…"
			return m, gitFetch(path)
		})),
		wt("Commit changes", m.keys.Commit, Model.openCommitModal),
		wt("Rebase onto branch", m.keys.Rebase, Model.openRebaseModal),
		feature("Update from base", m.keys.Update, Model.openUpdateBaseModal),
		feature("Prune worktree", m.keys.Prune, Model.openPruneModal),
		feature("Create pull request", m.keys.CreatePR, Model.openCreatePR),
		feature("Copy file into worktree", m.keys.CopyFile, Model.openCopyModal),
		wt("Yank path / branch / PR URL", m.keys.Yank, Model.openYankModal),
	})

	cmds = append(cmds, tag("Tabs", []paletteCmd{
		wt("Show git log", m.keys.LogTab, Model.openLogTab),
		pr("Show PR detail", Model.openPRTab),
		feature("Show diff vs base", m.keys.DiffTab, Model.openDiffTab),
		wt("Show inspector", m.keys.InspectTab, Model.openInspectTab),
		paletteCmd{label: "Show CI checks", binding: m.keys.ChecksTab, scopeHint: wtScope, available: needBranch, run: Model.openChecksTab},
		wt("Show processes", m.keys.ViewProcs, Model.toggleProcsTab),
		wt("Show agent", m.keys.AgentTab, Model.openAgentTab),
	})...)

	cmds = append(cmds, tag("AI Agent", []paletteCmd{
		global("Agent: Start new task (auto worktree)", key.Binding{}, Model.openAgentAutoStartModal),
		wt("Agent: Start new task in selected worktree", key.Binding{}, Model.openAgentStartModal),
		wt("Agent: Stop active run", key.Binding{}, Model.agentStop),
		wt("Agent: Show status", m.keys.AgentTab, Model.openAgentTab),
		wt("Agent: Refresh view", key.Binding{}, Model.refreshAgentView),
	})...)

	cmds = append(cmds, tag("Tools", []paletteCmd{
		wt("Run package script", m.keys.Scripts, runOnWorktree(func(m Model, path string) (tea.Model, tea.Cmd) {
			return m, loadScripts(path)
		})),
		wt("Run alias", m.keys.Aliases, Model.openAliasModal),
		global("Filter worktree list", m.keys.Filter, func(m Model) (tea.Model, tea.Cmd) {
			m.focus = focusList
			m.term.Blur()
			m.list.Focus()
			return m, func() tea.Msg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}} }
		}),
		global("Cycle sort order", m.keys.Sort, func(m Model) (tea.Model, tea.Cmd) {
			m.sort = (m.sort + 1) % 6
			m.rebuildItems()
			return m, nil
		}),
		global("Preferences (theme, keys, defaults)", m.keys.Prefs, func(m Model) (tea.Model, tea.Cmd) {
			return m.openPrefs(false)
		}),
		global(mouseToggleLabel(m), key.Binding{}, Model.toggleMouse),
		global("Keybindings reference", m.keys.Help, Model.openKeymap),
		global("Status glyph legend", key.Binding{}, Model.openLegend),
		global("Quit bonsai", m.keys.Quit, func(m Model) (tea.Model, tea.Cmd) {
			return m.quit()
		}),
	})...)

	// Config editors (.bonsai.yaml): one entry per setting.
	for _, s := range configSettings {
		s := s
		cmds = append(cmds, paletteCmd{
			label:   s.label,
			section: "Config",
			run:     func(m Model) (tea.Model, tea.Cmd) { return m.openConfigSetting(s) },
		})
	}

	cmds = append(cmds, tag("PR actions", []paletteCmd{

		// PR actions (normally tab-local).
		pr("PR: approve", func(m Model) (tea.Model, tea.Cmd) {
			modal := modals.NewInput(modals.KindReviewApprove, "Approve PR #"+strconv.Itoa(m.prNumber()), "optional comment")
			modal.SetSize(m.width, m.height)
			m.modal = &modal
			return m, nil
		}),
		pr("PR: request changes", func(m Model) (tea.Model, tea.Cmd) {
			modal := modals.NewInput(modals.KindReviewChanges, "Request changes on PR #"+strconv.Itoa(m.prNumber()), "what needs changing")
			modal.SetSize(m.width, m.height)
			m.modal = &modal
			return m, nil
		}),
		pr("PR: merge", func(m Model) (tea.Model, tea.Cmd) {
			modal := modals.NewSelect(modals.KindMergeStrategy, "Merge PR #"+strconv.Itoa(m.prNumber())+" via", []string{"merge", "squash", "rebase"})
			modal.SetSize(m.width, m.height)
			m.modal = &modal
			return m, nil
		}),
		pr("PR: close", func(m Model) (tea.Model, tea.Cmd) {
			n := m.prNumber()
			return m.confirm("Close PR #"+strconv.Itoa(n)+"?", "", prClose(m.repoDir, n))
		}),
		pr("PR: reopen", func(m Model) (tea.Model, tea.Cmd) {
			return m, prReopen(m.repoDir, m.prNumber())
		}),
		pr("PR: mark ready for review", func(m Model) (tea.Model, tea.Cmd) {
			return m, prReady(m.repoDir, m.prNumber())
		}),
	})...)

	// Process control (normally tab-local).
	cmds = append(cmds, tag("Processes", []paletteCmd{
		{label: "Process: kill selected", binding: m.keys.Kill, scopeHint: wtScope, available: needProcess, run: func(m Model) (tea.Model, tea.Cmd) {
			wt, _ := m.selectedWorktree()
			if id, ok := m.selectedProcID(wt.Path); ok {
				m.procs.Kill(id)
				m.refreshProcPane()
			}
			return m, nil
		}},
		{label: "Process: restart selected", binding: m.keys.Restart, scopeHint: wtScope, available: needProcess, run: func(m Model) (tea.Model, tea.Cmd) {
			wt, _ := m.selectedWorktree()
			if id, ok := m.selectedProcID(wt.Path); ok {
				if p, err := m.procs.Restart(id); err == nil {
					m.activeProc[wt.Path] = p.ID
				} else {
					m.status = "restart: " + err.Error()
				}
				m.refreshProcPane()
			}
			return m, nil
		}},
		{label: "Process: restart policy", binding: m.keys.SetPolicy, scopeHint: wtScope, available: needProcess, run: func(m Model) (tea.Model, tea.Cmd) {
			wt, _ := m.selectedWorktree()
			if id, ok := m.selectedProcID(wt.Path); ok {
				return m.openPolicyModal(id)
			}
			return m, nil
		}},
		{label: "Process: view multiple together", binding: m.keys.MultiView, scopeHint: wtScope, available: needProcess, run: func(m Model) (tea.Model, tea.Cmd) {
			return m.openProcMultiViewModal()
		}},
		{label: "Process: tag selected", binding: m.keys.RenameProc, scopeHint: wtScope, available: needProcess, run: func(m Model) (tea.Model, tea.Cmd) {
			wt, _ := m.selectedWorktree()
			if id, ok := m.selectedProcID(wt.Path); ok {
				return m.openRenameProcModal(id)
			}
			return m, nil
		}},
		{label: "Processes: view all running", binding: m.keys.ProcModal, run: func(m Model) (tea.Model, tea.Cmd) {
			return m.openProcModal()
		}},
	})...)

	// Aliases: one entry each, run in the selected worktree with hook-variable
	// expansion (state aliases shadow config aliases of the same name).
	seen := map[string]bool{}
	for _, a := range append(m.state.SortedAliases(), m.cfg.Aliases...) {
		if seen[a.Name] {
			continue
		}
		seen[a.Name] = true
		name := a.Name
		cmds = append(cmds, paletteCmd{
			label:     "alias: " + name,
			section:   "Aliases",
			scopeHint: wtScope,
			available: needWorktree,
			run: func(m Model) (tea.Model, tea.Cmd) {
				wt, ok := m.selectedWorktree()
				if !ok {
					return m, nil
				}
				cmd := m.aliasCommand(name)
				if cmd == "" {
					return m, nil
				}
				prNum, _ := m.prForBranch(wt.Branch)
				vars := config.HookVars(m.repoDir, wt.Path, wt.Branch, m.upstreamFor(wt.Branch), prNum)
				return m.spawn(wt.Path, name, coreexec.ExpandVars(cmd, vars)), nil
			},
		})
	}
	return cmds
}
