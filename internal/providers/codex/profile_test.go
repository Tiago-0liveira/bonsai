package codex

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

func strPtr(s string) *string { return &s }

func settingsMap(t *testing.T, raw json.RawMessage) map[string]any {
	t.Helper()
	out := map[string]any{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatal(err)
		}
	}
	return out
}

func TestProfileFieldsMirrorLaunchFields(t *testing.T) {
	fields := (&Provider{}).ProfileFields()
	if got := agents.FieldIDs(fields); !reflect.DeepEqual(got, []string{"model", "permissions", "effort"}) {
		t.Fatalf("ids = %v", got)
	}
	if fields[0].Kind != agents.LaunchFieldModel || !fields[2].PerModel {
		t.Fatalf("fields = %+v", fields)
	}
	for _, field := range fields[1:] {
		if field.Default != "" || field.Options[0].Value != "" || field.Options[0].Label != agents.ProviderDefaultLabel {
			t.Fatalf("%s: %+v", field.ID, field)
		}
	}
}

// Every value a profile field declares is stored as settings ParseSettings
// accepts, and reads back as the same field value.
func TestProfileFieldValuesPassParseSettings(t *testing.T) {
	p := &Provider{}
	account := agents.Account{ID: "acct_x", Provider: ProviderID, Name: "work", Settings: json.RawMessage(`{"auth_mode":"api-key","seeded_from":"/seed"}`)}
	for _, field := range p.ProfileFields() {
		values := agents.FieldValues(field)
		if field.Kind == agents.LaunchFieldModel {
			values = []string{"gpt-5.5"}
		}
		for _, value := range values {
			raw, err := p.UpdateProfile(account, map[string]*string{field.ID: strPtr(value)})
			if err != nil {
				t.Fatalf("%s=%s: %v", field.ID, value, err)
			}
			next := account
			next.Settings = raw
			settings, err := ParseSettings(next)
			if err != nil {
				t.Fatalf("%s=%s: ParseSettings: %v", field.ID, value, err)
			}
			if settings.AuthMode != AuthAPIKey || settings.SeededFrom != "/seed" {
				t.Fatalf("%s=%s: sign-in settings changed: %+v", field.ID, value, settings)
			}
			if got := p.ProfileDefaults(next)[field.ID]; got != value {
				t.Fatalf("%s=%s reads back as %q", field.ID, value, got)
			}
		}
	}
}

func TestUpdateProfile(t *testing.T) {
	p := &Provider{}
	account := agents.Account{ID: "acct_x", Provider: ProviderID, Name: "work", Settings: json.RawMessage(`{"auth_mode":"device","seeded_from":"/seed","effort":"low","sandbox":"read-only","approval":"on-request"}`)}

	raw, err := p.UpdateProfile(account, map[string]*string{"model": strPtr("gpt-5.5"), "effort": strPtr("high"), "permissions": strPtr("bypass")})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"auth_mode": "device", "seeded_from": "/seed", "model": "gpt-5.5", "effort": "high", "sandbox": "bypass"}
	if got := settingsMap(t, raw); !reflect.DeepEqual(got, want) {
		t.Fatalf("settings = %v", got)
	}

	// nil and "" both unset; unsetting permissions removes sandbox and approval.
	raw, err = p.UpdateProfile(account, map[string]*string{"effort": nil, "permissions": strPtr("")})
	if err != nil {
		t.Fatal(err)
	}
	if got := settingsMap(t, raw); !reflect.DeepEqual(got, map[string]any{"auth_mode": "device", "seeded_from": "/seed"}) {
		t.Fatalf("unset settings = %v", got)
	}

	for name, changes := range map[string]map[string]*string{
		"unknown field":     {"sandbox": strPtr("read-only")},
		"auth field":        {"auth_mode": strPtr("chatgpt")},
		"bad effort":        {"effort": strPtr("extreme")},
		"bad preset":        {"permissions": strPtr("yolo")},
		"dash model":        {"model": strPtr("--evil")},
		"control in model":  {"model": strPtr("a\nb")},
		"effort injection":  {"effort": strPtr(`high" sandbox_mode="danger-full-access`)},
		"bypass + approval": {"model": strPtr("gpt-5.5")},
	} {
		target := account
		if name == "bypass + approval" {
			// Hand-written settings that ParseSettings rejects stay rejected.
			target.Settings = json.RawMessage(`{"sandbox":"bypass","approval":"never"}`)
		}
		if _, err := p.UpdateProfile(target, changes); err == nil {
			t.Errorf("%s accepted", name)
		}
	}

	// A preset repairs such settings: it replaces both keys.
	broken := account
	broken.Settings = json.RawMessage(`{"sandbox":"bypass","approval":"never"}`)
	raw, err = p.UpdateProfile(broken, map[string]*string{"permissions": strPtr("auto")})
	if err != nil {
		t.Fatal(err)
	}
	if got := settingsMap(t, raw); !reflect.DeepEqual(got, map[string]any{"sandbox": "workspace-write", "approval": "on-request"}) {
		t.Fatalf("repaired settings = %v", got)
	}
}

func TestProfileDefaults(t *testing.T) {
	p := &Provider{}
	account := agents.Account{Settings: json.RawMessage(`{"auth_mode":"api-key","model":"m","effort":"high","sandbox":"danger-full-access","approval":"never"}`)}
	if got := p.ProfileDefaults(account); !reflect.DeepEqual(got, map[string]string{"model": "m", "effort": "high", "permissions": "full-access"}) {
		t.Fatalf("defaults = %v", got)
	}
	// A pair that matches no preset has no permissions value.
	account.Settings = json.RawMessage(`{"sandbox":"workspace-write","approval":"never"}`)
	if got := p.ProfileDefaults(account); len(got) != 0 {
		t.Fatalf("defaults = %v", got)
	}
}

// With the model list cached, the effort must be one the model supports, as
// for a launch; without it any shipped level passes.
func TestUpdateProfileChecksCachedModelEfforts(t *testing.T) {
	now := time.Now()
	p := &Provider{now: func() time.Time { return now }}
	account := agents.Account{ID: "acct_x", Provider: ProviderID, Name: "work"}
	if _, err := p.UpdateProfile(account, map[string]*string{"model": strPtr("gpt-6-luna"), "effort": strPtr("ultra")}); err != nil {
		t.Fatalf("uncached: %v", err)
	}
	p.modelCache = map[agents.AccountID]modelsEntry{account.ID: {at: now, ok: true, models: []agents.ModelOption{
		{ID: "gpt-6.1-sol", Default: true, Efforts: []string{"low", "high", "ultra"}},
		{ID: "gpt-6-luna", Efforts: []string{"low", "high"}},
	}}}
	_, err := p.UpdateProfile(account, map[string]*string{"model": strPtr("gpt-6-luna"), "effort": strPtr("ultra")})
	if err == nil || !strings.Contains(err.Error(), "not supported by this model") {
		t.Fatalf("err = %v", err)
	}
	// The default model applies when the profile sets none.
	if _, err := p.UpdateProfile(account, map[string]*string{"effort": strPtr("ultra")}); err != nil {
		t.Fatalf("default model: %v", err)
	}
	stored := account
	stored.Settings = json.RawMessage(`{"model":"gpt-6-luna"}`)
	if _, err := p.UpdateProfile(stored, map[string]*string{"effort": strPtr("ultra")}); err == nil {
		t.Fatal("effort checked against the default model instead of the stored one")
	}
}
