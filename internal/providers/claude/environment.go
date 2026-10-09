package claude

import (
	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

// scrubPrefixes removes every inherited Claude or Anthropic variable. They
// override the config dir (API keys, tokens, cloud-provider switches, gateways),
// so a leaked one would silently run a profile as a different identity.
var scrubPrefixes = []string{"CLAUDE", "ANTHROPIC_"}

// profileEnvironment is the environment layered on the scrubbed parent
// environment. HOME is deliberately not overridden so git, gh and ssh keep
// working. token is only set for token-mode profiles.
func profileEnvironment(dir string, account agents.Account, session agents.Session, token string) map[string]string {
	env := map[string]string{
		"CLAUDE_CONFIG_DIR":       dir,
		"BONSAI_AGENT_PROVIDER":   string(ProviderID),
		"BONSAI_AGENT_ACCOUNT_ID": string(account.ID),
		"BONSAI_AGENT_SESSION_ID": string(session.ID),
	}
	if token != "" {
		env["CLAUDE_CODE_OAUTH_TOKEN"] = token
		env["CLAUDE_CODE_SUBPROCESS_ENV_SCRUB"] = "1"
	}
	return env
}
