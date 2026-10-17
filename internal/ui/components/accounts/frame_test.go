package accounts

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func plainLines(s string) []string {
	return strings.Split(ansi.Strip(s), "\n")
}

func assertBox(t *testing.T, lines []string, w, h int) {
	t.Helper()
	if len(lines) != h {
		t.Fatalf("got %d lines, want %d", len(lines), h)
	}
	for i, l := range lines {
		if got := ansi.StringWidth(l); got != w {
			t.Fatalf("line %d is %d cells, want %d: %q", i, got, w, l)
		}
	}
}

func TestFrameSizes(t *testing.T) {
	left := []string{"left one", "\x1b[31mred left\x1b[0m"}
	right := []string{"right " + strings.Repeat("long ", 40)}
	for _, w := range []int{1, 2, 6, 40, 80, 100, 180} {
		for _, h := range []int{1, 2, 10, 30, 60} {
			out := Frame("agent accounts", left, right, "j/k Navigate  ·  Esc Close", w, h)
			assertBox(t, plainLines(out), w, h)
			single := FrameSingle("usage", right, "Esc Close", w, h)
			assertBox(t, plainLines(single), w, h)
		}
	}
}

func TestFrameHasNoModalChrome(t *testing.T) {
	for _, out := range []string{
		Frame("agent accounts", []string{"ACCOUNTS"}, []string{"detail"}, "Esc Close", 160, 48),
		FrameSingle("agent accounts", []string{"detail"}, "Esc Close", 160, 48),
	} {
		lines := plainLines(out)
		assertBox(t, lines, 160, 48)
		if strings.TrimSpace(lines[0]) != "AGENT ACCOUNTS" || strings.TrimSpace(lines[len(lines)-1]) != "Esc Close" {
			t.Fatalf("title/footer missing:\n%s", out)
		}
		if strings.ContainsAny(out, "╭╮╰╯├┤┴│") {
			t.Fatalf("pane still has modal borders:\n%s", out)
		}
		if !strings.Contains(lines[2], "detail") {
			t.Fatalf("missing content: %q", lines[2])
		}
	}
}

func TestFrameTinyKeepsHeight(t *testing.T) {
	for _, h := range []int{0, 1, 2, 5} {
		out := Frame("t", nil, nil, "f", 20, h)
		got := 0
		if out != "" {
			got = len(strings.Split(out, "\n"))
		}
		if got != h {
			t.Fatalf("h=%d: got %d lines", h, got)
		}
	}
}

func TestLeftWidth(t *testing.T) {
	if got := LeftWidth(100); got != 26 {
		t.Fatalf("wide: %d", got)
	}
	if got := LeftWidth(60); got != 24 {
		t.Fatalf("medium: %d", got)
	}
	if got := LeftWidth(40); got*2+4 > 40 {
		t.Fatalf("narrow left %d leaves no right column", got)
	}
}

func stripLine(s string) string { return ansi.Strip(s) }
