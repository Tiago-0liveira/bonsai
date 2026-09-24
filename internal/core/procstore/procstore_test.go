package procstore

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRecordRoundTrip(t *testing.T) {
	root := t.TempDir()
	s := New(root)
	if err := s.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.Dir(), ".gitignore")); err != nil {
		t.Fatalf("gitignore not created: %v", err)
	}

	r := &Record{
		ID:        3,
		Label:     "dev",
		Command:   "npm run dev",
		Worktree:  "/wt/feat-x",
		PID:       4242,
		Status:    StatusRunning,
		Policy:    Policy{Mode: PolicyAlways, MaxRestarts: 5},
		StartedAt: time.Now().Truncate(time.Second),
	}
	if err := s.WriteRecord(r); err != nil {
		t.Fatal(err)
	}
	got, err := s.ReadRecord(3)
	if err != nil {
		t.Fatal(err)
	}
	if got.Label != "dev" || got.PID != 4242 || got.Policy.Mode != PolicyAlways {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
}

func TestListAndMaxID(t *testing.T) {
	s := New(t.TempDir())
	for _, id := range []int{1, 5, 2} {
		if err := s.WriteRecord(&Record{ID: id, Label: "x"}); err != nil {
			t.Fatal(err)
		}
	}
	recs, err := s.ListRecords()
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 3 || recs[0].ID != 1 || recs[2].ID != 5 {
		t.Fatalf("unexpected order/count: %+v", recs)
	}
	max, err := s.MaxID()
	if err != nil || max != 5 {
		t.Fatalf("MaxID = %d, %v", max, err)
	}

	if err := s.RemoveRecord(5); err != nil {
		t.Fatal(err)
	}
	if max, _ := s.MaxID(); max != 2 {
		t.Fatalf("after remove MaxID = %d", max)
	}
}

func TestTryLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.lock")
	l1, err := TryLock(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := TryLock(path); err != ErrLocked {
		t.Fatalf("second TryLock = %v, want ErrLocked", err)
	}
	if err := l1.Unlock(); err != nil {
		t.Fatal(err)
	}
	l2, err := TryLock(path)
	if err != nil {
		t.Fatalf("relock after unlock: %v", err)
	}
	_ = l2.Unlock()
}

func TestIndexRegisterDeregister(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	appData := t.TempDir()
	t.Setenv("AppData", appData)
	t.Setenv("APPDATA", appData)

	// A live daemon = this test process (its pid is alive) with an existing sock.
	sock := filepath.Join(t.TempDir(), "daemon.sock")
	if err := os.WriteFile(sock, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Register("/repo/a", sock, os.Getpid()); err != nil {
		t.Fatal(err)
	}
	list, err := ListDaemons()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Root != filepath.Clean("/repo/a") {
		t.Fatalf("unexpected list: %+v", list)
	}

	if err := Deregister("/repo/a"); err != nil {
		t.Fatal(err)
	}
	if list, _ := ListDaemons(); len(list) != 0 {
		t.Fatalf("expected empty after deregister, got %+v", list)
	}
}

func TestIndexPrunesStale(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	appData := t.TempDir()
	t.Setenv("AppData", appData)
	t.Setenv("APPDATA", appData)
	// pid 0x7fffffff is (almost certainly) not alive -> entry pruned on read.
	if err := Register("/repo/dead", "/nonexistent.sock", 0x7fffffff); err != nil {
		t.Fatal(err)
	}
	if list, _ := ListDaemons(); len(list) != 0 {
		t.Fatalf("stale entry not pruned: %+v", list)
	}
}
