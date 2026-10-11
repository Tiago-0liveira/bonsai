package codex

import (
	"context"
	"fmt"
	"os"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

// RemoveAccount logs the profile out before Bonsai deletes its home, which
// also clears a login an enforced keyring store holds outside the home. It is
// best effort: the caller turns a failure into a warning and removes the
// profile anyway.
func (p *Provider) RemoveAccount(ctx context.Context, account agents.Account) error {
	defer p.forgetStatus(account.ID)
	home := homeDir(p.accounts, account)
	if _, err := os.Stat(home); err != nil {
		return nil
	}
	if !readIdentity(home).Present && p.queryStatus(ctx, account) != statusLoggedIn {
		return nil
	}
	prepared, err := p.profileCommand(account, "logout")
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, logoutTimeout)
	defer cancel()
	if _, err := p.runner.Output(ctx, prepared, nil); err != nil {
		return fmt.Errorf("codex logout: %v", err)
	}
	return nil
}
