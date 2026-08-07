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

func TestDiskUsageKB(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "big.bin"), string(make([]byte, 10*1024)))

	kb, err := DiskUsageKB(dir)
	if err != nil {
		t.Fatal(err)
	}
	if kb < 10 {
		t.Errorf("DiskUsageKB = %d, want >= 10", kb)
	}

	if _, err := DiskUsageKB(filepath.Join(dir, "nope")); err == nil {
		t.Errorf("expected error for nonexistent dir")
	}
}

func TestHumanSize(t *testing.T) {
	cases := []struct {
		kb   int64
		want string
	}{
		{0, "0 KB"},
		{512, "512 KB"},
		{1024, "1.0 MB"},
		{1536, "1.5 MB"},
		{10 * 1024, "10.0 MB"},
		{1234 * 1024, "1.2 GB"},
	}
	for _, c := range cases {
		if got := HumanSize(c.kb); got != c.want {
			t.Errorf("HumanSize(%d) = %q, want %q", c.kb, got, c.want)
		}
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
