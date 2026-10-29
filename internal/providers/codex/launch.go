package codex

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

func (p *Provider) PrepareSession(ctx context.Context, req agents.PrepareSessionRequest) (agents.PreparedSession, error) {
	if req.Launch.ReadOnly {
		return agents.PreparedSession{}, agents.RequireReadOnly(p)
	}
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
	home := homeDir(p.accounts, req.Account)
	if info, err := os.Stat(home); err != nil || !info.IsDir() {
		return agents.PreparedSession{}, fmt.Errorf("codex profile %q has no home directory; remove and add it again", req.Account.Name)
	}
	env := p.profileEnvironment(home, req.Account, req.Session)
	if req.Activity.Usable() {
		env[req.Activity.SpoolEnv] = req.Activity.SpoolPath
		// Hooks are an upgrade over the terminal notifications, never a
		// precondition: when they cannot be installed or trusted the session
		// starts anyway and reports activity from its terminal only.
		_ = p.installActivityHooks(ctx, req.Account, req.Activity, executable)
	}
	return agents.PreparedSession{
		Executable:       executable,
		Args:             launchArgs(settings, req.Args, req.Launch, req.Activity),
		Dir:              req.Session.WorkDir,
		EnvSet:           env,
		EnvUnsetPrefixes: scrubPrefixes,
	}, nil
}

// FinalizeSession has nothing to do: sessions share the profile's CODEX_HOME and
// Bonsai never copies or reconciles credentials.
func (p *Provider) FinalizeSession(context.Context, agents.FinalizeSessionRequest) error {
	return nil
}

// ValidateLaunch rejects options Codex has no equivalent for, and bad values,
// before any session exists.
func (p *Provider) ValidateLaunch(account agents.Account, launch agents.LaunchOptions) error {
	if launch.ReadOnly {
		return agents.RequireReadOnly(p)
	}
	if launch.FullAccess != nil {
		return fmt.Errorf("codex does not support full access; use the Full access or Bypass permissions")
	}
	if launch.PermissionMode != "" {
		return fmt.Errorf("codex does not support permission modes; use sandbox and approval")
	}
	if err := validatePermissions(launch.Model, "", launch.Sandbox, launch.Approval); err != nil {
		return err
	}
	settings, err := ParseSettings(account)
	if err != nil {
		return fmt.Errorf("invalid profile settings")
	}
	return p.validateEffort(account, settings, launch)
}

// validateEffort checks the launch effort against the model's own list when the
// profile's model list is cached (see Models), and against every level Codex
// has shipped otherwise. It never starts Codex: an unknown model list only
// makes the check more permissive.
func (p *Provider) validateEffort(account agents.Account, settings Settings, launch agents.LaunchOptions) error {
	if launch.Effort == "" {
		return nil
	}
	if known := p.effortsFor(account, first(launch.Model, settings.Model)); len(known) > 0 {
		if !slices.Contains(known, launch.Effort) {
			return fmt.Errorf("effort %q is not supported by this model (use %s)", launch.Effort, strings.Join(known, ", "))
		}
		return nil
	}
	return validateChoice("effort", launch.Effort, reasoningLevel)
}

// LaunchFields declares a permissions preset select, which sends sandbox and
// approval together, and a per-model effort select.
func (p *Provider) LaunchFields() []agents.LaunchField {
	efforts := []agents.FieldOption{{Value: "", Label: "Profile default"}}
	for _, effort := range reasoningLevel {
		efforts = append(efforts, agents.FieldOption{Value: effort, Label: effort})
	}
	return []agents.LaunchField{
		{ID: "permissions", Label: "Permissions", Kind: agents.LaunchFieldSelect, Default: "auto", Options: []agents.FieldOption{
			{Value: "read-only", Label: "Read only", Set: map[string]string{"sandbox": "read-only", "approval": "on-request"}},
			{Value: "auto", Label: "Auto", Set: map[string]string{"sandbox": "workspace-write", "approval": "on-request"}},
			{Value: "full-access", Label: "Full access", Tone: agents.ToneDanger, Help: "Codex can change any file and use the network without asking.",
				Set: map[string]string{"sandbox": "danger-full-access", "approval": "never"}},
			{Value: "bypass", Label: "Bypass", Tone: agents.ToneDanger, Help: "Codex runs every command without a sandbox and without asking.",
				Set: map[string]string{"sandbox": SandboxBypass}},
		}},
		{ID: "effort", Label: "Effort", Kind: agents.LaunchFieldSelect, PerModel: true, Options: efforts},
	}
}

