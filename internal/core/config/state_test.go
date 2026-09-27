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
	if st.Prefs.Layout.Version != 0 || st.Prefs.Layout.Axis != "" ||
		len(st.Prefs.Layout.Order) != 0 || len(st.Prefs.Layout.Sizes) != 0 {
		t.Fatalf("old state should leave layout empty, got %+v", st.Prefs.Layout)
	}
	if st.Prefs.Theme != "bonsai" || st.Prefs.Sort != "activity" ||
		!st.Prefs.PruneMerge || st.Prefs.PRStatus != "compact" ||
		st.Prefs.Editor != "nvim" || st.Prefs.Keys["new_worktree"] != "N" {
		t.Fatalf("pre-existing prefs changed while decoding: %+v", st.Prefs)
	}
}

func TestStateLayoutRoundTripPreservesOtherPrefs(t *testing.T) {
	want := Prefs{
		Theme:      "sakura",
		Sort:       "dirty",
		Keys:       map[string]string{"prune": "X"},
		PruneMerge: true,
		PRStatus:   "off",
		Editor:     "hx",
		Layout: TUILayoutPrefs{
			Version: 1,
			Axis:    "vertical",
			Order:   []string{"workspace", "worktrees"},
			Sizes: map[string]int{
				"worktrees": 65,
				"workspace": 35,
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
	if got.Prefs.Layout.Version != want.Layout.Version ||
		got.Prefs.Layout.Axis != want.Layout.Axis ||
		len(got.Prefs.Layout.Order) != 2 ||
		got.Prefs.Layout.Order[0] != "workspace" ||
		got.Prefs.Layout.Order[1] != "worktrees" ||
		got.Prefs.Layout.Sizes["worktrees"] != 65 ||
		got.Prefs.Layout.Sizes["workspace"] != 35 {
		t.Fatalf("layout round trip mismatch: got %+v want %+v", got.Prefs.Layout, want.Layout)
	}
}
