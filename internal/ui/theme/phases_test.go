package theme

import (
	"github.com/charmbracelet/lipgloss"
	"testing"
)

func phaseRoles(p Palette) []lipgloss.Color {
	return []lipgloss.Color{p.PhaseBacklog, p.PhasePlanning, p.PhaseReady, p.PhaseBuilding, p.PhaseReview, p.PhaseDone}
}
func TestPhaseRoles(t *testing.T) {
	for _, name := range Presets() {
		t.Run(name, func(t *testing.T) {
			for i, c := range phaseRoles(presets[name]) {
				if c == "" {
					t.Errorf("empty preset role %d", i)
				}
			}
			overrides := map[string]string{}
			for _, role := range []string{"backlog", "planning", "ready", "building", "review", "done"} {
				overrides["phase_"+role] = "#123456"
			}
			for _, c := range phaseRoles(Resolve(name, overrides)) {
				if c != "#123456" {
					t.Errorf("override ignored: %s", c)
				}
			}
		})
	}
	// Partial custom palettes resolve through the same path as presets.
	const partial = "test-partial"
	presets[partial] = Palette{Muted: "1", Accent: "2", Success: "3", PRBadge: "4", Dim: "5"}
	defer delete(presets, partial)
	for i, c := range phaseRoles(Resolve(partial, nil)) {
		if c == "" {
			t.Errorf("empty fallback role %d", i)
		}
	}
	p := Resolve("mono", nil)
	seen := map[lipgloss.Color]bool{}
	for _, c := range phaseRoles(p) {
		if seen[c] {
			t.Fatal("mono phases repeat a gray")
		}
		seen[c] = true
	}
}
