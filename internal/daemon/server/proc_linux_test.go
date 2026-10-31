//go:build linux

package server

import (
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
	"github.com/Tiago-0liveira/bonsai/internal/daemon/protocol"
)

func TestSpawnPersistsStartIdentity(t *testing.T) {
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
	ticks, bootID := procstore.StartIdentity(rec.PID)
	if rec.StartTicks == 0 || rec.BootID == "" || rec.StartTicks != ticks || rec.BootID != bootID {
		t.Fatalf("spawn did not capture the child's start identity: %+v", rec)
	}
	stored, err := s.store.ReadRecord(rec.ID)
	if err != nil || stored.StartTicks != rec.StartTicks || stored.BootID != rec.BootID {
		t.Fatalf("persisted identity: %+v, %v", stored, err)
	}
	group := s.serveSnapshot(&serveRuntime{ProcessIDs: map[string]int{"child": rec.ID}})
	if len(group.Processes) != 1 || group.Processes[0].StartTicks != rec.StartTicks || group.Processes[0].BootID != rec.BootID {
		t.Fatalf("serve snapshot lost process identity: %+v", group.Processes)
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
			ticks, bootID := procstore.StartIdentity(cmd.Process.Pid)
			rec := &procstore.Record{
				ID: 1, PID: cmd.Process.Pid, Worktree: s.root,
				StartedAt: time.Now().Add(shift), StartTicks: ticks, BootID: bootID,
				Status: procstore.StatusRunning,
			}
			if rec.StartTicks == 0 || rec.BootID == "" {
				t.Fatal("child has no readable start identity")
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
			if waitForProcessExit(rec.PID, rec.StartedAt, rec.StartTicks, rec.BootID, rec.Worktree, 0) {
				t.Fatal("live orphan reported stopped after clock step")
			}
		})
	}
}

func TestOrphanFromDifferentBoot(t *testing.T) {
	for _, operation := range []string{"adopt", "kill"} {
		t.Run(operation, func(t *testing.T) {
			s := streamTestServer(t)
			cmd := exec.Command("sleep", "30")
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
			})
			ticks, bootID := procstore.StartIdentity(cmd.Process.Pid)
			if ticks == 0 || bootID == "" {
				t.Fatal("child has no readable start identity")
			}
			rec := &procstore.Record{
				ID: 1, PID: cmd.Process.Pid, Worktree: s.root, StartedAt: time.Now(),
				StartTicks: ticks, BootID: bootID + "-other", Status: procstore.StatusOrphan,
			}
			if err := s.store.WriteRecord(rec); err != nil {
				t.Fatal(err)
			}
			if operation == "adopt" {
				s.adoptExisting()
			} else {
				mp := &managedProc{rec: rec}
				s.procs[rec.ID] = mp
				if !s.killManaged(mp) {
					t.Fatal("kill did not resolve the mismatched orphan")
				}
			}
			stored, err := s.store.ReadRecord(rec.ID)
			if err != nil || stored.Status != procstore.StatusLost {
				t.Fatalf("different-boot record was not marked lost: %+v, %v", stored, err)
			}
			if !procstore.ProcessMatches(rec.PID, rec.StartedAt, ticks, bootID, rec.Worktree) {
				t.Fatal("unrelated live child was killed")
			}
			if !waitForProcessExit(rec.PID, rec.StartedAt, rec.StartTicks, rec.BootID, rec.Worktree, 0) {
				t.Fatal("different-boot process was treated as the managed child")
			}
		})
	}
}

func TestStartClearsPreviousIdentityOnFailure(t *testing.T) {
	s := streamTestServer(t)
	mp := &managedProc{generation: 1, rec: &procstore.Record{
		ID: 1, Worktree: s.root, Program: filepath.Join(s.root, "missing-executable"),
		Status: procstore.StatusStarting, StartTicks: 123, BootID: "previous-boot",
	}}
	if err := s.start(mp, 1); err == nil {
		t.Fatal("missing executable started")
	}
	rec := s.snapshot(mp)
	if rec.StartTicks != 0 || rec.BootID != "" {
		t.Fatalf("failed start retained previous identity: %+v", rec)
	}
}
