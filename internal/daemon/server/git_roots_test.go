package server_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	coregit "github.com/Tiago-0liveira/bonsai/internal/core/git"
	"github.com/Tiago-0liveira/bonsai/internal/daemon/gitbridge"
)

func TestBrowserCreationReadsCurrentRootsThroughDaemon(t *testing.T) {
	c, root := newDaemon(t)
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v %s", err, out)
		}
	}
	run("init", "-b", "main")
	run("-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "--allow-empty", "-m", "initial")
	settings, err := config.ProjectRootsPath()
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := config.UpdateProjectRoots(settings, "add", 0, root, "")
	if err != nil {
		t.Fatal(err)
	}
	create := func(branch string) *gitbridge.Result {
		t.Helper()
		args, _ := json.Marshal(map[string]string{"mode": "new", "branch": branch, "base": "main"})
		result, err := c.Git(gitbridge.Command{ID: branch, UserID: "local-browser", RepositoryID: "local", Type: "git.worktree.create", Arguments: args, CreatedAt: time.Now().UTC()})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	first := create("first")
	if first.Error != nil {
		t.Fatal(first.Error)
	}
	trees, err := coregit.ListWorktrees(root)
	if err != nil || len(trees) != 2 {
		t.Fatal(trees, err)
	}
	canonical, err := config.CanonicalDirectory(root)
	if err != nil {
		t.Fatal(err)
	}
	expected := filepath.Join(canonical, ".bonsai", "worktrees", config.ProjectID(canonical))
	if !config.ContainsPath(expected, trees[1].Path) {
		t.Fatal(trees[1].Path, expected)
	}
	// A changed explicit YAML root is read without restarting the daemon.
	if err := os.WriteFile(filepath.Join(root, ".bonsai.yaml"), []byte("worktree:\n  root: alternate\n"), 0600); err != nil {
		t.Fatal(err)
	}
	second := create("second")
	if second.Error != nil {
		t.Fatal(second.Error)
	}
	trees, err = coregit.ListWorktrees(root)
	if err != nil || len(trees) != 3 {
		t.Fatal(trees, err)
	}
	found := false
	for _, tree := range trees {
		if tree.Branch == "second" {
			found = config.ContainsPath(filepath.Join(canonical, "alternate"), tree.Path)
		}
	}
	if !found {
		t.Fatal("did not re-read placement", trees)
	}
	if _, err = config.UpdateProjectRoots(settings, "remove", cfg.Revision, "", cfg.Roots[0].ID); err != nil {
		t.Fatal(err)
	}
	if result := create("after-removal"); result.Error == nil {
		t.Fatal("creation allowed after root removal")
	}
	trees, err = coregit.ListWorktrees(root)
	if err != nil || len(trees) != 3 {
		t.Fatal("existing trees lost", trees, err)
	}
}
