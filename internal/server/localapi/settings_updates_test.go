package localapi

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/livehooks"
	"github.com/Tiago-0liveira/bonsai/internal/webtunnel"
)

func TestUpdatesSettingsEndpoint(t *testing.T) {
	standard := authorizedRequest(t, newTestServer(t), "GET", "/api/settings/updates", "", nil)
	if standard.Code != 200 || !strings.Contains(standard.Body.String(), `"mode":"standard"`) || !strings.Contains(standard.Body.String(), `"live":null`) || !strings.Contains(standard.Body.String(), `"standard_interval_seconds":30`) {
		t.Fatalf("standard: %d %s", standard.Code, standard.Body)
	}

	s := newLiveTestServer(t)
	s.liveHooks.mu.Lock()
	s.liveHooks.options = webtunnel.Options{Preset: webtunnel.CloudflaredQuick}
	s.liveHooks.repos = []string{"acme/repo", "acme/new"}
	s.liveHooks.tunnelUp = true
	s.liveHooks.state = config.WebLiveState{
		InstallID: "0123456789abcdef",
		PublicURL: "https://first.trycloudflare.com",
		Repositories: map[string]config.WebLiveRepository{
			"acme/repo": {HookID: 41, HookURL: livehooks.HookURL("https://first.trycloudflare.com", "0123456789abcdef"), SecretFingerprint: "feedface", State: config.WebLiveStateLive, LastPingAt: time.Unix(1_900_000_000, 0).UTC()},
		},
	}
	s.liveHooks.mu.Unlock()
	w := authorizedRequest(t, s, "GET", "/api/settings/updates", "", nil)
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	body := w.Body.String()
	for _, leak := range []string{liveSecret, "bonsai=", "0123456789abcdef", "feedface", "https://"} {
		if strings.Contains(body, leak) {
			t.Fatalf("response contains %q: %s", leak, body)
		}
	}
	var got updatesSettingsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Mode != "live" || got.Live == nil || got.Live.PublicHost != "first.trycloudflare.com" || !got.Live.TunnelUp || got.Live.SafetyPollSeconds != 600 || len(got.Live.Repositories) != 2 {
		t.Fatalf("%+v", got)
	}
	if r := got.Live.Repositories[0]; r.FullName != "acme/repo" || r.State != "live" || !r.Healthy || r.LastPingAt == nil || r.ProjectIDs == nil {
		t.Fatalf("repo %+v", r)
	}
	if r := got.Live.Repositories[1]; r.State != "pending" || r.Healthy {
		t.Fatalf("new repo %+v", r)
	}

	// Session required, like every other settings route.
	req := httptest.NewRequest("GET", "http://127.0.0.1:7001/api/settings/updates", nil)
	req.Header.Set("Origin", ProductionBrowserOrigin)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != 401 && rec.Code != 403 {
		t.Fatalf("no session: %d", rec.Code)
	}
}
