package antigravity

import (
	"path/filepath"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

func credentialLockPath(accounts agents.AccountStore, account agents.Account) string {
	return filepath.Join(accounts.AccountDir(account.ID), "credential.lock")
}

func vaultPath(accounts agents.AccountStore, account agents.Account) string {
	return filepath.Join(accounts.CredentialDir(account.ID), "antigravity.json")
}

func antigravityDir(home string) string {
	return filepath.Join(home, ".gemini", "antigravity-cli")
}

func oauthPath(home string) string {
	return filepath.Join(antigravityDir(home), "antigravity-oauth-token")
}

func providerSettingsPath(home string) string {
	return filepath.Join(antigravityDir(home), "settings.json")
}

func generationPath(session agents.Session) string {
	return filepath.Join(session.RuntimeDir, "credential-generation.json")
}
