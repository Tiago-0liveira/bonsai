package playgroundbackend

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/launch"
	coregit "github.com/Tiago-0liveira/bonsai/internal/core/git"
	domain "github.com/Tiago-0liveira/bonsai/internal/git"
	"github.com/Tiago-0liveira/bonsai/internal/git/github"
	"github.com/Tiago-0liveira/bonsai/internal/ui/playgroundapi"
)

func prRig(t *testing.T) (*rig, string) {
	r := newRig(t, "main")
	wt := r.worktree("feature")
	r.add("s", wt)
	return r, wt
}

func openPR(n int, head string) github.PullRequest {
	return github.PullRequest{Number: n, Title: "Add feature", State: "open", Head: "feature", HeadRepository: "acme/widgets", Base: "main", HeadSHA: head, URL: fmt.Sprintf("https://github.com/acme/widgets/pull/%d", n)}
}

func TestPullRequestFound(t *testing.T) {
	r, wt := prRig(t)
	head := sh(t, wt, "rev-parse", "HEAD")
	older := openPR(8, "abc")
	older.State = "closed"
	r.gh.prs = []github.PullRequest{older, openPR(9, head), {Number: 3, Head: "other-branch", HeadRepository: "acme/widgets", State: "open"}, {Number: 4, Head: "feature", HeadRepository: "fork/widgets", State: "open"}}
	r.gh.details[9] = github.PullRequestDetail{PullRequest: openPR(9, head), Mergeable: "conflicting", ReviewSummary: github.ReviewSummary{Approvals: 2, ChangesRequested: 1}, RequestedReviewers: []string{"a"}}
	v, err := r.b.PullRequest(context.Background(), playgroundapi.PRRequest{SessionID: "s"})
	if err != nil || v.State != playgroundapi.PRFound || v.PR == nil {
		t.Fatalf("view = %+v err=%v", v, err)
	}
	pr := v.PR
	if pr.Number != 9 || pr.Status != playgroundapi.PROpen || pr.Mergeable != playgroundapi.MergeConflicting || !pr.ReviewKnown ||
		pr.Approvals != 2 || pr.ChangesRequested != 1 || pr.RequestedReviews != 1 || !pr.HeadMatchesLocal || pr.FromAttempt || v.LocalHead != head {
		t.Fatalf("pr = %+v", pr)
	}
	if r.gh.filter.Head != "acme:feature" || r.gh.filter.State != "all" {
		t.Fatalf("filter = %+v", r.gh.filter)
	}
	if strings.Contains(strings.Join(r.gh.calls, ","), "token") {
		t.Fatal("credentials from the remote URL reached the service")
	}
}

func TestPullRequestVerifiedAbsenceVsUnavailable(t *testing.T) {
	ctx := context.Background()
	r, _ := prRig(t)
	r.gh.prs = []github.PullRequest{{Number: 3, Head: "other", HeadRepository: "acme/widgets"}}
	v, err := r.b.PullRequest(ctx, playgroundapi.PRRequest{SessionID: "s"})
	if err != nil || v.State != playgroundapi.PRNone || v.PR != nil || v.Code != "" {
		t.Fatalf("none = %+v err=%v", v, err)
	}

	for name, tc := range map[string]struct {
		err  error
		code playgroundapi.Code
	}{
		"auth":    {domain.ErrAuth, playgroundapi.CodeAuthRequired},
		"denied":  {domain.ErrForbidden, playgroundapi.CodePermission},
		"limited": {domain.E("rate_limited", "secret provider text"), playgroundapi.CodeRateLimited},
		"missing": {domain.ErrNotFound, playgroundapi.CodeNotFound},
		"offline": {fmt.Errorf("dial: %w", domain.ErrOffline), playgroundapi.CodeOffline},
		"other":   {errors.New("HTTP 500 token=abc"), playgroundapi.CodeFailed},
	} {
		r.gh.prsErr = tc.err
		v, err = r.b.PullRequest(ctx, playgroundapi.PRRequest{SessionID: "s"})
		if err != nil || v.State != playgroundapi.PRUnavailable || v.Code != tc.code || v.PR != nil {
			t.Errorf("%s: %+v err=%v", name, v, err)
		}
	}

	r.gh.prsErr = nil
	r.b.deps.GitHub = nil
	v, _ = r.b.PullRequest(ctx, playgroundapi.PRRequest{SessionID: "s"})
	if v.State != playgroundapi.PRUnavailable || v.Code != playgroundapi.CodeNoGitHub {
		t.Fatalf("no service = %+v", v)
	}
	r.b.deps.GitHub = r.gh
	r.remotes = []coregit.Remote{{Name: "origin", URL: "https://gitlab.com/acme/widgets.git"}}
	v, _ = r.b.PullRequest(ctx, playgroundapi.PRRequest{SessionID: "s"})
	if v.State != playgroundapi.PRUnavailable || v.Code != playgroundapi.CodeNoGitHubRemote {
		t.Fatalf("no remote = %+v", v)
	}
}

