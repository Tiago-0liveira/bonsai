package localapi

import (
	"bytes"
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	domain "github.com/Tiago-0liveira/bonsai/internal/git"
	"github.com/Tiago-0liveira/bonsai/internal/git/github/app"
	"github.com/Tiago-0liveira/bonsai/internal/livehooks"
	"github.com/Tiago-0liveira/bonsai/internal/server/webhooks"
	"github.com/Tiago-0liveira/bonsai/internal/webtunnel"
)

// memoryHooks is an in-memory GitHub for the live controller.
type memoryHooks struct {
	mu      sync.Mutex
	hooks   map[int64]app.Hook
	secrets map[int64]string
	nextID  int64
	calls   []string
}

func newMemoryHooks() *memoryHooks {
	return &memoryHooks{hooks: map[int64]app.Hook{}, secrets: map[int64]string{}, nextID: 40}
}

func (m *memoryHooks) record(call string) {
	m.mu.Lock()
	m.calls = append(m.calls, call)
	m.mu.Unlock()
}

func (m *memoryHooks) take() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := m.calls
	m.calls = nil
	return out
}

func (m *memoryHooks) RepoAdmin(context.Context, string) (bool, error) {
	m.record("admin")
	return true, nil
}

func (m *memoryHooks) ListHooks(context.Context, string) ([]app.Hook, error) {
	m.record("list")
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []app.Hook{}
	for _, h := range m.hooks {
		out = append(out, h)
	}
	return out, nil
}

func (m *memoryHooks) GetHook(_ context.Context, _ string, id int64) (app.Hook, error) {
	m.record("get")
	m.mu.Lock()
	defer m.mu.Unlock()
	h, ok := m.hooks[id]
	if !ok {
		return app.Hook{}, domain.E("not_found", "Not Found")
	}
	return h, nil
}

func (m *memoryHooks) CreateHook(_ context.Context, _ string, spec app.HookSpec) (app.Hook, error) {
	m.record("create")
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextID++
	h := app.Hook{ID: m.nextID, Active: true, Events: spec.Events, Config: app.HookConfig{URL: spec.URL, ContentType: "json", InsecureSSL: "0"}}
	m.hooks[h.ID], m.secrets[h.ID] = h, spec.Secret
	return h, nil
}

func (m *memoryHooks) UpdateHook(_ context.Context, _ string, id int64, spec app.HookSpec) (app.Hook, error) {
	m.record("update")
	m.mu.Lock()
	defer m.mu.Unlock()
	h := m.hooks[id]
	h.Config.URL, h.Events, h.Active = spec.URL, spec.Events, true
	m.hooks[id], m.secrets[id] = h, spec.Secret
	return h, nil
}

func (m *memoryHooks) DeleteHook(context.Context, string, int64) error {
	m.record("delete")
	return errors.New("the live controller never deletes hooks")
}

func (m *memoryHooks) PingHook(context.Context, string, int64) error {
	m.record("ping")
	return nil
}

func (m *memoryHooks) setLastResponse(id int64, code int, message string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	h := m.hooks[id]
	h.LastResponse = app.HookResponse{Code: &code, Status: "active", Message: message}
	m.hooks[id] = h
}

func (m *memoryHooks) hookURL(id int64) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.hooks[id].Config.URL
}

// fakeTunnel is a public URL source the test controls.
type fakeTunnel struct {
	mu  sync.Mutex
	url string
	err error
}

func (f *fakeTunnel) set(url string, err error) {
	f.mu.Lock()
	f.url, f.err = url, err
	f.mu.Unlock()
}

func (f *fakeTunnel) source(_ context.Context, options webtunnel.Options) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if options.Preset != webtunnel.CloudflaredQuick {
		return "", errors.New("unexpected preset " + options.Preset)
	}
	return f.url, f.err
}

