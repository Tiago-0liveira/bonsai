// Package fs handles fuzzy file search over the main repo tree and copying files
// from the main repo into a worktree.
package fs

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/sahilm/fuzzy"
)

// skipDirs are never descended into during indexing.
var skipDirs = map[string]bool{
	".git":         true,
	"node_modules": true,
}

// Index walks root and returns repo-relative file paths, excluding skipDirs.
func Index(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files = append(files, rel)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

// Ranker reports a copy-frequency score for a repo-relative path. Higher scores
// float to the top when fuzzy match scores tie.
type Ranker func(relPath string) int

// Search fuzzy-matches query against the indexed files. When query is empty the
// full list is returned ordered by Ranker (most-copied first). Results are
// otherwise ordered by fuzzy score, breaking ties with Ranker.
func Search(files []string, query string, rank Ranker) []string {
	if rank == nil {
		rank = func(string) int { return 0 }
	}

	if query == "" {
		out := make([]string, len(files))
		copy(out, files)
		sort.SliceStable(out, func(i, j int) bool {
			return rank(out[i]) > rank(out[j])
		})
		return out
	}

	matches := fuzzy.Find(query, files)
	sort.SliceStable(matches, func(i, j int) bool {
		if matches[i].Score != matches[j].Score {
			return matches[i].Score > matches[j].Score
		}
		return rank(matches[i].Str) > rank(matches[j].Str)
	})

	out := make([]string, len(matches))
	for i, m := range matches {
		out[i] = m.Str
	}
	return out
}

// Copy copies the file at absolute src to absolute dst, creating parent dirs and
// preserving the source file mode. It refuses a no-op copy onto the same file,
// which would otherwise truncate it to empty.
func Copy(src, dst string) error {
	if sameFile(src, dst) {
		return fmt.Errorf("source and destination are the same file: %s", src)
	}

	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	info, err := in.Stat()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode())
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}

// sameFile reports whether src and dst resolve to the same location. It first
// tries os.SameFile (handles symlinks/hardlinks when dst exists) then falls back
// to a cleaned absolute-path comparison.
func sameFile(src, dst string) bool {
	if si, err := os.Stat(src); err == nil {
		if di, err := os.Stat(dst); err == nil {
			return os.SameFile(si, di)
		}
	}
	a, err1 := filepath.Abs(src)
	b, err2 := filepath.Abs(dst)
	return err1 == nil && err2 == nil && filepath.Clean(a) == filepath.Clean(b)
}
