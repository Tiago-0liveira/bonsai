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

func oauthPath(home string) string {
	return filepath.Join(home, ".gemini", "oauth_creds.json")
}

func accountsPath(home string) string {
	return filepath.Join(home, ".gemini", "google_accounts.json")
}

func userIDPath(home string) string {
	return filepath.Join(home, ".gemini", "user_id")
}

func generationPath(session agents.Session) string {
	return filepath.Join(session.RuntimeDir, "credential-generation.json")
}
