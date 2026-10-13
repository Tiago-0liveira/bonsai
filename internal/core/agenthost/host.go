package agenthost

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/agentterminal"
	"github.com/Tiago-0liveira/bonsai/internal/version"
)

// Spec is the session a holder runs. It reaches the holder on stdin, so the
// prompt never lands on disk or in argv.
type Spec struct {
	// Summary is the session as the API created it, in state starting.
	Summary agentterminal.Summary `json:"summary"`
	WorkDir string                `json:"work_dir"`
	Cols    int                   `json:"cols"`
	Rows    int                   `json:"rows"`
	Launch  agents.LaunchOptions  `json:"launch"`
}

// Deps are the stores and providers a holder runs its session with.
type Deps struct {
	Accounts agents.AccountStore
	Sessions agents.SessionStore
	Registry *agents.Registry
	Paths    Paths
	// Linger is how long an ended holder waits for its clients to read the
	// final status; zero means ten seconds.
	Linger time.Duration
}

// replayChunk bounds one replayed output frame.
const replayChunk = 64 << 10

type host struct {
	deps       Deps
	generation string
	stream     *agentterminal.Stream[agentterminal.Summary]
	workDir    string
	cancel     context.CancelFunc

	mu       sync.Mutex
	summary  agentterminal.Summary
	monitors map[*serverConn]struct{}
	conns    map[*serverConn]struct{}
	ended    chan struct{}
}

// Serve runs spec's session to its end, serving clients on the session's
// socket. ready is called once: with nil when the socket accepts clients, or
// with the error that prevented it. Canceling ctx stops the session (SIGTERM
// reaches the holder that way); Serve returns after the session ended and
// clients had Linger to read its final status.
func Serve(ctx context.Context, spec Spec, deps Deps, ready func(error)) error {
	err := serve(ctx, spec, deps, ready)
	if ready != nil && err != nil {
		ready(err)
	}
	return err
}

func serve(ctx context.Context, spec Spec, deps Deps, ready func(error)) error {
	id := spec.Summary.ID
	if !validID(id) {
		return fmt.Errorf("invalid agent session id")
	}
	if !agentterminal.Supported {
		return fmt.Errorf("interactive terminals unsupported on this platform")
	}
	if !agentterminal.Dimensions(spec.Cols, spec.Rows) {
		return fmt.Errorf("terminal dimensions must be between 1 and 500")
	}
	if err := deps.Paths.ensure(); err != nil {
		return err
	}
	lock, err := lockFile(deps.Paths.lock(id))
	if err != nil {
		return fmt.Errorf("agent session %s already has a host", id)
	}
	defer lock.Close()
	_ = Prune(deps.Paths)
	account, err := deps.Accounts.Get(spec.Summary.AccountID)
	if err != nil {
		return fmt.Errorf("profile unavailable")
	}
	provider, err := deps.Registry.Get(account.Provider)
	if err != nil {
		return fmt.Errorf("provider not available")
	}
	runtime, err := deps.Sessions.Get(agents.SessionID(id))
	if err != nil {
		return fmt.Errorf("session unavailable")
	}
	socket := deps.Paths.socket(id)
	// The lock is ours, so a socket left here belongs to a dead holder.
	_ = os.Remove(socket)
	listener, err := net.Listen("unix", socket)
	if err != nil {
		return err
	}
	defer os.Remove(socket)
	defer listener.Close()
	if err := os.Chmod(socket, 0o600); err != nil {
		return err
	}
	var b [8]byte
	_, _ = rand.Read(b[:])
	sessionCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h := &host{deps: deps, generation: hex.EncodeToString(b[:]), stream: agentterminal.NewStream[agentterminal.Summary](spec.Cols, spec.Rows), workDir: spec.WorkDir, cancel: cancel, summary: spec.Summary, monitors: map[*serverConn]struct{}{}, conns: map[*serverConn]struct{}{}, ended: make(chan struct{})}
	if err := h.writeRecord(); err != nil {
		return err
	}
	if ready != nil {
		ready(nil)
	}
	ready = nil
	go h.accept(listener)
	stopped := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			h.stop("")
		case <-stopped:
		}
	}()
	agentterminal.RunSession(sessionCtx, h.stream, provider, account, runtime, deps.Sessions, spec.Cols, spec.Rows, spec.Launch, h.update)
	close(stopped)
	close(h.ended)
	h.linger()
	return nil
}

// update applies one summary change and publishes the result to terminal
// clients, monitors and the record.
func (h *host) update(change func(*agentterminal.Summary) error) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := change(&h.summary); err != nil {
		return err
	}
	h.publish()
	return nil
}

// publish sends the summary everywhere. Callers hold h.mu.
func (h *host) publish() {
	h.stream.Status(h.summary)
	summary := h.summary
	frame := Frame{Type: "status", Offset: h.stream.Offset(), Status: &summary}
	for c := range h.monitors {
		c.queue(frame)
	}
	_ = h.writeRecord()
}

// writeRecord saves the holder's record. Callers hold h.mu, or own h alone.
func (h *host) writeRecord() error {
	r := Record{Version: RecordVersion, ID: h.summary.ID, PID: os.Getpid(), Socket: h.deps.Paths.socket(h.summary.ID), Generation: h.generation, BinaryVersion: version.String(), Protocol: Version, WorkDir: h.workDir, Summary: h.summary}
	if at := h.stream.LastOutput(); !at.IsZero() {
		r.LastOutputAt = &at
	}
	return writeRecord(h.deps.Paths, r)
}

