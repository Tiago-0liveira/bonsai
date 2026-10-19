package theme

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

func TestProviderGlyphs(t *testing.T) {
	tests := []struct {
		provider     agents.ProviderID
		unicode, asc string
	}{
		{"claude", "✹", "C"},
		{"codex", "❃", "X"},
		{"antigravity", "✦", "A"},
		{"other", "•", "?"},
	}
	for _, tt := range tests {
		t.Run(string(tt.provider), func(t *testing.T) {
			if got := ProviderGlyph(tt.provider, false); got != tt.unicode {
				t.Errorf("unicode glyph = %q, want %q", got, tt.unicode)
			}
			if got := ProviderGlyph(tt.provider, true); got != tt.asc {
				t.Errorf("ascii glyph = %q, want %q", got, tt.asc)
			}
			if w := ansi.StringWidth(tt.unicode); w != 1 {
				t.Errorf("unicode glyph width = %d, want 1", w)
			}
		})
	}
}

func TestAgentBadge(t *testing.T) {
	tests := []struct {
		name     string
		provider agents.ProviderID
		count    int
		ascii    bool
		want     string
	}{
		{"single", "claude", 1, false, "✹"},
		{"zero", "codex", 0, false, "❃"},
		{"count", "claude", 2, false, "✹2"},
		{"ascii count", "antigravity", 3, true, "A3"},
		{"ascii single", "codex", 1, true, "X"},
		{"unknown count", "other", 12, false, "•12"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := AgentBadge(tt.provider, tt.count, tt.ascii); got != tt.want {
				t.Errorf("AgentBadge = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestProviderIdentityColors(t *testing.T) {
	old := Current
	t.Cleanup(func() { Current = old })
	Current = Resolve("nord", nil)
	tests := []struct {
		provider agents.ProviderID
		want     interface{}
	}{
		{"claude", lipgloss.Color("#D97757")},
		{"codex", lipgloss.Color("#D1D5DB")},
		{"antigravity", lipgloss.Color("#4285F4")},
		{"other", Current.Dim},
	}
	for _, tt := range tests {
		if got := ProviderStyle(tt.provider).GetForeground(); got != tt.want {
			t.Errorf("%s foreground = %v, want %v", tt.provider, got, tt.want)
		}
	}
}
