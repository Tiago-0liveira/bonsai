package accounts

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/agents/usagefmt"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
	"github.com/Tiago-0liveira/bonsai/internal/ui/usagestore"
)

// groupOrder fixes the provider order; providers not listed follow, by ID.
var groupOrder = []agents.ProviderID{"claude", "codex", "antigravity"}

// Sidebar lists accounts grouped by provider. Both the accounts modal and the
// new-agent dialog render it so they show accounts identically.
type Sidebar struct {
	Accounts []agentsapi.Account
	Selected agents.AccountID
	Usage    usagestore.Store
	// Busy accounts show "busy" in place of their usage.
	Busy  map[agents.AccountID]bool
	ASCII bool
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

func (s Sidebar) profileLabel(a agentsapi.Account) string {
	if !s.ShowPlan {
		return a.Name
	}
	plan := planOf(a, s.Usage)
	if plan == "" {
		return a.Name
	}
	return plan + " · " + a.Name
}

// planOf is the short plan label: the usage snapshot's when known, else the
// account's, without Antigravity's "Google AI" prefix.
func planOf(a agentsapi.Account, usage usagestore.Store) string {
	plan := a.Info.Plan
	if e := usage[a.ID]; e.Have && e.Usage.Snapshot.Plan != "" {
		plan = e.Usage.Snapshot.Plan
	}
	if a.Provider == "antigravity" {
		plan = strings.TrimSpace(strings.TrimPrefix(plan, "Google AI"))
	}
	return plan
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
	ordered := s.Ordered()
	for i, a := range ordered {
		if a.ID == s.Selected {
			label = fmt.Sprintf("%s · %s (%d/%d)", providerMark(a.Provider, s.ASCII), s.profileLabel(a), i+1, len(ordered))
			if s.Busy[a.ID] {
				label += " · busy"
			}
			break
		}
	}
	if focused {
		return lipgloss.NewStyle().Bold(true).Reverse(true).Render("↑↓ " + label)
	}
	return label
}
