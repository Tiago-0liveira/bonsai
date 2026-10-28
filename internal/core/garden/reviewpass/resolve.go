package reviewpass

import (
	"context"
	"errors"
	"fmt"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/config"
)

type Accounts interface {
	ResolveLaunch(context.Context, agents.AccountID, agents.LaunchOptions) (agents.LaunchResolution, error)
}
type Source string

const (
	SourceBuiltIn  Source = "built-in"
	SourcePersonal Source = "personal"
	SourceRun      Source = "run"
	SourceProfile  Source = "profile"
)

type Override struct {
	Enabled       *bool
	AccountID     *agents.AccountID
	Model, Effort *string
	Native        *Native
	Checks        *[]config.GardenCheck
}
type Resolved struct {
	Enabled, Available bool
	Reason             string
	Policy             Policy
	Sources            map[string]Source
}

func Resolve(ctx context.Context, prefs *config.GardenAutoReviewPrefs, o *Override, a Accounts) (Resolved, error) {
	out := Resolved{Sources: map[string]Source{}}
	for _, k := range []string{"enabled", "account", "model", "effort", "native", "checks"} {
		out.Sources[k] = SourceBuiltIn
	}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	p := config.GardenAutoReviewPrefs{}
	if prefs != nil && prefs.Validate() == nil {
		p = *prefs.Clone()
		out.Sources["enabled"] = SourcePersonal
		if p.Account != "" {
			out.Sources["account"] = SourcePersonal
		}
		if p.Model != "" {
			out.Sources["model"] = SourcePersonal
		}
		if p.Effort != "" {
			out.Sources["effort"] = SourcePersonal
		}
		if p.PermissionMode != "" || p.Sandbox != "" || p.Approval != "" || p.FullAccess != nil {
			out.Sources["native"] = SourcePersonal
		}
		if p.Checks != nil {
			out.Sources["checks"] = SourcePersonal
		}
	}
	if o != nil {
		// Validate even disabled overrides, so an invalid local edit is never hidden.
		v := config.GardenAutoReviewPrefs{}
		if o.AccountID != nil {
			v.Account = string(*o.AccountID)
			p.Account = v.Account
			out.Sources["account"] = SourceRun
		}
		if o.Model != nil {
			v.Model = *o.Model
			p.Model = *o.Model
			out.Sources["model"] = SourceRun
		}
		if o.Effort != nil {
			v.Effort = *o.Effort
			p.Effort = *o.Effort
			out.Sources["effort"] = SourceRun
		}
		if o.Native != nil {
			v.PermissionMode = o.Native.PermissionMode
			v.Sandbox = o.Native.Sandbox
			v.Approval = o.Native.Approval
			v.FullAccess = o.Native.FullAccess
			p.PermissionMode = v.PermissionMode
			p.Sandbox = v.Sandbox
			p.Approval = v.Approval
			p.FullAccess = v.FullAccess
			out.Sources["native"] = SourceRun
		}
		if o.Checks != nil {
			v.Checks = *o.Checks
			p.Checks = v.Checks
			out.Sources["checks"] = SourceRun
		}
		if err := v.Validate(); err != nil {
			return out, fmt.Errorf("%w: override: %w", ErrInvalid, err)
		}
		if o.Enabled != nil {
			p.Enabled = *o.Enabled
			out.Sources["enabled"] = SourceRun
		}
	}
	out.Enabled = p.Enabled
	unavailable := func(reason string) (Resolved, error) { out.Reason = reason; return out, nil }
	if !p.Enabled {
		return unavailable("Automatic review is off.")
	}
	if p.Account == "" {
		return unavailable("Choose a reviewer account.")
	}
	if a == nil {
		return unavailable("Agent accounts are unavailable here.")
	}
	native := Native{PermissionMode: p.PermissionMode, Sandbox: p.Sandbox, Approval: p.Approval, FullAccess: p.FullAccess}
	res, err := a.ResolveLaunch(ctx, agents.AccountID(p.Account), native.launch(p.Model, p.Effort))
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return out, ctxErr
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return out, err
		}
		if errors.Is(err, agents.ErrAccountNotFound) {
			return unavailable("The reviewer account is not set up on this machine.")
		}
		return unavailable("The reviewer account cannot launch.")
	}
	if err := ctx.Err(); err != nil {
		return out, err
	}
	if res.AccountID != agents.AccountID(p.Account) || res.Provider == "" || res.Launch.Prompt != "" || res.Launch.ReadOnly || res.Launch.PlanFile != "" || res.Launch.DisplayName != "" {
		return unavailable("The reviewer account cannot launch.")
	}
	if err := agents.NativePermissions(res.Provider, res.Launch); err != nil {
		if errors.Is(err, agents.ErrNativeUnsupported) {
			return unavailable(fmt.Sprintf("The reviewer's permission mode is not supported by %s.", providerName(res.Provider)))
		}
		return unavailable("Choose the reviewer's permissions; its profile leaves them undecided.")
	}
	esc := agents.EscalatedLaunch(res.Provider, res.Launch)
	inheritedNative := false
	for _, k := range []string{"sandbox", "approval", "permission_mode", "full_access"} {
		inheritedNative = inheritedNative || res.Inherited[k]
	}
	if esc && inheritedNative {
		return unavailable("The reviewer profile defaults to full access; choose its permissions explicitly.")
	}
	for _, k := range []string{"model", "effort"} {
		if res.Inherited[k] {
			out.Sources[k] = SourceProfile
		}
	}
	if inheritedNative {
		out.Sources["native"] = SourceProfile
	}
	out.Policy = Policy{Passes: Passes, Reviewer: Reviewer{Provider: res.Provider, AccountID: res.AccountID, AccountName: res.AccountName, Model: res.Launch.Model, Effort: res.Launch.Effort, Native: nativeFrom(res.Launch), Escalated: esc}}
	for _, c := range p.Checks {
		out.Policy.Checks = append(out.Policy.Checks, CheckFrom(c))
	}
	if out.Policy.Validate() != nil {
		out.Policy = Policy{}
		return unavailable("The reviewer account cannot launch.")
	}
	out.Available = true
	return out, nil
}

// Unknown IDs are never interpolated into a reason: a provider could return prose.
func providerName(p agents.ProviderID) string {
	switch p {
	case "codex", "claude", "antigravity":
		return string(p)
	}
	return "unknown provider"
}