func writeLiveConfig(t *testing.T, repositories ...string) (configPath, statePath string) {
	t.Helper()
	dir := t.TempDir()
	configPath, statePath = filepath.Join(dir, "web.json"), filepath.Join(dir, "web-state.json")
	if _, err := config.UpdateWebConfig(configPath, ^uint64(0), func(cfg *config.WebConfig) error {
		cfg.Updates.Mode = config.WebUpdatesLive
		cfg.Updates.Live.Tunnel = webtunnel.CloudflaredQuick
		cfg.Updates.Live.Repositories = repositories
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return configPath, statePath
}

type liveHarness struct {
	*providerHarness
	ctl     *liveController
	hooks   *memoryHooks
	tunnel  *fakeTunnel
	state   string
	queued  *[]queuedRefresh
	logs    *bytes.Buffer
	nowLive *time.Time
}

func newLiveHarness(t *testing.T) *liveHarness {
	t.Helper()
	h := newProviderHarness(t, staticToken("tok-live"))
	configPath, statePath := writeLiveConfig(t, "Acme/Repo")
	hooks, tunnel := newMemoryHooks(), &fakeTunnel{}
	ctl := newLiveController(configPath, statePath, []byte(liveSecret), hooks, tunnel.source, h.syncer)
	now := time.Unix(1_900_000_000, 0).UTC()
	ctl.now = func() time.Time { return now }
	queued := []queuedRefresh{}
	var mu sync.Mutex
	ctl.queue = func(id string, scope refreshScope, force bool) {
		if scope != refreshProvider {
			t.Errorf("scope = %v", scope)
		}
		mu.Lock()
		queued = append(queued, queuedRefresh{id, force})
		mu.Unlock()
	}
	h.syncer.live = ctl
	var logs bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(previous) })
	if !ctl.load() {
		t.Fatal("live updates are off")
	}
	return &liveHarness{providerHarness: h, ctl: ctl, hooks: hooks, tunnel: tunnel, state: statePath, queued: &queued, logs: &logs, nowLive: &now}
}

func (h *liveHarness) advance(d time.Duration) { *h.nowLive = h.nowLive.Add(d) }

func (h *liveHarness) saved(t *testing.T) config.WebLiveState {
	t.Helper()
	state, err := config.ReadWebState(h.state)
	if err != nil {
		t.Fatal(err)
	}
	return state.Live
}

