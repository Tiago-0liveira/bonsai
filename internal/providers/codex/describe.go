package codex

import (
	"context"
	"fmt"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

// DescribeAccount returns display-safe profile details. It never returns an
// error: anything it cannot determine is reported as unknown.
func (p *Provider) DescribeAccount(ctx context.Context, account agents.Account) agents.AccountInfo {
	settings, err := ParseSettings(account)
	if err != nil {
		return agents.AccountInfo{Warnings: []string{"Profile settings are invalid"}}
	}
	info := agents.AccountInfo{AuthMode: settings.AuthMode, Options: map[string]any{}}
	for key, value := range map[string]string{"model": settings.Model, "effort": settings.Effort, "sandbox": settings.Sandbox, "approval": settings.Approval} {
		if value != "" {
			info.Options[key] = value
		}
	}
	id := readIdentity(homeDir(p.accounts, account))
	info.Identity = id.String()
	info.Plan = planLabel(id.Plan)
	status := p.queryStatus(ctx, account)
	info.Options["auth_status"] = status.String()
	switch {
	case status == statusLoggedOut:
		info.Warnings = append(info.Warnings, fmt.Sprintf("Not logged in; run `bonsai agent account login %s`", account.Name))
	case status == statusLoggedIn && !id.Present:
		// Only a store enforced by an administrator keeps the login elsewhere.
		info.Warnings = append(info.Warnings, "Login is not stored in the profile (keyring storage enforced?); identity is unknown")
	}
	if other := p.sameLoginAs(account.ID, id); other != "" {
		info.Warnings = append(info.Warnings, fmt.Sprintf("Same login as profile %q; both share its usage limits", other))
	}
	return info
}

// sameLoginAs names another Codex profile signed in to the same ChatGPT
// account, or returns "".
func (p *Provider) sameLoginAs(self agents.AccountID, id identity) string {
	if id.Email == "" {
		return ""
	}
	accounts, err := p.accounts.List()
	if err != nil {
		return ""
	}
	for _, other := range accounts {
		if other.Provider != ProviderID || other.ID == self {
			continue
		}
		if id.sameLogin(readIdentity(homeDir(p.accounts, other))) {
			return other.Name
		}
	}
	return ""
}
