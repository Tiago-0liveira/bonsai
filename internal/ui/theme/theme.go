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
}

// presets are the built-in palettes.
var presets = map[string]Palette{
	// bonsai: bark-brown base with leaf-green accents.
	"bonsai": {
		Accent: "#8db84c", BorderFocus: "#8db84c", Border: "#5a4632", Dim: "#9c8265",
		Text: "#e9e2d5", Success: "#9fc96b", Danger: "#d1604f", Warning: "#dba54c", PRBadge: "#c99a5b",
	},
	// sakura: the original pink bonsai look.
	"sakura": {
		Accent: "205", BorderFocus: "205", Border: "240", Dim: "240",
		Text: "252", Success: "42", Danger: "203", Warning: "208", PRBadge: "212",
	},
	"dracula": {
		Accent: "141", BorderFocus: "141", Border: "238", Dim: "244",
		Text: "253", Success: "84", Danger: "203", Warning: "215", PRBadge: "212",
	},
	"nord": {
		Accent: "110", BorderFocus: "110", Border: "239", Dim: "245",
		Text: "254", Success: "108", Danger: "167", Warning: "179", PRBadge: "116",
	},
	"mono": {
		Accent: "255", BorderFocus: "255", Border: "240", Dim: "243",
		Text: "252", Success: "250", Danger: "245", Warning: "248", PRBadge: "255",
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
