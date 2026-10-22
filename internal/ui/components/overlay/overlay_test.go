package overlay

import (
	"fmt"
	"image/color"
	"strings"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
)

func screen(t *testing.T, frame string, w, h int) *vt.Emulator {
	t.Helper()
	v := vt.NewEmulator(w, h)
	if _, err := v.Write([]byte(strings.ReplaceAll(frame, "\n", "\r\n"))); err != nil {
		t.Fatal(err)
	}
	return v
}

func equalColor(a, b color.Color) bool {
	if a == nil || b == nil {
		return a == b
	}
	ar, ag, ab, aa := a.RGBA()
	br, bg, bb, ba := b.RGBA()
	return ar == br && ag == bg && ab == bb && aa == ba
}

func TestCutCells(t *testing.T) {
	for _, tc := range []struct {
		name, text, want string
		left, width      int
	}{
		{"ascii padding", "abc", "bc  ", 1, 4},
		{"combining", "Ae\u0301Z", "e\u0301", 1, 1},
		{"combining base excluded", "e\u0301Z", "Z ", 1, 2},
		{"wide left", "A界Z", " Z", 2, 2},
		{"wide right", "A界Z", "A ", 0, 2},
		{"wide both edges", "界界", "  ", 1, 2},
		{"joined emoji left", "A👩‍💻Z", " Z", 2, 2},
		{"joined emoji right", "A👩‍💻Z", "A ", 0, 2},
		{"flag", "🇵🇹Z", " Z", 1, 2},
		{"keycap", "1️⃣Z", " Z", 1, 2},
		{"past end", "界", "   ", 4, 3},
		{"empty", "", "  ", 0, 2},
		{"zero", "界", "", 0, 0},
		{"styled", "\x1b[31m界e\u0301\x1b[0m!", " e\u0301!", 1, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := cut(tc.text, tc.left, tc.width, "")
			if ansi.Strip(got) != tc.want || ansi.StringWidth(got) != tc.width {
				t.Fatalf("cut = %q (%d cells), want %q", got, ansi.StringWidth(got), tc.want)
			}
		})
	}
}

func TestComposeClippedEdges(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		x, y       int
	}{
		{"inside", "abcd\na界d\nabcd", 1, 1},
		{"left", "abcd\n bcd\nabcd", -1, 1},
		{"right", "abcd\nabc \nabcd", 3, 1},
		{"above", "abcd\nabcd\nabcd", 1, -1},
		{"below", "abcd\nabcd\nabcd", 1, 3},
		{"outside", "abcd\nabcd\nabcd", 8, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := Compose(Frame{"abcd\nabcd\nabcd", 4, 3}, Frame{"界", 2, 1}, tc.x, tc.y, nil)
			if ansi.Strip(got) != tc.want {
				t.Fatalf("got %q, want %q", ansi.Strip(got), tc.want)
			}
		})
	}
}

func TestComposeStyles(t *testing.T) {
	bg := ansi.RGBColor{R: 26, G: 33, B: 26}
	for _, tc := range []struct {
		name, parent, panel string
	}{
		{"unclosed", "\x1b[1;31;44mabcdefgh\nijklmnop", "\x1b[32mX"},
		{"selective reset", "\x1b[1;31;44mab\x1b[22mcdefgh\nijklmnop", "\x1b[1;32mX\x1b[22;39;49mY"},
		{"compound reset", "\x1b[0;31;44mabcdefgh\nijklmnop", "\x1b[0;32;41mX\x1b[0mY"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			parent := Frame{tc.parent, 8, 2}
			frame := Compose(parent, Frame{tc.panel, 4, 1}, 2, 0, ansi.NewStyle().BackgroundColor(bg))
			got := screen(t, frame+"\r\nQ", 9, 3)
			want := screen(t, Compose(parent, Frame{}, 0, 0, nil), 9, 3)
			for y := 0; y < 2; y++ {
				for x := 0; x < 8; x++ {
					cell := got.CellAt(x, y)
					if y == 0 && x >= 2 && x < 6 {
						if !equalColor(cell.Style.Bg, bg) {
							t.Errorf("panel (%d,%d) background = %v", x, y, cell.Style.Bg)
						}
					} else if !cell.Equal(want.CellAt(x, y)) {
						t.Errorf("parent (%d,%d) = %+v, want %+v", x, y, cell, want.CellAt(x, y))
					}
				}
			}
			if !got.CellAt(0, 2).Style.IsZero() {
				t.Errorf("style leaked into next line: %+v", got.CellAt(0, 2))
			}
			if got.CellAt(6, 0).Style.Attrs&uv.AttrBold != want.CellAt(6, 0).Style.Attrs&uv.AttrBold {
				t.Error("parent bold was not restored")
			}
		})
	}
}

func TestGeometryAndBounds(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {120, 35}, {160, 48}, {1, 1}, {0, 0}, {0, 3}, {3, 0}, {2, 2}, {-1, -1}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			w, h := max(size[0], 0), max(size[1], 0)
			x, y, pw, ph := Center(size[0], size[1])
			if x < 0 || y < 0 || x+pw > w || y+ph > h || x != (w-pw)/2 || y != (h-ph)/2 {
				t.Fatalf("unbounded geometry: %d,%d %dx%d", x, y, pw, ph)
			}
			got := Compose(Frame{"oversized parent\n界", w, h}, Frame{"panel", pw, ph}, x, y, nil)
			if h == 0 {
				if got != "" {
					t.Fatal("nonempty zero-height frame")
				}
				return
			}
			lines := strings.Split(got, "\n")
			if len(lines) != h {
				t.Fatalf("height %d, want %d", len(lines), h)
			}
			for _, line := range lines {
				if ansi.StringWidth(line) != w {
					t.Fatalf("width %d, want %d", ansi.StringWidth(line), w)
				}
			}
		})
	}
}

func TestPanelPadding(t *testing.T) {
	bg := ansi.RGBColor{R: 26, G: 33, B: 26}
	for _, pos := range [][2]int{{1, 1}, {-1, -1}, {5, 2}} {
		t.Run(fmt.Sprint(pos), func(t *testing.T) {
			frame := Compose(Frame{"abcdefgh\nabcdefgh\nabcdefgh\nabcdefgh", 8, 4},
				Frame{"\x1b[41mX\x1b[49mY", 4, 3}, pos[0], pos[1], ansi.NewStyle().BackgroundColor(bg))
			v := screen(t, frame, 9, 5)
			for y := max(pos[1], 0); y < min(pos[1]+3, 4); y++ {
				for x := max(pos[0], 0); x < min(pos[0]+4, 8); x++ {
					if !equalColor(v.CellAt(x, y).Style.Bg, bg) {
						t.Fatalf("unpainted text or padding at %d,%d", x, y)
					}
				}
			}
		})
	}
}

func TestCenterReferenceSizes(t *testing.T) {
	for _, tc := range []struct {
		width, height, x, y, w, h int
	}{
		{80, 24, 1, 2, 78, 20},
		{120, 35, 8, 5, 104, 25},
		{160, 48, 28, 11, 104, 26},
	} {
		t.Run(fmt.Sprintf("%dx%d", tc.width, tc.height), func(t *testing.T) {
			x, y, w, h := Center(tc.width, tc.height)
			if x != tc.x || y != tc.y || w != tc.w || h != tc.h {
				t.Fatalf("geometry %d,%d %dx%d, want %d,%d %dx%d", x, y, w, h, tc.x, tc.y, tc.w, tc.h)
			}
		})
	}
}
