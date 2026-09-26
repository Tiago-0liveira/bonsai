//go:build !linux && !darwin && !windows

package antigravity

import "github.com/Tiago-0liveira/bonsai/internal/core/agents"

func platformCredentialEnv(agents.Session) map[string]string { return nil }
