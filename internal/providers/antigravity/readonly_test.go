package antigravity

import (
	"errors"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

// Antigravity has no enforced read-only mode, so the role is unavailable and the
// launch is refused instead of running unrestricted.
func TestReadOnlyIsRefused(t *testing.T) {
	p := testProvider()
	if s := agents.ReadOnlyOf(p); s.Supported || s.Reason == "" {
		t.Fatalf("verdict = %+v", s)
	}
	account := agents.Account{ID: "acct_ro", Provider: ProviderID}
	if err := p.ValidateLaunch(account, agents.LaunchOptions{ReadOnly: true}); !errors.Is(err, agents.ErrReadOnlyUnsupported) {
		t.Fatalf("ValidateLaunch = %v", err)
	}
	_, err := p.PrepareSession(t.Context(), agents.PrepareSessionRequest{Account: account, Launch: agents.LaunchOptions{ReadOnly: true}})
	if !errors.Is(err, agents.ErrReadOnlyUnsupported) {
		t.Fatalf("PrepareSession = %v", err)
	}
}
