package exec

import (
	"testing"
	"time"
)

// waitDone polls until p reports Done or the deadline passes.
func waitDone(p *Process, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if p.Done() {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return p.Done()
}

func TestKillByIDLeavesSiblings(t *testing.T) {
	m := NewManager()
	dir := t.TempDir()

	long, err := m.Spawn(dir, "long", "sleep 30")
	if err != nil {
		t.Fatal(err)
	}
	short, err := m.Spawn(dir, "short", "true")
	if err != nil {
		t.Fatal(err)
	}

	m.KillByID(dir, long.ID)
	if !waitDone(long, time.Second) {
		t.Fatal("killed process still running")
	}
	// The sibling must remain listed.
	if _, ok := m.GetByID(dir, short.ID); !ok {
		t.Fatal("sibling process was removed")
	}
	if len(m.List(dir)) != 2 {
		t.Fatalf("List len = %d, want 2", len(m.List(dir)))
	}
}

func TestRestartRespawns(t *testing.T) {
	m := NewManager()
	dir := t.TempDir()

	p, err := m.Spawn(dir, "job", "sleep 30")
	if err != nil {
		t.Fatal(err)
	}
	np, err := m.Restart(dir, p.ID)
	if err != nil {
		t.Fatalf("Restart: %v", err)
	}
	if np.ID == p.ID {
		t.Fatal("restart reused the old ID")
	}
	if np.Label != "job" || np.Command != "sleep 30" {
		t.Fatalf("restart lost label/command: %q %q", np.Label, np.Command)
	}
	if !waitDone(p, time.Second) {
		t.Fatal("original process not killed by restart")
	}
	m.KillByID(dir, np.ID) // cleanup
}

func TestRemoveDropsFromList(t *testing.T) {
	m := NewManager()
	dir := t.TempDir()

	p, err := m.Spawn(dir, "job", "true")
	if err != nil {
		t.Fatal(err)
	}
	waitDone(p, time.Second)
	m.Remove(dir, p.ID)
	if _, ok := m.GetByID(dir, p.ID); ok {
		t.Fatal("removed process still present")
	}
	if len(m.List(dir)) != 0 {
		t.Fatalf("List len = %d, want 0", len(m.List(dir)))
	}
}
