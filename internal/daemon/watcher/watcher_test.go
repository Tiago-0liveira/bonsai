package watcher

import (
	"context"
	domain "github.com/Tiago-0liveira/bonsai/internal/git"
	"github.com/Tiago-0liveira/bonsai/internal/git/local"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestDirtyContentChangesAndSemanticDedupe(t *testing.T) {
	root := t.TempDir()
	for _, args := range [][]string{{"init", "-b", "main"}, {"config", "user.name", "Test"}, {"config", "user.email", "test@example.com"}, {"commit", "--allow-empty", "-m", "initial"}} {
		c := exec.Command("git", args...)
		c.Dir = root
		if b, e := c.CombinedOutput(); e != nil {
			t.Fatalf("%v %s", e, b)
		}
	}
	svc, e := local.New([]local.Config{{ID: "repo", Root: root, WorktreeRoot: t.TempDir()}})
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := make(chan domain.RepositoryState, 10)
	w := Watcher{Local: svc, RepositoryID: "repo", Roots: []string{root}, Interval: 100 * time.Millisecond, Publish: func(_ context.Context, s domain.RepositoryState) error { out <- s; return nil }}
	go w.Run(ctx)
	next := func() domain.RepositoryState {
		t.Helper()
		select {
		case s := <-out:
			return s
		case <-time.After(3 * time.Second):
			t.Fatal("missing snapshot")
			return domain.RepositoryState{}
		}
	}
	next()
	os.WriteFile(filepath.Join(root, "file"), []byte("one"), 0600)
	first := next()
	os.WriteFile(filepath.Join(root, "file"), []byte("two"), 0600)
	second := next()
	if first.Worktrees[0].Status.ContentVersion == second.Worktrees[0].Status.ContentVersion {
		t.Fatal("dirty content changed without a revision")
	}
	select {
	case <-out:
		t.Fatal("unchanged periodic snapshot emitted")
	case <-time.After(250 * time.Millisecond):
	}
}
