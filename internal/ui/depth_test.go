package ui

import (
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
)

func depthModel(depth int, glyph string) Model {
	m := New(".", &config.Config{}, &config.State{Prefs: config.Prefs{DepthGlyph: glyph}}, WithDepth(depth))
	m.width, m.height = 80, 24
	return m
}

func TestStatusBarDepthIndicator(t *testing.T) {
	for _, w := range []int{40, 80, 160} {
		m := depthModel(0, "")
		m.width = w
		if bar := ansi.Strip(m.statusBar()); strings.Contains(bar, "🪴") {
			t.Errorf("w=%d depth 0 shows marker: %q", w, bar)
		}
		m = depthModel(1, "")
		m.width = w
		bar := ansi.Strip(m.statusBar())
		if !strings.HasPrefix(bar, "🪴2 ") {
			t.Errorf("w=%d depth 1 bar = %q", w, bar)
		}
		if got := ansi.StringWidth(m.statusBar()); got > w {
			t.Errorf("w=%d bar width %d overflows", w, got)
		}
	}
	if bar := ansi.Strip(depthModel(2, "🌳").statusBar()); !strings.HasPrefix(bar, "🌳3 ") {
		t.Errorf("custom glyph bar = %q", bar)
	}
}

func TestShellExitBanner(t *testing.T) {
	m := depthModel(0, "")
	got, _ := m.onOpDone(opDoneMsg{label: "shell"})
	if s := got.(Model).status; s != "back in bonsai 🪴1" {
		t.Errorf("top-level banner = %q", s)
	}
	m = depthModel(2, "🌳")
	got, _ = m.onOpDone(opDoneMsg{label: "shell"})
	if s := got.(Model).status; s != "back in bonsai 🌳3" {
		t.Errorf("nested banner = %q", s)
	}
	got, _ = depthModel(0, "").onOpDone(opDoneMsg{label: "shell", err: errors.New("boom")})
	if s := got.(Model).status; s != "shell: boom" {
		t.Errorf("error status = %q", s)
	}
}

func TestShellExitBannerVisible(t *testing.T) {
	for _, width := range []int{40, 80, 160} {
		for _, depth := range []int{0, 2} {
			m := depthModel(depth, "")
			m.width = width
			got, _ := m.onOpDone(opDoneMsg{label: "shell"})
			m = got.(Model)
			bar := ansi.Strip(m.statusBar())
			if !strings.Contains(bar, m.status) {
				t.Errorf("width=%d depth=%d banner missing from %q", width, depth, bar)
			}
			if got := ansi.StringWidth(m.statusBar()); got > width {
				t.Errorf("width=%d depth=%d bar width %d overflows", width, depth, got)
			}
		}
	}
}
