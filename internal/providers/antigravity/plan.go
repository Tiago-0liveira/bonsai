package antigravity

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

// The plan comes from the Cloud Code API: loadCodeAssist names it in
// paidTier.name, in the same call that names the project the quota is read
// for. Usage keeps the last plan it saw in <AccountDir>/plan.json, so listing
// profiles and a usage read that falls back to agy only read that file; none
// of them starts agy.
const maxPlanLength = 40

type planState struct {
	Plan      string    `json:"plan,omitempty"`
	Email     string    `json:"email,omitempty"`
	CheckedAt time.Time `json:"checked_at,omitzero"`
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

// savePlan stores the plan the API reported for email, unless it is stored
// already.
func (p *Provider) savePlan(account agents.Account, email, plan string) {
	if p.accounts == nil || email == "" || plan == "" {
		return
	}
	if state := p.readPlanState(account); state.Plan == plan && strings.EqualFold(state.Email, email) {
		return
	}
	_ = writePrivateJSON(planPath(p.accounts, account), planState{Plan: plan, Email: email, CheckedAt: p.clock().UTC()})
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
