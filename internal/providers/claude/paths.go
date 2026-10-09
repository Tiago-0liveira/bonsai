package claude

import (
	"path/filepath"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

// configDir is the profile's persistent CLAUDE_CONFIG_DIR, shared by every
// session of the profile.
func configDir(accounts agents.AccountStore, account agents.Account) string {
	return filepath.Join(accounts.AccountDir(account.ID), "config")
}

func tokenPath(accounts agents.AccountStore, account agents.Account) string {
	return filepath.Join(accounts.CredentialDir(account.ID), "claude-token.json")
}
