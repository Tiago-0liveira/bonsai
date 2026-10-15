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
	header := func() {
		fmt.Fprint(out, "\x1b[1;1H\x1b[2K  \x1b[1mme@example.com\x1b[0m \x1b[36m(Google AI Pro)\x1b[0m\x1b[5;3H")
	}
	chat := func() {
		fmt.Fprint(out, "\x1b[2J\x1b[5;1H> \x1b[2mType a message\x1b[0m\x1b[9;1H gemini-3.8-flash (high)")
	}
	// The pages of agy's first-run wizard; page 2 keeps page 1's banner.
	page1 := func() {
		fmt.Fprint(out, "\x1b[2J\x1b[H Welcome to Antigravity CLI!\r\n\r\n Choose your color scheme\r\n  > Dark\r\n    Light\r\n\r\n [Next]\r\n\r\n enter Confirm")
	}
	page2 := func() {
		fmt.Fprint(out, "\x1b[2J\x1b[H Welcome to Antigravity CLI!\r\n\r\n By continuing you accept the Terms of Service\r\n [x] Yes, I agree to send usage statistics\r\n\r\n [Previous] [Done]")
	}
	fmt.Fprint(out, "\x1b[?1049h")
	// The keys each mode accepts, in order; anything else ends the fake.
	var expect string
	switch mode {
	case "wizard", "wizard-stuck":
		page1()
		expect = map[string]string{"wizard": "\r\x1b[B\x1b[C\r", "wizard-stuck": "\r"}[mode]
	case "wizard-page2":
		page2()
		expect = "\x1b[B\x1b[C\r"
	default:
		// Other text first, then the header painted at the top by moving the cursor.
		chat()
	}
	switch mode {
	case "found", "stubborn":
		time.Sleep(150 * time.Millisecond)
		header()
	case "header-late":
		time.Sleep(1500 * time.Millisecond)
		header()
	case "wrong":
		time.Sleep(150 * time.Millisecond)
		fmt.Fprint(out, "\x1b[1;1H  other@example.com (Google AI Pro)\x1b[5;3H")
	}
	if mode == "stubborn" {
		signal.Ignore(syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
		time.Sleep(time.Minute)
		return 0
	}
	keysFile := os.Getenv("BONSAI_FAKE_AGY_KEYS")
	var keys []byte
	interrupts := 0
	buf := make([]byte, 64)
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil {
			return 0
		}
		for _, b := range buf[:n] {
			if b == 0x03 {
				interrupts++
				continue
			}
			keys = append(keys, b)
		}
		if keysFile != "" {
			_ = os.WriteFile(keysFile, keys, 0o600)
		}
		got := string(keys)
		if !strings.HasPrefix(expect, got) {
			return 4 // the probe must never type anything else
		}
		switch {
		case got == "":
		case mode == "wizard" && got == "\r":
			page2()
		case mode != "wizard-stuck" && got == expect:
			chat()
			header()
		}
		if interrupts >= 2 {
			fmt.Fprint(out, "\x1b[?1049l")
			return 0
		}
	}
}

func shortProbeTimings(t *testing.T) {
	t.Helper()
	vars := []*time.Duration{&probeFirstCheck, &probeDeadline, &probeWizardWait, &probeInterrupt, &probeExitWait, &probeSignalWait}
	short := []time.Duration{200 * time.Millisecond, 2 * time.Second, 500 * time.Millisecond, 100 * time.Millisecond, 300 * time.Millisecond, 300 * time.Millisecond}
	for i, v := range vars {
		old := *v
		*v = short[i]
		t.Cleanup(func() { *v = old })
	}
}

// runFake probes the fake agy in mode and returns the plan, the error and the
// pids of the fake and its child.
func runFake(t *testing.T, ctx context.Context, mode string) (string, error, []int) {
	plan, err, pids, _ := runFakeKeys(t, ctx, mode)
	return plan, err, pids
}

