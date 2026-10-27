package vterm

import (
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	uv "github.com/charmbracelet/ultraviolet"
)

const (
	chunkWait = 2 * time.Second
	quietWait = 50 * time.Millisecond
	// inputCap mirrors inputBuf; a test that pins the cap guards the bound on
	// memory held for a slow or absent reader.
	inputCap = 64
)

// readChunk returns the next chunk on m.Input(), failing after chunkWait.
func readChunk(t *testing.T, m *Model) []byte {
	t.Helper()
	select {
	case c, ok := <-m.Input():
		if !ok {
			t.Fatal("Input() closed while waiting for a chunk")
		}
		return c
	case <-time.After(chunkWait):
		t.Fatal("timed out waiting for an Input() chunk")
	}
	return nil
}

// expectNothing fails if any chunk arrives within quietWait.
func expectNothing(t *testing.T, m *Model) {
	t.Helper()
	select {
	case c, ok := <-m.Input():
		if !ok {
			t.Fatal("Input() closed unexpectedly")
		}
		t.Fatalf("unexpected Input() chunk %q", c)
	case <-time.After(quietWait):
	}
}

// collectUntil concatenates chunks until the accumulated bytes end with end.
// Bracketed paste is written in several pieces by the emulator.
func collectUntil(t *testing.T, m *Model, end string) string {
	t.Helper()
	var b strings.Builder
	deadline := time.After(chunkWait)
	for !strings.HasSuffix(b.String(), end) {
		select {
		case c, ok := <-m.Input():
			if !ok {
				t.Fatalf("Input() closed; got %q, want suffix %q", b.String(), end)
			}
			b.Write(c)
		case <-deadline:
			t.Fatalf("timed out; got %q, want suffix %q", b.String(), end)
		}
	}
	return b.String()
}

func enableMode(t *testing.T, m *Model, seq string) {
	t.Helper()
	m.Write([]byte(seq))
}

func TestTerminalEventsFollowAgentInputModes(t *testing.T) {
	m := newTerm(t, 80, 24, 80, 24)
	m.Write([]byte("\x1b[?1h\x1b[?2004h"))
	m.SendEvent(uv.KeyPressEvent{Code: uv.KeyUp})
	if got := string(readChunk(t, m)); got != "\x1bOA" {
		t.Fatalf("application cursor key = %q", got)
	}
	m.SendEvent(uv.PasteEvent{Content: "hello\nworld"})
	if got := collectUntil(t, m, "\x1b[201~"); got != "\x1b[200~hello\nworld\x1b[201~" {
		t.Fatalf("bracketed paste = %q", got)
	}
	m.SendEvent(uv.KeyPressEvent{Code: 'c', Mod: uv.ModCtrl})
	if got := string(readChunk(t, m)); got != "\x03" {
		t.Fatalf("Ctrl-C = %q", got)
	}
}

