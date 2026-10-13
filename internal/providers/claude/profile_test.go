package claude

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

func strPtr(s string) *string { return &s }

func TestProfileFieldsMirrorLaunchFields(t *testing.T) {
	fields := (&Provider{}).ProfileFields()
	if got := agents.FieldIDs(fields); !reflect.DeepEqual(got, []string{"model", "permission_mode", "effort"}) {
		t.Fatalf("ids = %v", got)
	}
	for _, field := range fields[1:] {
		if field.Options[0].Value != "" || field.Options[0].Label != agents.ProviderDefaultLabel {
			t.Fatalf("%s: %+v", field.ID, field.Options[0])
		}
	}
}

// Every value a profile field declares is stored as settings ParseSettings
// accepts, reads back unchanged and leaves the sign-in settings alone.
func TestProfileFieldValuesPassParseSettings(t *testing.T) {
	p := &Provider{}
	account := agents.Account{ID: "acct_x", Provider: ProviderID, Name: "work", Settings: json.RawMessage(`{"auth_mode":"token","seeded_from":"/seed"}`)}
	for _, field := range p.ProfileFields() {
		values := agents.FieldValues(field)
		if field.Kind == agents.LaunchFieldModel {
			values = []string{"opus"}
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
			if settings.AuthMode != AuthToken || settings.SeededFrom != "/seed" {
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
	account := agents.Account{Settings: json.RawMessage(`{"auth_mode":"login","seeded_from":"/seed","effort":"low","permission_mode":"plan"}`)}
	raw, err := p.UpdateProfile(account, map[string]*string{"model": strPtr("sonnet"), "effort": nil, "permission_mode": strPtr("")})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	_ = json.Unmarshal(raw, &got)
	if !reflect.DeepEqual(got, map[string]any{"auth_mode": "login", "seeded_from": "/seed", "model": "sonnet"}) {
		t.Fatalf("settings = %v", got)
	}
	for name, changes := range map[string]map[string]*string{
		"unknown field":   {"sandbox": strPtr("read-only")},
		"auth field":      {"auth_mode": strPtr("token")},
		"seed field":      {"seeded_from": strPtr("/elsewhere")},
		"bad effort":      {"effort": strPtr("ultra")},
		"bad permissions": {"permission_mode": strPtr("yolo")},
		"dash model":      {"model": strPtr("--evil")},
	} {
		if _, err := p.UpdateProfile(account, changes); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}
