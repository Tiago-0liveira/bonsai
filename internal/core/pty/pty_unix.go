//go:build !windows

package pty

import (
	"context"
	"errors"
	"os/exec"

	"github.com/charmbracelet/x/xpty"
)

func newBackend(cols, rows int) (backend, error) {
	return xpty.NewPty(cols, rows)
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
