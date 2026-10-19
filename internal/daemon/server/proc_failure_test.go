package server

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
)

func TestStartFailureDefersTerminalStateToOnExit(t *testing.T) {
	s := streamTestServer(t)
	mp := &managedProc{generation: 1, rec: &procstore.Record{
		ID: 1, Worktree: s.root, Program: filepath.Join(s.root, "missing-executable"),
		Status: procstore.StatusStarting,
		Policy: procstore.Policy{Mode: procstore.PolicyOnFailure, MaxRestarts: 2},
	}}
	if err := s.store.WriteRecord(mp.rec); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.procs[mp.rec.ID] = mp
	s.mu.Unlock()
	rec := s.snapshot(mp)
	for attempt := 1; attempt <= rec.Policy.MaxRestarts+1; attempt++ {
		mp.mu.Lock()
		gen := mp.generation
		mp.rec.Status = procstore.StatusStarting
		if err := s.store.WriteRecord(mp.rec); err != nil {
			mp.mu.Unlock()
			t.Fatal(err)
		}
		mp.mu.Unlock()

		// Inspect the exact boundary between start and onExit rather than
		// hoping a poll catches a transient terminal write.
		err := s.start(mp, gen)
		if err == nil || errors.Is(err, errGenerationMismatch) {
			t.Fatalf("start error = %v", err)
		}
		stored, readErr := s.store.ReadRecord(rec.ID)
		if readErr != nil || stored.Status != procstore.StatusStarting || s.snapshot(mp).Status != procstore.StatusStarting {
			t.Fatalf("start published a policy decision: %+v, %v", stored, readErr)
		}
		retry := attempt <= rec.Policy.MaxRestarts
		if retry {
			// Block timer callbacks at the registry until their generation is
			// invalidated, even if CI pauses after onExit schedules a retry.
			s.mu.Lock()
		}
		s.onExit(mp, err, time.Now(), make(chan struct{}), gen)
		if retry {
			mp.mu.Lock()
			if mp.restartTimer != nil {
				mp.restartTimer.Stop()
			}
			mp.restartTimer = nil
			mp.generation++
			mp.mu.Unlock()
			s.mu.Unlock()
		}
		rec = s.snapshot(mp)
		want := procstore.StatusBackoff
		if !retry {
			want = procstore.StatusFailed
		}
		if rec.Status != want || rec.ExitError != err.Error() || rec.ExitCode == nil || *rec.ExitCode != -1 {
			t.Fatalf("attempt %d: %+v, want %s with launch diagnostics", attempt, rec, want)
		}
	}
	if rec.RetryAt != nil || rec.RetryCount != rec.Policy.MaxRestarts {
		t.Fatalf("terminal record has pending retries: %+v", rec)
	}
	s.mu.Lock()
	active := s.activeCountLocked()
	s.mu.Unlock()
	if active != 0 {
		t.Fatalf("terminal process counted as active: %d", active)
	}
	data, err := s.store.ReadCombinedLog(rec.ID)
	if err != nil || strings.Count(string(data), "failed to start · ") != 3 {
		t.Fatalf("want one launch failure marker per attempt: %q, %v", data, err)
	}
}

func TestStartFailureGenerationInvalidationKeepsStoppedState(t *testing.T) {
	s := streamTestServer(t)
	mp := &managedProc{generation: 1, rec: &procstore.Record{
		ID: 1, Worktree: s.root, Program: filepath.Join(s.root, "missing-executable"),
		Status: procstore.StatusStarting,
		Policy: procstore.Policy{Mode: procstore.PolicyOnFailure, MaxRestarts: 2},
	}}
	s.procs[1] = mp
	err := s.start(mp, 1)
	if err == nil || errors.Is(err, errGenerationMismatch) {
		t.Fatalf("start error = %v", err)
	}
	if !s.killManaged(mp) {
		t.Fatal("kill did not invalidate the failed launch")
	}
	before := s.snapshot(mp)
	done := make(chan struct{})
	s.onExit(mp, err, time.Now(), done, 1)
	select {
	case <-done:
	default:
		t.Fatal("stale exit notification was not completed")
	}
	if err := s.start(mp, 1); !errors.Is(err, errGenerationMismatch) {
		t.Fatalf("stale start error = %v", err)
	}
	after, err := s.store.ReadRecord(1)
	if err != nil || after.Status != procstore.StatusStopped || after.Revision != before.Revision || after.Attempt != before.Attempt {
		t.Fatalf("stale callbacks changed the stopped record: %+v, %v", after, err)
	}
}
