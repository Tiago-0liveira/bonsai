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
	for _, w := range []int{40, 80, 100} {
		for _, h := range []int{10, 30} {
			out := Frame("agent accounts", left, right, "j/k Navigate  ·  Esc Close", w, h)
			assertBox(t, plainLines(out), w, h)
			single := FrameSingle("usage", right, "Esc Close", w, h)
			assertBox(t, plainLines(single), w, h)
		}
	}
}

func TestFrameChrome(t *testing.T) {
	lines := plainLines(Frame("agent accounts", []string{"ACCOUNTS"}, []string{"detail"}, "Esc Close", 80, 12))
	if !strings.HasPrefix(lines[0], "╭─ AGENT ACCOUNTS ─") || !strings.HasSuffix(lines[0], "╮") {
		t.Fatalf("top border: %q", lines[0])
	}
	if strings.Trim(lines[1], "│ ") != "" || strings.Contains(lines[1][3:len(lines[1])-3], "│") {
		t.Fatalf("row under the border should be blank without a divider: %q", lines[1])
	}
	div := strings.Index(lines[2], "│ ACCOUNTS") // first body row
	if div != 0 || !strings.Contains(lines[2], "│ detail") {
		t.Fatalf("body row: %q", lines[2])
	}
	sep := lines[len(lines)-3]
	if !strings.HasPrefix(sep, "├") || !strings.HasSuffix(sep, "┤") || strings.Count(sep, "┴") != 1 {
		t.Fatalf("separator: %q", sep)
	}
	// The join must sit directly under the column divider.
	if ansi.StringWidth(sep[:strings.Index(sep, "┴")]) != ansi.StringWidth(lines[2][:strings.Index(lines[2], "│ detail")]) {
		t.Fatalf("separator join not under divider:\n%s\n%s", lines[2], sep)
	}
	if lines[len(lines)-2] != "│ Esc Close"+strings.Repeat(" ", 80-ansi.StringWidth("│ Esc Close")-1)+"│" {
		t.Fatalf("footer: %q", lines[len(lines)-2])
	}
	if !strings.HasPrefix(lines[len(lines)-1], "╰") || !strings.HasSuffix(lines[len(lines)-1], "╯") {
		t.Fatalf("bottom border: %q", lines[len(lines)-1])
	}
}

func TestFrameSingleSeparatorHasNoJoin(t *testing.T) {
	lines := plainLines(FrameSingle("usage", []string{"x"}, "Esc", 64, 10))
	sep := lines[len(lines)-3]
	if strings.Contains(sep, "┴") || !strings.HasPrefix(sep, "├") || !strings.HasSuffix(sep, "┤") {
		t.Fatalf("separator: %q", sep)
	}
	if !strings.HasPrefix(lines[0], "╭─ USAGE ─") {
		t.Fatalf("title: %q", lines[0])
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
	if got := LeftWidth(40); got*2+7 > 40 {
		t.Fatalf("narrow left %d leaves no right column", got)
	}
}

func stripLine(s string) string { return ansi.Strip(s) }
