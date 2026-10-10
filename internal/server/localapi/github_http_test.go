package localapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/trace"
	domain "github.com/Tiago-0liveira/bonsai/internal/git"
	gh "github.com/Tiago-0liveira/bonsai/internal/git/github"
	"github.com/Tiago-0liveira/bonsai/internal/git/github/ghcli"
	"github.com/gorilla/websocket"
)

// fakeGitHubAPI is a small GitHub REST server with ETags. Branches and the
// open PR list have two pages each.
type fakeGitHubAPI struct {
	mu        sync.Mutex
	remaining int
	validAuth map[string]bool
	responses []fakeResponse
}

type fakeResponse struct {
	resource string
	status   int
}

func (f *fakeGitHubAPI) body(r *http.Request) (string, string, bool) {
	page := r.URL.Query().Get("page")
	switch path := r.URL.Path; {
	case path == "/repos/acme/repo":
		return "repo", `{"id":1,"full_name":"acme/repo","default_branch":"main"}`, true
	case path == "/repos/acme/repo/branches" && page == "1":
		return "branches:1", `[{"name":"main","commit":{"sha":"m1"}}]`, true
	case path == "/repos/acme/repo/branches" && page == "2":
		return "branches:2", `[{"name":"feature","commit":{"sha":"abc123"}}]`, true
	case path == "/repos/acme/repo/pulls" && page == "1":
		return "pulls:1", `[{"number":7,"title":"Feature","state":"open","head":{"ref":"feature","sha":"abc123","repo":{"full_name":"acme/repo"}},"base":{"ref":"main"},"updated_at":"2026-10-01T00:00:00Z"}]`, true
	case path == "/repos/acme/repo/pulls" && page == "2":
		return "pulls:2", `[{"number":3,"title":"Older","state":"open","head":{"ref":"older","sha":"def456","repo":{"full_name":"acme/repo"}},"base":{"ref":"main"},"updated_at":"2026-09-01T00:00:00Z"}]`, true
	case path == "/repos/acme/repo/commits/abc123/check-runs":
		return "check-runs", `{"check_runs":[{"id":1,"name":"ci","status":"completed","conclusion":"success"}]}`, true
	case path == "/repos/acme/repo/commits/abc123/statuses":
		return "statuses", `[]`, true
	}
	return "", "", false
}

func (f *fakeGitHubAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	w.Header().Set("X-RateLimit-Limit", "5000")
	w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(f.remaining))
	w.Header().Set("X-RateLimit-Reset", "2000000000")
	resource, body, ok := f.body(r)
	if !ok {
		resource = r.URL.Path
	}
	status := http.StatusOK
	switch {
	case f.validAuth != nil && !f.validAuth[r.Header.Get("Authorization")]:
		status = http.StatusUnauthorized
		body = `{"message":"Bad credentials"}`
	case !ok:
		status = http.StatusNotFound
		body = `{"message":"Not Found"}`
	}
	if status == http.StatusOK {
		etag := fmt.Sprintf(`"%x"`, sha256.Sum256([]byte(body)))
		if r.Header.Get("If-None-Match") == etag {
			status = http.StatusNotModified
		}
		w.Header().Set("ETag", etag)
		if page := r.URL.Query().Get("page"); page == "1" && (strings.HasSuffix(r.URL.Path, "/branches") || strings.HasSuffix(r.URL.Path, "/pulls")) {
			next := *r.URL
			q := next.Query()
			q.Set("page", "2")
			next.RawQuery = q.Encode()
			w.Header().Set("Link", `<`+next.String()+`>; rel="next"`)
		}
	}
	f.responses = append(f.responses, fakeResponse{resource: resource, status: status})
	w.WriteHeader(status)
	if status != http.StatusNotModified {
		_, _ = w.Write([]byte(body))
	}
}

func (f *fakeGitHubAPI) take() []fakeResponse {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.responses
	f.responses = nil
	return out
}

type providerHarness struct {
	syncer    *stateSync
	projectID string
	worktree  string
	api       *fakeGitHubAPI
	now       *time.Time
	shared    *ghcli.Shared
}

