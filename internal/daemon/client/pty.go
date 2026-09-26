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

// PTYAttachment is a transport-neutral full-duplex terminal attachment.
type PTYAttachment interface {
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

	writeMu sync.Mutex
	closed  bool
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
	}
	go a.readLoop()
	go func() {
		select {
		case <-ctx.Done():
			_ = a.Close()
		case <-a.events:
			// The read loop owns channel closure. If it ends first this goroutine
			// may consume one event, so do not use this branch.
		}
	}()
	// Replace the watcher above with a non-consuming closure signal.
	// A tiny forwarding goroutine is avoided by watching the socket lifetime in
	// readLoop; context cancellation still needs to close the transport.
	go func() {
		<-ctx.Done()
		_ = a.Close()
	}()
	return a, nil
}

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
	_ = a.enc.WriteRequest(&protocol.Request{Kind: protocol.KindPTYDetach})
	a.closed = true
	err := a.conn.Close()
	a.writeMu.Unlock()
	return err
}

func (a *ptyAttachment) closeFromReader() {
	a.writeMu.Lock()
	if !a.closed {
		a.closed = true
		_ = a.conn.Close()
	}
	a.writeMu.Unlock()
}

func (a *ptyAttachment) readLoop() {
	defer close(a.events)
	defer a.closeFromReader()

	for {
		resp, err := a.dec.ReadResponse()
		if err != nil {
			return
		}
		switch resp.Kind {
		case protocol.KindPTYOutput:
			a.events <- PTYEvent{
				Kind: protocol.KindPTYOutput,
				Seq:  resp.Seq,
				Data: append([]byte(nil), resp.Data...),
			}
		case protocol.KindPTYExit:
			a.events <- PTYEvent{
				Kind: protocol.KindPTYExit, ExitCode: resp.ExitCode, Error: resp.ExitError,
			}
			return
		case protocol.KindPTYError:
			a.events <- PTYEvent{Kind: protocol.KindPTYError, Error: resp.Error}
			if resp.EOF {
				return
			}
		default:
			if resp.Error != "" {
				a.events <- PTYEvent{Kind: protocol.KindPTYError, Error: resp.Error}
			}
			if resp.EOF {
				return
			}
		}
	}
}
