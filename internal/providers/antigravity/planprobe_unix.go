//go:build linux || darwin || freebsd || openbsd || netbsd || dragonfly

package antigravity

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"slices"
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
	logf := probeLogFrom(ctx)
	cmd := exec.Command(executable)
	cmd.Dir = dir
	cmd.Env = append(env, "TERM=xterm-256color")
	// creack/pty starts the child in its own session (Setsid), so its pid is
	// also its process group.
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: 140, Rows: 40})
	if err != nil {
		logf.Printf("pty start failed: %v", err)
		return "", err
	}
	logf.Printf("pty started, agy pid %d, looking for %q (deadline %s, poll %s)", cmd.Process.Pid, email, probeDeadline, probePoll)
	var waitErr error
	exited := make(chan struct{})
	go func() {
		waitErr = cmd.Wait()
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
				logf.Printf("pty read ended: %v", err)
				return
			}
		}
	}()
	defer func() {
		stopProbe(cmd, ptmx, exited, logf)
		ptmx.Close()
	}()

	deadline := time.NewTimer(probeDeadline)
	defer deadline.Stop()
	tick := time.NewTicker(probePoll)
	defer tick.Stop()
	var last []string
	for attempt := 1; ; attempt++ {
		lines := screen.Lines()
		if plan, ok := findPlan(lines, email); ok {
			logf.Printf("attempt %d: found plan %q", attempt, plan)
			logf.Screen(lines)
			return plan, nil
		}
		// The screen is dumped when it changed since the last attempt, so a
		// quiet agy does not fill the log 10 times a second.
		if !slices.Equal(lines, last) {
			logf.Printf("attempt %d: no match for %q, screen changed", attempt, email)
			logf.Screen(lines)
			last = lines
		} else {
			logf.Printf("attempt %d: no match, screen unchanged", attempt)
		}
		select {
		case <-ctx.Done():
			logf.Printf("stopped: %v", ctx.Err())
			return "", ctx.Err()
		case <-deadline.C:
			logf.Printf("stopped: deadline %s reached, plan header not found", probeDeadline)
			return "", errors.New("plan header not found")
		case <-exited:
			lines := screen.Lines()
			if plan, ok := findPlan(lines, email); ok {
				logf.Printf("agy exited (%v) but its screen had the plan %q", waitErr, plan)
				return plan, nil
			}
			logf.Printf("stopped: agy exited before showing the plan (%v)", waitErr)
			logf.Screen(lines)
			return "", errors.New("agy exited before showing the plan")
		case <-tick.C:
		}
	}
}

// stopProbe asks agy to quit with Ctrl+C twice, then ends its process group
// with SIGTERM and SIGKILL, waiting after each step.
func stopProbe(cmd *exec.Cmd, ptmx *os.File, exited <-chan struct{}, logf *probeLog) {
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
		logf.Printf("stop: agy already exited")
		return
	default:
	}
	logf.Printf("stop: Ctrl+C")
	_, _ = ptmx.Write([]byte{0x03})
	if wait(probeInterrupt) {
		logf.Printf("stop: agy exited after Ctrl+C")
		return
	}
	logf.Printf("stop: Ctrl+C again")
	_, _ = ptmx.Write([]byte{0x03})
	if wait(probeExitWait) {
		logf.Printf("stop: agy exited after second Ctrl+C")
		return
	}
	logf.Printf("stop: SIGTERM to group %d", pid)
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	if wait(probeSignalWait) {
		logf.Printf("stop: agy exited after SIGTERM")
		return
	}
	logf.Printf("stop: SIGKILL to group %d", pid)
	_ = syscall.Kill(-pid, syscall.SIGKILL)
	<-exited
	logf.Printf("stop: agy killed")
}
