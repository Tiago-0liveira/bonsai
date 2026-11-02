package codex

import (
	"errors"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

func TestReadOnlyIsRefused(t *testing.T) {
	e := newTestEnv(t)
	account := e.addLogin("planner")
	if s := agents.ReadOnlyOf(e.provider); s.Supported || s.Reason == "" {
		t.Fatalf("verdict = %+v", s)
	}
	for _, settings := range []string{`{}`, `{"sandbox":"bypass"}`, `{"sandbox":"read-only","approval":"never"}`} {
		t.Run(settings, func(t *testing.T) {
			account.Settings = []byte(settings)
			if err := e.provider.ValidateLaunch(account, agents.LaunchOptions{ReadOnly: true}); !errors.Is(err, agents.ErrReadOnlyUnsupported) {
				t.Fatalf("ValidateLaunch = %v", err)
			}
			_, err := e.provider.PrepareSession(t.Context(), agents.PrepareSessionRequest{Account: account, Launch: agents.LaunchOptions{ReadOnly: true}})
			if !errors.Is(err, agents.ErrReadOnlyUnsupported) {
				t.Fatalf("PrepareSession = %v", err)
			}
		})
	}
}
