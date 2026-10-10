package ghcli

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/trace"
	domain "github.com/Tiago-0liveira/bonsai/internal/git"
	gh "github.com/Tiago-0liveira/bonsai/internal/git/github"
)

// fakeGitHub serves a few REST resources with strong ETags and records the
// status of every response.
type fakeGitHub struct {
	mu        sync.Mutex
	statuses  []int
	auth      []string
	validAuth string
	remaining int
	bodies    map[string]string
}

func (f *fakeGitHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.auth = append(f.auth, r.Header.Get("Authorization"))
	w.Header().Set("X-RateLimit-Limit", "5000")
	w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(f.remaining))
	w.Header().Set("X-RateLimit-Reset", "2000000000")
	w.Header().Set("X-RateLimit-Resource", "core")
	if f.validAuth != "" && r.Header.Get("Authorization") != f.validAuth {
		f.statuses = append(f.statuses, http.StatusUnauthorized)
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"Bad credentials"}`))
		return
	}
	body, ok := f.bodies[r.URL.Path]
	if !ok {
		f.statuses = append(f.statuses, http.StatusNotFound)
		http.NotFound(w, r)
		return
	}
	etag := fmt.Sprintf(`"%x"`, sha256.Sum256([]byte(body)))
	if r.Header.Get("If-None-Match") == etag {
		f.statuses = append(f.statuses, http.StatusNotModified)
		w.WriteHeader(http.StatusNotModified)
		return
	}
	f.statuses = append(f.statuses, http.StatusOK)
	w.Header().Set("ETag", etag)
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(body))
}

func (f *fakeGitHub) take() (statuses []int, auth []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	statuses, auth = f.statuses, f.auth
	f.statuses, f.auth = nil, nil
	return statuses, auth
}

func newFakeShared(t *testing.T, f *fakeGitHub, tokens *fakeTokens) (*Shared, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(f)
	t.Cleanup(server.Close)
	shared := NewShared()
	shared.SetAPIBase(DefaultHost, server.URL)
	shared.SetTokenRunner(tokens.run)
	return shared, server
}

func TestServiceSendsTheGhTokenAndRevalidatesWithETags(t *testing.T) {
	f := &fakeGitHub{remaining: 4999, bodies: map[string]string{
		"/repos/acme/repo":       `{"id":1,"full_name":"acme/repo","default_branch":"main"}`,
		"/repos/acme/repo/pulls": `[{"number":7,"title":"t","state":"open","head":{"ref":"feature","sha":"abc"}}]`,
	}}
	tokens := &fakeTokens{tokens: []string{"tok-1"}}
	shared, _ := newFakeShared(t, f, tokens)
	service := shared.Service(t.TempDir())

	requests0, notModified0 := trace.HTTPCounts()
	for round := 0; round < 2; round++ {
		repo, err := service.Repository(context.Background(), "acme/repo")
		if err != nil || repo.FullName != "acme/repo" {
			t.Fatalf("round %d: %+v %v", round, repo, err)
		}
		page, err := service.PullRequestPage(context.Background(), "acme/repo", gh.PRFilter{State: "open"}, 1)
		if err != nil || len(page.Items) != 1 || page.Items[0].Head != "feature" {
			t.Fatalf("round %d: %+v %v", round, page, err)
		}
		if page.NotModified != (round == 1) {
			t.Fatalf("round %d: NotModified = %v", round, page.NotModified)
		}
	}
	statuses, auth := f.take()
	if want := []int{200, 200, 304, 304}; !equalInts(statuses, want) {
		t.Fatalf("statuses = %v, want %v", statuses, want)
	}
	for _, header := range auth {
		if header != "Bearer tok-1" {
			t.Fatalf("Authorization = %q", header)
		}
	}
	requests1, notModified1 := trace.HTTPCounts()
	if requests1-requests0 != 4 || notModified1-notModified0 != 2 {
		t.Fatalf("trace counts: requests %d, 304s %d", requests1-requests0, notModified1-notModified0)
	}
	if got := tokens.calls.Load(); got != 1 {
		t.Fatalf("gh auth token ran %d times", got)
	}
	if rate, ok := service.RateLimit(); !ok || rate.Remaining != 4999 || rate.Limit != 5000 || rate.Low() {
		t.Fatalf("rate = %+v %v", rate, ok)
	}
}

func TestUnauthorizedReReadsTheTokenOnce(t *testing.T) {
	f := &fakeGitHub{remaining: 4999, validAuth: "Bearer tok-2", bodies: map[string]string{
		"/repos/acme/repo": `{"id":1,"full_name":"acme/repo","default_branch":"main"}`,
	}}
	tokens := &fakeTokens{tokens: []string{"tok-1", "tok-2"}}
	shared, _ := newFakeShared(t, f, tokens)
	service := shared.Service(t.TempDir())
	if _, err := service.Repository(context.Background(), "acme/repo"); err != nil {
		t.Fatal(err)
	}
	statuses, auth := f.take()
	if !equalInts(statuses, []int{401, 200}) || auth[0] != "Bearer tok-1" || auth[1] != "Bearer tok-2" {
		t.Fatalf("statuses %v auth %v", statuses, auth)
	}
	// A token that is still rejected after the re-read is reported, not retried.
	f.validAuth = "Bearer never"
	tokens.mu.Lock()
	tokens.tokens = []string{"tok-3"}
	tokens.mu.Unlock()
	_, err := service.Repository(context.Background(), "acme/repo")
	if domain.Code(err) != "unauthorized" {
		t.Fatalf("err = %v", err)
	}
	if statuses, _ := f.take(); !equalInts(statuses, []int{401, 401}) {
		t.Fatalf("statuses %v", statuses)
	}
}

func TestTokenIsOnlySentToTheConfiguredAPIHost(t *testing.T) {
	var leaked bool
	foreign := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leaked = r.Header.Get("Authorization") != ""
	}))
	defer foreign.Close()
	f := &fakeGitHub{bodies: map[string]string{}}
	tokens := &fakeTokens{tokens: []string{"tok-1"}}
	shared, _ := newFakeShared(t, f, tokens)
	client := &http.Client{Transport: &httpTransport{shared: shared}}
	for _, target := range []string{foreign.URL + "/repos/acme/repo", "http://example.com/api/v3/repos/acme/repo"} {
		resp, err := client.Get(target)
		if err == nil {
			resp.Body.Close()
			t.Fatalf("request to %s was sent", target)
		}
	}
	if leaked {
		t.Fatal("token sent to a foreign host")
	}
	host, ok := shared.tokenHost(mustURL("https://ghe.example.com/api/v3/repos/acme/repo"))
	if !ok || host != "ghe.example.com" {
		t.Fatalf("GHES REST host = %q %v", host, ok)
	}
	if host, ok := shared.tokenHost(mustURL("https://ghe.example.com/api/graphql")); !ok || host != "ghe.example.com" {
		t.Fatalf("GHES GraphQL host = %q %v", host, ok)
	}
	if host, ok := NewShared().tokenHost(mustURL("https://api.github.com/repos/acme/repo")); !ok || host != DefaultHost {
		t.Fatalf("github.com host = %q %v", host, ok)
	}
}

func TestMissingGhIsAGitHubAuthError(t *testing.T) {
	f := &fakeGitHub{bodies: map[string]string{}}
	tokens := &fakeTokens{err: &exec.Error{Name: "gh", Err: exec.ErrNotFound}}
	shared, _ := newFakeShared(t, f, tokens)
	_, err := shared.Service(t.TempDir()).Repository(context.Background(), "acme/repo")
	if domain.Code(err) != "github_auth" || !strings.Contains(err.Error(), "https://cli.github.com") {
		t.Fatalf("err = %v", err)
	}
	if statuses, _ := f.take(); len(statuses) != 0 {
		t.Fatalf("GitHub was called without a token: %v", statuses)
	}
	if status := shared.AuthStatus(context.Background(), DefaultHost); status.State != AuthMissing {
		t.Fatalf("status %+v", status)
	}
}

func TestETagCacheIsBoundedByBytes(t *testing.T) {
	c := newETagCache(4096)
	for i := range 10 {
		c.put(&etagEntry{key: strconv.Itoa(i), body: make([]byte, 1000)})
	}
	if c.used > 4096 || c.len() != 3 {
		t.Fatalf("used %d entries %d", c.used, c.len())
	}
	if c.get("9", "") == nil || c.get("0", "") != nil {
		t.Fatal("LRU kept the wrong entries")
	}
	c.put(&etagEntry{key: "x", tokenID: "a", body: []byte("{}")})
	if c.get("x", "b") != nil || c.get("x", "a") != nil {
		t.Fatal("an entry was served to another token")
	}
}

func TestRateLimitTracking(t *testing.T) {
	r := newRateTracker()
	h := http.Header{}
	h.Set("X-RateLimit-Limit", "5000")
	h.Set("X-RateLimit-Remaining", "400")
	h.Set("X-RateLimit-Reset", "2000000000")
	r.observe("github.com", h)
	h.Set("X-RateLimit-Resource", "graphql")
	h.Set("X-RateLimit-Remaining", "1")
	r.observe("github.com", h)
	rate, ok := r.get("github.com")
	if !ok || rate.Remaining != 400 || !rate.Low() || rate.Exhausted(time.Unix(1, 0)) {
		t.Fatalf("rate %+v", rate)
	}
	if !(Rate{Limit: 5000, Remaining: 0, Reset: time.Unix(10, 0)}).Exhausted(time.Unix(5, 0)) {
		t.Fatal("exhausted window not reported")
	}
}

func TestPrewarmReadsTheTokenAndRateLimit(t *testing.T) {
	f := &fakeGitHub{remaining: 4000, bodies: map[string]string{"/rate_limit": `{"resources":{}}`}}
	tokens := &fakeTokens{tokens: []string{"tok-1"}}
	shared, _ := newFakeShared(t, f, tokens)
	shared.Prewarm(context.Background(), DefaultHost)
	if rate, ok := shared.RateLimit(DefaultHost); !ok || rate.Remaining != 4000 {
		t.Fatalf("rate %+v %v", rate, ok)
	}
	if tokens.calls.Load() != 1 {
		t.Fatal("token not read")
	}
}

func TestFallsBackToGhAPIWhenGhCannotPrintAToken(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake gh uses a shell script")
	}
	dir := t.TempDir()
	script := `#!/bin/sh
if [ "$1" = "auth" ]; then
  echo 'unknown command "token" for "gh auth"' >&2
  exit 1
fi
printf 'HTTP/1.1 200 OK\r\nContent-Type: application/json\r\n\r\n'
printf '{"id":123,"full_name":"owner/repo","default_branch":"main"}'
`
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	_, gh0 := trace.Counts()
	repo, err := NewShared().Service(dir).Repository(context.Background(), "owner/repo")
	if err != nil || repo.FullName != "owner/repo" {
		t.Fatalf("%+v %v", repo, err)
	}
	if _, gh1 := trace.Counts(); gh1-gh0 != 2 {
		t.Fatalf("gh spawns = %d, want auth token + api", gh1-gh0)
	}
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func mustURL(raw string) *url.URL {
	u, err := url.Parse(raw)
	if err != nil {
		panic(err)
	}
	return u
}
