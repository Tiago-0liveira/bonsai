package localapi

import (
	"bytes"
	"encoding/json"
	"github.com/Tiago-0liveira/bonsai/internal/webtunnel"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/server/webhooks"
)

const liveSecret = "fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210"

func newLiveTestServer(t *testing.T) *Server {
	t.Helper()
	s, err := New(Config{
		ProjectRootsPath: t.TempDir() + "/project-roots.json",
		Address:          "127.0.0.1:7001",
		BrowserOrigin:    ProductionBrowserOrigin,
		WebhookAddress:   "127.0.0.1:7002",
		WebhookSecret:    []byte(liveSecret),
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func signedWebhook(t *testing.T, target, host, delivery, event, body string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(body))
	req.Host = host
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Delivery", delivery)
	req.Header.Set("X-GitHub-Event", event)
	req.Header.Set("X-Hub-Signature-256", webhooks.Sign([]byte(liveSecret), []byte(body)))
	return req
}

// The two listeners never share a route: API paths are 404 on the webhook
// port, and the API port does not serve the receiver.
func TestLiveReceiverAndAPIRoutesAreDisjoint(t *testing.T) {
	s := newLiveTestServer(t)
	var queued []string
	var mu sync.Mutex
	s.stateSync.liveQueue = func(id string, _ refreshScope, _ bool) {
		mu.Lock()
		defer mu.Unlock()
		queued = append(queued, id)
	}
	var logs bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(previous)

	receiver := s.live.Handler()
	const tunnelHost = "first-quiet-example-words.trycloudflare.com"
	for _, path := range []string{"/", "/healthz", "/version", "/app", "/app/settings", "/api/session", "/api/projects", "/api/settings/project-roots", "/ws", "/events"} {
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			req := httptest.NewRequest(method, "http://127.0.0.1:7002"+path, nil)
			req.Host = tunnelHost
			rec := httptest.NewRecorder()
			receiver.ServeHTTP(rec, req)
			if rec.Code != http.StatusNotFound {
				t.Errorf("webhook port %s %s = %d, want 404", method, path, rec.Code)
			}
		}
	}

	body := `{"ref":"refs/heads/main","repository":{"id":5,"full_name":"acme/repo"}}`
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, signedWebhook(t, "http://127.0.0.1:7001/github/webhook", "127.0.0.1:7001", "d1", "push", body))
	if rec.Code >= 200 && rec.Code < 300 {
		t.Fatalf("API port served the webhook route: %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	receiver.ServeHTTP(rec, signedWebhook(t, "http://127.0.0.1:7002/github/webhook?bonsai=install", tunnelHost, "d2", "push", body))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("receiver = %d %s", rec.Code, rec.Body)
	}
	if strings.Contains(logs.String(), liveSecret) {
		t.Fatalf("webhook secret logged:\n%s", logs.String())
	}
	if !strings.Contains(logs.String(), "live updates: push for acme/repo, 0 project(s)") {
		t.Fatalf("delivery not logged:\n%s", logs.String())
	}
}

func TestNewRejectsUnsafeWebhookReceiver(t *testing.T) {
	roots := t.TempDir() + "/project-roots.json"
	for name, cfg := range map[string]Config{
		"not loopback":  {WebhookAddress: "0.0.0.0:7002", WebhookSecret: []byte(liveSecret)},
		"same port":     {WebhookAddress: "127.0.0.1:7001", WebhookSecret: []byte(liveSecret)},
		"no secret":     {WebhookAddress: "127.0.0.1:7002"},
		"repo scoped":   {WebhookAddress: "127.0.0.1:7002", WebhookSecret: []byte(liveSecret), RepoDir: t.TempDir()},
		"bad address":   {WebhookAddress: "7002", WebhookSecret: []byte(liveSecret)},
		"public suffix": {WebhookAddress: "example.com:7002", WebhookSecret: []byte(liveSecret)},
	} {
		cfg.ProjectRootsPath = roots
		cfg.Address = "127.0.0.1:7001"
		cfg.BrowserOrigin = ProductionBrowserOrigin
		if _, err := New(cfg); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

type queuedRefresh struct {
	id    string
	force bool
}

func TestLiveEventsQueueMatchingProjects(t *testing.T) {
	h := newProviderHarness(t, staticToken("tok-live"))
	h.syncer.refreshProvider(h.projectID, false) // learn the repository ID (1)
	_ = h.api.take()
	var queued []queuedRefresh
	h.syncer.liveQueue = func(id string, scope refreshScope, force bool) {
		if scope != refreshProvider {
			t.Errorf("scope = %v", scope)
		}
		queued = append(queued, queuedRefresh{id, force})
	}
	for _, tc := range []struct {
		name  string
		event webhooks.LiveEvent
		want  []queuedRefresh
	}{
		{"by id", webhooks.LiveEvent{Event: "push", RepositoryID: 1, RepositoryFullName: "acme/renamed"}, []queuedRefresh{{h.projectID, true}}},
		{"by name", webhooks.LiveEvent{Event: "pull_request", Action: "opened", RepositoryFullName: "ACME/Repo"}, []queuedRefresh{{h.projectID, true}}},
		{"other repo", webhooks.LiveEvent{Event: "push", RepositoryID: 2, RepositoryFullName: "acme/other"}, nil},
		{"ping", webhooks.LiveEvent{Event: "ping", RepositoryID: 1, RepositoryFullName: "acme/repo", HookID: 9}, nil},
		{"checks", webhooks.LiveEvent{Event: "check_run", Action: "completed", RepositoryID: 1, RepositoryFullName: "acme/repo", HeadSHA: "abc123"}, []queuedRefresh{{h.projectID, false}}},
	} {
		queued = nil
		h.syncer.handleLiveEvent(tc.event)
		if len(queued) != len(tc.want) || (len(queued) > 0 && queued[0] != tc.want[0]) {
			t.Errorf("%s: queued %+v, want %+v", tc.name, queued, tc.want)
		}
	}

	// The checks event expired exactly that commit: the non-forced refresh
	// re-reads its checks and nothing else.
	h.syncer.refreshProvider(h.projectID, false)
	var resources []string
	for _, r := range h.api.take() {
		resources = append(resources, r.resource)
	}
	if strings.Join(resources, ",") != "check-runs,statuses" && strings.Join(resources, ",") != "statuses,check-runs" {
		t.Fatalf("refresh after a checks event read %v, want only that commit's checks", resources)
	}
	if n := h.syncer.providers.invalidateChecks("ffffff0"); n != 0 {
		t.Fatalf("unknown SHA expired %d entries", n)
	}
	if n := h.syncer.providers.invalidateChecks("ABC123"); n != 1 {
		t.Fatalf("SHA match is case-sensitive: %d", n)
	}

	snapshot, _ := json.Marshal(h.snapshot(t))
	if strings.Contains(string(snapshot), liveSecret) {
		t.Fatal("secret in browser projection")
	}
}

func TestTunnelURLComesFromTheCurrentRun(t *testing.T) {
	log := "[16:49:44] tunnel SYS started\n" +
		"[16:49:49] tunnel ERR INF |  https://old-run.trycloudflare.com  |\n" +
		"[16:52:20] tunnel SYS stopped\n" +
		"[16:52:26] tunnel SYS started\n" +
		"[16:52:26] tunnel ERR INF Requesting new quick Tunnel on trycloudflare.com...\n"
	if u, ok := webtunnel.URLFromLog(webtunnel.CloudflaredQuick, "", currentRun(log, webtunnel.SidecarName)); ok {
		t.Fatalf("took the previous run's URL %s", u)
	}
	log += "[16:52:33] tunnel ERR INF |  https://new-run.trycloudflare.com  |\n"
	if u, _ := webtunnel.URLFromLog(webtunnel.CloudflaredQuick, "", currentRun(log, webtunnel.SidecarName)); u != "https://new-run.trycloudflare.com" {
		t.Fatalf("url = %q", u)
	}
	// A log that starts mid-run (the tail cut the start line) is used whole.
	if got := currentRun("[1] tunnel ERR x\n", webtunnel.SidecarName); got != "[1] tunnel ERR x\n" {
		t.Fatalf("got %q", got)
	}
}
