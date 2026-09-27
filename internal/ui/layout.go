package ui

import "github.com/Tiago-0liveira/bonsai/internal/core/config"

type paneID string

const (
	paneWorktrees paneID = "worktrees"
	paneWorkspace paneID = "workspace"
)

type layoutAxis int

const (
	axisHorizontal layoutAxis = iota
	axisVertical
)

const (
	layoutVersion           = 1
	minPanePercent          = 20
	maxPanePercent          = 80
	minWorktreesOuterWidth  = 16
	minWorkspaceOuterWidth  = 12
	minWorktreesOuterHeight = 3
	minWorkspaceOuterHeight = 5
)

type layoutSpec struct {
	Axis             layoutAxis
	Order            [2]paneID
	WorktreesPercent int
}

type paneRect struct {
	X int
	Y int
	W int
	H int
}

func (r paneRect) contains(x, y int) bool {
	return r.W > 0 && r.H > 0 &&
		x >= r.X && x < r.X+r.W &&
		y >= r.Y && y < r.Y+r.H
}

type resolvedLayout struct {
	Axis      layoutAxis
	Order     [2]paneID
	Worktrees paneRect
	Workspace paneRect
	Body      paneRect
}

func defaultLayoutSpec() layoutSpec {
	return layoutSpec{
		Axis:             axisHorizontal,
		Order:            [2]paneID{paneWorktrees, paneWorkspace},
		WorktreesPercent: 35,
	}
}

func (s layoutSpec) valid() bool {
	if s.Axis != axisHorizontal && s.Axis != axisVertical {
		return false
	}
	if s.WorktreesPercent < minPanePercent || s.WorktreesPercent > maxPanePercent {
		return false
	}
	if s.Order[0] == s.Order[1] {
		return false
	}
	seenWorktrees := false
	seenWorkspace := false
	for _, id := range s.Order {
		switch id {
		case paneWorktrees:
			seenWorktrees = true
		case paneWorkspace:
			seenWorkspace = true
		default:
			return false
		}
	}
	return seenWorktrees && seenWorkspace
}

func normalizeLayoutPrefs(p config.TUILayoutPrefs) layoutSpec {
	def := defaultLayoutSpec()

	// A completely absent layout is the backward-compatible legacy default.
	if p.Version == 0 && p.Axis == "" && len(p.Order) == 0 && len(p.Sizes) == 0 {
		return def
	}
	if p.Version != layoutVersion {
		return def
	}

	var axis layoutAxis
	switch p.Axis {
	case "horizontal":
		axis = axisHorizontal
	case "vertical":
		axis = axisVertical
	default:
		return def
	}

	if len(p.Order) != 2 {
		return def
	}
	var order [2]paneID
	for i, raw := range p.Order {
		switch paneID(raw) {
		case paneWorktrees, paneWorkspace:
			order[i] = paneID(raw)
		default:
			return def
		}
	}
	if order[0] == order[1] {
		return def
	}

	if p.Sizes == nil || len(p.Sizes) != 2 {
		return def
	}
	worktrees, okWorktrees := p.Sizes[string(paneWorktrees)]
	workspace, okWorkspace := p.Sizes[string(paneWorkspace)]
	if !okWorktrees || !okWorkspace {
		return def
	}
	if worktrees <= 0 || workspace <= 0 || worktrees+workspace != 100 {
		return def
	}
	if worktrees < minPanePercent || worktrees > maxPanePercent ||
		workspace < minPanePercent || workspace > maxPanePercent {
		return def
	}

	spec := layoutSpec{
		Axis:             axis,
		Order:            order,
		WorktreesPercent: worktrees,
	}
	if !spec.valid() {
		return def
	}
	return spec
}

func (s layoutSpec) persisted() config.TUILayoutPrefs {
	if !s.valid() {
		s = defaultLayoutSpec()
	}
	return config.TUILayoutPrefs{
		Version: layoutVersion,
		Axis:    s.axisName(),
		Order:   s.orderNames(),
		Sizes: map[string]int{
			string(paneWorktrees): s.WorktreesPercent,
			string(paneWorkspace): 100 - s.WorktreesPercent,
		},
	}
}

func (s layoutSpec) axisName() string {
	if s.Axis == axisVertical {
		return "vertical"
	}
	return "horizontal"
}

func (s layoutSpec) orderNames() []string {
	if !s.valid() {
		s = defaultLayoutSpec()
	}
	return []string{string(s.Order[0]), string(s.Order[1])}
}

func splitCells(total, worktreesPercent, minWorktrees, minWorkspace int) (worktrees, workspace int) {
	if total <= 0 {
		return 0, 0
	}
	if total == 1 {
		return 1, 0
	}

	worktrees = total * worktreesPercent / 100
	if worktrees < 1 {
		worktrees = 1
	}
	if worktrees > total-1 {
		worktrees = total - 1
	}

	if total >= minWorktrees+minWorkspace {
		if worktrees < minWorktrees {
			worktrees = minWorktrees
		}
		if worktrees > total-minWorkspace {
			worktrees = total - minWorkspace
		}
	}

	workspace = total - worktrees
	return worktrees, workspace
}

func resolvePaneLayout(width, height, statusBarHeight int, spec layoutSpec) resolvedLayout {
	if !spec.valid() {
		spec = defaultLayoutSpec()
	}
	if width < 0 {
		width = 0
	}
	if height < 0 {
		height = 0
	}
	if statusBarHeight < 0 {
		statusBarHeight = 0
	}
	if statusBarHeight > height {
		statusBarHeight = height
	}

	body := paneRect{W: width, H: height - statusBarHeight}
	out := resolvedLayout{
		Axis:  spec.Axis,
		Order: spec.Order,
		Body:  body,
	}

	if spec.Axis == axisVertical {
		worktreesH, workspaceH := splitCells(
			body.H,
			spec.WorktreesPercent,
			minWorktreesOuterHeight,
			minWorkspaceOuterHeight,
		)
		worktrees := paneRect{W: body.W, H: worktreesH}
		workspace := paneRect{W: body.W, H: workspaceH}
		if spec.Order[0] == paneWorktrees {
			worktrees.Y = 0
			workspace.Y = worktreesH
		} else {
			workspace.Y = 0
			worktrees.Y = workspaceH
		}
		out.Worktrees = worktrees
		out.Workspace = workspace
		return out
	}

	worktreesW, workspaceW := splitCells(
		body.W,
		spec.WorktreesPercent,
		minWorktreesOuterWidth,
		minWorkspaceOuterWidth,
	)
	worktrees := paneRect{W: worktreesW, H: body.H}
	workspace := paneRect{W: workspaceW, H: body.H}
	if spec.Order[0] == paneWorktrees {
		worktrees.X = 0
		workspace.X = worktreesW
	} else {
		workspace.X = 0
		worktrees.X = workspaceW
	}
	out.Worktrees = worktrees
	out.Workspace = workspace
	return out
}

func (l resolvedLayout) paneAt(x, y int) (paneID, bool) {
	if l.Worktrees.contains(x, y) {
		return paneWorktrees, true
	}
	if l.Workspace.contains(x, y) {
		return paneWorkspace, true
	}
	return "", false
}
