package config

import (
	"encoding/json"
	"testing"
)

func TestStateWithoutLayoutStillDecodes(t *testing.T) {
	data := []byte(`{
	  "copy_counts": {},
	  "aliases": [],
	  "prefs": {
	    "theme": "bonsai",
	    "sort": "activity",
	    "keys": {"new_worktree": "N"},
	    "prune_merge": true,
	    "pr_status": "compact",
	    "editor": "nvim"
	  }
	}`)

	var st State
	if err := json.Unmarshal(data, &st); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if st.Prefs.Layout.Version != 0 || st.Prefs.Layout.Root != nil ||
		st.Prefs.Layout.Axis != "" || len(st.Prefs.Layout.Order) != 0 ||
		len(st.Prefs.Layout.Sizes) != 0 {
		t.Fatalf("old state should leave layout empty, got %+v", st.Prefs.Layout)
	}
	if st.Prefs.Theme != "bonsai" || st.Prefs.Sort != "activity" ||
		!st.Prefs.PruneMerge || st.Prefs.PRStatus != "compact" ||
		st.Prefs.Editor != "nvim" || st.Prefs.Keys["new_worktree"] != "N" {
		t.Fatalf("pre-existing prefs changed while decoding: %+v", st.Prefs)
	}
}

func TestVersion1LayoutStillDecodes(t *testing.T) {
	data := []byte(`{
	  "prefs": {
	    "layout": {
	      "version": 1,
	      "axis": "vertical",
	      "order": ["workspace", "worktrees"],
	      "sizes": {"worktrees": 65, "workspace": 35}
	    }
	  }
	}`)
	var st State
	if err := json.Unmarshal(data, &st); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if st.Prefs.Layout.Version != 1 ||
		st.Prefs.Layout.Axis != "vertical" ||
		len(st.Prefs.Layout.Order) != 2 ||
		st.Prefs.Layout.Order[0] != "workspace" ||
		st.Prefs.Layout.Sizes["worktrees"] != 65 {
		t.Fatalf("v1 layout decode mismatch: %+v", st.Prefs.Layout)
	}
}

func TestStateVersion2LayoutRoundTripPreservesOtherPrefs(t *testing.T) {
	want := Prefs{
		Theme:      "sakura",
		Sort:       "dirty",
		Keys:       map[string]string{"prune": "X"},
		PruneMerge: true,
		PRStatus:   "off",
		Editor:     "hx",
		Layout: TUILayoutPrefs{
			Version: 2,
			Root: &TUILayoutNodePrefs{
				Type:  "split",
				Axis:  "horizontal",
				Ratio: 40,
				First: &TUILayoutNodePrefs{
					Type: "pane",
					Pane: &TUILayoutPanePrefs{
						ID:    "left",
						Views: []string{"worktrees", "inspect"},
					},
				},
				Second: &TUILayoutNodePrefs{
					Type: "pane",
					Pane: &TUILayoutPanePrefs{
						ID:    "right",
						Views: []string{"log", "processes", "diff", "checks", "pr"},
					},
				},
			},
		},
	}

	data, err := json.Marshal(State{CopyCounts: map[string]int{}, Prefs: want})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var got State
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}

	if got.Prefs.Theme != want.Theme ||
		got.Prefs.Sort != want.Sort ||
		got.Prefs.PruneMerge != want.PruneMerge ||
		got.Prefs.PRStatus != want.PRStatus ||
		got.Prefs.Editor != want.Editor ||
		got.Prefs.Keys["prune"] != want.Keys["prune"] {
		t.Fatalf("non-layout prefs were not preserved: got %+v want %+v", got.Prefs, want)
	}
	root := got.Prefs.Layout.Root
	if got.Prefs.Layout.Version != 2 || root == nil ||
		root.Type != "split" || root.Axis != "horizontal" || root.Ratio != 40 ||
		root.First == nil || root.First.Pane == nil ||
		root.First.Pane.ID != "left" ||
		len(root.First.Pane.Views) != 2 ||
		root.First.Pane.Views[1] != "inspect" ||
		root.Second == nil || root.Second.Pane == nil ||
		root.Second.Pane.ID != "right" ||
		len(root.Second.Pane.Views) != 5 {
		t.Fatalf("v2 layout round trip mismatch: got %+v", got.Prefs.Layout)
	}
}
