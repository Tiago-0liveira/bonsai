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

func stripLine(s string) string { return ansi.Strip(s) }
