//go:build !windows

package procstore

import (
	"os"
	"testing"
	"time"
)

func TestDarwinProcessMatches_Hardened(t *testing.T) {
	// dead PID -> false
	if processMatchesDarwin(0x7fffffff, time.Now()) {
		t.Fatal("expected dead PID to return false")
	}

	// zero start time (identity cannot be verified safely) -> false
	livePID := os.Getpid()
	if processMatchesDarwin(livePID, time.Time{}) {
		t.Fatal("expected zero start time to return false")
	}

	// live PID + clearly mismatched start metadata -> false
	mismatchedStart := time.Now().Add(-24 * time.Hour)
	if processMatchesDarwin(livePID, mismatchedStart) {
		t.Fatal("expected mismatched start time to return false")
	}
}
