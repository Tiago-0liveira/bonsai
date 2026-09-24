//go:build !windows

package server

import (
	"errors"
	"fmt"
	"os/exec"
	"syscall"
)

func formatExitError(err error) string {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		if st, ok := ee.Sys().(syscall.WaitStatus); ok && st.Signaled() {
			return fmt.Sprintf("killed by %s · failed", st.Signal())
		}
		return fmt.Sprintf("exited %d · failed", ee.ExitCode())
	}
	return "failed · " + err.Error()
}
