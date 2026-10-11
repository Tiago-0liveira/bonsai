package codex

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

const (
	AuthChatGPT = "chatgpt"
	AuthDevice  = "device"
	AuthAPIKey  = "api-key"
)

// SandboxBypass is Bonsai's name for --dangerously-bypass-approvals-and-sandbox.
const SandboxBypass = "bypass"

var (
	authModes      = []string{AuthChatGPT, AuthDevice, AuthAPIKey}
	sandboxModes   = []string{"read-only", "workspace-write", "danger-full-access", SandboxBypass}
	approvalModes  = []string{"untrusted", "on-request", "never"}
	reasoningLevel = []string{"none", "minimal", "low", "medium", "high", "xhigh", "max", "ultra"}
)

// Settings are the profile defaults stored in the account record. Per-launch
// options override them.
type Settings struct {
	AuthMode   string `json:"auth_mode,omitempty"`
	Model      string `json:"model,omitempty"`
	Effort     string `json:"effort,omitempty"`
	Sandbox    string `json:"sandbox,omitempty"`
	Approval   string `json:"approval,omitempty"`
	SeededFrom string `json:"seeded_from,omitempty"`
}

// ParseSettings decodes and validates the profile settings. A missing auth mode
// means a ChatGPT login.
func ParseSettings(account agents.Account) (Settings, error) {
	var settings Settings
	if len(account.Settings) > 0 {
		if err := json.Unmarshal(account.Settings, &settings); err != nil {
			return Settings{}, fmt.Errorf("codex settings: %w", err)
		}
	}
	if settings.AuthMode == "" {
		settings.AuthMode = AuthChatGPT
	}
	if !slices.Contains(authModes, settings.AuthMode) {
		return Settings{}, fmt.Errorf("codex settings: unknown auth mode %q", settings.AuthMode)
	}
	if err := validatePermissions(settings.Model, settings.Effort, settings.Sandbox, settings.Approval); err != nil {
		return Settings{}, fmt.Errorf("codex settings: %w", err)
	}
	return settings, nil
}

func (s Settings) raw() json.RawMessage {
	data, _ := json.Marshal(s)
	return data
}

func validatePermissions(model, effort, sandbox, approval string) error {
	if err := validateModel(model); err != nil {
		return err
	}
	if err := validateChoice("effort", effort, reasoningLevel); err != nil {
		return err
	}
	if err := validateChoice("sandbox", sandbox, sandboxModes); err != nil {
		return err
	}
	if err := validateChoice("approval policy", approval, approvalModes); err != nil {
		return err
	}
	if sandbox == SandboxBypass && approval != "" {
		return fmt.Errorf("bypass already skips approvals; do not set an approval policy")
	}
	return nil
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

func validateChoice(name, value string, allowed []string) error {
	if value != "" && !slices.Contains(allowed, value) {
		return fmt.Errorf("unsupported %s %q (use %s)", name, value, strings.Join(allowed, ", "))
	}
	return nil
}
