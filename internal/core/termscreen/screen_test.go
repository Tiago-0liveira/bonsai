package termscreen

import (
	"strings"
	"testing"
)

func render(cols, rows int, chunks ...string) *Screen {
	s := New(cols, rows)
	for _, chunk := range chunks {
		_, _ = s.Write([]byte(chunk))
	}
	return s
}

func TestPrintWrapAndScroll(t *testing.T) {
	s := render(5, 3, "abcdefg\r\nxy\r\nz\r\nlast")
	want := []string{"xy", "z", "last"}
	if got := s.Lines(); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("lines = %q, want %q", got, want)
	}
	// A full row does not wrap until the next character arrives.
	s = render(3, 2, "abc\r\nd")
	if got := s.String(); got != "abc\nd" {
		t.Fatalf("pending wrap = %q", got)
	}
}

func TestCursorMovementAndErase(t *testing.T) {
	for name, tc := range map[string]struct {
		in   string
		want string
	}{
		"cup and cha":       {"\x1b[2;3Hx\x1b[1Gy", "\ny x"},
		"cuu cud cuf cub":   {"\x1b[3;1Ha\x1b[2Ab\x1b[1Bc\x1b[3Cd\x1b[2De", " b\n  c  ed\na"},
		"erase line right":  {"hello\x1b[1;3H\x1b[K", "he"},
		"erase line left":   {"hello\x1b[1;3H\x1b[1K", "   lo"},
		"erase whole line":  {"hello\x1b[2K", ""},
		"erase below":       {"aa\r\nbb\r\ncc\x1b[2;2H\x1b[J", "aa\nb"},
		"erase above":       {"aa\r\nbb\r\ncc\x1b[2;1H\x1b[1J", "\n b\ncc"},
		"erase screen":      {"aa\r\nbb\x1b[2J", ""},
		"erase chars":       {"abcdef\x1b[1;2H\x1b[3X", "a   ef"},
		"delete chars":      {"abcdef\x1b[1;2H\x1b[2P", "adef"},
		"insert chars":      {"abcd\x1b[1;2H\x1b[2@", "a  bcd"},
		"vpa":               {"\x1b[3dz", "\n\nz"},
		"backspace and tab": {"ab\bc\tz", "ac      z"},
		"save restore":      {"\x1b7\x1b[3;3Hb\x1b8a", "a\n\n  b"},
		"csi save restore":  {"\x1b[2;2H\x1b[s\x1b[3;3Hb\x1b[ua", "\n a\n  b"},
		"colors ignored":    {"\x1b[1;31mred\x1b[0m", "red"},
		"osc ignored":       {"\x1b]0;title\x07ok", "ok"},
	} {
		if got := strings.TrimRight(render(10, 3, tc.in).String(), "\n"); got != strings.TrimRight(tc.want, "\n") {
			t.Errorf("%s: %q, want %q", name, got, tc.want)
		}
	}
}

func TestAltScreenAndInsertDeleteLines(t *testing.T) {
	s := render(10, 3, "main", "\x1b[?1049h\x1b[H", "alt")
	if got := s.String(); !strings.HasPrefix(got, "alt") {
		t.Fatalf("alt = %q", got)
	}
	_, _ = s.Write([]byte("\x1b[?1049l"))
	if _, _ = s.Write([]byte("!")); s.Lines()[0] != "main!" {
		t.Fatalf("back to main = %q", s.Lines()[0])
	}
	s = render(5, 3, "a\r\nb\r\nc\x1b[2;1H\x1b[L")
	if got := s.String(); got != "a\n\nb" {
		t.Fatalf("insert line = %q", got)
	}
	s = render(5, 3, "a\r\nb\r\nc\x1b[1;1H\x1b[M")
	if got := s.String(); got != "b\nc\n" {
		t.Fatalf("delete line = %q", got)
	}
}

// A header painted after other text, by moving the cursor back to the top, as
// agy draws "email (plan)".
func TestLateHeaderPaintedByCursorMovement(t *testing.T) {
	s := render(60, 10,
		"\x1b[?1049h\x1b[2J\x1b[H",
		"\x1b[5;1H> \x1b[2mType a message\x1b[0m",
		"\x1b[9;1H gemini-3.8-flash (high)",
		"\x1b[1;1H\x1b[2K\x1b[1m  me@example.com\x1b[0m \x1b[36m(Google AI Pro)\x1b[0m",
		"\x1b[5;3H",
	)
	lines := s.Lines()
	if lines[0] != "  me@example.com (Google AI Pro)" || lines[4] != "> Type a message" {
		t.Fatalf("screen =\n%s", s)
	}
}

// Split escape sequences across writes still parse.
func TestSplitWrites(t *testing.T) {
	s := render(20, 2, "\x1b[", "2;", "3Hx", "\x1b", "[1;1Hy")
	if got := s.String(); got != "y\n  x" {
		t.Fatalf("split = %q", got)
	}
}
