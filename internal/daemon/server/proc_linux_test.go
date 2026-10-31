//go:build linux

package server

import (
	"os/exec"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
	"github.com/Tiago-0liveira/bonsai/internal/daemon/protocol"
)

func TestSpawnPersistsStartTicks(t *testing.T) {
	s := streamTestServer(t)
	rec, err := s.spawn(&protocol.Request{
		Worktree: s.root, Program: "sleep", Args: []string{"5"},
		Policy: &procstore.Policy{Mode: procstore.PolicyNo},
	})
	if err != nil {
		t.Fatal(err)
	}
	mp := s.procs[rec.ID]
	t.Cleanup(func() {
		s.killManaged(mp)
		select {
		case <-mp.waitDone:
		case <-time.After(3 * time.Second):
			t.Error("spawned child did not exit")
		}
	})
	if rec.StartTicks == 0 || rec.StartTicks != procstore.StartTicks(rec.PID) {
		t.Fatalf("spawn did not capture the child's start ticks: %+v", rec)
	}
	stored, err := s.store.ReadRecord(rec.ID)
	if err != nil || stored.StartTicks != rec.StartTicks {
		t.Fatalf("persisted ticks: %+v, %v", stored, err)
	}
}

func TestAdoptExistingAfterClockStep(t *testing.T) {
	for _, shift := range []time.Duration{-time.Hour, time.Hour} {
		t.Run(shift.String(), func(t *testing.T) {
			s := streamTestServer(t)
			cmd := exec.Command("sleep", "5")
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
			})
			rec := &procstore.Record{
				ID: 1, PID: cmd.Process.Pid, Worktree: s.root,
				StartedAt: time.Now().Add(shift), StartTicks: procstore.StartTicks(cmd.Process.Pid),
				Status: procstore.StatusRunning,
			}
			if rec.StartTicks == 0 {
				t.Fatal("child has no readable start ticks")
			}
			if err := s.store.WriteRecord(rec); err != nil {
				t.Fatal(err)
			}
			s.adoptExisting()
			list := s.list()
			if len(list) != 1 || list[0].Status != procstore.StatusOrphan {
				t.Fatalf("live child was lost after clock step: %+v", list)
			}
			s.mu.Lock()
			active := s.activeCountLocked()
			s.mu.Unlock()
			if active != 1 {
				t.Fatalf("active count = %d, want 1", active)
			}
			if waitForProcessExit(rec.PID, rec.StartedAt, rec.StartTicks, rec.Worktree, 0) {
				t.Fatal("live orphan reported stopped after clock step")
			}
		})
	}
}
