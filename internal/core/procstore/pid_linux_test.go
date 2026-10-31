//go:build linux

package procstore

import (
	"testing"
	"time"
)

func TestProcessMatchesStartIdentity(t *testing.T) {
	pid, startedAt := startWaitingChild(t)
	ticks, bootID := StartIdentity(pid)
	if ticks == 0 || bootID == "" {
		t.Fatal("live child has no readable start identity")
	}
	tests := []struct {
		name      string
		startedAt time.Time
		ticks     uint64
		bootID    string
		want      bool
	}{
		{"exact identity", startedAt, ticks, bootID, true},
		{"clock stepped forward", startedAt.Add(time.Hour), ticks, bootID, true},
		{"clock stepped backward", startedAt.Add(-time.Hour), ticks, bootID, true},
		{"different ticks", startedAt, ticks + 1, bootID, false},
		{"different boot", startedAt, ticks, bootID + "-other", false},
		{"legacy within tolerance", startedAt, 0, "", true},
		{"legacy outside tolerance", startedAt.Add(time.Hour), 0, "", false},
		{"ticks without boot within tolerance", startedAt, ticks, "", true},
		{"ticks without boot outside tolerance", startedAt.Add(time.Hour), ticks, "", false},
		{"ticks without boot ignores ticks", startedAt, ticks + 1, "", true},
		{"boot without ticks uses legacy", startedAt, 0, bootID + "-other", true},
		{"missing start metadata", time.Time{}, ticks, bootID, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ProcessMatches(pid, tt.startedAt, tt.ticks, tt.bootID, ""); got != tt.want {
				t.Fatalf("ProcessMatches = %v, want %v", got, tt.want)
			}
		})
	}
	for _, pid := range []int{0, -1} {
		if ticks, bootID := StartIdentity(pid); ticks != 0 || bootID != "" {
			t.Errorf("StartIdentity(%d) = (%d, %q), want (0, empty)", pid, ticks, bootID)
		}
		if ProcessMatches(pid, startedAt, ticks, bootID, "") {
			t.Errorf("PID %d matched without readable metadata", pid)
		}
	}
}

func TestStartIdentityBootIDUnavailable(t *testing.T) {
	pid, startedAt := startWaitingChild(t)
	ticks, bootID := StartIdentity(pid)
	if ticks == 0 || bootID == "" {
		t.Fatal("live child has no readable start identity")
	}
	original := bootIDSource
	t.Cleanup(func() { bootIDSource = original })
	bootIDSource = func() string { return "" }
	if ticks, bootID := StartIdentity(pid); ticks != 0 || bootID != "" {
		t.Fatalf("StartIdentity = (%d, %q), want (0, empty)", ticks, bootID)
	}
	if ProcessMatches(pid, startedAt, ticks, bootID, "") {
		t.Fatal("matched a boot-scoped identity without a readable current boot ID")
	}
	if !ProcessMatches(pid, startedAt, ticks, "", "") {
		t.Fatal("legacy record did not use the time check when the current boot ID was unreadable")
	}
}

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
	if !ProcessMatches(pid, startedAt, 0, "", "") {
		t.Fatal("live original child classified as lost after a delayed first inspection")
	}
}
