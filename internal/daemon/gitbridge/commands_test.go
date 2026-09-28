package gitbridge

import (
	"context"
	"encoding/json"
	domain "github.com/Tiago-0liveira/bonsai/internal/git"
	"github.com/Tiago-0liveira/bonsai/internal/git/local"
	store "github.com/Tiago-0liveira/bonsai/internal/storage/git"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func repository(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{{"init", "-b", "main"}, {"config", "user.email", "test@example.com"}, {"config", "user.name", "Test"}, {"commit", "--allow-empty", "-m", "initial"}} {
		c := exec.Command("git", args...)
		c.Dir = dir
		if b, e := c.CombinedOutput(); e != nil {
			t.Fatalf("%v %s", e, b)
		}
	}
	return dir
}
func TestCommandsDedupeAcrossRestartAndBindTargets(t *testing.T) {
	ctx := context.Background()
	a, b := repository(t), repository(t)
	svc, e := local.New([]local.Config{{ID: "a", Root: a, WorktreeRoot: t.TempDir()}, {ID: "b", Root: b, WorktreeRoot: t.TempDir()}})
	if e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(t.TempDir(), "journal.json")
	db, _ := store.Open(path)
	x := &Executor{Local: svc, Journal: db}
	command := Command{ID: "create", UserID: "user", RepositoryID: "a", Type: "git.worktree.create", Arguments: MarshalArguments(map[string]string{"mode": "new", "branch": "test", "base": "main"}), CreatedAt: time.Now().UTC()}
	result := x.Execute(ctx, command)
	if result.Error != nil {
		t.Fatal(result.Error)
	}
	var wt domain.Worktree
	json.Unmarshal(result.Payload, &wt)
	again := x.Execute(ctx, command)
	if string(again.Payload) != string(result.Payload) {
		t.Fatal("duplicate changed result")
	}
	command.Arguments = MarshalArguments(map[string]string{"mode": "new", "branch": "other", "base": "main"})
	if r := x.Execute(ctx, command); r.Error == nil || r.Error.Code != "invalid" {
		t.Fatal(r)
	}
	remove := Command{ID: "remove", UserID: "user", RepositoryID: "a", WorktreeID: wt.ID, Type: "git.worktree.remove", Arguments: json.RawMessage(`{}`), CreatedAt: time.Now().UTC()}
	if r := x.Execute(ctx, remove); r.Error != nil {
		t.Fatal(r)
	}
	reopened, _ := store.Open(path)
	x = &Executor{Local: svc, Journal: reopened}
	if r := x.Execute(ctx, remove); r.Error != nil {
		t.Fatalf("retry after delete and restart: %+v", r)
	}
	bad := Command{ID: "read", UserID: "user", RepositoryID: "a", WorktreeID: local.ID("b", b), Type: "git.file.read", Arguments: json.RawMessage(`{"path":"secret"}`), CreatedAt: time.Now().UTC()}
	if r := x.Execute(ctx, bad); r.Error == nil || r.Error.Code != "forbidden" {
		t.Fatal("cross-repository read", r)
	}
	bad.Type = "shell"
	if r := x.Execute(ctx, bad); r.Error == nil {
		t.Fatal("shell allowed")
	}
	os.WriteFile(filepath.Join(a, "x"), []byte("x"), 0600)
}
