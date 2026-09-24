package ui

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/modals"
)

// procRowLabel renders a one-line description of a process for the modals.
func procRowLabel(r *procstore.Record, url string) string {
	line := fmt.Sprintf("#%d %s (%s)", r.ID, labelOf(r), r.Status)
	if r.Policy.Mode != "" && r.Policy.Mode != procstore.PolicyNo {
		line += " [" + r.Policy.Mode + "]"
	}
	if url != "" {
		line += "  " + url
	}
	return line
}

// labelOf returns a process's display label, falling back to its command.
func labelOf(r *procstore.Record) string {
	if r.Label != "" {
		return r.Label
	}
	return r.Command
}

// openProcModal shows every process across all worktrees in a sectioned fuzzy
// modal; selecting one jumps to it (its worktree + Processes tab).
func (m Model) openProcModal() (tea.Model, tea.Cmd) {
	m.procs.refresh()
	m.procModalRecs = map[string]*procstore.Record{}

	build := func(query string) []modals.Section {
		var sections []modals.Section
		for _, t := range m.worktrees {
			recs := m.procs.List(t.Path)
			if len(recs) == 0 {
				continue
			}
			var items []string
			q := strings.ToLower(strings.TrimSpace(query))
			for _, r := range recs {
				label := procRowLabel(r, m.procs.LastURL(r.ID))
				if q != "" && !strings.Contains(strings.ToLower(label), q) {
					continue
				}
				m.procModalRecs[label] = r
				items = append(items, label)
			}
			if len(items) > 0 {
				name := t.Branch
				if name == "" {
					name = filepath.Base(t.Path)
				}
				sections = append(sections, modals.Section{Name: name, Items: items})
			}
		}
		return sections
	}

	initial := build("")
	if len(initial) == 0 {
		m.status = "no processes"
		return m, nil
	}
	modal := modals.NewSectionedFuzzy(modals.KindProcesses, "Processes", build, initial)
	modal.SetSize(m.width, m.height)
	m.modal = &modal
	return m, nil
}

// onProcModalSubmit jumps to the process whose row was chosen.
func (m Model) onProcModalSubmit(label string) (tea.Model, tea.Cmd) {
	m.modal = nil
	rec, ok := m.procModalRecs[label]
	if !ok {
		return m, nil
	}
	// Select the owning worktree, switch to the Processes tab, focus the process.
	m.list.SelectByPath(rec.Worktree)
	m.rightTab = tabProcs
	m.focus = focusTerminal
	m.activeProc[rec.Worktree] = rec.ID
	m.refreshProcPane()
	return m, nil
}

// openPolicyModal shows a picker for a process's restart policy (no /
// on-failure / always), applied to the running daemon immediately on submit.
// Persist a permanent default by editing the `processes:` section of
// .bonsai.yaml instead.
func (m Model) openPolicyModal(id int) (tea.Model, tea.Cmd) {
	rec, ok := m.procs.GetByID(m.currentPath(), id)
	if !ok {
		return m, nil
	}
	m.policyModalID = id
	cur := rec.Policy.Mode
	if cur == "" {
		cur = procstore.PolicyNo
	}
	items := []string{procstore.PolicyNo, procstore.PolicyOnFailure, procstore.PolicyAlways}
	modal := modals.NewSelect(modals.KindSetPolicy, fmt.Sprintf("#%d restart policy (current: %s)", id, cur), items)
	modal.SetSize(m.width, m.height)
	m.modal = &modal
	return m, nil
}

// onPolicyModalSubmit applies the chosen restart policy.
func (m Model) onPolicyModalSubmit(choice string) (tea.Model, tea.Cmd) {
	id := m.policyModalID
	pol := procstore.Policy{Mode: choice, MaxRestarts: procstore.DefaultPolicy().MaxRestarts}
	if _, err := m.procs.SetPolicy(id, pol); err != nil {
		m.status = "policy: " + err.Error()
		return m, nil
	}
	m.status = fmt.Sprintf("#%d restart policy: %s", id, choice)
	m.refreshProcPane()
	return m, nil
}

// procTagMaxLen bounds a process's multi-view display tag.
const procTagMaxLen = 8

// procTag returns a process's short display tag for the multi-view merged
// log: a user override if set (see openRenameProcModal), else its label /
// command truncated to procTagMaxLen runes.
func (m Model) procTag(id int) string {
	if v, ok := m.procLabelOverride[id]; ok {
		return v
	}
	rec, ok := m.procs.GetByID(m.currentPath(), id)
	if !ok {
		return fmt.Sprintf("#%d", id)
	}
	s := labelOf(rec)
	if r := []rune(s); len(r) > procTagMaxLen {
		s = string(r[:procTagMaxLen])
	}
	return s
}

