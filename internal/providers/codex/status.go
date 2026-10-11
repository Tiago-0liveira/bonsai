package codex

import (
	"context"
	"errors"
	"os/exec"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

type authStatus int

const (
	statusUnknown authStatus = iota
	statusLoggedIn
	statusLoggedOut
)

func (s authStatus) String() string {
	switch s {
	case statusLoggedIn:
		return "logged_in"
	case statusLoggedOut:
		return "logged_out"
	}
	return "unknown"
}

// profileCommand builds a codex invocation inside the profile's environment,
// with the file credential store forced.
func (p *Provider) profileCommand(account agents.Account, args ...string) (agents.PreparedSession, error) {
	executable, err := p.binaryResolver.Resolve()
	if err != nil {
		return agents.PreparedSession{}, err
	}
	home := homeDir(p.accounts, account)
	return agents.PreparedSession{
		Executable:       executable,
		Args:             append([]string{"-c", storeOverride}, args...),
		Dir:              home,
		EnvSet:           p.profileEnvironment(home, account, agents.Session{}),
		EnvUnsetPrefixes: scrubPrefixes,
	}, nil
}

// statusFromExit maps `codex login status`: exit 0 is logged in, exit 1 logged
// out, anything else (timeout, crash, missing binary) unknown.
func statusFromExit(err error) authStatus {
	if err == nil {
		return statusLoggedIn
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1 {
		return statusLoggedOut
	}
	return statusUnknown
}

// queryStatus runs `codex login status` for the profile. Known results are
// cached for a minute; an unknown one briefly, so a hung binary is not re-run
// (and waited on) for every poll of the profile list.
func (p *Provider) queryStatus(ctx context.Context, account agents.Account) authStatus {
	p.statusMu.Lock()
	entry, ok := p.statuses[account.ID]
	p.statusMu.Unlock()
	if ok {
		ttl := statusTTL
		if entry.status == statusUnknown {
			ttl = unknownStatusTTL
		}
		if p.now().Sub(entry.fetchedAt) < ttl {
			return entry.status
		}
	}
	prepared, err := p.profileCommand(account, "login", "status")
	if err != nil {
		return statusUnknown
	}
	parent := ctx
	ctx, cancel := context.WithTimeout(ctx, statusTimeout)
	defer cancel()
	_, err = p.runner.Output(ctx, prepared, nil)
	status := statusFromExit(err)
	if ctx.Err() != nil {
		status = statusUnknown
	}
	if parent.Err() == nil { // a caller's cancellation says nothing about Codex
		p.statusMu.Lock()
		p.statuses[account.ID] = statusEntry{status: status, fetchedAt: p.now()}
		p.statusMu.Unlock()
	}
	return status
}

func (p *Provider) forgetStatus(id agents.AccountID) {
	p.statusMu.Lock()
	delete(p.statuses, id)
	p.statusMu.Unlock()
}
