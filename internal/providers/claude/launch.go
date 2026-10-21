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
	token, err := p.profileToken(settings, req.Account)
	if err != nil {
		return agents.PreparedSession{}, err
	}
	if req.Launch.ReadOnly && len(req.Args) > 0 {
		// Explicit args could re-add a tool or a permission mode after the
		// restrictions, so a read-only launch accepts none.
		return agents.PreparedSession{}, fmt.Errorf("%w: a read-only launch takes no explicit arguments", agents.ErrInvalidLaunch)
	}
	args, providerSessionID, err := launchArgs(settings, req.Args, req.Launch, req.Activity)
	if err != nil {
		return agents.PreparedSession{}, err
	}
	env := profileEnvironment(dir, req.Account, req.Session, token)
	if req.Activity.Usable() {
		env[req.Activity.SpoolEnv] = req.Activity.SpoolPath
	}
	return agents.PreparedSession{
		Executable:        executable,
		Args:              args,
		Dir:               req.Session.WorkDir,
		EnvSet:            env,
		EnvUnsetPrefixes:  scrubPrefixes,
		ProviderSessionID: providerSessionID,
	}, nil
}

// profileToken is the stored token of a token-mode profile, empty otherwise.
func (p *Provider) profileToken(settings Settings, account agents.Account) (string, error) {
	if settings.AuthMode != AuthToken {
		return "", nil
	}
	file, err := readToken(tokenPath(p.accounts, account))
	if err != nil {
		return "", fmt.Errorf("%w: token for profile %q is missing or unreadable", agents.ErrNotAuthenticated, account.Name)
	}
	return file.Token, nil
}

// FinalizeSession has nothing to do: sessions share the profile's config dir and
// Bonsai never copies or reconciles credentials.
func (p *Provider) FinalizeSession(context.Context, agents.FinalizeSessionRequest) error {
	return nil
}

