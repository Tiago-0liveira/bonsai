package claude

import (
	"errors"
	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"reflect"
	"slices"
	"testing"
)

func TestGardenResolveLaunchKeepsClaudeNativeModes(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	p := New(nil, nil)
	var _ agents.LaunchResolver = p
	for _, tc := range []struct {
		name, settings string
		override, want agents.LaunchOptions
	}{{"inherited", `{"model":"opus","effort":"high","permission_mode":"acceptEdits"}`, agents.LaunchOptions{}, agents.LaunchOptions{Model: "opus", Effort: "high", PermissionMode: "acceptEdits"}}, {"explicit default", `{"permission_mode":"bypassPermissions"}`, agents.LaunchOptions{PermissionMode: "default"}, agents.LaunchOptions{PermissionMode: "default"}}, {"unresolved", `{}`, agents.LaunchOptions{}, agents.LaunchOptions{}}, {"override", `{"model":"opus","effort":"high","permission_mode":"plan"}`, agents.LaunchOptions{Model: "sonnet", Effort: "low", PermissionMode: "auto"}, agents.LaunchOptions{Model: "sonnet", Effort: "low", PermissionMode: "auto"}}} {
		t.Run(tc.name, func(t *testing.T) {
			a := agents.Account{ID: "acct_garden", Provider: ProviderID, Settings: []byte(tc.settings)}
			got, err := p.ResolveLaunch(a, tc.override)
			if err != nil || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("resolution=%+v want=%+v err=%v", got, tc.want, err)
			}
			settings, err := ParseSettings(a)
			if err != nil {
				t.Fatal(err)
			}
			original, _, err := launchArgs(settings, nil, tc.override, nil)
			if err != nil {
				t.Fatal(err)
			}
			frozen, _, err := launchArgs(Settings{}, nil, got, nil)
			if err != nil {
				t.Fatal(err)
			}
			stripSession := func(args []string) []string {
				if i := slices.Index(args, "--session-id"); i >= 0 {
					return append(append([]string{}, args[:i]...), args[i+2:]...)
				}
				return args
			}
			if !reflect.DeepEqual(stripSession(original), stripSession(frozen)) {
				t.Fatalf("argv changed: %q / %q", original, frozen)
			}
			if string(a.Settings) != tc.settings {
				t.Fatal("defaults mutated")
			}
		})
	}
}
func TestGardenResolveLaunchRejectsUndeclaredClaudeModeAndCrossProviderFields(t *testing.T) {
	p := New(nil, nil)
	yes := true
	for _, override := range []agents.LaunchOptions{{PermissionMode: "dontAsk"}, {PermissionMode: "invented"}, {Sandbox: "read-only"}, {Approval: "never"}, {FullAccess: &yes}, {Prompt: "task"}, {DisplayName: "task"}, {ReadOnly: true}, {Model: "--bad"}, {Effort: "bogus"}} {
		if _, err := p.ResolveLaunch(agents.Account{Provider: ProviderID}, override); !errors.Is(err, agents.ErrInvalidLaunch) {
			t.Fatalf("override=%+v err=%v", override, err)
		}
	}
}
