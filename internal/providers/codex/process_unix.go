//go:build !windows

package codex

import (
	"os/exec"
	"syscall"
)

// isolate runs cmd in its own process group and kills the whole group when its
// context ends, so a timed-out codex leaves no child holding its output open.
func isolate(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
			return cmd.Process.Kill()
		}
		return nil
	}
}
