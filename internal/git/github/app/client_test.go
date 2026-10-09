package app

import (
	"context"
	"encoding/json"
	"fmt"
	domain "github.com/Tiago-0liveira/bonsai/internal/git"
	gh "github.com/Tiago-0liveira/bonsai/internal/git/github"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

type testTokens struct{}

func (testTokens) Token(_ context.Context, _ string, mutation bool) (string, error) {
	if mutation {
		return "user", nil
	}
	return "installation", nil
}
func TestPaginationAuthAndMergeProtection(t *testing.T) {
	reads, mutations := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			reads++
			if r.Header.Get("Authorization") != "Bearer installation" {
				t.Error("wrong read token")
			}
			if r.URL.Query().Get("page") == "1" {
				w.Header().Set("Link", `<https://api.github.com/next>; rel="next"`)
				fmt.Fprint(w, `[{"name":"main","commit":{"sha":"remote-head"}}]`)
			} else {
				fmt.Fprint(w, `[{"name":"feature","commit":{"sha":"second"}}]`)
			}
			return
		}
		mutations++
		if r.Header.Get("Authorization") != "Bearer user" {
			t.Error("wrong mutation token")
		}
		w.WriteHeader(405)
		fmt.Fprint(w, `{"message":"Branch protection rejected merge"}`)
	}))
	defer server.Close()
	c := New(testTokens{})
	c.BaseURL = server.URL
	branches, e := c.Branches(context.Background(), "owner/repo")
	if e != nil || len(branches) != 2 || branches[0].RemoteHeadSHA != "remote-head" || reads != 2 {
		t.Fatal(branches, e, reads)
	}
	if e = c.MergePullRequest(context.Background(), gh.MergePullRequestRequest{Repository: "owner/repo", Number: 1, Method: "merge"}); domain.Code(e) != "invalid" || mutations != 0 {
		t.Fatal(e)
	}
	e = c.MergePullRequest(context.Background(), gh.MergePullRequestRequest{Repository: "owner/repo", Number: 1, Method: "merge", HeadSHA: "expected"})
	if e == nil || mutations != 1 {
		t.Fatal(e)
	}
}
func TestRateLimitAndUnknownMergeability(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Remaining", "0")
		w.WriteHeader(403)
		fmt.Fprint(w, `{"message":"rate limited"}`)
	}))
	defer server.Close()
	c := New(testTokens{})
	c.BaseURL = server.URL
	_, e := c.Repository(context.Background(), "owner/repo")
	if domain.Code(e) != "rate_limited" {
		t.Fatal(e)
	}
}

func TestPullRequestIncludesHeadRepositoryIdentity(t *testing.T) {
	var raw rawPR
	if err := json.Unmarshal([]byte(`{
		"number": 12,
		"state": "open",
		"head": {"ref": "feature", "sha": "abc", "repo": {"full_name": "fork/widgets"}},
		"base": {"ref": "main", "sha": "def"}
	}`), &raw); err != nil {
		t.Fatal(err)
	}
	pull := raw.domain()
	if pull.Head != "feature" || pull.HeadSHA != "abc" || pull.HeadRepository != "fork/widgets" {
		t.Fatalf("pull request identity = %+v", pull)
	}
}

func TestAllStatePullRequestPaginationBeyondPreviousLimit(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Query().Get("state") != "all" || r.URL.Query().Get("sort") != "updated" {
			t.Error("missing all-state, updated-order pagination")
		}
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		if page < 101 {
			w.Header().Set("Link", `<https://api.github.com/next>; rel="next"`)
		}
		fmt.Fprintf(w, `[{"number":%d,"state":"closed","merged_at":"2026-01-01T00:00:00Z","head":{"ref":"feature","repo":{"full_name":"fork/repo"}}}]`, page)
	}))
	defer server.Close()
	c := New(testTokens{})
	c.BaseURL = server.URL
	prs, err := c.PullRequests(context.Background(), "owner/repo", gh.PRFilter{State: "all"})
	if err != nil || len(prs) != 101 || calls != 101 || prs[100].State != "merged" || prs[0].HeadRepository != "fork/repo" {
		t.Fatal(len(prs), calls, err)
	}
}

func pullRequestServer(t *testing.T, state string, compareStatus int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.EscapedPath() {
		case "/repos/owner/repo/pulls/7":
			fmt.Fprintf(w, `{"number":7,"state":%q,"head":{"ref":"feat/x","sha":"abc"},"base":{"ref":"release/1"},
				"mergeable":true,"additions":120,"deletions":30,"changed_files":9,
				"requested_reviewers":[{"login":"ana"},{"login":"bo"}],"requested_teams":[{"slug":"core"}]}`, state)
		case "/repos/owner/repo/pulls/7/reviews":
			fmt.Fprint(w, `[
				{"id":1,"user":{"login":"cy"},"state":"APPROVED"},
				{"id":2,"user":{"login":"di"},"state":"CHANGES_REQUESTED"},
				{"id":3,"user":{"login":"di"},"state":"APPROVED"},
				{"id":4,"user":{"login":"ed"},"state":"APPROVED"},
				{"id":5,"user":{"login":"ed"},"state":"CHANGES_REQUESTED"},
				{"id":6,"user":{"login":"fy"},"state":"COMMENTED"}]`)
		case "/graphql":
			var body struct {
				Query     string
				Variables map[string]string
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if r.Header.Get("Authorization") != "Bearer installation" {
				t.Errorf("behind-by is a read and must use the read token, got %q", r.Header.Get("Authorization"))
			}
			if v := body.Variables; v["owner"] != "owner" || v["name"] != "repo" || v["base"] != "refs/heads/release/1" || v["head"] != "abc" {
				t.Errorf("variables = %v", v)
			}
			if compareStatus != http.StatusOK {
				w.WriteHeader(compareStatus)
				fmt.Fprint(w, `{"message":"nope"}`)
				return
			}
			fmt.Fprint(w, `{"data":{"repository":{"ref":{"compare":{"behindBy":5}}}}}`)
		default:
			fmt.Fprint(w, `[]`)
		}
	}))
}

