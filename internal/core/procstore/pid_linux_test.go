//go:build linux

package procstore

import (
	"testing"
	"time"
)

func TestParseStatStartTicks(t *testing.T) {
	tests := []struct {
		name, stat string
		want       uint64
		wantErr    bool
	}{
		{"plain", "12 (sleep) S 1 12 12 0 -1 4194304 100 0 0 0 0 0 0 0 20 0 1 0 987654 1000 100 18446744073709551615 0", 987654, false},
		{"command with spaces and parens", "12 (a) b (c) S 1 12 12 0 -1 4194304 100 0 0 0 0 0 0 0 20 0 1 0 4242 1000 100 0", 4242, false},
		{"no command", "garbage", 0, true},
		{"truncated", "12 (x) S 1 2", 0, true},
	}
	for _, tt := range tests {
		got, err := parseStatStartTicks(tt.stat)
		if (err != nil) != tt.wantErr || got != tt.want {
			t.Errorf("%s: got %d, %v; want %d, err=%v", tt.name, got, err, tt.want, tt.wantErr)
		}
	}
}

func TestParseBootTime(t *testing.T) {
	got, err := parseBootTime([]byte("cpu  1 2 3\nbtime 1700000000\nprocesses 5\n"))
	if err != nil || !got.Equal(time.Unix(1700000000, 0)) {
		t.Fatalf("got %v, %v", got, err)
	}
	if _, err := parseBootTime([]byte("cpu 1\n")); err == nil {
		t.Error("missing btime accepted")
	}
}

// Regression: the /proc/<pid> mtime reflects when the directory was first
// inspected, so a live child looked up after the tolerance had passed was
// classified as lost.
func TestProcessMatchesLiveChildFirstInspectedLate(t *testing.T) {
	if testing.Short() {
		t.Skip("waits past the start tolerance")
	}
	pid, startedAt := startWaitingChild(t)
	time.Sleep(processStartTolerance + time.Second)
	if !ProcessMatches(pid, startedAt, "") {
		t.Fatal("live original child classified as lost after a delayed first inspection")
	}
}
