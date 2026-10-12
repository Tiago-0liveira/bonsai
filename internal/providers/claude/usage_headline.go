package claude

import "github.com/Tiago-0liveira/bonsai/internal/core/agents"

// usageNote is shown next to Claude usage wherever it is displayed.
const usageNote = "Read from an undocumented endpoint; it may change. Bonsai never refreshes logins."

// UsageNote tells readers where Claude usage comes from.
func (p *Provider) UsageNote() string { return usageNote }

// UsageHeadline puts the session (5h) and weekly limits in the headline; the
// per-model weekly limits and any other limit follow as extras, in snapshot
// order.
func (p *Provider) UsageHeadline(snapshot agents.UsageSnapshot) agents.UsageHeadline {
	var five, week *agents.UsageLimit
	var extra []agents.UsageLimit
	for i := range snapshot.Limits {
		switch limit := &snapshot.Limits[i]; limit.ID {
		case "five_hour":
			five = limit
		case "seven_day":
			week = limit
		default:
			extra = append(extra, *limit)
		}
	}
	return agents.NewUsageHeadline(five, week, extra)
}
