package local

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// absoluteGitDir returns the per-worktree git directory. A worktree's `.git`
// is either the directory itself or a one-line `gitdir: <path>` file, so the
// answer is on disk and needs no process. Anything unexpected, or an
// environment that redirects Git, falls back to asking Git.
func absoluteGitDir(ctx context.Context, dir string) (string, error) {
	if os.Getenv("GIT_DIR") == "" {
		if gd, ok := readGitDir(dir); ok {
			return gd, nil
		}
	}
	return trimmed(ctx, dir, "rev-parse", "--absolute-git-dir")
}

func readGitDir(dir string) (string, bool) {
	dotGit := filepath.Join(dir, ".git")
	info, err := os.Lstat(dotGit)
	if err != nil {
		return "", false
	}
	if info.IsDir() {
		return dotGit, true
	}
	if !info.Mode().IsRegular() {
		return "", false
	}
	f, err := os.Open(dotGit)
	if err != nil {
		return "", false
	}
	defer f.Close()
	buf, err := io.ReadAll(io.LimitReader(f, 4096))
	if err != nil {
		return "", false
	}
	line, _, _ := strings.Cut(string(buf), "\n")
	value, ok := strings.CutPrefix(strings.TrimSpace(line), "gitdir:")
	if !ok {
		return "", false
	}
	gd := strings.TrimSpace(value)
	if gd == "" {
		return "", false
	}
	if !filepath.IsAbs(gd) {
		gd = filepath.Join(dir, gd)
	}
	gd = filepath.Clean(gd)
	if st, err := os.Stat(gd); err != nil || !st.IsDir() {
		return "", false
	}
	return gd, true
}
