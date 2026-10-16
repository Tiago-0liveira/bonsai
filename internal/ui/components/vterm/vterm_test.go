package vterm

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

func newTerm(t *testing.T, cols, rows, w, h int) *Model {
	t.Helper()
	m := New(cols, rows)
	m.SetViewport(w, h)
	t.Cleanup(m.Close)
	return m
}

func lines(t *testing.T, m *Model, w, h int) []string {
	t.Helper()
	ls := strings.Split(m.View(), "\n")
	if len(ls) != h {
		t.Fatalf("got %d lines, want %d", len(ls), h)
	}
	for i, l := range ls {
		if got := ansi.StringWidth(l); got != w {
			t.Fatalf("line %d width %d, want %d: %q", i, got, w, l)
		}
		if !strings.HasSuffix(l, "\x1b[0m") {
			t.Fatalf("line %d does not end with SGR reset: %q", i, l)
		}
	}
	return ls
}

func plain(ls []string) []string {
	out := make([]string, len(ls))
	for i, l := range ls {
		out[i] = strings.TrimRight(ansi.Strip(l), " ")
	}
	return out
}

func TestPlainTextAndWrap(t *testing.T) {
	m := newTerm(t, 10, 4, 10, 4)
	m.Write([]byte("hello\r\n0123456789ABC"))
	got := plain(lines(t, m, 10, 4))
	want := []string{"hello", "0123456789", "ABC", ""}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestCUPAndErase(t *testing.T) {
	m := newTerm(t, 20, 5, 20, 5)
	m.Write([]byte("\x1b[2J\x1b[3;5Hmid\x1b[1;1Hxxxxxxxx\x1b[1;4H\x1b[K"))
	got := plain(lines(t, m, 20, 5))
	if got[2] != "    mid" {
		t.Errorf("row 2 = %q", got[2])
	}
	if got[0] != "xxx" {
		t.Errorf("row 0 = %q, want erase-to-end-of-line applied", got[0])
	}
}

func TestSGR(t *testing.T) {
	tests := []struct{ name, seq string }{
		{"16", "\x1b[31mtext\x1b[0m"},
		{"256", "\x1b[38;5;208mtext\x1b[0m"},
		{"truecolor", "\x1b[38;2;10;200;30mtext\x1b[0m"},
		{"truecolor bg", "\x1b[48;2;10;20;30;1mtext\x1b[0m"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newTerm(t, 20, 2, 20, 2)
			m.Write([]byte(tt.seq))
			ls := lines(t, m, 20, 2)
			if !strings.Contains(ansi.Strip(ls[0]), "text") {
				t.Errorf("content lost: %q", ls[0])
			}
			if !strings.Contains(ls[0], "\x1b[") || ls[0] == ansi.Strip(ls[0])+sgrReset {
				t.Errorf("no SGR emitted: %q", ls[0])
			}
		})
	}
}

func TestBoxDrawing(t *testing.T) {
	m := newTerm(t, 8, 3, 8, 3)
	m.Write([]byte("╭──────╮\r\n│ hi   │\r\n╰──────╯"))
	got := plain(lines(t, m, 8, 3))
	if got[0] != "╭──────╮" || got[1] != "│ hi   │" {
		t.Errorf("got %q", got)
	}
}

func TestAltScreen(t *testing.T) {
	m := newTerm(t, 20, 3, 20, 3)
	m.Write([]byte("main text"))
	m.Write([]byte("\x1b[?1049h\x1b[2J\x1b[1;1Halt text"))
	if !m.AltScreen() {
		t.Fatal("AltScreen = false after enter")
	}
	if got := plain(lines(t, m, 20, 3)); got[0] != "alt text" {
		t.Errorf("alt row = %q", got[0])
	}
	m.Write([]byte("\x1b[?1049l"))
	if m.AltScreen() {
		t.Fatal("AltScreen = true after exit")
	}
	if got := plain(lines(t, m, 20, 3)); got[0] != "main text" {
		t.Errorf("main row after exit = %q", got[0])
	}
}

