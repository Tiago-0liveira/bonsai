package antigravity

import (
	"strings"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

// UsageHeadline puts the Gemini 5h and weekly pools in the headline. The
// Claude & GPT pools follow as extras, 5h first, with Window set to "5h" or
// "weekly" as classified; other limits are left out. The first limit of each
// pool wins.
func (p *Provider) UsageHeadline(snapshot agents.UsageSnapshot) agents.UsageHeadline {
	var gemini5h, geminiWeek, third5h, thirdWeek *agents.UsageLimit
	first := func(slot **agents.UsageLimit, limit *agents.UsageLimit) {
		if *slot == nil {
			*slot = limit
		}
	}
	for i := range snapshot.Limits {
		limit := &snapshot.Limits[i]
		switch classifyUsageLimit(*limit) {
		case "gemini-5h":
			first(&gemini5h, limit)
		case "gemini-weekly":
			first(&geminiWeek, limit)
		case "third-5h":
			first(&third5h, limit)
		case "third-weekly":
			first(&thirdWeek, limit)
		}
	}
	var extra []agents.UsageLimit
	for _, pool := range []struct {
		limit  *agents.UsageLimit
		window string
	}{{third5h, "5h"}, {thirdWeek, "weekly"}} {
		if pool.limit != nil {
			limit := *pool.limit
			limit.Window = pool.window
			extra = append(extra, limit)
		}
	}
	// Copy the headline limits: API labels such as "5h session limit" must
	// use the same canonical windows as the other providers.
	normalize := func(limit *agents.UsageLimit, window string) *agents.UsageLimit {
		if limit == nil {
			return nil
		}
		copy := *limit
		copy.Window = window
		return &copy
	}
	return agents.NewUsageHeadline(normalize(gemini5h, "5h"), normalize(geminiWeek, "weekly"), extra)
}

// classifyUsageLimit names the pool of a limit from its id, group, label and
// window: "gemini-5h", "gemini-weekly", "third-5h", "third-weekly", or "".
func classifyUsageLimit(limit agents.UsageLimit) string {
	// Cloud Code and agy spell the same windows differently. Normalize their
	// separators before classification so refreshes keep the same headline.
	text := strings.ToLower(strings.Join([]string{limit.ID, limit.Group, limit.Label, limit.Window}, " "))
	text = strings.NewReplacer("_", " ", "-", " ").Replace(text)
	weekly := strings.Contains(text, "week") || strings.Contains(text, "7 day") || strings.Contains(text, "7d")
	fiveHour := strings.Contains(text, "5h") || strings.Contains(text, "five hour") || strings.Contains(text, "5 hour")
	if !weekly && !fiveHour {
		return ""
	}

	switch {
	case strings.Contains(text, "gemini"):
		if weekly {
			return "gemini-weekly"
		}
		return "gemini-5h"
	case strings.Contains(text, "claude"), strings.Contains(text, "gpt"), strings.Contains(text, "3p"), strings.Contains(text, "third"):
		if weekly {
			return "third-weekly"
		}
		return "third-5h"
	default:
		return ""
	}
}