// newProviderHarness wires one project whose worktree tracks acme/repo's
// "feature" branch to the in-process GitHub client and a fake GitHub.
func newProviderHarness(t *testing.T, tokens ghcli.TokenRunner) *providerHarness {
	t.Helper()
	api := &fakeGitHubAPI{remaining: 4900}
	server := httptest.NewServer(api)
	t.Cleanup(server.Close)
	shared := ghcli.NewShared()
	shared.SetAPIBase(ghcli.DefaultHost, server.URL)
	shared.SetTokenRunner(tokens)

	root := t.TempDir()
	projectID := "project-acme"
	project := projectServices{
		info:   ProjectInfo{ID: projectID, Path: root, Available: true, FullName: "acme/repo"},
		daemon: &syncTestDaemon{root: root},
		github: shared.Service(root),
	}
	registry := &syncTestRegistry{entries: map[string]projectServices{projectID: project}}
	syncer := newStateSync(registry, newEventHub())
	now := time.Unix(1_900_000_000, 0).UTC()
	syncer.now = func() time.Time { return now }
	syncer.ReconcileCatalog()
	worktreeID := root + "#feature"
	local := domain.RepositoryState{
		ID:      localRepositoryID,
		Remotes: []domain.RemoteIdentity{{Name: "origin", Host: "github.com", Owner: "acme", Repository: "repo", FullName: "acme/repo"}},
		Branches: []domain.Branch{
			{Name: "feature", Upstream: "origin/feature"},
			{Name: "origin/feature", Remote: true, LocalRemoteRefSHA: "abc123"},
		},
		Worktrees: []domain.Worktree{{ID: worktreeID, Path: root, Branch: "feature", HeadSHA: "abc123", Status: &domain.WorkingTreeStatus{Branch: "feature", Upstream: "origin/feature", LocalRemoteRefSHA: "abc123", HeadSHA: "abc123"}}},
	}
	syncer.commitProject(project, "seed", func(snapshot *browserSnapshot) {
		snapshot.Local = &local
		snapshot.Freshness["local"] = browserFreshness{State: "ready"}
	})
	return &providerHarness{syncer: syncer, projectID: projectID, worktree: worktreeID, api: api, now: &now, shared: shared}
}

func staticToken(token string) ghcli.TokenRunner {
	return func(context.Context, string) ([]byte, []byte, error) { return []byte(token + "\n"), nil, nil }
}

func (h *providerHarness) snapshot(t *testing.T) browserSnapshot {
	t.Helper()
	snapshot, ok := h.syncer.CachedSnapshot(h.projectID)
	if !ok {
		t.Fatal("no snapshot")
	}
	return snapshot
}

func TestWarmProviderRefreshMakesOnlyNotModifiedRequests(t *testing.T) {
	h := newProviderHarness(t, staticToken("tok-warm"))

	h.syncer.refreshProvider(h.projectID, false)
	cold := h.api.take()
	for _, response := range cold {
		if response.status != http.StatusOK {
			t.Fatalf("cold response %+v", response)
		}
	}
	snapshot := h.snapshot(t)
	if snapshot.Remote == nil || len(snapshot.Remote.PullRequests) != 2 || !snapshot.Remote.PRCatalogComplete {
		t.Fatalf("cold catalog %+v", snapshot.Remote)
	}
	state := snapshot.WorktreeState[h.worktree]
	if state.PullRequest == nil || state.PullRequest.Number != 7 || state.CI.Status != "passed" {
		t.Fatalf("cold worktree state %+v", state)
	}

	// A forced refresh of an unchanged repository: every read is conditional
	// and answered 304, and the second PR page is skipped.
	*h.now = h.now.Add(time.Minute)
	h.syncer.refreshProvider(h.projectID, true)
	warm := h.api.take()
	resources := map[string]bool{}
	for _, response := range warm {
		if response.status != http.StatusNotModified {
			t.Fatalf("warm refresh made a non-304 request: %+v (all: %+v)", response, warm)
		}
		resources[response.resource] = true
	}
	for _, want := range []string{"repo", "branches:1", "branches:2", "pulls:1", "check-runs", "statuses"} {
		if !resources[want] {
			t.Fatalf("warm refresh did not revalidate %s: %+v", want, warm)
		}
	}
	if resources["pulls:2"] {
		t.Fatalf("an unchanged first PR page did not skip the rest: %+v", warm)
	}
	if after := h.snapshot(t); len(after.Remote.PullRequests) != 2 || after.WorktreeState[h.worktree].CI.Status != "passed" || after.Freshness["provider"].Error != nil {
		t.Fatalf("warm refresh changed the projection: %+v %+v", after.Remote, after.Freshness["provider"])
	}

	// The shortcut stands in for a full scan only for a while.
	*h.now = h.now.Add(prFullReconcileInterval + time.Second)
	h.syncer.refreshProvider(h.projectID, true)
	reconcile := h.api.take()
	found := false
	for _, response := range reconcile {
		if response.status != http.StatusNotModified {
			t.Fatalf("reconcile made a non-304 request: %+v", response)
		}
		found = found || response.resource == "pulls:2"
	}
	if !found {
		t.Fatalf("full reconcile skipped page 2: %+v", reconcile)
	}
}

