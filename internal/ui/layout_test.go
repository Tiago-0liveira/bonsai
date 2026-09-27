package ui

import (
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
)

func validPersistedLayout() config.TUILayoutPrefs {
	return config.TUILayoutPrefs{
		Version: layoutVersion,
		Axis:    "vertical",
		Order:   []string{"workspace", "worktrees"},
		Sizes: map[string]int{
			"worktrees": 40,
			"workspace": 60,
		},
	}
}

func TestNormalizeLayoutPrefs(t *testing.T) {
	def := defaultLayoutSpec()
	valid := validPersistedLayout()
	wantValid := layoutSpec{
		Axis:             axisVertical,
		Order:            [2]paneID{paneWorkspace, paneWorktrees},
		WorktreesPercent: 40,
	}

	tests := []struct {
		name string
		pref config.TUILayoutPrefs
		want layoutSpec
	}{
		{name: "missing layout", pref: config.TUILayoutPrefs{}, want: def},
		{name: "valid", pref: valid, want: wantValid},
		{name: "missing axis", pref: func() config.TUILayoutPrefs {
			p := valid
			p.Axis = ""
			return p
		}(), want: def},
		{name: "unknown axis", pref: func() config.TUILayoutPrefs {
			p := valid
			p.Axis = "diagonal"
			return p
		}(), want: def},
		{name: "missing order", pref: func() config.TUILayoutPrefs {
			p := valid
			p.Order = nil
			return p
		}(), want: def},
		{name: "duplicate worktrees", pref: func() config.TUILayoutPrefs {
			p := valid
			p.Order = []string{"worktrees", "worktrees"}
			return p
		}(), want: def},
		{name: "duplicate workspace", pref: func() config.TUILayoutPrefs {
			p := valid
			p.Order = []string{"workspace", "workspace"}
			return p
		}(), want: def},
		{name: "unknown pane", pref: func() config.TUILayoutPrefs {
			p := valid
			p.Order = []string{"worktrees", "other"}
			return p
		}(), want: def},
		{name: "missing size", pref: func() config.TUILayoutPrefs {
			p := valid
			p.Sizes = map[string]int{"worktrees": 40}
			return p
		}(), want: def},
		{name: "negative size", pref: func() config.TUILayoutPrefs {
			p := valid
			p.Sizes = map[string]int{"worktrees": -5, "workspace": 105}
			return p
		}(), want: def},
		{name: "zero size", pref: func() config.TUILayoutPrefs {
			p := valid
			p.Sizes = map[string]int{"worktrees": 0, "workspace": 100}
			return p
		}(), want: def},
		{name: "sizes do not total 100", pref: func() config.TUILayoutPrefs {
			p := valid
			p.Sizes = map[string]int{"worktrees": 40, "workspace": 50}
			return p
		}(), want: def},
		{name: "split under 20", pref: func() config.TUILayoutPrefs {
			p := valid
			p.Sizes = map[string]int{"worktrees": 15, "workspace": 85}
			return p
		}(), want: def},
		{name: "split over 80", pref: func() config.TUILayoutPrefs {
			p := valid
			p.Sizes = map[string]int{"worktrees": 85, "workspace": 15}
			return p
		}(), want: def},
		{name: "future version", pref: func() config.TUILayoutPrefs {
			p := valid
			p.Version = layoutVersion + 1
			return p
		}(), want: def},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeLayoutPrefs(tt.pref); got != tt.want {
				t.Fatalf("normalizeLayoutPrefs() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestLayoutPersistedRoundTrip(t *testing.T) {
	want := layoutSpec{
		Axis:             axisVertical,
		Order:            [2]paneID{paneWorkspace, paneWorktrees},
		WorktreesPercent: 65,
	}
	if got := normalizeLayoutPrefs(want.persisted()); got != want {
		t.Fatalf("round trip = %+v, want %+v", got, want)
	}
}

func TestResolvePaneLayoutGeometry(t *testing.T) {
	sizes := [][2]int{
		{40, 15},
		{60, 20},
		{80, 24},
		{100, 40},
		{120, 30},
		{160, 50},
	}
	axes := []layoutAxis{axisHorizontal, axisVertical}
	orders := [][2]paneID{
		{paneWorktrees, paneWorkspace},
		{paneWorkspace, paneWorktrees},
	}
	splits := []int{20, 35, 50, 65, 80}

	for _, size := range sizes {
		for _, axis := range axes {
			for _, order := range orders {
				for _, split := range splits {
					spec := layoutSpec{
						Axis:             axis,
						Order:            order,
						WorktreesPercent: split,
					}
					got := resolvePaneLayout(size[0], size[1], 1, spec)
					name := spec.axisName() + "/" + string(order[0])

					if got.Body.W != size[0] || got.Body.H != size[1]-1 {
						t.Fatalf("%s %dx%d split %d: body = %+v", name, size[0], size[1], split, got.Body)
					}
					for pane, r := range map[paneID]paneRect{
						paneWorktrees: got.Worktrees,
						paneWorkspace: got.Workspace,
					} {
						if r.W <= 0 || r.H <= 0 {
							t.Fatalf("%s %dx%d split %d: %s has non-positive rect %+v", name, size[0], size[1], split, pane, r)
						}
						if r.X < 0 || r.Y < 0 || r.X+r.W > got.Body.W || r.Y+r.H > got.Body.H {
							t.Fatalf("%s %dx%d split %d: %s outside body: %+v body=%+v", name, size[0], size[1], split, pane, r, got.Body)
						}
					}
					if rectsOverlap(got.Worktrees, got.Workspace) {
						t.Fatalf("%s %dx%d split %d: panes overlap: %+v %+v", name, size[0], size[1], split, got.Worktrees, got.Workspace)
					}

					if axis == axisHorizontal {
						if got.Worktrees.W+got.Workspace.W != got.Body.W {
							t.Fatalf("%s: widths do not consume body", name)
						}
						if got.Worktrees.H != got.Body.H || got.Workspace.H != got.Body.H {
							t.Fatalf("%s: horizontal panes do not fill body height", name)
						}
						if got.Body.W >= minWorktreesOuterWidth+minWorkspaceOuterWidth &&
							(got.Worktrees.W < minWorktreesOuterWidth || got.Workspace.W < minWorkspaceOuterWidth) {
							t.Fatalf("%s: horizontal minimums not enforced: %+v %+v", name, got.Worktrees, got.Workspace)
						}
						if order[0] == paneWorktrees && got.Worktrees.X != 0 {
							t.Fatalf("%s: first worktrees pane X=%d", name, got.Worktrees.X)
						}
						if order[0] == paneWorkspace && got.Workspace.X != 0 {
							t.Fatalf("%s: first workspace pane X=%d", name, got.Workspace.X)
						}
					} else {
						if got.Worktrees.H+got.Workspace.H != got.Body.H {
							t.Fatalf("%s: heights do not consume body", name)
						}
						if got.Worktrees.W != got.Body.W || got.Workspace.W != got.Body.W {
							t.Fatalf("%s: vertical panes do not fill body width", name)
						}
						if got.Body.H >= minWorktreesOuterHeight+minWorkspaceOuterHeight &&
							(got.Worktrees.H < minWorktreesOuterHeight || got.Workspace.H < minWorkspaceOuterHeight) {
							t.Fatalf("%s: vertical minimums not enforced: %+v %+v", name, got.Worktrees, got.Workspace)
						}
						if order[0] == paneWorktrees && got.Worktrees.Y != 0 {
							t.Fatalf("%s: first worktrees pane Y=%d", name, got.Worktrees.Y)
						}
						if order[0] == paneWorkspace && got.Workspace.Y != 0 {
							t.Fatalf("%s: first workspace pane Y=%d", name, got.Workspace.Y)
						}
					}
				}
			}
		}
	}
}

func TestResolvedLayoutPaneAtFollowsAllArrangements(t *testing.T) {
	for _, axis := range []layoutAxis{axisHorizontal, axisVertical} {
		for _, order := range [][2]paneID{
			{paneWorktrees, paneWorkspace},
			{paneWorkspace, paneWorktrees},
		} {
			got := resolvePaneLayout(100, 30, 1, layoutSpec{
				Axis:             axis,
				Order:            order,
				WorktreesPercent: 35,
			})
			for want, r := range map[paneID]paneRect{
				paneWorktrees: got.Worktrees,
				paneWorkspace: got.Workspace,
			} {
				x := r.X + r.W/2
				y := r.Y + r.H/2
				if pane, ok := got.paneAt(x, y); !ok || pane != want {
					t.Fatalf("axis=%v order=%v point=(%d,%d): paneAt = %q,%v want %q,true", axis, order, x, y, pane, ok, want)
				}
			}
			if _, ok := got.paneAt(0, got.Body.H); ok {
				t.Fatalf("axis=%v order=%v: status bar row must not belong to a pane", axis, order)
			}
		}
	}
}

func rectsOverlap(a, b paneRect) bool {
	return a.X < b.X+b.W && a.X+a.W > b.X &&
		a.Y < b.Y+b.H && a.Y+a.H > b.Y
}
