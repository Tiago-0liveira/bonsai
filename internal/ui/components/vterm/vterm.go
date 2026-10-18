// Package vterm renders a raw agent PTY byte stream into a fixed rectangle
// using the charmbracelet/x/vt emulator. It also turns keys, pastes, focus
// changes and the emulator's own replies into bytes on Input(). It owns no
// sockets and no key routing: the caller decides who may write to the agent.
package vterm

import (
	"io"
	"strings"
	"sync"
	"sync/atomic"

	tea "github.com/charmbracelet/bubbletea"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/vt"
)

const (
	sgrReset   = "\x1b[0m"
	maxEmuSize = 1000 // guards against absurd PTY sizes allocating huge screens
	inputBuf   = 64   // Input() capacity; the oldest chunk is dropped beyond it
)

// Model hosts one emulator and renders it into a w x h pane. All methods are
// safe for concurrent use.
type Model struct {
	mu   sync.Mutex
	emu  *vt.Emulator
	pipe io.Closer

	w, h      int // pane rectangle
	focused   bool
	replaying atomic.Bool // read by drain without mu
	title     string
	lines     []string
	dirty     bool
	closed    bool
	drainDone chan struct{}
	input     chan []byte
	dropped   atomic.Uint64
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
		input:     make(chan []byte, inputBuf),
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
// reply, so a reader must always exist. It is the only sender on m.input, never
// blocks on it and never takes m.mu (Write holds m.mu while waiting on it).
func (m *Model) drain() {
	defer close(m.drainDone)
	defer close(m.input)
	buf := make([]byte, 4096)
	for {
		n, err := m.emu.Read(buf)
		if err != nil {
			return
		}
		// Replies to queries from replayed history are for long-gone requests.
		if n == 0 || m.replaying.Load() {
			continue
		}
		m.enqueue(append([]byte(nil), buf[:n]...))
	}
}

func (m *Model) enqueue(chunk []byte) {
	for {
		select {
		case m.input <- chunk:
			return
		default:
		}
		select {
		case <-m.input:
			m.dropped.Add(1)
		default:
		}
	}
}

// Input yields the bytes the terminal wants sent to the agent: encoded keys,
// pastes, focus reports and replies to emulator queries. The caller (the TUI
// session layer) forwards each chunk to the holder only when it is the writer,
// and drops it otherwise. The channel closes after Close. A consumer that
// stalls loses the oldest chunks (see Dropped), so drain it promptly.
func (m *Model) Input() <-chan []byte { return m.input }

// Dropped counts chunks discarded because Input() was full.
func (m *Model) Dropped() uint64 { return m.dropped.Load() }

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

// SetFocused shows the cursor only while focused and reports focus changes to
// apps that enabled mode 1004. Mouse is owned by the TUI (cell motion) and is
// not forwarded.
func (m *Model) SetFocused(f bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.focused == f {
		return
	}
	m.focused, m.dirty = f, true
	if m.closed {
		return
	}
	if f {
		m.emu.Focus()
	} else {
		m.emu.Blur()
	}
}

// SetReplaying marks the stream as history replay: everything the emulator
// would emit meanwhile (query replies, and keys sent during the replay) is
// discarded instead of reaching Input().
func (m *Model) SetReplaying(r bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !r && m.replaying.Load() && !m.closed {
		// A Write during replay returns once drain's Read took the reply bytes,
		// but drain checks the flag afterwards and could see false and queue a
		// stale reply. A zero-length pipe write is delivered as an empty Read and
		// returns only when drain has finished the previous iteration.
		m.emu.SendText("")
	}
	m.replaying.Store(r)
}

// SendKey encodes k for the agent onto Input(). It reports false when the key
// has no encoding (or is the TUI's prefix key) or the model is closed.
func (m *Model) SendKey(k tea.KeyMsg) bool {
	if k.Paste {
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.closed {
			return false
		}
		m.emu.Paste(string(k.Runes))
		return true
	}
	ev, raw, ok := toKeyEvent(k)
	if !ok {
		return false
	}
	// emu.SendKey reads the mode map that Write mutates under mu. Holding mu
	// cannot deadlock: drain never takes it and always reads the pipe.
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return false
	}
	if raw != "" {
		m.emu.SendText(raw)
	} else {
		m.emu.SendKey(ev)
	}
	return true
}

// SendPaste sends s, bracketed only if the app enabled mode 2004.
func (m *Model) SendPaste(s string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.closed {
		m.emu.Paste(s)
	}
}

// SendEvent encodes a decoded terminal key or paste using the agent's current
// modes. The CLI uses this when hosting the emulator in a real terminal.
func (m *Model) SendEvent(event uv.Event) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return
	}
	switch event := event.(type) {
	case uv.KeyPressEvent:
		m.emu.SendKey(event)
	case uv.PasteEvent:
		m.emu.Paste(event.Content)
	}
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
