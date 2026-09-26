//go:build windows

package antigravity

import "github.com/Tiago-0liveira/bonsai/internal/core/agents"

func platformCredentialEnv(agents.Session) map[string]string {
	// environment.go also redirects the Windows profile/AppData locations.
	// Forced file storage prevents shared Credential Manager state from becoming
	// the account/session identity boundary.
	return nil
}
