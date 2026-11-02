package theme

import (
	"strings"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents/usagefmt"
	"github.com/charmbracelet/lipgloss"
)

// Bar colors filled and empty runs independently so unused capacity stays muted.
func Bar(frac float64, width int, ascii bool, filledStyle, emptyStyle lipgloss.Style) string {
	width = max(0, width)
	b := usagefmt.Bar(frac, width)
	filled, empty := "▰", "▱"
	if ascii {
		filled, empty = "#", "."
	}
	n := strings.Count(b, "█")
	return filledStyle.Render(strings.Repeat(filled, n)) +
		emptyStyle.Render(strings.Repeat(empty, width-n))
}
