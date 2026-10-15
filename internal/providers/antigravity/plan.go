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
	planMaxAge       = 24 * time.Hour
	planRetryAfter   = 15 * time.Minute
	planProbeTimeout = 20 * time.Second
	maxPlanLength    = 40
)

// planProbes allows one probe at a time in the whole process.
var planProbes = make(chan struct{}, 1)

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
// forced (an explicit usage refresh). One probe runs per profile and one in the
// whole process; a probe that is already running is not repeated.
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
		ctx, cancel := context.WithTimeout(context.Background(), planProbeTimeout)
		defer cancel()
		logf := openProbeLog(planLogPath(p.accounts, account))
		defer logf.Close()
		ctx = withProbeLog(ctx, logf)
		logf.Printf("probe for %s (profile %s, force=%t)", email, account.ID, force)
		logf.Printf("stored state: plan=%q email=%q checked_at=%s failed_at=%s",
			before.Plan, before.Email, before.CheckedAt.Format(time.RFC3339), before.FailedAt.Format(time.RFC3339))
		select {
		case planProbes <- struct{}{}:
			defer func() { <-planProbes }()
		case <-ctx.Done():
			logf.Printf("gave up waiting for another probe to finish: %v", ctx.Err())
			return
		}
		plan, err := p.probeAccountPlan(ctx, account, email)
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

// probeAccountPlan runs the probe in a throwaway session of the profile, from
// an empty working directory, and reconciles the credentials after it.
func (p *Provider) probeAccountPlan(ctx context.Context, account agents.Account, email string) (string, error) {
	probe := p.probe
	if probe == nil {
		probe = probePlan
	}
	var plan string
	err := p.withProfile(ctx, account, func(ctx context.Context, run profileRun) error {
		dir, err := os.MkdirTemp("", "bonsai-agy-plan-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(dir)
		logf := probeLogFrom(ctx)
		logf.Printf("starting %s in %s", run.executable, dir)
		plan, err = probe(ctx, run.executable, dir, agents.BuildEnvironment(os.Environ(), run.env, nil), email)
		return err
	})
	if err != nil {
		probeLogFrom(ctx).Printf("profile run ended with error: %v", err)
	}
	return plan, err
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