// launchArgs builds argv as
//
//	[explicit...] -c cli_auth_credentials_store="file" --no-daemon [-m M]
//	  [-c model_reasoning_effort="E"] [-s S -a A | --dangerously-bypass-approvals-and-sandbox]
//	  [-- prompt]
//
// Per-launch options win over profile defaults and explicit args win over both.
// --no-daemon keeps the whole session (and its tool shells) in Bonsai's process
// group instead of a shared background app-server. The prompt comes last after
// `--` so a leading dash stays literal; when the explicit args carry their own
// `--`, Bonsai's flags go before it.
//
// activity adds the notification settings (see notificationArgs) for the
// terminal fallback, except those the explicit args already choose.
//
// When the explicit args start with something other than a flag, the caller is
// driving a subcommand (`login`, `resume --last`) or a positional prompt, so
// only the global options go in front: the credential store override and
// --no-daemon, which keeps a resumed or forked TUI out of the shared daemon too.
func launchArgs(settings Settings, explicit []string, launch agents.LaunchOptions, activity *agents.ActivityRequest) []string {
	if len(explicit) > 0 && !strings.HasPrefix(explicit[0], "-") {
		global := []string{"-c", storeOverride}
		if !hasFlag(explicit, "--no-daemon") {
			global = append(global, "--no-daemon")
		}
		if activity.Usable() {
			global = append(global, notificationArgs(explicit)...)
		}
		return append(global, explicit...)
	}
	head, tail := explicit, []string(nil)
	if i := slices.Index(explicit, "--"); i >= 0 {
		head, tail = explicit[:i], explicit[i:]
	}
	args := append(append([]string(nil), head...), "-c", storeOverride)
	if !hasFlag(args, "--no-daemon") {
		args = append(args, "--no-daemon")
	}
	if activity.Usable() {
		args = append(args, notificationArgs(args)...)
	}
	if model := first(launch.Model, settings.Model); model != "" && !hasFlag(args, "-m", "--model") {
		args = append(args, "-m", model)
	}
	if effort := first(launch.Effort, settings.Effort); effort != "" && !hasConfig(args, "model_reasoning_effort") {
		args = append(args, "-c", fmt.Sprintf("model_reasoning_effort=%q", effort))
	}
	// Sandbox and approval are one choice: a launch preset replaces both profile
	// defaults instead of mixing with them.
	sandbox, approval := settings.Sandbox, settings.Approval
	if launch.Sandbox != "" || launch.Approval != "" {
		sandbox, approval = launch.Sandbox, launch.Approval
	}
	explicitBypass := hasFlag(args, "--dangerously-bypass-approvals-and-sandbox", "--yolo")
	explicitSandbox := hasFlag(args, "-s", "--sandbox")
	explicitApproval := hasFlag(args, "-a", "--ask-for-approval", "--approve-for-me")
	switch {
	case explicitBypass:
	case sandbox == SandboxBypass:
		if !explicitSandbox && !explicitApproval {
			args = append(args, "--dangerously-bypass-approvals-and-sandbox")
		}
	default:
		if sandbox != "" && !explicitSandbox {
			args = append(args, "-s", sandbox)
		}
		if approval != "" && !explicitApproval {
			args = append(args, "-a", approval)
		}
	}
	if tail != nil {
		return append(args, tail...)
	}
	if launch.Prompt != "" {
		args = append(args, "--", launch.Prompt)
	}
	return args
}

