//go:build linux || darwin || freebsd || openbsd || netbsd || dragonfly

package antigravity

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/termscreen"
	"github.com/creack/pty"
)

const planProbeSupported = true

// Probe timings; variables so tests can shorten them. agy runs at most
// probeDeadline from the PTY start; its screen is first read after
// probeFirstCheck, then every probePoll.
var (
	probeFirstCheck = time.Second
	probeDeadline   = 7 * time.Second
	probePoll       = 50 * time.Millisecond
	probeKeyGap     = 100 * time.Millisecond
	probeWizardWait = 2 * time.Second
	probeInterrupt  = 300 * time.Millisecond
	probeExitWait   = time.Second
	probeSignalWait = time.Second
)

// wizardPage is a page of agy's first-run wizard, told apart by its text, and
// the keys that answer it: a home agy has not run in yet shows the wizard
// instead of the header.
type wizardPage struct {
	n     int
	marks []string
	keys  [][]byte
	names string
}

// wizardPages: page 2 is looked for first, as page 1's banner may still be on
// screen above it. Page 1 (colour scheme) takes the default with Enter. Page 2
// (consent) leaves its checkbox as it is, where Enter would toggle it, and
// moves to [Done] with Down and Right before Enter.
var wizardPages = []wizardPage{
	{n: 2, marks: []string{"Terms of Service", "Yes, I agree"}, keys: [][]byte{[]byte("\x1b[B"), []byte("\x1b[C"), []byte("\r")}, names: "Down, Right, Enter"},
	{n: 1, marks: []string{"Choose your color scheme"}, keys: [][]byte{[]byte("\r")}, names: "Enter"},
}

// findWizardPage returns the wizard page on screen, or nil.
func findWizardPage(lines []string) *wizardPage {
	for i := range wizardPages {
		for _, line := range lines {
			for _, mark := range wizardPages[i].marks {
				if strings.Contains(line, mark) {
					return &wizardPages[i]
				}
			}
		}
	}
	return nil
}

// probePlan starts interactive agy in a PTY and reads the plan from the
// "email (plan)" header it paints, which only exists on screen. The only bytes
// it writes to agy are the answers to the first-run wizard, each page's at
// most once, and two Ctrl+C to quit, so no prompt ever reaches a model. The
// process group is always ended and waited for.
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
	logf.Printf("pty started, agy pid %d, looking for %q (first check %s, poll %s, deadline %s)", cmd.Process.Pid, email, probeFirstCheck, probePoll, probeDeadline)
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
	// Nothing is read or sent before the first check: agy paints in steps.
	next := time.NewTimer(probeFirstCheck)
	defer next.Stop()
	answered := map[int]time.Time{}
	var last []string
	for attempt := 1; ; attempt++ {
		select {
		case <-ctx.Done():
			logf.Printf("stopped: %v", ctx.Err())
			return "", ctx.Err()
		case <-deadline.C:
			logf.Printf("stopped: deadline %s reached, plan header not found", probeDeadline)
			logf.Screen(screen.Lines())
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
		case <-next.C:
		}
		lines := screen.Lines()
		if plan, ok := findPlan(lines, email); ok {
			logf.Printf("attempt %d: found plan %q", attempt, plan)
			logf.Screen(lines)
			return plan, nil
		}
		// The screen is dumped when it changed since the last attempt, so a
		// quiet agy does not fill the log 20 times a second.
		if !slices.Equal(lines, last) {
			logf.Printf("attempt %d: no match for %q, screen changed", attempt, email)
			logf.Screen(lines)
			last = lines
		}
		if page := findWizardPage(lines); page != nil {
			at, ok := answered[page.n]
			switch {
			case !ok:
				logf.Printf("attempt %d: first-run wizard page %d on screen; sending %s", attempt, page.n, page.names)
				if err := sendKeys(ctx, ptmx, page.keys); err != nil {
					logf.Printf("stopped: sending wizard keys: %v", err)
					return "", err
				}
				answered[page.n] = time.Now()
				logf.Printf("wizard page %d answered", page.n)
				// Dump the next screen whatever it is: it shows what the keys did.
				last = nil
			case time.Since(at) >= probeWizardWait:
				logf.Printf("stopped: wizard page %d still on screen %s after its keys", page.n, probeWizardWait)
				return "", fmt.Errorf("first-run wizard not completed (page %d)", page.n)
			}
		}
		next.Reset(probePoll)
	}
}

// sendKeys writes each key on its own, probeKeyGap apart, so the TUI reads
// them as separate key events.
func sendKeys(ctx context.Context, w io.Writer, keys [][]byte) error {
	for i, key := range keys {
		if i > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(probeKeyGap):
			}
		}
		if _, err := w.Write(key); err != nil {
			return err
		}
	}
	return nil
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
