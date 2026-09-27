//go:build linux

package antigravity

import "github.com/Tiago-0liveira/bonsai/internal/core/agents"

func platformCredentialEnv(agents.Session) map[string]string {
	// GEMINI_FORCE_FILE_STORAGE is set by environment.go, keeping provider auth
	// inside the isolated session HOME instead of the host keyring.
	return nil
}
