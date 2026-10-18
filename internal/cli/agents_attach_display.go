package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/ui/components/vterm"
)

// Agent escape sequences run inside an emulator so screen clears, alternate
// screens and scroll regions cannot erase Bonsai's controls or confirmation.
type attachDisplay struct {
	term       *vterm.Model
	out        io.Writer
	cols, rows int
	status     string
	readOnly   bool
	writer     bool
	confirm    bool
	dirty      bool
}

func newAttachDisplay(out io.Writer, cols, rows int, readOnly, writer bool) *attachDisplay {
	d := &attachDisplay{out: out, cols: cols, rows: rows, readOnly: readOnly, writer: writer, dirty: true}
	d.term = vterm.New(cols, d.bodyRows())
	d.term.SetReplaying(true)
	d.term.SetFocused(writer)
	_, _ = io.WriteString(out, "\x1b[?1049h\x1b[?25l\x1b[?2004h")
	return d
}

func (d *attachDisplay) controls() []string {
	lines := []string{"Ctrl+] d: detach (keep running)", "Ctrl+] k: kill (confirm)"}
	if !d.readOnly {
		lines = append(lines, "Ctrl+] t: take control")
	}
	joined := strings.Join(lines, " · ")
	if ansi.StringWidth(joined) <= d.cols {
		return []string{joined}
	}
	return lines
}

func (d *attachDisplay) bodyRows() int { return max(d.rows-1-len(d.controls()), 1) }

func (d *attachDisplay) resize(cols, rows int) {
	d.cols, d.rows, d.dirty = cols, rows, true
	d.term.SetViewport(cols, d.bodyRows())
	if d.writer {
		d.term.ResizeEmulator(cols, d.bodyRows())
	}
}

func (d *attachDisplay) render() {
	if !d.dirty {
		return
	}
	d.dirty = false
	state := "READ-ONLY"
	if d.writer {
		state = "CONTROL"
	}
	status := state + " · " + d.status
	if d.confirm {
		status = "Kill agent? [y/N] · Esc cancels"
	}
	lines := strings.Split(d.term.View(), "\n")
	lines = append(lines, "\x1b[0;7m"+attachLine(status, d.cols)+"\x1b[0m")
	for _, line := range d.controls() {
		lines = append(lines, attachLine(line, d.cols))
	}
	// Avoid a trailing newline: writing past the last row would scroll the
	// terminal and move the controls away from their reserved position.
	_, _ = fmt.Fprint(d.out, "\x1b[H"+strings.Join(lines[:min(len(lines), d.rows)], "\r\n"))
}

func attachLine(s string, cols int) string {
	s = ansi.Truncate(s, cols, "")
	return s + strings.Repeat(" ", max(cols-ansi.StringWidth(s), 0))
}

func (d *attachDisplay) close() {
	d.term.Close()
	_, _ = io.WriteString(d.out, "\x1b[?2004l\x1b[0m\x1b[?25h\x1b[?1049l")
}

func attachEvents(in io.Reader) (<-chan uv.Event, func(), error) {
	reader, err := uv.NewCancelReader(in)
	if err != nil {
		return nil, nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	events := make(chan uv.Event)
	go func() {
		defer close(events)
		_ = uv.NewTerminalReader(reader, os.Getenv("TERM")).StreamEvents(ctx, events)
	}()
	return events, func() {
		cancel()
		reader.Cancel()
		// TerminalReader flushes decoded events before it exits, so drain them
		// even after attach has stopped consuming keyboard input.
		for range events {
		}
		_ = reader.Close()
	}, nil
}
