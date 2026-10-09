package claude

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

// authStatus is the subset of `claude auth status` JSON Bonsai uses.
type authStatus struct {
	LoggedIn   bool   `json:"loggedIn"`
	AuthMethod string `json:"authMethod"`
	Email      string `json:"email"`
}

func parseAuthStatus(data []byte) (authStatus, bool) {
	var status authStatus
	if err := json.Unmarshal(data, &status); err != nil {
		return authStatus{}, false
	}
	return status, true
}

// profileCommand builds a claude invocation inside the profile's environment.
func (p *Provider) profileCommand(account agents.Account, args ...string) (agents.PreparedSession, error) {
	executable, err := p.binaryResolver.Resolve()
	if err != nil {
		return agents.PreparedSession{}, err
	}
	dir := configDir(p.accounts, account)
	return agents.PreparedSession{
		Executable:       executable,
		Args:             args,
		Dir:              dir,
		EnvSet:           profileEnvironment(dir, account, agents.Session{}, ""),
		EnvUnsetPrefixes: scrubPrefixes,
	}, nil
}

// queryStatus runs `claude auth status` for a login profile. known is false
// when the status could not be determined (timeout, missing binary, bad output);
// that is never an error.
func (p *Provider) queryStatus(ctx context.Context, account agents.Account) (status authStatus, known bool) {
	p.statusMu.Lock()
	entry, ok := p.statuses[account.ID]
	p.statusMu.Unlock()
	if ok && p.now().Sub(entry.fetchedAt) < statusTTL {
		return entry.status, true
	}
	prepared, err := p.profileCommand(account, "auth", "status")
	if err != nil {
		return authStatus{}, false
	}
	ctx, cancel := context.WithTimeout(ctx, statusTimeout)
	defer cancel()
	out, _ := p.runner.Output(ctx, prepared)
	// Exit code 1 means logged out but still prints the JSON.
	status, ok = parseAuthStatus(out)
	if !ok || ctx.Err() != nil {
		return authStatus{}, false
	}
	p.statusMu.Lock()
	p.statuses[account.ID] = statusEntry{status: status, fetchedAt: p.now()}
	p.statusMu.Unlock()
	return status, true
}

func (p *Provider) forgetStatus(id agents.AccountID) {
	p.statusMu.Lock()
	delete(p.statuses, id)
	p.statusMu.Unlock()
}

// storedIdentity reads the signed-in email from <configDir>/.claude.json.
func storedIdentity(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, ".claude.json"))
	if err != nil {
		return ""
	}
	var file struct {
		OAuthAccount struct {
			EmailAddress string `json:"emailAddress"`
		} `json:"oauthAccount"`
	}
	if json.Unmarshal(data, &file) != nil {
		return ""
	}
	return strings.TrimSpace(file.OAuthAccount.EmailAddress)
}

// DescribeAccount returns display-safe profile details. It never returns an
// error: anything it cannot determine is reported as unknown.
func (p *Provider) DescribeAccount(ctx context.Context, account agents.Account) agents.AccountInfo {
	settings, err := ParseSettings(account)
	if err != nil {
		return agents.AccountInfo{Warnings: []string{"Profile settings are invalid"}}
	}
	info := agents.AccountInfo{AuthMode: settings.AuthMode, Options: map[string]any{}}
	if settings.Model != "" {
		info.Options["model"] = settings.Model
	}
	if settings.PermissionMode != "" {
		info.Options["permission_mode"] = settings.PermissionMode
	}
	if settings.Effort != "" {
		info.Options["effort"] = settings.Effort
	}
	if settings.AuthMode == AuthToken {
		info.Options["auth_status"], info.Warnings = p.describeToken(account)
		return info
	}
	info.Identity = storedIdentity(configDir(p.accounts, account))
	state := "unknown"
	if status, known := p.queryStatus(ctx, account); known {
		state = "logged_out"
		if status.LoggedIn {
			state = "logged_in"
			if info.Identity == "" {
				info.Identity = status.Email
			}
		} else {
			info.Warnings = append(info.Warnings, "Not logged in; start a session and run /login")
		}
	}
	info.Options["auth_status"] = state
	return info
}

func (p *Provider) describeToken(account agents.Account) (state string, warnings []string) {
	token, err := readToken(tokenPath(p.accounts, account))
	if err != nil {
		return "missing", []string{"Token missing; remove and add the profile again"}
	}
	remaining := token.ExpiresAt.Sub(p.now())
	switch {
	case token.ExpiresAt.IsZero():
		return "token", nil
	case remaining <= 0:
		return "expired", []string{"Token expired; remove and add the profile again"}
	case remaining < 30*24*time.Hour:
		return "token", []string{"Token expires in " + humanDays(remaining) + "; plan to replace it"}
	}
	return "token", nil
}

func humanDays(d time.Duration) string {
	days := int(d.Hours()/24) + 1
	if days == 1 {
		return "1 day"
	}
	return fmt.Sprintf("%d days", days)
}
