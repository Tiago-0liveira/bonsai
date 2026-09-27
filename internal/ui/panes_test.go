package ui

import (
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
)

func TestDefaultDynamicLayoutMatchesLegacyGrouping(t *testing.T) {
	layout := defaultDynamicLayout()
	if !layout.valid() {
		t.Fatal("default dynamic layout must be valid")
	}
	panes := layout.panes()
	if len(panes) != 2 {
		t.Fatalf("default panes = %d, want 2", len(panes))
	}
	if panes[0].ID != paneID("pane-worktrees") || len(panes[0].Views) != 1 || panes[0].Views[0] != viewWorktrees {
		t.Fatalf("unexpected worktree pane: %+v", panes[0])
	}
	wantWorkspace := []viewID{viewLog, viewProcesses, viewInspect, viewDiff, viewChecks, viewPR}
	if panes[1].ID != paneID("pane-workspace") || len(panes[1].Views) != len(wantWorkspace) {
		t.Fatalf("unexpected workspace pane: %+v", panes[1])
	}
	for i, want := range wantWorkspace {
		if panes[1].Views[i] != want {
			t.Fatalf("workspace view %d = %q, want %q", i, panes[1].Views[i], want)
		}
	}
}

func TestNormalizeDynamicLayoutMigratesVersion1(t *testing.T) {
	p := config.TUILayoutPrefs{
		Version: 1,
		Axis:    "vertical",
		Order:   []string{"workspace", "worktrees"},
		Sizes: map[string]int{
			"worktrees": 35,
			"workspace": 65,
		},
	}
	layout := normalizeDynamicLayout(p)
	if !layout.valid() {
		t.Fatal("migrated layout invalid")
	}
	if layout.Root.Axis != axisVertical || layout.Root.Ratio != 65 {
		t.Fatalf("root = axis %v ratio %d, want vertical/65", layout.Root.Axis, layout.Root.Ratio)
	}
	panes := layout.panes()
	if panes[0].ID != paneID("pane-workspace") || panes[1].ID != paneID("pane-worktrees") {
		t.Fatalf("migration order = %q/%q", panes[0].ID, panes[1].ID)
	}
}

func TestDynamicLayoutPersistenceRoundTrip(t *testing.T) {
	layout := dynamicLayout{Root: &layoutTreeNode{
		Axis:  axisHorizontal,
		Ratio: 40,
		First: &layoutTreeNode{Pane: &paneSpec{
			ID:    "left",
			Views: []viewID{viewWorktrees, viewInspect},
		}},
		Second: &layoutTreeNode{
			Axis:  axisVertical,
			Ratio: 50,
			First: &layoutTreeNode{Pane: &paneSpec{
				ID:    "top-right",
				Views: []viewID{viewLog, viewDiff},
			}},
			Second: &layoutTreeNode{Pane: &paneSpec{
				ID:    "bottom-right",
				Views: []viewID{viewProcesses, viewChecks, viewPR},
			}},
		},
	}}
	if !layout.valid() {
		t.Fatal("fixture invalid")
	}
	got := normalizeDynamicLayout(layout.persisted())
	if !got.valid() {
		t.Fatal("round-trip layout invalid")
	}
	if got.Root.Ratio != 40 || got.Root.Second == nil || got.Root.Second.Axis != axisVertical {
		t.Fatalf("round-trip tree changed: %+v", got.Root)
	}
}

func TestDynamicLayoutRejectsMalformedTrees(t *testing.T) {
	valid := defaultDynamicLayout().persisted()

	tests := []struct {
		name string
		mut  func(*config.TUILayoutPrefs)
	}{
		{"missing root", func(p *config.TUILayoutPrefs) { p.Root = nil }},
		{"unknown node", func(p *config.TUILayoutPrefs) { p.Root.Type = "grid" }},
		{"bad axis", func(p *config.TUILayoutPrefs) { p.Root.Axis = "diagonal" }},
		{"bad ratio", func(p *config.TUILayoutPrefs) { p.Root.Ratio = 10 }},
		{"missing child", func(p *config.TUILayoutPrefs) { p.Root.Second = nil }},
		{"empty pane", func(p *config.TUILayoutPrefs) { p.Root.First.Pane.Views = nil }},
		{"duplicate pane id", func(p *config.TUILayoutPrefs) { p.Root.Second.Pane.ID = p.Root.First.Pane.ID }},
		{"duplicate view", func(p *config.TUILayoutPrefs) {
			p.Root.Second.Pane.Views = append(p.Root.Second.Pane.Views, "worktrees")
		}},
		{"missing view", func(p *config.TUILayoutPrefs) {
			p.Root.Second.Pane.Views = p.Root.Second.Pane.Views[:len(p.Root.Second.Pane.Views)-1]
		}},
		{"unknown view", func(p *config.TUILayoutPrefs) {
			p.Root.Second.Pane.Views[0] = "unknown"
		}},
		{"future version", func(p *config.TUILayoutPrefs) { p.Version = 99 }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := valid
			p.Root = clonePersistedNode(valid.Root)
			tt.mut(&p)
			got := normalizeDynamicLayout(p)
			if !sameDefaultGrouping(got) {
				t.Fatalf("invalid layout did not fall back to default: %+v", got.Root)
			}
		})
	}
}

