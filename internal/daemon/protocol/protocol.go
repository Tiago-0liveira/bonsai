// Package protocol defines the wire format between bonsai clients (TUI, CLI) and
// the per-repo daemon. Frames are newline-delimited JSON: a client writes one
// Request; the daemon writes one or more Responses (log streams emit many frames
// terminated by one with EOF set). The daemon's on-disk Record type is reused
// verbatim so there is a single source of truth for process shape.
package protocol

import (
	"encoding/json"
	"github.com/Tiago-0liveira/bonsai/internal/daemon/gitbridge"
	"io"

	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
)

// Version is bumped when the wire format changes incompatibly. Ping returns it so
// a client can detect a daemon left over from an older bonsai build.
const Version = 8

// Request kinds.
const (
	KindGit           = "git"
	KindSpawn         = "spawn"
	KindList          = "list"
	KindKill          = "kill"
	KindRestart       = "restart"
	KindSetPolicy     = "setPolicy"
	KindLogs          = "logs"
	KindProcessStream = "processStream"
	KindAttach        = "attach"
	KindRemove        = "remove"
	KindServeStart    = "serveStart"
	KindServeStatus   = "serveStatus"
	KindServeStop     = "serveStop"
	KindServeRestart  = "serveRestart"
	KindServeLogs     = "serveLogs"
	KindPing          = "ping"
	KindShutdown      = "shutdown"
)

// Request is a single client command.
type Request struct {
	Git  *gitbridge.Command `json:"git,omitempty"`
	Kind string             `json:"kind"`

	// Spawn.
	Worktree      string            `json:"worktree,omitempty"`
	Branch        string            `json:"branch,omitempty"`
	Label         string            `json:"label,omitempty"`
	Command       string            `json:"command,omitempty"`
	Program       string            `json:"program,omitempty"`
	Args          []string          `json:"args,omitempty"`
	WorkingDir    string            `json:"working_dir,omitempty"`
	Environment   map[string]string `json:"environment,omitempty"`
	ExpectedPort  int               `json:"expected_port,omitempty"`
	ServeGroup    string            `json:"serve_group,omitempty"`
	ServeName     string            `json:"serve_name,omitempty"`
	ServeRequired bool              `json:"serve_required,omitempty"`
	Policy        *procstore.Policy `json:"policy,omitempty"`

	// Serve-group lifecycle.
	ServeSpec   *procstore.ServeSpec `json:"serve,omitempty"`
	ProcessName string               `json:"process_name,omitempty"`

	// Target for kill/restart/setPolicy/logs.
	StopFirst bool   `json:"stop_first,omitempty"`
	ID        int    `json:"id,omitempty"`
	All       bool   `json:"all,omitempty"`       // kill: every process
	Worktree2 string `json:"worktree2,omitempty"` // kill: restrict to a worktree path

	// Logs.
	Follow          bool   `json:"follow,omitempty"`
	TailLines       int    `json:"tail_lines,omitempty"`
	Grep            string `json:"grep,omitempty"`
	GrepInsensitive bool   `json:"grep_i,omitempty"`

	Generation string `json:"generation,omitempty"`
	Offset     int64  `json:"offset,omitempty"`

	// Shutdown.
	Force bool `json:"force,omitempty"`
}

// Response is a single daemon reply frame.
type Response struct {
	Git   *gitbridge.Result `json:"git,omitempty"`
	OK    bool              `json:"ok"`
	Error string            `json:"error,omitempty"`

	Visibility *procstore.ProcessVisibility `json:"process_visibility,omitempty"`
	Record     *procstore.Record            `json:"record,omitempty"`  // spawn/restart
	Records    []*procstore.Record          `json:"records,omitempty"` // list
	Killed     []int                        `json:"killed,omitempty"`  // kill
	ServeGroup *procstore.ServeGroup        `json:"serve_group,omitempty"`

	// Ping.
	Version   int `json:"version,omitempty"`
	PID       int `json:"pid,omitempty"`
	ProcCount int `json:"proc_count,omitempty"`

	// Logs stream.
	LogChunk   string `json:"log_chunk,omitempty"`
	Data       []byte `json:"data,omitempty"`
	Offset     int64  `json:"offset"`
	Generation string `json:"generation,omitempty"`
	Gap        bool   `json:"gap,omitempty"`
	EOF        bool   `json:"eof,omitempty"` // final frame of a (possibly multi-frame) reply
}

// Encoder writes newline-delimited JSON frames.
type Encoder struct{ enc *json.Encoder }

// NewEncoder wraps w.
func NewEncoder(w io.Writer) *Encoder { return &Encoder{enc: json.NewEncoder(w)} }

// WriteRequest sends a request frame.
func (e *Encoder) WriteRequest(r *Request) error { return e.enc.Encode(r) }

// WriteResponse sends a response frame.
func (e *Encoder) WriteResponse(r *Response) error { return e.enc.Encode(r) }

// Decoder reads newline-delimited JSON frames from a stream.
type Decoder struct{ dec *json.Decoder }

// NewDecoder wraps r.
func NewDecoder(r io.Reader) *Decoder { return &Decoder{dec: json.NewDecoder(r)} }

// ReadRequest reads the next request frame.
func (d *Decoder) ReadRequest() (*Request, error) {
	var r Request
	if err := d.dec.Decode(&r); err != nil {
		return nil, err
	}
	return &r, nil
}

// ReadResponse reads the next response frame.
func (d *Decoder) ReadResponse() (*Response, error) {
	var r Response
	if err := d.dec.Decode(&r); err != nil {
		return nil, err
	}
	return &r, nil
}