func TestPullRequestDetailCarriesEnrichments(t *testing.T) {
	server := pullRequestServer(t, "open", http.StatusOK)
	defer server.Close()
	c := New(testTokens{})
	c.BaseURL = server.URL
	d, err := c.PullRequest(context.Background(), "owner/repo", 7)
	if err != nil {
		t.Fatal(err)
	}
	if d.Additions != 120 || d.Deletions != 30 || d.ChangedFiles != 9 {
		t.Fatalf("totals = %d/%d/%d", d.Additions, d.Deletions, d.ChangedFiles)
	}
	if len(d.RequestedReviewers) != 3 || d.RequestedReviewers[0] != "ana" || d.RequestedReviewers[1] != "bo" || d.RequestedReviewers[2] != "core" {
		t.Fatalf("requested reviewers = %v", d.RequestedReviewers)
	}
	if d.ReviewSummary != (gh.ReviewSummary{Approvals: 2, ChangesRequested: 1}) {
		t.Fatalf("review summary = %+v", d.ReviewSummary)
	}
	if d.BehindBy == nil || *d.BehindBy != 5 {
		t.Fatalf("behind by = %v", d.BehindBy)
	}
}

func TestPullRequestDetailOmitsWhatItCannotKnow(t *testing.T) {
	failing := pullRequestServer(t, "open", http.StatusNotFound)
	defer failing.Close()
	c := New(testTokens{})
	c.BaseURL = failing.URL
	d, err := c.PullRequest(context.Background(), "owner/repo", 7)
	if err != nil || d.BehindBy != nil {
		t.Fatalf("failed comparison must not fail the pull request: %v %v", d.BehindBy, err)
	}
	out, _ := json.Marshal(d)
	if strings.Contains(string(out), "behind_by") {
		t.Fatalf("behind_by should be omitted: %s", out)
	}

	inner := pullRequestServer(t, "open", http.StatusOK)
	defer inner.Close()
	graphqlError := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/graphql" {
			fmt.Fprint(w, `{"data":{"repository":{"ref":null}},"errors":[{"message":"Could not resolve"}]}`)
			return
		}
		inner.Config.Handler.ServeHTTP(w, r)
	}))
	defer graphqlError.Close()
	c.BaseURL = graphqlError.URL
	if d, err = c.PullRequest(context.Background(), "owner/repo", 7); err != nil || d.BehindBy != nil {
		t.Fatalf("a GraphQL error must hide the row, not fail: %v %v", d.BehindBy, err)
	}

	closed := pullRequestServer(t, "closed", http.StatusOK)
	defer closed.Close()
	c.BaseURL = closed.URL
	if d, err = c.PullRequest(context.Background(), "owner/repo", 7); err != nil || d.BehindBy != nil {
		t.Fatalf("closed pull requests are not compared: %v %v", d.BehindBy, err)
	}
}

func TestChecksCarryTimingWhenGitHubReportsIt(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/statuses") {
			fmt.Fprint(w, `[{"id":9,"context":"legacy","state":"success"}]`)
			return
		}
		fmt.Fprint(w, `{"check_runs":[
			{"id":1,"name":"done","status":"completed","conclusion":"success","started_at":"2026-01-01T10:00:00Z","completed_at":"2026-01-01T10:02:08Z"},
			{"id":2,"name":"going","status":"in_progress","started_at":"2026-01-01T10:00:00Z","completed_at":null},
			{"id":3,"name":"queued","status":"queued"}]}`)
	}))
	defer server.Close()
	c := New(testTokens{})
	c.BaseURL = server.URL
	checks, err := c.Checks(context.Background(), "owner/repo", "abc")
	if err != nil || len(checks) != 4 {
		t.Fatal(checks, err)
	}
	if got := checks[0].CompletedAt.Sub(*checks[0].StartedAt); got != 128*time.Second {
		t.Fatalf("duration = %v", got)
	}
	if checks[1].StartedAt == nil || checks[1].CompletedAt != nil {
		t.Fatalf("running check = %+v", checks[1])
	}
	if checks[2].StartedAt != nil || checks[3].StartedAt != nil {
		t.Fatalf("untimed checks = %+v %+v", checks[2], checks[3])
	}
	out, _ := json.Marshal(checks[3])
	if strings.Contains(string(out), "_at") {
		t.Fatalf("timing keys should be omitted: %s", out)
	}
}
