//go:build !windows

package plan

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
)

func TestReadPlanFileDoesNotBlockOnAFifo(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plan.md")
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Skip("mkfifo unavailable")
	}
	done := make(chan error, 1)
	go func() { _, err := readPlanFile(path); done <- err }()
	select {
	case err := <-done:
		if !errors.Is(err, ErrPlanFileInvalid) {
			t.Fatalf("fifo = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("reading a FIFO blocked")
	}
}

func TestPlanDirectoriesMustBePrivate(t *testing.T) {
	f := newFixture(t)
	a := f.start()
	f.writePlan(a, "# Plan", time.Hour)
	if info := f.svc.InspectPlanFile(a, idle()); info.State != PlanFileReady {
		t.Fatalf("private tree = %+v", info)
	}
	root, _ := config.GardenPlansRoot()
	repoKey := filepath.Dir(filepath.Dir(filepath.Dir(a.PlanPath)))
	for _, dir := range []string{root, repoKey} {
		if err := os.Chmod(dir, 0o777); err != nil {
			t.Fatal(err)
		}
		if info := f.svc.InspectPlanFile(a, idle()); info.State != PlanFileInvalid {
			t.Fatalf("%s writable by others = %+v", dir, info)
		}
		if _, _, err := f.svc.Plan(context.Background(), a.ID); !errors.Is(err, ErrPlanFileInvalid) {
			t.Fatalf("Plan under %s = %v", dir, err)
		}
		if err := os.Chmod(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
}
