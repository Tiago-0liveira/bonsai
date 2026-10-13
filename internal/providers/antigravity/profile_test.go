package antigravity

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

func strPtr(s string) *string { return &s }

func TestProfileFieldsMirrorLaunchFields(t *testing.T) {
	fields := (&Provider{}).ProfileFields()
	if got := agents.FieldIDs(fields); !reflect.DeepEqual(got, []string{"model", "full_access"}) {
		t.Fatalf("ids = %v", got)
	}
	if fields[1].Kind != agents.LaunchFieldToggle || !reflect.DeepEqual(agents.FieldValues(fields[1]), []string{"false", "true"}) {
		t.Fatalf("full access = %+v", fields[1])
	}
}

// Every value a profile field declares is stored as settings ParseSettings
// accepts and changes the launch argv as a launch option would.
func TestProfileFieldValuesPassParseSettings(t *testing.T) {
	p := &Provider{}
	account := agents.Account{ID: "acct_x", Provider: ProviderID, Name: "work"}
	for _, field := range p.ProfileFields() {
		values := agents.FieldValues(field)
		if field.Kind == agents.LaunchFieldModel {
			values = []string{"gemini-3-pro"}
		}
		for _, value := range values {
			raw, err := p.UpdateProfile(account, map[string]*string{field.ID: strPtr(value)})
			if err != nil {
				t.Fatalf("%s=%s: %v", field.ID, value, err)
			}
			next := account
			next.Settings = raw
			if _, err := ParseSettings(next); err != nil {
				t.Fatalf("%s=%s: ParseSettings: %v", field.ID, value, err)
			}
			want := value
			if field.ID == "full_access" && value == "false" {
				want = "" // off is the unset state
			}
			if got := p.ProfileDefaults(next)[field.ID]; got != want {
				t.Fatalf("%s=%s reads back as %q", field.ID, value, got)
			}
		}
	}
}

func TestUpdateProfile(t *testing.T) {
	p := &Provider{}
	account := agents.Account{Settings: json.RawMessage(`{"model":"old","future":1}`)}
	raw, err := p.UpdateProfile(account, map[string]*string{"model": strPtr("new"), "full_access": strPtr("true")})
	if err != nil {
		t.Fatal(err)
	}
	next := agents.Account{Settings: raw}
	settings, _ := ParseSettings(next)
	if settings.Model != "new" || !settings.DangerouslySkipPermissions {
		t.Fatalf("settings = %+v", settings)
	}
	if args := invocationArgs(settings, nil); !reflect.DeepEqual(args, []string{"--model", "new", "--dangerously-skip-permissions"}) {
		t.Fatalf("argv = %v", args)
	}
	raw, err = p.UpdateProfile(next, map[string]*string{"model": nil, "full_access": strPtr("false")})
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `{"future":1}` {
		t.Fatalf("unset settings = %s", raw)
	}
	for name, changes := range map[string]map[string]*string{
		"unknown field":   {"effort": strPtr("high")},
		"raw key":         {"dangerously_skip_permissions": strPtr("true")},
		"bad full access": {"full_access": strPtr("yes")},
		"dash model":      {"model": strPtr("--evil")},
	} {
		if _, err := p.UpdateProfile(account, changes); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}
