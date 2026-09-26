//go:build !windows

package pty

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"

	creackpty "github.com/creack/pty"
)

type unixBackend struct {
	cols   int
	rows   int
	master *os.File
}

func newBackend(cols, rows int) (backend, error) {
	return &unixBackend{cols: cols, rows: rows}, nil
}

func (p *unixBackend) Start(cmd *exec.Cmd) error {
	if p.master != nil {
		return fmt.Errorf("PTY already started")
	}
	master, err := creackpty.StartWithSize(cmd, &creackpty.Winsize{
		Rows: uint16(p.rows),
		Cols: uint16(p.cols),
	})
	if err != nil {
		return err
	}
	p.master = master
	return nil
}

func (p *unixBackend) Read(buf []byte) (int, error) {
	if p.master == nil {
		return 0, os.ErrInvalid
	}
	return p.master.Read(buf)
}

func (p *unixBackend) Write(buf []byte) (int, error) {
	if p.master == nil {
		return 0, os.ErrInvalid
	}
	return p.master.Write(buf)
}

func (p *unixBackend) Resize(cols, rows int) error {
	if p.master == nil {
		return os.ErrInvalid
	}
	if err := creackpty.Setsize(p.master, &creackpty.Winsize{
		Rows: uint16(rows),
		Cols: uint16(cols),
	}); err != nil {
		return err
	}
	p.cols, p.rows = cols, rows
	return nil
}

func (p *unixBackend) Size() (int, int, error) {
	if p.master == nil {
		return 0, 0, os.ErrInvalid
	}
	rows, cols, err := creackpty.Getsize(p.master)
	if err != nil {
		return 0, 0, err
	}
	return cols, rows, nil
}

func (p *unixBackend) Close() error {
	if p.master == nil {
		return nil
	}
	err := p.master.Close()
	p.master = nil
	return err
}

func waitProcess(ctx context.Context, cmd *exec.Cmd) error {
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		var killErr error
		if cmd.Process != nil {
			killErr = cmd.Process.Kill()
		}
		<-done // always reap
		if killErr != nil {
			return errors.Join(ctx.Err(), killErr)
		}
		return ctx.Err()
	}
}

var _ io.ReadWriteCloser = (*unixBackend)(nil)
