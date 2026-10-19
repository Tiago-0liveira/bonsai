package localapi

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
)

func TestAgentSettingsRevisionsAndConflicts(t *testing.T) {
	s := newTestServer(t)
	unavailable := authorizedRequest(t, s, "GET", "/api/settings/agents", "", nil)
	if unavailable.Code != 503 {
		t.Fatalf("without web.json: %d %s", unavailable.Code, unavailable.Body)
	}
	s.webConfigPath = filepath.Join(t.TempDir(), "web.json")
	read := func() agentSettingsResponse {
		t.Helper()
		w := authorizedRequest(t, s, "GET", "/api/settings/agents", "", nil)
		var out agentSettingsResponse
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil {
			t.Fatalf("read: %d %s", w.Code, w.Body)
		}
		return out
	}
	initial := read()
	if initial.KeepRunning || initial.ActiveSessions != 0 {
		t.Fatalf("default %+v", initial)
	}
	put := authorizedRequest(t, s, "POST", "/api/settings/agents", "on", map[string]any{"keep_running": true, "revision": initial.Revision})
	var changed agentSettingsResponse
	if put.Code != 200 || json.Unmarshal(put.Body.Bytes(), &changed) != nil || !changed.KeepRunning || changed.Revision != initial.Revision+1 {
		t.Fatalf("put: %d %s", put.Code, put.Body)
	}
	if cfg, _, err := config.ReadWebConfig(s.webConfigPath); err != nil || !cfg.Agents.KeepRunning {
		t.Fatalf("saved %+v %v", cfg.Agents, err)
	}
	if stale := authorizedRequest(t, s, "POST", "/api/settings/agents", "stale", map[string]any{"keep_running": false, "revision": initial.Revision}); stale.Code != 409 {
		t.Fatalf("stale revision: %d %s", stale.Code, stale.Body)
	}
	if missing := authorizedRequest(t, s, "POST", "/api/settings/agents", "missing", map[string]any{"keep_running": false}); missing.Code != 400 {
		t.Fatalf("missing revision: %d %s", missing.Code, missing.Body)
	}
	if !read().KeepRunning {
		t.Fatal("rejected writes changed the setting")
	}
}

func TestAgentSettingsNotifications(t *testing.T) {
	s := newTestServer(t)
	s.webConfigPath = filepath.Join(t.TempDir(), "web.json")
	read := func() agentSettingsResponse {
		t.Helper()
		w := authorizedRequest(t, s, "GET", "/api/settings/agents", "", nil)
		var out agentSettingsResponse
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil {
			t.Fatalf("read: %d %s", w.Code, w.Body)
		}
		return out
	}
	initial := read()
	if initial.Notifications.Enabled || !initial.Notifications.NeedsInput || !initial.Notifications.TurnComplete {
		t.Fatalf("default %+v", initial.Notifications)
	}
	put := authorizedRequest(t, s, "POST", "/api/settings/agents", "n1", map[string]any{"notifications": map[string]any{"enabled": true, "turn_complete": false}, "revision": initial.Revision})
	var changed agentSettingsResponse
	if put.Code != 200 || json.Unmarshal(put.Body.Bytes(), &changed) != nil {
		t.Fatalf("put: %d %s", put.Code, put.Body)
	}
	want := config.WebAgentNotifications{Enabled: true, NeedsInput: true, TurnComplete: false}
	if changed.Notifications != want || changed.KeepRunning {
		t.Fatalf("changed %+v", changed)
	}
	if cfg, _, err := config.ReadWebConfig(s.webConfigPath); err != nil || cfg.Agents.Notifications != want {
		t.Fatalf("saved %+v %v", cfg.Agents, err)
	}
	if empty := authorizedRequest(t, s, "POST", "/api/settings/agents", "n2", map[string]any{"revision": changed.Revision}); empty.Code != 400 {
		t.Fatalf("no field: %d %s", empty.Code, empty.Body)
	}
	if unknown := authorizedRequest(t, s, "POST", "/api/settings/agents", "n3", map[string]any{"notifications": map[string]any{"sound": true}, "revision": changed.Revision}); unknown.Code != 400 {
		t.Fatalf("unknown key: %d %s", unknown.Code, unknown.Body)
	}
}
