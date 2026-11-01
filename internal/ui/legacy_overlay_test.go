package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/ui/components/modals"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/worktreelist"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func TestLegacyOverlayRetainsBase(t *testing.T) {
	for _, size := range [][2]int{{160, 45}, {100, 30}, {40, 12}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			m := renderModel()
			m.list = worktreelist.New()
			m.ready = true
			m.width, m.height = size[0], size[1]
			m.layout()
			modal := modals.NewInput(modals.KindCreateNew, "Unique modal title", "branch")
			modal.SetSize(m.width, m.height)
			m.modal = &modal
			out := m.View()
			plain := ansi.Strip(out)
			if !strings.Contains(plain, "Unique modal title") || !strings.Contains(plain, "Log (") {
				t.Fatalf("missing modal or base:\n%s", plain)
			}
			if lipgloss.Height(out) != m.height || lipgloss.Width(out) > m.width {
				t.Fatalf("frame %dx%d", lipgloss.Width(out), lipgloss.Height(out))
			}
		})
	}
}
