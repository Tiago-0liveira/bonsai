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
	"testing"
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
