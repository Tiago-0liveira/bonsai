package gardenbackend

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGitWorkInspectReadsHeadTreeDirtyAndDiffstat(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "none"))
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.invalid", "-c", "commit.gpgsign=false"}, args...)...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	git("init", "-q")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-q", "-m", "base")
	base := git("rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\ntwo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git("commit", "-q", "-am", "work")
	w, err := gitWork{}.Inspect(context.Background(), dir, base)
	if err != nil || w.Head != git("rev-parse", "HEAD") || w.Tree != git("rev-parse", "HEAD^{tree}") || w.Dirty || len(w.Files) != 1 || w.Files[0].Path != "a.txt" || w.Totals.Add != 1 {
		t.Fatalf("clean = %+v %v", w, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "untracked.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if w, err = (gitWork{}).Inspect(context.Background(), dir, base); err != nil || !w.Dirty {
		t.Fatalf("untracked = %+v %v", w, err)
	}
	// Inherited locators must not redirect the read to another repository.
	t.Setenv("GIT_DIR", filepath.Join(t.TempDir(), "bogus"))
	t.Setenv("GIT_WORK_TREE", t.TempDir())
	t.Setenv("GIT_INDEX_FILE", filepath.Join(t.TempDir(), "index"))
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "core.bare")
	t.Setenv("GIT_CONFIG_VALUE_0", "true")
	if w, err = (gitWork{}).Inspect(context.Background(), dir, base); err != nil || w.Head != git2(t, dir, "rev-parse", "HEAD") || !w.Dirty {
		t.Fatalf("scrubbed env = %+v %v", w, err)
	}
	if _, err = (gitWork{}).Inspect(context.Background(), dir, "--output=x"); err == nil {
		t.Fatal("option-like base accepted")
	}
}

// git2 runs git with the redirecting variables removed, so it can verify what the seam should have read.
func git2(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "GIT_") {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}
