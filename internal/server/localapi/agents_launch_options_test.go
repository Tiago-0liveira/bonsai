package localapi

import (
	"reflect"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

// The TUI builds launch options with agents.LaunchOptionsFrom; the web client
// sends the same choices as request fields. Both must reach the provider as
// identical options.
func TestTUILaunchOptionsMatchWebRequest(t *testing.T) {
	fields := []agents.LaunchField{
		{ID: "access", Kind: agents.LaunchFieldSelect, Options: []agents.FieldOption{
			{Value: "ask"},
			{Value: "full", Set: map[string]string{"permission_mode": "bypass", "full_access": "true"}},
		}},
		{ID: "effort", Kind: agents.LaunchFieldSelect, Options: []agents.FieldOption{{Value: "low"}, {Value: "high"}}},
		{ID: "sandbox", Kind: agents.LaunchFieldSelect, Options: []agents.FieldOption{{Value: ""}, {Value: "read-only"}}},
		{ID: "full_access", Kind: agents.LaunchFieldToggle},
	}
	on := true
	off := false
	cases := []struct {
		name   string
		values map[string]string
		web    agentRequest
	}{
		{"preset", map[string]string{"access": "full", "effort": "high"}, agentRequest{Model: "m", PermissionMode: "bypass", FullAccess: &off, Effort: "high"}},
		{"plain select", map[string]string{"access": "ask", "effort": "low", "sandbox": "read-only"}, agentRequest{Model: "m", FullAccess: &off, Effort: "low", Sandbox: "read-only"}},
		{"empty select sends nothing", map[string]string{"sandbox": ""}, agentRequest{Model: "m", FullAccess: &off}},
		{"toggle", map[string]string{"full_access": "true"}, agentRequest{Model: "m", FullAccess: &on}},
		{"nothing chosen", nil, agentRequest{Model: "m", FullAccess: &off}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tui := agents.LaunchOptionsFrom(fields, "m", tc.values)
			web := tc.web.launchOptions()
			if !reflect.DeepEqual(tui.Values(), web.Values()) || tui.Model != web.Model {
				t.Fatalf("tui %+v (%v) != web %+v (%v)", tui, tui.Values(), web, web.Values())
			}
		})
	}
}
