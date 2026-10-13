package codex

import (
	"context"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

// SetupSpec declares Codex's sign-in modes and seed source for the web.
// Device code is recommended where a browser on this host may not open.
func (p *Provider) SetupSpec(context.Context) agents.SetupSpec {
	remote := agents.RemoteHost(p.hostEnv)
	spec := agents.SetupSpec{
		AuthModes: []agents.AuthModeOption{
			{Value: AuthChatGPT, Label: "Browser sign-in", Help: "Sign in with ChatGPT in a browser on this computer.", Recommended: !remote},
			{Value: AuthDevice, Label: "Device code", Help: "Enter a one-time code on any device; for SSH, WSL and headless hosts.", Recommended: remote},
			{Value: AuthAPIKey, Label: "API key", Help: "Use an OpenAI API key, billed per use; usage limits are not shown.", Secret: &agents.SecretSpec{Label: "OpenAI API key", Placeholder: "sk-…"}},
		},
	}
	if src, _, err := p.seedSource(""); err == nil {
		home, _ := p.hostHome()
		spec.Seed = &agents.SeedSpec{Source: agents.HomeRelative(src, home)}
	}
	return spec
}