func TestPullRequestDetachedAndDetailFailure(t *testing.T) {
	ctx := context.Background()
	r := newRig(t, "main")
	det := r.worktree("x")
	sh(t, det, "checkout", "--detach")
	r.add("d", det)
	v, err := r.b.PullRequest(ctx, playgroundapi.PRRequest{SessionID: "d"})
	if err != nil || v.State != playgroundapi.PRUnavailable || v.Code != playgroundapi.CodeNoBranch || len(r.gh.calls) != 0 {
		t.Fatalf("detached = %+v err=%v calls=%v", v, err, r.gh.calls)
	}

	r, wt := prRig(t)
	head := sh(t, wt, "rev-parse", "HEAD")
	r.gh.prs = []github.PullRequest{openPR(9, head)}
	r.gh.detailErr = errors.New("boom")
	v, err = r.b.PullRequest(ctx, playgroundapi.PRRequest{SessionID: "s"})
	if err != nil || v.State != playgroundapi.PRFound || v.DetailErr == nil || v.PR == nil || v.PR.ReviewKnown || v.PR.Mergeable != playgroundapi.MergeUnknown {
		t.Fatalf("detail failure = %+v err=%v", v, err)
	}
}

func TestPullRequestPrefersAttemptLinkButVerifiesHead(t *testing.T) {
	ctx := context.Background()
	r, wt := prRig(t)
	head := sh(t, wt, "rev-parse", "HEAD")
	a := launch.Attempt{ID: "att", Local: r.b.local, Issue: garden.IssueID(5), Number: 12}
	a.Proposal.Repository = "acme/widgets"
	a.PR = launch.PRLink{State: launch.PRLinked, Number: 21}
	r.attempts.byID["att"] = a
	r.sessions.list[0].Task = &agents.TaskRef{IssueID: 5, AttemptID: "att"}
	r.remotes = nil // the attempt's verified repository is enough

	r.gh.details[21] = github.PullRequestDetail{PullRequest: openPR(21, head), Mergeable: "mergeable"}
	v, err := r.b.PullRequest(ctx, playgroundapi.PRRequest{SessionID: "s"})
	if err != nil || v.State != playgroundapi.PRFound || v.PR.Number != 21 || !v.PR.FromAttempt || len(r.gh.calls) != 1 {
		t.Fatalf("linked = %+v err=%v calls=%v", v, err, r.gh.calls)
	}

	// The linked number now points at another branch: it is not trusted.
	other := openPR(21, head)
	other.Head = "unrelated"
	r.gh.details[21] = github.PullRequestDetail{PullRequest: other}
	r.gh.details[30] = github.PullRequestDetail{PullRequest: openPR(30, head)}
	r.gh.prs = []github.PullRequest{openPR(30, head)}
	r.gh.calls = nil
	v, err = r.b.PullRequest(ctx, playgroundapi.PRRequest{SessionID: "s"})
	if err != nil || v.State != playgroundapi.PRFound || v.PR.Number != 30 || v.PR.FromAttempt {
		t.Fatalf("mismatched link = %+v err=%v", v, err)
	}

	// A failed lookup of the linked number is unavailable, not "none".
	r.gh.detailErr = domain.ErrOffline
	v, _ = r.b.PullRequest(ctx, playgroundapi.PRRequest{SessionID: "s"})
	if v.State != playgroundapi.PRUnavailable || v.Code != playgroundapi.CodeOffline {
		t.Fatalf("linked lookup failure = %+v", v)
	}
}

func TestChecksAggregation(t *testing.T) {
	ctx := context.Background()
	chk := func(name, status, conclusion string) github.Check {
		return github.Check{Name: name, Status: status, Conclusion: conclusion, URL: "https://github.com/acme/widgets/runs/1"}
	}
	cases := map[string]struct {
		in   []github.Check
		want playgroundapi.ChecksState
		c    playgroundapi.ChecksCounts
	}{
		"empty":           {nil, playgroundapi.ChecksNone, playgroundapi.ChecksCounts{}},
		"passed":          {[]github.Check{chk("a", "completed", "success"), chk("b", "completed", "skipped"), chk("c", "completed", "neutral")}, playgroundapi.ChecksPassed, playgroundapi.ChecksCounts{Passed: 3}},
		"pending":         {[]github.Check{chk("a", "completed", "success"), chk("b", "in_progress", "")}, playgroundapi.ChecksPending, playgroundapi.ChecksCounts{Passed: 1, Pending: 1}},
		"failed wins":     {[]github.Check{chk("a", "queued", ""), chk("b", "completed", "failure")}, playgroundapi.ChecksFailed, playgroundapi.ChecksCounts{Pending: 1, Failed: 1}},
		"cancelled":       {[]github.Check{chk("a", "completed", "cancelled")}, playgroundapi.ChecksFailed, playgroundapi.ChecksCounts{Failed: 1}},
		"unknown is open": {[]github.Check{chk("a", "completed", "mystery"), chk("b", "completed", "success")}, playgroundapi.ChecksPending, playgroundapi.ChecksCounts{Passed: 1, Pending: 1}},
		"no conclusion":   {[]github.Check{chk("a", "completed", "")}, playgroundapi.ChecksPending, playgroundapi.ChecksCounts{Pending: 1}},
		"status error":    {[]github.Check{chk("a", "completed", "error")}, playgroundapi.ChecksFailed, playgroundapi.ChecksCounts{Failed: 1}},
	}
	for name, tc := range cases {
		r, wt := prRig(t)
		r.gh.checks = tc.in
		head := sh(t, wt, "rev-parse", "HEAD")
		v, err := r.b.Checks(ctx, playgroundapi.ChecksRequest{SessionID: "s"})
		if err != nil || v.State != tc.want || v.Counts != tc.c || v.HeadSHA != head || !v.IsLocal || v.LocalHead != head {
			t.Errorf("%s: %+v err=%v", name, v, err)
		}
	}
}

