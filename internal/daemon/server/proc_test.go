package server

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
)

func TestFailedStartDefersTerminalStateUntilRetryDecision(t *testing.T) {
	for _, tc := range []struct {
		name       string
		failures   int
		wantStatus string
		wantActive int
	}{
		{name: "retry remaining", wantStatus: procstore.StatusBackoff, wantActive: 1},
		{name: "retries exhausted", failures: 1, wantStatus: procstore.StatusFailed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := streamTestServer(t)
			exe, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			mp := &managedProc{
				rec: &procstore.Record{
					ID: 1, Worktree: s.root, WorkingDir: filepath.Join(s.root, "missing-directory"),
					Program: exe, Status: procstore.StatusStarting,
					Policy: procstore.Policy{Mode: procstore.PolicyOnFailure, MaxRestarts: 1},
				},
				generation: 1, consecFails: tc.failures,
			}
			s.procs[1] = mp
			if err := s.store.WriteRecord(mp.rec); err != nil {
				t.Fatal(err)
			}

			// Observe the precise interval between a failed launch and onExit.
			// List/stream readers must not mistake it for an exhausted retry budget.
			startErr := s.start(mp, 1)
			if startErr == nil {
				t.Fatal("expected launch to fail in the missing working directory")
			}
			if rec := s.list()[0]; rec.Status != procstore.StatusStarting {
				t.Fatalf("published premature terminal status: %+v", rec)
			}
			stored, err := s.store.ReadRecord(1)
			if err != nil || stored.Status != procstore.StatusStarting {
				t.Fatalf("persisted premature terminal status: %+v, %v", stored, err)
			}

			s.onExit(mp, startErr, time.Now(), make(chan struct{}), 1)
			rec := s.snapshot(mp)
			if rec.Status != tc.wantStatus || rec.ExitError != startErr.Error() || rec.ExitCode == nil || *rec.ExitCode != -1 {
				t.Fatalf("retry decision lost status or diagnostics: %+v", rec)
			}
			stored, err = s.store.ReadRecord(1)
			if err != nil || stored.Status != tc.wantStatus || stored.ExitError != rec.ExitError {
				t.Fatalf("persisted retry decision: %+v, %v", stored, err)
			}
			s.mu.Lock()
			active := s.activeCountLocked()
			s.mu.Unlock()
			if active != tc.wantActive {
				t.Fatalf("active count = %d, want %d", active, tc.wantActive)
			}
		})
	}
}
