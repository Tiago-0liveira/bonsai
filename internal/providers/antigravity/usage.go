package antigravity

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

// cliRaceDelay is how long the Cloud Code read has to produce a Gemini pool
// before agy's `/usage` is started beside it. Production answers in well under
// a second, so agy is not started for a profile that is read normally.
const cliRaceDelay = 1500 * time.Millisecond

// geminiMissTTL is how long a profile whose Gemini pool neither source listed
// goes without agy being started for it again.
const geminiMissTTL = 10 * time.Minute

type geminiMissCache struct {
	mu     sync.Mutex
	missed map[agents.AccountID]time.Time
}

func (c *geminiMissCache) recent(id agents.AccountID, now time.Time) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	at, ok := c.missed[id]
	return ok && now.Sub(at) < geminiMissTTL
}

func (c *geminiMissCache) set(id agents.AccountID, now time.Time, missed bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !missed {
		delete(c.missed, id)
		return
	}
	if c.missed == nil {
		c.missed = map[agents.AccountID]time.Time{}
	}
	c.missed[id] = now
}

// Usage reads the quota buckets, the plan and the credit balance from the
// Cloud Code API and, when that fails or lists no Gemini pool, the quota from
// agy's `/usage` (a built-in command that costs no tokens). The two race: agy
// is started when the API read has failed, has answered without a Gemini pool,
// or is slow, and the first source that lists a Gemini pool wins. The agy
// result has no plan or credits of its own: the plan is the last one stored and
// the credits are left out.
func (p *Provider) Usage(ctx context.Context, account agents.Account, _ agents.UsageOptions) (agents.UsageSnapshot, error) {
	snapshot := agents.UsageSnapshot{Provider: ProviderID, AccountID: account.ID}
	read, err := p.readQuota(ctx, account)
	if err != nil {
		return agents.UsageSnapshot{}, err
	}
	snapshot.Limits, snapshot.Credits = read.limits, read.credits
	snapshot.FetchedAt = time.Now().UTC()
	if p.accounts != nil {
		if email, complete, _ := p.signedIn(account); complete {
			p.savePlan(account, email, read.plan)
			snapshot.Plan = p.planFor(account, email)
		}
	}
	if read.plan != "" {
		snapshot.Plan = read.plan
	}
	return snapshot, nil
}

// readQuota races the Cloud Code read against `/usage`. A source that lists a
// Gemini pool wins at once; otherwise the best answer without one is used, the
// API's first because it also carries the plan and credits.
func (p *Provider) readQuota(ctx context.Context, account agents.Account) (cloudCodeRead, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	type cloudOutcome struct {
		read cloudCodeRead
		err  error
	}
	type cliOutcome struct {
		limits []agents.UsageLimit
		err    error
	}
	cloudCh := make(chan cloudOutcome, 1)
	cliCh := make(chan cliOutcome, 1)
	go func() {
		read, err := p.cloudCodeUsage(ctx, account)
		cloudCh <- cloudOutcome{read, err}
	}()
	cloudPending, cliPending, cliStarted := true, false, false
	skipCLI := p.noGemini.recent(account.ID, p.clock())
	startCLI := func() {
		if cliStarted || skipCLI {
			return
		}
		cliStarted, cliPending = true, true
		go func() {
			limits, err := p.cliUsage(ctx, account)
			cliCh <- cliOutcome{limits, err}
		}()
	}
	delay := time.NewTimer(cliRaceDelay)
	defer delay.Stop()

	var cloudBest, cliBest *cloudCodeRead
	var errs []error
	for cloudPending || cliPending {
		select {
		case o := <-cloudCh:
			cloudPending = false
			switch {
			case o.err != nil:
				errs = append(errs, o.err)
			case hasGeminiPool(o.read.limits):
				p.noGemini.set(account.ID, p.clock(), false)
				return o.read, nil
			default:
				read := o.read
				cloudBest = &read
			}
			startCLI()
		case <-delay.C:
			startCLI()
		case o := <-cliCh:
			cliPending = false
			switch {
			case o.err != nil:
				errs = append(errs, o.err)
			case hasGeminiPool(o.limits):
				p.noGemini.set(account.ID, p.clock(), false)
				return cloudCodeRead{limits: o.limits, plan: planOf(cloudBest), credits: creditsOf(cloudBest)}, nil
			default:
				cliBest = &cloudCodeRead{limits: o.limits}
			}
		}
	}
	// Both sources finished without a Gemini pool: this profile has none.
	if cloudBest != nil || cliBest != nil {
		p.noGemini.set(account.ID, p.clock(), true)
	}
	if cloudBest != nil {
		return *cloudBest, nil
	}
	if cliBest != nil {
		return *cliBest, nil
	}
	return cloudCodeRead{}, errors.Join(errs...)
}

func planOf(read *cloudCodeRead) string {
	if read == nil {
		return ""
	}
	return read.plan
}

func creditsOf(read *cloudCodeRead) []agents.UsageCredit {
	if read == nil {
		return nil
	}
	return read.credits
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
