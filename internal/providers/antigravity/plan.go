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

// refreshPlan probes the plan in the background when it is due, or always when
// forced (an explicit usage refresh). One probe runs per profile and process,
// five in the whole process, and one per profile across processes; a probe
// that is already running is not repeated. Only a probe that ran agy and found
// no plan backs off: waiting for a slot or the lock, or failing before agy
// started, leaves plan.json as it was.
func (p *Provider) refreshPlan(account agents.Account, email string, force bool) {
	if !planProbeSupported || p.accounts == nil || email == "" {
		return
	}
	before := p.readPlanState(account)
	if !force && !needsProbe(before, email, p.clock()) {
		return
	}
	p.planMu.Lock()
	if p.planFlights == nil {
		p.planFlights = map[agents.AccountID]bool{}
	}
	if p.planFlights[account.ID] {
		p.planMu.Unlock()
		return
	}
	p.planFlights[account.ID] = true
	p.planWG.Add(1)
	p.planMu.Unlock()
	go func() {
		defer func() {
			p.planMu.Lock()
			delete(p.planFlights, account.ID)
			p.planMu.Unlock()
			p.planWG.Done()
		}()
		logf := openProbeLog(planLogPath(p.accounts, account))
		defer logf.Close()
		ctx := withProbeLog(context.Background(), logf)
		logf.Printf("probe for %s (profile %s, force=%t)", email, account.ID, force)
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

// waitPlanProbes waits for the background probes; tests use it.
func (p *Provider) waitPlanProbes() { p.planWG.Wait() }

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