func TestChecksFreshnessUnavailableAndCaps(t *testing.T) {
	ctx := context.Background()
	r, wt := prRig(t)
	head := sh(t, wt, "rev-parse", "HEAD")

	older := strings.Repeat("a", 40)
	v, err := r.b.Checks(ctx, playgroundapi.ChecksRequest{SessionID: "s", HeadSHA: strings.ToUpper(older)})
	if err != nil || v.HeadSHA != older || v.IsLocal || v.LocalHead != head {
		t.Fatalf("older commit = %+v err=%v", v, err)
	}
	if !strings.HasSuffix(r.gh.calls[len(r.gh.calls)-1], "@"+older) {
		t.Fatalf("calls = %v", r.gh.calls)
	}
	for _, bad := range []string{"main", "../x", "abc", "zz" + strings.Repeat("a", 38)} {
		_, err = r.b.Checks(ctx, playgroundapi.ChecksRequest{SessionID: "s", HeadSHA: bad})
		wantCode(t, err, playgroundapi.CodeInvalidInput)
	}

	r.gh.checksErr = errors.New("500 from github")
	v, err = r.b.Checks(ctx, playgroundapi.ChecksRequest{SessionID: "s"})
	if err != nil || v.State != playgroundapi.ChecksUnavailable || v.Code != playgroundapi.CodeFailed || len(v.Checks) != 0 {
		t.Fatalf("failed lookup = %+v err=%v", v, err)
	}
	r.gh.checksErr = nil

	for i := 0; i < playgroundapi.MaxChecks+10; i++ {
		r.gh.checks = append(r.gh.checks, github.Check{Name: fmt.Sprintf("c%03d", i), Status: "completed", Conclusion: "success"})
	}
	r.gh.checks = append(r.gh.checks, github.Check{Name: "zzz-last", Status: "completed", Conclusion: "failure"})
	v, err = r.b.Checks(ctx, playgroundapi.ChecksRequest{SessionID: "s"})
	if err != nil || !v.Truncated || len(v.Checks) != playgroundapi.MaxChecks || v.Counts.Passed != playgroundapi.MaxChecks+10 || v.Counts.Failed != 1 || v.Checks[0].Name != "zzz-last" || v.State != playgroundapi.ChecksFailed {
		t.Fatalf("cap = trunc=%v n=%d counts=%+v first=%q", v.Truncated, len(v.Checks), v.Counts, v.Checks[0].Name)
	}

	// Checks do not depend on the pull request lookup.
	r.gh.prsErr = errors.New("down")
	if v, err = r.b.Checks(ctx, playgroundapi.ChecksRequest{SessionID: "s"}); err != nil || v.State != playgroundapi.ChecksFailed {
		t.Fatalf("checks with PR lookup down = %+v err=%v", v, err)
	}

	// A session outside the repository gets no CI read at all.
	r.sessions.list = append(r.sessions.list, session("alien", t.TempDir(), "other"))
	n := len(r.gh.calls)
	_, err = r.b.Checks(ctx, playgroundapi.ChecksRequest{SessionID: "alien"})
	wantCode(t, err, playgroundapi.CodeNotInRepository)
	if len(r.gh.calls) != n {
		t.Fatal("GitHub was called for a session outside the repository")
	}
}

func TestParseGitHubRemote(t *testing.T) {
	for in, want := range map[string]string{
		"https://github.com/acme/widgets.git":          "acme/widgets",
		"https://user:tok@github.com/acme/widgets":     "acme/widgets",
		"git@github.com:acme/widgets.git":              "acme/widgets",
		"ssh://git@github.com/acme/widgets.git":        "acme/widgets",
		"https://GitHub.com/Acme/Widgets/":             "Acme/Widgets",
		"https://github.com.evil.example/acme/widgets": "",
		"https://gitlab.com/acme/widgets.git":          "",
		"git@github.com:acme/widgets/extra.git":        "",
		"file:///srv/acme/widgets":                     "",
		"/local/path":                                  "",
		"https://github.com/acme":                      "",
		"https://github.com/acme/widgets?x=1#frag":     "acme/widgets",
		"https://github.com/acme/wid gets":             "",
	} {
		got, ok := parseGitHubRemote(in)
		if got != want || ok != (want != "") {
			t.Errorf("parseGitHubRemote(%q) = %q,%v want %q", in, got, ok, want)
		}
	}
}
