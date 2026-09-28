//go:build windows

package pty

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"

	"github.com/charmbracelet/x/xpty"
)

func newBackend(cols, rows int) (backend, error) {
	return xpty.NewPty(cols, rows)
}

// ConPTY processes are not compatible with exec.Cmd.Wait on supported Go
// versions. Reap the os.Process directly and synthesize exec.ExitError so the
// daemon sees the same non-zero-exit semantics as it does on Unix.
func waitProcess(ctx context.Context, cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return fmt.Errorf("process not started")
	}
	type result struct {
		state *os.ProcessState
		err   error
	}
	done := make(chan result, 1)
	go func() {
		state, err := cmd.Process.Wait()
		done <- result{state: state, err: err}
	}()

	var r result
	select {
	case r = <-done:
	case <-ctx.Done():
		killErr := cmd.Process.Kill()
		r = <-done
		cmd.ProcessState = r.state
		if killErr != nil {
			return errors.Join(ctx.Err(), killErr)
		}
		return ctx.Err()
	}
	cmd.ProcessState = r.state
	if r.err != nil {
		return r.err
	}
	if r.state != nil && !r.state.Success() {
		return &exec.ExitError{ProcessState: r.state}
	}
	return nil
}
