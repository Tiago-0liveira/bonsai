// Package vterm renders a raw agent PTY byte stream into a fixed rectangle
// using the charmbracelet/x/vt emulator. It is pure presentation: no sockets,
// no key routing.
package vterm

import (
	"io"
	"strings"
	"sync"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/vt"
)

const (
	sgrReset   = "\x1b[0m"
	maxEmuSize = 1000 // guards against absurd PTY sizes allocating huge screens
)

// Model hosts one emulator and renders it into a w x h pane. All methods are
// safe for concurrent use.
type Model struct {
	mu   sync.Mutex
	emu  *vt.Emulator
	pipe io.Closer

	w, h      int // pane rectangle
	focused   bool
	replaying bool
	title     string
	lines     []string
	dirty     bool
	closed    bool
	drainDone chan struct{}
	closeOnce sync.Once
}

// New creates a terminal emulating cols x rows. The pane starts at the same size.
func New(cols, rows int) *Model {
	cols, rows = clampEmu(cols), clampEmu(rows)
	m := &Model{
		emu:       vt.NewEmulator(cols, rows),
		w:         cols,
		h:         rows,
		dirty:     true,
		drainDone: make(chan struct{}),
	}
	// No scrollback UI exists; a small ring keeps scrolling cheap (the default
	// 10k-line buffer made full-screen redraw replays several times slower).
	m.emu.SetScrollbackSize(100)
	m.emu.SetCallbacks(vt.Callbacks{Title: func(s string) { m.title = s }})
	// Closing the pipe writer (not Emulator.Close, which flips an unsynchronized
	// flag that Read also reads) is what ends the drainer.
	m.pipe, _ = m.emu.InputPipe().(io.Closer)
	go m.drain()
	return m
}

// drain empties the emulator's input side. It is a synchronous io.Pipe: a
// query in the output stream (DSR, DA) blocks Write until someone reads the
// reply, so a reader must always exist even when the reply is discarded.
func (m *Model) drain() {
	defer close(m.drainDone)
	buf := make([]byte, 4096)
	for {
		// B2 routes these bytes (key encodings, replies) to an Input() channel.
		_, err := m.emu.Read(buf)
		if err != nil {
			return
		}
	}
}

// Write feeds PTY output to the emulator.
func (m *Model) Write(p []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return
	}
	_, _ = m.emu.Write(p)
	m.dirty = true
}

// SetViewport sets the pane rectangle that View fills.
func (m *Model) SetViewport(w, h int) {
	if w < 0 {
		w = 0
	}
	if h < 0 {
		h = 0
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if w != m.w || h != m.h {
		m.w, m.h, m.dirty = w, h, true
	}
}

// ResizeEmulator matches the emulator to the holder's PTY size. It never
// reflows: content stays where the agent drew it.
func (m *Model) ResizeEmulator(cols, rows int) {
	cols, rows = clampEmu(cols), clampEmu(rows)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || (cols == m.emu.Width() && rows == m.emu.Height()) {
		return
	}
	m.emu.Resize(cols, rows)
	m.dirty = true
}

// SetFocused shows the cursor only while focused.
func (m *Model) SetFocused(f bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.focused != f {
		m.focused, m.dirty = f, true
	}
}

// SetReplaying marks the stream as history replay. B1 only records it; B2 uses
// it to discard replies to long-gone queries.
func (m *Model) SetReplaying(r bool) {
	m.mu.Lock()
	m.replaying = r
	m.mu.Unlock()
}

// Title returns the last OSC 0/2 window title.
func (m *Model) Title() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.title
}

// AltScreen reports whether the alternate screen is active.
func (m *Model) AltScreen() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.emu.IsAltScreen()
}

// Close stops the input drainer and waits for it. Further writes are ignored.
func (m *Model) Close() {
	m.closeOnce.Do(func() {
		m.mu.Lock()
		m.closed = true
		m.mu.Unlock()
		if m.pipe != nil {
			_ = m.pipe.Close()
		}
		<-m.drainDone
	})
}

// View returns exactly h lines, each w cells wide and ending in an SGR reset so
// surrounding borders are never tinted.
func (m *Model) View() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.dirty || m.lines == nil {
		m.lines = m.render()
		m.dirty = false
	}
	return strings.Join(m.lines, "\n")
}

func (m *Model) render() []string {
	out := make([]string, m.h)
	blank := strings.Repeat(" ", m.w) + sgrReset
	cols, rows := m.emu.Width(), m.emu.Height()

	// Observer crop: bottom-aligned (agents keep their status line at the
	// bottom), shifted up only when the cursor row would be cut off.
	top := 0
	if rows > m.h {
		top = rows - m.h
		if cy := m.emu.CursorPosition().Y; cy < top {
			top = cy
		}
	}
	cur := uv.Position{X: -1, Y: -1}
	if m.focused && !m.emu.CursorHidden() {
		cur = m.emu.CursorPosition()
	}
	for y := range out {
		if sy := top + y; sy < rows {
			out[y] = m.renderRow(sy, min(cols, m.w), m.w, cur)
		} else {
			out[y] = blank
		}
	}
	return out
}

// renderRow builds one line from cells rather than Render(): Render drops
// trailing blanks, cannot crop or overlay a cursor, and does not pad, so every
// line would need re-measuring anyway.
func (m *Model) renderRow(y, limit, width int, cur uv.Position) string {
	var b strings.Builder
	var pen uv.Style
	used := 0
	for x := 0; x < limit; {
		c := m.emu.CellAt(x, y)
		if c == nil || (c.Width == 0 && c.Content == "") {
			x++ // continuation cell of a wide grapheme, or a hole
			continue
		}
		cw := max(c.Width, 1)
		content, style := c.Content, c.Style
		if content == "" {
			content = " "
		}
		if x+cw > limit { // wide glyph cut by the pane edge
			content, cw = " ", 1
		}
		if y == cur.Y && x == cur.X {
			style.Attrs |= uv.AttrReverse
		}
		switch {
		case style.IsZero() && !pen.IsZero():
			b.WriteString(sgrReset)
			pen = uv.Style{}
		case !style.Equal(&pen):
			b.WriteString(style.Diff(&pen))
			pen = style
		}
		b.WriteString(content)
		used += cw
		x += max(c.Width, 1)
	}
	if !pen.IsZero() {
		b.WriteString(sgrReset)
	}
	if used < width {
		b.WriteString(strings.Repeat(" ", width-used))
	}
	// The reset is always last so a styled border around the pane is not tinted.
	b.WriteString(sgrReset)
	return b.String()
}

func clampEmu(n int) int {
	return max(1, min(n, maxEmuSize))
}
