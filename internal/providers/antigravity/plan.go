package antigravity

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

// agy has no command that names the plan: it only paints "email (plan)" at the
// top of its interactive screen. Bonsai reads that screen in a PTY now and then
// (probePlan) and keeps the answer in <AccountDir>/plan.json. Listing profiles
// only reads that file; it never starts agy.
const (
	planMaxAge     = 24 * time.Hour
	planRetryAfter = 15 * time.Minute
	maxPlanLength  = 40
)

// planProbes allows five probes at a time in the whole process. A probe waits
// for a slot with no clock running: its time only starts with agy.
var planProbes = make(chan struct{}, 5)

// planLockWait caps how long a probe waits for another process probing the
// same profile, about one probe's run; then it is skipped, not failed.
// A variable so tests can shorten it.
var planLockWait = 12 * time.Second

type planState struct {
	Plan      string    `json:"plan,omitempty"`
	Email     string    `json:"email,omitempty"`
	CheckedAt time.Time `json:"checked_at,omitzero"`
	FailedAt  time.Time `json:"failed_at,omitzero"`
}

func planPath(accounts agents.AccountStore, account agents.Account) string {
	return filepath.Join(accounts.AccountDir(account.ID), "plan.json")
}

func (p *Provider) readPlanState(account agents.Account) planState {
	var state planState
	if data, err := os.ReadFile(planPath(p.accounts, account)); err == nil {
		_ = json.Unmarshal(data, &state)
	}
	return state
}

// planFor is the stored plan of the signed-in email, or "".
func (p *Provider) planFor(account agents.Account, email string) string {
	if p.accounts == nil || email == "" {
		return ""
	}
	if state := p.readPlanState(account); strings.EqualFold(state.Email, email) {
		return state.Plan
	}
	return ""
}

// needsProbe: a new email, no plan yet or a plan older than a day, unless a
// probe for this email failed in the last 15 minutes.
func needsProbe(state planState, email string, now time.Time) bool {
	switch {
	case email == "":
		return false
	case !strings.EqualFold(state.Email, email):
		return true
	case !state.FailedAt.IsZero() && now.Sub(state.FailedAt) < planRetryAfter:
		return false
	case state.Plan == "":
		return true
	}
	return now.Sub(state.CheckedAt) > planMaxAge
}

// refreshPlan probes the plan in the background when it is due: a new email, no
// plan yet, or one older than a day. An explicit usage refresh does not force
// one; the plan barely changes. One probe runs per profile and process,
// five in the whole process, and one per profile across processes; a probe
// that is already running is not repeated. Only a probe that ran agy and found
// no plan backs off: waiting for a slot or the lock, or failing before agy
// started, leaves plan.json as it was.
func (p *Provider) refreshPlan(account agents.Account, email string) {
	if !planProbeSupported || p.accounts == nil || email == "" {
		return
	}
	before := p.readPlanState(account)
	if !needsProbe(before, email, p.clock()) {
		return
	}
	base, done, ok := p.startFlight(flightKey{"plan", account.ID})
	if !ok {
		return
	}
	go func() {
		defer done()
		logf := openProbeLog(planLogPath(p.accounts, account))
		defer logf.Close()
		ctx := withProbeLog(base, logf)
		logf.Printf("probe for %s (profile %s)", email, account.ID)
		logf.Printf("stored state: plan=%q email=%q checked_at=%s failed_at=%s",
			before.Plan, before.Email, before.CheckedAt.Format(time.RFC3339), before.FailedAt.Format(time.RFC3339))
		select {
		case planProbes <- struct{}{}:
			defer func() { <-planProbes }()
		case <-ctx.Done():
			logf.Printf("gave up waiting for a probe slot: %v", ctx.Err())
			return
		}
		lockCtx, cancelLock := context.WithTimeout(ctx, planLockWait)
		lock, err := agents.LockFileContext(lockCtx, probeLockPath(p.accounts, account))
		cancelLock()
		if err != nil {
			logf.Printf("skipped: another process is probing this profile (%v)", err)
			return
		}
		defer lock.Unlock()
		plan, ran, err := p.probeAccountPlan(ctx, account, email)
		if !ran {
			logf.Printf("skipped: agy did not start (%v); no backoff", err)
			return
		}
		if ctx.Err() != nil {
			logf.Printf("cancelled: %v; no backoff", ctx.Err())
			return
		}
		state := p.readPlanState(account)
		if !strings.EqualFold(state.Email, email) {
			state = planState{Email: email}
		}
		if err != nil || plan == "" {
			state.FailedAt = p.clock().UTC()
			logf.Printf("probe failed: err=%v plan=%q; next try after %s", err, plan, planRetryAfter)
		} else {
			state = planState{Plan: plan, Email: email, CheckedAt: p.clock().UTC()}
			logf.Printf("probe ok: plan=%q", plan)
		}
		if err := writePrivateJSON(planPath(p.accounts, account), state); err != nil {
			logf.Printf("could not write %s: %v", planPath(p.accounts, account), err)
		}
	}()
}

