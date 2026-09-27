package ui

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
)

const dynamicLayoutVersion = 2
const maxLayoutDepth = 12

type paneSpec struct {
	ID    paneID
	Views []viewID
}

type layoutTreeNode struct {
	Axis   layoutAxis
	Ratio  int
	First  *layoutTreeNode
	Second *layoutTreeNode
	Pane   *paneSpec
}

type dynamicLayout struct {
	Root *layoutTreeNode
}

type resolvedPane struct {
	ID    paneID
	Views []viewID
	Rect  paneRect
}

type resolvedDynamicLayout struct {
	Body  paneRect
	Panes []resolvedPane
}

func defaultDynamicLayout() dynamicLayout {
	return dynamicLayout{
		Root: &layoutTreeNode{
			Axis:  axisHorizontal,
			Ratio: 35,
			First: &layoutTreeNode{Pane: &paneSpec{
				ID:    paneID("pane-worktrees"),
				Views: []viewID{viewWorktrees},
			}},
			Second: &layoutTreeNode{Pane: &paneSpec{
				ID: paneID("pane-workspace"),
				Views: []viewID{
					viewLog,
					viewProcesses,
					viewInspect,
					viewDiff,
					viewChecks,
					viewPR,
				},
			}},
		},
	}
}

func cloneLayoutNode(n *layoutTreeNode) *layoutTreeNode {
	if n == nil {
		return nil
	}
	out := &layoutTreeNode{
		Axis:   n.Axis,
		Ratio:  n.Ratio,
		First:  cloneLayoutNode(n.First),
		Second: cloneLayoutNode(n.Second),
	}
	if n.Pane != nil {
		out.Pane = &paneSpec{
			ID:    n.Pane.ID,
			Views: append([]viewID(nil), n.Pane.Views...),
		}
	}
	return out
}

func normalizeDynamicLayout(p config.TUILayoutPrefs) dynamicLayout {
	if p.Version == dynamicLayoutVersion {
		root, ok := decodePersistedLayoutNode(p.Root, 0)
		if ok {
			layout := dynamicLayout{Root: root}
			if layout.valid() {
				return layout
			}
		}
		return defaultDynamicLayout()
	}

	// Missing and version-1 state use the existing v1 normalizer, then migrate
	// the two conceptual panes into v2 leaf panes.
	legacy := normalizeLayoutPrefs(p)
	return migrateLegacyLayout(legacy)
}

func migrateLegacyLayout(old layoutSpec) dynamicLayout {
	if !old.valid() {
		old = defaultLayoutSpec()
	}
	first := paneID("pane-worktrees")
	second := paneID("pane-workspace")
	firstViews := []viewID{viewWorktrees}
	secondViews := []viewID{viewLog, viewProcesses, viewInspect, viewDiff, viewChecks, viewPR}
	if old.Order[0] == paneWorkspace {
		first, second = second, first
		firstViews, secondViews = secondViews, firstViews
	}

	ratio := old.WorktreesPercent
	if old.Order[0] == paneWorkspace {
		ratio = 100 - old.WorktreesPercent
	}
	return dynamicLayout{
		Root: &layoutTreeNode{
			Axis:   old.Axis,
			Ratio:  ratio,
			First:  &layoutTreeNode{Pane: &paneSpec{ID: first, Views: firstViews}},
			Second: &layoutTreeNode{Pane: &paneSpec{ID: second, Views: secondViews}},
		},
	}
}

