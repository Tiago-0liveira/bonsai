package agents

import (
	"fmt"
	"strconv"
)

// LaunchOptionsFrom builds launch options from a form's values, keyed by field
// ID, the same way the web's launch dialog builds its start request: a select
// whose chosen option carries Set sends those fields instead of its own, an
// empty select sends nothing and a toggle sends its boolean (false when
// unchosen). Missing selects are skipped, so callers seed defaults themselves.
// Keys that are not launch options are ignored; the prompt and display name stay unset.
func LaunchOptionsFrom(fields []LaunchField, model string, values map[string]string) LaunchOptions {
	request := map[string]string{}
	for _, field := range fields {
		value, ok := values[field.ID]
		if !ok && field.Kind != LaunchFieldToggle {
			continue
		}
		if field.Kind == LaunchFieldToggle {
			if on, err := strconv.ParseBool(value); err == nil {
				request[field.ID] = strconv.FormatBool(on)
			} else {
				request[field.ID] = "false"
			}
			continue
		}
		preset := false
		for _, option := range field.Options {
			if option.Value == value && len(option.Set) > 0 {
				for key, set := range option.Set {
					request[key] = set
				}
				preset = true
				break
			}
		}
		if !preset && value != "" {
			request[field.ID] = value
		}
	}
	launch := LaunchOptions{
		Model:          model,
		PermissionMode: request["permission_mode"],
		Effort:         request["effort"],
		Sandbox:        request["sandbox"],
		Approval:       request["approval"],
	}
	if value, ok := request["full_access"]; ok {
		on := value == "true"
		launch.FullAccess = &on
	}
	return launch
}

// NativePermissions requires decided permission values owned by the provider.
func NativePermissions(provider ProviderID, l LaunchOptions) error {
	unsupported := func(why string) error {
		return fmt.Errorf("%w: %s: %w", ErrNativeUnsupported, why, ErrInvalidLaunch)
	}
	switch provider {
	case "codex":
		if l.PermissionMode != "" || l.FullAccess != nil {
			return unsupported("codex uses sandbox and approval")
		}
		switch l.Sandbox {
		case "":
			return fmt.Errorf("%w: sandbox", ErrNativeUnresolved)
		case "bypass":
			if l.Approval != "" {
				return unsupported("bypass takes no approval policy")
			}
			return nil
		case "read-only", "workspace-write", "danger-full-access":
		default:
			return unsupported("unknown sandbox")
		}
		switch l.Approval {
		case "":
			return fmt.Errorf("%w: approval policy", ErrNativeUnresolved)
		case "untrusted", "on-request", "never":
		default:
			return unsupported("unknown approval policy")
		}
	case "claude":
		if l.Sandbox != "" || l.Approval != "" || l.FullAccess != nil {
			return unsupported("claude uses a permission mode")
		}
		if l.PermissionMode == "" {
			return fmt.Errorf("%w: permission mode", ErrNativeUnresolved)
		}
	case "antigravity":
		if l.Sandbox != "" || l.Approval != "" || l.PermissionMode != "" {
			return unsupported("antigravity uses full access")
		}
		if l.FullAccess == nil {
			return fmt.Errorf("%w: full access", ErrNativeUnresolved)
		}
	default:
		return unsupported("unknown provider")
	}
	return nil
}

// EscalatedLaunch reports modes that allow acting without asking.
func EscalatedLaunch(provider ProviderID, l LaunchOptions) bool {
	switch provider {
	case "codex":
		return l.Sandbox == "danger-full-access" || l.Sandbox == "bypass"
	case "claude":
		return l.PermissionMode == "bypassPermissions" || l.PermissionMode == "auto" || l.PermissionMode == "dontAsk"
	case "antigravity":
		return l.FullAccess != nil && *l.FullAccess
	}
	return false
}
