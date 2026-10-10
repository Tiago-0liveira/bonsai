package livehooks

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/git/github/app"
	"github.com/Tiago-0liveira/bonsai/internal/server/webhooks"
)

const (
	repo      = "acme/repo"
	installID = "0123456789abcdef"
	secretA   = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	secretB   = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

// fakeGitHub serves the repository and hook endpoints the hook manager uses.
type fakeGitHub struct {
	t *testing.T

	mu     sync.Mutex
	admin  bool
	scopes string // X-OAuth-Scopes; "-" leaves the header out
	// hidden answers hook calls with 404, as GitHub does for a classic token
	// without hook scope.
	hidden  bool
	hooks   map[int64]*app.Hook
	secrets map[int64]string
	pings   map[int64]int
	nextID  int64
	calls   []string
}

func newFakeGitHub(t *testing.T) (*fakeGitHub, *app.Client) {
	f := &fakeGitHub{t: t, admin: true, scopes: "gist, read:org, repo, workflow", hooks: map[int64]*app.Hook{}, secrets: map[int64]string{}, pings: map[int64]int{}, nextID: 100}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /repos/acme/repo", func(w http.ResponseWriter, r *http.Request) {
		f.write(w, 200, map[string]any{"id": 1, "full_name": repo, "permissions": map[string]bool{"admin": f.admin, "push": true}})
	})
	mux.HandleFunc("GET /repos/acme/repo/hooks", func(w http.ResponseWriter, r *http.Request) {
		if f.denied(w) {
			return
		}
		out := []app.Hook{}
		for _, id := range f.ids() {
			out = append(out, *f.hooks[id])
		}
		f.write(w, 200, out)
	})
	mux.HandleFunc("POST /repos/acme/repo/hooks", func(w http.ResponseWriter, r *http.Request) {
		if f.denied(w) {
			return
		}
		var body struct {
			Name   string
			Active bool
			Events []string
			Config app.HookConfig
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name != "web" || !body.Active || body.Config.ContentType != "json" {
			f.write(w, 422, map[string]string{"message": "Validation Failed"})
			return
		}
		f.nextID++
		hook := &app.Hook{ID: f.nextID, Name: "web", Active: true, Events: body.Events, Config: app.HookConfig{URL: body.Config.URL, ContentType: "json", InsecureSSL: "0", Secret: "********"}, LastResponse: app.HookResponse{Status: "unused"}}
		f.hooks[hook.ID] = hook
		f.secrets[hook.ID] = body.Config.Secret
		f.pings[hook.ID]++ // GitHub pings a new hook
		f.write(w, 201, hook)
	})
	mux.HandleFunc("/repos/acme/repo/hooks/{id}", func(w http.ResponseWriter, r *http.Request) {
		if f.denied(w) {
			return
		}
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		hook := f.hooks[id]
		if hook == nil {
			f.write(w, 404, map[string]string{"message": "Not Found"})
			return
		}
		switch r.Method {
		case http.MethodGet:
			f.write(w, 200, hook)
		case http.MethodPatch:
			var body struct {
				Active bool
				Events []string
				Config app.HookConfig
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			hook.Active, hook.Events, hook.Config.URL = body.Active, body.Events, body.Config.URL
			if body.Config.Secret != "" {
				f.secrets[id] = body.Config.Secret
			}
			f.write(w, 200, hook)
		case http.MethodDelete:
			delete(f.hooks, id)
			w.WriteHeader(204)
		}
	})
	mux.HandleFunc("POST /repos/acme/repo/hooks/{id}/pings", func(w http.ResponseWriter, r *http.Request) {
		if f.denied(w) {
			return
		}
		id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if f.hooks[id] == nil {
			f.write(w, 404, map[string]string{"message": "Not Found"})
			return
		}
		f.pings[id]++
		w.WriteHeader(204)
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.calls = append(f.calls, r.Method+" "+r.URL.Path)
		if f.scopes != "-" {
			w.Header().Set("X-OAuth-Scopes", f.scopes)
		}
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)
	c := app.New(tokens{})
	c.BaseURL = server.URL
	return f, c
}

type tokens struct{}

func (tokens) Token(context.Context, string, bool) (string, error) { return "tok", nil }

func (f *fakeGitHub) denied(w http.ResponseWriter) bool {
	if f.hidden {
		f.write(w, 404, map[string]string{"message": "Not Found"})
	}
	return f.hidden
}

func (f *fakeGitHub) write(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (f *fakeGitHub) ids() []int64 {
	var ids []int64
	for id := range f.hooks {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

func (f *fakeGitHub) take() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.calls
	f.calls = nil
	return out
}

func (f *fakeGitHub) setLastResponse(id int64, code int, message string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.hooks[id].LastResponse = app.HookResponse{Code: &code, Status: "active", Message: message}
}

func want(publicURL, secret string) Desired {
	return Desired{PublicURL: publicURL, InstallID: installID, Secret: []byte(secret)}
}

func TestHookURLAndMarker(t *testing.T) {
	u := HookURL("https://a.trycloudflare.com/", installID)
	if u != "https://a.trycloudflare.com/github/webhook?bonsai="+installID {
		t.Fatal(u)
	}
	if id, ok := InstallIDOf(u); !ok || id != installID {
		t.Fatal(id, ok)
	}
	for _, other := range []string{"https://a.example/hooks?bonsai=" + installID, "https://a.example/github/webhook", "::"} {
		if _, ok := InstallIDOf(other); ok {
			t.Errorf("%q has a marker", other)
		}
	}
	if IsOwnHook(app.Hook{Config: app.HookConfig{URL: HookURL("https://x", "other")}}, installID) {
		t.Fatal("another machine's hook is ours")
	}
	if fp := SecretFingerprint([]byte(secretA)); len(fp) != 16 || strings.Contains(secretA, fp) || fp == SecretFingerprint([]byte(secretB)) {
		t.Fatal(fp)
	}
	for _, event := range webhooks.LiveHookEvents {
		action := ""
		switch event {
		case "pull_request", "pull_request_review", "check_run", "check_suite", "workflow_run":
			action = map[string]string{"pull_request": "opened", "pull_request_review": "submitted", "check_run": "completed", "check_suite": "completed", "workflow_run": "completed"}[event]
		}
		if !webhooks.AllowedLiveEventAction(event, action) {
			t.Errorf("hook subscribes to %s, which the receiver refuses", event)
		}
	}
}

func TestReconcileCreatesRepointsAndChecksHealth(t *testing.T) {
	f, c := newFakeGitHub(t)
	ctx := context.Background()
	now := time.Unix(1_900_000_000, 0).UTC()

	// Created with the receiver's events, the marked URL and the secret.
	state := Reconcile(ctx, c, repo, want("https://first.trycloudflare.com", secretA), config.WebLiveRepository{}, now)
	if state.State != config.WebLiveStateWaiting || state.HookID != 101 || state.LastError != "" || !state.ConfiguredAt.Equal(now) {
		t.Fatalf("created: %+v", state)
	}
	hook := f.hooks[101]
	if hook.Config.URL != "https://first.trycloudflare.com/github/webhook?bonsai="+installID || f.secrets[101] != secretA || !slices.Equal(hook.Events, webhooks.LiveHookEvents) {
		t.Fatalf("hook %+v secret %q", hook, f.secrets[101])
	}
	if state.HookURL != hook.Config.URL || state.SecretFingerprint != SecretFingerprint([]byte(secretA)) {
		t.Fatalf("state %+v", state)
	}
	if f.pings[101] != 1 {
		t.Fatalf("pings = %d", f.pings[101])
	}
	f.take()

	// Unchanged: no write; waiting until the ping window passes, then
	// GitHub's last delivery decides.
	if again := Reconcile(ctx, c, repo, want("https://first.trycloudflare.com", secretA), state, now.Add(time.Second)); again.State != config.WebLiveStateWaiting {
		t.Fatalf("within the ping window: %+v", again)
	}
	f.setLastResponse(101, 200, "OK")
	state = Check(ctx, c, repo, want("https://first.trycloudflare.com", secretA), state, now.Add(time.Minute))
	if state.State != config.WebLiveStateLive || state.LastError != "" {
		t.Fatalf("healthy: %+v", state)
	}
	for _, call := range f.take() {
		if !strings.HasPrefix(call, "GET ") {
			t.Fatalf("unchanged hook was written: %v", call)
		}
	}

	// Failing deliveries are reported with GitHub's last response.
	f.setLastResponse(101, 530, "Origin DNS error")
	failing := Check(ctx, c, repo, want("https://first.trycloudflare.com", secretA), state, now.Add(2*time.Minute))
	if failing.State != config.WebLiveStateFailing || failing.LastError != "last delivery got HTTP 530: Origin DNS error" {
		t.Fatalf("failing: %+v", failing)
	}

	// A new public URL (quick tunnel restart) re-points the hook and pings.
	moved := Check(ctx, c, repo, want("https://second.trycloudflare.com", secretA), failing, now.Add(3*time.Minute))
	if moved.State != config.WebLiveStateWaiting || moved.HookID != 101 || !strings.HasPrefix(f.hooks[101].Config.URL, "https://second.trycloudflare.com/") || f.pings[101] != 2 || moved.LastError != "" {
		t.Fatalf("moved: %+v hook %+v pings %d", moved, f.hooks[101], f.pings[101])
	}
	if calls := f.take(); !slices.Contains(calls, "PATCH /repos/acme/repo/hooks/101") || !slices.Contains(calls, "POST /repos/acme/repo/hooks/101/pings") {
		t.Fatalf("calls %v", calls)
	}

	// A rotated secret is sent again although the URL did not change.
	rotated := Reconcile(ctx, c, repo, want("https://second.trycloudflare.com", secretB), moved, now.Add(4*time.Minute))
	if f.secrets[101] != secretB || rotated.SecretFingerprint != SecretFingerprint([]byte(secretB)) || rotated.State != config.WebLiveStateWaiting {
		t.Fatalf("rotated: %+v secret %q", rotated, f.secrets[101])
	}
	if len(f.hooks) != 1 {
		t.Fatalf("hooks %v", f.ids())
	}
}

func TestReconcileFindsHookByMarkerAndIgnoresOthers(t *testing.T) {
	f, c := newFakeGitHub(t)
	ctx := context.Background()
	now := time.Unix(1_900_000_000, 0).UTC()
	// Another machine's Bonsai hook and an unrelated hook stay untouched.
	f.hooks[7] = &app.Hook{ID: 7, Active: true, Events: []string{"push"}, Config: app.HookConfig{URL: HookURL("https://other.example", "feedfacefeedface"), ContentType: "json"}}
	f.hooks[8] = &app.Hook{ID: 8, Active: true, Events: []string{"push"}, Config: app.HookConfig{URL: "https://ci.example/hook", ContentType: "json"}}
	// Ours, found by marker after web-state.json was lost: re-sent its secret.
	f.hooks[9] = &app.Hook{ID: 9, Active: true, Events: webhooks.LiveHookEvents, Config: app.HookConfig{URL: HookURL("https://same.example", installID), ContentType: "json"}}

	state := Reconcile(ctx, c, repo, want("https://same.example", secretA), config.WebLiveRepository{}, now)
	if state.HookID != 9 || f.secrets[9] != secretA || len(f.hooks) != 3 || state.State != config.WebLiveStateWaiting {
		t.Fatalf("state %+v hooks %v secret %q", state, f.ids(), f.secrets[9])
	}
	if f.hooks[7].Config.URL != HookURL("https://other.example", "feedfacefeedface") || f.hooks[8].Config.URL != "https://ci.example/hook" {
		t.Fatal("touched another hook")
	}

	// A stored ID that now belongs to someone else's hook is not used.
	stale := config.WebLiveRepository{HookID: 8, SecretFingerprint: SecretFingerprint([]byte(secretA))}
	if got := Reconcile(ctx, c, repo, want("https://same.example", secretA), stale, now); got.HookID != 9 {
		t.Fatalf("stale id: %+v", got)
	}

	// The hook deleted on GitHub is created again by the health check.
	delete(f.hooks, 9)
	f.take()
	recreated := Check(ctx, c, repo, want("https://same.example", secretA), state, now.Add(time.Hour))
	if recreated.HookID == 9 || recreated.HookID == 0 || recreated.State != config.WebLiveStateWaiting {
		t.Fatalf("recreated: %+v", recreated)
	}
	if calls := f.take(); !slices.Contains(calls, "POST /repos/acme/repo/hooks") {
		t.Fatalf("calls %v", calls)
	}
}

func TestReconcileNeedsAdminAndScope(t *testing.T) {
	f, c := newFakeGitHub(t)
	ctx := context.Background()
	now := time.Unix(1_900_000_000, 0).UTC()

	f.admin = false
	state := Reconcile(ctx, c, repo, want("https://x.example", secretA), config.WebLiveRepository{}, now)
	if state.State != config.WebLiveStateNeedsAdmin || !strings.Contains(state.LastError, "not an admin") || len(f.hooks) != 0 {
		t.Fatalf("not admin: %+v", state)
	}
	for _, call := range f.take() {
		if strings.Contains(call, "/hooks") {
			t.Fatalf("hook call without admin: %s", call)
		}
	}

	f.admin = true
	f.hidden = true
	f.scopes = "gist, read:org"
	state = Reconcile(ctx, c, repo, want("https://x.example", secretA), config.WebLiveRepository{}, now)
	if state.State != config.WebLiveStateScopeMissing || !strings.Contains(state.LastError, "gh auth refresh -h github.com -s admin:repo_hook") {
		t.Fatalf("scope: %+v", state)
	}

	// A classic token with repo, or a fine-grained token (no scope header),
	// is not a scope problem: the 404 stays a 404.
	for _, scopes := range []string{"repo", "-"} {
		f.scopes = scopes
		state = Reconcile(ctx, c, repo, want("https://x.example", secretA), config.WebLiveRepository{}, now)
		if state.State != config.WebLiveStateFailing || strings.Contains(state.LastError, "auth refresh") {
			t.Fatalf("scopes %q: %+v", scopes, state)
		}
	}

	// A transient error keeps a live hook live.
	f.hidden = false
	f.scopes = "repo"
	live := config.WebLiveRepository{HookID: 5, State: config.WebLiveStateLive}
	c.BaseURL = "http://127.0.0.1:1"
	if got := Check(ctx, c, repo, want("https://x.example", secretA), live, now); got.State != config.WebLiveStateLive || !strings.HasPrefix(got.LastError, "GitHub: ") {
		t.Fatalf("transient: %+v", got)
	}
}

func TestPingAndRemove(t *testing.T) {
	f, c := newFakeGitHub(t)
	ctx := context.Background()
	now := time.Unix(1_900_000_000, 0).UTC()
	state := Reconcile(ctx, c, repo, want("https://x.example", secretA), config.WebLiveRepository{}, now)
	if err := Ping(ctx, c, repo, state); err != nil || f.pings[state.HookID] != 2 {
		t.Fatal(err, f.pings)
	}
	if err := Ping(ctx, c, repo, config.WebLiveRepository{}); err == nil {
		t.Fatal("pinged no hook")
	}
	f.hooks[8] = &app.Hook{ID: 8, Config: app.HookConfig{URL: "https://ci.example/hook"}}

	// Found by marker when the stored ID is gone.
	removed, err := Remove(ctx, c, repo, config.WebLiveRepository{}, installID)
	if err != nil || !removed || len(f.hooks) != 1 || f.hooks[8] == nil {
		t.Fatal(removed, err, f.ids())
	}
	if removed, err := Remove(ctx, c, repo, state, installID); err != nil || removed {
		t.Fatal("removed twice", removed, err)
	}
}

func TestScopeErrorsFromTheClient(t *testing.T) {
	f, c := newFakeGitHub(t)
	ctx := context.Background()
	f.hidden = true
	f.scopes = "read:repo_hook"
	if _, err := c.ListHooks(ctx, repo); err == nil || app.IsHookScope(err) {
		t.Fatalf("read:repo_hook may list hooks: %v", err)
	}
	if _, err := c.CreateHook(ctx, repo, app.HookSpec{URL: "https://x.example/github/webhook", Secret: "s", Events: []string{"push"}}); !app.IsHookScope(err) {
		t.Fatalf("read:repo_hook cannot create hooks: %v", err)
	}
	if _, err := c.CreateHook(ctx, repo, app.HookSpec{URL: "http://x.example", Secret: "s", Events: []string{"push"}}); err == nil {
		t.Fatal("accepted a plain-HTTP hook URL")
	}
	if err := c.PingHook(ctx, repo, 0); err == nil {
		t.Fatal("pinged hook 0")
	}
}
