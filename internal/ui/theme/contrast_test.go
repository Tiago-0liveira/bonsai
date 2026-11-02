package theme

import (
	"image/color"
	"math"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func luminance(c color.Color) float64 {
	r, g, b, _ := c.RGBA()
	linear := func(v uint32) float64 {
		x := float64(v) / 65535
		if x <= 0.04045 {
			return x / 12.92
		}
		return math.Pow((x+0.055)/1.055, 2.4)
	}
	return .2126*linear(r) + .7152*linear(g) + .0722*linear(b)
}

func TestPresetMutedContrast(t *testing.T) {
	for _, name := range Presets() {
		t.Run(name, func(t *testing.T) {
			p := Resolve(name, nil)
			for role, c := range map[string]lipgloss.Color{"dim": p.Dim, "muted": p.Muted} {
				for _, bg := range []lipgloss.Color{p.Background, p.Surface, "#000000"} {
					a, b := luminance(ANSIColor(c)), luminance(ANSIColor(bg))
					if a < b {
						a, b = b, a
					}
					if ratio := (a + .05) / (b + .05); ratio < 4.5 {
						t.Errorf("%s on %s contrast %.2f < 4.5", role, bg, ratio)
					}
				}
			}
		})
	}
}