// stop ends the session: SIGTERM to its process group, SIGKILL two seconds
// later. reason, when set, is recorded as the session's error.
func (h *host) stop(reason string) {
	h.mu.Lock()
	if h.summary.Active() {
		if reason != "" {
			h.summary.Error = reason
		}
		if h.summary.State != "stopping" {
			h.summary.State = "stopping"
			h.publish()
		}
	}
	h.mu.Unlock()
	h.cancel()
}

func (h *host) info() Info {
	h.mu.Lock()
	summary := h.summary
	h.mu.Unlock()
	cols, rows := h.stream.Size()
	info := Info{Summary: summary, PID: os.Getpid(), Clients: h.stream.Clients(), Cols: cols, Rows: rows}
	if at := h.stream.LastOutput(); !at.IsZero() {
		info.LastOutputAt = &at
	}
	return info
}

// linger gives clients time to read the final status, then closes them.
func (h *host) linger() {
	wait := h.deps.Linger
	if wait == 0 {
		wait = 10 * time.Second
	}
	deadline := time.After(wait)
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		h.mu.Lock()
		clients := len(h.conns)
		h.mu.Unlock()
		if clients == 0 {
			break
		}
		select {
		case <-deadline:
		case <-tick.C:
			continue
		}
		break
	}
	h.stream.DetachAll()
	h.mu.Lock()
	for c := range h.conns {
		_ = c.conn.Close()
	}
	h.mu.Unlock()
}

func (h *host) accept(listener net.Listener) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		go h.handle(conn)
	}
}

// serverConn is one client connection. Frames reach it in order through out,
// written by one goroutine, so a slow client never blocks the stream.
type serverConn struct {
	conn net.Conn
	out  chan Frame
	once sync.Once
	gone chan struct{}
}

func (c *serverConn) queue(f Frame) {
	select {
	case c.out <- f:
	case <-c.gone:
	default:
		// Too slow: drop the client rather than the holder's pace.
		c.close()
	}
}

func (c *serverConn) close() {
	c.once.Do(func() {
		close(c.gone)
		_ = c.conn.Close()
	})
}

func (c *serverConn) writeLoop() {
	for {
		select {
		case <-c.gone:
			return
		case f := <-c.out:
			_ = c.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if encode(c.conn, f) != nil {
				c.close()
				return
			}
		}
	}
}

func (h *host) handle(conn net.Conn) {
	c := &serverConn{conn: conn, out: make(chan Frame, 256), gone: make(chan struct{})}
	defer c.close()
	h.mu.Lock()
	h.conns[c] = struct{}{}
	h.mu.Unlock()
	defer func() {
		h.mu.Lock()
		delete(h.conns, c)
		delete(h.monitors, c)
		h.mu.Unlock()
	}()
	dec := newDecoder(conn)
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	var hello Request
	if err := dec.decode(&hello); err != nil || hello.Type != "hello" {
		return
	}
	_ = conn.SetReadDeadline(time.Time{})
	go c.writeLoop()
	if hello.Version != Version {
		c.queue(Frame{Type: "error", Version: Version, Error: fmt.Sprintf("agent host speaks protocol %d, client %d", Version, hello.Version)})
		time.Sleep(100 * time.Millisecond)
		return
	}
	var subscriber uint64
	switch hello.Mode {
	case ModeStatus:
		h.mu.Lock()
		summary := h.summary
		h.monitors[c] = struct{}{}
		no := false
		c.queue(Frame{Type: "ready", Version: Version, Generation: h.generation, Writer: &no, Status: &summary})
		h.mu.Unlock()
	case ModeTerminal:
		offset := hello.Offset
		if hello.Generation != h.generation {
			offset = 0
		}
		h.mu.Lock()
		id, writer, replay, frames := h.stream.AttachAs(offset, h.summary, hello.Client, hello.Take)
		c.queue(Frame{Type: "ready", Version: Version, Generation: h.generation, Writer: &writer})
		for _, f := range replay {
			for f.Type == "output" && len(f.Data) > replayChunk {
				head := f
				head.Data = f.Data[:replayChunk]
				c.queue(streamFrame(head))
				f.Offset += replayChunk
				f.Data = f.Data[replayChunk:]
			}
			c.queue(streamFrame(f))
		}
		h.mu.Unlock()
		subscriber = id
		defer h.stream.Detach(subscriber)
		go func() {
			for {
				select {
				case f, ok := <-frames:
					if !ok {
						c.close()
						return
					}
					c.queue(streamFrame(f))
				case <-c.gone:
					return
				}
			}
		}()
	default:
		c.queue(Frame{Type: "error", Error: "unknown mode"})
		return
	}
	for {
		var r Request
		if dec.decode(&r) != nil {
			return
		}
		switch r.Type {
		case "input":
			if subscriber == 0 || len(r.Data) > agentterminal.InputLimit {
				c.queue(Frame{Type: "error", Error: "terminal input unavailable"})
				continue
			}
			if err := h.stream.Input(subscriber, r.Data); err != nil && !errors.Is(err, agentterminal.ErrNoProcess) {
				c.queue(Frame{Type: "error", Error: "terminal input unavailable"})
			}
		case "resize":
			if subscriber == 0 || h.stream.Resize(subscriber, r.Cols, r.Rows) != nil {
				c.queue(Frame{Type: "error", Error: "terminal resize unavailable"})
			}
		case "take":
			if subscriber == 0 || h.stream.Take(subscriber) != nil {
				c.queue(Frame{Type: "error", Error: "only terminal clients can take control"})
			}
		case "stop":
			h.stop(r.Error)
		case "info":
			info := h.info()
			c.queue(Frame{Type: "info", Info: &info})
		default:
			c.queue(Frame{Type: "error", Error: "unknown request"})
		}
	}
}
