package gym

import (
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func initTestGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "-b", "main")
	runGit(t, dir, "config", "user.email", "test@test.com")
	runGit(t, dir, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# test"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "init")
	return dir
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v: %s", args, err, out)
	}
}

func TestStoreBindingLifecycle(t *testing.T) {
	repoDir := initTestGitRepo(t)
	store, err := NewStore(repoDir)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	if store.RepoID() == "" {
		t.Fatal("expected non-empty RepoID")
	}

	ws, err := store.ResolveWorkspaceIdentity(repoDir)
	if err != nil {
		t.Fatalf("ResolveWorkspaceIdentity: %v", err)
	}
	if ws.WorktreeID == "" {
		t.Fatal("expected non-empty WorktreeID")
	}

	// Verify idempotency of workspace resolution
	ws2, err := store.ResolveWorkspaceIdentity(repoDir)
	if err != nil {
		t.Fatalf("ResolveWorkspaceIdentity second call: %v", err)
	}
	if ws.WorktreeID != ws2.WorktreeID {
		t.Fatalf("expected identical WorktreeID: %s vs %s", ws.WorktreeID, ws2.WorktreeID)
	}

	binding := &RunBinding{
		SchemaVersion: CurrentSchemaVersion,
		Workspace:     *ws,
		RequestID:     "req-1",
		RunID:         "run-1",
		CreatedAt:     time.Now(),
		Submission:    SubmissionAcknowledged,
	}

	if err := store.SaveBinding(binding); err != nil {
		t.Fatalf("SaveBinding failed: %v", err)
	}

	loaded, err := store.GetBinding(ws.WorktreeID)
	if err != nil {
		t.Fatalf("GetBinding failed: %v", err)
	}
	if loaded == nil || loaded.RunID != "run-1" {
		t.Fatalf("unexpected loaded binding: %+v", loaded)
	}

	list, err := store.ListBindings()
	if err != nil {
		t.Fatalf("ListBindings failed: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 binding in list, got %d", len(list))
	}

	if err := store.ArchiveBinding(binding); err != nil {
		t.Fatalf("ArchiveBinding failed: %v", err)
	}

	activeAfterArchive, err := store.GetBinding(ws.WorktreeID)
	if err != nil {
		t.Fatalf("GetBinding after archive failed: %v", err)
	}
	if activeAfterArchive != nil {
		t.Fatalf("expected nil active binding after archive, got %+v", activeAfterArchive)
	}
}

func TestArchivingOldRunPreservesNewBinding(t *testing.T) {
	repoDir := initTestGitRepo(t)
	store, err := NewStore(repoDir)
	if err != nil {
		t.Fatal(err)
	}
	ws, err := store.ResolveWorkspaceIdentity(repoDir)
	if err != nil {
		t.Fatal(err)
	}
	old := &RunBinding{SchemaVersion: CurrentSchemaVersion, Workspace: *ws,
		RunID: "run-old", RequestID: "req-old", Submission: SubmissionAcknowledged}
	newBinding := &RunBinding{SchemaVersion: CurrentSchemaVersion, Workspace: *ws,
		RunID: "run-new", RequestID: "req-new", Submission: SubmissionAcknowledged}
	if err := store.SaveBinding(newBinding); err != nil {
		t.Fatal(err)
	}
	if err := store.ArchiveBinding(old); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetBinding(ws.WorktreeID)
	if err != nil || got == nil || got.RunID != "run-new" {
		t.Fatalf("new binding overwritten: %+v, %v", got, err)
	}
}

func TestStoreAdvisoryLock(t *testing.T) {
	repoDir := initTestGitRepo(t)
	store, err := NewStore(repoDir)
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}

	var wg sync.WaitGroup
	counter := 0

	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = store.WithLock(func() error {
				val := counter
				time.Sleep(5 * time.Millisecond)
				counter = val + 1
				return nil
			})
		}()
	}
	wg.Wait()

	if counter != 5 {
		t.Errorf("expected counter 5 after serialized locks, got %d", counter)
	}
}
