package antigravity

import (
	"errors"
	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"reflect"
	"testing"
)

func TestGardenResolveLaunchDistinguishesExplicitFalseAntigravityAccess(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	p := New(nil, nil)
	var _ agents.LaunchResolver = p
	yes, no := true, false
	for _, tc := range []struct {
		name, settings string
		override       agents.LaunchOptions
		access         bool
	}{{"known native default", `{}`, agents.LaunchOptions{}, false}, {"inherited true", `{"dangerously_skip_permissions":true}`, agents.LaunchOptions{}, true}, {"false overrides true", `{"dangerously_skip_permissions":true}`, agents.LaunchOptions{FullAccess: &no}, false}, {"explicit true", `{}`, agents.LaunchOptions{FullAccess: &yes}, true}} {
		t.Run(tc.name, func(t *testing.T) {
			a := agents.Account{ID: "acct_garden", Provider: ProviderID, Settings: []byte(tc.settings)}
			got, err := p.ResolveLaunch(a, tc.override)
			if err != nil || got.FullAccess == nil || *got.FullAccess != tc.access || got.Sandbox != "" || got.Approval != "" || got.PermissionMode != "" || got.Prompt != "" {
				t.Fatalf("resolution=%+v err=%v", got, err)
			}
			if string(a.Settings) != tc.settings {
				t.Fatal("defaults mutated")
			}
			*got.FullAccess = !*got.FullAccess
			again, err := p.ResolveLaunch(a, tc.override)
			if err != nil || *again.FullAccess != tc.access {
				t.Fatalf("pointer alias changed defaults: %+v %v", again, err)
			}
		})
	}
	a := agents.Account{ID: "acct_garden", Provider: ProviderID, Settings: []byte(`{"model":"gemini-3.8-flash","effort":"medium"}`)}
	got, err := p.ResolveLaunch(a, agents.LaunchOptions{Effort: "low"})
	if err != nil {
		t.Fatal(err)
	}
	if got.Model != "gemini-3.8-flash" || got.Effort != "low" || !reflect.DeepEqual(got.FullAccess, &no) {
		t.Fatalf("model/effort lost: %+v", got)
	}
}
func TestGardenResolveLaunchRejectsAntigravityCrossProviderAndInvalidFields(t *testing.T) {
	p := New(nil, nil)
	for _, override := range []agents.LaunchOptions{{Sandbox: "read-only"}, {Approval: "never"}, {PermissionMode: "default"}, {Prompt: "task"}, {DisplayName: "task"}, {ReadOnly: true}, {Model: "--bad"}, {Effort: "-x"}} {
		if _, err := p.ResolveLaunch(agents.Account{Provider: ProviderID}, override); !errors.Is(err, agents.ErrInvalidLaunch) {
			t.Fatalf("override=%+v err=%v", override, err)
		}
	}
	if _, err := p.ResolveLaunch(agents.Account{Settings: []byte(`{`)}, agents.LaunchOptions{}); !errors.Is(err, agents.ErrInvalidLaunch) {
		t.Fatal(err)
	}
}
