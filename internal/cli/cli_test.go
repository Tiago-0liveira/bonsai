package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func initRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	repo := filepath.Join(dir, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"},
		{"config", "user.email", "t@t.t"},
		{"config", "user.name", "t"},
	} {
		c := exec.Command("git", args...)
		c.Dir = repo
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(repo, ".env"), []byte("SECRET"), 0o644); err != nil {
		t.Fatal(err)
	}
	add := exec.Command("git", "add", "-A")
	add.Dir = repo
	if out, err := add.CombinedOutput(); err != nil {
		t.Fatalf("git add: %v: %s", err, out)
	}
	commit := exec.Command("git", "commit", "-qm", "init")
	commit.Dir = repo
	if out, err := commit.CombinedOutput(); err != nil {
		t.Fatalf("commit: %v: %s", err, out)
	}
	return repo
}

// runIn changes into repo, runs the subcommand, and restores the cwd.
func runIn(t *testing.T, repo string, args ...string) (string, error) {
	t.Helper()
	old, _ := os.Getwd()
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(old)

	var out, errb bytes.Buffer
	err := Run(args, &out, &errb)
	if err != nil {
		return errb.String(), err
	}
	return out.String(), nil
}

func TestCreateListCopyPath(t *testing.T) {
	repo := initRepo(t)

	// create a new-branch worktree
	out, err := runIn(t, repo, "create", "feat/x")
	if err != nil {
		t.Fatalf("create: %v (%s)", err, out)
	}
	wtPath := strings.TrimSpace(out)
	if _, err := os.Stat(wtPath); err != nil {
		t.Fatalf("worktree path not created: %v", err)
	}

	// list shows both worktrees
	out, err = runIn(t, repo, "list")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "feat/x") || !strings.Contains(out, "(main)") {
		t.Fatalf("list missing entries: %q", out)
	}

	// path resolves the branch
	out, err = runIn(t, repo, "path", "feat/x")
	if err != nil {
		t.Fatalf("path: %v", err)
	}
	if strings.TrimSpace(out) != wtPath {
		t.Fatalf("path = %q, want %q", strings.TrimSpace(out), wtPath)
	}

	// copy .env into the worktree
	if _, err := runIn(t, repo, "copy", ".env", "feat/x"); err != nil {
		t.Fatalf("copy: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(wtPath, ".env"))
	if err != nil {
		t.Fatalf("copied file: %v", err)
	}
	if string(data) != "SECRET" {
		t.Fatalf("copied content = %q", data)
	}
}

func TestHelpAndShellInitOutsideRepo(t *testing.T) {
	noRepo := t.TempDir()

	out, err := runIn(t, noRepo, "help")
	if err != nil {
		t.Fatalf("help outside repo: %v (%s)", err, out)
	}
	if !strings.Contains(out, "bonsai") {
		t.Errorf("help output looks wrong: %q", out)
	}

	out, err = runIn(t, noRepo, "shell-init")
	if err != nil {
		t.Fatalf("shell-init outside repo: %v (%s)", err, out)
	}
	if !strings.Contains(out, "bcd") {
		t.Errorf("shell-init output looks wrong: %q", out)
	}
}
