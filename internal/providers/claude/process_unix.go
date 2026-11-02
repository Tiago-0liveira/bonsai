//go:build !windows

package claude

import (
	"os/exec"
	"syscall"
)

// isolate runs cmd in its own process group and kills the whole group when its
// context ends, so a cancelled claude leaves no child holding its output open.
func isolate(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
			return cmd.Process.Kill()
		}
		return nil
	}
}
