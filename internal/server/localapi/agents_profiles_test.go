//go:build linux || darwin

package localapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	gitlocal "github.com/Tiago-0liveira/bonsai/internal/git/local"
)

// editableTestProvider stores "model" and "mode" defaults as plain settings
// keys, and fails its provider-side cleanup on removal.
type editableTestProvider struct {
	describedTestProvider
}

func (p *editableTestProvider) ProfileFields() []agents.LaunchField {
	return agents.ProfileFieldsFrom([]agents.LaunchField{{ID: "mode", Label: "Mode", Kind: agents.LaunchFieldSelect, Options: []agents.FieldOption{{Value: "", Label: "Profile default"}, {Value: "fast", Label: "Fast"}, {Value: "safe", Label: "Safe"}}}})
}
func (p *editableTestProvider) ProfileDefaults(account agents.Account) map[string]string {
	out := map[string]string{}
	var settings map[string]any
	_ = json.Unmarshal(account.Settings, &settings)
	for _, key := range []string{"model", "mode"} {
		if value, ok := settings[key].(string); ok {
			out[key] = value
		}
	}
	return out
}
func (p *editableTestProvider) UpdateProfile(account agents.Account, changes map[string]*string) (json.RawMessage, error) {
	merged := map[string]any{}
	for id, value := range changes {
		merged[id] = nil
		if value == nil || *value == "" {
			continue
		}
		if id == "mode" && *value != "fast" && *value != "safe" {
			return nil, fmt.Errorf("unsupported mode %q (use fast, safe)", *value)
		}
		merged[id] = *value
	}
	return agents.MergeSettings(account.Settings, merged)
}
func (p *editableTestProvider) RemoveAccount(context.Context, agents.Account) error {
	return errors.New("logout failed")
}

// profileTestServer adds an account service and an editable provider with a
// profile Work (holding a secret-looking sign-in setting) and a profile Taken.
func profileTestServer(t *testing.T) (*Server, agents.AccountID) {
	t.Helper()
	s, _, _ := terminalTestServer(t)
	runtime := s.agents.runtime
	runtime.AccountService = &agents.AccountService{Store: runtime.Accounts, Sessions: runtime.Sessions, Registry: runtime.Registry}
	if err := runtime.Registry.Register(&editableTestProvider{describedTestProvider: describedTestProvider{id: "editable", available: agents.Availability{Available: true}}}); err != nil {
		t.Fatal(err)
	}
	id, _ := agents.NewAccountID()
	if err := runtime.Accounts.Create(agents.Account{ID: id, Provider: "editable", Name: "Work", Settings: json.RawMessage(`{"auth_mode":"SETTINGS-MARKER","mode":"safe"}`)}); err != nil {
		t.Fatal(err)
	}
	other, _ := agents.NewAccountID()
	if err := runtime.Accounts.Create(agents.Account{ID: other, Provider: "editable", Name: "Taken"}); err != nil {
		t.Fatal(err)
	}
	return s, id
}

func decodeProfile(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("%v: %s", err, body)
	}
	return out
}

func apiErrorCode(t *testing.T, body []byte) string {
	t.Helper()
	var out struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(body, &out)
	return out.Error.Code
}

