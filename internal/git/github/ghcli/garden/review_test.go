package garden

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	core "github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/connect"
	gardensync "github.com/Tiago-0liveira/bonsai/internal/core/garden/sync"
)

func (f *fakeTransport) paths(prefix string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, r := range f.requests {
		if strings.HasPrefix(r.URL.Path, prefix) {
			n++
		}
	}
	return n
}
func (f *fakeTransport) writes() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, r := range f.requests {
		if r.Method != http.MethodGet && r.URL.Path != "/graphql" {
			n++
		}
	}
	return n
}

func TestGetIssueRejectsPullRequestsAndTreatsNullAsAbsent(t *testing.T) {
	for _, tc := range []struct {
		name, pr string
		wantErr  bool
	}{{"object", `{"url":"x"}`, true}, {"empty object", `{}`, true}, {"null", `null`, false}} {
		t.Run(tc.name, func(t *testing.T) {
			a, _ := fixture(t, func(*http.Request) (*http.Response, error) {
				return response(200, `{"id":1,"number":1,"user":{"id":5},"pull_request":`+tc.pr+`}`, ""), nil
			})
			_, err := a.Reader().GetIssue(context.Background(), 9, 1)
			if tc.wantErr != (err != nil) {
				t.Fatalf("err=%v", err)
			}
			if tc.wantErr {
				if !errors.Is(err, core.ErrNotFound) {
					t.Fatalf("PR must read as not found: %v", err)
				}
				if a.issueNumbers[9][1] != 0 {
					t.Fatal("PR mapping must be evicted from the number cache")
				}
			}
		})
	}
}

func TestPullRequestNullIsNotFilteredFromListing(t *testing.T) {
	a, _ := fixture(t, func(*http.Request) (*http.Response, error) {
		return response(200, `[{"id":11,"number":1,"user":{"id":7},"pull_request":null,"labels":[{"name":"bonsai:managed"}]}]`, ""), nil
	})
	p, err := a.Reader().ListManagedIssues(context.Background(), 9, "")
	if err != nil || len(p.Items) != 1 {
		t.Fatalf("page=%+v err=%v", p, err)
	}
}

func TestGetIssueRejectsIdentityMismatchAndEvictsCache(t *testing.T) {
	a, _ := fixture(t, func(*http.Request) (*http.Response, error) {
		return response(200, `{"id":2,"number":1,"user":{"id":5}}`, ""), nil
	})
	if _, err := a.Reader().GetIssue(context.Background(), 9, 1); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("mismatched ID accepted: %v", err)
	}
	if a.issueNumbers[9][1] != 0 {
		t.Fatal("stale number mapping kept after mismatch")
	}
}

func TestGetIssueMissingAuthorKeepsIssueWithMarker(t *testing.T) {
	a, _ := fixture(t, func(*http.Request) (*http.Response, error) {
		return response(200, `{"id":1,"number":1,"user":null,"labels":[{"name":"bonsai:managed"}]}`, ""), nil
	})
	got, err := a.Reader().GetIssue(context.Background(), 9, 1)
	if err != nil || got.AuthorID != "" || len(got.Problems) != 1 || got.Problems[0] != core.IssueProblemAuthor {
		t.Fatalf("issue=%+v err=%v", got, err)
	}
}

func TestPullRequestIDNeverResolvesToANumberOrWrites(t *testing.T) {
	a, f := fixture(t, func(r *http.Request) (*http.Response, error) {
		return response(200, `[{"id":50,"number":5,"pull_request":{},"user":{"id":1}},{"id":51,"number":6,"pull_request":null,"user":{"id":1}}]`, ""), nil
	})
	ctx := context.Background()
	if _, err := a.Writer().AppendComment(ctx, 9, 50, "x"); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("comment on PR ID: %v", err)
	}
	if err := a.Writer().UpdateLabels(ctx, 9, 50, []string{"bonsai:managed"}); !errors.Is(err, core.ErrNotFound) {
		t.Fatalf("label write on PR ID: %v", err)
	}
	if f.writes() != 0 {
		t.Fatal("write reached the transport for a pull request")
	}
	if a.issueNumbers[9][50] != 0 || a.issueNumbers[9][51] != 6 {
		t.Fatalf("number cache: %+v", a.issueNumbers[9])
	}
}

