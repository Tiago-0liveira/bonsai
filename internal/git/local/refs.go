package local

import (
	"context"
	"strconv"
	"strings"
	"time"

	domain "github.com/Tiago-0liveira/bonsai/internal/git"
)

// refFormat is one NUL-separated record per ref. The tip commit's subject and
// author ride along so a worktree's last commit needs no `git log -1`.
const refFormat = "%(refname)%00%(objectname)%00%(upstream:short)%00%(symref)%00%(upstream)%00%(committerdate:unix)%00%(subject)%00%(authorname)"

const refFields = 8

// refInfo is one branch ref with its tip commit metadata.
type refInfo struct {
	Branch  domain.Branch
	SHA     string
	Subject string
	Author  string
}

// refIndex answers "what does this ref point at" without spawning Git.
type refIndex struct {
	list  []refInfo
	byRef map[string]*refInfo
	// originHead is the target of refs/remotes/origin/HEAD, when it is set.
	originHead string
}

func listRefs(ctx context.Context, root string) (*refIndex, error) {
	out, err := run(ctx, root, "for-each-ref", "--format="+refFormat, "refs/heads/", "refs/remotes/")
	if err != nil {
		return nil, err
	}
	return parseRefs(out), nil
}

func parseRefs(out string) *refIndex {
	idx := &refIndex{list: []refInfo{}, byRef: map[string]*refInfo{}}
	for _, line := range strings.Split(strings.TrimSuffix(out, "\n"), "\n") {
		p := strings.Split(line, "\x00")
		if len(p) != refFields {
			continue
		}
		if p[3] != "" {
			if p[0] == "refs/remotes/origin/HEAD" {
				idx.originHead = p[3]
			}
			continue
		}
		b := domain.Branch{Ref: p[0], Upstream: p[2], UpstreamRef: p[4]}
		if sec, err := strconv.ParseInt(p[5], 10, 64); err == nil {
			when := time.Unix(sec, 0).UTC()
			b.LastCommitAt = &when
		}
		b.Remote = strings.HasPrefix(p[0], "refs/remotes/")
		if b.Remote {
			b.Name = strings.TrimPrefix(p[0], "refs/remotes/")
			b.LocalRemoteRefSHA = p[1]
			b.RemoteName, _, _ = strings.Cut(b.Name, "/")
		} else {
			b.Name = strings.TrimPrefix(p[0], "refs/heads/")
			b.LocalHeadSHA = p[1]
		}
		idx.list = append(idx.list, refInfo{Branch: b, SHA: p[1], Subject: p[6], Author: p[7]})
	}
	for i := range idx.list {
		idx.byRef[idx.list[i].Branch.Ref] = &idx.list[i]
	}
	return idx
}

func (x *refIndex) branches() []domain.Branch {
	out := make([]domain.Branch, 0, len(x.list))
	for _, info := range x.list {
		out = append(out, info.Branch)
	}
	return out
}

// upstreamSHA is the commit the local branch's upstream ref points at. It
// reports false when the answer is not certain, so the caller asks Git.
func (x *refIndex) upstreamSHA(branch string) (string, bool) {
	if x == nil || branch == "" {
		return "", false
	}
	local := x.byRef["refs/heads/"+branch]
	if local == nil || local.Branch.UpstreamRef == "" {
		return "", false
	}
	upstream := x.byRef[local.Branch.UpstreamRef]
	if upstream == nil || upstream.SHA == "" {
		return "", false
	}
	return upstream.SHA, true
}

// tipCommit returns the branch tip as a Commit when it is exactly head.
func (x *refIndex) tipCommit(branch, head string) (domain.Commit, bool) {
	if x == nil || branch == "" || head == "" {
		return domain.Commit{}, false
	}
	info := x.byRef["refs/heads/"+branch]
	if info == nil || info.SHA != head || info.Branch.LastCommitAt == nil {
		return domain.Commit{}, false
	}
	return domain.Commit{SHA: info.SHA, Subject: info.Subject, Author: info.Author, When: *info.Branch.LastCommitAt}, true
}
