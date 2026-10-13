// Package termscreen renders a terminal program's output onto a grid of
// characters, so text a TUI paints by moving the cursor can be read back as it
// appears on screen. It keeps characters only: colors, modes and scroll regions
// are ignored, and every rune takes one cell.
package termscreen

import (
	"strings"
	"sync"

	"github.com/charmbracelet/x/ansi"
)

// Screen is a cols×rows grid fed by Write. It is safe for one writer and
// concurrent readers.
type Screen struct {
	mu     sync.Mutex
	parser *ansi.Parser

	cols, rows int
	cells      [][]rune
	main       [][]rune // the main screen while the alternate one is shown

	x, y        int
	savedX      int
	savedY      int
	pendingWrap bool
}

// New returns a blank screen; sizes below 1 become 1.
func New(cols, rows int) *Screen {
	s := &Screen{cols: max(cols, 1), rows: max(rows, 1)}
	s.cells = s.blank()
	s.parser = ansi.NewParser()
	s.parser.SetHandler(ansi.Handler{
		Print:     s.print,
		Execute:   s.execute,
		HandleCsi: s.csi,
		HandleEsc: s.esc,
	})
	return s
}

// Write feeds terminal output to the screen. It never fails.
func (s *Screen) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.parser.Parse(p)
	return len(p), nil
}

// Lines returns the visible rows, with trailing blanks removed.
func (s *Screen) Lines() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.cells))
	for i, row := range s.cells {
		out[i] = strings.TrimRight(string(row), " ")
	}
	return out
}

// String returns the visible rows joined by newlines.
func (s *Screen) String() string {
	return strings.Join(s.Lines(), "\n")
}

func (s *Screen) blank() [][]rune {
	cells := make([][]rune, s.rows)
	for i := range cells {
		cells[i] = s.blankRow()
	}
	return cells
}

func (s *Screen) blankRow() []rune {
	row := make([]rune, s.cols)
	for i := range row {
		row[i] = ' '
	}
	return row
}

func (s *Screen) print(r rune) {
	if s.pendingWrap {
		s.x = 0
		s.lineFeed()
		s.pendingWrap = false
	}
	s.cells[s.y][s.x] = r
	if s.x == s.cols-1 {
		s.pendingWrap = true
		return
	}
	s.x++
}

func (s *Screen) execute(b byte) {
	switch b {
	case '\r':
		s.moveTo(0, s.y)
	case '\n', '\v', '\f':
		s.pendingWrap = false
		s.lineFeed()
	case '\b':
		s.moveTo(s.x-1, s.y)
	case '\t':
		s.moveTo((s.x/8+1)*8, s.y)
	}
}

// lineFeed moves down a row, scrolling the screen up at the bottom.
func (s *Screen) lineFeed() {
	if s.y < s.rows-1 {
		s.y++
		return
	}
	s.scrollUp(1)
}

func (s *Screen) scrollUp(n int) {
	n = min(n, s.rows)
	s.cells = append(s.cells[n:], s.blank()[:n]...)
}

func (s *Screen) scrollDown(n int) {
	n = min(n, s.rows)
	s.cells = append(s.blank()[:n], s.cells[:s.rows-n]...)
}

func (s *Screen) moveTo(x, y int) {
	s.x = min(max(x, 0), s.cols-1)
	s.y = min(max(y, 0), s.rows-1)
	s.pendingWrap = false
}

func (s *Screen) clearCells(row, from, to int) {
	from, to = max(from, 0), min(to, s.cols)
	for i := from; i < to; i++ {
		s.cells[row][i] = ' '
	}
}

