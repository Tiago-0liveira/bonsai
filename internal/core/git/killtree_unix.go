//go:build !windows

package git

import (
	"os/exec"
	"syscall"
)

// killTreeOnCancel runs git in its own process group and kills the whole group
// on cancellation, so hooks and helpers die with it.
func killTreeOnCancel(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
