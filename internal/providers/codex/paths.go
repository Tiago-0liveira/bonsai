package codex

import (
	"path/filepath"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

// homeDir is the profile's persistent CODEX_HOME, shared by every session of
// the profile. Codex keeps its config, auth.json, sessions and state in it.
func homeDir(accounts agents.AccountStore, account agents.Account) string {
	return filepath.Join(accounts.AccountDir(account.ID), "home")
}

func authPath(home string) string { return filepath.Join(home, "auth.json") }

func configPath(home string) string { return filepath.Join(home, "config.toml") }
