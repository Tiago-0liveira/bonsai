package claude

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

// RemoveAccount logs the profile out before Bonsai deletes its directory. On
// macOS this clears the profile's Keychain entry. It is best effort: the caller
// turns a failure into a warning and removes the profile anyway.
func (p *Provider) RemoveAccount(ctx context.Context, account agents.Account) error {
	defer p.forgetStatus(account.ID)
	settings, err := ParseSettings(account)
	if err != nil || settings.AuthMode != AuthLogin {
		return nil
	}
	if _, err := os.Stat(configDir(p.accounts, account)); err != nil {
		return nil
	}
	prepared, err := p.profileCommand(account, "auth", "logout")
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, logoutTimeout)
	defer cancel()
	if _, err := p.runner.Output(ctx, prepared); err != nil {
		return fmt.Errorf("claude auth logout: %s", strings.TrimSpace(err.Error()))
	}
	return nil
}