func TestLiveControllerKeepsHooksPointedAtTheTunnel(t *testing.T) {
	h := newLiveHarness(t)
	ctx := context.Background()

	// No public URL yet: nothing on GitHub, retried soon, and recorded why.
	h.tunnel.set("", errors.New("the tunnel has not printed its public URL yet"))
	if next := h.ctl.step(ctx); next != liveURLWait {
		t.Fatalf("next = %v", next)
	}
	if calls := h.hooks.take(); len(calls) != 0 {
		t.Fatalf("GitHub calls without a URL: %v", calls)
	}
	if saved := h.saved(t); saved.TunnelError == "" || saved.InstallID == "" {
		t.Fatalf("saved %+v", saved)
	}

	// The URL appears: the hook is created for the configured repository.
	h.tunnel.set("https://first.trycloudflare.com", nil)
	h.advance(time.Second)
	if next := h.ctl.step(ctx); next != liveURLWait {
		t.Fatalf("waiting hook next = %v", next)
	}
	saved := h.saved(t)
	repo, ok := saved.Repositories["Acme/Repo"]
	if !ok || repo.State != config.WebLiveStateWaiting || repo.HookID != 41 || saved.PublicURL != "https://first.trycloudflare.com" || saved.TunnelError != "" {
		t.Fatalf("saved %+v", saved)
	}
	if got := h.hooks.hookURL(41); got != livehooks.HookURL("https://first.trycloudflare.com", saved.InstallID) {
		t.Fatalf("hook url %s", got)
	}
	if h.ctl.healthy("acme/repo") {
		t.Fatal("healthy before the ping")
	}

	// The ping arrives: live, and the project gets the refresh that covers
	// the time before.
	h.ctl.observe(webhooks.LiveEvent{Event: "ping", RepositoryFullName: "acme/repo", HookID: 41})
	if !h.ctl.healthy("ACME/REPO") || h.ctl.healthy("acme/other") {
		t.Fatal("healthy() after the ping")
	}
	if len(*h.queued) != 1 || (*h.queued)[0] != (queuedRefresh{h.projectID, true}) {
		t.Fatalf("queued %+v", *h.queued)
	}
	if repo := h.saved(t).Repositories["Acme/Repo"]; repo.State != config.WebLiveStateLive || repo.LastPingAt.IsZero() {
		t.Fatalf("after ping %+v", repo)
	}
	// Untracked repositories are not recorded.
	h.ctl.observe(webhooks.LiveEvent{Event: "push", RepositoryFullName: "acme/other"})
	if _, _, ok := h.saved(t).Repository("acme/other"); ok {
		t.Fatal("untracked repository recorded")
	}

	// Same URL: no GitHub writes until the health interval.
	h.hooks.take()
	h.advance(liveURLPoll)
	if next := h.ctl.step(ctx); next != liveURLPoll {
		t.Fatalf("next = %v", next)
	}
	if calls := h.hooks.take(); len(calls) != 0 {
		t.Fatalf("calls %v", calls)
	}
	h.advance(liveHealthInterval)
	h.ctl.step(ctx)
	if calls := h.hooks.take(); strings.Join(calls, ",") != "get" {
		t.Fatalf("health check calls %v", calls)
	}

	// A failing hook (GitHub's last delivery failed) is pinged at the next
	// health check, and stays failing until a delivery proves it again.
	h.hooks.setLastResponse(41, 530, "Origin DNS error")
	h.advance(liveHealthInterval)
	h.ctl.step(ctx)
	if calls := h.hooks.take(); strings.Join(calls, ",") != "get,ping" || h.ctl.healthy("acme/repo") {
		t.Fatalf("failing hook: calls %v healthy %v", calls, h.ctl.healthy("acme/repo"))
	}
	if repo := h.saved(t).Repositories["Acme/Repo"]; repo.State != config.WebLiveStateFailing || !strings.Contains(repo.LastError, "HTTP 530") {
		t.Fatalf("failing %+v", repo)
	}
	*h.queued = nil
	h.ctl.observe(webhooks.LiveEvent{Event: "ping", RepositoryFullName: "acme/repo"})
	h.hooks.setLastResponse(41, 200, "OK")
	if !h.ctl.healthy("acme/repo") || len(*h.queued) != 1 {
		t.Fatalf("recovered: healthy %v queued %+v", h.ctl.healthy("acme/repo"), *h.queued)
	}

	// The tunnel restarts with a new URL: the hook is re-pointed and pinged.
	h.tunnel.set("https://second.trycloudflare.com", nil)
	h.advance(liveURLPoll)
	h.ctl.step(ctx)
	if calls := strings.Join(h.hooks.take(), ","); !strings.Contains(calls, "update") || !strings.Contains(calls, "ping") || strings.Contains(calls, "create") {
		t.Fatalf("calls %v", calls)
	}
	if got := h.hooks.hookURL(41); !strings.HasPrefix(got, "https://second.trycloudflare.com/github/webhook?bonsai=") {
		t.Fatalf("hook url %s", got)
	}
	if h.ctl.healthy("acme/repo") {
		t.Fatal("healthy while waiting for the new ping")
	}

	// No ping: pinged again after livePingRetry, at most livePingRetries times.
	pings := 0
	for i := 0; i < livePingRetries+2; i++ {
		h.advance(livePingRetry)
		h.ctl.step(ctx)
		for _, call := range h.hooks.take() {
			if call == "ping" {
				pings++
			}
		}
	}
	if pings != livePingRetries {
		t.Fatalf("retried %d pings", pings)
	}

	// A delivery proves the hook; the tunnel going away makes it unhealthy
	// (standard polling again) until it is back.
	h.ctl.observe(webhooks.LiveEvent{Event: "push", RepositoryFullName: "Acme/Repo"})
	if !h.ctl.healthy("acme/repo") {
		t.Fatal("not healthy after a delivery")
	}
	*h.queued = nil
	h.tunnel.set("", errors.New("the tunnel is failed; see bonsai web logs tunnel"))
	h.advance(liveURLPoll)
	if next := h.ctl.step(ctx); next != liveURLWait || h.ctl.healthy("acme/repo") {
		t.Fatalf("tunnel down: next %v healthy %v", next, h.ctl.healthy("acme/repo"))
	}
	view := h.ctl.view()
	if view.TunnelUp || !strings.Contains(view.TunnelError, "tunnel is failed") {
		t.Fatalf("view %+v", view)
	}
	h.tunnel.set("https://second.trycloudflare.com", nil)
	h.advance(liveURLWait)
	h.ctl.step(ctx)
	if !h.ctl.healthy("acme/repo") || len(*h.queued) != 1 {
		t.Fatalf("tunnel back: healthy %v queued %+v", h.ctl.healthy("acme/repo"), *h.queued)
	}

	// The secret never reaches GitHub calls' records, logs or the state file.
	raw, _ := os.ReadFile(h.state)
	for name, text := range map[string]string{"state": string(raw), "logs": h.logs.String()} {
		if strings.Contains(text, liveSecret) {
			t.Fatalf("secret in %s", name)
		}
	}
	if !strings.Contains(string(raw), livehooks.SecretFingerprint([]byte(liveSecret))) {
		t.Fatalf("no secret fingerprint in state:\n%s", raw)
	}
	for _, line := range []string{"live updates: public URL https://first.trycloudflare.com", "live updates: Acme/Repo is live"} {
		if !strings.Contains(h.logs.String(), line) {
			t.Fatalf("log lacks %q:\n%s", line, h.logs.String())
		}
	}
}

