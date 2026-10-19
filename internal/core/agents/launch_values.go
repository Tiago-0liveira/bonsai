package agents

import "strconv"

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
