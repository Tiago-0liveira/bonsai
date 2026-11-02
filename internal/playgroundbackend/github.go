package playgroundbackend

import (
	"context"
	"errors"
	"net"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden/launch"
	domain "github.com/Tiago-0liveira/bonsai/internal/git"
	"github.com/Tiago-0liveira/bonsai/internal/git/github"
	"github.com/Tiago-0liveira/bonsai/internal/ui/playgroundapi"
)

const githubTimeout = 20 * time.Second

// githubCode reduces a GitHub failure to a fixed code. Provider text is never kept.
func githubCode(err error) playgroundapi.Code {
	var netErr net.Error
	var urlErr *url.Error
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return playgroundapi.CodeTimeout
	case errors.Is(err, domain.ErrAuth):
		return playgroundapi.CodeAuthRequired
	case errors.Is(err, domain.ErrForbidden):
		return playgroundapi.CodePermission
	case errors.Is(err, domain.ErrNotFound):
		return playgroundapi.CodeNotFound
	case domain.Code(err) == "rate_limited":
		return playgroundapi.CodeRateLimited
	case errors.Is(err, domain.ErrOffline), errors.As(err, &netErr), errors.As(err, &urlErr):
		return playgroundapi.CodeOffline
	}
	return playgroundapi.CodeFailed
}

// parseGitHubRemote returns owner/repo for a github.com remote URL in the
// HTTPS, SCP or ssh:// form. Credentials in the URL are ignored and never kept.
func parseGitHubRemote(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	var path string
	switch {
	case strings.HasPrefix(raw, "git@github.com:"):
		path = strings.TrimPrefix(raw, "git@github.com:")
	case strings.Contains(raw, "://"):
		u, err := url.Parse(raw)
		if err != nil || !strings.EqualFold(u.Hostname(), "github.com") || (u.Scheme != "https" && u.Scheme != "http" && u.Scheme != "ssh" && u.Scheme != "git") {
			return "", false
		}
		path = strings.TrimPrefix(u.Path, "/")
	default:
		return "", false
	}
	path = strings.TrimSuffix(strings.TrimSuffix(path, "/"), ".git")
	owner, name, ok := strings.Cut(path, "/")
	if !ok || owner == "" || name == "" || strings.Contains(name, "/") || strings.ContainsAny(path, " \t\n?#") {
		return "", false
	}
	return owner + "/" + name, true
}

// repoSlug names the GitHub repository to ask: the attempt's verified
// repository when there is one, else origin (then any other github.com remote).
func (b *Backend) repoSlug(ctx context.Context, a *launch.Attempt) (string, playgroundapi.Code) {
	if a != nil && a.Proposal.Repository != "" {
		if slug, ok := parseGitHubRemote("https://github.com/" + a.Proposal.Repository); ok {
			return slug, ""
		}
	}
	if b.deps.Remotes == nil {
		return "", playgroundapi.CodeNoGitHubRemote
	}
	remotes, err := b.deps.Remotes(ctx)
	if err != nil {
		return "", playgroundapi.CodeFailed
	}
	sort.SliceStable(remotes, func(i, j int) bool { return remotes[i].Name == "origin" && remotes[j].Name != "origin" })
	for _, r := range remotes {
		if slug, ok := parseGitHubRemote(r.URL); ok {
			return slug, ""
		}
	}
	return "", playgroundapi.CodeNoGitHubRemote
}

// localHead returns the worktree's HEAD from the worktree list, empty for an
// unborn branch.
func (sc scoped) localHead() string {
	h := strings.ToLower(sc.tree.HEAD)
	if !shaRE.MatchString(h) || strings.Trim(h, "0") == "" {
		return ""
	}
	return h
}

func (sc scoped) branch() string {
	if sc.tree.Branch == "(detached)" {
		return ""
	}
	return sc.tree.Branch
}

