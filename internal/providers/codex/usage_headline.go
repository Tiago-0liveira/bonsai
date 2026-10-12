package codex

import "github.com/Tiago-0liveira/bonsai/internal/core/agents"

// UsageHeadline puts the main 5-hour and weekly windows in the headline; every
// other window and the limits of other metered features follow as extras, in
// snapshot order.
func (p *Provider) UsageHeadline(snapshot agents.UsageSnapshot) agents.UsageHeadline {
	var five, week *agents.UsageLimit
	var extra []agents.UsageLimit
	for i := range snapshot.Limits {
		switch limit := &snapshot.Limits[i]; limit.ID {
		case "five_hour":
			five = limit
		case "weekly":
			week = limit
		default:
			extra = append(extra, *limit)
		}
	}
	return agents.NewUsageHeadline(five, week, extra)
}
