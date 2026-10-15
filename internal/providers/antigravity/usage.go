package antigravity

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

// Credits move slowly and only `/credits` reports them, so a usage read from
// the Cloud Code API shows the last balance and reads a new one in the
// background when this one is older than creditsMaxAge, or on an explicit
// refresh. A failed read is retried after creditsRetryAfter.
const (
	creditsMaxAge     = 15 * time.Minute
	creditsRetryAfter = 15 * time.Minute
)

// creditsState is <AccountDir>/credits.json: the last `/credits` balance.
type creditsState struct {
	Balance   *float64  `json:"balance,omitempty"`
	CheckedAt time.Time `json:"checked_at,omitzero"`
	FailedAt  time.Time `json:"failed_at,omitzero"`
}

func creditsPath(accounts agents.AccountStore, account agents.Account) string {
	return filepath.Join(accounts.AccountDir(account.ID), "credits.json")
}

// Usage reads the quota buckets from the Cloud Code API and, when that fails,
// from agy's `/usage`, with the credit balance (`/credits`); both built-in
// commands cost no tokens. The plan is the one last read from agy's screen. A
// due plan probe or credit read starts in the background.
func (p *Provider) Usage(ctx context.Context, account agents.Account, opts agents.UsageOptions) (agents.UsageSnapshot, error) {
	snapshot := agents.UsageSnapshot{Provider: ProviderID, AccountID: account.ID}
	if limits, err := p.cloudCodeUsage(ctx, account); err == nil {
		snapshot.Limits = limits
		snapshot.Credits = creditsOf(p.readCreditsState(account))
		p.refreshCredits(account, opts.Refresh)
	} else {
		limits, credits, err := p.cliUsage(ctx, account)
		if err != nil {
			return agents.UsageSnapshot{}, err
		}
		snapshot.Limits, snapshot.Credits = limits, credits
	}
	snapshot.FetchedAt = time.Now().UTC()
	if p.accounts != nil {
		if email, complete, _ := p.signedIn(account); complete {
			snapshot.Plan = p.planFor(account, email)
			p.refreshPlan(account, email, opts.Refresh)
		}
	}
	return snapshot, nil
}

// cliUsage runs `/usage` and `/credits` in a throwaway session. Credits are
// extra: without them the usage still stands.
func (p *Provider) cliUsage(ctx context.Context, account agents.Account) ([]agents.UsageLimit, []agents.UsageCredit, error) {
	var limits []agents.UsageLimit
	var credits []agents.UsageCredit
	err := p.withProfile(ctx, account, func(ctx context.Context, run profileRun) error {
		out, err := p.runner()(ctx, run.executable, run.dir, run.env, "--dangerously-skip-permissions", "-p", "/usage", "--output-format", "json")
		if err != nil {
			return err
		}
		if limits, err = ParseUsage(out); err != nil {
			return err
		}
		if balance, err := p.runCredits(ctx, run); err == nil {
			p.saveCredits(account, creditsState{Balance: &balance, CheckedAt: p.clock().UTC()})
			credits = creditsOf(creditsState{Balance: &balance})
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	return limits, credits, nil
}

var errNoCredits = errors.New("antigravity /credits: no balance")

func (p *Provider) runCredits(ctx context.Context, run profileRun) (float64, error) {
	out, err := p.runner()(ctx, run.executable, run.dir, run.env, "--dangerously-skip-permissions", "-p", "/credits", "--output-format", "json")
	if err != nil {
		return 0, err
	}
	balance, ok := ParseCredits(out)
	if !ok {
		return 0, errNoCredits
	}
	return balance, nil
}

// refreshCredits reads `/credits` in the background when the stored balance
// is due, or always when forced. One read runs per profile at a time.
func (p *Provider) refreshCredits(account agents.Account, force bool) {
	if p.accounts == nil {
		return
	}
	state := p.readCreditsState(account)
	now := p.clock()
	if !force && (now.Sub(state.CheckedAt) < creditsMaxAge || now.Sub(state.FailedAt) < creditsRetryAfter) {
		return
	}
	base, done, ok := p.startFlight(flightKey{"credits", account.ID})
	if !ok {
		return
	}
	go func() {
		defer done()
		var balance float64
		err := p.withProfile(base, account, func(ctx context.Context, run profileRun) error {
			var err error
			balance, err = p.runCredits(ctx, run)
			return err
		})
		if base.Err() != nil {
			return // cancelled: no backoff
		}
		state := p.readCreditsState(account)
		if err != nil {
			state.FailedAt = p.clock().UTC()
		} else {
			state = creditsState{Balance: &balance, CheckedAt: p.clock().UTC()}
		}
		p.saveCredits(account, state)
	}()
}

func (p *Provider) readCreditsState(account agents.Account) creditsState {
	var state creditsState
	if p.accounts == nil {
		return state
	}
	if data, err := os.ReadFile(creditsPath(p.accounts, account)); err == nil {
		_ = json.Unmarshal(data, &state)
	}
	return state
}

func (p *Provider) saveCredits(account agents.Account, state creditsState) {
	if p.accounts != nil {
		_ = writePrivateJSON(creditsPath(p.accounts, account), state)
	}
}

func creditsOf(state creditsState) []agents.UsageCredit {
	if state.Balance == nil {
		return nil
	}
	balance := *state.Balance
	return []agents.UsageCredit{{ID: "antigravity_credits", Label: "Credits", Unit: agents.CreditUnitCredits, Balance: &balance, Enabled: true}}
}

// ParseCredits reads `remaining_credits` from `/credits` JSON, at the top level
// or inside the command result agy wraps it in. The upgrade link is ignored.
func ParseCredits(data []byte) (float64, bool) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var root any
	if dec.Decode(&root) != nil {
		return 0, false
	}
	return findCredits(root, 0)
}

func findCredits(v any, depth int) (float64, bool) {
	m, ok := v.(map[string]any)
	if !ok || depth > 4 {
		return 0, false
	}
	if n, ok := m["remaining_credits"].(json.Number); ok {
		if f, err := n.Float64(); err == nil && f >= 0 {
			return f, true
		}
	}
	for _, key := range []string{"command", "data", "result"} {
		if f, ok := findCredits(m[key], depth+1); ok {
			return f, true
		}
	}
	return 0, false
}