func TestAgentProvidersDeclareProfileFields(t *testing.T) {
	s, _ := profileTestServer(t)
	w := agentAPIRequest(t, s, "GET", "/api/agents/providers", "", "")
	var providers []struct {
		ID            agents.ProviderID    `json:"id"`
		ProfileFields []agents.LaunchField `json:"profile_fields"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &providers); err != nil {
		t.Fatal(err)
	}
	for _, p := range providers {
		switch p.ID {
		case "editable":
			if got := agents.FieldIDs(p.ProfileFields); !slices.Equal(got, []string{"model", "mode"}) || p.ProfileFields[0].Kind != "model" || p.ProfileFields[1].Options[0].Label != agents.ProviderDefaultLabel {
				t.Fatalf("profile fields = %+v", p.ProfileFields)
			}
		default:
			if p.ProfileFields != nil {
				t.Fatalf("%s declares profile fields without an editor", p.ID)
			}
		}
	}
}

func TestAgentAccountGetAndPatch(t *testing.T) {
	s, id := profileTestServer(t)
	path := "/api/agents/accounts/" + string(id)

	w := agentAPIRequest(t, s, "GET", path, "", "")
	if w.Code != 200 {
		t.Fatalf("get: %d %s", w.Code, w.Body.String())
	}
	profile := decodeProfile(t, w.Body.Bytes())
	if profile["name"] != "Work" || profile["identity"] != "someone" || !reflect.DeepEqual(profile["defaults"], map[string]any{"mode": "safe"}) {
		t.Fatalf("profile = %v", profile)
	}
	if strings.Contains(w.Body.String(), "SETTINGS-MARKER") || strings.Contains(w.Body.String(), "settings") {
		t.Fatalf("raw settings leaked: %s", w.Body.String())
	}
	// The list carries the same defaults.
	if w := agentAPIRequest(t, s, "GET", "/api/agents/accounts", "", ""); !strings.Contains(w.Body.String(), `"defaults":{"mode":"safe"}`) {
		t.Fatalf("list = %s", w.Body.String())
	}

	etag := agentAPIRequest(t, s, "GET", "/api/agents/accounts", "", "").Header().Get("ETag")
	subscription, events := s.eventHub.subscribe()
	defer s.eventHub.unsubscribe(subscription)

	w = agentAPIRequest(t, s, "PATCH", path, "", `{"name":"Renamed","defaults":{"model":"m-2","mode":null}}`)
	if w.Code != 200 {
		t.Fatalf("patch: %d %s", w.Code, w.Body.String())
	}
	profile = decodeProfile(t, w.Body.Bytes())
	if profile["name"] != "Renamed" || !reflect.DeepEqual(profile["defaults"], map[string]any{"model": "m-2"}) || profile["identity"] != "someone" {
		t.Fatalf("patched = %v", profile)
	}
	stored, _ := s.agents.runtime.Accounts.Get(id)
	var settings map[string]any
	_ = json.Unmarshal(stored.Settings, &settings)
	if !reflect.DeepEqual(settings, map[string]any{"auth_mode": "SETTINGS-MARKER", "model": "m-2"}) {
		t.Fatalf("stored settings = %v", settings)
	}
	select {
	case event := <-events:
		if event.Type != agentsChangedEvent {
			t.Fatalf("event = %+v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("no agents_changed after PATCH")
	}
	w = agentAPIRequestHeaders(t, s, "GET", "/api/agents/accounts", map[string]string{"If-None-Match": etag})
	if w.Code != 200 || w.Header().Get("ETag") == etag {
		t.Fatalf("ETag unchanged after PATCH: %d", w.Code)
	}

	// Defaults alone, and a name alone.
	if w := agentAPIRequest(t, s, "PATCH", path, "", `{"defaults":{"mode":"fast"}}`); w.Code != 200 || !strings.Contains(w.Body.String(), `"mode":"fast"`) {
		t.Fatalf("defaults only: %d %s", w.Code, w.Body.String())
	}
	if w := agentAPIRequest(t, s, "PATCH", path, "", `{"name":"Work"}`); w.Code != 200 || decodeProfile(t, w.Body.Bytes())["name"] != "Work" {
		t.Fatalf("name only: %d %s", w.Code, w.Body.String())
	}
}

func TestAgentAccountPatchRejections(t *testing.T) {
	s, id := profileTestServer(t)
	path := "/api/agents/accounts/" + string(id)
	before, _ := s.agents.runtime.Accounts.Get(id)
	long := strings.Repeat("x", 300)
	for _, tc := range []struct {
		name, path, body string
		status           int
		code             string
	}{
		{"empty body", path, `{}`, 400, "invalid"},
		{"empty defaults", path, `{"defaults":{}}`, 400, "invalid"},
		{"unknown body field", path, `{"settings":{"mode":"fast"}}`, 400, "invalid_json"},
		{"not a string", path, `{"defaults":{"mode":1}}`, 400, "invalid_json"},
		{"empty name", path, `{"name":""}`, 400, "invalid_name"},
		{"bad name", path, `{"name":"-dash"}`, 400, "invalid_name"},
		{"long name", path, `{"name":"` + strings.Repeat("a", 65) + `"}`, 400, "invalid_name"},
		{"name taken", path, `{"name":"Taken"}`, 409, "name_taken"},
		{"unknown default", path, `{"defaults":{"auth_mode":"login"}}`, 400, "invalid_profile"},
		{"bad value", path, `{"name":"Other","defaults":{"mode":"reckless"}}`, 400, "invalid_profile"},
		{"long value", path, `{"defaults":{"model":"` + long + `"}}`, 400, "invalid"},
		{"unknown profile", "/api/agents/accounts/acct_missing", `{"name":"x"}`, 404, "not_found"},
	} {
		w := agentAPIRequest(t, s, "PATCH", tc.path, "", tc.body)
		if w.Code != tc.status || apiErrorCode(t, w.Body.Bytes()) != tc.code {
			t.Errorf("%s: %d %s", tc.name, w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "SETTINGS-MARKER") {
			t.Errorf("%s: settings leaked: %s", tc.name, w.Body.String())
		}
	}
	// The provider's own message is passed through.
	w := agentAPIRequest(t, s, "PATCH", path, "", `{"defaults":{"mode":"reckless"}}`)
	if !strings.Contains(w.Body.String(), `unsupported mode \"reckless\" (use fast, safe)`) {
		t.Fatalf("message = %s", w.Body.String())
	}
	after, _ := s.agents.runtime.Accounts.Get(id)
	if after.Name != before.Name || string(after.Settings) != string(before.Settings) || !after.UpdatedAt.Equal(before.UpdatedAt) {
		t.Fatalf("rejected PATCH changed the profile: %+v", after)
	}
	// Profiles of providers without an editor can be renamed, not given defaults.
	fixture, err := s.agents.runtime.Accounts.GetByName("antigravity", "Fixture")
	if err != nil {
		t.Fatal(err)
	}
	if w := agentAPIRequest(t, s, "PATCH", "/api/agents/accounts/"+string(fixture.ID), "", `{"defaults":{"model":"m"}}`); w.Code != 400 || apiErrorCode(t, w.Body.Bytes()) != "invalid_profile" {
		t.Fatalf("no editor: %d %s", w.Code, w.Body.String())
	}
	if w := agentAPIRequest(t, s, "PATCH", "/api/agents/accounts/"+string(fixture.ID), "", `{"name":"Fixture2"}`); w.Code != 200 {
		t.Fatalf("rename without editor: %d %s", w.Code, w.Body.String())
	}
}

