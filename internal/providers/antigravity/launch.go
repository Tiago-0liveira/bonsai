package antigravity

import (
	"context"
	"fmt"
	"strings"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

func (p *Provider) PrepareSession(ctx context.Context, req agents.PrepareSessionRequest) (agents.PreparedSession, error) {
	settings, err := ParseSettings(req.Account)
	if err != nil {
		return agents.PreparedSession{}, err
	}
	executable, err := p.binaryResolver.Resolve()
	if err != nil {
		return agents.PreparedSession{}, err
	}
	models := p.launchModels(ctx, req.Account, settings, req.Args, req.Launch)
	if err := p.credentialMgr.Materialize(ctx, req.Account, req.Session); err != nil {
		return agents.PreparedSession{}, err
	}
	return agents.PreparedSession{
		Executable: executable,
		Args:       launchArgs(settings, req.Args, req.Launch, models),
		Dir:        req.Session.WorkDir,
		EnvSet:     buildEnvironment(req.Account, req.Session),
	}, nil
}

func (p *Provider) FinalizeSession(ctx context.Context, req agents.FinalizeSessionRequest) error {
	return p.credentialMgr.Reconcile(ctx, req.Account, req.Session)
}

// launchModels is the model list a launch composes its model ID from. A model
// with efforts is listed by its base ID, so when a model is chosen and nothing
// is cached yet, agy is asked once (bounded by the models timeout).
func (p *Provider) launchModels(ctx context.Context, account agents.Account, settings Settings, explicit []string, launch agents.LaunchOptions) []agents.ModelOption {
	if hasFlag(explicit, "--model", "-m") || first(launch.Model, settings.Model) == "" {
		return nil
	}
	if models := p.knownModels(account.ID); models != nil {
		return models
	}
	models, _ := p.Models(ctx, account)
	return models
}

// ValidateLaunch rejects options agy has no equivalent for, an effort the
// cached model list says the model lacks, and unreadable profile settings
// before any session is created. It never starts agy.
func (p *Provider) ValidateLaunch(account agents.Account, launch agents.LaunchOptions) error {
	if launch.PermissionMode != "" {
		return fmt.Errorf("antigravity does not support a permission mode; use full access")
	}
	if launch.Sandbox != "" || launch.Approval != "" {
		return fmt.Errorf("antigravity does not support sandbox or approval settings; use full access")
	}
	settings, err := ParseSettings(account)
	if err != nil {
		return fmt.Errorf("invalid profile settings")
	}
	if err := validateEffort(first(launch.Model, settings.Model), launch.Effort, p.knownModels(account.ID)); err != nil {
		return err
	}
	return nil
}

// LaunchFields declares the effort select and the full-access switch; the
// profile's own setting is the switch's initial state. Efforts come from the
// chosen model; the options are the fallback for a model agy did not list.
func (p *Provider) LaunchFields() []agents.LaunchField {
	efforts := []agents.FieldOption{{Value: "", Label: "Profile default"}}
	for _, effort := range []string{"low", "medium", "high"} {
		efforts = append(efforts, agents.FieldOption{Value: effort, Label: effort})
	}
	return []agents.LaunchField{{
		ID: "effort", Label: "Effort", Kind: agents.LaunchFieldSelect, PerModel: true, Options: efforts,
	}, {
		ID: "full_access", Label: "Full access", Kind: agents.LaunchFieldToggle,
		Help: "Allow Antigravity to run tools without permission prompts.",
		Options: []agents.FieldOption{
			{Value: "false", Label: "Ask permissions"},
			{Value: "true", Label: "Full access", Tone: agents.ToneDanger},
		},
	}}
}

// profileEffortApplies reports whether the profile's default effort can be
// added to model: not to a full ID that names its own effort, and not to a
// model the list says lacks it.
func profileEffortApplies(model, effort string, models []agents.ModelOption) bool {
	if effort == "" || validateEffort(model, effort, models) != nil {
		return false
	}
	if _, full := effortBase(models, model); full {
		return false
	}
	if findModel(models, model) == nil {
		for _, known := range effortOrder {
			if strings.HasSuffix(model, "-"+known) {
				return false
			}
		}
	}
	return true
}

func first(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// launchArgs applies per-launch options on top of the profile settings without
// saving them. The prompt is bound to its flag so prompts that begin with a dash
// stay literal, and it always comes last.
//
// The effort is not a flag: agy lists one model ID per effort, so the model
// and effort are composed into the one ID agy receives with --model.
func launchArgs(settings Settings, explicit []string, launch agents.LaunchOptions, models []agents.ModelOption) []string {
	args := append([]string(nil), explicit...)
	if !hasFlag(args, "--model", "-m") {
		model := first(launch.Model, settings.Model)
		effort := launch.Effort
		if effort == "" && profileEffortApplies(model, settings.Effort, models) {
			effort = settings.Effort
		}
		if model = composeModel(model, effort, models); model != "" {
			args = append(args, "--model", model)
		}
	}
	if launch.FullAccess != nil {
		settings.DangerouslySkipPermissions = *launch.FullAccess
	}
	args = invocationArgs(settings, args)
	if launch.Prompt != "" {
		args = append(args, "--prompt-interactive="+launch.Prompt)
	}
	return args
}
