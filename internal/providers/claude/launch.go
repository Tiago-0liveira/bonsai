package claude

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"strings"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

func (p *Provider) PrepareSession(_ context.Context, req agents.PrepareSessionRequest) (agents.PreparedSession, error) {
	settings, err := ParseSettings(req.Account)
	if err != nil {
		return agents.PreparedSession{}, err
	}
	if err := p.ValidateLaunch(req.Account, req.Launch); err != nil {
		return agents.PreparedSession{}, fmt.Errorf("%w: %v", agents.ErrInvalidLaunch, err)
	}
	executable, err := p.binaryResolver.Resolve()
	if err != nil {
		return agents.PreparedSession{}, err
	}
	dir := configDir(p.accounts, req.Account)
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return agents.PreparedSession{}, fmt.Errorf("claude profile %q has no config directory; remove and add it again", req.Account.Name)
	}
	var token string
	if settings.AuthMode == AuthToken {
		file, err := readToken(tokenPath(p.accounts, req.Account))
		if err != nil {
			return agents.PreparedSession{}, fmt.Errorf("%w: token for profile %q is missing or unreadable", agents.ErrNotAuthenticated, req.Account.Name)
		}
		token = file.Token
	}
	args, providerSessionID, err := launchArgs(settings, req.Args, req.Launch)
	if err != nil {
		return agents.PreparedSession{}, err
	}
	return agents.PreparedSession{
		Executable:        executable,
		Args:              args,
		Dir:               req.Session.WorkDir,
		EnvSet:            profileEnvironment(dir, req.Account, req.Session, token),
		EnvUnsetPrefixes:  scrubPrefixes,
		ProviderSessionID: providerSessionID,
	}, nil
}

// FinalizeSession has nothing to do: sessions share the profile's config dir and
// Bonsai never copies or reconciles credentials.
func (p *Provider) FinalizeSession(context.Context, agents.FinalizeSessionRequest) error {
	return nil
}

// ValidateLaunch rejects options Claude Code has no equivalent for, and bad
// values, before any session exists.
func (p *Provider) ValidateLaunch(account agents.Account, launch agents.LaunchOptions) error {
	if launch.FullAccess != nil {
		return fmt.Errorf("claude does not support full access; use the bypassPermissions permission mode")
	}
	if err := validateModel(launch.Model); err != nil {
		return err
	}
	if err := validatePermissionMode(launch.PermissionMode); err != nil {
		return err
	}
	if err := validateEffort(launch.Effort); err != nil {
		return err
	}
	if _, err := ParseSettings(account); err != nil {
		return fmt.Errorf("invalid profile settings")
	}
	return nil
}

// launchArgs builds argv as
// [explicit...] --model M --permission-mode P --effort E --name N --session-id U [-- prompt].
// Per-launch options win over profile defaults, explicit args win over both, and
// the prompt always comes last after `--` so a leading dash stays literal.
//
// When the explicit args start with something other than a flag, the caller is
// driving a subcommand (`mcp list`) or a positional prompt, so nothing is added.
func launchArgs(settings Settings, explicit []string, launch agents.LaunchOptions) (args []string, providerSessionID string, err error) {
	args = append([]string(nil), explicit...)
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		return args, "", nil
	}
	if model := first(launch.Model, settings.Model); model != "" && !hasFlag(args, "--model") {
		args = append(args, "--model", model)
	}
	if mode := first(launch.PermissionMode, settings.PermissionMode); mode != "" && !hasFlag(args, "--permission-mode", "--dangerously-skip-permissions") {
		args = append(args, "--permission-mode", mode)
	}
	if effort := first(launch.Effort, settings.Effort); effort != "" && !hasFlag(args, "--effort") {
		args = append(args, "--effort", effort)
	}
	if launch.DisplayName != "" && !hasFlag(args, "--name", "-n") {
		args = append(args, "--name", launch.DisplayName)
	}
	if !hasFlag(args, "--session-id", "--resume", "-r", "--continue", "-c", "--from-pr") {
		providerSessionID, err = newUUID()
		if err != nil {
			return nil, "", err
		}
		args = append(args, "--session-id", providerSessionID)
	}
	if launch.Prompt != "" {
		args = append(args, "--", launch.Prompt)
	}
	return args, providerSessionID, nil
}

func first(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func newUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate session id: %w", err)
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:]), nil
}
