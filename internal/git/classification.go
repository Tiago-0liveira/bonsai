package git

import "sort"

// ClassifyWorktrees is shared by watcher and full-refresh projections. A failed
// status read carries the last known connection, never infers a deleted ref.
func ClassifyWorktrees(state *RepositoryState, previous *RepositoryState) {
	known := map[string]WorktreeConnection{}
	if previous != nil {
		for _, w := range previous.Worktrees {
			known[w.ID] = w.Connection
		}
	}
	branches := map[string]Branch{}
	for _, b := range state.Branches {
		if !b.Remote {
			branches[b.Name] = b
		}
	}
	members := []string{}
	for i := range state.Worktrees {
		w := &state.Worktrees[i]
		c := WorktreeConnection{State: "unlinked"}
		switch {
		case w.Main:
			c = WorktreeConnection{State: "linked", Reason: "main"}
		case w.Status == nil || w.StatusError != nil:
			c = known[w.ID]
			if c.State == "" {
				c = WorktreeConnection{State: "unknown", Reason: "status_unavailable"}
			}
			c.StatusUnknown = true
		case w.Status.HeadState == "detached" || w.Status.Branch == "(detached)":
			c.Reason = "detached_head"
		case w.Status.HeadState == "unborn" || w.Status.HeadSHA == "":
			c.Reason = "unborn_branch"
		default:
			b, exists := branches[w.Branch]
			switch {
			case !exists:
				c.Reason = "branch_ref_missing"
			case w.Status.Upstream == "" && b.Upstream == "":
				c.Reason = "no_upstream"
			case w.Status.LocalRemoteRefSHA == "":
				c.Reason = "upstream_missing"
			default:
				c.State = "linked"
			}
		}
		w.Connection = c
		if !w.Main && c.State == "unlinked" {
			members = append(members, w.ID)
		}
	}
	sort.Strings(members)
	state.Groups = []WorktreeGroup{}
	if len(members) > 0 {
		state.Groups = append(state.Groups, WorktreeGroup{ID: "unlinked:" + state.ID, Kind: "unlinked", WorktreeIDs: members})
	}
}
