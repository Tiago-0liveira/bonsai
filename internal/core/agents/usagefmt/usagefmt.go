// Package usagefmt formats agent usage as plain text. The CLI and the TUI
// both show usage and must agree on every number; colors stay with each caller.
package usagefmt

import (
	"fmt"
	"strings"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

// Bar draws remaining usage as filled and empty cells; callers color it.
func Bar(fraction float64, width int) string {
	if fraction < 0 {
		fraction = 0
	}
	if fraction > 1 {
		fraction = 1
	}
	filled := int(fraction*float64(width) + 0.5)
	if filled > width {
		filled = width
	}
	return strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
}

// Percent rounds to the nearest whole percent, so the CLI and TUI never differ by one.
func Percent(fraction float64) int {
	return int(fraction*100 + 0.5)
}

// Duration formats a countdown. precise keeps minutes up to 72h for headers;
// otherwise it rounds to hours or days to fit table cells.
func Duration(d time.Duration, precise bool) string {
	if d <= 0 {
		return "now"
	}
	minutes := int(d.Round(time.Minute) / time.Minute)
	if minutes < 1 {
		return "<1m"
	}
	hours := minutes / 60
	mins := minutes % 60
	if precise && hours < 72 {
		if hours == 0 {
			return fmt.Sprintf("%dm", mins)
		}
		return fmt.Sprintf("%dh%02dm", hours, mins)
	}
	if hours < 24 {
		if mins >= 30 {
			hours++
		}
		return fmt.Sprintf("%dh", hours)
	}
	days := hours / 24
	if hours%24 >= 12 {
		days++
	}
	return fmt.Sprintf("%dd", days)
}

// ResetsIn formats an optional reset time; now is passed in so tests and
// rendering share one clock.
func ResetsIn(now time.Time, t *time.Time, precise bool) string {
	if t == nil {
		return "-"
	}
	if !t.After(now) {
		return "now"
	}
	return Duration(t.Sub(now), precise)
}

// Rank identifies the caller-neutral severity band for remaining usage.
type Rank int

const (
	// RankGood marks at least three quarters of usage remaining.
	RankGood Rank = iota
	// RankFair marks at least half of usage remaining.
	RankFair
	// RankLow marks at least one quarter of usage remaining.
	RankLow
	// RankCritical marks less than one quarter of usage remaining.
	RankCritical
)

// RankOf applies the shared CLI severity thresholds to a remaining fraction.
func RankOf(fraction float64) Rank {
	switch {
	case fraction >= 0.75:
		return RankGood
	case fraction >= 0.50:
		return RankFair
	case fraction >= 0.25:
		return RankLow
	default:
		return RankCritical
	}
}

// WindowName labels a limit for headers. Providers report the weekly window
// as "weekly", which reads poorly next to "5h", so it becomes "Week".
func WindowName(limit *agents.UsageLimit, fallback string) string {
	if limit == nil {
		return fallback
	}
	if limit.Window == "weekly" {
		return "Week"
	}
	if limit.Window != "" {
		return limit.Window
	}
	if limit.Label != "" {
		return limit.Label
	}
	return fallback
}

// Headline is the full TUI header line. Unknown limits are skipped rather
// than shown as zero, so a provider without a weekly window still reads right.
func Headline(h agents.UsageHeadline, now time.Time) string {
	parts := headlineParts(h, true, false)
	if h.NextReset != nil && h.NextReset.After(now) {
		parts = append(parts, "Reset "+Duration(h.NextReset.Sub(now), true))
	}
	return strings.Join(parts, " · ")
}

// Compact is Headline for narrow panes: it drops the reset and "left" so
// both windows still fit.
func Compact(h agents.UsageHeadline) string {
	return strings.Join(headlineParts(h, false, true), " · ")
}

func headlineParts(h agents.UsageHeadline, includeLeft, shortenWeek bool) []string {
	limits := []struct {
		limit    *agents.UsageLimit
		fallback string
	}{
		{limit: h.Primary, fallback: "Primary"},
		{limit: h.Secondary, fallback: "Secondary"},
	}
	parts := make([]string, 0, len(limits))
	for _, item := range limits {
		fraction, ok := agents.RemainingFraction(item.limit)
		if !ok {
			continue
		}
		name := WindowName(item.limit, item.fallback)
		if shortenWeek && name == "Week" {
			name = "W"
		}
		part := fmt.Sprintf("%s %d%%", name, Percent(fraction))
		if includeLeft {
			part += " left"
		}
		parts = append(parts, part)
	}
	return parts
}