func TestUpdateLabelsRefusesPullRequestRow(t *testing.T) {
	a, f := fixture(t, func(r *http.Request) (*http.Response, error) {
		return response(200, `{"id":1,"number":1,"user":{"id":5},"pull_request":{},"labels":[]}`, ""), nil
	})
	if err := a.Writer().UpdateLabels(context.Background(), 9, 1, []string{"bonsai:managed"}); err == nil {
		t.Fatal("PR labels updated")
	}
	if f.writes() != 0 {
		t.Fatal("write reached the transport for a pull request")
	}
}

func commentPages(total int, lastNoNext bool) func(*http.Request) (*http.Response, error) {
	return func(r *http.Request) (*http.Response, error) {
		page := 1
		fmt.Sscanf(r.URL.Query().Get("page"), "%d", &page)
		var b strings.Builder
		b.WriteString("[")
		start := (page - 1) * 100
		for i := start; i < start+100 && i < total; i++ {
			if i > start {
				b.WriteString(",")
			}
			fmt.Fprintf(&b, `{"id":%d,"body":"c","user":{"id":4,"login":"a"},"created_at":"2024-01-01T00:00:00Z"}`, i+1)
		}
		b.WriteString("]")
		link := ""
		if !(lastNoNext && start+100 >= total) {
			link = fmt.Sprintf(`<https://api.github.test/repos/acme/repo/issues/1/comments?per_page=100&page=%d>; rel="next"`, page+1)
		}
		return response(200, b.String(), link), nil
	}
}

func TestListCommentsHardCap(t *testing.T) {
	a, f := fixture(t, commentPages(100000, false))
	got, err := a.Reader().ListComments(context.Background(), 9, 1)
	if !errors.Is(err, core.ErrTooManyComments) || len(got) != core.MaxComments+1 {
		t.Fatalf("len=%d err=%v", len(got), err)
	}
	if n := f.paths("/repos/acme/repo/issues/1/comments"); n != 21 {
		t.Fatalf("requests=%d, listing must stop as soon as the cap is exceeded", n)
	}
	a, _ = fixture(t, commentPages(core.MaxComments, true))
	got, err = a.Reader().ListComments(context.Background(), 9, 1)
	if err != nil || len(got) != core.MaxComments {
		t.Fatalf("exactly at the cap must succeed: len=%d err=%v", len(got), err)
	}
}

func TestNextLinkMustAdvanceExactlyOnePage(t *testing.T) {
	const base = "https://api.github.test"
	const path = "/repos/acme/repo/issues/1/comments"
	for _, tc := range []struct {
		name    string
		page    int
		current int
		ok      bool
	}{{"next", 3, 2, true}, {"same page", 2, 2, false}, {"backwards", 1, 2, false}, {"skip", 4, 2, false}} {
		t.Run(tc.name, func(t *testing.T) {
			h := fmt.Sprintf(`<%s%s?per_page=100&page=%d>; rel="next"`, base, path, tc.page)
			got, err := nextLinkPath(h, base, path, tc.current)
			if tc.ok != (err == nil && got != "") {
				t.Fatalf("got=%q err=%v", got, err)
			}
		})
	}
}

func TestListCommentsRejectsLoopingLink(t *testing.T) {
	a, f := fixture(t, func(r *http.Request) (*http.Response, error) {
		return response(200, `[]`, `<https://api.github.test/repos/acme/repo/issues/1/comments?per_page=100&page=1>; rel="next"`), nil
	})
	if _, err := a.Reader().ListComments(context.Background(), 9, 1); !errors.Is(err, core.ErrInvalidCursor) {
		t.Fatalf("looping link accepted: %v", err)
	}
	if n := f.paths("/repos/acme/repo/issues/1/comments"); n != 1 {
		t.Fatalf("requests=%d", n)
	}
}

func TestListManagedIssuesRejectsRepeatedPage(t *testing.T) {
	a, _ := fixture(t, func(r *http.Request) (*http.Response, error) {
		return response(200, `[]`, `<https://api.github.test/repos/acme/repo/issues?state=all&per_page=100&labels=bonsai%3Amanaged&page=2>; rel="next"`), nil
	})
	p, err := a.Reader().ListManagedIssues(context.Background(), 9, "")
	if err != nil || p.NextCursor == "" {
		t.Fatalf("page=%+v err=%v", p, err)
	}
	// The server repeats page 2 while on page 2: a loop, not progress.
	if _, err := a.Reader().ListManagedIssues(context.Background(), 9, p.NextCursor); !errors.Is(err, core.ErrInvalidCursor) {
		t.Fatalf("repeated page accepted: %v", err)
	}
}

