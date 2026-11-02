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

// hooksPath is where agy reads its global hooks: ~/.gemini/config/hooks.json,
// shared by the CLI and its backend.
func hooksPath(home string) string {
	return filepath.Join(home, ".gemini", "config", "hooks.json")
}

// notificationsMarkerPath exists when the session turned notifications on.
func notificationsMarkerPath(session agents.Session) string {
	return filepath.Join(session.RuntimeDir, "activity-notifications")
}

func generationPath(session agents.Session) string {
	return filepath.Join(session.RuntimeDir, "credential-generation.json")
}

// Sign-in runs agy in one persistent home per profile, so agy's own first-run
// and trust state survive between sign-ins, from a fixed workspace in it.
// probe.lock keeps two processes from running agy in that home at once.
func probeHomeDir(accounts agents.AccountStore, account agents.Account) string {
	return filepath.Join(accounts.AccountDir(account.ID), "home")
}

func probeWorkspaceDir(accounts agents.AccountStore, account agents.Account) string {
	return filepath.Join(probeHomeDir(accounts, account), "workspace")
}

func probeLockPath(accounts agents.AccountStore, account agents.Account) string {
	return filepath.Join(accounts.AccountDir(account.ID), "probe.lock")
}
