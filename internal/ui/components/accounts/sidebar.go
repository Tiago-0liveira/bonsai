package accounts

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/agents/usagefmt"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
)

// usageMinWidth is the narrowest sidebar that still has room for a usage
// indicator between the name and the auth dot.
const usageMinWidth = 26

// groupOrder fixes the provider order; providers not listed follow, by ID.
var groupOrder = []agents.ProviderID{"claude", "codex", "antigravity"}

// Sidebar lists accounts grouped by provider. Both the accounts modal and the
// new-agent dialog render it so they show accounts identically.
type Sidebar struct {
	Accounts []agentsapi.Account
	Selected agents.AccountID
	// AddSelected puts the marker on "+ Add account" instead of an account.
	AddSelected bool
	Usage       map[agents.AccountID]agentsapi.Usage
	// Busy accounts show "busy" in place of their usage.
	Busy  map[agents.AccountID]bool
	ASCII bool
	// HideAdd drops the "+ Add account" row, for callers that cannot add.
	HideAdd bool
	Focused bool
	// ShowPlan replaces usage with a plan label in the profiles view.
	ShowPlan bool
}

// Ordered returns the accounts in display order (provider groups, then input
// order within a group), which is the order j/k must walk.
func (s Sidebar) Ordered() []agentsapi.Account {
	var out []agentsapi.Account
	for _, g := range s.groups() {
		out = append(out, g.accounts...)
	}
	return out
}

type group struct {
	provider agents.ProviderID
	accounts []agentsapi.Account
}

func (s Sidebar) groups() []group {
	byProvider := map[agents.ProviderID][]agentsapi.Account{}
	for _, a := range s.Accounts {
		byProvider[a.Provider] = append(byProvider[a.Provider], a)
	}
	var out []group
	for _, p := range groupOrder {
		if accts, ok := byProvider[p]; ok {
			out = append(out, group{p, accts})
			delete(byProvider, p)
		}
	}
	var rest []agents.ProviderID
	for p := range byProvider {
		rest = append(rest, p)
	}
	sort.Slice(rest, func(i, j int) bool { return rest[i] < rest[j] })
	for _, p := range rest {
		out = append(out, group{p, byProvider[p]})
	}
	return out
}

// View renders exactly h lines of w cells with "+ Add account" on the last.
func (s Sidebar) View(w, h int) []string {
	if h <= 0 || w <= 0 {
		return nil
	}
	th := theme.Current
	head := lipgloss.NewStyle().Bold(true).Foreground(th.Text)
	if s.Focused {
		head = head.Reverse(true)
	}
	var body []string
	selected := -1
	for i, g := range s.groups() {
		if i > 0 {
			body = append(body, "")
		}
		body = append(body, theme.ProviderStyle(g.provider).Bold(true).Render(strings.ToUpper(providerName(g.provider))))
		for _, a := range g.accounts {
			if !s.AddSelected && a.ID == s.Selected {
				selected = len(body)
			}
			body = append(body, s.row(a, w, !s.AddSelected && a.ID == s.Selected))
		}
	}

	lines := make([]string, h)
	// Rows between the two-line heading and the pinned add row; scroll so the
	// selected account stays visible when they do not all fit.
	avail := h - 3
	if s.HideAdd {
		avail = h - 2
	}
	start := 0
	if avail > 0 && selected >= avail {
		start = selected - avail + 1
	}
	for i := range lines {
		lines[i] = fit("", w)
	}
	lines[0] = fit(head.Render("ACCOUNTS"), w)
	for i := 0; i < avail && start+i < len(body); i++ {
		lines[2+i] = fit(body[start+i], w)
	}
	if !s.HideAdd {
		lines[h-1] = fit(s.addRow(), w)
	}
	return lines
}

func (s Sidebar) addRow() string {
	th := theme.Current
	if s.AddSelected {
		return lipgloss.NewStyle().Bold(true).Foreground(th.Accent).Render("> + Add account")
	}
	return lipgloss.NewStyle().Foreground(th.Dim).Render("+ Add account")
}