func graphqlBody(t *testing.T, r *http.Request) (string, map[string]any) {
	t.Helper()
	raw, _ := io.ReadAll(r.Body)
	var req struct {
		Query     string         `json:"query"`
		Variables map[string]any `json:"variables"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		t.Fatal(err)
	}
	return req.Query, req.Variables
}

func TestListProjectsPaginatesAndFailsClosed(t *testing.T) {
	node := func(id, title string, fieldsMore bool) string {
		return fmt.Sprintf(`{"id":%q,"title":%q,"number":1,"fields":{"pageInfo":{"hasNextPage":%v},"nodes":[]}}`, id, title, fieldsMore)
	}
	envelope := func(more bool, cursor string, nodes ...string) string {
		return fmt.Sprintf(`{"data":{"repositoryOwner":{"login":"acme","projectsV2":{"pageInfo":{"hasNextPage":%v,"endCursor":%q},"nodes":[%s]}}}}`, more, cursor, strings.Join(nodes, ","))
	}
	t.Run("two pages", func(t *testing.T) {
		var afters []any
		a, _ := fixture(t, func(r *http.Request) (*http.Response, error) {
			_, vars := graphqlBody(t, r)
			afters = append(afters, vars["after"])
			if vars["after"] == nil {
				return response(200, envelope(true, "c1", node("P1", "One", false)), ""), nil
			}
			return response(200, envelope(false, "", node("P2", "Garden", false)), ""), nil
		})
		ps, err := a.ListProjects(context.Background(), 9)
		if err != nil || len(ps) != 2 || ps[1].Name != "Garden" || len(afters) != 2 || afters[1] != "c1" {
			t.Fatalf("ps=%+v afters=%v err=%v", ps, afters, err)
		}
	})
	t.Run("never ending list is truncated", func(t *testing.T) {
		calls := 0
		a, _ := fixture(t, func(r *http.Request) (*http.Response, error) {
			calls++
			return response(200, envelope(true, fmt.Sprint("c", calls), node("P", "One", false)), ""), nil
		})
		if _, err := a.ListProjects(context.Background(), 9); !errors.Is(err, ErrTruncated) || calls != maxProjectPages {
			t.Fatalf("err=%v calls=%d", err, calls)
		}
	})
	t.Run("missing cursor", func(t *testing.T) {
		a, _ := fixture(t, func(*http.Request) (*http.Response, error) {
			return response(200, envelope(true, "", node("P", "One", false)), ""), nil
		})
		if _, err := a.ListProjects(context.Background(), 9); !errors.Is(err, ErrTruncated) {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("truncated fields", func(t *testing.T) {
		a, _ := fixture(t, func(*http.Request) (*http.Response, error) {
			return response(200, envelope(false, "", node("P", "One", true)), ""), nil
		})
		if _, err := a.ListProjects(context.Background(), 9); !errors.Is(err, ErrTruncated) {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("ensure never creates from a truncated list", func(t *testing.T) {
		creates := 0
		a, _ := fixture(t, func(r *http.Request) (*http.Response, error) {
			q, _ := graphqlBody(t, r)
			if strings.Contains(q, "GardenCreateProject") {
				creates++
			}
			return response(200, envelope(true, "", node("P", "One", false)), ""), nil
		})
		if _, err := a.EnsureProject(context.Background(), 9, "Garden"); err == nil || creates != 0 {
			t.Fatalf("err=%v creates=%d", err, creates)
		}
	})
}

func TestBareForbiddenIsNeverPermissionDeniedOrRateLimit(t *testing.T) {
	a, _ := fixture(t, func(*http.Request) (*http.Response, error) { return response(403, `{"message":"x"}`, ""), nil })
	_, err := a.Reader().GetIssue(context.Background(), 9, 1)
	if !errors.Is(err, core.ErrForbidden) || errors.Is(err, core.ErrPermissionDenied) || errors.Is(err, connect.ErrPermissionDenied) || errors.Is(err, core.ErrRateLimited) {
		t.Fatalf("err=%v", err)
	}
}

func TestRepositoryLevelDenialMeansPermissionDenied(t *testing.T) {
	for _, status := range []int{403, 404} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			a, _ := fixture(t, func(*http.Request) (*http.Response, error) { return response(200, `[]`, ""), nil })
			now := time.Now()
			a.now = func() time.Time { return now }
			a.rememberRepository(repoJSON{ID: 9, FullName: "acme/repo"})
			now = now.Add(2 * repoCacheTTL)
			a.client.Transport.(*fakeTransport).fn = func(*http.Request) (*http.Response, error) { return response(status, `{}`, ""), nil }
			_, err := a.Reader().ListManagedIssues(context.Background(), 9, "")
			if !errors.Is(err, core.ErrPermissionDenied) {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func TestAuthAndRateLimitErrorsCarryBothVocabularies(t *testing.T) {
	a, _ := fixture(t, func(*http.Request) (*http.Response, error) { return response(401, `{}`, ""), nil })
	_, err := a.Reader().GetIssue(context.Background(), 9, 1)
	if !errors.Is(err, connect.ErrAuthRequired) || !errors.Is(err, core.ErrAuthRequired) {
		t.Fatalf("401: %v", err)
	}
	now := time.Unix(1_700_000_000, 0)
	for _, tc := range []struct {
		name   string
		status int
		h      http.Header
		want   time.Duration
	}{
		{"429 seconds", 429, http.Header{"Retry-After": {"7"}}, 7 * time.Second},
		{"403 seconds", 403, http.Header{"Retry-After": {"11"}}, 11 * time.Second},
		{"403 reset", 403, http.Header{"X-Ratelimit-Remaining": {"0"}, "X-Ratelimit-Reset": {fmt.Sprint(now.Add(90 * time.Second).Unix())}}, 90 * time.Second},
		{"429 no hint", 429, nil, 0},
		{"429 date", 429, http.Header{"Retry-After": {now.Add(30 * time.Second).UTC().Format(http.TimeFormat)}}, 30 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, _ := fixture(t, func(*http.Request) (*http.Response, error) {
				r := response(tc.status, `{}`, "")
				for k, v := range tc.h {
					r.Header[k] = v
				}
				return r, nil
			})
			a.now = func() time.Time { return now }
			_, err := a.Reader().GetIssue(context.Background(), 9, 1)
			var rl core.RateLimitError
			if !errors.Is(err, connect.ErrRateLimited) || !errors.Is(err, core.ErrRateLimited) || !errors.As(err, &rl) || rl.RetryAfter != tc.want {
				t.Fatalf("err=%v rl=%+v", err, rl)
			}
		})
	}
}

func TestSyncReportsRateLimitHintFromAdapterError(t *testing.T) {
	a, _ := fixture(t, func(*http.Request) (*http.Response, error) {
		r := response(429, `{}`, "")
		r.Header.Set("Retry-After", "120")
		return r, nil
	})
	local, _ := core.NewLocalRepoID("l")
	snap := gardensync.Load(context.Background(), gardensync.Request{GardenID: core.GardenID{Repo: 9}, LocalRepoID: local, Generation: 1, Epoch: 1, Issues: a.Reader(), Limits: gardensync.Limits{PageCap: 2, ItemCap: 5, CallTimeout: time.Second}})
	st := gardensync.NewStore(core.GardenID{Repo: 9}, local)
	if err := st.Apply(snap); err != nil {
		t.Fatal(err)
	}
	status := gardensync.Status(gardensync.AssociationView{State: gardensync.AssociationConnected}, st.Read())
	if status.State != "rate-limited" || status.RetryAfter != 120*time.Second {
		t.Fatalf("status=%+v", status)
	}
}

func TestDeletedAuthorDoesNotBlockTheGarden(t *testing.T) {
	a, _ := fixture(t, func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/comments") {
			return response(200, `[{"id":3,"body":"hi","user":null,"created_at":"2024-01-01T00:00:00Z"},{"id":4,"body":"hi","user":{"id":0,"login":"ghost"},"created_at":"2024-01-01T00:00:00Z"}]`, ""), nil
		}
		return response(200, `[
			{"id":0,"number":1,"user":{"id":1},"labels":[{"name":"bonsai:managed"}]},
			{"id":12,"number":0,"user":{"id":1},"labels":[{"name":"bonsai:managed"}]},
			{"id":13,"number":3,"user":{"id":0,"login":"ghost"},"labels":[{"name":"bonsai:managed"}]},
			{"id":14,"number":4,"user":{"id":9},"labels":[{"name":"bonsai:managed"}]}]`, ""), nil
	})
	p, err := a.Reader().ListManagedIssues(context.Background(), 9, "")
	if err != nil || !p.Complete || len(p.Items) != 4 {
		t.Fatalf("page=%+v err=%v", p, err)
	}
	if p.Items[0].ID != 0 || p.Items[1].ID != 0 || p.Items[2].ID != 13 || len(p.Items[2].Problems) != 1 || p.Items[3].AuthorID != "9" {
		t.Fatalf("items=%+v", p.Items)
	}
	comments, err := a.Reader().ListComments(context.Background(), 9, 13)
	if err != nil || comments[0].AuthorID != "" || comments[1].AuthorID != "" {
		t.Fatalf("comment authors must be empty (untrusted): %+v err=%v", comments, err)
	}
	local, _ := core.NewLocalRepoID("l")
	snap := gardensync.Load(context.Background(), gardensync.Request{GardenID: core.GardenID{Repo: 9}, LocalRepoID: local, Issues: a.Reader(), Limits: gardensync.Limits{PageCap: 2, ItemCap: 10, CallTimeout: time.Second}})
	if !snap.Complete {
		t.Fatalf("one bad row must not make the Garden incomplete: %+v", snap.Problems)
	}
	var attention int
	for _, it := range snap.Items {
		if it.Lane.Lane == core.LaneAttention {
			attention++
		}
	}
	if attention != len(snap.Items) {
		t.Fatalf("malformed rows must be Attention: %+v", snap.Items)
	}
}

func TestRepositoryPathIsCachedWithTTL(t *testing.T) {
	a, f := fixture(t, func(*http.Request) (*http.Response, error) { return response(200, `[]`, ""), nil })
	now := time.Now()
	a.now = func() time.Time { return now }
	a.rememberRepository(repoJSON{ID: 9, FullName: "acme/repo", NodeID: "R", Owner: struct {
		Login  string `json:"login"`
		NodeID string `json:"node_id"`
	}{NodeID: "O"}})
	for i := 0; i < 3; i++ {
		if _, err := a.Reader().ListManagedIssues(context.Background(), 9, ""); err != nil {
			t.Fatal(err)
		}
	}
	if n := f.paths("/repos/acme/repo/issues"); n != 3 {
		t.Fatalf("list calls=%d", n)
	}
	if n := f.requestsTo("/repos/acme/repo"); n != 0 {
		t.Fatalf("verified repository re-fetched %d times within TTL", n)
	}
	now = now.Add(repoCacheTTL + time.Second)
	if _, err := a.Reader().ListManagedIssues(context.Background(), 9, ""); err != nil {
		t.Fatal(err)
	}
	if n := f.requestsTo("/repos/acme/repo"); n != 1 {
		t.Fatalf("expired entry must be re-verified exactly once, got %d", n)
	}
}

func (f *fakeTransport) requestsTo(path string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, r := range f.requests {
		if r.URL.Path == path {
			n++
		}
	}
	return n
}

func TestSeedAllowsWarmStartAndStillVerifiesIdentity(t *testing.T) {
	newAdapter := func(returnedID int64) (*Adapter, *fakeTransport) {
		f := &fakeTransport{}
		f.fn = func(r *http.Request) (*http.Response, error) {
			if r.URL.Path == "/repos/acme/repo" {
				return response(200, fmt.Sprintf(`{"id":%d,"full_name":"acme/repo"}`, returnedID), ""), nil
			}
			return response(200, `[]`, ""), nil
		}
		a, err := New(&http.Client{Transport: f}, "https://api.github.test")
		if err != nil {
			t.Fatal(err)
		}
		return a, f
	}
	a, f := newAdapter(9)
	if _, err := a.Reader().ListManagedIssues(context.Background(), 9, ""); !errors.Is(err, connect.ErrNoGitHubRemote) {
		t.Fatalf("unseeded adapter: %v", err)
	}
	if err := a.Seed(9, "acme", "repo"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Reader().ListManagedIssues(context.Background(), 9, ""); err != nil {
		t.Fatalf("seeded warm start: %v", err)
	}
	if f.requestsTo("/repos/acme/repo") != 1 {
		t.Fatal("a seeded entry must be verified on first use")
	}
	a, _ = newAdapter(77)
	_ = a.Seed(9, "acme", "repo")
	if _, err := a.Reader().ListManagedIssues(context.Background(), 9, ""); !errors.Is(err, connect.ErrRepositoryIdentityChanged) {
		t.Fatalf("identity change not detected: %v", err)
	}
	for _, bad := range [][2]string{{"", "r"}, {"o", ""}, {"..", "r"}, {"o", "."}, {"o/x", "r"}} {
		if err := a.Seed(5, bad[0], bad[1]); err == nil {
			t.Fatalf("Seed(%q,%q) accepted", bad[0], bad[1])
		}
	}
	if err := a.Seed(0, "o", "r"); err == nil {
		t.Fatal("zero ID accepted")
	}
}

func TestSeedNeverReplacesVerifiedObservation(t *testing.T) {
	a, _ := fixture(t, func(*http.Request) (*http.Response, error) { return response(200, `[]`, ""), nil })
	if err := a.Seed(9, "other", "name"); err != nil {
		t.Fatal(err)
	}
	if got, _ := a.repository(9); got.FullName != "acme/repo" || got.verified.IsZero() {
		t.Fatalf("verified entry replaced: %+v", got)
	}
}

func TestParseLinksHandlesCommasAndQuotes(t *testing.T) {
	h := `<https://api.github.test/a?x=1,2>; rel="prev"; title="a, b; c", <https://api.github.test/b?page=2>; rel="next last"`
	got, err := parseLinks(h)
	if err != nil || len(got) != 2 || got[0].target != "https://api.github.test/a?x=1,2" || !reflect.DeepEqual(got[1].rels, []string{"next", "last"}) {
		t.Fatalf("links=%+v err=%v", got, err)
	}
	for _, bad := range []string{`<https://x`, `https://x; rel="next"`, `<https://x>; rel="next`, `<https://x> rel=next`} {
		if _, err := parseLinks(bad); err == nil {
			t.Errorf("accepted malformed %q", bad)
		}
	}
	if _, err := nextLinkPath(`<https://api.github.test/p?page=2>; rel="next", <https://api.github.test/p?page=2>; rel="next"`, "https://api.github.test", "/p", 1); err == nil {
		t.Error("two next links accepted")
	}
	if got, err := nextLinkPath(``, "https://api.github.test", "/p", 1); got != "" || err != nil {
		t.Errorf("empty header: %q %v", got, err)
	}
}

func TestRemoteRepoRejectsDotSegments(t *testing.T) {
	for _, raw := range []string{"https://github.com/../x", "https://github.com/x/..", "https://github.com/./x", "git@github.com:../x.git", "https://github.com/x/..git"} {
		if _, _, err := remoteRepo(raw); err == nil {
			t.Errorf("accepted %q", raw)
		}
	}
	if o, n, err := remoteRepo("git@github.com:acme/repo.git"); err != nil || o != "acme" || n != "repo" {
		t.Fatalf("valid scp remote rejected: %v", err)
	}
	a, f := fixture(t, func(*http.Request) (*http.Response, error) { return response(200, `{}`, ""), nil })
	for _, name := range []string{"../etc", "a/..", "a/b/c"} {
		if _, err := (RepositoryResolver{Adapter: a}).ResolveRepositoryID(context.Background(), 9, name); !errors.Is(err, connect.ErrNoGitHubRemote) {
			t.Errorf("%q: %v", name, err)
		}
	}
	if f.paths("/") != 0 {
		t.Fatal("invalid names reached the transport")
	}
}

func TestConstructorsFailClosed(t *testing.T) {
	if _, err := New(nil, "https://api.github.com"); !errors.Is(err, ErrNilHTTPClient) {
		t.Fatalf("nil client: %v", err)
	}
	for _, base := range []string{"https://ghe.example.com/api/v3", "http://api.github.com", "https://api.github.com/x", "https://u:p@api.github.com", "api.github.com", "https://api.github.com?x=1"} {
		if _, err := New(http.DefaultClient, base); !errors.Is(err, ErrUnsupportedBase) {
			t.Errorf("%q accepted: %v", base, err)
		}
	}
	if a, err := New(http.DefaultClient, ""); err != nil || a.baseURL != defaultBase {
		t.Fatalf("default base: %v", err)
	}
	if a, err := FromService(nil); a != nil || err == nil {
		t.Fatalf("FromService(nil) must return an error, not a nil adapter: %v", err)
	}
}

func TestReaderAndWriterExposeOnlyTheirPorts(t *testing.T) {
	if n := reflect.TypeOf(&IssueReader{}).NumMethod(); n != 4 {
		t.Fatalf("reader exposes %d methods", n)
	}
	if n := reflect.TypeOf(&IssueWriter{}).NumMethod(); n != 2 {
		t.Fatalf("writer exposes %d methods", n)
	}
}

func TestOversizedCommentIsATypedError(t *testing.T) {
	a, _ := fixture(t, func(*http.Request) (*http.Response, error) { return response(201, `{"id":1}`, ""), nil })
	_, err := a.Writer().AppendComment(context.Background(), 9, 1, strings.Repeat("x", maxComment+1))
	if !errors.Is(err, core.ErrCommentTooLarge) {
		t.Fatalf("err=%v", err)
	}
}

func TestGraphQLErrorTypesMapToTypedErrors(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		header     http.Header
		want       error
		notOffline bool
	}{
		{"rate", `{"errors":[{"type":"RATE_LIMITED"}]}`, nil, core.ErrRateLimited, true},
		{"not found", `{"errors":[{"type":"NOT_FOUND"}]}`, nil, core.ErrNotFound, true},
		{"forbidden", `{"errors":[{"type":"FORBIDDEN"}]}`, nil, core.ErrForbidden, true},
		{"reserved field", `{"errors":[{"type":"UNPROCESSABLE","message":"Name cannot have a reserved value, Name has already been taken"}]}`, nil, connect.ErrGitHubValidation, true},
		{"scopes", `{"errors":[{"type":"INSUFFICIENT_SCOPES"}]}`, nil, nil, true},
		{"unknown", `{"errors":[{"type":"SOMETHING_NEW"}]}`, nil, core.ErrGraphQL, true},
		{"untyped", `{"errors":[{"message":"x"}]}`, nil, core.ErrGraphQL, true},
		{"data null", `{"data":null}`, nil, core.ErrGraphQL, true},
		{"mixed prefers specific", `{"errors":[{"type":"SOMETHING_NEW"},{"type":"RATE_LIMITED"}]}`, nil, core.ErrRateLimited, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, _ := fixture(t, func(*http.Request) (*http.Response, error) {
				r := response(200, tc.body, "")
				for k, v := range tc.header {
					r.Header[k] = v
				}
				return r, nil
			})
			_, err := a.ListProjects(context.Background(), 9)
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("err=%v want %v", err, tc.want)
			}
			if tc.name == "scopes" {
				var miss connect.ErrScopeMissing
				if !errors.As(err, &miss) {
					t.Fatalf("err=%v", err)
				}
			}
			if errors.Is(err, connect.ErrOffline) {
				t.Fatalf("GraphQL error mapped to offline: %v", err)
			}
		})
	}
}