// PullRequest implements playgroundapi.Backend.
func (b *Backend) PullRequest(ctx context.Context, req playgroundapi.PRRequest) (playgroundapi.PRView, error) {
	ctx, cancel := context.WithTimeout(ctx, githubTimeout)
	defer cancel()
	gen := b.genPR.Add(1)
	sc, err := b.scope(ctx, req.SessionID)
	if err != nil {
		return playgroundapi.PRView{}, err
	}
	v := playgroundapi.PRView{Stamp: b.stamp(gen), SessionID: req.SessionID, State: playgroundapi.PRUnavailable, Branch: clean(sc.branch(), 200), LocalHead: sc.localHead()}
	unavailable := func(code playgroundapi.Code) (playgroundapi.PRView, error) {
		if code == playgroundapi.CodeFailed || code == playgroundapi.CodeTimeout || code == playgroundapi.CodeOffline {
			if e := ctxErr(ctx); e != nil && !errors.Is(e, context.DeadlineExceeded) {
				return playgroundapi.PRView{}, e
			}
		}
		v.State, v.Code = playgroundapi.PRUnavailable, code
		return v, nil
	}
	branch := sc.branch()
	if sc.missing || branch == "" {
		return unavailable(playgroundapi.CodeNoBranch)
	}
	if b.deps.GitHub == nil {
		return unavailable(playgroundapi.CodeNoGitHub)
	}
	var attempt *launch.Attempt
	if a, ok := b.attempt(ctx, sc.session); ok {
		attempt = &a
	}
	repo, code := b.repoSlug(ctx, attempt)
	if code != "" {
		return unavailable(code)
	}

	fromAttempt := false
	if attempt != nil && attempt.PR.State == launch.PRLinked && attempt.PR.Number > 0 {
		// The linked number is only a hint: the pull request must still be this branch's.
		d, derr := b.deps.GitHub.PullRequest(ctx, repo, attempt.PR.Number)
		switch {
		case derr != nil:
			return unavailable(githubCode(derr))
		case matchesBranch(d.PullRequest, repo, branch):
			return b.found(v, d, sc.localHead(), true), nil
		}
	}
	owner, _, _ := strings.Cut(repo, "/")
	list, lerr := b.deps.GitHub.PullRequests(ctx, repo, github.PRFilter{State: "all", Head: owner + ":" + branch})
	if lerr != nil {
		return unavailable(githubCode(lerr))
	}
	var pick *github.PullRequest
	for i := range list {
		if !matchesBranch(list[i], repo, branch) {
			continue
		}
		// The list is newest-updated first: prefer an open one, else the newest.
		if pick == nil || (list[i].State == "open" && pick.State != "open") {
			pick = &list[i]
		}
	}
	if pick == nil {
		v.State, v.Code = playgroundapi.PRNone, ""
		return v, nil
	}
	d, derr := b.deps.GitHub.PullRequest(ctx, repo, pick.Number)
	if derr != nil {
		v.State = playgroundapi.PRFound
		v.PR = pullRequestOf(github.PullRequestDetail{PullRequest: *pick, Mergeable: "unknown"}, sc.localHead(), fromAttempt, false)
		v.DetailErr = fail(githubCode(derr), "Pull request details could not be read.")
		return v, nil
	}
	return b.found(v, d, sc.localHead(), fromAttempt), nil
}

func local(sc scoped) string { return sc.localHead() }

func matchesBranch(pr github.PullRequest, repo, branch string) bool {
	return pr.Head == branch && (pr.HeadRepository == "" || strings.EqualFold(pr.HeadRepository, repo))
}

func (b *Backend) found(v playgroundapi.PRView, d github.PullRequestDetail, localHead string, fromAttempt bool) playgroundapi.PRView {
	v.State, v.Code = playgroundapi.PRFound, ""
	v.PR = pullRequestOf(d, localHead, fromAttempt, true)
	return v
}

func pullRequestOf(d github.PullRequestDetail, localHead string, fromAttempt, detail bool) *playgroundapi.PullRequest {
	pr := &playgroundapi.PullRequest{
		Number: d.Number, Title: clean(d.Title, 300), Draft: d.Draft, URL: safeURL(d.URL),
		Base: clean(d.Base, 200), Head: clean(d.Head, 200), HeadSHA: strings.ToLower(d.HeadSHA),
		Mergeable: playgroundapi.MergeUnknown, FromAttempt: fromAttempt,
		HeadMatchesLocal: localHead != "" && strings.EqualFold(localHead, d.HeadSHA),
	}
	switch d.State {
	case "open":
		pr.Status = playgroundapi.PROpen
	case "merged":
		pr.Status = playgroundapi.PRMerged
	default:
		pr.Status = playgroundapi.PRClosed
	}
	if detail {
		switch d.Mergeable {
		case "mergeable":
			pr.Mergeable = playgroundapi.MergeMergeable
		case "conflicting":
			pr.Mergeable = playgroundapi.MergeConflicting
		}
		pr.ReviewKnown = true
		pr.Approvals, pr.ChangesRequested = d.ReviewSummary.Approvals, d.ReviewSummary.ChangesRequested
		pr.RequestedReviews = len(d.RequestedReviewers)
	}
	return pr
}