// ReadOnly refuses the planner role because a shell sandbox does not constrain
// configured MCP tools, plugins or hooks, and patch denial is unverified.
func (p *Provider) ReadOnly() agents.ReadOnlySupport {
	return agents.ReadOnlySupport{Reason: "Codex cannot enforce read-only access across patch tools, MCP, plugins and hooks"}
}

func first(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// hasFlag reports whether args set one of the flags, as `--flag`, `--flag=v`,
// or for a short flag also `-fv`.
func hasFlag(args []string, names ...string) bool {
	for _, arg := range args {
		if arg == "--" {
			return false
		}
		for _, name := range names {
			if arg == name || strings.HasPrefix(arg, name+"=") {
				return true
			}
			if len(name) == 2 && !strings.HasPrefix(arg, "--") && strings.HasPrefix(arg, name) {
				return true
			}
		}
	}
	return false
}

// hasConfig reports whether args override the config key with -c/--config.
func hasConfig(args []string, key string) bool {
	for i, arg := range args {
		var value string
		switch {
		case arg == "--":
			return false
		case (arg == "-c" || arg == "--config") && i+1 < len(args):
			value = args[i+1]
		case strings.HasPrefix(arg, "--config="):
			value = strings.TrimPrefix(arg, "--config=")
		case strings.HasPrefix(arg, "-c") && !strings.HasPrefix(arg, "--"):
			value = strings.TrimPrefix(arg, "-c")
		default:
			continue
		}
		name, _, _ := strings.Cut(value, "=")
		if strings.TrimSpace(name) == key {
			return true
		}
	}
	return false
}

// ResolveLaunch returns the options a launch with these overrides runs with:
// the model, effort and the sandbox and approval pair, profile defaults filled
// in. Like launchArgs, an override of either sandbox or approval replaces both
// profile values. A stored pair that matches no preset is kept as it is. It
// starts nothing and the prompt and display name stay unset.
func (p *Provider) ResolveLaunch(account agents.Account, launch agents.LaunchOptions) (agents.LaunchOptions, error) {
	if launch.ReadOnly || launch.Prompt != "" || launch.DisplayName != "" {
		return agents.LaunchOptions{}, fmt.Errorf("%w: only model, effort, sandbox and approval can be resolved", agents.ErrInvalidLaunch)
	}
	if err := p.ValidateLaunch(account, launch); err != nil {
		return agents.LaunchOptions{}, fmt.Errorf("%w: %v", agents.ErrInvalidLaunch, err)
	}
	settings, err := ParseSettings(account)
	if err != nil {
		return agents.LaunchOptions{}, fmt.Errorf("%w: invalid profile settings", agents.ErrInvalidLaunch)
	}
	effective := agents.LaunchOptions{
		Model:    first(launch.Model, settings.Model),
		Effort:   first(launch.Effort, settings.Effort),
		Sandbox:  settings.Sandbox,
		Approval: settings.Approval,
	}
	if launch.Sandbox != "" || launch.Approval != "" {
		effective.Sandbox, effective.Approval = launch.Sandbox, launch.Approval
	}
	if err := validatePermissions(effective.Model, effective.Effort, effective.Sandbox, effective.Approval); err != nil {
		return agents.LaunchOptions{}, fmt.Errorf("%w: %v", agents.ErrInvalidLaunch, err)
	}
	if err := p.validateEffort(account, settings, effective); err != nil {
		return agents.LaunchOptions{}, fmt.Errorf("%w: %v", agents.ErrInvalidLaunch, err)
	}
	return effective, nil
}

var _ agents.LaunchResolver = (*Provider)(nil)

// CachedModels returns the model list already learned for the profile, however
// old, and never starts Codex.
func (p *Provider) CachedModels(account agents.Account) []agents.ModelOption {
	p.modelsMu.Lock()
	defer p.modelsMu.Unlock()
	return append([]agents.ModelOption(nil), p.modelCache[account.ID].models...)
}

var _ agents.CachedModelLister = (*Provider)(nil)
