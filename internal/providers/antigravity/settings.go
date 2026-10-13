package antigravity

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

type Settings struct {
	Model string `json:"model,omitempty"`
	// Effort is the default effort, composed into the model ID at launch.
	Effort                     string `json:"effort,omitempty"`
	DangerouslySkipPermissions bool   `json:"dangerously_skip_permissions,omitempty"`
}

func ParseSettings(account agents.Account) (Settings, error) {
	if len(account.Settings) == 0 {
		return Settings{}, nil
	}
	var settings Settings
	if err := json.Unmarshal(account.Settings, &settings); err != nil {
		return Settings{}, fmt.Errorf("antigravity settings: %w", err)
	}
	if settings.Effort != "" && !effortRE.MatchString(settings.Effort) {
		return Settings{}, fmt.Errorf("antigravity settings: invalid effort")
	}
	return settings, nil
}

func invocationArgs(settings Settings, explicit []string) []string {
	args := append([]string(nil), explicit...)
	if settings.Model != "" && !hasFlag(args, "--model", "-m") {
		args = append(args, "--model", settings.Model)
	}
	if settings.DangerouslySkipPermissions && !hasFlag(args, "--dangerously-skip-permissions") {
		args = append(args, "--dangerously-skip-permissions")
	}
	return args
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
