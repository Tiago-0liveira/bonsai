package procstore

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCommandIdentityBoundariesAndOwnership(t *testing.T) {
	base := Record{ID: 1, Worktree: "/repo/a", WorkingDir: "/repo/a/pkg", Program: "node", Args: []string{"a b", "c"}}
	key := CommandIdentity(&base)
	for _, alter := range []func(*Record){
		func(r *Record) { r.Worktree = "/repo/b" },
		func(r *Record) { r.WorkingDir = "/repo/a/other" },
		func(r *Record) { r.Args = []string{"a", "b c"} },
		func(r *Record) { r.Args = []string{"c", "a b"} },
		func(r *Record) { r.ServeName = "api" },
	} {
		r := base
		alter(&r)
		if CommandIdentity(&r) == key {
			t.Fatal("distinct invocation collapsed")
		}
	}
	copy := base
	copy.ID = 99
	copy.Label = "different"
	copy.Environment = map[string]string{"SECRET": "private"}
	copy.Revision = 42
	if CommandIdentity(&copy) != key {
		t.Fatal("transport or environment changed command identity")
	}
	a := Record{Worktree: "/repo", Command: "echo 'a b'"}
	b := a
	b.Command = "echo a\\ b"
	if CommandIdentity(&a) == CommandIdentity(&b) {
		t.Fatal("legacy shell commands must be exact")
	}
}

func TestExecutionCounterAndDurableRemoval(t *testing.T) {
	s := New(t.TempDir())
	if err := s.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	legacy := &Record{ID: 8, Worktree: s.Root(), Command: "echo ok"}
	if err := s.WriteRecord(legacy); err != nil {
		t.Fatal(err)
	}
	order, err := s.NextExecutionOrder()
	if err != nil || order != 9 {
		t.Fatalf("legacy order: %d %v", order, err)
	}
	r := *legacy
	r.ID = 9
	r.ExecutionOrder = order
	if err := s.WriteRecord(&r); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"", ".1", ".cursor"} {
		if err := os.WriteFile(s.LogPath(r.ID)+suffix, []byte("retained"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.BeginRemoval(&r); err != nil {
		t.Fatal(err)
	}
	// Simulate a cleanup failure. Published authority must still retain the run.
	if err := os.Remove(s.LogPath(r.ID) + ".1"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(s.LogPath(r.ID)+".1", 0700); err != nil {
		t.Fatal(err)
	}
	blocker := filepath.Join(s.LogPath(r.ID)+".1", "blocked")
	if err := os.WriteFile(blocker, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.CompleteRemoval(&r); err == nil {
		t.Fatal("cleanup failure swallowed")
	}
	if _, err := s.ReadRecord(r.ID); err != nil {
		t.Fatal("retry metadata lost", err)
	}
	v, err := s.ReadVisibility()
	if err != nil || v.Deleted[r.ID] {
		t.Fatal("failed removal published as complete")
	}
	if err := os.Remove(blocker); err != nil {
		t.Fatal(err)
	}
	if err := New(s.Root()).RecoverRemovals(); err != nil {
		t.Fatal(err)
	}
	v, err = s.ReadVisibility()
	if err != nil || !v.Deleted[r.ID] || v.Cutoffs[CommandIdentity(&r)] != order {
		t.Fatalf("authority: %+v %v", v, err)
	}
	for _, path := range []string{s.RecordPath(r.ID), s.LogPath(r.ID), s.LogPath(r.ID) + ".1", s.LogPath(r.ID) + ".cursor"} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("artifact survived %s: %v", path, err)
		}
	}
	next, err := New(s.Root()).NextExecutionOrder()
	if err != nil || next <= order {
		t.Fatalf("counter reused: %d %v", next, err)
	}
	if err := s.CompleteRemoval(&r); err != nil {
		t.Fatal("idempotent cleanup", err)
	}
}

func TestVisibilityFailureRetainsFullRetryMetadataAfterRecordCleanup(t *testing.T) {
	s := New(t.TempDir())
	if err := s.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	r := &Record{ID: 1, ExecutionOrder: 10, Worktree: s.Root(), WorkingDir: s.Root(), Program: "node", Args: []string{"space argument"}, Environment: map[string]string{"PRIVATE": "value"}, Status: StatusStopped}
	if err := s.WriteRecord(r); err != nil {
		t.Fatal(err)
	}
	if err := s.BeginRemoval(r); err != nil {
		t.Fatal(err)
	}
	blocker := filepath.Join(s.Dir(), "process-visibility.json.tmp")
	if err := os.Mkdir(blocker, 0700); err != nil {
		t.Fatal(err)
	}
	if err := s.CompleteRemoval(r); err == nil {
		t.Fatal("visibility persistence failure swallowed")
	}
	if _, err := s.ReadRecord(r.ID); !os.IsNotExist(err) {
		t.Fatalf("expected cleanup to reach metadata: %v", err)
	}
	pending, err := New(s.Root()).PendingRemovalRecords()
	if err != nil || len(pending) != 1 || pending[0].Args[0] != "space argument" || pending[0].Environment["PRIVATE"] != "value" {
		t.Fatalf("retry metadata lost: %+v %v", pending, err)
	}
	v, err := s.ReadVisibility()
	if err != nil || v.Deleted[r.ID] || v.Cutoffs[CommandIdentity(r)] != 0 {
		t.Fatalf("failed removal published: %+v %v", v, err)
	}
	if err := os.Remove(blocker); err != nil {
		t.Fatal(err)
	}
	if err := New(s.Root()).RecoverRemovals(); err != nil {
		t.Fatal(err)
	}
	v, err = s.ReadVisibility()
	if err != nil || !v.Deleted[r.ID] {
		t.Fatalf("retry did not complete: %+v %v", v, err)
	}
	pending, err = s.PendingRemovalRecords()
	if err != nil || len(pending) != 0 {
		t.Fatalf("retry journal survived: %+v %v", pending, err)
	}
}
