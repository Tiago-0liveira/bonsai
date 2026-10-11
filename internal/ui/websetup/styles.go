package websetup

import (
	"io"
	"os"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
	"github.com/Tiago-0liveira/bonsai/internal/websetup/checks"
)

// styles are built from the bonsai TUI palette on a renderer bound to the
// program's output, so colors match the rest of bonsai and NO_COLOR (or a
// terminal without colors) gives plain text. Meaning never depends on color:
// every state also has a glyph and words.
type styles struct {
	title, dim, text, accent, key, ok, warn, fail, focus lipgloss.Style
	box, boxFocus                                        lipgloss.Style
}

// NewRenderer returns the renderer for out, honouring NO_COLOR.
func NewRenderer(out io.Writer) *lipgloss.Renderer {
	r := lipgloss.NewRenderer(out)
	if os.Getenv("NO_COLOR") != "" {
		r.SetColorProfile(termenv.Ascii)
		// Bubbles components (the text inputs) style themselves with the
		// default renderer.
		lipgloss.SetColorProfile(termenv.Ascii)
	}
	return r
}

func newStyles(r *lipgloss.Renderer, p theme.Palette) styles {
	s := r.NewStyle()
	return styles{
		title:  s.Foreground(p.Accent).Bold(true),
		dim:    s.Foreground(p.Dim),
		text:   s.Foreground(p.Text),
		accent: s.Foreground(p.Accent),
		key:    s.Foreground(p.Text).Bold(true),
		ok:     s.Foreground(p.Success),
		warn:   s.Foreground(p.Warning),
		fail:   s.Foreground(p.Danger),
		focus:  s.Foreground(p.Accent).Bold(true),
		box: s.Border(lipgloss.RoundedBorder()).BorderForeground(p.Border).
			Padding(0, 1),
		boxFocus: s.Border(lipgloss.RoundedBorder()).BorderForeground(p.BorderFocus).
			Padding(0, 1),
	}
}

// glyph renders a check state as a symbol that reads without color.
func (s styles) glyph(state checks.State) string {
	switch state {
	case checks.OK:
		return s.ok.Render("✓")
	case checks.Warn:
		return s.warn.Render("!")
	case checks.Fail:
		return s.fail.Render("✗")
	default:
		return s.dim.Render("–")
	}
}

func (s styles) stepGlyph(state StepState) string {
	switch state {
	case StepDone:
		return s.ok.Render("✓")
	case StepFailed:
		return s.fail.Render("✗")
	case StepSkipped:
		return s.dim.Render("–")
	default:
		return s.accent.Render("…")
	}
}
