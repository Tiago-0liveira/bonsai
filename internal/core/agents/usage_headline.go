package agents

import "time"

// UsageLowThreshold is the headroom below which a profile counts as low.
const UsageLowThreshold = 0.20

// UsageHeadline is a provider's reading of a usage snapshot: which limits are
// the headline pair, the other limits worth showing, and the profile's score.
// The CLI dashboard and the local API both use it, so they rank profiles alike.
type UsageHeadline struct {
	Primary   *UsageLimit  `json:"primary,omitempty"`   // e.g. 5h
	Secondary *UsageLimit  `json:"secondary,omitempty"` // e.g. weekly
	Extra     []UsageLimit `json:"extra,omitempty"`     // per-model, other features, in provider order
	// Score is the lowest remaining fraction of Primary and Secondary.
	Score *float64 `json:"score,omitempty"`
	// Low means Score is below UsageLowThreshold.
	Low bool `json:"low,omitempty"`
	// NextReset is the reset of Secondary, or of Primary when Secondary has none.
	NextReset *time.Time `json:"next_reset,omitempty"`
}

// NewUsageHeadline fills the score, low flag and next reset from the headline
// pair. Limits are copied, so the headline never aliases a snapshot.
func NewUsageHeadline(primary, secondary *UsageLimit, extra []UsageLimit) UsageHeadline {
	h := UsageHeadline{Primary: copyLimit(primary), Secondary: copyLimit(secondary)}
	if len(extra) > 0 {
		h.Extra = append([]UsageLimit(nil), extra...)
	}
	found := false
	var score float64
	for _, limit := range []*UsageLimit{h.Primary, h.Secondary} {
		remaining, ok := RemainingFraction(limit)
		if ok && (!found || remaining < score) {
			score, found = remaining, true
		}
	}
	if found {
		h.Score = &score
		h.Low = score < UsageLowThreshold
	}
	for _, limit := range []*UsageLimit{h.Secondary, h.Primary} {
		if limit != nil && limit.ResetsAt != nil {
			reset := *limit.ResetsAt
			h.NextReset = &reset
			break
		}
	}
	return h
}

// RemainingFraction returns a limit's remaining fraction clamped to [0, 1].
func RemainingFraction(limit *UsageLimit) (float64, bool) {
	if limit == nil || limit.RemainingFraction == nil {
		return 0, false
	}
	return min(max(*limit.RemainingFraction, 0), 1), true
}

func copyLimit(limit *UsageLimit) *UsageLimit {
	if limit == nil {
		return nil
	}
	c := *limit
	return &c
}

// UsageHeadline returns the headline of a snapshot from its provider's
// UsageLayout, or nil when the provider has none or is not registered.
func (r *Registry) UsageHeadline(snapshot UsageSnapshot) *UsageHeadline {
	p, err := r.Get(snapshot.Provider)
	if err != nil {
		return nil
	}
	layout, ok := p.(UsageLayout)
	if !ok {
		return nil
	}
	h := layout.UsageHeadline(snapshot)
	return &h
}