func TestProviderFreshnessReportsMissingGitHubLogin(t *testing.T) {
	missing := func(context.Context, string) ([]byte, []byte, error) {
		return nil, nil, &exec.Error{Name: "gh", Err: exec.ErrNotFound}
	}
	h := newProviderHarness(t, missing)
	h.syncer.refreshProvider(h.projectID, false)
	snapshot := h.snapshot(t)
	provider := snapshot.Freshness["provider"]
	if provider.State != "error" || provider.Error == nil || provider.Error.Code != "github_auth" || !strings.Contains(provider.Error.Message, "gh auth login") {
		t.Fatalf("provider freshness %+v", provider)
	}
	if snapshot.Freshness["local"].State != "ready" || snapshot.Local == nil {
		t.Fatalf("local state was affected: %+v", snapshot.Freshness["local"])
	}
	if responses := h.api.take(); len(responses) != 0 {
		t.Fatalf("GitHub was called without a token: %+v", responses)
	}

	unauthenticated := func(context.Context, string) ([]byte, []byte, error) {
		return nil, []byte("no oauth token found for github.com"), &exec.ExitError{}
	}
	h = newProviderHarness(t, unauthenticated)
	h.syncer.refreshProvider(h.projectID, false)
	if provider := h.snapshot(t).Freshness["provider"]; provider.Error == nil || provider.Error.Code != "github_auth" || !strings.Contains(provider.Error.Message, "gh auth login --hostname github.com") {
		t.Fatalf("provider freshness %+v", provider)
	}
}

func TestProviderFreshnessReportsLowRateLimit(t *testing.T) {
	h := newProviderHarness(t, staticToken("tok-rate"))
	h.api.remaining = 120 // of 5000: below 10%
	h.syncer.refreshProvider(h.projectID, false)
	provider := h.snapshot(t).Freshness["provider"]
	if provider.State != "ready" || provider.Error == nil || provider.Error.Code != "rate_limited" {
		t.Fatalf("provider freshness %+v", provider)
	}
	if provider.Error.ResetAt == nil || !provider.Error.ResetAt.Equal(time.Unix(2000000000, 0)) {
		t.Fatalf("reset_at %+v", provider.Error.ResetAt)
	}
	raw, err := json.Marshal(provider)
	if err != nil || !strings.Contains(string(raw), `"reset_at":"2033-05-18T03:33:20Z"`) {
		t.Fatalf("wire freshness %s %v", raw, err)
	}

	h.api.remaining = 4000
	*h.now = h.now.Add(time.Minute)
	h.syncer.refreshProvider(h.projectID, true)
	if provider := h.snapshot(t).Freshness["provider"]; provider.Error != nil {
		t.Fatalf("recovered rate limit still reported: %+v", provider)
	}
}

