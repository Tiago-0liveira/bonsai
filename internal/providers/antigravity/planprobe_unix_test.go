//go:build linux || darwin || freebsd || openbsd || netbsd || dragonfly

package antigravity

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/term"
)

// The fake agy is this test binary started again with BONSAI_FAKE_AGY set to
// a mode. It puts its terminal in raw mode, as agy does, so Ctrl+C arrives as
// a byte and not as a signal.
func TestMain(m *testing.M) {
	if mode := os.Getenv("BONSAI_FAKE_AGY"); mode != "" {
		os.Exit(fakeAgy(mode))
	}
	os.Exit(m.Run())
}

func fakeAgy(mode string) int {
	if mode == "child" {
		time.Sleep(time.Minute)
		return 0
	}
	if pidFile := os.Getenv("BONSAI_FAKE_AGY_PIDS"); pidFile != "" {
		// A child in the same process group, as agy's helpers are.
		child := exec.Command(os.Args[0])
		child.Env = append(os.Environ(), "BONSAI_FAKE_AGY=child")
		if err := child.Start(); err == nil {
			_ = os.WriteFile(pidFile, []byte(fmt.Sprintf("%d %d", os.Getpid(), child.Process.Pid)), 0o600)
		}
	}
	if _, err := term.MakeRaw(int(os.Stdin.Fd())); err != nil {
		return 3
	}
	out := os.Stdout
	// Other text first, then the header painted at the top by moving the cursor.
	fmt.Fprint(out, "\x1b[?1049h\x1b[2J\x1b[5;1H> \x1b[2mType a message\x1b[0m\x1b[9;1H gemini-3.8-flash (high)")
	time.Sleep(150 * time.Millisecond)
	switch mode {
	case "found", "stubborn":
		fmt.Fprint(out, "\x1b[1;1H\x1b[2K  \x1b[1mme@example.com\x1b[0m \x1b[36m(Google AI Pro)\x1b[0m\x1b[5;3H")
	case "wrong":
		fmt.Fprint(out, "\x1b[1;1H  other@example.com (Google AI Pro)\x1b[5;3H")
	}
	if mode == "stubborn" {
		signal.Ignore(syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
		time.Sleep(time.Minute)
		return 0
	}
	interrupts := 0
	buf := make([]byte, 64)
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil {
			return 0
		}
		for _, b := range buf[:n] {
			if b != 0x03 {
				return 4 // the probe must never type anything else
			}
			interrupts++
		}
		if interrupts >= 2 {
			fmt.Fprint(out, "\x1b[?1049l")
			return 0
		}
	}
}

func shortProbeTimings(t *testing.T) {
	t.Helper()
	old := []time.Duration{probeDeadline, probeInterrupt, probeExitWait, probeSignalWait}
	probeDeadline, probeInterrupt, probeExitWait, probeSignalWait = 2*time.Second, 100*time.Millisecond, 300*time.Millisecond, 300*time.Millisecond
	t.Cleanup(func() {
		probeDeadline, probeInterrupt, probeExitWait, probeSignalWait = old[0], old[1], old[2], old[3]
	})
}

// runFake probes the fake agy in mode and returns the plan, the error and the
// pids of the fake and its child.
func runFake(t *testing.T, ctx context.Context, mode string) (string, error, []int) {
	t.Helper()
	pidFile := filepath.Join(t.TempDir(), "pids")
	env := append(os.Environ(), "BONSAI_FAKE_AGY="+mode, "BONSAI_FAKE_AGY_PIDS="+pidFile)
	plan, err := probePlan(ctx, os.Args[0], t.TempDir(), env, "me@example.com")
	var pids []int
	if data, readErr := os.ReadFile(pidFile); readErr == nil {
		for _, field := range strings.Fields(string(data)) {
			if pid, err := strconv.Atoi(field); err == nil {
				pids = append(pids, pid)
			}
		}
	}
	if len(pids) != 2 {
		t.Fatalf("fake did not record its pids: %v", pids)
	}
	return plan, err, pids
}

// assertGone waits briefly for orphans to be reaped, then fails if any pid
// still runs.
func assertGone(t *testing.T, pids []int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for _, pid := range pids {
		for syscall.Kill(pid, 0) == nil {
			if time.Now().After(deadline) {
				t.Fatalf("process %d is still running", pid)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
}

func TestProbePlanReadsTheLatePaintedHeader(t *testing.T) {
	shortProbeTimings(t)
	start := time.Now()
	plan, err, pids := runFake(t, t.Context(), "found")
	if err != nil || plan != "Google AI Pro" {
		t.Fatalf("plan = %q, %v", plan, err)
	}
	// The fake only exits after two Ctrl+C, before any signal is needed.
	if elapsed := time.Since(start); elapsed > probeDeadline {
		t.Fatalf("probe took %v", elapsed)
	}
	assertGone(t, pids)
}

func TestProbePlanIgnoresAnotherEmail(t *testing.T) {
	shortProbeTimings(t)
	plan, err, pids := runFake(t, t.Context(), "wrong")
	if err == nil || plan != "" {
		t.Fatalf("plan = %q, %v", plan, err)
	}
	assertGone(t, pids)
}

func TestProbePlanTimesOutWhenNothingIsPainted(t *testing.T) {
	shortProbeTimings(t)
	plan, err, pids := runFake(t, t.Context(), "never")
	if err == nil || plan != "" {
		t.Fatalf("plan = %q, %v", plan, err)
	}
	assertGone(t, pids)
}

func TestProbePlanKillsAnAgyThatIgnoresCtrlC(t *testing.T) {
	shortProbeTimings(t)
	start := time.Now()
	plan, err, pids := runFake(t, t.Context(), "stubborn")
	if err != nil || plan != "Google AI Pro" {
		t.Fatalf("plan = %q, %v", plan, err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("kill path took %v", elapsed)
	}
	assertGone(t, pids)
}

func TestProbePlanStopsOnCancel(t *testing.T) {
	shortProbeTimings(t)
	ctx, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()
	plan, err, pids := runFake(t, ctx, "never")
	if !errors.Is(err, context.DeadlineExceeded) || plan != "" {
		t.Fatalf("plan = %q, %v", plan, err)
	}
	assertGone(t, pids)
}
