// Package pty provides Bonsai's platform-neutral pseudo-terminal abstraction.
// The rest of Bonsai depends on these interfaces rather than on the underlying
// Unix PTY or Windows ConPTY implementation.
package pty

import (
	"context"
	"fmt"
	"os/exec"
)

const (
	DefaultCols  = 80
	DefaultRows  = 24
	MaxDimension = 32767
)

// Session is the live terminal endpoint owned by the daemon.
type Session interface {
	Read([]byte) (int, error)
	Write([]byte) (int, error)
	Resize(cols, rows int) error
	Size() (cols, rows int, err error)
	Close() error
}

// Process is a child process started with all standard streams connected to a
// pseudo-terminal.
type Process interface {
	Session() Session
	Wait(context.Context) error
}

type backend interface {
	Session
	Start(*exec.Cmd) error
}

type process struct {
	cmd     *exec.Cmd
	session backend
}

// ValidateSize rejects nonsensical or platform-hostile terminal dimensions.
func ValidateSize(cols, rows int) error {
	if cols <= 0 || rows <= 0 {
		return fmt.Errorf("terminal dimensions must be positive (got %dx%d)", cols, rows)
	}
	if cols > MaxDimension || rows > MaxDimension {
		return fmt.Errorf("terminal dimensions exceed maximum %d (got %dx%d)", MaxDimension, cols, rows)
	}
	return nil
}

// NormalizeSize applies the default 80x24 dimensions to zero-valued spawn
// fields, then validates the result. Negative values are never treated as
// defaults.
func NormalizeSize(cols, rows int) (int, int, error) {
	if cols == 0 {
		cols = DefaultCols
	}
	if rows == 0 {
		rows = DefaultRows
	}
	if err := ValidateSize(cols, rows); err != nil {
		return 0, 0, err
	}
	return cols, rows, nil
}

// Start creates a platform PTY/ConPTY, attaches cmd to it, and starts cmd.
func Start(cmd *exec.Cmd, cols, rows int) (Process, error) {
	if cmd == nil {
		return nil, fmt.Errorf("nil command")
	}
	cols, rows, err := NormalizeSize(cols, rows)
	if err != nil {
		return nil, err
	}
	session, err := newBackend(cols, rows)
	if err != nil {
		return nil, err
	}
	if err := session.Start(cmd); err != nil {
		_ = session.Close()
		return nil, err
	}
	return &process{cmd: cmd, session: session}, nil
}

func (p *process) Session() Session { return p.session }

func (p *process) Wait(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	return waitProcess(ctx, p.cmd)
}
