package codex

import (
	"errors"
	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"reflect"
	"testing"
)

func TestGardenResolveLaunchPreservesExactCodexPairAndArgv(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	p := New(nil, nil)
	var _ agents.LaunchResolver = p
	for _, tc := range []struct {
		name, settings string
		override, want agents.LaunchOptions
	}{{"non preset inherited", `{"model":"gpt-6.1-sol","effort":"high","sandbox":"read-only","approval":"untrusted"}`, agents.LaunchOptions{}, agents.LaunchOptions{Model: "gpt-6.1-sol", Effort: "high", Sandbox: "read-only", Approval: "untrusted"}}, {"explicit pair replaces defaults", `{"model":"old","effort":"high","sandbox":"read-only","approval":"untrusted"}`, agents.LaunchOptions{Model: "new", Effort: "low", Sandbox: "workspace-write", Approval: "on-request"}, agents.LaunchOptions{Model: "new", Effort: "low", Sandbox: "workspace-write", Approval: "on-request"}}, {"bypass has no invented approval", `{"sandbox":"read-only","approval":"untrusted"}`, agents.LaunchOptions{Sandbox: SandboxBypass}, agents.LaunchOptions{Sandbox: SandboxBypass}}, {"unresolved stays unresolved", `{}`, agents.LaunchOptions{}, agents.LaunchOptions{}}} {
		t.Run(tc.name, func(t *testing.T) {
			a := agents.Account{ID: "acct_garden", Provider: ProviderID, Settings: []byte(tc.settings)}
			before := string(a.Settings)
			got, err := p.ResolveLaunch(a, tc.override)
			if err != nil || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("resolution=%+v want=%+v err=%v", got, tc.want, err)
			}
			settings, err := ParseSettings(a)
			if err != nil {
				t.Fatal(err)
			}
			if original, frozen := launchArgs(settings, nil, tc.override, nil), launchArgs(Settings{}, nil, got, nil); !reflect.DeepEqual(original, frozen) {
				t.Fatalf("argv changed: %q / %q", original, frozen)
			}
			if string(a.Settings) != before {
				t.Fatal("resolver mutated account defaults")
			}
		})
	}
}
func TestGardenResolveLaunchRejectsInvalidCodexOverrides(t *testing.T) {
	p := New(nil, nil)
	yes := true
	for _, override := range []agents.LaunchOptions{{FullAccess: &yes}, {PermissionMode: "default"}, {Prompt: "task"}, {DisplayName: "task"}, {ReadOnly: true}, {Model: "--model"}, {Effort: "bogus"}, {Sandbox: "unknown"}, {Approval: "unknown"}} {
		if _, err := p.ResolveLaunch(agents.Account{ID: "acct_garden", Provider: ProviderID}, override); !errors.Is(err, agents.ErrInvalidLaunch) {
			t.Fatalf("override=%+v err=%v", override, err)
		}
	}
	if _, err := p.ResolveLaunch(agents.Account{Settings: []byte(`{`)}, agents.LaunchOptions{}); !errors.Is(err, agents.ErrInvalidLaunch) {
		t.Fatal(err)
	}
}
