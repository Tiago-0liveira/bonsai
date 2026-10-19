// Package theme centralizes the UI color palette so the whole TUI can be
// restyled from one place, driven by config (a preset plus per-role overrides).
package theme

import "github.com/charmbracelet/lipgloss"

// Palette is the set of semantic color roles the UI styles are built from.
type Palette struct {
	Accent      lipgloss.Color // primary highlight (selection, active tab, titles)
	BorderFocus lipgloss.Color // focused pane border
	Border      lipgloss.Color // blurred pane border
	Dim         lipgloss.Color // secondary/disabled text
	Text        lipgloss.Color // normal text
	Success     lipgloss.Color // running/ok/approved
	Danger      lipgloss.Color // failed/destructive/changes-requested
	Warning     lipgloss.Color // status line, pending
	PRBadge     lipgloss.Color // "#N" PR badge

	// Surface roles for the redesigned shell: three stepped neutrals plus a
	// readable secondary text (Dim stays the disabled/hint tone).
	Background lipgloss.Color // full-screen base
	Surface    lipgloss.Color // header and footer bands
	Raised     lipgloss.Color // elements lifted off the surface
	Muted      lipgloss.Color // secondary text on the surfaces
}

// presets are the built-in palettes.
var presets = map[string]Palette{
	// bonsai: bark-brown base with leaf-green accents.
	"bonsai": {
		Accent: "#8db84c", BorderFocus: "#8db84c", Border: "#5a4632", Dim: "#9c8265",
		Text: "#e9e2d5", Success: "#9fc96b", Danger: "#d1604f", Warning: "#dba54c", PRBadge: "#c99a5b",
		Background: "#111611", Surface: "#1a211a", Raised: "#262f24", Muted: "#a5a894",
	},
	// sakura: the original pink bonsai look.
	"sakura": {
		Accent: "205", BorderFocus: "205", Border: "240", Dim: "240",
		Text: "252", Success: "42", Danger: "203", Warning: "208", PRBadge: "212",
		Background: "233", Surface: "235", Raised: "237", Muted: "246",
	},
	"dracula": {
		Accent: "141", BorderFocus: "141", Border: "238", Dim: "244",
		Text: "253", Success: "84", Danger: "203", Warning: "215", PRBadge: "212",
		Background: "234", Surface: "236", Raised: "238", Muted: "247",
	},
	"nord": {
		Accent: "110", BorderFocus: "110", Border: "239", Dim: "245",
		Text: "254", Success: "108", Danger: "167", Warning: "179", PRBadge: "116",
		Background: "235", Surface: "236", Raised: "238", Muted: "248",
	},
	"mono": {
		Accent: "255", BorderFocus: "255", Border: "240", Dim: "243",
		Text: "252", Success: "250", Danger: "245", Warning: "248", PRBadge: "255",
		Background: "233", Surface: "235", Raised: "237", Muted: "247",
	},
}

// presetOrder is the display order for the preset list.
var presetOrder = []string{"bonsai", "sakura", "dracula", "nord", "mono"}

// Presets returns the built-in palette names in display order.
func Presets() []string { return presetOrder }

// Valid reports whether name is a built-in preset.
func Valid(name string) bool {
	_, ok := presets[name]
	return ok
}

// Current is the palette in effect; components read it when (re)building styles.
var Current = presets["bonsai"]

// applyOverride sets a single role by name (lowercase). Unknown roles are ignored.
func (p *Palette) applyOverride(role, color string) {
	c := lipgloss.Color(color)
	switch role {
	case "accent":
		p.Accent = c
	case "border_focus", "borderfocus":
		p.BorderFocus = c
	case "border":
		p.Border = c
	case "dim":
		p.Dim = c
	case "text":
		p.Text = c
	case "success":
		p.Success = c
	case "danger":
		p.Danger = c
	case "warning":
		p.Warning = c
	case "pr_badge", "prbadge":
		p.PRBadge = c
	case "background":
		p.Background = c
	case "surface":
		p.Surface = c
	case "raised":
		p.Raised = c
	case "muted":
		p.Muted = c
	}
}

// Resolve builds a Palette from a preset name plus per-role overrides. An unknown
// preset falls back to bonsai.
func Resolve(preset string, overrides map[string]string) Palette {
	p, ok := presets[preset]
	if !ok {
		p = presets["bonsai"]
	}
	for role, color := range overrides {
		if color != "" {
			p.applyOverride(role, color)
		}
	}
	return p
}

// ApplyContrast adjusts the neutral surfaces for a contrast mode. "flat"
// collapses Surface and Raised onto Background so terminals with a poor
// neutral ramp show no stepped bands; "" and "layered" keep the theme's own
// shades, which stay theme-defined rather than derived.
func ApplyContrast(p Palette, mode string) Palette {
	if mode == "flat" {
		p.Surface = p.Background
		p.Raised = p.Background
	}
	return p
}
