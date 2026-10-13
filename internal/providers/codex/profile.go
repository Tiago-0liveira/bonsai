package codex

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

// ProfileFields declares the profile defaults: a model, the permissions preset
// (stored as sandbox and approval) and the per-model effort.
func (p *Provider) ProfileFields() []agents.LaunchField {
	return agents.ProfileFieldsFrom(p.LaunchFields())
}

// ProfileDefaults reports the stored defaults by field ID. A sandbox/approval
// pair that matches no preset has no permissions value.
func (p *Provider) ProfileDefaults(account agents.Account) map[string]string {
	settings, err := ParseSettings(account)
	if err != nil {
		return nil
	}
	out := map[string]string{}
	if settings.Model != "" {
		out["model"] = settings.Model
	}
	if settings.Effort != "" {
		out["effort"] = settings.Effort
	}
	if preset := agents.PresetValue(p.permissionsField(), map[string]string{"sandbox": settings.Sandbox, "approval": settings.Approval}); preset != "" {
		out["permissions"] = preset
	}
	return out
}

// UpdateProfile merges the changes into the stored settings. Choosing a
// permissions preset replaces both sandbox and approval, as a launch does. Only
// the result is validated, so an edit can repair hand-written settings.
func (p *Provider) UpdateProfile(account agents.Account, changes map[string]*string) (json.RawMessage, error) {
	merged := map[string]any{}
	for id, value := range changes {
		v := ""
		if value != nil {
			v = *value
		}
		switch id {
		case "model", "effort":
			merged[id] = optional(v)
		case "permissions":
			preset, err := agents.PresetSettings(p.permissionsField(), v)
			if err != nil {
				return nil, err
			}
			for key, value := range preset {
				merged[key] = optional(value)
			}
		default:
			return nil, fmt.Errorf("codex profiles have no default %q", id)
		}
	}
	raw, err := agents.MergeSettings(account.Settings, merged)
	if err != nil {
		return nil, err
	}
	next := account
	next.Settings = raw
	settings, err := ParseSettings(next)
	if err != nil {
		return nil, err
	}
	// Like a launch, check the effort against the model's own list when it is
	// cached; validation never starts Codex.
	if settings.Effort != "" {
		if known := p.effortsFor(account, settings.Model); len(known) > 0 && !slices.Contains(known, settings.Effort) {
			return nil, fmt.Errorf("effort %q is not supported by this model (use %s)", settings.Effort, strings.Join(known, ", "))
		}
	}
	return raw, nil
}

func (p *Provider) permissionsField() agents.LaunchField {
	for _, field := range p.LaunchFields() {
		if field.ID == "permissions" {
			return field
		}
	}
	return agents.LaunchField{}
}

// optional maps an empty value to a deleted key.
func optional(value string) any {
	if value == "" {
		return nil
	}
	return value
}