// safeURL keeps only an https GitHub-style URL without credentials.
func safeURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Host == "" {
		return ""
	}
	return clean(u.String(), 500)
}

// Checks implements playgroundapi.Backend.
func (b *Backend) Checks(ctx context.Context, req playgroundapi.ChecksRequest) (playgroundapi.ChecksView, error) {
	ctx, cancel := context.WithTimeout(ctx, githubTimeout)
	defer cancel()
	gen := b.genChecks.Add(1)
	if req.HeadSHA != "" && !anyRE.MatchString(req.HeadSHA) {
		return playgroundapi.ChecksView{}, fail(playgroundapi.CodeInvalidInput, "That commit is not valid.")
	}
	sc, err := b.scope(ctx, req.SessionID)
	if err != nil {
		return playgroundapi.ChecksView{}, err
	}
	sha := strings.ToLower(req.HeadSHA)
	if sha == "" {
		sha = sc.localHead()
	}
	v := playgroundapi.ChecksView{Stamp: b.stamp(gen), SessionID: req.SessionID, State: playgroundapi.ChecksUnavailable, HeadSHA: sha, LocalHead: sc.localHead()}
	v.IsLocal = sha != "" && sha == v.LocalHead
	unavailable := func(code playgroundapi.Code) (playgroundapi.ChecksView, error) {
		if code == playgroundapi.CodeFailed || code == playgroundapi.CodeTimeout || code == playgroundapi.CodeOffline {
			if e := ctxErr(ctx); e != nil && !errors.Is(e, context.DeadlineExceeded) {
				return playgroundapi.ChecksView{}, e
			}
		}
		v.State, v.Code = playgroundapi.ChecksUnavailable, code
		return v, nil
	}
	if sha == "" {
		return unavailable(playgroundapi.CodeNoBranch)
	}
	if b.deps.GitHub == nil {
		return unavailable(playgroundapi.CodeNoGitHub)
	}
	var attempt *launch.Attempt
	if a, ok := b.attempt(ctx, sc.session); ok {
		attempt = &a
	}
	repo, code := b.repoSlug(ctx, attempt)
	if code != "" {
		return unavailable(code)
	}
	list, cerr := b.deps.GitHub.Checks(ctx, repo, sha)
	if cerr != nil {
		return unavailable(githubCode(cerr))
	}
	v.State, v.Code = aggregate(list, &v)
	return v, nil
}

// resultOf classifies one check. Anything not known to have passed or failed is
// pending, so an unknown conclusion can never turn the aggregate green.
func resultOf(c github.Check) playgroundapi.ChecksState {
	if c.Status != "completed" {
		return playgroundapi.ChecksPending
	}
	switch c.Conclusion {
	case "success", "neutral", "skipped":
		return playgroundapi.ChecksPassed
	case "failure", "timed_out", "cancelled", "action_required", "startup_failure", "error":
		return playgroundapi.ChecksFailed
	}
	return playgroundapi.ChecksPending
}

func aggregate(list []github.Check, v *playgroundapi.ChecksView) (playgroundapi.ChecksState, playgroundapi.Code) {
	rows := make([]playgroundapi.Check, 0, len(list))
	for _, c := range list {
		r := resultOf(c)
		switch r {
		case playgroundapi.ChecksFailed:
			v.Counts.Failed++
		case playgroundapi.ChecksPassed:
			v.Counts.Passed++
		default:
			v.Counts.Pending++
		}
		rows = append(rows, playgroundapi.Check{Name: clean(c.Name, 200), Status: clean(c.Status, 40), Conclusion: clean(c.Conclusion, 40), URL: safeURL(c.URL), Result: r})
	}
	rank := map[playgroundapi.ChecksState]int{playgroundapi.ChecksFailed: 0, playgroundapi.ChecksPending: 1, playgroundapi.ChecksPassed: 2}
	sort.SliceStable(rows, func(i, j int) bool {
		if rank[rows[i].Result] != rank[rows[j].Result] {
			return rank[rows[i].Result] < rank[rows[j].Result]
		}
		return rows[i].Name < rows[j].Name
	})
	if len(rows) > playgroundapi.MaxChecks {
		rows, v.Truncated = rows[:playgroundapi.MaxChecks], true
	}
	v.Checks = rows
	switch {
	case v.Counts.Failed > 0:
		return playgroundapi.ChecksFailed, ""
	case v.Counts.Pending > 0:
		return playgroundapi.ChecksPending, ""
	case v.Counts.Passed > 0:
		return playgroundapi.ChecksPassed, ""
	}
	return playgroundapi.ChecksNone, ""
}