func TestListProjectsReadsRepositoryOwnerNotViewer(t *testing.T) {
	node := `{"id":"P1","title":"Garden","number":3,"creator":{"login":"viewer-user"},"fields":{"pageInfo":{"hasNextPage":false},"nodes":[]}}`
	list := func(t *testing.T, body string) ([]core.Project, error, map[string]any) {
		var vars map[string]any
		a, _ := fixture(t, func(r *http.Request) (*http.Response, error) {
			_, vars = graphqlBody(t, r)
			return response(200, body, ""), nil
		})
		ps, err := a.ListProjects(context.Background(), 9)
		return ps, err, vars
	}
	t.Run("owner login comes from the repository", func(t *testing.T) {
		ps, err, vars := list(t, `{"data":{"repositoryOwner":{"login":"ACME","projectsV2":{"pageInfo":{"hasNextPage":false},"nodes":[`+node+`]}}}}`)
		if err != nil || len(ps) != 1 || ps[0].Owner != "acme" || ps[0].CreatorLogin != "viewer-user" || vars["owner"] != "acme" {
			t.Fatalf("ps=%+v vars=%v err=%v", ps, vars, err)
		}
	})
	t.Run("foreign owner in response fails closed", func(t *testing.T) {
		ps, err, _ := list(t, `{"data":{"repositoryOwner":{"login":"someone-else","projectsV2":{"pageInfo":{"hasNextPage":false},"nodes":[`+node+`]}}}}`)
		if !errors.Is(err, core.ErrGraphQL) || ps != nil {
			t.Fatalf("ps=%+v err=%v", ps, err)
		}
	})
	t.Run("missing owner is not found", func(t *testing.T) {
		if _, err, _ := list(t, `{"data":{"repositoryOwner":null}}`); !errors.Is(err, core.ErrNotFound) {
			t.Fatalf("err=%v", err)
		}
	})
}
