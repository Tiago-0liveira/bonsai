package fs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIndexExcludesSkipDirs(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "main.go"), "package main")
	mustWrite(t, filepath.Join(root, "src", "app.ts"), "x")
	mustWrite(t, filepath.Join(root, ".git", "config"), "x")
	mustWrite(t, filepath.Join(root, "node_modules", "pkg", "index.js"), "x")

	files, err := Index(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if f == filepath.Join(".git", "config") || f == filepath.Join("node_modules", "pkg", "index.js") {
			t.Fatalf("skip dir leaked into index: %s", f)
		}
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d: %v", len(files), files)
	}
}

func TestSearchRanksByCopyFrequency(t *testing.T) {
	files := []string{"a/config.yaml", "b/config.yaml"}
	rank := func(p string) int {
		if p == "b/config.yaml" {
			return 5
		}
		return 0
	}
	// Both match "config" equally; the higher-ranked path wins the tie.
	got := Search(files, "config", rank)
	if got[0] != "b/config.yaml" {
		t.Fatalf("expected higher-ranked file first, got %v", got)
	}
}

func TestCopy(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src.txt")
	dst := filepath.Join(root, "nested", "dst.txt")
	mustWrite(t, src, "hello")

	if err := Copy(src, dst); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello" {
		t.Fatalf("copied content = %q", data)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
