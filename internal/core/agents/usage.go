package agents

import "time"

type UsageSnapshot struct {
	Provider  ProviderID   `json:"provider"`
	AccountID AccountID    `json:"account_id"`
	FetchedAt time.Time    `json:"fetched_at"`
	Limits    []UsageLimit `json:"limits"`
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
	Error   error
}