// The token from gh must never reach logs, trace output, errors or the
// browser projection, including across a 401 re-read.
func TestGitHubTokenNeverLeaks(t *testing.T) {
	const first, second = "gho_FIRSTsecretTOKENvalue0001", "gho_SECONDsecretTOKENvalue002"
	var calls int
	var mu sync.Mutex
	runner := func(context.Context, string) ([]byte, []byte, error) {
		mu.Lock()
		defer mu.Unlock()
		calls++
		if calls == 1 {
			return []byte(first), nil, nil
		}
		return []byte(second), nil, nil
	}
	h := newProviderHarness(t, runner)
	h.api.validAuth = map[string]bool{"Bearer " + second: true}

	var logs, spans bytes.Buffer
	previousLog := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(previousLog)
	restore := trace.SetOutput(&spans)
	defer restore()
	trace.SetEnabled(true)
	defer trace.SetEnabled(false)

	h.syncer.refreshProvider(h.projectID, false)
	*h.now = h.now.Add(time.Minute)
	h.syncer.refreshProvider(h.projectID, true)
	snapshot := h.snapshot(t)
	if snapshot.Freshness["provider"].Error != nil || snapshot.Remote == nil {
		t.Fatalf("refresh failed: %+v", snapshot.Freshness["provider"])
	}
	// A rejected token afterwards produces an error that is checked too.
	h.api.validAuth = map[string]bool{}
	*h.now = h.now.Add(time.Minute)
	h.syncer.refreshProvider(h.projectID, true)
	failed := h.snapshot(t).Freshness["provider"]
	if failed.Error == nil {
		t.Fatal("expected an authentication failure")
	}

	projection, err := json.Marshal(h.snapshot(t))
	if err != nil {
		t.Fatal(err)
	}
	if spans.Len() == 0 {
		t.Fatal("trace captured nothing")
	}
	for name, captured := range map[string]string{"logs": logs.String(), "trace": spans.String(), "projection": string(projection), "error": failed.Error.Message} {
		for _, token := range []string{first, second} {
			if strings.Contains(captured, token) {
				t.Fatalf("token leaked into %s", name)
			}
		}
	}
}

func TestNextProviderPoll(t *testing.T) {
	last := time.Unix(1_000_000, 0)
	now := last.Add(time.Second)
	healthy := ghcli.Rate{Limit: 5000, Remaining: 4000, Reset: now.Add(time.Hour)}
	low := ghcli.Rate{Limit: 5000, Remaining: 100, Reset: now.Add(time.Hour)}
	exhausted := ghcli.Rate{Limit: 5000, Remaining: 0, Reset: now.Add(time.Hour)}
	for _, test := range []struct {
		name      string
		visible   bool
		running   bool
		rate      ghcli.Rate
		rateKnown bool
		want      time.Time
	}{
		{name: "background", want: last.Add(5 * time.Minute)},
		{name: "visible", visible: true, rate: healthy, rateKnown: true, want: last.Add(30 * time.Second)},
		{name: "visible with CI running", visible: true, running: true, want: last.Add(15 * time.Second)},
		{name: "running CI in the background", running: true, want: last.Add(5 * time.Minute)},
		{name: "low rate limit", visible: true, rate: low, rateKnown: true, want: last.Add(2 * time.Minute)},
		{name: "low rate limit in the background", rate: low, rateKnown: true, want: last.Add(20 * time.Minute)},
		{name: "exhausted until reset", visible: true, rate: exhausted, rateKnown: true, want: now.Add(time.Hour)},
		{name: "past reset", visible: true, rate: ghcli.Rate{Limit: 5000, Remaining: 0, Reset: now.Add(-time.Second)}, rateKnown: true, want: last.Add(30 * time.Second)},
	} {
		if got := nextProviderPoll(last, test.visible, test.running, test.rate, test.rateKnown, now); !got.Equal(test.want) {
			t.Errorf("%s: next = %v, want %v", test.name, got.Sub(last), test.want.Sub(last))
		}
	}
	if StandardUpdateInterval != 30*time.Second {
		t.Fatalf("StandardUpdateInterval = %v", StandardUpdateInterval)
	}
}