// openRenameProcModal lets the user set a custom short tag for a process,
// used to identify its lines in the multi-view merged log.
func (m Model) openRenameProcModal(id int) (tea.Model, tea.Cmd) {
	m.renameProcID = id
	modal := modals.NewInput(modals.KindRenameProc, fmt.Sprintf("#%d tag", id), "e.g. api, web, worker")
	modal.SetInitial(m.procTag(id))
	modal.SetSize(m.width, m.height)
	m.modal = &modal
	return m, nil
}

// onRenameProcSubmit applies (or, if blank, clears) a process's custom tag.
func (m Model) onRenameProcSubmit(value string) (tea.Model, tea.Cmd) {
	value = strings.TrimSpace(value)
	if value == "" {
		delete(m.procLabelOverride, m.renameProcID)
	} else {
		if r := []rune(value); len(r) > procTagMaxLen {
			value = string(r[:procTagMaxLen])
		}
		m.procLabelOverride[m.renameProcID] = value
	}
	m.refreshProcPane()
	return m, nil
}

// openProcMultiViewModal lets the user pick multiple processes, within the
// current worktree, to show together in one merged, color-tagged log.
func (m Model) openProcMultiViewModal() (tea.Model, tea.Cmd) {
	wt, ok := m.selectedWorktree()
	if !ok {
		return m, nil
	}
	recs := m.procs.List(wt.Path)
	if len(recs) == 0 {
		m.status = "no processes"
		return m, nil
	}
	selected := map[int]bool{}
	for _, id := range m.procMultiSel[wt.Path] {
		selected[id] = true
	}
	m.procMultiPickIDs = make([]int, len(recs))
	items := make([]string, len(recs))
	pre := make([]bool, len(recs))
	for i, r := range recs {
		m.procMultiPickIDs[i] = r.ID
		items[i] = procRowLabel(r, m.procs.LastURL(r.ID))
		pre[i] = selected[r.ID]
	}
	modal := modals.NewMultiSelect(modals.KindMultiView, "View which processes together?", items, pre)
	modal.SetSize(m.width, m.height)
	m.modal = &modal
	return m, nil
}

// onMultiViewSubmit records which processes to show together and refreshes
// the pane. Fewer than two selections falls back to the normal single-process
// view (via activeProc).
func (m Model) onMultiViewSubmit(selected []int) (tea.Model, tea.Cmd) {
	m.modal = nil
	wt, ok := m.selectedWorktree()
	if !ok {
		return m, nil
	}
	var ids []int
	for _, i := range selected {
		if i >= 0 && i < len(m.procMultiPickIDs) {
			ids = append(ids, m.procMultiPickIDs[i])
		}
	}
	m.procMultiSel[wt.Path] = ids
	if len(ids) > 0 {
		m.activeProc[wt.Path] = ids[len(ids)-1]
	}
	m.refreshProcPane()
	return m, nil
}

// currentPath returns the selected worktree path (empty if none).
func (m Model) currentPath() string {
	if wt, ok := m.selectedWorktree(); ok {
		return wt.Path
	}
	return ""
}

// quit shows the keep/kill modal when processes are running, else quits. The
// daemon owns the processes, so quitting the TUI keeps them alive by default;
// the modal only stops the ones the user unchecks.
func (m Model) quit() (tea.Model, tea.Cmd) {
	m.procs.refresh()
	var running []*procstore.Record
	for _, r := range m.procs.All() {
		if procstore.IsActive(r.Status) {
			running = append(running, r)
		}
	}
	if len(running) == 0 {
		return m, tea.Quit
	}

	m.quitRecs = running
	items := make([]string, len(running))
	pre := make([]bool, len(running))
	for i, r := range running {
		wt := filepath.Base(r.Worktree)
		items[i] = fmt.Sprintf("%s · %s", wt, procRowLabel(r, m.procs.LastURL(r.ID)))
		pre[i] = true // default: keep everything
	}
	modal := modals.NewMultiSelect(modals.KindQuit, "Keep which processes running?", items, pre)
	modal.SetSize(m.width, m.height)
	m.modal = &modal
	return m, nil
}

// onQuitSubmit kills the unchecked processes, then quits. selected holds the
// indices to KEEP running.
func (m Model) onQuitSubmit(selected []int) (tea.Model, tea.Cmd) {
	keep := map[int]bool{}
	for _, i := range selected {
		keep[i] = true
	}
	for i, r := range m.quitRecs {
		if !keep[i] {
			m.procs.Kill(r.ID)
		}
	}
	return m, tea.Quit
}