func decodePersistedLayoutNode(p *config.TUILayoutNodePrefs, depth int) (*layoutTreeNode, bool) {
	if p == nil || depth > maxLayoutDepth {
		return nil, false
	}
	switch p.Type {
	case "pane":
		if p.Pane == nil || p.Pane.ID == "" || len(p.Pane.Views) == 0 {
			return nil, false
		}
		views := make([]viewID, 0, len(p.Pane.Views))
		for _, raw := range p.Pane.Views {
			id := viewID(raw)
			if !validViewID(id) {
				return nil, false
			}
			views = append(views, id)
		}
		return &layoutTreeNode{Pane: &paneSpec{ID: paneID(p.Pane.ID), Views: views}}, true

	case "split":
		var axis layoutAxis
		switch p.Axis {
		case "horizontal":
			axis = axisHorizontal
		case "vertical":
			axis = axisVertical
		default:
			return nil, false
		}
		if p.Ratio < minPanePercent || p.Ratio > maxPanePercent {
			return nil, false
		}
		first, ok := decodePersistedLayoutNode(p.First, depth+1)
		if !ok {
			return nil, false
		}
		second, ok := decodePersistedLayoutNode(p.Second, depth+1)
		if !ok {
			return nil, false
		}
		return &layoutTreeNode{
			Axis:   axis,
			Ratio:  p.Ratio,
			First:  first,
			Second: second,
		}, true
	default:
		return nil, false
	}
}

func (l dynamicLayout) valid() bool {
	if l.Root == nil {
		return false
	}
	panes := map[paneID]bool{}
	views := map[viewID]bool{}
	if !validateLayoutNode(l.Root, 0, panes, views) {
		return false
	}
	if len(views) != len(registeredViews) {
		return false
	}
	for _, id := range registeredViews {
		if !views[id] {
			return false
		}
	}
	return true
}

func validateLayoutNode(n *layoutTreeNode, depth int, panes map[paneID]bool, views map[viewID]bool) bool {
	if n == nil || depth > maxLayoutDepth {
		return false
	}
	if n.Pane != nil {
		if n.First != nil || n.Second != nil || n.Pane.ID == "" || panes[n.Pane.ID] || len(n.Pane.Views) == 0 {
			return false
		}
		panes[n.Pane.ID] = true
		for _, id := range n.Pane.Views {
			if !validViewID(id) || views[id] {
				return false
			}
			views[id] = true
		}
		return true
	}
	if n.First == nil || n.Second == nil {
		return false
	}
	if n.Axis != axisHorizontal && n.Axis != axisVertical {
		return false
	}
	if n.Ratio < minPanePercent || n.Ratio > maxPanePercent {
		return false
	}
	return validateLayoutNode(n.First, depth+1, panes, views) &&
		validateLayoutNode(n.Second, depth+1, panes, views)
}

func (l dynamicLayout) persisted() config.TUILayoutPrefs {
	if !l.valid() {
		l = defaultDynamicLayout()
	}
	return config.TUILayoutPrefs{
		Version: dynamicLayoutVersion,
		Root:    persistLayoutNode(l.Root),
	}
}

func persistLayoutNode(n *layoutTreeNode) *config.TUILayoutNodePrefs {
	if n == nil {
		return nil
	}
	if n.Pane != nil {
		views := make([]string, len(n.Pane.Views))
		for i, id := range n.Pane.Views {
			views[i] = string(id)
		}
		return &config.TUILayoutNodePrefs{
			Type: "pane",
			Pane: &config.TUILayoutPanePrefs{
				ID:    string(n.Pane.ID),
				Views: views,
			},
		}
	}
	axis := "horizontal"
	if n.Axis == axisVertical {
		axis = "vertical"
	}
	return &config.TUILayoutNodePrefs{
		Type:   "split",
		Axis:   axis,
		Ratio:  n.Ratio,
		First:  persistLayoutNode(n.First),
		Second: persistLayoutNode(n.Second),
	}
}

func (l dynamicLayout) panes() []paneSpec {
	if !l.valid() {
		l = defaultDynamicLayout()
	}
	out := []paneSpec{}
	collectPanes(l.Root, &out)
	return out
}

func collectPanes(n *layoutTreeNode, out *[]paneSpec) {
	if n == nil {
		return
	}
	if n.Pane != nil {
		*out = append(*out, paneSpec{
			ID:    n.Pane.ID,
			Views: append([]viewID(nil), n.Pane.Views...),
		})
		return
	}
	collectPanes(n.First, out)
	collectPanes(n.Second, out)
}

func (l dynamicLayout) paneContaining(view viewID) (paneSpec, bool) {
	for _, p := range l.panes() {
		for _, id := range p.Views {
			if id == view {
				return p, true
			}
		}
	}
	return paneSpec{}, false
}

