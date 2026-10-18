package accounts

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/agents/usagefmt"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
)

const barCells = 10

func (m Model) usageKey(k tea.KeyMsg) (Model, tea.Cmd) {
	switch k.String() {
	case "r", "enter":
		if m.selected == "" || m.usageLoading {
			return m, nil
		}
		m.usageLoading = true
		return m, usageCmd(m.api, m.selected, m.next(&m.latest.usage), true)
	}
	return m, nil
}

func (m Model) usageView(rw, rows int) []string {
	th := theme.Current
	dim := lipgloss.NewStyle().Foreground(th.Dim)
	refresh := dim.Render("[r] Refresh usage")

	u, have := m.usage[m.selected]
	if !have {
		body := []string{dim.Render("Loading usage…")}
		if !m.usageLoading {
			body = []string{dim.Render("Usage unavailable")}
			if m.usageErr != "" {
				body = append(body, dim.Render(m.usageErr))
			}
		}
		return pinned(body, []string{refresh}, rows)
	}

	body := windowLines(u, m.now(), m.ascii)
	body = append(body, additionalLines(u, m.ascii)...)
	if len(body) == 0 {
		body = []string{dim.Render("No usage reported.")}
	}
	if u.Stale {
		for i, l := range body {
			body[i] = dim.Render(ansi.Strip(l))
		}
	}

	var tail []string
	if u.Stale {
		note := "Showing last good data"
		if u.Err != "" {
			note += " · " + u.Err
		}
		tail = append(tail, dim.Render(note))
	}
	updated := ""
	switch {
	case m.usageLoading:
		updated = "Refreshing…"
	case !u.FetchedAt.IsZero():
		updated = updatedText(m.now(), u.FetchedAt)
	}
	if updated != "" {
		tail = append(tail, dim.Render(updated))
	}
	tail = append(tail, "", refresh)
	return pinned(body, tail, rows)
}

// pinned keeps tail on the last rows of the column and fits body above it,
// dropping blank lines first when space is short.
func pinned(body, tail []string, rows int) []string {
	body = squeeze(body, rows-len(tail))
	out := append([]string{}, body...)
	for len(out) < rows-len(tail) {
		out = append(out, "")
	}
	return append(out, tail...)
}

// windowLines draws the two headline windows: name, meter, reset, blank.
func windowLines(u agentsapi.Usage, now time.Time, ascii bool) []string {
	var out []string
	for _, w := range []struct {
		limit    *agents.UsageLimit
		fallback string
	}{{u.Headline.Primary, "Primary"}, {u.Headline.Secondary, "Secondary"}} {
		if w.limit == nil {
			continue
		}
		out = append(out, windowTitle(w.limit, w.fallback))
		_, ok := agents.RemainingFraction(w.limit)
		if !ok {
			out = append(out, lipgloss.NewStyle().Foreground(theme.Current.Dim).Render("Unknown"), "")
			continue
		}
		out = append(out, limitValue(w.limit, ascii, true))
		if w.limit.ResetsAt != nil {
			out = append(out, resetsText(now, w.limit.ResetsAt))
		}
		out = append(out, "")
	}
	return out
}

func windowTitle(limit *agents.UsageLimit, fallback string) string {
	switch name := usagefmt.WindowName(limit, fallback); name {
	case "5h":
		return "5-hour window"
	case "Week":
		return "Weekly window"
	default:
		return name
	}
}

func resetsText(now time.Time, t *time.Time) string {
	if r := usagefmt.ResetsIn(now, t, true); r != "now" {
		return "Resets in " + r
	}
	return "Resets now"
}

// meter is the bar colored by how much is left.
func meter(fraction float64, ascii bool) string {
	return meterWidth(fraction, barCells, ascii)
}

func meterWidth(fraction float64, width int, ascii bool) string {
	bar := usagefmt.Bar(fraction, width)
	if ascii {
		bar = strings.NewReplacer("█", "#", "░", ".").Replace(bar)
	}
	return rankStyle(usagefmt.RankOf(fraction)).Render(bar)
}

func limitLabel(l agents.UsageLimit) string {
	if l.Label != "" {
		return l.Label
	}
	return l.ID
}

// additionalLines lists limits beyond the headline windows plus credit balances.
func additionalLines(u agentsapi.Usage, ascii bool) []string {
	type row struct{ label, value string }
	var rows []row
	extra := u.Headline.Extra
	if u.Headline.Primary == nil && u.Headline.Secondary == nil && len(extra) == 0 {
		// Keep unfamiliar limits visible in the same details layout without
		// reviving a separate pool UI or inventing shared windows.
		extra = u.Snapshot.Limits
	}
	for _, l := range extra {
		value := "unknown"
		if _, ok := agents.RemainingFraction(&l); ok {
			value = limitValue(&l, ascii, true)
		}
		label := limitLabel(l)
		if l.Group != "" && l.Window != "" {
			label = l.Group + " · " + usagefmt.WindowName(&l, "")
		}
		rows = append(rows, row{label, value})
	}
	for _, c := range u.Snapshot.Credits {
		rows = append(rows, row{c.Label, creditText(c)})
	}
	if len(rows) == 0 {
		return nil
	}
	labelW := 0
	for _, r := range rows {
		labelW = max(labelW, lipgloss.Width(r.label))
	}
	out := []string{"Additional limits"}
	for _, r := range rows {
		out = append(out, r.label+strings.Repeat(" ", labelW-lipgloss.Width(r.label)+2)+r.value)
	}
	return append(out, "")
}

func creditText(c agents.UsageCredit) string {
	switch {
	case !c.Enabled:
		return "Off"
	case c.Unlimited:
		return "Unlimited"
	}
	var amount float64
	switch {
	case c.Balance != nil:
		amount = *c.Balance
	case c.Limit != nil && c.Used != nil:
		amount = *c.Limit - *c.Used
	default:
		return "-"
	}
	if c.Unit == agents.CreditUnitUSD {
		return fmt.Sprintf("$%.2f", amount)
	}
	return fmt.Sprintf("%.0f %s", amount, c.Unit)
}

// updatedText is "Updated N seconds ago" in the largest whole unit.
func updatedText(now, t time.Time) string {
	d := max(now.Sub(t), 0)
	switch {
	case d < time.Second:
		return "Updated just now"
	case d < time.Minute:
		return "Updated " + plural(int(d/time.Second), "second") + " ago"
	case d < time.Hour:
		return "Updated " + plural(int(d/time.Minute), "minute") + " ago"
	case d < 24*time.Hour:
		return "Updated " + plural(int(d/time.Hour), "hour") + " ago"
	}
	return "Updated " + plural(int(d/(24*time.Hour)), "day") + " ago"
}