func clonePersistedNode(n *config.TUILayoutNodePrefs) *config.TUILayoutNodePrefs {
	if n == nil {
		return nil
	}
	out := &config.TUILayoutNodePrefs{
		Type:  n.Type,
		Axis:  n.Axis,
		Ratio: n.Ratio,
		First: clonePersistedNode(n.First),
		Second: clonePersistedNode(n.Second),
	}
	if n.Pane != nil {
		out.Pane = &config.TUILayoutPanePrefs{
			ID:    n.Pane.ID,
			Views: append([]string(nil), n.Pane.Views...),
		}
	}
	return out
}

func sameDefaultGrouping(layout dynamicLayout) bool {
	want := defaultDynamicLayout().persisted()
	got := layout.persisted()
	return persistedLayoutEqual(got.Root, want.Root)
}

func persistedLayoutEqual(a, b *config.TUILayoutNodePrefs) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.Type != b.Type || a.Axis != b.Axis || a.Ratio != b.Ratio {
		return false
	}
	if (a.Pane == nil) != (b.Pane == nil) {
		return false
	}
	if a.Pane != nil {
		if a.Pane.ID != b.Pane.ID || len(a.Pane.Views) != len(b.Pane.Views) {
			return false
		}
		for i := range a.Pane.Views {
			if a.Pane.Views[i] != b.Pane.Views[i] {
				return false
			}
		}
	}
	return persistedLayoutEqual(a.First, b.First) && persistedLayoutEqual(a.Second, b.Second)
}

func TestResolveDynamicLayoutNestedGeometry(t *testing.T) {
	layout := dynamicLayout{Root: &layoutTreeNode{
		Axis:  axisHorizontal,
		Ratio: 35,
		First: &layoutTreeNode{Pane: &paneSpec{
			ID:    "work",
			Views: []viewID{viewWorktrees},
		}},
		Second: &layoutTreeNode{
			Axis:  axisVertical,
			Ratio: 50,
			First: &layoutTreeNode{Pane: &paneSpec{
				ID:    "log",
				Views: []viewID{viewLog, viewInspect, viewDiff},
			}},
			Second: &layoutTreeNode{Pane: &paneSpec{
				ID:    "proc",
				Views: []viewID{viewProcesses, viewChecks, viewPR},
			}},
		},
	}}
	resolved := resolveDynamicLayout(100, 40, 1, layout)
	if len(resolved.Panes) != 3 {
		t.Fatalf("resolved panes = %d, want 3", len(resolved.Panes))
	}
	for _, pane := range resolved.Panes {
		if pane.Rect.W <= 0 || pane.Rect.H <= 0 {
			t.Fatalf("pane %q non-positive rect: %+v", pane.ID, pane.Rect)
		}
		if pane.Rect.X < 0 || pane.Rect.Y < 0 ||
			pane.Rect.X+pane.Rect.W > resolved.Body.W ||
			pane.Rect.Y+pane.Rect.H > resolved.Body.H {
			t.Fatalf("pane %q outside body: %+v body=%+v", pane.ID, pane.Rect, resolved.Body)
		}
	}
	for i := range resolved.Panes {
		for j := i + 1; j < len(resolved.Panes); j++ {
			if rectsOverlap(resolved.Panes[i].Rect, resolved.Panes[j].Rect) {
				t.Fatalf("panes overlap: %+v %+v", resolved.Panes[i], resolved.Panes[j])
			}
		}
	}
}
