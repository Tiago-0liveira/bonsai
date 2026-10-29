package procstore

import (
	"io"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestProcessMatchesRejectsClearlyDifferentStartTime(t *testing.T) {
	pid := os.Getpid()
	if !PidAlive(pid) {
		t.Fatalf("current process PID %d should be alive", pid)
	}
	if ProcessMatches(pid, time.Now().Add(-2*time.Hour), "") {
		t.Fatalf("PID %d matched a start time from two hours ago", pid)
	}
}

// TestHelperProcessWait is the body of the child started by startWaitingChild.
func TestHelperProcessWait(t *testing.T) {
	if os.Getenv("BONSAI_PROCSTORE_HELPER") != "1" {
		t.Skip("helper process only")
	}
	_, _ = io.Copy(io.Discard, os.Stdin)
}

// startWaitingChild starts a child that lives until the returned stop is
// called and reports when it was started.
func startWaitingChild(t *testing.T) (pid int, startedAt time.Time) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperProcessWait$")
	cmd.Env = append(os.Environ(), "BONSAI_PROCSTORE_HELPER=1")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	startedAt = time.Now()
	t.Cleanup(func() {
		_ = stdin.Close()
		_ = cmd.Wait()
	})
	return cmd.Process.Pid, startedAt
}

func TestProcessMatchesLiveChild(t *testing.T) {
	pid, startedAt := startWaitingChild(t)
	if !ProcessMatches(pid, startedAt, "") {
		t.Fatalf("live child PID %d did not match its recorded start", pid)
	}
	if ProcessMatches(pid, startedAt.Add(-time.Hour), "") {
		t.Fatalf("live child PID %d matched a start an hour earlier", pid)
	}
}

func TestProcessMatchesRejectsMissingStartMetadata(t *testing.T) {
	pid := os.Getpid()
	if ProcessMatches(pid, time.Time{}, "") {
		t.Fatalf("PID %d matched without persisted start metadata", pid)
	}
}
