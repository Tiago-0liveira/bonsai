package antigravity

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

// ProfileFields declares the profile defaults: a model, an effort and full
// access (stored as dangerously_skip_permissions).
func (p *Provider) ProfileFields() []agents.LaunchField {
	return agents.ProfileFieldsFrom(p.LaunchFields())
}

// ProfileDefaults reports the stored defaults by field ID. Full access is
// listed only when on.
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
	if settings.DangerouslySkipPermissions {
		out["full_access"] = "true"
	}
	return out
}

// UpdateProfile merges the changes into the stored settings. Full access off
// removes the setting, which is agy's own default.
func (p *Provider) UpdateProfile(account agents.Account, changes map[string]*string) (json.RawMessage, error) {
	merged := map[string]any{}
	for id, value := range changes {
		v := ""
		if value != nil {
			v = *value
		}
		switch id {
		case "model":
			if err := validateModel(v); err != nil {
				return nil, err
			}
			merged["model"] = nil
			if v != "" {
				merged["model"] = v
			}
		case "effort":
			if v != "" && !effortRE.MatchString(v) {
				return nil, fmt.Errorf("invalid effort")
			}
			merged["effort"] = nil
			if v != "" {
				merged["effort"] = v
			}
		case "full_access":
			on := false
			if v != "" {
				var err error
				if on, err = strconv.ParseBool(v); err != nil || (v != "true" && v != "false") {
					return nil, fmt.Errorf("unsupported full access %q (use false, true)", v)
				}
			}
			merged["dangerously_skip_permissions"] = nil
			if on {
				merged["dangerously_skip_permissions"] = true
			}
		default:
			return nil, fmt.Errorf("antigravity profiles have no default %q", id)
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

// validateModel keeps a profile model a single argv value: agy receives it as
// `--model <value>`.
func validateModel(model string) error {
	if model == "" {
		return nil
	}
	if len(model) > 128 || strings.HasPrefix(model, "-") || strings.ContainsFunc(model, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return fmt.Errorf("invalid model")
	}
	return nil
}
