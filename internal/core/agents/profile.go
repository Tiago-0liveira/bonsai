package agents

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
)

// ProviderDefaultLabel names the empty choice of a profile field: the launch
// uses whatever the provider itself defaults to.
const ProviderDefaultLabel = "Provider default"

// ProfileModelField is the model field every ProfileEditor declares first.
func ProfileModelField() LaunchField {
	return LaunchField{ID: "model", Label: "Model", Kind: LaunchFieldModel}
}

// ProfileFieldsFrom turns launch fields into profile fields: the model field
// first, then each launch field with its own default cleared. Every select
// starts with an empty "Provider default" choice (relabelling the launch
// field's empty "Profile default" one), so a profile can always go back to the
// provider default. Toggles keep their two options; "false" is the unset state.
func ProfileFieldsFrom(launch []LaunchField) []LaunchField {
	out := []LaunchField{ProfileModelField()}
	for _, field := range launch {
		field.Default = ""
		options := make([]FieldOption, 0, len(field.Options)+1)
		if field.Kind == LaunchFieldSelect {
			options = append(options, FieldOption{Value: "", Label: ProviderDefaultLabel})
		}
		for _, option := range field.Options {
			if field.Kind == LaunchFieldSelect && option.Value == "" {
				continue
			}
			option.Set = maps.Clone(option.Set)
			options = append(options, option)
		}
		field.Options = options
		out = append(out, field)
	}
	return out
}

// PresetValue returns the option of a preset select whose Set matches values
// exactly (missing keys count as empty), or "" when none does.
func PresetValue(field LaunchField, values map[string]string) string {
	for _, option := range field.Options {
		if option.Value == "" || len(option.Set) == 0 {
			continue
		}
		match := true
		for key := range presetKeys(field) {
			if option.Set[key] != values[key] {
				match = false
				break
			}
		}
		if match {
			return option.Value
		}
	}
	return ""
}

// presetKeys lists every request field any option of a preset select sets.
func presetKeys(field LaunchField) map[string]bool {
	keys := map[string]bool{}
	for _, option := range field.Options {
		for key := range option.Set {
			keys[key] = true
		}
	}
	return keys
}

// PresetSettings returns the settings a preset option writes: every key the
// preset can set, with the chosen option's value or "" (unset). An unknown
// value is an error naming the choices.
func PresetSettings(field LaunchField, value string) (map[string]string, error) {
	out := map[string]string{}
	for key := range presetKeys(field) {
		out[key] = ""
	}
	if value == "" {
		return out, nil
	}
	for _, option := range field.Options {
		if option.Value == value && len(option.Set) > 0 {
			maps.Copy(out, option.Set)
			return out, nil
		}
	}
	return nil, fmt.Errorf("unsupported %s %q (use %s)", strings.ToLower(field.Label), value, strings.Join(FieldValues(field), ", "))
}

// FieldValues lists a field's non-empty option values in display order.
func FieldValues(field LaunchField) []string {
	var values []string
	for _, option := range field.Options {
		if option.Value != "" {
			values = append(values, option.Value)
		}
	}
	return values
}

// FieldIDs lists the IDs of fields in order.
func FieldIDs(fields []LaunchField) []string {
	ids := make([]string, len(fields))
	for i, field := range fields {
		ids[i] = field.ID
	}
	return ids
}

// UnknownProfileFields returns the change keys no field declares, sorted.
func UnknownProfileFields(fields []LaunchField, changes map[string]*string) []string {
	ids := FieldIDs(fields)
	var unknown []string
	for key := range changes {
		if !slices.Contains(ids, key) {
			unknown = append(unknown, key)
		}
	}
	slices.Sort(unknown)
	return unknown
}

// MergeSettings applies changes to stored settings JSON without decoding it into
// a provider type, so keys a change does not name (sign-in mode, seed source,
// keys of a newer Bonsai) survive unchanged. A nil value deletes the key.
func MergeSettings(raw json.RawMessage, changes map[string]any) (json.RawMessage, error) {
	values := map[string]json.RawMessage{}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &values); err != nil {
			return nil, fmt.Errorf("profile settings are not an object")
		}
		if values == nil {
			values = map[string]json.RawMessage{}
		}
	}
	for key, value := range changes {
		if value == nil {
			delete(values, key)
			continue
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		values[key] = encoded
	}
	if len(values) == 0 {
		return nil, nil
	}
	return json.Marshal(values)
}
