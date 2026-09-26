//go:build darwin

package antigravity

import "github.com/Tiago-0liveira/bonsai/internal/core/agents"

func platformCredentialEnv(agents.Session) map[string]string {
	// Force file storage in the isolated session HOME. This deliberately avoids
	// mutating the user's login keychain and gives concurrent sessions private
	// mutable authentication state.
	return nil
}
