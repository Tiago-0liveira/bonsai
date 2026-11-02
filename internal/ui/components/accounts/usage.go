package accounts

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/agents/usagefmt"
)

const barCells = 10

func (m Model) usageKey(k tea.KeyMsg) (Model, tea.Cmd) {
	switch k.String() {
	case "r", "enter":
		var cmd tea.Cmd
		m.Browser, cmd = m.Browser.RefreshUsage()
		return m, cmd
	case "down", "j":
		m.usageOff = min(m.usageOff+1, m.usageScrollMax())
	case "up", "k":
		m.usageOff = max(min(m.usageOff, m.usageScrollMax())-1, 0)
	}
	return m, nil
}

// usageScrollMax is how far the Usage section can scroll in the current size,
// 0 when everything fits.
func (m Model) usageScrollMax() int {
	if _, ok := m.Current(); !ok || m.tab != TabUsage {
		return 0
	}
	w, h := FrameSize(m.w, m.h)
	iw := ContentWidth(w)
	_, _, gap, free := m.frame(iw, h)
	_, insp, avail := m.inspectLayout(iw, free, gap)
	return insp.scrollMax(max(avail-len(insp.tail), 0))
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