func (s *Screen) csi(cmd ansi.Cmd, params ansi.Params) {
	// n is the first parameter, where a missing or zero value means 1.
	n := func(i int) int {
		v, _, _ := params.Param(i, 1)
		return max(v, 1)
	}
	if cmd.Prefix() == '?' {
		switch cmd.Final() {
		case 'h', 'l':
			params.ForEach(0, func(_, mode int, _ bool) {
				on := cmd.Final() == 'h'
				if mode == 1049 && on {
					s.savedX, s.savedY = s.x, s.y
				}
				if mode == 47 || mode == 1047 || mode == 1049 {
					s.altScreen(on)
				}
				if mode == 1049 && !on {
					s.moveTo(s.savedX, s.savedY)
				}
			})
		}
		return
	}
	if cmd.Prefix() != 0 || cmd.Intermediate() != 0 {
		return
	}
	switch cmd.Final() {
	case 'A':
		s.moveTo(s.x, s.y-n(0))
	case 'B', 'e':
		s.moveTo(s.x, s.y+n(0))
	case 'C', 'a':
		s.moveTo(s.x+n(0), s.y)
	case 'D':
		s.moveTo(s.x-n(0), s.y)
	case 'E':
		s.moveTo(0, s.y+n(0))
	case 'F':
		s.moveTo(0, s.y-n(0))
	case 'G', '`':
		s.moveTo(n(0)-1, s.y)
	case 'd':
		s.moveTo(s.x, n(0)-1)
	case 'H', 'f':
		s.moveTo(n(1)-1, n(0)-1)
	case 'K':
		mode, _, _ := params.Param(0, 0)
		switch mode {
		case 0:
			s.clearCells(s.y, s.x, s.cols)
		case 1:
			s.clearCells(s.y, 0, s.x+1)
		case 2:
			s.clearCells(s.y, 0, s.cols)
		}
	case 'J':
		mode, _, _ := params.Param(0, 0)
		switch mode {
		case 0:
			s.clearCells(s.y, s.x, s.cols)
			for row := s.y + 1; row < s.rows; row++ {
				s.clearCells(row, 0, s.cols)
			}
		case 1:
			for row := 0; row < s.y; row++ {
				s.clearCells(row, 0, s.cols)
			}
			s.clearCells(s.y, 0, s.x+1)
		case 2, 3:
			s.cells = s.blank()
		}
	case 'X':
		s.clearCells(s.y, s.x, s.x+n(0))
	case 'P':
		row := s.cells[s.y]
		count := min(n(0), s.cols-s.x)
		copy(row[s.x:], row[s.x+count:])
		s.clearCells(s.y, s.cols-count, s.cols)
	case '@':
		row := s.cells[s.y]
		count := min(n(0), s.cols-s.x)
		copy(row[s.x+count:], row[s.x:s.cols-count])
		s.clearCells(s.y, s.x, s.x+count)
	case 'L':
		count := min(n(0), s.rows-s.y)
		rest := append(s.blank()[:count], s.cells[s.y:s.rows-count]...)
		s.cells = append(s.cells[:s.y:s.y], rest...)
	case 'M':
		count := min(n(0), s.rows-s.y)
		rest := append(append([][]rune{}, s.cells[s.y+count:]...), s.blank()[:count]...)
		s.cells = append(s.cells[:s.y:s.y], rest...)
	case 'S':
		s.scrollUp(n(0))
	case 'T':
		s.scrollDown(n(0))
	case 's':
		s.savedX, s.savedY = s.x, s.y
	case 'u':
		s.moveTo(s.savedX, s.savedY)
	}
}

func (s *Screen) esc(cmd ansi.Cmd) {
	if cmd.Intermediate() != 0 {
		return
	}
	switch cmd.Final() {
	case '7':
		s.savedX, s.savedY = s.x, s.y
	case '8':
		s.moveTo(s.savedX, s.savedY)
	case 'D':
		s.lineFeed()
	case 'E':
		s.moveTo(0, s.y)
		s.lineFeed()
	case 'M':
		if s.y == 0 {
			s.scrollDown(1)
		} else {
			s.y--
		}
	case 'c':
		s.cells, s.main = s.blank(), nil
		s.moveTo(0, 0)
	}
}

// altScreen switches to a blank alternate screen and back.
func (s *Screen) altScreen(on bool) {
	switch {
	case on && s.main == nil:
		s.main, s.cells = s.cells, s.blank()
	case !on && s.main != nil:
		s.cells, s.main = s.main, nil
	}
}
