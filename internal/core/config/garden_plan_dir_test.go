package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestGardenPlanDirIsPathSafeAndUnderRoot(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	root, err := GardenPlansRoot()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, ".bonsai", "garden", "plans"); root != want {
		t.Fatalf("root = %q, want %q", root, want)
	}
	plain, err := GardenPlanDir("local-abc_1", 42, "plan-0123")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(root, "local-abc_1", "42", "plan-0123"); plain != want {
		t.Fatalf("plain dir = %q, want %q", plain, want)
	}
	hostile := []string{"..", ".", "../..", "a/b", `a\b`, "/etc", ".hidden", "-flag", "h-deadbeef", "with space", "päth", strings.Repeat("x", 65)}
	seen := map[string]string{}
	for _, id := range hostile {
		dir, err := GardenPlanDir(id, 7, id)
		if err != nil {
			t.Fatalf("%q: %v", id, err)
		}
		if !ContainsPath(root, dir) || dir == root {
			t.Fatalf("%q escaped: %q", id, dir)
		}
		rel, _ := filepath.Rel(root, dir)
		parts := strings.Split(rel, string(filepath.Separator))
		if len(parts) != 3 {
			t.Fatalf("%q: want repo/issue/attempt, got %q", id, rel)
		}
		if prev, dup := seen[dir]; dup {
			t.Fatalf("%q and %q share %q", id, prev, dir)
		}
		seen[dir] = id
	}
	for _, bad := range []struct {
		repo    string
		issue   uint64
		attempt string
	}{{"", 1, "a"}, {"r", 0, "a"}, {"r", 1, ""}} {
		if _, err := GardenPlanDir(bad.repo, bad.issue, bad.attempt); err == nil {
			t.Fatalf("%+v: want error", bad)
		}
	}
}