func resolveDynamicLayout(width, height, statusBarHeight int, layout dynamicLayout) resolvedDynamicLayout {
	if !layout.valid() {
		layout = defaultDynamicLayout()
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
	out := resolvedDynamicLayout{Body: body}
	resolveDynamicNode(layout.Root, body, &out.Panes)
	return out
}

func resolveDynamicNode(n *layoutTreeNode, rect paneRect, out *[]resolvedPane) {
	if n == nil {
		return
	}
	if n.Pane != nil {
		*out = append(*out, resolvedPane{
			ID:    n.Pane.ID,
			Views: append([]viewID(nil), n.Pane.Views...),
			Rect:  rect,
		})
		return
	}

	if n.Axis == axisVertical {
		minFirst := minHeightForNode(n.First)
		minSecond := minHeightForNode(n.Second)
		firstH := splitByRatio(rect.H, n.Ratio, minFirst, minSecond)
		first := paneRect{X: rect.X, Y: rect.Y, W: rect.W, H: firstH}
		second := paneRect{X: rect.X, Y: rect.Y + firstH, W: rect.W, H: rect.H - firstH}
		resolveDynamicNode(n.First, first, out)
		resolveDynamicNode(n.Second, second, out)
		return
	}

	minFirst := minWidthForNode(n.First)
	minSecond := minWidthForNode(n.Second)
	firstW := splitByRatio(rect.W, n.Ratio, minFirst, minSecond)
	first := paneRect{X: rect.X, Y: rect.Y, W: firstW, H: rect.H}
	second := paneRect{X: rect.X + firstW, Y: rect.Y, W: rect.W - firstW, H: rect.H}
	resolveDynamicNode(n.First, first, out)
	resolveDynamicNode(n.Second, second, out)
}

func splitByRatio(total, ratio, minFirst, minSecond int) int {
	if total <= 1 {
		return total
	}
	first := total * ratio / 100
	if first < 1 {
		first = 1
	}
	if first > total-1 {
		first = total - 1
	}
	if total >= minFirst+minSecond {
		if first < minFirst {
			first = minFirst
		}
		if first > total-minSecond {
			first = total - minSecond
		}
	}
	return first
}

func minWidthForNode(n *layoutTreeNode) int {
	if n == nil {
		return 1
	}
	if n.Pane != nil {
		min := minWorkspaceOuterWidth
		for _, id := range n.Pane.Views {
			if id == viewWorktrees && minWorktreesOuterWidth > min {
				min = minWorktreesOuterWidth
			}
		}
		return min
	}
	if n.Axis == axisHorizontal {
		return minWidthForNode(n.First) + minWidthForNode(n.Second)
	}
	a := minWidthForNode(n.First)
	b := minWidthForNode(n.Second)
	if b > a {
		return b
	}
	return a
}

func minHeightForNode(n *layoutTreeNode) int {
	if n == nil {
		return 1
	}
	if n.Pane != nil {
		return minWorkspaceOuterHeight
	}
	if n.Axis == axisVertical {
		return minHeightForNode(n.First) + minHeightForNode(n.Second)
	}
	a := minHeightForNode(n.First)
	b := minHeightForNode(n.Second)
	if b > a {
		return b
	}
	return a
}

func (r resolvedDynamicLayout) pane(id paneID) (resolvedPane, bool) {
	for _, p := range r.Panes {
		if p.ID == id {
			return p, true
		}
	}
	return resolvedPane{}, false
}

func (r resolvedDynamicLayout) paneAt(x, y int) (resolvedPane, bool) {
	for _, p := range r.Panes {
		if p.Rect.contains(x, y) {
			return p, true
		}
	}
	return resolvedPane{}, false
}

func renderDynamicNode(n *layoutTreeNode, rendered map[paneID]string) string {
	if n == nil {
		return ""
	}
	if n.Pane != nil {
		return rendered[n.Pane.ID]
	}
	if n.Axis == axisVertical {
		return lipgloss.JoinVertical(lipgloss.Left, renderDynamicNode(n.First, rendered), renderDynamicNode(n.Second, rendered))
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, renderDynamicNode(n.First, rendered), renderDynamicNode(n.Second, rendered))
}
