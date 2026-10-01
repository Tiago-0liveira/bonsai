package localapi

import (
	"testing"
	"time"

	domain "github.com/Tiago-0liveira/bonsai/internal/git"
	gh "github.com/Tiago-0liveira/bonsai/internal/git/github"
)

func TestBranchInventoryUsesFullRefsUpstreamsAndProviderIdentity(t *testing.T) {
	snapshot := browserSnapshot{Local: &domain.RepositoryState{
		Branches: []domain.Branch{
			{Name: "same", Ref: "refs/heads/same", LocalHeadSHA: "local"},
			{Name: "renamed", Ref: "refs/heads/renamed", UpstreamRef: "refs/remotes/origin/feature"},
			{Name: "origin/same", Ref: "refs/remotes/origin/same", Remote: true, LocalRemoteRefSHA: "origin"},
			{Name: "origin/feature", Ref: "refs/remotes/origin/feature", Remote: true},
			{Name: "fork/same", Ref: "refs/remotes/fork/same", Remote: true},
		},
		Worktrees: []domain.Worktree{{ID: "wt", Branch: "renamed"}},
		Remotes:   []domain.RemoteIdentity{{Name: "origin", FullName: "owner/repo"}, {Name: "fork", FullName: "contributor/repo"}},
	}, Remote: &browserRemoteSnapshot{
		Repository:   gh.RemoteRepository{FullName: "owner/repo"},
		Branches:     []gh.RemoteBranch{{Name: "same"}, {Name: "provider-only"}, {Name: "HEAD"}},
		PullRequests: []gh.PullRequest{{Number: 1, Head: "same", HeadRepository: "owner/repo"}, {Number: 2, Head: "same", HeadRepository: "contributor/repo"}},
	}}
	deriveBranchCandidates(&snapshot)
	byID := map[string]browserBranchCandidate{}
	for _, c := range snapshot.BranchCandidates {
		byID[c.ID] = c
	}
	if len(byID) != 6 {
		t.Fatal(byID)
	}
	if byID["refs/remotes/origin/same"].UnavailableReason != "local_branch_conflict" || len(byID["refs/heads/same"].PullRequests) != 0 {
		t.Fatal(byID)
	}
	if byID["refs/remotes/origin/same"].PullRequests[0].Number != 1 || byID["refs/remotes/fork/same"].PullRequests[0].Number != 2 {
		t.Fatal("fork incorrectly associated", byID)
	}
	if c := byID["refs/remotes/origin/feature"]; c.LocalBranch != "renamed" || len(c.WorktreeIDs) != 1 || c.UnavailableReason != "already_checked_out" {
		t.Fatal(c)
	}
	if c := byID["provider:origin:provider-only"]; c.Source != "provider" || c.CreationMode != "" || c.UnavailableReason != "fetch_required" {
		t.Fatal(c)
	}
	snapshot.Local.Remotes[0].FullName = "different/repo"
	deriveBranchCandidates(&snapshot)
	for _, c := range snapshot.BranchCandidates {
		if c.Source == "provider" {
			t.Fatal("fabricated a remote identity", c)
		}
	}
}

func TestSyncAndProviderCompletionTimestampsAreSemantic(t *testing.T) {
	first, second := time.Unix(1, 0).UTC(), time.Unix(2, 0).UTC()
	a := browserSnapshot{Freshness: map[string]browserFreshness{"local": {State: "ready", UpdatedAt: &first}}, Sync: domain.RepositorySync{Fetch: domain.SyncOutcome{State: "ready", CompletedAt: &first}}}
	b := cloneSnapshot(a)
	b.Freshness["local"] = browserFreshness{State: "ready", UpdatedAt: &second}
	if !snapshotsSemanticallyEqual(a, b) {
		t.Fatal("local polling timestamp caused an event")
	}
	b.Sync.Fetch.CompletedAt = &second
	if snapshotsSemanticallyEqual(a, b) {
		t.Fatal("fetch completion was suppressed")
	}
	b = cloneSnapshot(a)
	a.Freshness["provider"] = browserFreshness{State: "ready", UpdatedAt: &first}
	b.Freshness["provider"] = browserFreshness{State: "ready", UpdatedAt: &second}
	if snapshotsSemanticallyEqual(a, b) {
		t.Fatal("provider completion was suppressed")
	}
}
