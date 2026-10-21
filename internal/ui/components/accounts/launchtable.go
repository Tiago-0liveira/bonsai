package accounts

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/agents/usagefmt"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
	"github.com/Tiago-0liveira/bonsai/internal/ui/usagestore"
)

// fiveHourState is what is known about an account's five-hour window. Cells
// are built from it so "no window", "unknown value" and "still loading" can
// never look alike or read as a zero or full meter.
type fiveHourState struct {
	text     string // set when there is no value to draw: Loading…, Unavailable, Not reported, Unknown
	fraction float64
	hasValue bool
	stale    bool
	reset    string // countdown, "Unknown", or the none marker
	hasReset bool
}

// fiveHourLimit finds the reported five-hour window. Only that window counts:
// a weekly limit or reset grant must never stand in for it.
func fiveHourLimit(u agentsapi.Usage) *agents.UsageLimit {
	for _, l := range []*agents.UsageLimit{u.Headline.Primary, u.Headline.Secondary} {
		if fiveHour(l) {
			return l
		}
	}
	for i := range u.Snapshot.Limits {
		if fiveHour(&u.Snapshot.Limits[i]) {
			return &u.Snapshot.Limits[i]
		}
	}
	return nil
}

func fiveHourOf(e usagestore.Entry, now time.Time, none string) fiveHourState {
	if !e.Have {
		if e.Err != "" && !e.Refreshing {
			return fiveHourState{text: "Unavailable", reset: none}
		}
		return fiveHourState{text: "Loading…", reset: "Loading…"}
	}
	l := fiveHourLimit(e.Usage)
	if l == nil {
		return fiveHourState{text: "Not reported", reset: none, stale: e.Usage.Stale}
	}
	s := fiveHourState{stale: e.Usage.Stale, reset: "Unknown"}
	if f, ok := agents.RemainingFraction(l); ok {
		s.fraction, s.hasValue = f, true
	} else {
		s.text = "Unknown"
	}
	if l.ResetsAt != nil {
		s.reset, s.hasReset = usagefmt.ResetsIn(now, l.ResetsAt, true), true
	}
	return s
}

// LaunchColumns are the 5H LEFT and RESETS IN columns of the launcher table.
// The capacity column narrows by shortening its meter, then dropping it, and
// finally moves under the account name as "5h left 72% · Resets in 2h10m".
func LaunchColumns(usage usagestore.Store, now func() time.Time, ascii bool) []Column {
	th := theme.Current
	dim := lipgloss.NewStyle().Foreground(th.Dim)
	none := "—"
	if ascii {
		none = "-"
	}
	state := func(a agentsapi.Account) fiveHourState { return fiveHourOf(usage[a.ID], now(), none) }

	// capacity renders the value with a meter of the given cells (0 for the
	// number alone). Stale values lose their severity color and say so.
	capacity := func(cells int, label bool) func(agentsapi.Account) string {
		return func(a agentsapi.Account) string {
			s := state(a)
			if !s.hasValue {
				text := s.text
				if label {
					text = "5h left " + text
				}
				if !s.stale {
					return dim.Render(text)
				}
				// A stale reading without a value still says it is stale. The
				// 12-cell form has no room for "Unknown · Stale".
				if cells == 0 && !label && text == "Unknown" {
					text = "Unk"
				}
				return dim.Render(text+" ·") + " " + lipgloss.NewStyle().Foreground(th.Warning).Render("Stale")
			}
			pct := fmt.Sprintf("%d%%", usagefmt.Percent(s.fraction))
			style := rankStyle(usagefmt.RankOf(s.fraction))
			bar := ""
			if cells > 0 {
				bar = stripMeter(s.fraction, cells, ascii) + " "
				if s.stale {
					bar = dim.Render(ansi.Strip(bar))
				}
			}
			out := bar
			if label {
				out = "5h left "
			}
			if s.stale {
				return dim.Render(ansi.Strip(out)+pct) + " " + lipgloss.NewStyle().Foreground(th.Warning).Render("Stale")
			}
			return out + style.Render(pct)
		}
	}
	resetOf := func(prefix string) func(agentsapi.Account) string {
		return func(a agentsapi.Account) string {
			s := state(a)
			if s.hasReset {
				return prefix + s.reset
			}
			if prefix != "" {
				return dim.Render(prefix + s.reset)
			}
			return dim.Render(s.reset)
		}
	}
	return []Column{
		{
			Title: "5H LEFT", Width: barCells + 11, Under: true, UnderCell: capacity(0, true),
			Cell: capacity(barCells, false),
			Narrow: []Variant{
				{Width: 5 + 11, Cell: capacity(5, false)},
				{Width: 12, Cell: capacity(0, false)},
			},
		},
		{Title: "RESETS IN", Width: 9, Under: true, UnderCell: resetOf("Resets in "), Cell: resetOf("")},
	}
}

// UsedMeter is the five-hour capacity an account has used, as a meter of the
// given cells and its percentage. It reports false when no value is known, so
// callers skip the line rather than draw a zero or full meter.
func UsedMeter(usage usagestore.Store, a agentsapi.Account, now time.Time, cells int, ascii bool) (meter string, usedPct int, ok bool) {
	s := fiveHourOf(usage[a.ID], now, "-")
	if !s.hasValue || cells < 1 {
		return "", 0, false
	}
	filled, empty := "▰", "▱"
	if ascii {
		filled, empty = "#", "."
	}
	n := strings.Count(usagefmt.Bar(1-s.fraction, cells), "█")
	fill := rankStyle(usagefmt.RankOf(s.fraction))
	if s.stale {
		fill = lipgloss.NewStyle().Foreground(theme.Current.Dim)
	}
	meter = fill.Render(strings.Repeat(filled, n)) + lipgloss.NewStyle().Foreground(theme.Current.Border).Render(strings.Repeat(empty, cells-n))
	return meter, 100 - usagefmt.Percent(s.fraction), true
}