// A ping can arrive while the controller is still waiting for GitHub's
// answer to the create call; it must not be overwritten by waiting_for_ping.
func TestLiveControllerKeepsAPingThatRacesTheCreate(t *testing.T) {
	h := newLiveHarness(t)
	h.tunnel.set("https://first.trycloudflare.com", nil)
	racing := &pingDuringCreate{memoryHooks: h.hooks, observe: func() {
		h.ctl.observe(webhooks.LiveEvent{Event: "ping", RepositoryFullName: "acme/repo"})
	}, advance: func() { h.advance(time.Millisecond) }}
	h.ctl.hooks = racing
	h.ctl.step(context.Background())
	if repo := h.saved(t).Repositories["Acme/Repo"]; repo.State != config.WebLiveStateLive || repo.HookID == 0 {
		t.Fatalf("state %+v", repo)
	}
}

type pingDuringCreate struct {
	*memoryHooks
	observe, advance func()
}

func (p *pingDuringCreate) CreateHook(ctx context.Context, repo string, spec app.HookSpec) (app.Hook, error) {
	hook, err := p.memoryHooks.CreateHook(ctx, repo, spec)
	p.advance()
	p.observe()
	return hook, err
}

func TestLiveControllerStartupRefreshesEveryProject(t *testing.T) {
	h := newLiveHarness(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h.tunnel.set("", errors.New("not yet"))
	done := make(chan struct{})
	go func() {
		h.ctl.Run(ctx)
		close(done)
	}()
	// A forced provider job runs without any browser connected.
	waitFor(t, func() bool {
		h.syncer.mu.Lock()
		defer h.syncer.mu.Unlock()
		return !h.syncer.jobLocked(h.projectID).providerStartedAt.IsZero()
	})
	cancel()
	<-done
}

func TestLiveControllerOffInStandardMode(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "web.json")
	if _, _, err := config.EnsureWebConfig(configPath); err != nil {
		t.Fatal(err)
	}
	ctl := newLiveController(configPath, filepath.Join(dir, "web-state.json"), []byte(liveSecret), newMemoryHooks(), (&fakeTunnel{}).source, nil)
	if ctl.load() {
		t.Fatal("live controller on in standard mode")
	}
	if ctl.healthy("acme/repo") || (*liveController)(nil).healthy("acme/repo") {
		t.Fatal("healthy")
	}
	if ctl := newLiveController("", "", nil, nil, nil, nil); ctl.load() {
		t.Fatal("ran without paths")
	}
}

