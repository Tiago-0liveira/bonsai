package codex

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

// The app-server is Codex's published JSON-RPC protocol over stdio
// (`codex app-server generate-json-schema` prints it). Bonsai uses it only to
// read what the TUI shows: model list and rate limits. Token refreshes, if one
// is needed, are done by Codex itself.

// appServerTimeout bounds one whole exchange, process start to exit. A variable
// so tests can shorten it.
var appServerTimeout = 15 * time.Second

const (
	// maxAppServerOutput caps everything read from one app-server process.
	maxAppServerOutput = 1 << 20
	// appServerGrace is how long a process gets to exit after its stdin closes
	// before its process group is killed.
	appServerGrace = time.Second
	// clientVersion is reported in `initialize`; the protocol does not gate on it.
	clientVersion = "1"
)

// rpcError is a JSON-RPC error reply. It carries only the code: the message and
// data are server-controlled text and are never surfaced.
type rpcError struct {
	method string
	code   int
}

func (e *rpcError) Error() string {
	return fmt.Sprintf("codex app-server: %s failed (code %d)", e.method, e.code)
}

type rpcMessage struct {
	ID     json.RawMessage `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code int `json:"code"`
	} `json:"error"`
}

// rpcConn is one live `codex app-server` process. Calls are sequential.
type rpcConn struct {
	stdin    io.Writer
	messages <-chan rpcMessage
	nextID   int
	// oversized reports that the output cap was hit; set before messages closes.
	oversized *bool
}

// call sends one request and waits for the reply with the same id. Server
// notifications and requests, and replies to nothing Bonsai asked, are skipped.
func (c *rpcConn) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.nextID++
	id := c.nextID
	if err := c.send(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); err != nil {
		return nil, err
	}
	want := strconv.Itoa(id)
	for {
		select {
		case msg, ok := <-c.messages:
			if !ok {
				if *c.oversized {
					return nil, errors.New("codex app-server: response too large")
				}
				return nil, errors.New("codex app-server: exited early")
			}
			if string(bytes.TrimSpace(msg.ID)) != want {
				continue
			}
			if msg.Error != nil {
				return nil, &rpcError{method: method, code: msg.Error.Code}
			}
			return msg.Result, nil
		case <-ctx.Done():
			return nil, fmt.Errorf("codex app-server: %s: %w", method, ctx.Err())
		}
	}
}

func (c *rpcConn) notify(method string) error {
	return c.send(map[string]any{"jsonrpc": "2.0", "method": method})
}

func (c *rpcConn) send(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if _, err := c.stdin.Write(append(data, '\n')); err != nil {
		// The pipe is closed only when the process has gone.
		return errors.New("codex app-server: exited early")
	}
	return nil
}

// withAppServer starts `codex app-server` with the profile's environment,
// completes the initialize handshake and runs fn. The process always runs in
// its own process group and is gone when this returns, whatever fn did.
func (p *Provider) withAppServer(ctx context.Context, account agents.Account, fn func(context.Context, *rpcConn) error) error {
	prepared, err := p.profileCommand(account, "app-server")
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, appServerTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, prepared.Executable, prepared.Args...)
	cmd.Dir = prepared.Dir
	cmd.Env = prepared.Environment(os.Environ())
	cmd.Stderr = io.Discard
	cmd.WaitDelay = 2 * time.Second
	isolate(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("codex app-server: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("codex app-server: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("codex app-server: %w", err)
	}

	messages := make(chan rpcMessage)
	oversized := new(bool)
	readerDone := make(chan struct{})
	stopReader := make(chan struct{})
	go func() {
		defer close(readerDone)
		defer close(messages)
		limited := &io.LimitedReader{R: stdout, N: maxAppServerOutput}
		reader := bufio.NewReader(limited)
		for {
			line, err := reader.ReadBytes('\n')
			if len(bytes.TrimSpace(line)) > 0 {
				var msg rpcMessage
				if json.Unmarshal(line, &msg) == nil && len(msg.ID) > 0 {
					select {
					case messages <- msg:
					case <-stopReader:
						return
					}
				}
			}
			if err != nil {
				*oversized = limited.N == 0
				return
			}
		}
	}()

	// Teardown, in this order: close stdin so a well-behaved server exits by
	// itself and give it a moment; then kill the group whatever is left (a
	// server stuck writing to a pipe nobody reads, or a child it started); the
	// reader sees EOF, and only then is the process reaped.
	defer func() {
		close(stopReader)
		_ = stdin.Close()
		select {
		case <-readerDone:
		case <-time.After(appServerGrace):
		}
		cancel()
		<-readerDone
		_ = cmd.Wait()
	}()

	conn := &rpcConn{stdin: stdin, messages: messages, oversized: oversized}
	if _, err := conn.call(ctx, "initialize", map[string]any{
		"clientInfo": map[string]string{"name": "bonsai", "version": clientVersion},
	}); err != nil {
		return err
	}
	if err := conn.notify("initialized"); err != nil {
		return err
	}
	return fn(ctx, conn)
}

// flightGroup coalesces concurrent identical requests: callers with the same key
// share one execution and one result.
type flightGroup struct {
	mu    sync.Mutex
	calls map[string]*flightCall
}

type flightCall struct {
	done chan struct{}
	val  any
}

// do runs fn once per key at a time. fn gets a context that outlives the first
// caller's cancellation, so one impatient caller does not fail the others; each
// caller still returns as soon as its own ctx ends.
func (g *flightGroup) do(ctx context.Context, key string, fn func(context.Context) any) (any, error) {
	g.mu.Lock()
	if g.calls == nil {
		g.calls = map[string]*flightCall{}
	}
	call, running := g.calls[key]
	if !running {
		call = &flightCall{done: make(chan struct{})}
		g.calls[key] = call
		detached := context.WithoutCancel(ctx)
		go func() {
			call.val = fn(detached)
			g.mu.Lock()
			delete(g.calls, key)
			g.mu.Unlock()
			close(call.done)
		}()
	}
	g.mu.Unlock()
	select {
	case <-call.done:
		return call.val, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
