//go:build !windows

package agenthost

import (
	"os"
	"os/exec"
	"syscall"

	"golang.org/x/sys/unix"
)

func setDetach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

func releaseStdout() {
	devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		return
	}
	defer devnull.Close()
	_ = unix.Dup2(int(devnull.Fd()), int(os.Stdout.Fd()))
}
