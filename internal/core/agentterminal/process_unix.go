//go:build linux || darwin || freebsd || openbsd || netbsd || dragonfly

package agentterminal

import (
	"os"
	"os/exec"
	"strings"
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
	cmd.Env = childEnvironment(p, os.Environ())
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	if err != nil {
		return nil, err
	}
	return &process{f, cmd}, nil
}

// childEnvironment prepares the PTY child's environment. The web terminal
// renders truecolor, so COLORTERM is always advertised and TERM falls back to
// xterm-256color when the host has none or "dumb". FORCE_COLOR and
// CLICOLOR_FORCE leak in from the daemon's process launcher; with a real PTY
// tools detect color themselves, so they are dropped. NO_COLOR is the user's
// choice and is left alone. The provider's own EnvSet is applied last, so it
// still wins over every default here.
func childEnvironment(p agents.PreparedSession, base []string) []string {
	set := map[string]string{"COLORTERM": "truecolor"}
	if term := lastEnvValue(base, "TERM"); term == "" || term == "dumb" {
		set["TERM"] = "xterm-256color"
	}
	return p.Environment(agents.BuildEnvironment(base, set, []string{"FORCE_COLOR", "CLICOLOR_FORCE"}))
}

func lastEnvValue(env []string, name string) string {
	value := ""
	for _, entry := range env {
		if rest, ok := strings.CutPrefix(entry, name+"="); ok {
			value = rest
		}
	}
	return value
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
