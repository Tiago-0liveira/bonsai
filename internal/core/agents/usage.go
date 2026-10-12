package agents

import "time"

type UsageSnapshot struct {
	Provider  ProviderID `json:"provider"`
	AccountID AccountID  `json:"account_id"`
	FetchedAt time.Time  `json:"fetched_at"`
	// Plan is the subscription plan the provider reports (for example "plus"),
	// when it reports one.
	Plan     string       `json:"plan,omitempty"`
	Limits   []UsageLimit `json:"limits"`
	Warnings []string     `json:"warnings,omitempty"`
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