func TestPollProvidersFavoursTheVisibleProject(t *testing.T) {
	registry := &syncTestRegistry{entries: map[string]projectServices{
		"visible":    {info: ProjectInfo{ID: "visible", Available: true}, github: &syncTestGitHub{}},
		"background": {info: ProjectInfo{ID: "background", Available: true}, github: &syncTestGitHub{}},
	}}
	syncer := newStateSync(registry, newEventHub())
	now := time.Unix(1_000_000, 0)
	syncer.now = func() time.Time { return now }
	syncer.ReconcileCatalog()
	syncer.SetFocus(2, "not-discovered-yet")
	syncer.SetFocus(1, "visible")
	if visible := syncer.visibleProjects(); len(visible) != 2 || !visible["visible"] || !visible["not-discovered-yet"] {
		t.Fatalf("visible = %v", visible)
	}
	if syncer.priorityID() != "visible" {
		t.Fatalf("priority = %q", syncer.priorityID())
	}
	started := now.Add(-31 * time.Second)
	syncer.mu.Lock()
	syncer.jobLocked("visible").providerStartedAt = started
	syncer.jobLocked("background").providerStartedAt = started
	syncer.mu.Unlock()

	syncer.pollProviders()
	waitFor(t, func() bool {
		syncer.mu.Lock()
		defer syncer.mu.Unlock()
		j := syncer.jobLocked("visible")
		return j.providerStartedAt.Equal(now) && !j.providerRunning
	})
	syncer.mu.Lock()
	background := syncer.jobLocked("background").providerStartedAt
	syncer.mu.Unlock()
	if !background.Equal(started) {
		t.Fatal("a background project was polled at the visible cadence")
	}

	// Once nothing is in view, everything is on the background cadence.
	syncer.SetFocus(1, "")
	syncer.ClearFocus(2)
	now = now.Add(time.Minute)
	syncer.pollProviders()
	time.Sleep(20 * time.Millisecond)
	syncer.mu.Lock()
	visibleStarted := syncer.jobLocked("visible").providerStartedAt
	syncer.mu.Unlock()
	if !visibleStarted.Equal(now.Add(-time.Minute)) {
		t.Fatal("a hidden project was polled at the visible cadence")
	}
	syncer.ClearFocus(1)
	if len(syncer.visibleProjects()) != 0 {
		t.Fatal("focus outlived its subscriber")
	}
}

