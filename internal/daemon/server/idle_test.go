package server

import (
	"testing"
	"time"
)

func TestIdleShutdown(t *testing.T) {
	for _, tc := range []struct {
		name    string
		timeout time.Duration
	}{
		{"enabled", 10 * time.Millisecond},
		{"disabled", 0},
		{"negative disables", -time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &Server{done: make(chan struct{}), idleTimeout: idleTimeout}
			s.SetIdleTimeout(tc.timeout)
			s.mu.Lock()
			s.armIdleLocked()
			s.mu.Unlock()
			t.Cleanup(func() {
				s.mu.Lock()
				defer s.mu.Unlock()
				if s.idleTimer != nil {
					s.idleTimer.Stop()
				}
			})

			if tc.timeout > 0 {
				select {
				case <-s.done:
				case <-time.After(time.Second):
					t.Fatal("idle server did not shut down")
				}
				return
			}

			select {
			case <-s.done:
				t.Fatal("idle server shut down with idle shutdown disabled")
			case <-time.After(50 * time.Millisecond):
			}
			s.Stop(false)
			select {
			case <-s.done:
			default:
				t.Fatal("explicit Stop did not shut down the server")
			}
		})
	}
}