func TestAgentAccountDeleteGuardsActiveSessions(t *testing.T) {
	s, _ := profileTestServer(t)
	runtime := s.agents.runtime
	// A provider that runs sessions and whose cleanup fails.
	provider := &removableTestProvider{}
	if err := runtime.Registry.Register(provider); err != nil {
		t.Fatal(err)
	}
	id, _ := agents.NewAccountID()
	if err := runtime.Accounts.Create(agents.Account{ID: id, Provider: provider.ID(), Name: "Busy"}); err != nil {
		t.Fatal(err)
	}
	path := "/api/agents/accounts/" + string(id)
	dir := s.registry.Default().info.Path
	tree := gitlocal.ID(localRepositoryID, dir)
	first, err := s.agents.manager.Start("repo", tree, id, "", dir, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	// A session of the same profile in another project counts too.
	second, err := s.agents.manager.Start("elsewhere", "tree-2", id, "", dir, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	if w := agentAPIRequest(t, s, "DELETE", path, "", `{}`); w.Code != 400 {
		t.Fatalf("body accepted: %d", w.Code)
	}
	w := agentAPIRequest(t, s, "DELETE", path, "", "")
	var conflict struct {
		Error struct {
			Code       string   `json:"code"`
			SessionIDs []string `json:"session_ids"`
		} `json:"error"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &conflict)
	if w.Code != 409 || conflict.Error.Code != "profile_in_use" || !slices.Equal(conflict.Error.SessionIDs, []string{first.ID, second.ID}) {
		t.Fatalf("in use: %d %s", w.Code, w.Body.String())
	}
	if _, err := runtime.Accounts.Get(id); err != nil {
		t.Fatalf("profile removed while in use: %v", err)
	}

	for _, session := range []struct{ project, id string }{{"repo", first.ID}, {"elsewhere", second.ID}} {
		if err := s.agents.manager.Stop(session.project, session.id); err != nil {
			t.Fatal(err)
		}
	}
	waitFor(t, func() bool { return len(s.agents.manager.ActiveSessions(id)) == 0 })
	subscription, events := s.eventHub.subscribe()
	defer s.eventHub.unsubscribe(subscription)
	w = agentAPIRequest(t, s, "DELETE", path, "", "")
	var removed struct {
		Warnings []string `json:"warnings"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &removed)
	if w.Code != 200 || len(removed.Warnings) != 1 || !strings.Contains(removed.Warnings[0], "provider cleanup failed: logout failed") {
		t.Fatalf("remove: %d %s", w.Code, w.Body.String())
	}
	if _, err := runtime.Accounts.Get(id); !errors.Is(err, agents.ErrAccountNotFound) {
		t.Fatalf("profile kept: %v", err)
	}
	// Session stops publish project updates asynchronously, so they can still
	// arrive after subscribing. Wait for the deletion's agents_changed event.
	timeout := time.NewTimer(time.Second)
	defer timeout.Stop()
waitForDeletion:
	for {
		select {
		case event := <-events:
			switch event.Type {
			case agentsChangedEvent:
				break waitForDeletion
			case "project_update":
				continue
			default:
				t.Fatalf("event = %+v", event)
			}
		case <-timeout.C:
			t.Fatal("no agents_changed after DELETE")
		}
	}
	if w := agentAPIRequest(t, s, "DELETE", path, "", ""); w.Code != 404 {
		t.Fatalf("second delete: %d", w.Code)
	}
}

// removableTestProvider runs sessions like terminalTestProvider and fails its
// provider-side cleanup.
type removableTestProvider struct {
	terminalTestProvider
}

func (p *removableTestProvider) ID() agents.ProviderID { return "removable" }
func (p *removableTestProvider) RemoveAccount(context.Context, agents.Account) error {
	return errors.New("logout failed")
}

func TestAgentAccountDeleteWithoutWarnings(t *testing.T) {
	s, _ := profileTestServer(t)
	fixture, err := s.agents.runtime.Accounts.GetByName("antigravity", "Fixture")
	if err != nil {
		t.Fatal(err)
	}
	w := agentAPIRequest(t, s, "DELETE", "/api/agents/accounts/"+string(fixture.ID), "", "")
	if w.Code != 200 || strings.TrimSpace(w.Body.String()) != `{"warnings":[]}` {
		t.Fatalf("remove: %d %s", w.Code, w.Body.String())
	}
}