func TestWideChars(t *testing.T) {
	tests := []struct{ name, in string }{
		{"cjk", "漢字テスト"},
		{"emoji", "a😀b🎉c"},
		{"mixed at edge", "123456789漢"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newTerm(t, 10, 2, 10, 2)
			m.Write([]byte(tt.in))
			ls := lines(t, m, 10, 2)
			if !strings.Contains(strings.Join(plain(ls), ""), strings.TrimSuffix(tt.in, "漢")) {
				t.Errorf("content lost: %q", plain(ls))
			}
		})
	}
	t.Run("two cells", func(t *testing.T) {
		m := newTerm(t, 10, 1, 10, 1)
		m.Write([]byte("漢X"))
		if got := plain(lines(t, m, 10, 1))[0]; got != "漢X" {
			t.Errorf("got %q", got)
		}
		if ansi.StringWidth("漢X") != 3 {
			t.Fatal("test premise: 漢 is 2 cells")
		}
	})
	t.Run("crop through wide glyph", func(t *testing.T) {
		m := newTerm(t, 10, 1, 3, 1)
		m.Write([]byte("ab漢c"))
		lines(t, m, 3, 1)
	})
}

func TestScrollRegion(t *testing.T) {
	m := newTerm(t, 10, 5, 10, 5)
	m.Write([]byte("top\r\n"))
	m.Write([]byte("\x1b[2;4r\x1b[2;1H"))
	for i := 1; i <= 5; i++ {
		m.Write([]byte(fmt.Sprintf("L%d\r\n", i)))
	}
	m.Write([]byte("\x1b[5;1Hfoot"))
	got := plain(lines(t, m, 10, 5))
	if got[0] != "top" || got[4] != "foot" {
		t.Errorf("rows outside region changed: %q", got)
	}
	if got[1] != "L4" || got[2] != "L5" || got[3] != "" {
		t.Errorf("region did not scroll: %q", got)
	}
}

func TestResizeKeepsCursorInBounds(t *testing.T) {
	m := newTerm(t, 20, 10, 20, 10)
	m.SetFocused(true)
	m.Write([]byte("\x1b[10;20Hx"))
	for _, sz := range [][2]int{{5, 3}, {40, 20}, {1, 1}, {0, 0}, {120, 40}} {
		m.ResizeEmulator(sz[0], sz[1])
		pos := m.emu.CursorPosition()
		if pos.X < 0 || pos.Y < 0 || pos.X >= m.emu.Width() || pos.Y >= m.emu.Height() {
			t.Errorf("resize %v: cursor %v out of bounds", sz, pos)
		}
		lines(t, m, 20, 10)
	}
}

func TestViewDimensions(t *testing.T) {
	sizes := []struct {
		name             string
		cols, rows, w, h int
	}{
		{"tiny", 80, 24, 1, 1},
		{"zero height", 80, 24, 10, 0},
		{"narrow 80x24", 80, 24, 80, 24},
		{"pane bigger", 40, 10, 100, 30},
		{"pane smaller", 120, 40, 50, 12},
		{"wide only", 120, 5, 30, 5},
		{"tall only", 10, 60, 10, 20},
	}
	for _, tt := range sizes {
		t.Run(tt.name, func(t *testing.T) {
			m := newTerm(t, tt.cols, tt.rows, tt.w, tt.h)
			m.SetFocused(true)
			var b strings.Builder
			for y := 0; y < tt.rows; y++ {
				fmt.Fprintf(&b, "\x1b[3%dm%s\x1b[0m\r\n", y%8, strings.Repeat("漢a😀", 50))
			}
			m.Write([]byte(b.String()))
			if tt.h == 0 {
				if v := m.View(); v != "" {
					t.Errorf("h=0 View = %q", v)
				}
				return
			}
			lines(t, m, tt.w, tt.h)
		})
	}
}

