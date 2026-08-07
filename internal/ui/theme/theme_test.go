package theme

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestResolvePreset(t *testing.T) {
	p := Resolve("nord", nil)
	if p.Accent != presets["nord"].Accent {
		t.Errorf("nord accent = %v, want %v", p.Accent, presets["nord"].Accent)
	}
}

func TestResolveUnknownFallsBackToBonsai(t *testing.T) {
	p := Resolve("does-not-exist", nil)
	if p.Accent != presets["bonsai"].Accent {
		t.Errorf("unknown preset accent = %v, want bonsai %v", p.Accent, presets["bonsai"].Accent)
	}
}

func TestResolveOverrides(t *testing.T) {
	p := Resolve("bonsai", map[string]string{
		"accent": "#ff0000",
		"danger": "99",
		"":       "ignored",
	})
	if p.Accent != lipgloss.Color("#ff0000") {
		t.Errorf("accent override = %v", p.Accent)
	}
	if p.Danger != lipgloss.Color("99") {
		t.Errorf("danger override = %v", p.Danger)
	}
	// Untouched role keeps the preset value.
	if p.Success != presets["bonsai"].Success {
		t.Errorf("success changed unexpectedly: %v", p.Success)
	}
}
