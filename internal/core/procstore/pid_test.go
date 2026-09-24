package procstore

import (
	"os"
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

func TestProcessMatchesCurrentProcess(t *testing.T) {
	pid := os.Getpid()
	if !ProcessMatches(pid, time.Now(), "") {
		t.Fatalf("current process PID %d did not match recent start metadata", pid)
	}
}

func TestProcessMatchesRejectsMissingStartMetadata(t *testing.T) {
	pid := os.Getpid()
	if ProcessMatches(pid, time.Time{}, "") {
		t.Fatalf("PID %d matched without persisted start metadata", pid)
	}
}
