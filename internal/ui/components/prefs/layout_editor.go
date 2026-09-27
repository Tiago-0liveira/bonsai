package prefs

import (
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
)

const (
	layoutPrefsVersion = 2
	layoutMaxDepth     = 12
)

var layoutViewIDs = []string{
	"worktrees",
	"log",
	"processes",
	"inspect",
	"diff",
	"checks",
	"pr",
}

func defaultLayoutPrefs() LayoutPrefs {
	return config.TUILayoutPrefs{
		Version: layoutPrefsVersion,
		Root: &config.TUILayoutNodePrefs{
			Type:  "split",
			Axis:  "horizontal",
			Ratio: 35,
			First: &config.TUILayoutNodePrefs{
				Type: "pane",
				Pane: &config.TUILayoutPanePrefs{
					ID:    "pane-worktrees",
					Views: []string{"worktrees"},
				},
			},
			Second: &config.TUILayoutNodePrefs{
				Type: "pane",
				Pane: &config.TUILayoutPanePrefs{
					ID:    "pane-workspace",
					Views: []string{"log", "processes", "inspect", "diff", "checks", "pr"},
				},
			},
		},
	}
}

func normalizeLayoutPrefs(p LayoutPrefs) LayoutPrefs {
	if p.Version != layoutPrefsVersion || !validLayoutPrefs(p) {
		return defaultLayoutPrefs()
	}
	return cloneLayoutPrefs(p)
}

func cloneLayoutPrefs(p LayoutPrefs) LayoutPrefs {
	out := p
	out.Order = append([]string(nil), p.Order...)
	if p.Sizes != nil {
		out.Sizes = make(map[string]int, len(p.Sizes))
		for k, v := range p.Sizes {
			out.Sizes[k] = v
		}
	}
	out.Root = cloneLayoutNode(p.Root)
	return out
}

func cloneLayoutNode(n *config.TUILayoutNodePrefs) *config.TUILayoutNodePrefs {
	if n == nil {
		return nil
	}
	out := &config.TUILayoutNodePrefs{
		Type:  n.Type,
		Axis:  n.Axis,
		Ratio: n.Ratio,
		First: cloneLayoutNode(n.First),
		Second: cloneLayoutNode(n.Second),
	}
	if n.Pane != nil {
		out.Pane = &config.TUILayoutPanePrefs{
			ID:    n.Pane.ID,
			Views: append([]string(nil), n.Pane.Views...),
		}
	}
	return out
}

func validLayoutPrefs(p LayoutPrefs) bool {
	if p.Root == nil {
		return false
	}
	panes := map[string]bool{}
	views := map[string]bool{}
	if !validLayoutNode(p.Root, 0, panes, views) {
		return false
	}
	if len(views) != len(layoutViewIDs) {
		return false
	}
	for _, id := range layoutViewIDs {
		if !views[id] {
			return false
		}
	}
	return true
}

func validLayoutNode(n *config.TUILayoutNodePrefs, depth int, panes, views map[string]bool) bool {
	if n == nil || depth > layoutMaxDepth {
		return false
	}
	switch n.Type {
	case "pane":
		if n.Pane == nil || n.Pane.ID == "" || panes[n.Pane.ID] || len(n.Pane.Views) == 0 ||
			n.First != nil || n.Second != nil {
			return false
		}
		panes[n.Pane.ID] = true
		for _, view := range n.Pane.Views {
			if !knownLayoutView(view) || views[view] {
				return false
			}
			views[view] = true
		}
		return true
	case "split":
		if n.Pane != nil || n.First == nil || n.Second == nil ||
			(n.Axis != "horizontal" && n.Axis != "vertical") ||
			n.Ratio < 20 || n.Ratio > 80 {
			return false
		}
		return validLayoutNode(n.First, depth+1, panes, views) &&
			validLayoutNode(n.Second, depth+1, panes, views)
	default:
		return false
	}
}

func knownLayoutView(id string) bool {
	for _, v := range layoutViewIDs {
		if v == id {
			return true
		}
	}
	return false
}

