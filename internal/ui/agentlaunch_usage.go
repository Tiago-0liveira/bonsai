package ui

import (
	"fmt"
	"strings"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/agents/usagefmt"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/accounts"
	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
	"github.com/charmbracelet/lipgloss"
)

// Match named windows rather than borrowing a weekly value for missing 5H data.
func launchWindows(u agentsapi.Usage) [2]*agents.UsageLimit {
	var out [2]*agents.UsageLimit
	limits := []*agents.UsageLimit{u.Headline.Primary, u.Headline.Secondary}
	for i := range u.Snapshot.Limits {
		limits = append(limits, &u.Snapshot.Limits[i])
	}
	for _, l := range limits {
		if l == nil {
			continue
		}
		i := -1
		switch strings.ToLower(l.Window) {
		case "5h":
			i = 0
		case "weekly", "7d", "7_day", "7day", "week":
			i = 1
		}
		if i < 0 {
			switch l.ID {
			case "five_hour", "5h":
				i = 0
			case "seven_day", "weekly":
				i = 1
			}
		}
		if i >= 0 && out[i] == nil {
			out[i] = l
		}
	}
	return out
}

func (d launchDialog) suggestedAccount() agents.AccountID {
	best := -1.0
	var id agents.AccountID
	for _, a := range d.Ordered() {
		if accounts.NeedsSignIn(a.Info) {
			continue
		}
		entry := d.Usage()[a.ID]
		if !entry.Have || entry.Stale() {
			continue
		}
		score := 1.0
		have := false
		for _, l := range launchWindows(entry.Usage) {
			if f, ok := agents.RemainingFraction(l); ok {
				score = min(score, f)
				have = true
			}
		}
		if have && score > best {
			id, best = a.ID, score
		}
	}
	return id
}

func (d launchDialog) usageColumns() []accounts.Column {
	dim := lipgloss.NewStyle().Foreground(theme.Current.Dim)
	var cols []accounts.Column
	for i, title := range []string{"5H", "WEEK"} {
		cell := func(a agentsapi.Account) string {
			e := d.Usage()[a.ID]
			if !e.Have {
				if e.Err != "" {
					return dim.Render("Unavailable")
				}
				return dim.Render("Loading…")
			}
			l := launchWindows(e.Usage)[i]
			if l == nil {
				return dim.Render("Not reported")
			}
			f, ok := agents.RemainingFraction(l)
			if !ok {
				return dim.Render("Unknown")
			}
			text := fmt.Sprintf("%d%%", usagefmt.Percent(f))
			if e.Stale() {
				text += " Stale"
			}
			return dim.Render(text)
		}
		cols = append(cols, accounts.Column{Title: title, Width: 12, Under: true, Cell: cell, UnderCell: func(a agentsapi.Account) string { return title + " " + cell(a) }})
	}
	reset := func(a agentsapi.Account) string {
		var binding *agents.UsageLimit
		score := 2.0
		label := ""
		for i, l := range launchWindows(d.Usage()[a.ID].Usage) {
			if f, ok := agents.RemainingFraction(l); ok && f < score {
				binding, score = l, f
				label = []string{"5H", "WEEK"}[i]
			}
		}
		if binding == nil || binding.ResetsAt == nil {
			return dim.Render("Unknown")
		}
		return label + " " + usagefmt.ResetsIn(d.now(), binding.ResetsAt, true)
	}
	cols = append(cols, accounts.Column{Title: "RESETS IN", Width: 14, Under: true, Cell: reset, UnderCell: reset})
	cols = append(cols, accounts.Column{Title: "", Width: 9, Optional: true, Cell: func(a agentsapi.Account) string {
		if a.ID == d.suggested {
			return dim.Render("suggested")
		}
		return ""
	}})
	return cols
}