func TestSendKeyEncoding(t *testing.T) {
	t.Parallel()
	key := func(typ tea.KeyType) tea.KeyMsg { return tea.KeyMsg{Type: typ} }
	runes := func(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }
	alt := func(k tea.KeyMsg) tea.KeyMsg { k.Alt = true; return k }

	tests := []struct {
		name   string
		key    tea.KeyMsg
		normal string
		app    string // DECCKM (mode 1) encoding; "" means same as normal
	}{
		{"up", key(tea.KeyUp), "\x1b[A", "\x1bOA"},
		{"down", key(tea.KeyDown), "\x1b[B", "\x1bOB"},
		{"right", key(tea.KeyRight), "\x1b[C", "\x1bOC"},
		{"left", key(tea.KeyLeft), "\x1b[D", "\x1bOD"},
		{"enter", key(tea.KeyEnter), "\r", ""},
		{"tab", key(tea.KeyTab), "\t", ""},
		{"shift+tab", key(tea.KeyShiftTab), "\x1b[Z", ""},
		{"backspace", key(tea.KeyBackspace), "\x7f", ""},
		{"delete", key(tea.KeyDelete), "\x1b[3~", ""},
		{"esc", key(tea.KeyEsc), "\x1b", ""},
		// x/vt always sends CSI H/F for Home/End; xterm uses SS3 in DECCKM.
		// Pinned as-is, see the report note.
		{"home", key(tea.KeyHome), "\x1b[H", "\x1b[H"},
		{"end", key(tea.KeyEnd), "\x1b[F", "\x1b[F"},
		{"pgup", key(tea.KeyPgUp), "\x1b[5~", ""},
		{"pgdn", key(tea.KeyPgDown), "\x1b[6~", ""},
		{"insert", key(tea.KeyInsert), "\x1b[2~", ""},
		{"F1", key(tea.KeyF1), "\x1bOP", ""},
		{"F5", key(tea.KeyF5), "\x1b[15~", ""},
		{"F12", key(tea.KeyF12), "\x1b[24~", ""},
		{"ctrl+a", key(tea.KeyCtrlA), "\x01", ""},
		{"ctrl+z", key(tea.KeyCtrlZ), "\x1a", ""},
		{"ctrl+space", key(tea.KeyCtrlAt), "\x00", ""},
		{"space", key(tea.KeySpace), " ", ""},
		{"rune a", runes("a"), "a", ""},
		{"rune A", runes("A"), "A", ""},
		{"ime latin", runes("çã"), "çã", ""},
		{"ime cjk", runes("日本"), "日本", ""},
		{"alt+x", alt(runes("x")), "\x1bx", ""},
		{"alt+up", alt(key(tea.KeyUp)), "\x1b\x1b[A", "\x1b\x1bOA"},
		{"ctrl+up", key(tea.KeyCtrlUp), "\x1b[1;5A", ""},
		{"shift+up", key(tea.KeyShiftUp), "\x1b[1;2A", ""},
		{"ctrl+shift+up", key(tea.KeyCtrlShiftUp), "\x1b[1;6A", ""},
		{"alt+shift+up", alt(key(tea.KeyShiftUp)), "\x1b[1;4A", ""},
		{"ctrl+pgup", key(tea.KeyCtrlPgUp), "\x1b[5;5~", ""},
	}

	modes := []struct {
		name string
		seq  string // written to the emulator before the key
		app  bool
	}{
		{"normal", "", false},
		{"app cursor", "\x1b[?1h", true},
	}
	for _, mode := range modes {
		t.Run(mode.name, func(t *testing.T) {
			t.Parallel()
			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					t.Parallel()
					m := newTerm(t, 80, 24, 80, 24)
					enableMode(t, m, mode.seq)
					want := tt.normal
					if mode.app && tt.app != "" {
						want = tt.app
					}
					if !m.SendKey(tt.key) {
						t.Fatalf("SendKey(%v) = false, want true", tt.key)
					}
					if got := string(readChunk(t, m)); got != want {
						t.Errorf("SendKey(%v) = %q, want %q", tt.key, got, want)
					}
				})
			}
		})
	}
}

func TestPaste(t *testing.T) {
	t.Parallel()
	const text = "hello world"
	paste := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(text), Paste: true}

	t.Run("without mode 2004 is raw", func(t *testing.T) {
		t.Parallel()
		m := newTerm(t, 80, 24, 80, 24)
		if !m.SendKey(paste) {
			t.Fatal("SendKey(paste) = false, want true")
		}
		if got := string(readChunk(t, m)); got != text {
			t.Errorf("SendKey(paste) = %q, want %q", got, text)
		}
		m.SendPaste(text)
		if got := string(readChunk(t, m)); got != text {
			t.Errorf("SendPaste = %q, want %q", got, text)
		}
	})

	t.Run("with mode 2004 is bracketed", func(t *testing.T) {
		t.Parallel()
		const want = "\x1b[200~" + text + "\x1b[201~"
		m := newTerm(t, 80, 24, 80, 24)
		enableMode(t, m, "\x1b[?2004h")
		if !m.SendKey(paste) {
			t.Fatal("SendKey(paste) = false, want true")
		}
		if got := collectUntil(t, m, "\x1b[201~"); got != want {
			t.Errorf("SendKey(paste) = %q, want %q", got, want)
		}
		m.SendPaste(text)
		if got := collectUntil(t, m, "\x1b[201~"); got != want {
			t.Errorf("SendPaste = %q, want %q", got, want)
		}
	})
}

func TestSendKeyRejectsUnencodable(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		key  tea.KeyMsg
	}{
		{"ctrl+] is the TUI prefix", tea.KeyMsg{Type: tea.KeyCtrlCloseBracket}},
		{"unknown key type", tea.KeyMsg{Type: tea.KeyType(-9999)}},
		{"F13 has no encoding", tea.KeyMsg{Type: tea.KeyF13}},
		{"F20 has no encoding", tea.KeyMsg{Type: tea.KeyF20}},
		{"empty runes", tea.KeyMsg{Type: tea.KeyRunes}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := newTerm(t, 80, 24, 80, 24)
			if m.SendKey(tt.key) {
				t.Errorf("SendKey(%v) = true, want false", tt.key)
			}
			expectNothing(t, m)
		})
	}
}

