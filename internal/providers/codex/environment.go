package codex

import (
	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

// scrubPrefixes removes every inherited Codex or OpenAI variable. They switch
// the home, API key, access token, endpoint or sandbox, so a leaked one would
// silently run a profile as a different identity.
var scrubPrefixes = []string{"CODEX_", "OPENAI_"}

// keptHostVariables survive the scrub: a corporate CA bundle is not identity.
var keptHostVariables = []string{"CODEX_CA_CERTIFICATE"}

// profileEnvironment is the environment layered on the scrubbed parent
// environment. HOME is deliberately not overridden so git, gh and ssh keep
// working.
func (p *Provider) profileEnvironment(home string, account agents.Account, session agents.Session) map[string]string {
	env := map[string]string{
		"CODEX_HOME":              home,
		"BONSAI_AGENT_PROVIDER":   string(ProviderID),
		"BONSAI_AGENT_ACCOUNT_ID": string(account.ID),
		"BONSAI_AGENT_SESSION_ID": string(session.ID),
	}
	for _, name := range keptHostVariables {
		if value := p.hostEnv(name); value != "" {
			env[name] = value
		}
	}
	return env
}

// storeOverride forces the file credential store on every invocation, so a
// profile's login always lives in its own auth.json and never in a shared
// OS keyring entry.
const storeOverride = `cli_auth_credentials_store="file"`
