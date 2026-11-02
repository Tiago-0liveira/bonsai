package config

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Review trigger values. Manual never starts a reviewer on its own.
const (
	GardenReviewTriggerManual         = "manual"
	GardenReviewTriggerAwaitingReview = "awaiting-review"
	GardenReviewTriggerNewCommit      = "new-commit"
)

// GardenReviewScopeAttempts limits reviews to Garden attempts Bonsai launched on this machine. It is the only
// scope; the field exists so the enum can grow without a format change.
const GardenReviewScopeAttempts = "attempts"

const (
	// DefaultGardenReviewMaxAutoRuns bounds automatic reviewer runs per attempt (cost-storm guard).
	DefaultGardenReviewMaxAutoRuns = 3
	// MaxGardenReviewMaxAutoRuns is the largest value a user may configure.
	MaxGardenReviewMaxAutoRuns = 10
	maxGardenReviewAccountLen  = 128
)

// GardenReviewPrefs is the local policy for reviewer sessions on Garden attempts. nil in Prefs means OFF.
//
// Consent rule: this value, loaded only from the local state file by LoadState, is the only thing that may enable
// automatic reviewer runs. Nothing read from GitHub (issue or pull request text, comments, labels) can set or
// change it, so this type must never be embedded in a Garden or remote-document struct.
//
// The reviewer role is not a field: a reviewer session is always read-only and may write only its one report file.
//
// Empty Trigger means manual; empty Scope means attempts; MaxAutoRuns 0 means 3 (cap 10).
type GardenReviewPrefs struct {
	Enabled bool `json:"enabled"`
	// AccountID is the account that runs reviewer sessions; required when Enabled.
	AccountID   string `json:"account_id,omitempty"`
	Trigger     string `json:"trigger,omitempty"`
	Scope       string `json:"scope,omitempty"`
	MaxAutoRuns int    `json:"max_auto_runs,omitempty"`
	// RequireCI makes accepting while CI has not passed need an extra acknowledgment.
	RequireCI bool `json:"require_ci,omitempty"`
	// BlockOnHigh makes accepting with open high findings need an extra acknowledgment.
	BlockOnHigh bool `json:"block_on_high,omitempty"`
}

// Validate rejects unknown enums, out-of-range counts and an enabled policy without an account.
func (p GardenReviewPrefs) Validate() error {
	switch p.Trigger {
	case "", GardenReviewTriggerManual, GardenReviewTriggerAwaitingReview, GardenReviewTriggerNewCommit:
	default:
		return fmt.Errorf("%w: trigger", ErrInvalidGardenReview)
	}
	if p.Scope != "" && p.Scope != GardenReviewScopeAttempts {
		return fmt.Errorf("%w: scope", ErrInvalidGardenReview)
	}
	if p.MaxAutoRuns < 0 || p.MaxAutoRuns > MaxGardenReviewMaxAutoRuns {
		return fmt.Errorf("%w: max auto runs", ErrInvalidGardenReview)
	}
	if p.AccountID != strings.TrimSpace(p.AccountID) || len(p.AccountID) > maxGardenReviewAccountLen || !utf8.ValidString(p.AccountID) ||
		strings.IndexFunc(p.AccountID, unicode.IsControl) >= 0 {
		return fmt.Errorf("%w: account", ErrInvalidGardenReview)
	}
	if p.Enabled && p.AccountID == "" {
		return fmt.Errorf("%w: account required", ErrInvalidGardenReview)
	}
	return nil
}

// validateIfSet lets nil (off) pass.
func (p *GardenReviewPrefs) validateIfSet() error {
	if p == nil {
		return nil
	}
	return p.Validate()
}

// Effective returns the policy with defaults filled in. A nil receiver is the default: off.
func (p *GardenReviewPrefs) Effective() GardenReviewPrefs {
	if p == nil {
		return GardenReviewPrefs{Trigger: GardenReviewTriggerManual, Scope: GardenReviewScopeAttempts, MaxAutoRuns: DefaultGardenReviewMaxAutoRuns}
	}
	out := *p
	if out.Trigger == "" {
		out.Trigger = GardenReviewTriggerManual
	}
	if out.Scope == "" {
		out.Scope = GardenReviewScopeAttempts
	}
	if out.MaxAutoRuns == 0 {
		out.MaxAutoRuns = DefaultGardenReviewMaxAutoRuns
	}
	return out
}
