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

func TestPresetsDefineEveryRole(t *testing.T) {
	for _, name := range Presets() {
		p := presets[name]
		roles := map[string]lipgloss.Color{
			"accent": p.Accent, "border_focus": p.BorderFocus, "border": p.Border, "dim": p.Dim,
			"text": p.Text, "success": p.Success, "danger": p.Danger, "warning": p.Warning,
			"pr_badge": p.PRBadge, "background": p.Background, "surface": p.Surface,
			"raised": p.Raised, "muted": p.Muted,
		}
		for role, c := range roles {
			if c == "" {
				t.Errorf("preset %s: role %s is empty", name, role)
			}
		}
		if got := Resolve(name, nil); got != p {
			t.Errorf("Resolve(%s) differs from the preset", name)
		}
	}
}

func TestSurfaceRoleOverrides(t *testing.T) {
	p := Resolve("nord", map[string]string{
		"background": "#010203", "surface": "11", "raised": "12", "muted": "13",
	})
	got := []lipgloss.Color{p.Background, p.Surface, p.Raised, p.Muted}
	want := []lipgloss.Color{"#010203", "11", "12", "13"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("override %d = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestApplyContrastAllPresets(t *testing.T) {
	for _, name := range Presets() {
		base := Resolve(name, nil)
		for _, mode := range []string{"", "layered", "flat", "bogus"} {
			p := ApplyContrast(base, mode)
			for role, c := range map[string]lipgloss.Color{
				"accent": p.Accent, "text": p.Text, "background": p.Background,
				"surface": p.Surface, "raised": p.Raised, "muted": p.Muted,
			} {
				if c == "" {
					t.Errorf("%s/%q: role %s is empty", name, mode, role)
				}
			}
			if p.Text != base.Text || p.Accent != base.Accent || p.Muted != base.Muted || p.Background != base.Background {
				t.Errorf("%s/%q: text roles or background changed", name, mode)
			}
			flat := mode == "flat"
			if flat != (p.Surface == p.Background && p.Raised == p.Background) {
				t.Errorf("%s/%q: flat collapse = %v", name, mode, !flat)
			}
			if !flat && (p.Surface != base.Surface || p.Raised != base.Raised) {
				t.Errorf("%s/%q: layered shades changed", name, mode)
			}
		}
	}
}
