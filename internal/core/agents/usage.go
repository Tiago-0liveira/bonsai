package agents

import "time"

type UsageSnapshot struct {
	Provider  ProviderID `json:"provider"`
	AccountID AccountID  `json:"account_id"`
	FetchedAt time.Time  `json:"fetched_at"`
	// Plan is the subscription plan the provider reports (for example "plus"),
	// when it reports one.
	Plan   string       `json:"plan,omitempty"`
	Limits []UsageLimit `json:"limits"`
	// Credits are money or credit balances beside the plan limits, such as
	// Claude's usage credits or a Codex credit balance.
	Credits []UsageCredit `json:"credits,omitempty"`
	// ResetCredits are one-off rate-limit resets the account was granted
	// (Codex). Bonsai only shows them; it never redeems one.
	ResetCredits []ResetCredit `json:"reset_credits,omitempty"`
	Warnings     []string      `json:"warnings,omitempty"`
}

// Credit units.
const (
	CreditUnitUSD     = "USD"
	CreditUnitCredits = "credits"
)

// UsageCredit is one balance of money or credits. Amounts are in major units
// (dollars, not cents); nil means the provider did not say.
type UsageCredit struct {
	ID        string     `json:"id"`
	Label     string     `json:"label"`
	Unit      string     `json:"unit"`
	Used      *float64   `json:"used,omitempty"`
	Limit     *float64   `json:"limit,omitempty"`
	Balance   *float64   `json:"balance,omitempty"`
	Unlimited bool       `json:"unlimited,omitempty"`
	Enabled   bool       `json:"enabled"`
	ResetsAt  *time.Time `json:"resets_at,omitempty"`
}

// ResetCredit is one granted rate-limit reset, or Count of them from one
// grant. The provider's own credit ID is never copied.
type ResetCredit struct {
	Title  string `json:"title"`
	Status string `json:"status,omitempty"` // "available" when it can be used now
	// Count is how many resets are left in the grant; 0 means one.
	Count int `json:"count,omitempty"`
	// Clears names the limits a reset refills, such as "five_hour".
	Clears    []string   `json:"clears,omitempty"`
	GrantedAt *time.Time `json:"granted_at,omitempty"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

type UsageLimit struct {
	ID                string     `json:"id"`
	Group             string     `json:"group,omitempty"`
	Label             string     `json:"label,omitempty"`
	Window            string     `json:"window,omitempty"`
	RemainingFraction *float64   `json:"remaining_fraction,omitempty"`
	ResetsAt          *time.Time `json:"resets_at,omitempty"`
}

type UsageOptions struct {
	Refresh bool
}

type AccountUsageResult struct {
	Account Account
	Usage   *UsageSnapshot
	// Headline is the provider's UsageLayout reading of Usage, when it has one.
	Headline *UsageHeadline
	Error    error
}