func waitFor(t *testing.T, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !done() {
		if time.Now().After(deadline) {
			t.Fatal("condition not reached")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestEventSocketFocusFrames(t *testing.T) {
	s := newTestServer(t)
	registry := s.registry.(*discoveredProjectRegistry)
	registry.mu.Lock()
	registry.entries["focus-project"] = projectServices{info: ProjectInfo{ID: "focus-project"}}
	registry.mu.Unlock()
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	host := strings.TrimPrefix(ts.URL, "http://")
	s.expectedHost = host
	session, err := s.sessions.create()
	if err != nil {
		t.Fatal(err)
	}
	conn, _, err := websocket.DefaultDialer.Dial("ws://"+host+"/events", http.Header{"Origin": []string{ProductionBrowserOrigin}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.WriteJSON(websocketAuth{Type: "authenticate", Token: session.Token, ActiveProject: "focus-project"}); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	for {
		var event localEvent
		if err := conn.ReadJSON(&event); err != nil {
			t.Fatal(err)
		}
		if event.Type == "bootstrap_complete" {
			break
		}
	}
	focused := func(want string) func() bool {
		return func() bool {
			visible := s.stateSync.visibleProjects()
			if want == "" {
				return len(visible) == 0
			}
			return len(visible) == 1 && visible[want]
		}
	}
	waitFor(t, focused("focus-project"))
	if err := conn.WriteJSON(websocketFocus{Type: "focus", ActiveProject: ""}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, focused(""))
	if err := conn.WriteJSON(websocketFocus{Type: "focus", ActiveProject: "focus-project"}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, focused("focus-project"))
	// A project the server has not discovered yet is kept: the browser will not
	// send it again once it appears.
	if err := conn.WriteJSON(websocketFocus{Type: "focus", ActiveProject: "not-discovered-yet"}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, focused("not-discovered-yet"))
	if s.stateSync.priorityID() != "focus-project" {
		t.Fatalf("an unknown project became the priority project: %q", s.stateSync.priorityID())
	}

	// Oversized frames close the socket, and the subscriber's focus goes away.
	if err := conn.WriteJSON(websocketFocus{Type: "focus", ActiveProject: "focus-project"}); err != nil {
		t.Fatal(err)
	}
	waitFor(t, focused("focus-project"))
	if err := conn.WriteMessage(websocket.TextMessage, bytes.Repeat([]byte("x"), websocketClientFrameLimit+1)); err != nil {
		t.Fatal(err)
	}
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			break
		}
	}
	waitFor(t, focused(""))
}

// A focused project that the registry discovers only later is polled at the
// visible cadence from then on.
func TestFocusOnAProjectDiscoveredLater(t *testing.T) {
	registry := &syncTestRegistry{entries: map[string]projectServices{}}
	syncer := newStateSync(registry, newEventHub())
	now := time.Unix(1_000_000, 0)
	syncer.now = func() time.Time { return now }
	syncer.SetFocus(1, "late")
	registry.mu.Lock()
	registry.entries["late"] = projectServices{info: ProjectInfo{ID: "late", Available: true}, github: &syncTestGitHub{}}
	registry.mu.Unlock()
	syncer.ReconcileCatalog()
	syncer.mu.Lock()
	syncer.jobLocked("late").providerStartedAt = now.Add(-31 * time.Second)
	syncer.mu.Unlock()
	syncer.pollProviders()
	waitFor(t, func() bool {
		syncer.mu.Lock()
		defer syncer.mu.Unlock()
		return syncer.jobLocked("late").providerStartedAt.Equal(now)
	})
}

// A low rate limit annotates freshness but must not stop a catalog that is
// still paging.
func TestLowRateLimitKeepsPagingThePRCatalog(t *testing.T) {
	h := newProviderHarness(t, staticToken("tok-pages"))
	h.api.remaining = 100
	syncer := h.syncer
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	syncer.mu.Lock()
	syncer.runCtx = ctx
	syncer.mu.Unlock()
	// Leave the catalog mid-scan, as a repository with more than
	// prPagesPerJob pages would after one job.
	paging := &pagingGitHub{Service: h.shared.Service(t.TempDir())}
	registry := syncer.registry.(*syncTestRegistry)
	registry.mu.Lock()
	project := registry.entries[h.projectID]
	project.github = paging
	registry.entries[h.projectID] = project
	registry.mu.Unlock()

	syncer.refreshProvider(h.projectID, false)
	snapshot := h.snapshot(t)
	if provider := snapshot.Freshness["provider"]; provider.Error == nil || provider.Error.Code != "rate_limited" || snapshot.Remote == nil || !snapshot.Remote.PRCatalogLoading {
		t.Fatalf("expected a loading catalog with a rate-limit note: %+v %+v", provider, snapshot.Remote)
	}
	waitFor(t, func() bool {
		syncer.mu.Lock()
		defer syncer.mu.Unlock()
		return syncer.jobLocked(h.projectID).providerStartedAt.Equal(*h.now)
	})
}

// pagingGitHub reports one more PR page than it ever serves, so every catalog
// job ends mid-scan.
type pagingGitHub struct {
	*ghcli.Service
}

func (p *pagingGitHub) PullRequestPage(ctx context.Context, repo string, f gh.PRFilter, page int) (gh.PullRequestPage, error) {
	batch, err := p.Service.PullRequestPage(ctx, repo, f, 1)
	batch.NextPage = page + 1
	return batch, err
}

func TestRateLimitResetOnlyExplainsTheCoreWindow(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	limited := browserFreshness{State: "stale", Error: &browserStateError{Code: "rate_limited", Message: "secondary rate limit"}}
	healthy := ghcli.Rate{Limit: 5000, Remaining: 4000, Reset: now.Add(55 * time.Minute)}
	if got := withRateLimit(limited, healthy, true, now); got.Error.ResetAt != nil {
		t.Fatalf("a secondary limit got the core reset time: %v", got.Error.ResetAt)
	}
	low := ghcli.Rate{Limit: 5000, Remaining: 0, Reset: now.Add(5 * time.Minute)}
	if got := withRateLimit(limited, low, true, now); got.Error.ResetAt == nil || !got.Error.ResetAt.Equal(low.Reset) {
		t.Fatalf("core exhaustion lost its reset time: %+v", got.Error)
	}
	if limited.Error.ResetAt != nil {
		t.Fatal("withRateLimit mutated its input")
	}
}
