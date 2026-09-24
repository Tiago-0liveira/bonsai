//go:build !windows

package exec

import (
	"os/exec"
	"syscall"
)

func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func killProcessTree(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	KillPID(cmd.Process.Pid)
}

// KillPID terminates the process group (or single process) identified by pid.
func KillPID(pid int) {
	if pid <= 0 {
		return
	}
	if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil {
		// Fall back to the single process if the group signal failed.
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
}
