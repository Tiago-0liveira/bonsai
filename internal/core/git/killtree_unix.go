//go:build !windows

package git

import (
	"os/exec"
	"syscall"
)

// killTreeOnCancel starts a new session without a controlling terminal so tty
// prompts fail instead of stopping non-interactive git. Git leads its process
// group, so cancellation still kills hooks and helpers with it.
func killTreeOnCancel(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	setParentDeathSignal(cmd.SysProcAttr)
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}

// Git is its session and process-group leader, so this also reaches hooks,
// credential helpers and transports that inherited its group.
func terminateTree(pid int) {
	_ = syscall.Kill(-pid, syscall.SIGTERM)
}
