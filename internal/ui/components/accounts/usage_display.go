package accounts

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/agents/usagefmt"
	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
)

// Headline keeps usage consistent across session headers, launch and usage views.
// Only five-hour windows use bars; other windows show colored percentages.
func Headline(h agents.UsageHeadline, now time.Time, ascii, compact bool) string {
	var parts []string
	for _, item := range []struct {
		limit    *agents.UsageLimit
		fallback string
	}{{h.Primary, "Primary"}, {h.Secondary, "Secondary"}} {
		if _, ok := agents.RemainingFraction(item.limit); !ok {
			continue
		}
		name := usagefmt.WindowName(item.limit, item.fallback)
		if compact && name == "Week" {
			name = "W"
		}
		part := name + " " + limitValue(item.limit, ascii, false)
		if !compact {
			part += " left"
		}
		parts = append(parts, part)
	}
	if !compact && h.NextReset != nil && h.NextReset.After(now) {
		parts = append(parts, "Reset "+usagefmt.Duration(h.NextReset.Sub(now), true))
	}
	return strings.Join(parts, " · ")
}

// RemainingCapacity shares the Usage screen's segmented meter and percentage
// with other account-capacity views. Zero cells keeps only the percentage.
func RemainingCapacity(fraction float64, cells int, ascii bool) string {
	value := ""
	if cells > 0 {
		value = stripMeter(fraction, cells, ascii) + " "
	}
	return value + rankStyle(usagefmt.RankOf(fraction)).Bold(true).Render(fmt.Sprintf("%3d%% left", usagefmt.Percent(fraction)))
}

func fiveHour(l *agents.UsageLimit) bool {
	return l != nil && (l.Window == "5h" || l.ID == "five_hour" || l.ID == "5h")
}

func limitValue(l *agents.UsageLimit, ascii, verbose bool) string {
	fraction, ok := agents.RemainingFraction(l)
	if !ok {
		return lipgloss.NewStyle().Foreground(theme.Current.Dim).Render("Unknown")
	}
	if fiveHour(l) {
		return meter(fraction, ascii)
	}
	text := fmt.Sprintf("%d%%", usagefmt.Percent(fraction))
	if verbose {
		text += " remaining"
	}
	return rankStyle(usagefmt.RankOf(fraction)).Render(text)
}