func TestPollProvidersSlowsForLiveProjects(t *testing.T) {
	h := newLiveHarness(t)
	h.syncer.SetFocus(1, h.projectID)
	h.tunnel.set("https://first.trycloudflare.com", nil)
	h.ctl.step(context.Background())
	h.ctl.observe(webhooks.LiveEvent{Event: "ping", RepositoryFullName: "acme/repo"})
	h.api.take()

	due := func() bool {
		h.syncer.mu.Lock()
		h.syncer.jobLocked(h.projectID).providerStartedAt = h.now.Add(-time.Minute)
		h.syncer.mu.Unlock()
		h.syncer.pollProviders()
		queued := false
		waitFor(t, func() bool {
			h.syncer.mu.Lock()
			defer h.syncer.mu.Unlock()
			j := h.syncer.jobLocked(h.projectID)
			queued = queued || j.providerRunning || j.providerStartedAt.Equal(*h.now)
			return !j.providerRunning
		})
		return queued
	}
	if due() {
		t.Fatal("a live project was polled at the visible cadence")
	}
	// Unhealthy (tunnel down): back to the standard cadence.
	h.tunnel.set("", errors.New("down"))
	h.ctl.step(context.Background())
	if !due() {
		t.Fatal("an unhealthy live project was not polled")
	}
}

func TestLiveControllerLeavesRemovedRepositoriesToSetup(t *testing.T) {
	h := newLiveHarness(t)
	ctx := context.Background()
	h.tunnel.set("https://first.trycloudflare.com", nil)
	h.ctl.step(ctx)
	first := h.saved(t)
	if first.UpdatedAt.IsZero() || first.Repositories["Acme/Repo"].HookID == 0 {
		t.Fatalf("the first step must write: %+v", first)
	}

	// Setup takes Acme/Repo off live updates and deletes its entry with its
	// hook; a repository someone removed from web.json by hand keeps its
	// entry, so doctor can still find the hook.
	if _, err := config.UpdateWebConfig(h.ctl.configPath, ^uint64(0), func(cfg *config.WebConfig) error {
		cfg.Updates.Live.Repositories = []string{"acme/next"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := config.UpdateWebState(h.state, func(s *config.WebState) error {
		delete(s.Live.Repositories, "Acme/Repo")
		s.Live.Repositories["by/hand"] = config.WebLiveRepository{HookID: 7, State: config.WebLiveStateLive}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	h.advance(time.Second)
	h.ctl.step(ctx)
	saved := h.saved(t)
	if _, _, ok := saved.Repository("acme/repo"); ok {
		t.Fatalf("the controller wrote back a repository setup removed: %+v", saved.Repositories)
	}
	if r := saved.Repositories["by/hand"]; r.HookID != 7 {
		t.Fatalf("an entry the controller does not own was dropped: %+v", saved.Repositories)
	}
	if r, ok := saved.Repositories["acme/next"]; !ok || r.HookID == 0 {
		t.Fatalf("the new repository has no hook: %+v", saved.Repositories)
	}
}

func TestLiveControllerNoticesSettingsQuickly(t *testing.T) {
	h := newLiveHarness(t)
	h.tunnel.set("https://first.trycloudflare.com", nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go h.ctl.Run(ctx)
	waitFor := func(what string, cond func() bool) {
		t.Helper()
		deadline := time.Now().Add(2 * liveConfigWatch)
		for !cond() {
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting for %s", what)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	waitFor("the first hook", func() bool { _, r, _ := h.saved(t).Repository("acme/repo"); return r.HookID != 0 })
	// Live with a known URL: the next step is liveURLPoll away. A web.json
	// edit must not wait for it.
	h.ctl.observe(webhooks.LiveEvent{Event: "ping", RepositoryFullName: "acme/repo"})
	time.Sleep(10 * time.Millisecond) // a distinct modification time
	if _, err := config.UpdateWebConfig(h.ctl.configPath, ^uint64(0), func(cfg *config.WebConfig) error {
		cfg.Updates.Live.Repositories = append(cfg.Updates.Live.Repositories, "acme/next")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	waitFor("the new repository's hook", func() bool { _, r, _ := h.saved(t).Repository("acme/next"); return r.HookID != 0 })
}
