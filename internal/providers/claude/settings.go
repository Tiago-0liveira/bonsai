package claude

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

const (
	AuthLogin = "login"
	AuthToken = "token"
)

var (
	permissionModes = []string{"default", "acceptEdits", "plan", "auto", "dontAsk", "bypassPermissions"}
	effortLevels    = []string{"low", "medium", "high", "xhigh", "max"}
)

// Settings are the profile defaults stored in the account record. Per-launch
// options override them.
type Settings struct {
	AuthMode       string `json:"auth_mode,omitempty"`
	Model          string `json:"model,omitempty"`
	PermissionMode string `json:"permission_mode,omitempty"`
	Effort         string `json:"effort,omitempty"`
	SeededFrom     string `json:"seeded_from,omitempty"`
}

// ParseSettings decodes and validates the profile settings. A missing auth mode
// means login.
func ParseSettings(account agents.Account) (Settings, error) {
	var settings Settings
	if len(account.Settings) > 0 {
		if err := json.Unmarshal(account.Settings, &settings); err != nil {
			return Settings{}, fmt.Errorf("claude settings: %w", err)
		}
	}
	if settings.AuthMode == "" {
		settings.AuthMode = AuthLogin
	}
	if settings.AuthMode != AuthLogin && settings.AuthMode != AuthToken {
		return Settings{}, fmt.Errorf("claude settings: unknown auth mode %q", settings.AuthMode)
	}
	if err := validateModel(settings.Model); err != nil {
		return Settings{}, fmt.Errorf("claude settings: %w", err)
	}
	if err := validatePermissionMode(settings.PermissionMode); err != nil {
		return Settings{}, fmt.Errorf("claude settings: %w", err)
	}
	if err := validateEffort(settings.Effort); err != nil {
		return Settings{}, fmt.Errorf("claude settings: %w", err)
	}
	return settings, nil
}

func (s Settings) raw() json.RawMessage {
	data, _ := json.Marshal(s)
	return data
}

func validateModel(model string) error {
	if model == "" {
		return nil
	}
	if len(model) > 128 || strings.HasPrefix(model, "-") || strings.ContainsFunc(model, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return fmt.Errorf("invalid model")
	}
	return nil
}

func validatePermissionMode(mode string) error {
	if mode != "" && !slices.Contains(permissionModes, mode) {
		return fmt.Errorf("unsupported permission mode %q (use %s)", mode, strings.Join(permissionModes, ", "))
	}
	return nil
}

func validateEffort(effort string) error {
	if effort != "" && !slices.Contains(effortLevels, effort) {
		return fmt.Errorf("unsupported effort %q (use %s)", effort, strings.Join(effortLevels, ", "))
	}
	return nil
}

func hasFlag(args []string, names ...string) bool {
	for _, arg := range args {
		for _, name := range names {
			if arg == name || strings.HasPrefix(arg, name+"=") {
				return true
			}
		}
	}
	return false
}
