package theme

import (
	"strconv"

	"github.com/charmbracelet/lipgloss"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

// Provider IDs are spelled out here because the TUI must not import the
// provider packages that define them.
const (
	providerClaude      agents.ProviderID = "claude"
	providerCodex       agents.ProviderID = "codex"
	providerAntigravity agents.ProviderID = "antigravity"
)

// ProviderGlyph is the one-cell mark for an agent provider. ascii swaps in a
// letter for fonts or terminals that draw the unicode glyphs badly.
func ProviderGlyph(p agents.ProviderID, ascii bool) string {
	var u, a string
	switch p {
	case providerClaude:
		u, a = "✳", "C"
	case providerCodex:
		u, a = "◎", "X"
	case providerAntigravity:
		u, a = "✦", "A"
	default:
		u, a = "•", "?"
	}
	if ascii {
		return a
	}
	return u
}

// ProviderStyle keeps provider identity consistent across themes and views.
func ProviderStyle(p agents.ProviderID) lipgloss.Style {
	c := Current.Dim
	switch p {
	case providerClaude:
		c = lipgloss.Color("#D97757")
	case providerCodex:
		c = lipgloss.Color("#D1D5DB")
	case providerAntigravity:
		c = lipgloss.Color("#4285F4")
	}
	return lipgloss.NewStyle().Foreground(c)
}

// AgentBadge is a provider glyph plus a count when more than one agent of
// that provider runs. The count is a plain digit so the width stays
// predictable in tight rows.
func AgentBadge(p agents.ProviderID, count int, ascii bool) string {
	g := ProviderGlyph(p, ascii)
	if count > 1 {
		g += strconv.Itoa(count)
	}
	return g
}