func (s Sidebar) row(a agentsapi.Account, w int, selected bool) string {
	th := theme.Current
	marker := " "
	if selected {
		marker = lipgloss.NewStyle().Bold(true).Foreground(th.Accent).Render(">")
	}
	glyph := theme.ProviderStyle(a.Provider).Render(theme.ProviderGlyph(a.Provider, s.ASCII))

	right := s.dot(a)
	rightW := 1
	if !s.ShowPlan && w >= usageMinWidth {
		if s.Busy[a.ID] {
			right = lipgloss.NewStyle().Foreground(th.Warning).Render(fmt.Sprintf("%4s", "busy")) + " " + right
			rightW += 5
		} else if pct, ok := s.usageText(a.ID); ok {
			right = pct + " " + right
			rightW += 5
		}
	}
	// marker, space, glyph, space precede the name; one gap precedes the right part.
	nameW := max(w-4-rightW-1, 1)
	name := ansi.Truncate(s.profileLabel(a), nameW, "…")
	if selected {
		name = lipgloss.NewStyle().Bold(true).Foreground(th.Text).Render(name)
	}
	left := marker + " " + glyph + " " + name
	pad := max(w-ansi.StringWidth(left)-rightW, 1)
	return left + strings.Repeat(" ", pad) + right
}

func (s Sidebar) profileLabel(a agentsapi.Account) string {
	if !s.ShowPlan {
		return a.Name
	}
	plan := a.Info.Plan
	if u, ok := s.Usage[a.ID]; ok && u.Snapshot.Plan != "" {
		plan = u.Snapshot.Plan
	}
	if a.Provider == "antigravity" {
		plan = strings.TrimSpace(strings.TrimPrefix(plan, "Google AI"))
	}
	if plan == "" {
		return a.Name
	}
	return plan + " · " + a.Name
}

// usageText fits the headline usage in four cells, or hides unknown usage.
func (s Sidebar) usageText(id agents.AccountID) (string, bool) {
	u, ok := s.Usage[id]
	if !ok {
		return "", false
	}
	limit := u.Headline.Primary
	fraction, ok := agents.RemainingFraction(limit)
	if !ok {
		limit = u.Headline.Secondary
		fraction, ok = agents.RemainingFraction(limit)
	}
	if !ok {
		return "", false
	}
	var text string
	if fiveHour(limit) {
		text = meterWidth(fraction, 4, s.ASCII)
	} else {
		text = fmt.Sprintf("%4s", fmt.Sprintf("%d%%", usagefmt.Percent(fraction)))
	}
	if u.Stale {
		return lipgloss.NewStyle().Foreground(theme.Current.Dim).Render(ansi.Strip(text)), true
	}
	return rankStyle(usagefmt.RankOf(fraction)).Render(text), true
}

// rankStyle maps the shared severity bands to palette roles: fair still reads
// as fine, only low and critical draw the eye.
func rankStyle(r usagefmt.Rank) lipgloss.Style {
	th := theme.Current
	switch r {
	case usagefmt.RankGood, usagefmt.RankFair:
		return lipgloss.NewStyle().Foreground(th.Success)
	case usagefmt.RankLow:
		return lipgloss.NewStyle().Foreground(th.Warning)
	default:
		return lipgloss.NewStyle().Foreground(th.Danger)
	}
}

// authState is the sign-in health the sidebar dot and the Overview status show.
type authState int

const (
	authUnknown authState = iota
	authConnected
	authNeedsSignIn
)

// authOf reads the provider-reported "auth_status" option. Anything missing
// or unrecognized stays unknown rather than guessing the account is fine.
func authOf(info agents.AccountInfo) authState {
	status, _ := info.Options["auth_status"].(string)
	switch status {
	case "logged_in", "token":
		return authConnected
	case "logged_out", "missing", "expired":
		return authNeedsSignIn
	}
	return authUnknown
}

// NeedsSignIn reports that the provider says the account has no usable login.
func NeedsSignIn(info agents.AccountInfo) bool { return authOf(info) == authNeedsSignIn }

func (s Sidebar) dot(a agentsapi.Account) string {
	th := theme.Current
	glyph, off := "●", "●"
	if s.ASCII {
		glyph, off = "*", "o"
	}
	switch authOf(a.Info) {
	case authConnected:
		return lipgloss.NewStyle().Foreground(th.Success).Render(glyph)
	case authNeedsSignIn:
		return lipgloss.NewStyle().Foreground(th.Warning).Render(off)
	}
	return lipgloss.NewStyle().Foreground(th.Dim).Render(off)
}

// Selector keeps profile selection visible in a single-column pane.
func (s Sidebar) Selector(focused bool) string {
	label := "+ Add account"
	if !s.AddSelected {
		ordered := s.Ordered()
		for i, a := range ordered {
			if a.ID == s.Selected {
				label = fmt.Sprintf("%s · %s (%d/%d)", theme.ProviderStyle(a.Provider).Render(providerName(a.Provider)), s.profileLabel(a), i+1, len(ordered))
				if s.Busy[a.ID] {
					label += " · busy"
				}
				break
			}
		}
	}
	if focused {
		return lipgloss.NewStyle().Bold(true).Reverse(true).Render("↑↓ " + label)
	}
	return label
}
