package claude

import (
	"context"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

// SetupSpec declares Claude's sign-in modes and seed source for the web. A
// token profile without a pasted token runs `claude setup-token` first.
func (p *Provider) SetupSpec(context.Context) agents.SetupSpec {
	spec := agents.SetupSpec{
		AuthModes: []agents.AuthModeOption{
			{Value: AuthLogin, Label: "Browser sign-in", Help: "Sign in with your Claude account; usage limits are shown.", Recommended: true},
			{Value: AuthToken, Label: "Setup token", Help: "A long-lived token from `claude setup-token`; avoids login refresh races between sessions, usage limits are not shown.", Secret: &agents.SecretSpec{Label: "Setup token", Placeholder: "sk-ant-oat01-…", Optional: true}},
		},
	}
	if src, _, _, err := p.seedSource(""); err == nil {
		home, _ := p.hostHome()
		spec.Seed = &agents.SeedSpec{Source: agents.HomeRelative(src, home)}
	}
	return spec
}