func TestWriteNeverBlocksWithoutReader(t *testing.T) {
	t.Parallel()
	m := newTerm(t, 80, 24, 80, 24)
	if c := cap(m.Input()); c != inputCap {
		t.Fatalf("cap(Input()) = %d, want %d", c, inputCap)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 1000 {
			m.Write([]byte("\x1b[6n"))
			if n := len(m.Input()); n > inputCap {
				t.Errorf("len(Input()) = %d, exceeds cap %d", n, inputCap)
				return
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("1000 DSR writes with no reader did not complete")
	}
	if d := m.Dropped(); d == 0 {
		t.Error("Dropped() = 0 after overflowing Input()")
	}
	if n := len(m.Input()); n > inputCap {
		t.Errorf("final len(Input()) = %d, exceeds cap %d", n, inputCap)
	}
}

func TestReplaySuppressesReplies(t *testing.T) {
	t.Parallel()
	replyRe := regexp.MustCompile(`^\x1b\[\d+;\d+R$`)

	t.Run("replayed query is dropped", func(t *testing.T) {
		t.Parallel()
		m := newTerm(t, 80, 24, 80, 24)
		m.SetReplaying(true)
		m.Write([]byte("\x1b[6n"))
		expectNothing(t, m)
		m.SetReplaying(false)
		m.Write([]byte("\x1b[6n"))
		if got := string(readChunk(t, m)); !replyRe.MatchString(got) {
			t.Errorf("live DSR reply = %q, want match %v", got, replyRe)
		}
	})

	t.Run("toggling replay never leaks a stale reply", func(t *testing.T) {
		t.Parallel()
		m := newTerm(t, 80, 24, 80, 24)
		for range 200 {
			m.SetReplaying(true)
			m.Write([]byte("\x1b[6n"))
			m.SetReplaying(false)
		}
		expectNothing(t, m)
		if d := m.Dropped(); d != 0 {
			t.Errorf("Dropped() = %d, want 0", d)
		}
	})
}

func TestFocusReports(t *testing.T) {
	t.Parallel()

	t.Run("mode 1004 reports transitions only", func(t *testing.T) {
		t.Parallel()
		m := newTerm(t, 80, 24, 80, 24)
		enableMode(t, m, "\x1b[?1004h")

		m.SetFocused(true)
		if got := string(readChunk(t, m)); got != "\x1b[I" {
			t.Errorf("focus in = %q, want ESC[I", got)
		}
		m.SetFocused(true)
		expectNothing(t, m)

		m.SetFocused(false)
		if got := string(readChunk(t, m)); got != "\x1b[O" {
			t.Errorf("focus out = %q, want ESC[O", got)
		}
		m.SetFocused(false)
		expectNothing(t, m)
	})

	t.Run("without mode 1004 nothing is sent", func(t *testing.T) {
		t.Parallel()
		m := newTerm(t, 80, 24, 80, 24)
		m.SetFocused(true)
		expectNothing(t, m)
	})
}

func TestCloseSemantics(t *testing.T) {
	t.Parallel()

	t.Run("blocked reader observes close", func(t *testing.T) {
		t.Parallel()
		m := New(80, 24)
		got := make(chan bool, 1)
		go func() {
			_, ok := <-m.Input()
			got <- ok
		}()
		start := time.Now()
		m.Close()
		if d := time.Since(start); d > chunkWait {
			t.Errorf("Close took %v, want < %v", d, chunkWait)
		}
		select {
		case ok := <-got:
			if ok {
				t.Error("reader received a value, want closed channel")
			}
		case <-time.After(chunkWait):
			t.Fatal("blocked reader was not released by Close")
		}
	})

	t.Run("no reader, double close, then use is inert", func(t *testing.T) {
		t.Parallel()
		m := New(80, 24)
		m.Write([]byte("\x1b[6n"))
		m.Close()
		m.Close()
		if m.SendKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")}) {
			t.Error("SendKey after Close = true, want false")
		}
		// The DSR reply queued before Close may still be buffered; a closed
		// channel yields buffered values first, so drain them before expecting
		// the close.
		deadline := time.After(chunkWait)
		for {
			select {
			case _, ok := <-m.Input():
				if !ok {
					return
				}
			case <-deadline:
				t.Fatal("Input() not closed after Close")
			}
		}
	})
}

func TestCloseDoesNotLeakGoroutines(t *testing.T) {
	// Not parallel: NumGoroutine is process-wide.
	before := runtime.NumGoroutine()
	m := New(80, 24)
	m.SetFocused(true)
	enableMode(t, m, "\x1b[?1004h\x1b[?2004h")
	m.Write([]byte("\x1b[6n"))
	m.SendKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	m.Close()

	deadline := time.Now().Add(time.Second)
	for runtime.NumGoroutine() > before && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if after := runtime.NumGoroutine(); after > before {
		t.Errorf("goroutines before New = %d, after Close = %d", before, after)
	}
}
