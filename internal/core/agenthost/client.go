package agenthost

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
)

var errLineTooLong = errors.New("agent host message too long")

// ErrVersion means the holder was started by a bonsai build speaking another
// protocol. It keeps running its session; clients leave it alone.
var ErrVersion = errors.New("agent started by another bonsai version; stop it or let it finish")

// Conn is one client connection to a holder.
type Conn struct {
	conn net.Conn
	dec  *decoder
	mu   sync.Mutex
}

// Dial connects to the holder of r and sends hello (its version is filled in).
// It returns the holder's ready frame.
func Dial(r Record, hello Request) (*Conn, Frame, error) {
	return DialContext(context.Background(), r, hello)
}

// DialContext bounds both connecting and the initial holder handshake by ctx.
func DialContext(ctx context.Context, r Record, hello Request) (*Conn, Frame, error) {
	if r.Protocol != Version {
		return nil, Frame{}, ErrVersion
	}
	dialer := net.Dialer{Timeout: 2 * time.Second}
	conn, err := dialer.DialContext(ctx, "unix", r.Socket)
	if err != nil {
		return nil, Frame{}, err
	}
	stopCancel := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stopCancel()
	c := &Conn{conn: conn, dec: newDecoder(conn)}
	hello.Type, hello.Version = "hello", Version
	if err := c.Send(hello); err != nil {
		_ = conn.Close()
		return nil, Frame{}, err
	}
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	ready, err := c.Recv()
	_ = conn.SetReadDeadline(time.Time{})
	if err != nil {
		_ = conn.Close()
		if ctx.Err() != nil {
			return nil, Frame{}, ctx.Err()
		}
		return nil, Frame{}, err
	}
	if ready.Type != "ready" {
		_ = conn.Close()
		if ready.Type == "error" && strings.Contains(ready.Error, "protocol") {
			return nil, Frame{}, ErrVersion
		}
		return nil, Frame{}, fmt.Errorf("agent host refused: %s", ready.Error)
	}
	if err := ctx.Err(); err != nil {
		_ = conn.Close()
		return nil, Frame{}, err
	}
	return c, ready, nil
}

// Send writes one request.
func (c *Conn) Send(r Request) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	_ = c.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	return encode(c.conn, r)
}

// Recv reads the next frame.
func (c *Conn) Recv() (Frame, error) {
	var f Frame
	err := c.dec.decode(&f)
	return f, err
}

// Close ends the connection; the session keeps running.
func (c *Conn) Close() error { return c.conn.Close() }

// SetDeadline bounds the connection's reads and writes.
func (c *Conn) SetDeadline(t time.Time) error { return c.conn.SetDeadline(t) }

// Query asks a running holder for its info.
func Query(r Record) (Info, error) {
	c, _, err := Dial(r, Request{Mode: ModeStatus})
	if err != nil {
		return Info{}, err
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(3 * time.Second))
	if err := c.Send(Request{Type: "info"}); err != nil {
		return Info{}, err
	}
	for {
		f, err := c.Recv()
		if err != nil {
			return Info{}, err
		}
		if f.Type == "info" && f.Info != nil {
			return *f.Info, nil
		}
	}
}

// Stop asks a running holder to stop its session; reason, when set, becomes
// the session's error. It returns once the request is sent.
func Stop(r Record, reason string) error {
	c, _, err := Dial(r, Request{Mode: ModeStatus})
	if err != nil {
		return err
	}
	defer c.Close()
	return c.Send(Request{Type: "stop", Error: reason})
}

// Wait blocks until r's holder has exited, or timeout passes.
func Wait(p Paths, id string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if !held(p.lock(id)) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(25 * time.Millisecond)
	}
}