// probeAccountPlan runs the probe in the profile's persistent probe home, from
// its trusted workspace, with the credentials materialized before and
// reconciled after. ran tells whether the probe itself started.
func (p *Provider) probeAccountPlan(ctx context.Context, account agents.Account, email string) (plan string, ran bool, err error) {
	probe := p.probe
	if probe == nil {
		probe = probePlan
	}
	logf := probeLogFrom(ctx)
	home, workspace, err := p.ensureProbeHome(ctx, account)
	if err != nil {
		logf.Printf("could not prepare the probe home: %v", err)
		return "", false, err
	}
	err = p.withHome(ctx, account, home, workspace, func(ctx context.Context, run profileRun) error {
		logf.Printf("starting %s in %s (HOME=%s)", run.executable, run.dir, home)
		ran = true
		var probeErr error
		plan, probeErr = probe(ctx, run.executable, run.dir, agents.BuildEnvironment(os.Environ(), run.env, nil), email)
		return probeErr
	})
	if err != nil {
		logf.Printf("profile run ended with error: %v", err)
	}
	return plan, ran, err
}

// ensureProbeHome creates the profile's probe home and its workspace and
// trusts the workspace in the vault. Profiles set up before the probe had a
// home get both on their first probe; afterwards nothing is written.
func (p *Provider) ensureProbeHome(ctx context.Context, account agents.Account) (home, workspace string, err error) {
	home, workspace = probeHomeDir(p.accounts, account), probeWorkspaceDir(p.accounts, account)
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		return "", "", err
	}
	if err := (&fileCredentialManager{accounts: p.accounts}).ensureTrusted(ctx, account, workspace); err != nil {
		return "", "", err
	}
	return home, workspace, nil
}

// flightKey names one kind of background work for one profile.
type flightKey struct {
	kind    string
	account agents.AccountID
}

// startFlight claims key for background work and returns its context and the
// func that releases the claim; ok is false while the same work already runs.
// SettleUsage waits for every flight and cancels them when its ctx ends.
func (p *Provider) startFlight(key flightKey) (ctx context.Context, done func(), ok bool) {
	p.planMu.Lock()
	defer p.planMu.Unlock()
	if p.planFlights[key] {
		return nil, nil, false
	}
	if p.planFlights == nil {
		p.planFlights = map[flightKey]bool{}
	}
	p.planFlights[key] = true
	if p.planCtx == nil {
		p.planCtx, p.planCancel = context.WithCancel(context.Background())
	}
	p.planWG.Add(1)
	return p.planCtx, func() {
		p.planMu.Lock()
		delete(p.planFlights, key)
		p.planMu.Unlock()
		p.planWG.Done()
	}, true
}

// waitPlanProbes waits for the background probes and credit reads; tests use it.
func (p *Provider) waitPlanProbes() { p.planWG.Wait() }

// SettleUsage waits for the plan probes and credit reads Usage started, so a
// CLI run that exits right after its usage does not cut one off. When ctx ends
// first, they are cancelled (agy is stopped, the token reconciled) and waited
// for; a cancelled one records no failure.
func (p *Provider) SettleUsage(ctx context.Context) {
	done := make(chan struct{})
	go func() {
		p.planWG.Wait()
		close(done)
	}()
	select {
	case <-done:
		return
	case <-ctx.Done():
	}
	p.planMu.Lock()
	if p.planCancel != nil {
		p.planCancel()
		p.planCtx, p.planCancel = nil, nil
	}
	p.planMu.Unlock()
	<-done
}

// findPlan reads "<email> (<plan>)" from the screen. Only the signed-in email
// counts, so text that merely looks like a header is not taken.
func findPlan(lines []string, email string) (string, bool) {
	if email == "" {
		return "", false
	}
	re, err := regexp.Compile(`(?i)` + regexp.QuoteMeta(email) + `\s*\(([^()]+)\)`)
	if err != nil {
		return "", false
	}
	for _, line := range lines {
		if m := re.FindStringSubmatch(line); m != nil {
			if plan := cleanPlan(m[1]); plan != "" {
				return plan, true
			}
		}
	}
	return "", false
}

// cleanPlan keeps at most 40 printable characters of a plan name.
func cleanPlan(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsPrint(r) {
			return r
		}
		return -1
	}, strings.TrimSpace(s))
	if r := []rune(s); len(r) > maxPlanLength {
		s = strings.TrimSpace(string(r[:maxPlanLength]))
	}
	return s
}

// signedIn reads the profile's stored login without starting agy: whether
// its credentials are complete, and its email when the login named one.
func (p *Provider) signedIn(account agents.Account) (email string, complete bool, err error) {
	vault, err := (&fileCredentialManager{accounts: p.accounts}).readVault(account)
	if errors.Is(err, agents.ErrNotAuthenticated) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if len(vault.OAuth) == 0 {
		return "", false, nil
	}
	if !isFallbackIdentity(vault.Identity, account) && strings.Contains(vault.Identity, "@") {
		email = vault.Identity
	}
	return email, true, nil
}
