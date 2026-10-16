package agents_test

import (
	"reflect"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/providers/antigravity"
	"github.com/Tiago-0liveira/bonsai/internal/providers/claude"
	"github.com/Tiago-0liveira/bonsai/internal/providers/codex"
)

func TestLaunchOptionsFromRoundTripsProviderFields(t *testing.T) {
	tests := []struct {
		name   string
		fields []agents.LaunchField
		launch agents.LaunchOptions
		values map[string]string // form values producing launch
	}{
		{"claude", claude.New(nil, nil).LaunchFields(),
			agents.LaunchOptions{PermissionMode: "plan", Effort: "high"},
			map[string]string{"permission_mode": "plan", "effort": "high"}},
		{"claude defaults", claude.New(nil, nil).LaunchFields(), agents.LaunchOptions{}, map[string]string{"permission_mode": "", "effort": ""}},
		{"codex auto", codex.New(nil, nil).LaunchFields(),
			agents.LaunchOptions{Sandbox: "workspace-write", Approval: "on-request", Effort: "low"},
			map[string]string{"permissions": "auto", "effort": "low"}},
		{"codex full access", codex.New(nil, nil).LaunchFields(),
			agents.LaunchOptions{Sandbox: "danger-full-access", Approval: "never"},
			map[string]string{"permissions": "full-access", "effort": ""}},
		{"codex bypass", codex.New(nil, nil).LaunchFields(),
			agents.LaunchOptions{Sandbox: codex.SandboxBypass},
			map[string]string{"permissions": "bypass"}},
		{"antigravity on", antigravity.New(nil, nil).LaunchFields(),
			agents.LaunchOptions{FullAccess: ptr(true), Effort: "medium"},
			map[string]string{"effort": "medium", "full_access": "true"}},
		{"antigravity off", antigravity.New(nil, nil).LaunchFields(),
			agents.LaunchOptions{FullAccess: ptr(false)},
			map[string]string{"effort": "", "full_access": "false"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.launch.Model = "m"
			got := agents.LaunchOptionsFrom(tt.fields, "m", tt.values)
			if !reflect.DeepEqual(got, tt.launch) {
				t.Fatalf("LaunchOptionsFrom = %+v, want %+v", got, tt.launch)
			}
			// Values() speaks request fields; a preset select is found again
			// from the fields it sets, as the web does for saved profiles.
			back := got.Values()
			form := map[string]string{}
			for _, field := range tt.fields {
				if preset := agents.PresetValue(field, back); preset != "" {
					form[field.ID] = preset
				} else {
					form[field.ID] = back[field.ID]
				}
			}
			if again := agents.LaunchOptionsFrom(tt.fields, "m", form); !reflect.DeepEqual(again, got) {
				t.Fatalf("round trip = %+v, want %+v", again, got)
			}
		})
	}
}

func TestLaunchOptionsFromIgnoresUnknownAndMissing(t *testing.T) {
	fields := codex.New(nil, nil).LaunchFields()
	got := agents.LaunchOptionsFrom(fields, "", map[string]string{"bogus": "x", "sandbox": "read-only"})
	if !reflect.DeepEqual(got, agents.LaunchOptions{}) {
		t.Fatalf("got %+v, want zero", got)
	}
}

func TestLaunchOptionsFromMissingToggleSendsFalse(t *testing.T) {
	fields := []agents.LaunchField{{ID: "full_access", Kind: agents.LaunchFieldToggle}}
	got := agents.LaunchOptionsFrom(fields, "", nil)
	if got.FullAccess == nil || *got.FullAccess {
		t.Fatalf("FullAccess = %v, want false", got.FullAccess)
	}
}

func ptr[T any](v T) *T { return &v }
