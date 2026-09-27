package client

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"

	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
	corepty "github.com/Tiago-0liveira/bonsai/internal/core/pty"
	"github.com/Tiago-0liveira/bonsai/internal/daemon/protocol"
)

// PTYEvent is one raw terminal output or lifecycle event.
type PTYEvent struct {
	Kind     string
	Seq      uint64
	Data     []byte
	ExitCode int
	Error    string
}

// PTYInfo is immutable metadata returned by the PTY attach handshake.
type PTYInfo struct {
	ID      int
	Cols    int
	Rows    int
	NextSeq uint64
}

// PTYAttachment is a transport-neutral full-duplex terminal attachment.
type PTYAttachment interface {
	Info() PTYInfo
	Events() <-chan PTYEvent
	Write([]byte) error
	Resize(cols, rows int) error
	Close() error
}

type ptyAttachment struct {
	conn   net.Conn
	enc    *protocol.Encoder
	dec    *protocol.Decoder
	events chan PTYEvent
	info   PTYInfo

	writeMu  sync.Mutex
	closed   bool
	stop     chan struct{}
	done     chan struct{}
	stopOnce sync.Once
}

// SpawnPTY starts a shell command under a daemon-owned PTY.
func (c *Client) SpawnPTY(worktree, branch, label, command string, cols, rows int, policy *procstore.Policy) (*procstore.Record, error) {
	if err := c.ensureDaemon(); err != nil {
		return nil, err
	}
	resp, err := c.roundtrip(&protocol.Request{
		Kind: protocol.KindSpawn, Worktree: worktree, Branch: branch,
		Label: label, Command: command, Policy: policy,
		PTY: true, PTYCols: cols, PTYRows: rows,
	})
	if err != nil {
		return nil, err
	}
	return resp.Record, nil
}

// SpawnPTYExec starts a shell-free program+argv command under a daemon-owned PTY.
func (c *Client) SpawnPTYExec(worktree, branch, workingDir, label, program string, args []string, cols, rows int, policy *procstore.Policy) (*procstore.Record, error) {
	if err := c.ensureDaemon(); err != nil {
		return nil, err
	}
	display := strings.Join(append([]string{program}, args...), " ")
	resp, err := c.roundtrip(&protocol.Request{
		Kind: protocol.KindSpawn, Worktree: worktree, Branch: branch,
		Label: label, Command: display, Program: program,
		Args: append([]string(nil), args...), WorkingDir: workingDir, Policy: policy,
		PTY: true, PTYCols: cols, PTYRows: rows,
	})
	if err != nil {
		return nil, err
	}
	return resp.Record, nil
}

// AttachPTY opens a full-duplex PTY attachment. afterSeq replays buffered output
// with a greater sequence number before live output continues.
func (c *Client) AttachPTY(ctx context.Context, id int, afterSeq uint64) (PTYAttachment, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := c.ensureDaemon(); err != nil {
		return nil, err
	}
	conn, err := c.dial()
	if err != nil {
		return nil, err
	}
	enc := protocol.NewEncoder(conn)
	dec := protocol.NewDecoder(conn)
	if err := enc.WriteRequest(&protocol.Request{Kind: protocol.KindPTYAttach, ID: id, AfterSeq: afterSeq}); err != nil {
		_ = conn.Close()
		return nil, err
	}
	resp, err := dec.ReadResponse()
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	if resp.Error != "" {
		_ = conn.Close()
		return nil, fmt.Errorf("%s", resp.Error)
	}
	if resp.Kind != protocol.KindPTYAttached {
		_ = conn.Close()
		return nil, fmt.Errorf("unexpected PTY attach response %q", resp.Kind)
	}

	a := &ptyAttachment{
		conn: conn, enc: enc, dec: dec,
		events: make(chan PTYEvent, 64),
		info: PTYInfo{
			ID:      resp.ID,
			Cols:    resp.PTYCols,
			Rows:    resp.PTYRows,
			NextSeq: resp.NextSeq,
		},
		stop: make(chan struct{}),
		done: make(chan struct{}),
	}
	go a.readLoop()
	go func() {
		select {
		case <-ctx.Done():
			_ = a.Close()
		case <-a.done:
		}
	}()
	return a, nil
}

func (a *ptyAttachment) Info() PTYInfo             { return a.info }
func (a *ptyAttachment) Events() <-chan PTYEvent { return a.events }

func (a *ptyAttachment) Write(data []byte) error {
	if len(data) == 0 {
		return nil
	}
	return a.send(&protocol.Request{Kind: protocol.KindPTYInput, Data: append([]byte(nil), data...)})
}

func (a *ptyAttachment) Resize(cols, rows int) error {
	if err := corepty.ValidateSize(cols, rows); err != nil {
		return err
	}
	return a.send(&protocol.Request{Kind: protocol.KindPTYResize, PTYCols: cols, PTYRows: rows})
}

func (a *ptyAttachment) send(req *protocol.Request) error {
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	if a.closed {
		return net.ErrClosed
	}
	return a.enc.WriteRequest(req)
}

func (a *ptyAttachment) Close() error {
	a.writeMu.Lock()
	if a.closed {
		a.writeMu.Unlock()
		return nil
	}
	// Mark the close as intentional before touching the socket. This prevents
	// the read loop from reporting the resulting EOF as a transport failure.
	a.closed = true
	a.stopOnce.Do(func() { close(a.stop) })
	_ = a.enc.WriteRequest(&protocol.Request{Kind: protocol.KindPTYDetach})
	err := a.conn.Close()
	a.writeMu.Unlock()
	return err
}

func (a *ptyAttachment) closeFromReader() {
	a.writeMu.Lock()
	if !a.closed {
		a.closed = true
		a.stopOnce.Do(func() { close(a.stop) })
		_ = a.conn.Close()
	}
	a.writeMu.Unlock()
}

func (a *ptyAttachment) readLoop() {
	defer close(a.done)
	defer close(a.events)
	defer a.closeFromReader()

	emit := func(ev PTYEvent) bool {
		select {
		case a.events <- ev:
			return true
		case <-a.stop:
			return false
		}
	}

	for {
		resp, err := a.dec.ReadResponse()
		if err != nil {
			select {
			case <-a.stop:
				return
			default:
				_ = emit(PTYEvent{
					Kind:  protocol.KindPTYError,
					Error: fmt.Sprintf("PTY transport: %v", err),
				})
				return
			}
		}
		switch resp.Kind {
		case protocol.KindPTYOutput:
			if !emit(PTYEvent{
				Kind: protocol.KindPTYOutput,
				Seq:  resp.Seq,
				Data: append([]byte(nil), resp.Data...),
			}) {
				return
			}
		case protocol.KindPTYExit:
			_ = emit(PTYEvent{
				Kind: protocol.KindPTYExit, ExitCode: resp.ExitCode, Error: resp.ExitError,
			})
			return
		case protocol.KindPTYError:
			if !emit(PTYEvent{Kind: protocol.KindPTYError, Error: resp.Error}) {
				return
			}
			if resp.EOF {
				return
			}
		default:
			if resp.Error != "" {
				if !emit(PTYEvent{Kind: protocol.KindPTYError, Error: resp.Error}) {
					return
				}
			}
			if resp.EOF {
				return
			}
		}
	}
}