// ValidateLaunch rejects options Claude Code has no equivalent for, and bad
// values, before any session exists.
func (p *Provider) ValidateLaunch(account agents.Account, launch agents.LaunchOptions) error {
	if launch.ReadOnly && launch.PermissionMode != "" {
		return fmt.Errorf("a read-only launch cannot also set a permission mode")
	}
	if launch.PlanFile != "" {
		if !launch.ReadOnly {
			return fmt.Errorf("a plan file needs a read-only launch")
		}
		if err := validatePlanFile(launch.PlanFile); err != nil {
			return err
		}
	}
	if launch.FullAccess != nil {
		return fmt.Errorf("claude does not support full access; use the bypassPermissions permission mode")
	}
	if launch.Sandbox != "" || launch.Approval != "" {
		return fmt.Errorf("claude does not support sandbox or approval settings; use a permission mode")
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

// LaunchFields declares the permission mode and effort selects. An empty value
// keeps the profile default. The effort options are only the fallback: each
// listed model carries the efforts the API says it supports.
func (p *Provider) LaunchFields() []agents.LaunchField {
	efforts := []agents.FieldOption{{Value: "", Label: "Profile default"}}
	for _, effort := range effortLevels {
		efforts = append(efforts, agents.FieldOption{Value: effort, Label: effort})
	}
	return []agents.LaunchField{
		{ID: "permission_mode", Label: "Permission mode", Kind: agents.LaunchFieldSelect, Options: []agents.FieldOption{
			{Value: "", Label: "Profile default"},
			{Value: "default", Label: "Default"},
			{Value: "acceptEdits", Label: "Accept edits"},
			{Value: "plan", Label: "Plan"},
			{Value: "auto", Label: "Auto"},
			{Value: "bypassPermissions", Label: "Bypass permissions", Tone: agents.ToneDanger, Help: "Claude will run tools without asking."},
		}},
		{ID: "effort", Label: "Effort", Kind: agents.LaunchFieldSelect, PerModel: true, Options: efforts},
	}
}

// launchArgs builds argv as
// [explicit...] --model M --permission-mode P --effort E --name N --session-id U [--settings J] [-- prompt].
// Per-launch options win over profile defaults, explicit args win over both, and
// the prompt always comes last after `--` so a leading dash stays literal.
//
// When the explicit args start with something other than a flag, the caller is
// driving a subcommand (`mcp list`) or a positional prompt, so nothing is added.
//
// activity adds a per-session --settings layer with Bonsai's observe-only hooks.
// It is skipped when the caller brought --settings (they own that layer) or
// --bare (which ignores hooks): activity then stays unknown.
func launchArgs(settings Settings, explicit []string, launch agents.LaunchOptions, activity *agents.ActivityRequest) (args []string, providerSessionID string, err error) {
	args = append([]string(nil), explicit...)
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		return args, "", nil
	}
	if model := first(launch.Model, settings.Model); model != "" && !hasFlag(args, "--model") {
		args = append(args, "--model", model)
	}
	planSettings := ""
	if launch.PlanFile != "" && hasFlag(args, "--permission-mode", "--dangerously-skip-permissions", "--settings", "--add-dir", "--tools", "--allowedTools", "--allowed-tools", "--setting-sources") {
		// Explicit args could widen or replace the policy that confines the one write.
		return nil, "", fmt.Errorf("%w: a plan file launch takes no permission, tool, settings or directory arguments", agents.ErrInvalidLaunch)
	}
	if launch.PlanFile != "" && !launch.ReadOnly {
		return nil, "", fmt.Errorf("%w: a plan file needs a read-only launch", agents.ErrInvalidLaunch)
	}
	if launch.ReadOnly && launch.PlanFile != "" {
		scoped, settingsJSON, err := planFileLaunch(launch.PlanFile, activity)
		if err != nil {
			return nil, "", err
		}
		args = append(args, scoped...)
		planSettings = settingsJSON
	} else if launch.ReadOnly {
		// The profile's permission mode is dropped on purpose: a profile that
		// defaults to bypassPermissions must not widen a read-only run.
		args = append(args, readOnlyArgs...)
	} else if mode := first(launch.PermissionMode, settings.PermissionMode); mode != "" && !hasFlag(args, "--permission-mode", "--dangerously-skip-permissions") {
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
	if planSettings != "" {
		// One --settings value carries the permission policy and, when asked for, the activity hooks.
		args = append(args, "--settings", planSettings)
	} else if activity.Usable() && !hasFlag(args, "--settings", "--bare") {
		if hooks, err := activitySettings(activity); err == nil {
			args = append(args, "--settings", hooks)
		}
	}
	if launch.Prompt != "" {
		args = append(args, "--", launch.Prompt)
	}
	return args, providerSessionID, nil
}

// readOnlyArgs make Claude Code a reader. --tools replaces the built-in tool set
// with file reading and searching, so Edit, Write, Bash and the other tools do not
// exist for the model at all; --restricted additionally drops the code-running tools
// and WebFetch, confines file tools to the working directory, ignores user, project
// and local settings (a profile cannot allow more) and refuses bypassPermissions;
// --strict-mcp-config keeps every MCP server out because none is passed. The
// plan permission mode is added for its prompt behavior only: it is not what
// enforces anything. An older Claude Code that lacks one of these flags exits
// with an unknown-option error, so the launch fails closed.
var readOnlyArgs = []string{"--restricted", "--tools", "Read,Grep,Glob", "--strict-mcp-config", "--permission-mode", "plan"}

// ReadOnly implements agents.ReadOnlyEnforcer.
func (p *Provider) ReadOnly() agents.ReadOnlySupport {
	return agents.ReadOnlySupport{
		Supported: true,
		PlanFile:  true,
		Mechanism: "tool allowlist (claude --restricted --tools Read,Grep,Glob) with deny-by-default permissions (dontAsk)",
		Allows: []string{
			"Read, search and list files in the repository",
			"Write the plan file only: one file in a Bonsai-owned directory under ~/.bonsai/garden/plans",
			"No other file edits, shell commands, web access or MCP tools",
		},
	}
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

// ResolveLaunch returns the options a launch with these overrides runs with:
// the model, effort and permission mode, profile defaults filled in. An empty
// result mode means the profile pins none. It starts nothing and the prompt and
// display name stay unset.
func (p *Provider) ResolveLaunch(account agents.Account, launch agents.LaunchOptions) (agents.LaunchOptions, error) {
	if launch.ReadOnly || launch.PlanFile != "" || launch.Prompt != "" || launch.DisplayName != "" {
		return agents.LaunchOptions{}, fmt.Errorf("%w: only model, effort and permission mode can be resolved", agents.ErrInvalidLaunch)
	}
	if err := p.ValidateLaunch(account, launch); err != nil {
		return agents.LaunchOptions{}, fmt.Errorf("%w: %v", agents.ErrInvalidLaunch, err)
	}
	if launch.PermissionMode != "" && !p.declaresPermissionMode(launch.PermissionMode) {
		// The parser accepts modes the launch form never offers; a caller may
		// only choose one the form declares, though a stored profile may keep one.
		return agents.LaunchOptions{}, fmt.Errorf("%w: permission mode %q is not offered", agents.ErrInvalidLaunch, launch.PermissionMode)
	}
	settings, err := ParseSettings(account)
	if err != nil {
		return agents.LaunchOptions{}, fmt.Errorf("%w: invalid profile settings", agents.ErrInvalidLaunch)
	}
	effective := agents.LaunchOptions{
		Model:          first(launch.Model, settings.Model),
		Effort:         first(launch.Effort, settings.Effort),
		PermissionMode: first(launch.PermissionMode, settings.PermissionMode),
	}
	if err := p.ValidateLaunch(account, effective); err != nil {
		return agents.LaunchOptions{}, fmt.Errorf("%w: %v", agents.ErrInvalidLaunch, err)
	}
	return effective, nil
}

func (p *Provider) declaresPermissionMode(mode string) bool {
	for _, field := range p.LaunchFields() {
		if field.ID != "permission_mode" {
			continue
		}
		for _, option := range field.Options {
			if option.Value == mode {
				return true
			}
		}
	}
	return false
}

var _ agents.LaunchResolver = (*Provider)(nil)

// CachedModels returns the aliases and models already learned for the profile,
// however old, and never calls the API.
func (p *Provider) CachedModels(account agents.Account) []agents.ModelOption {
	p.modelsMu.Lock()
	defer p.modelsMu.Unlock()
	entry := p.modelsCache[account.ID]
	return append(append([]agents.ModelOption(nil), entry.aliases...), entry.models...)
}

var _ agents.CachedModelLister = (*Provider)(nil)