func TestObserverCropShowsCursorRow(t *testing.T) {
	t.Run("bottom aligned by default", func(t *testing.T) {
		m := newTerm(t, 20, 10, 20, 3)
		for i := 0; i < 10; i++ {
			m.Write([]byte(fmt.Sprintf("\x1b[%d;1Hrow%d", i+1, i)))
		}
		m.Write([]byte("\x1b[10;1H"))
		got := plain(lines(t, m, 20, 3))
		if got[0] != "row7" || got[2] != "row9" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("cursor above the bottom window", func(t *testing.T) {
		m := newTerm(t, 20, 10, 20, 3)
		for i := 0; i < 10; i++ {
			m.Write([]byte(fmt.Sprintf("\x1b[%d;1Hrow%d", i+1, i)))
		}
		m.Write([]byte("\x1b[3;1H"))
		got := plain(lines(t, m, 20, 3))
		if got[0] != "row2" {
			t.Errorf("cursor row not first visible: %q", got)
		}
	})
	t.Run("left columns kept", func(t *testing.T) {
		m := newTerm(t, 20, 2, 5, 2)
		m.Write([]byte("0123456789"))
		if got := plain(lines(t, m, 5, 2))[0]; got != "01234" {
			t.Errorf("got %q", got)
		}
	})
}

func TestCursor(t *testing.T) {
	tests := []struct {
		name    string
		focused bool
		seq     string
		want    bool
	}{
		{"focused visible", true, "ab", true},
		{"unfocused", false, "ab", false},
		{"focused hidden", true, "ab\x1b[?25l", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newTerm(t, 10, 2, 10, 2)
			m.SetFocused(tt.focused)
			m.Write([]byte(tt.seq))
			got := strings.Contains(m.View(), "\x1b[7m")
			if got != tt.want {
				t.Errorf("reverse present = %v, want %v", got, tt.want)
			}
		})
	}
	t.Run("focus change redraws", func(t *testing.T) {
		m := newTerm(t, 10, 2, 10, 2)
		_ = m.View()
		m.SetFocused(true)
		if !strings.Contains(m.View(), "\x1b[7m") {
			t.Error("cache not invalidated by SetFocused")
		}
	})
}

func TestTitle(t *testing.T) {
	m := newTerm(t, 10, 2, 10, 2)
	m.Write([]byte("\x1b]0;first\x07"))
	if m.Title() != "first" {
		t.Errorf("OSC 0 title = %q", m.Title())
	}
	m.Write([]byte("\x1b]2;second\x1b\\"))
	if m.Title() != "second" {
		t.Errorf("OSC 2 title = %q", m.Title())
	}
}

func TestQueryDoesNotBlock(t *testing.T) {
	m := newTerm(t, 10, 2, 10, 2)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 50; i++ {
			m.Write([]byte("\x1b[6n\x1b[c\x1b[>c"))
		}
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Write blocked on a terminal query nobody reads")
	}
}

func TestCloseStopsDrainer(t *testing.T) {
	ms := make([]*Model, 20)
	for i := range ms {
		ms[i] = New(10, 2)
	}
	for _, m := range ms {
		m.Close()
		select {
		case <-m.drainDone:
		default:
			t.Fatal("drainer still running after Close")
		}
		m.Close() // idempotent
		m.Write([]byte("ignored\x1b[6n"))
	}
}

func TestConcurrentWriteView(t *testing.T) {
	m := newTerm(t, 40, 10, 40, 10)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 200; i++ {
			m.Write([]byte("\x1b[32mline\x1b[0m\r\n"))
		}
	}()
	for i := 0; i < 200; i++ {
		_ = m.View()
		m.SetViewport(30+i%5, 8)
	}
	<-done
}

// TestVisualCapture prints a local capture for eyeballing a real agent:
//
//	BONSAI_VTERM_CAPTURE=/path/to/typescript go test ./internal/ui/components/vterm -run VisualCapture -v
//
// BONSAI_VTERM_SIZE=COLSxROWS (default 120x40). Never commit captures.
func TestVisualCapture(t *testing.T) {
	path := os.Getenv("BONSAI_VTERM_CAPTURE")
	if path == "" {
		t.Skip("BONSAI_VTERM_CAPTURE not set")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	cols, rows := 120, 40
	if s := os.Getenv("BONSAI_VTERM_SIZE"); s != "" {
		fmt.Sscanf(s, "%dx%d", &cols, &rows)
	}
	m := newTerm(t, cols, rows, cols, rows)
	m.Write(data)
	lines(t, m, cols, rows)
	fmt.Println(m.View())
}

func synthStream(n int) []byte {
	var b strings.Builder
	for i := 0; b.Len() < n; i++ {
		fmt.Fprintf(&b, "\x1b[38;2;%d;%d;%dm\x1b[48;5;%dmtoken %d \x1b[1;4mbold\x1b[0m \x1b[3%dmcolor\x1b[0m │ ╭─╮ 漢字\r\n",
			i%256, (i*7)%256, (i*13)%256, i%256, i, i%8)
	}
	return []byte(b.String())
}

func BenchmarkWriteReplay2MiB(b *testing.B) {
	data := synthStream(2 << 20)
	b.SetBytes(int64(len(data)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m := New(120, 40)
		m.Write(data)
		m.Close()
	}
}

func BenchmarkView120x40(b *testing.B) {
	m := New(120, 40)
	defer m.Close()
	m.SetViewport(120, 40)
	m.SetFocused(true)
	m.Write(synthStream(200 << 10))
	b.Run("dirty", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			m.SetFocused(i%2 == 0)
			_ = m.View()
		}
	})
	b.Run("cached", func(b *testing.B) {
		_ = m.View()
		for i := 0; i < b.N; i++ {
			_ = m.View()
		}
	})
}
