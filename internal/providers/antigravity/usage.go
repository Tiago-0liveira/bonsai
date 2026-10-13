package antigravity

import (
	"bytes"
	"context"
	"encoding/json"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

// Usage reads the quota buckets (`/usage`) and the credit balance
// (`/credits`), both built-in commands that cost no tokens, and the plan last
// read from agy's screen. A due plan probe starts in the background.
func (p *Provider) Usage(ctx context.Context, account agents.Account, opts agents.UsageOptions) (agents.UsageSnapshot, error) {
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
		// Credits are extra: without them the usage still stands.
		if out, err := p.runner()(ctx, run.executable, run.dir, run.env, "--dangerously-skip-permissions", "-p", "/credits", "--output-format", "json"); err == nil {
			if balance, ok := ParseCredits(out); ok {
				credits = []agents.UsageCredit{{ID: "antigravity_credits", Label: "Credits", Unit: agents.CreditUnitCredits, Balance: &balance, Enabled: true}}
			}
		}
		return nil
	})
	if err != nil {
		return agents.UsageSnapshot{}, err
	}
	snapshot := agents.UsageSnapshot{
		Provider:  ProviderID,
		AccountID: account.ID,
		FetchedAt: time.Now().UTC(),
		Limits:    limits,
		Credits:   credits,
	}
	if p.accounts != nil {
		if email, complete, _ := p.signedIn(account); complete {
			snapshot.Plan = p.planFor(account, email)
			p.refreshPlan(account, email, opts.Refresh)
		}
	}
	return snapshot, nil
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
