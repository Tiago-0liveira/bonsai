package claude

import (
	"encoding/json"
	"fmt"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

// ProfileFields declares the profile defaults: a model, a permission mode and
// an effort.
func (p *Provider) ProfileFields() []agents.LaunchField {
	return agents.ProfileFieldsFrom(p.LaunchFields())
}

// ProfileDefaults reports the stored defaults by field ID.
func (p *Provider) ProfileDefaults(account agents.Account) map[string]string {
	settings, err := ParseSettings(account)
	if err != nil {
		return nil
	}
	out := map[string]string{}
	for id, value := range map[string]string{"model": settings.Model, "permission_mode": settings.PermissionMode, "effort": settings.Effort} {
		if value != "" {
			out[id] = value
		}
	}
	return out
}

// UpdateProfile merges the changes into the stored settings; the sign-in mode
// and seed source are never touched. Only the result is validated, so an edit
// can repair hand-written settings.
func (p *Provider) UpdateProfile(account agents.Account, changes map[string]*string) (json.RawMessage, error) {
	merged := map[string]any{}
	for id, value := range changes {
		switch id {
		case "model", "permission_mode", "effort":
			merged[id] = nil
			if value != nil && *value != "" {
				merged[id] = *value
			}
		default:
			return nil, fmt.Errorf("claude profiles have no default %q", id)
		}
	}
	raw, err := agents.MergeSettings(account.Settings, merged)
	if err != nil {
		return nil, err
	}
	next := account
	next.Settings = raw
	if _, err := ParseSettings(next); err != nil {
		return nil, err
	}
	return raw, nil
}
