package server

import (
	"bytes"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
	"github.com/Tiago-0liveira/bonsai/internal/daemon/protocol"
)

func streamTestServer(t *testing.T) *Server {
	t.Helper()
	root := t.TempDir()
	store := procstore.New(root)
	if err := store.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	s := &Server{root: root, store: store, logCap: logCap, procs: map[int]*managedProc{}, nextID: 1, done: make(chan struct{})}
	t.Cleanup(func() {
		s.mu.Lock()
		if s.idleTimer != nil {
			s.idleTimer.Stop()
		}
		s.mu.Unlock()
		for _, mp := range s.procs {
			s.killManaged(mp)
		}
	})
	return s
}
func TestFailedExecutableRetainsIdentityDiagnosticsAndManualAttempts(t *testing.T) {
	s := streamTestServer(t)
	req := &protocol.Request{Worktree: s.root, Program: filepath.Join(s.root, "missing-executable"), Policy: &procstore.Policy{Mode: procstore.PolicyNo}}
	rec, err := s.spawn(req)
	if err != nil || rec == nil || rec.Status != procstore.StatusFailed || rec.ExitCode == nil || *rec.ExitCode != -1 {
		t.Fatalf("spawn: %+v, %v", rec, err)
	}
	frame, err := s.readProcessOutput(rec.ID, "", 0)
	if err != nil || !strings.Contains(string(frame.Data), "failed to start") {
		t.Fatalf("diagnostics: %+v, %v", frame, err)
	}
	first := frame.Offset
	gen := frame.Generation
	rec, err = s.restart(rec.ID)
	if err != nil || rec.Attempt != 2 {
		t.Fatalf("restart: %+v %v", rec, err)
	}
	next, err := s.readProcessOutput(rec.ID, gen, first)
	if err != nil || next.Gap || next.Generation != gen || !strings.Contains(string(next.Data), "restarted · attempt 2") {
		t.Fatalf("restart replay: %+v %v", next, err)
	}
	if next.Offset != first+int64(len(next.Data)) {
		t.Fatal("noncontiguous cursor")
	}
	stored, err := s.store.ReadRecord(rec.ID)
	if err != nil || stored.Revision != rec.Revision {
		t.Fatalf("persisted: %+v %v", stored, err)
	}
}
func TestStreamRotationGapReconnectAndDaemonReopen(t *testing.T) {
	s := streamTestServer(t)
	s.procs[1] = &managedProc{rec: &procstore.Record{ID: 1, Status: procstore.StatusRunning, Revision: 1}}
	path := s.store.LogPath(1)
	w, err := newLogWriter(path, 8, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	write := func(data string) {
		t.Helper()
		if _, err := w.Write([]byte(data)); err != nil {
			t.Fatal(err)
		}
	}
	write("aaaa")
	first, err := s.readProcessOutput(1, "", 0)
	if err != nil {
		t.Fatal(err)
	}
	write("bbbbbb")
	next, err := s.readProcessOutput(1, first.Generation, first.Offset)
	if err != nil || next.Gap || string(next.Data) != "bbbbbb" {
		t.Fatalf("rotation replay: %+v %v", next, err)
	}
	write("cccccc")
	write("dddddd")
	gap, err := s.readProcessOutput(1, first.Generation, first.Offset)
	if err != nil || !gap.Gap || string(gap.Data) != "ccccccdddddd" {
		t.Fatalf("eviction: %+v %v", gap, err)
	}
	_ = w.Close()
	// Opening another writer (a later attempt or a new daemon) preserves identity.
	reopened, err := newLogWriter(path, 8, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	_, _ = reopened.Write([]byte("ee"))
	tail, err := s.readProcessOutput(1, gap.Generation, gap.Offset)
	if err != nil || tail.Gap || string(tail.Data) != "ee" || tail.Generation != gap.Generation {
		t.Fatalf("reopen: %+v %v", tail, err)
	}
}
func TestProcessStreamDrainsBeforeTerminalAndCancelsSilentSubscription(t *testing.T) {
	s := streamTestServer(t)
	s.procs[1] = &managedProc{rec: &procstore.Record{ID: 1, Status: procstore.StatusDone, Revision: 3}}
	w, err := newLogWriter(s.store.LogPath(1), logCap, nil)
	if err != nil {
		t.Fatal(err)
	}
	output := bytes.Repeat([]byte("x"), 40<<10)
	_, _ = w.Write(output)
	_ = w.Close()
	first, err := s.readProcessOutput(1, "", 0)
	if err != nil || first.Record != nil || len(first.Data) != 32<<10 {
		t.Fatalf("premature terminal status: %+v %v", first, err)
	}
	tail, err := s.readProcessOutput(1, first.Generation, first.Offset)
	if err != nil || tail.Record == nil || tail.Record.Status != procstore.StatusDone || len(tail.Data) != 8<<10 {
		t.Fatalf("tail: %+v %v", tail, err)
	}
	a, b := net.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer a.Close()
		s.streamProcess(a, protocol.NewEncoder(a), &protocol.Request{ID: 1, Generation: tail.Generation, Offset: tail.Offset})
	}()
	if _, err := protocol.NewDecoder(b).ReadResponse(); err != nil {
		t.Fatal(err)
	}
	_ = b.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("silent stream did not cancel")
	}
}
func TestInvalidSpawnPolicyCreatesNoRecord(t *testing.T) {
	s := streamTestServer(t)
	for _, policy := range []procstore.Policy{{Mode: "bogus"}, {Mode: procstore.PolicyAlways, MaxRestarts: -1}, {Mode: procstore.PolicyAlways, MaxRestarts: 101}} {
		if _, err := s.spawn(&protocol.Request{Worktree: s.root, Program: "unused", Policy: &policy}); err == nil {
			t.Fatal("accepted invalid policy")
		}
	}
	if _, err := s.spawn(&protocol.Request{Worktree: s.root, Program: "unused", Args: []string{"invalid\x00argument"}}); err == nil {
		t.Fatal("accepted invalid OS argument")
	}
	records, _ := s.store.ListRecords()
	if len(records) != 0 {
		t.Fatalf("invalid requests persisted %+v", records)
	}
	if _, err := os.Stat(s.store.LogPath(1)); !os.IsNotExist(err) {
		t.Fatal("invalid request created output")
	}
}
