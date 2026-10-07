//go:build linux || darwin || freebsd || openbsd || netbsd || dragonfly

package agentterminal

import (
	"os"
	"os/exec"
	"syscall"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/creack/pty"
)

const Supported = true

type process struct {
	file *os.File
	cmd  *exec.Cmd
}

func launch(p agents.PreparedSession, cols, rows int) (*process, error) {
	cmd := exec.Command(p.Executable, p.Args...)
	cmd.Dir = p.Dir
	cmd.Env = agents.BuildEnvironment(os.Environ(), p.EnvSet, p.EnvUnset)
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	if err != nil {
		return nil, err
	}
	return &process{f, cmd}, nil
}
func (p *process) resize(cols, rows int) error {
	return pty.Setsize(p.file, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
}
func (p *process) signal(force bool) {
	sig := syscall.SIGTERM
	if force {
		sig = syscall.SIGKILL
	}
	_ = syscall.Kill(-p.cmd.Process.Pid, sig)
}
func (p *process) wait() (int, error) { err := p.cmd.Wait(); return p.cmd.ProcessState.ExitCode(), err }
