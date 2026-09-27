package ui

// viewID identifies one first-class TUI surface that can be assigned to a pane.
type viewID string

const (
	viewWorktrees viewID = "worktrees"
	viewLog       viewID = "log"
	viewProcesses viewID = "processes"
	viewInspect   viewID = "inspect"
	viewDiff      viewID = "diff"
	viewChecks    viewID = "checks"
	viewPR        viewID = "pr"
)

var registeredViews = []viewID{
	viewWorktrees,
	viewLog,
	viewProcesses,
	viewInspect,
	viewDiff,
	viewChecks,
	viewPR,
}

func validViewID(id viewID) bool {
	for _, v := range registeredViews {
		if v == id {
			return true
		}
	}
	return false
}

func viewTitle(id viewID) string {
	switch id {
	case viewWorktrees:
		return "Worktrees"
	case viewLog:
		return "Git Log"
	case viewProcesses:
		return "Processes"
	case viewInspect:
		return "Inspect"
	case viewDiff:
		return "Diff"
	case viewChecks:
		return "Checks"
	case viewPR:
		return "PR"
	default:
		return string(id)
	}
}

func viewForTab(t rightTab) viewID {
	switch t {
	case tabProcs:
		return viewProcesses
	case tabDiff:
		return viewDiff
	case tabPR:
		return viewPR
	case tabInspect:
		return viewInspect
	case tabChecks:
		return viewChecks
	default:
		return viewLog
	}
}

func tabForView(id viewID) (rightTab, bool) {
	switch id {
	case viewLog:
		return tabLog, true
	case viewProcesses:
		return tabProcs, true
	case viewDiff:
		return tabDiff, true
	case viewPR:
		return tabPR, true
	case viewInspect:
		return tabInspect, true
	case viewChecks:
		return tabChecks, true
	default:
		return tabLog, false
	}
}

func terminalBackedView(id viewID) bool {
	return id != viewWorktrees && validViewID(id)
}
