package git

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"
)

func TestRetryRename(t *testing.T) {
	transient := errors.New("destination is open")
	permanent := errors.New("destination directory is missing")
	for _, tt := range []struct {
		name      string
		failures  int
		failure   error
		wantCalls int
		wantTime  time.Duration
	}{
		{name: "immediate success", wantCalls: 1},
		{name: "transient failures", failures: 5, failure: transient, wantCalls: 6, wantTime: 125 * time.Millisecond},
		{name: "deadline", failures: 100, failure: transient, wantCalls: 23, wantTime: time.Second},
		{name: "non-retryable", failures: 1, failure: permanent, wantCalls: 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			start := time.Unix(0, 0)
			now := start
			calls := 0
			var lastErr error
			var sleeps []time.Duration
			err := retryRename("temp", "destination", func(oldpath, newpath string) error {
				if oldpath != "temp" || newpath != "destination" {
					t.Fatalf("rename paths = %q, %q", oldpath, newpath)
				}
				calls++
				if calls <= tt.failures {
					lastErr = fmt.Errorf("attempt %d: %w", calls, tt.failure)
					return lastErr
				}
				return nil
			}, func(err error) bool { return errors.Is(err, transient) }, func() time.Time { return now }, func(delay time.Duration) {
				if delay <= 0 || delay > 50*time.Millisecond {
					t.Fatalf("invalid backoff %s", delay)
				}
				sleeps = append(sleeps, delay)
				now = now.Add(delay)
			})
			if calls != tt.wantCalls || now.Sub(start) != tt.wantTime {
				t.Fatalf("calls = %d, elapsed = %s; want %d, %s", calls, now.Sub(start), tt.wantCalls, tt.wantTime)
			}
			if calls <= tt.failures {
				if err != lastErr {
					t.Fatalf("error = %v, want last error %v", err, lastErr)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			if tt.name == "transient failures" {
				want := []time.Duration{5 * time.Millisecond, 10 * time.Millisecond, 20 * time.Millisecond, 40 * time.Millisecond, 50 * time.Millisecond}
				if !reflect.DeepEqual(sleeps, want) {
					t.Fatalf("backoff = %v, want %v", sleeps, want)
				}
			}
		})
	}
}
