//go:build windows

package server

import (
	"errors"
	"fmt"
	"os/exec"
)

func formatExitError(err error) string {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return fmt.Sprintf("exited %d · failed", ee.ExitCode())
	}
	return "failed · " + err.Error()
}
