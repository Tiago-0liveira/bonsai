package antigravity

import (
	"context"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

// Usage reads the quota buckets, the plan and the credit balance from the
// Cloud Code API and, when that fails, the quota from agy's `/usage` (a
// built-in command that costs no tokens). The fallback has no plan or credits
// of its own: the plan is the last one stored and the credits are left out.
func (p *Provider) Usage(ctx context.Context, account agents.Account, _ agents.UsageOptions) (agents.UsageSnapshot, error) {
	snapshot := agents.UsageSnapshot{Provider: ProviderID, AccountID: account.ID}
	apiPlan := ""
	if read, err := p.cloudCodeUsage(ctx, account); err == nil {
		snapshot.Limits, snapshot.Credits, apiPlan = read.limits, read.credits, read.plan
	} else {
		limits, err := p.cliUsage(ctx, account)
		if err != nil {
			return agents.UsageSnapshot{}, err
		}
		snapshot.Limits = limits
	}
	snapshot.FetchedAt = time.Now().UTC()
	if p.accounts != nil {
		if email, complete, _ := p.signedIn(account); complete {
			p.savePlan(account, email, apiPlan)
			snapshot.Plan = p.planFor(account, email)
		}
	}
	if apiPlan != "" {
		snapshot.Plan = apiPlan
	}
	return snapshot, nil
}

// cliUsage runs `/usage` in a throwaway session.
func (p *Provider) cliUsage(ctx context.Context, account agents.Account) ([]agents.UsageLimit, error) {
	var limits []agents.UsageLimit
	err := p.withProfile(ctx, account, func(ctx context.Context, run profileRun) error {
		out, err := p.runner()(ctx, run.executable, run.dir, run.env, "--dangerously-skip-permissions", "-p", "/usage", "--output-format", "json")
		if err != nil {
			return err
		}
		limits, err = ParseUsage(out)
		return err
	})
	if err != nil {
		return nil, err
	}
	return limits, nil
}