func layoutSummary(p LayoutPrefs) string {
	p = normalizeLayoutPrefs(p)
	panes := 0
	var walk func(*config.TUILayoutNodePrefs)
	walk = func(n *config.TUILayoutNodePrefs) {
		if n == nil {
			return
		}
		if n.Type == "pane" {
			panes++
			return
		}
		walk(n.First)
		walk(n.Second)
	}
	walk(p.Root)
	return fmt.Sprintf("%d pane%s · %d views", panes, plural(panes), len(layoutViewIDs))
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

type layoutEditKind int

const (
	layoutEditSplit layoutEditKind = iota
	layoutEditPane
	layoutEditView
)

type layoutEditItem struct {
	kind      layoutEditKind
	path      []int
	viewIndex int
	depth     int
}

func layoutEditorItems(root *config.TUILayoutNodePrefs) []layoutEditItem {
	var out []layoutEditItem
	var walk func(*config.TUILayoutNodePrefs, []int, int)
	walk = func(n *config.TUILayoutNodePrefs, path []int, depth int) {
		if n == nil {
			return
		}
		if n.Type == "pane" {
			out = append(out, layoutEditItem{
				kind:  layoutEditPane,
				path:  append([]int(nil), path...),
				depth: depth,
			})
			for i := range n.Pane.Views {
				out = append(out, layoutEditItem{
					kind:      layoutEditView,
					path:      append([]int(nil), path...),
					viewIndex: i,
					depth:     depth + 1,
				})
			}
			return
		}
		out = append(out, layoutEditItem{
			kind:  layoutEditSplit,
			path:  append([]int(nil), path...),
			depth: depth,
		})
		walk(n.First, appendPath(path, 0), depth+1)
		walk(n.Second, appendPath(path, 1), depth+1)
	}
	walk(root, nil, 0)
	return out
}

func appendPath(path []int, part int) []int {
	out := append([]int(nil), path...)
	return append(out, part)
}

func nodeAtPath(root *config.TUILayoutNodePrefs, path []int) *config.TUILayoutNodePrefs {
	n := root
	for _, part := range path {
		if n == nil || n.Type != "split" {
			return nil
		}
		if part == 0 {
			n = n.First
		} else {
			n = n.Second
		}
	}
	return n
}

func (m Model) updateLayoutEditor(key tea.KeyMsg) (Model, tea.Cmd) {
	items := layoutEditorItems(m.layout.Root)
	if len(items) == 0 {
		m.layout = defaultLayoutPrefs()
		items = layoutEditorItems(m.layout.Root)
	}
	if m.layoutCursor >= len(items) {
		m.layoutCursor = len(items) - 1
	}
	if m.layoutCursor < 0 {
		m.layoutCursor = 0
	}

	switch key.String() {
	case "esc", "ctrl+c":
		m.layoutEditing = false
		m.msg = ""
		return m, nil
	case "up", "ctrl+k":
		m.layoutCursor = (m.layoutCursor - 1 + len(items)) % len(items)
		return m, nil
	case "down", "ctrl+j":
		m.layoutCursor = (m.layoutCursor + 1) % len(items)
		return m, nil
	case "r":
		m.layout = defaultLayoutPrefs()
		m.layoutCursor = 0
		m.msg = "layout reset to default"
		return m, m.save()
	}

	item := items[m.layoutCursor]
	node := nodeAtPath(m.layout.Root, item.path)
	if node == nil {
		return m, nil
	}

	switch key.String() {
	case "left":
		if item.kind == layoutEditSplit {
			node.Ratio -= 5
			if node.Ratio < 20 {
				node.Ratio = 20
			}
			m.msg = fmt.Sprintf("split ratio: %d/%d", node.Ratio, 100-node.Ratio)
			return m, m.save()
		}
	case "right":
		if item.kind == layoutEditSplit {
			node.Ratio += 5
			if node.Ratio > 80 {
				node.Ratio = 80
			}
			m.msg = fmt.Sprintf("split ratio: %d/%d", node.Ratio, 100-node.Ratio)
			return m, m.save()
		}
	case "o":
		if item.kind == layoutEditSplit {
			if node.Axis == "horizontal" {
				node.Axis = "vertical"
			} else {
				node.Axis = "horizontal"
			}
			m.msg = "split orientation: " + node.Axis
			return m, m.save()
		}
	case "s":
		if item.kind == layoutEditView && m.splitLayoutView(item, "horizontal") {
			m.msg = "split view horizontally"
			return m, m.save()
		}
	case "v":
		if item.kind == layoutEditView && m.splitLayoutView(item, "vertical") {
			m.msg = "split view vertically"
			return m, m.save()
		}
	case "m":
		if item.kind == layoutEditView && m.moveLayoutView(item) {
			m.msg = "moved view to next pane"
			return m, m.save()
		}
	case "[":
		if item.kind == layoutEditView && m.reorderLayoutView(item, -1) {
			m.msg = "view moved earlier"
			return m, m.save()
		}
	case "]":
		if item.kind == layoutEditView && m.reorderLayoutView(item, 1) {
			m.msg = "view moved later"
			return m, m.save()
		}
	case "g":
		if (item.kind == layoutEditPane || item.kind == layoutEditView) && m.mergeLayoutPane(item.path) {
			if m.layoutCursor > 0 {
				m.layoutCursor--
			}
			m.msg = "merged sibling panes"
			return m, m.save()
		}
	}
	return m, nil
}

func (m *Model) splitLayoutView(item layoutEditItem, axis string) bool {
	leaf := nodeAtPath(m.layout.Root, item.path)
	if leaf == nil || leaf.Type != "pane" || leaf.Pane == nil ||
		item.viewIndex < 0 || item.viewIndex >= len(leaf.Pane.Views) ||
		len(leaf.Pane.Views) <= 1 {
		return false
	}
	view := leaf.Pane.Views[item.viewIndex]
	remaining := append([]string(nil), leaf.Pane.Views[:item.viewIndex]...)
	remaining = append(remaining, leaf.Pane.Views[item.viewIndex+1:]...)
	oldPane := &config.TUILayoutNodePrefs{
		Type: "pane",
		Pane: &config.TUILayoutPanePrefs{
			ID:    leaf.Pane.ID,
			Views: remaining,
		},
	}
	newPane := &config.TUILayoutNodePrefs{
		Type: "pane",
		Pane: &config.TUILayoutPanePrefs{
			ID:    uniquePaneID(m.layout.Root, "pane-"+view),
			Views: []string{view},
		},
	}
	*leaf = config.TUILayoutNodePrefs{
		Type:   "split",
		Axis:   axis,
		Ratio:  50,
		First:  oldPane,
		Second: newPane,
	}
	return validLayoutPrefs(m.layout)
}

func uniquePaneID(root *config.TUILayoutNodePrefs, base string) string {
	used := map[string]bool{}
	var walk func(*config.TUILayoutNodePrefs)
	walk = func(n *config.TUILayoutNodePrefs) {
		if n == nil {
			return
		}
		if n.Type == "pane" && n.Pane != nil {
			used[n.Pane.ID] = true
			return
		}
		walk(n.First)
		walk(n.Second)
	}
	walk(root)
	if !used[base] {
		return base
	}
	for i := 2; ; i++ {
		id := base + "-" + strconv.Itoa(i)
		if !used[id] {
			return id
		}
	}
}

func (m *Model) reorderLayoutView(item layoutEditItem, delta int) bool {
	leaf := nodeAtPath(m.layout.Root, item.path)
	if leaf == nil || leaf.Type != "pane" || leaf.Pane == nil {
		return false
	}
	next := item.viewIndex + delta
	if item.viewIndex < 0 || item.viewIndex >= len(leaf.Pane.Views) ||
		next < 0 || next >= len(leaf.Pane.Views) {
		return false
	}
	leaf.Pane.Views[item.viewIndex], leaf.Pane.Views[next] =
		leaf.Pane.Views[next], leaf.Pane.Views[item.viewIndex]
	return true
}

func panePaths(root *config.TUILayoutNodePrefs) [][]int {
	items := layoutEditorItems(root)
	var out [][]int
	for _, item := range items {
		if item.kind == layoutEditPane {
			out = append(out, append([]int(nil), item.path...))
		}
	}
	return out
}

func samePath(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (m *Model) moveLayoutView(item layoutEditItem) bool {
	src := nodeAtPath(m.layout.Root, item.path)
	if src == nil || src.Type != "pane" || src.Pane == nil ||
		item.viewIndex < 0 || item.viewIndex >= len(src.Pane.Views) {
		return false
	}
	paths := panePaths(m.layout.Root)
	if len(paths) < 2 {
		return false
	}
	srcIdx := -1
	for i, path := range paths {
		if samePath(path, item.path) {
			srcIdx = i
			break
		}
	}
	if srcIdx < 0 {
		return false
	}
	targetPath := paths[(srcIdx+1)%len(paths)]
	target := nodeAtPath(m.layout.Root, targetPath)
	if target == nil || target.Pane == nil {
		return false
	}

	view := src.Pane.Views[item.viewIndex]
	src.Pane.Views = append(src.Pane.Views[:item.viewIndex], src.Pane.Views[item.viewIndex+1:]...)
	target.Pane.Views = append(target.Pane.Views, view)

	if len(src.Pane.Views) == 0 {
		if !collapseEmptyPane(m.layout.Root, item.path) {
			return false
		}
	}
	return validLayoutPrefs(m.layout)
}

func collapseEmptyPane(root *config.TUILayoutNodePrefs, path []int) bool {
	if len(path) == 0 {
		return false
	}
	parentPath := path[:len(path)-1]
	parent := nodeAtPath(root, parentPath)
	if parent == nil || parent.Type != "split" {
		return false
	}
	var sibling *config.TUILayoutNodePrefs
	if path[len(path)-1] == 0 {
		sibling = parent.Second
	} else {
		sibling = parent.First
	}
	if sibling == nil {
		return false
	}
	*parent = *cloneLayoutNode(sibling)
	return true
}

func (m *Model) mergeLayoutPane(path []int) bool {
	if len(path) == 0 {
		return false
	}
	leaf := nodeAtPath(m.layout.Root, path)
	if leaf == nil || leaf.Type != "pane" || leaf.Pane == nil {
		return false
	}
	parent := nodeAtPath(m.layout.Root, path[:len(path)-1])
	if parent == nil || parent.Type != "split" {
		return false
	}
	selectedFirst := path[len(path)-1] == 0
	var sibling *config.TUILayoutNodePrefs
	if selectedFirst {
		sibling = parent.Second
	} else {
		sibling = parent.First
	}
	if sibling == nil || sibling.Type != "pane" || sibling.Pane == nil {
		return false
	}

	views := make([]string, 0, len(leaf.Pane.Views)+len(sibling.Pane.Views))
	id := leaf.Pane.ID
	if selectedFirst {
		views = append(views, leaf.Pane.Views...)
		views = append(views, sibling.Pane.Views...)
	} else {
		id = sibling.Pane.ID
		views = append(views, sibling.Pane.Views...)
		views = append(views, leaf.Pane.Views...)
	}
	*parent = config.TUILayoutNodePrefs{
		Type: "pane",
		Pane: &config.TUILayoutPanePrefs{
			ID:    id,
			Views: views,
		},
	}
	return validLayoutPrefs(m.layout)
}

func (m Model) layoutEditorView() string {
	items := layoutEditorItems(m.layout.Root)
	if len(items) == 0 {
		return ""
	}
	cursor := m.layoutCursor
	if cursor < 0 {
		cursor = 0
	}
	if cursor >= len(items) {
		cursor = len(items) - 1
	}

	var b strings.Builder
	b.WriteString(titleStyle.Render("Layout editor") + "\n\n")
	for i, item := range items {
		node := nodeAtPath(m.layout.Root, item.path)
		if node == nil {
			continue
		}
		prefix := "  "
		if i == cursor {
			prefix = cursorStyle.Render("› ")
		}
		indent := strings.Repeat("  ", item.depth)
		line := ""
		switch item.kind {
		case layoutEditSplit:
			arrow := "↔"
			if node.Axis == "vertical" {
				arrow = "↕"
			}
			line = fmt.Sprintf("%s split %s %d/%d", arrow, node.Axis, node.Ratio, 100-node.Ratio)
		case layoutEditPane:
			line = "▪ " + node.Pane.ID
		case layoutEditView:
			view := node.Pane.Views[item.viewIndex]
			line = "○ " + view
		}
		if i == cursor {
			line = cursorStyle.Render(line)
		} else if item.kind == layoutEditSplit || item.kind == layoutEditPane {
			line = headerStyle.Render(line)
		}
		b.WriteString(prefix + indent + line)
		if i < len(items)-1 {
			b.WriteByte('\n')
		}
	}

	b.WriteString("\n\n")
	if m.msg != "" {
		b.WriteString(dimStyle.Render(m.msg) + "\n")
	}
	b.WriteString(dimStyle.Render(
		"↑/↓ select · ←/→ ratio · o orientation · s split ↔ · v split ↕ · m move view · [/] reorder · g merge · r reset · esc back",
	))

	box := boxStyle.Render(b.String())
	if m.width == 0 || m.height == 0 {
		return box
	}
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}
