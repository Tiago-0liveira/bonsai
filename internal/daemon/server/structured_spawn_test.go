package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
	"github.com/Tiago-0liveira/bonsai/internal/daemon/protocol"
)

func TestStructuredSpawnHelper(t *testing.T) {
	sep := -1
	for i, arg := range os.Args {
		if arg == "--" {
			sep = i
			break
		}
	}
	if sep < 0 || sep+2 >= len(os.Args) {
		return
	}
	out := os.Args[sep+1]
	payload := os.Args[sep+2]
	cwd, err := os.Getwd()
	if err != nil {
		os.Exit(2)
	}
	if err := os.WriteFile(out, []byte(cwd+"\n"+payload), 0o644); err != nil {
		os.Exit(3)
	}
}

func TestStructuredSpawnKeepsWorktreeOwnerAndNestedWorkingDir(t *testing.T) {
	root := t.TempDir()
	working := filepath.Join(root, "apps", "web")
	if err := os.MkdirAll(working, 0o755); err != nil {
		t.Fatal(err)
	}
	store := procstore.New(root)
	if err := store.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	s := &Server{
		root:   root,
		store:  store,
		logCap: logCap,
		procs:  map[int]*managedProc{},
		nextID: 1,
		done:   make(chan struct{}),
	}
	t.Cleanup(func() {
		s.mu.Lock()
		if s.idleTimer != nil {
			s.idleTimer.Stop()
		}
		s.mu.Unlock()
	})

	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(root, "structured-result.txt")
	marker := filepath.Join(root, "MUST_NOT_EXIST")
	literal := "arg with spaces; touch " + marker
	req := &protocol.Request{
		Kind:       protocol.KindSpawn,
		Worktree:   root,
		WorkingDir: working,
		Branch:     "feat/test",
		Label:      "structured",
		Command:    "display only",
		Program:    exe,
		Args:       []string{"-test.run=^TestStructuredSpawnHelper$", "--", out, literal},
	}
	rec, err := s.spawn(req)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Worktree != root {
		t.Fatalf("record worktree = %q, want %q", rec.Worktree, root)
	}
	if rec.WorkingDir != working {
		t.Fatalf("record working dir = %q, want %q", rec.WorkingDir, working)
	}
	if rec.Program != exe || len(rec.Args) != len(req.Args) {
		t.Fatalf("structured record = %+v", rec)
	}

	deadline := time.Now().Add(3 * time.Second)
	for {
		if data, err := os.ReadFile(out); err == nil {
			parts := strings.SplitN(string(data), "\n", 2)
			if len(parts) != 2 {
				t.Fatalf("helper output = %q", string(data))
			}
			if filepath.Clean(parts[0]) != filepath.Clean(working) {
				t.Fatalf("helper cwd = %q, want %q", parts[0], working)
			}
			if parts[1] != literal {
				t.Fatalf("helper arg = %q, want %q", parts[1], literal)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("structured helper did not produce output")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("metacharacters were interpreted by a shell; marker err=%v", err)
	}

	list := s.list()
	if len(list) != 1 || list[0].Worktree != root || list[0].WorkingDir != working {
		t.Fatalf("listed record lost owner/cwd: %+v", list)
	}

	deadline = time.Now().Add(3 * time.Second)
	for {
		list = s.list()
		if len(list) == 1 && procstore.IsTerminal(list[0].Status) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("structured child did not reach a terminal state: %+v", list)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
