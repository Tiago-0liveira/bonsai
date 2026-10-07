package git

import "testing"

func TestConnectionClassificationAndUnknownRetention(t *testing.T) {
	state := RepositoryState{ID: "repo", Branches: []Branch{{Name: "linked"}, {Name: "local"}, {Name: "gone"}}, Worktrees: []Worktree{
		{ID: "main", Main: true},
		{ID: "linked", Branch: "linked", Status: &WorkingTreeStatus{HeadSHA: "sha", Upstream: "origin/linked", LocalRemoteRefSHA: "up"}},
		{ID: "local", Branch: "local", Status: &WorkingTreeStatus{HeadSHA: "sha"}},
		{ID: "gone", Branch: "gone", Status: &WorkingTreeStatus{HeadSHA: "sha", Upstream: "origin/gone"}},
		{ID: "detached", Status: &WorkingTreeStatus{HeadState: "detached", HeadSHA: "sha"}},
		{ID: "unborn", Status: &WorkingTreeStatus{HeadState: "unborn"}},
		{ID: "missing", Branch: "missing", Status: &WorkingTreeStatus{HeadSHA: "sha"}},
		{ID: "unreadable", StatusError: &StateError{Code: "unavailable"}},
	}}
	ClassifyWorktrees(&state, nil)
	want := []string{"main", "", "no_upstream", "upstream_missing", "detached_head", "unborn_branch", "branch_ref_missing", "status_unavailable"}
	for i, w := range state.Worktrees {
		if w.Connection.Reason != want[i] {
			t.Fatalf("%s: %+v", w.ID, w.Connection)
		}
	}
	if len(state.Groups) != 1 || len(state.Groups[0].WorktreeIDs) != 5 {
		t.Fatal(state.Groups)
	}
	next := RepositoryState{ID: "repo", Worktrees: []Worktree{{ID: "linked"}, {ID: "local"}}}
	ClassifyWorktrees(&next, &state)
	if next.Worktrees[0].Connection.State != "linked" || !next.Worktrees[0].Connection.StatusUnknown || next.Worktrees[1].Connection.Reason != "no_upstream" || len(next.Groups[0].WorktreeIDs) != 1 {
		t.Fatal(next)
	}
}
