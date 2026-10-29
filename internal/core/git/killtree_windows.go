//go:build windows

package git

import (
	"os/exec"
	"strconv"
)

// killTreeOnCancel kills git and its descendants with taskkill /T on
// cancellation; Windows has no process groups to signal.
func killTreeOnCancel(cmd *exec.Cmd) {
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		_ = exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid)).Run()
		return cmd.Process.Kill()
	}
}
