package localapi

import (
	"sort"
	"strings"

	domain "github.com/Tiago-0liveira/bonsai/internal/git"
	gh "github.com/Tiago-0liveira/bonsai/internal/git/github"
)

func branchRef(b domain.Branch) string {
	if b.Ref != "" {
		return b.Ref
	}
	if b.Remote {
		return "refs/remotes/" + b.Name
	}
	return "refs/heads/" + b.Name
}

func deriveBranchCandidates(snapshot *browserSnapshot) {
	snapshot.BranchCandidates = []browserBranchCandidate{}
	if snapshot.Local == nil {
		return
	}
	local := snapshot.Local
	byRef := map[string]domain.Branch{}
	for _, b := range local.Branches {
		byRef[branchRef(b)] = b
	}
	seen := map[string]bool{}
	for _, b := range local.Branches {
		ref := branchRef(b)
		c := browserBranchCandidate{ID: ref, Ref: ref, Name: b.Name, Source: "local", LocalBranch: b.Name, HeadSHA: b.LocalHeadSHA, LastCommitAt: b.LastCommitAt, CreationMode: "existing", SourceRef: b.Name, PullRequests: []gh.PullRequest{}, WorktreeIDs: []string{}}
		providerRepo, providerBranch := "", ""
		if b.Remote {
			c.Source = "remote"
			c.Remote, c.Name, _ = strings.Cut(b.Name, "/")
			if c.Name == "HEAD" {
				continue
			}
			c.HeadSHA = b.LocalRemoteRefSHA
			c.LocalBranch = ""
			c.CreationMode, c.SourceRef = "remote", b.Name
			for _, lb := range local.Branches {
				upstreamRef := lb.UpstreamRef
				if upstreamRef == "" && lb.Upstream != "" {
					upstreamRef = "refs/remotes/" + lb.Upstream
				}
				if !lb.Remote && upstreamRef == ref {
					if c.LocalBranch != "" {
						c.CreationMode = ""
						c.UnavailableReason = "multiple_local_branches"
						break
					}
					c.LocalBranch, c.SourceRef, c.CreationMode = lb.Name, lb.Name, "existing"
				}
			}
			if c.LocalBranch == "" {
				if _, exists := byRef["refs/heads/"+c.Name]; exists {
					c.CreationMode = ""
					c.UnavailableReason = "local_branch_conflict"
				}
			}
			providerBranch = c.Name
			for _, remote := range local.Remotes {
				if remote.Name == c.Remote {
					providerRepo = remote.FullName
				}
			}
		} else {
			c.UpstreamRef = b.UpstreamRef
			upstream := b.Upstream
			if b.UpstreamRef != "" {
				upstream = strings.TrimPrefix(b.UpstreamRef, "refs/remotes/")
			}
			remoteName, name, ok := strings.Cut(upstream, "/")
			if ok {
				c.Remote = remoteName
				for _, remote := range local.Remotes {
					if remote.Name == remoteName {
						providerRepo, providerBranch = remote.FullName, name
					}
				}
			}
		}
		if snapshot.Remote != nil && providerRepo != "" {
			for _, pr := range snapshot.Remote.PullRequests {
				if pr.Head == providerBranch && strings.EqualFold(pr.HeadRepository, providerRepo) {
					c.PullRequests = append(c.PullRequests, pr)
				}
			}
		}
		for _, w := range local.Worktrees {
			if c.LocalBranch != "" && w.Branch == c.LocalBranch {
				c.WorktreeIDs = append(c.WorktreeIDs, w.ID)
			}
		}
		if len(c.WorktreeIDs) > 0 {
			c.CreationMode = ""
			c.UnavailableReason = "already_checked_out"
		}
		seen[ref] = true
		snapshot.BranchCandidates = append(snapshot.BranchCandidates, c)
	}
	if snapshot.Remote != nil {
		// Provider names only acquire a remote identity when that remote actually
		// points at the provider repository. They are never checkout-ready refs.
		for _, remote := range local.Remotes {
			if !strings.EqualFold(remote.FullName, snapshot.Remote.Repository.FullName) || remote.FullName == "" {
				continue
			}
			for _, b := range snapshot.Remote.Branches {
				ref := "refs/remotes/" + remote.Name + "/" + b.Name
				if seen[ref] || b.Name == "HEAD" {
					continue
				}
				c := browserBranchCandidate{ID: "provider:" + remote.Name + ":" + b.Name, Ref: ref, Name: b.Name, Source: "provider", Remote: remote.Name, HeadSHA: b.RemoteHeadSHA, UnavailableReason: "fetch_required", WorktreeIDs: []string{}, PullRequests: []gh.PullRequest{}}
				for _, pr := range snapshot.Remote.PullRequests {
					if pr.Head == b.Name && strings.EqualFold(pr.HeadRepository, remote.FullName) {
						c.PullRequests = append(c.PullRequests, pr)
					}
				}
				snapshot.BranchCandidates = append(snapshot.BranchCandidates, c)
			}
		}
	}
	sort.Slice(snapshot.BranchCandidates, func(i, j int) bool { return snapshot.BranchCandidates[i].ID < snapshot.BranchCandidates[j].ID })
}