// runFakeKeys is runFake that also returns every byte but Ctrl+C the fake read.
func runFakeKeys(t *testing.T, ctx context.Context, mode string) (string, error, []int, string) {
	t.Helper()
	pidFile := filepath.Join(t.TempDir(), "pids")
	keysFile := filepath.Join(t.TempDir(), "keys")
	env := append(os.Environ(), "BONSAI_FAKE_AGY="+mode, "BONSAI_FAKE_AGY_PIDS="+pidFile, "BONSAI_FAKE_AGY_KEYS="+keysFile)
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
	keys, _ := os.ReadFile(keysFile)
	return plan, err, pids, string(keys)
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

func TestProbePlanLogsEveryAttemptAndTheScreen(t *testing.T) {
	shortProbeTimings(t)
	path := filepath.Join(t.TempDir(), planLogName)
	logf := openProbeLog(path)
	plan, err, pids := runFake(t, withProbeLog(context.Background(), logf), "header-late")
	logf.Close()
	if err != nil || plan != "Google AI Pro" {
		t.Fatalf("plan = %q, err = %v", plan, err)
	}
	assertGone(t, pids)
	data, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	log := string(data)
	for _, want := range []string{"pty started", "attempt 1: no match", "found plan \"Google AI Pro\"", "me@example.com", "stop: "} {
		if !strings.Contains(log, want) {
			t.Errorf("log lacks %q:\n%s", want, log)
		}
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("log mode = %v, %v; want 0600", info.Mode().Perm(), err)
	}
}

func TestProbePlanAnswersTheFirstRunWizard(t *testing.T) {
	for mode, want := range map[string]string{
		"wizard":       "\r\x1b[B\x1b[C\r",
		"wizard-page2": "\x1b[B\x1b[C\r", // a home that stopped halfway
	} {
		t.Run(mode, func(t *testing.T) {
			shortProbeTimings(t)
			path := filepath.Join(t.TempDir(), planLogName)
			logf := openProbeLog(path)
			plan, err, pids, keys := runFakeKeys(t, withProbeLog(t.Context(), logf), mode)
			logf.Close()
			if err != nil || plan != "Google AI Pro" {
				t.Fatalf("plan = %q, %v", plan, err)
			}
			if keys != want {
				t.Fatalf("keys = %q, want %q", keys, want)
			}
			assertGone(t, pids)
			log, _ := os.ReadFile(path)
			if !strings.Contains(string(log), "wizard page 2 answered") || strings.Contains(string(log), "wizard page 1 answered") != (mode == "wizard") {
				t.Fatalf("log:\n%s", log)
			}
		})
	}
}

func TestProbePlanAnswersAWizardPageOnce(t *testing.T) {
	shortProbeTimings(t)
	plan, err, pids, keys := runFakeKeys(t, t.Context(), "wizard-stuck")
	if err == nil || err.Error() != "first-run wizard not completed (page 1)" || plan != "" {
		t.Fatalf("plan = %q, %v", plan, err)
	}
	if keys != "\r" {
		t.Fatalf("keys = %q, want one Enter", keys)
	}
	assertGone(t, pids)
}

// realFirstCheck puts back the real wait before the first look at the screen.
func realFirstCheck(t *testing.T) {
	old := probeFirstCheck
	probeFirstCheck = time.Second
	t.Cleanup(func() { probeFirstCheck = old })
}

func TestProbePlanLooksOnlyAfterTheFirstCheck(t *testing.T) {
	for mode, header := range map[string]time.Duration{"found": 150 * time.Millisecond, "header-late": 1500 * time.Millisecond} {
		t.Run(mode, func(t *testing.T) {
			shortProbeTimings(t)
			realFirstCheck(t)
			path := filepath.Join(t.TempDir(), planLogName)
			logf := openProbeLog(path)
			plan, err, pids := runFake(t, withProbeLog(t.Context(), logf), mode)
			logf.Close()
			if err != nil || plan != "Google AI Pro" {
				t.Fatalf("plan = %q, %v", plan, err)
			}
			assertGone(t, pids)
			// The first attempt comes after the first check and finds a header
			// painted before it; a later one is found within a poll or two.
			found := foundAt(t, path)
			if found < probeFirstCheck || found > max(probeFirstCheck, header)+500*time.Millisecond {
				t.Fatalf("plan found at %v", found)
			}
		})
	}
}

// foundAt reads when the probe log says the plan was found.
func foundAt(t *testing.T, path string) time.Duration {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		stamp, rest, ok := strings.Cut(line, " ")
		if ok && strings.Contains(rest, "found plan") {
			d, err := time.ParseDuration(stamp)
			if err != nil {
				t.Fatal(err)
			}
			return d
		}
	}
	t.Fatalf("no found line:\n%s", data)
	return 0
}
