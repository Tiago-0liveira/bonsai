//go:build linux || darwin || freebsd || openbsd || netbsd || dragonfly

package antigravity

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/termscreen"
	"github.com/creack/pty"
)

const planProbeSupported = true

// Probe timings; variables so tests can shorten them.
var (
	probeDeadline   = 8 * time.Second
	probePoll       = 100 * time.Millisecond
	probeInterrupt  = 300 * time.Millisecond
	probeExitWait   = 3 * time.Second
	probeSignalWait = time.Second
)

// probePlan starts interactive agy in a PTY and reads the plan from the
// "email (plan)" header it paints, which only exists on screen. It writes
// nothing to agy but two Ctrl+C to quit, so no prompt ever reaches a model.
// The process group is always ended and waited for.
func probePlan(ctx context.Context, executable, dir string, env []string, email string) (string, error) {
	cmd := exec.Command(executable)
	cmd.Dir = dir
	cmd.Env = append(env, "TERM=xterm-256color")
	// creack/pty starts the child in its own session (Setsid), so its pid is
	// also its process group.
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: 140, Rows: 40})
	if err != nil {
		return "", err
	}
	exited := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(exited)
	}()
	screen := termscreen.New(140, 40)
	go func() {
		buf := make([]byte, 32<<10)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				_, _ = screen.Write(buf[:n])
			}
			if err != nil {
				return
			}
		}
	}()
	defer func() {
		stopProbe(cmd, ptmx, exited)
		ptmx.Close()
	}()

	deadline := time.NewTimer(probeDeadline)
	defer deadline.Stop()
	tick := time.NewTicker(probePoll)
	defer tick.Stop()
	for {
		if plan, ok := findPlan(screen.Lines(), email); ok {
			return plan, nil
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-deadline.C:
			return "", errors.New("plan header not found")
		case <-exited:
			if plan, ok := findPlan(screen.Lines(), email); ok {
				return plan, nil
			}
			return "", errors.New("agy exited before showing the plan")
		case <-tick.C:
		}
	}
}

// stopProbe asks agy to quit with Ctrl+C twice, then ends its process group
// with SIGTERM and SIGKILL, waiting after each step.
func stopProbe(cmd *exec.Cmd, ptmx *os.File, exited <-chan struct{}) {
	wait := func(d time.Duration) bool {
		select {
		case <-exited:
			return true
		case <-time.After(d):
			return false
		}
	}
	pid := cmd.Process.Pid
	// Whatever agy started in its group goes with it.
	defer func() { _ = syscall.Kill(-pid, syscall.SIGKILL) }()
	select {
	case <-exited:
		return
	default:
	}
	_, _ = ptmx.Write([]byte{0x03})
	if wait(probeInterrupt) {
		return
	}
	_, _ = ptmx.Write([]byte{0x03})
	if wait(probeExitWait) {
		return
	}
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	if wait(probeSignalWait) {
		return
	}
	_ = syscall.Kill(-pid, syscall.SIGKILL)
	<-exited
}
