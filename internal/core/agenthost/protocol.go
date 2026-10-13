// Package agenthost runs each agent session in its own detached holder
// process (`bonsai __agent-host`), so a session outlives the API that started
// it. The holder owns the PTY, the bounded terminal stream and the session's
// lifecycle; the API, `bonsai agents` and, through the API, the browser are
// its clients over a per-session Unix socket.
package agenthost

import (
	"bufio"
	"encoding/json"
	"io"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agentterminal"
)

// Version is bumped when the holder protocol changes incompatibly. A holder
// keeps the version of the binary that started it, so clients refuse other
// versions instead of misreading them.
const Version = 1

// Connection modes a hello asks for.
const (
	// ModeTerminal attaches to the terminal stream: replay, live output and,
	// for the writer, input and resize.
	ModeTerminal = "terminal"
	// ModeStatus receives summaries only and never writes; the API monitors
	// sessions with it, and one-shot clients send info or stop on it.
	ModeStatus = "status"
)

// Request is one client message. The first one on a connection is a hello.
type Request struct {
	// Type is hello, input, resize, take, stop or info.
	Type    string `json:"type"`
	Version int    `json:"version,omitempty"`
	// Hello.
	Mode       string `json:"mode,omitempty"`
	Offset     uint64 `json:"offset,omitempty"`
	Generation string `json:"generation,omitempty"`
	Take       bool   `json:"take,omitempty"`
	// ReadOnly observes only: never the writer, cannot take control.
	ReadOnly bool `json:"read_only,omitempty"`
	// Client labels the connection in info: web or cli.
	Client string `json:"client,omitempty"`
	// Input.
	Data []byte `json:"data,omitempty"`
	// Resize.
	Cols int `json:"cols,omitempty"`
	Rows int `json:"rows,omitempty"`
	// Stop: an error to record on the session, such as a vanished worktree.
	Error string `json:"error,omitempty"`
}

// Frame is one holder message: ready (answering hello), the terminal stream's
// output, gap, status and writer frames, info, or error.
type Frame struct {
	Type       string                 `json:"type"`
	Version    int                    `json:"version,omitempty"`
	Generation string                 `json:"generation,omitempty"`
	Offset     uint64                 `json:"offset,omitempty"`
	Data       []byte                 `json:"data,omitempty"`
	Status     *agentterminal.Summary `json:"status,omitempty"`
	Writer     *bool                  `json:"writer,omitempty"`
	Info       *Info                  `json:"info,omitempty"`
	Error      string                 `json:"error,omitempty"`
}

// Info describes a running holder.
type Info struct {
	Summary      agentterminal.Summary        `json:"summary"`
	PID          int                          `json:"pid"`
	Clients      []agentterminal.StreamClient `json:"clients"`
	LastOutputAt *time.Time                   `json:"last_output_at,omitempty"`
	Cols         int                          `json:"cols"`
	Rows         int                          `json:"rows"`
}

func streamFrame(f agentterminal.Frame) Frame {
	return Frame{Type: f.Type, Offset: f.Offset, Data: f.Data, Status: f.Status, Writer: f.Writer}
}

// TerminalFrame is f as the terminal stream frame it carries.
func (f Frame) TerminalFrame() agentterminal.Frame {
	return agentterminal.Frame{Type: f.Type, Offset: f.Offset, Data: f.Data, Status: f.Status, Writer: f.Writer}
}

// Messages are newline-delimited JSON. Lines are bounded so a peer cannot
// make the other side buffer without limit; output frames carry at most 16
// KiB and a replay at most the 2 MiB stream (base64 grows it by a third).
const maxLine = 4 << 20

type decoder struct{ r *bufio.Reader }

func newDecoder(r io.Reader) *decoder { return &decoder{bufio.NewReaderSize(r, 64<<10)} }

func (d *decoder) decode(v any) error {
	var line []byte
	for {
		chunk, prefix, err := d.r.ReadLine()
		if err != nil {
			return err
		}
		line = append(line, chunk...)
		if len(line) > maxLine {
			return errLineTooLong
		}
		if !prefix {
			break
		}
	}
	return json.Unmarshal(line, v)
}

func encode(w io.Writer, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = w.Write(append(b, '\n'))
	return err
}
