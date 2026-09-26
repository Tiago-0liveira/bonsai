package pkgmgr

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveLocationInputs(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	nested := filepath.Join(project, "a", "b")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	t.Run("absolute", func(t *testing.T) {
		loc, err := resolveLocation(nested)
		if err != nil {
			t.Fatal(err)
		}
		if !samePath(loc.InputDir, nested) || !samePath(loc.ProjectRoot, nested) {
			t.Fatalf("location = %+v", loc)
		}
	})

	t.Run("relative", func(t *testing.T) {
		old, err := os.Getwd()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Chdir(root); err != nil {
			t.Fatal(err)
		}
		defer os.Chdir(old)
		loc, err := resolveLocation(filepath.Join("project", "a", "b"))
		if err != nil {
			t.Fatal(err)
		}
		if !samePath(loc.InputDir, nested) {
			t.Fatalf("InputDir = %q, want equivalent to %q", loc.InputDir, nested)
		}
	})

	t.Run("empty uses cwd", func(t *testing.T) {
		old, err := os.Getwd()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Chdir(nested); err != nil {
			t.Fatal(err)
		}
		defer os.Chdir(old)
		loc, err := resolveLocation("")
		if err != nil {
			t.Fatal(err)
		}
		if !samePath(loc.InputDir, nested) {
			t.Fatalf("InputDir = %q, want equivalent to %q", loc.InputDir, nested)
		}
	})
}

func TestResolveLocationErrors(t *testing.T) {
	if _, err := resolveLocation(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("expected nonexistent directory error")
	}

	dir := t.TempDir()
	file := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveLocation(file); err == nil {
		t.Fatal("expected file-not-directory error")
	}
}

func TestResolveLocationGitRoot(t *testing.T) {
	repo := t.TempDir()
	if err := os.Mkdir(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(repo, "apps", "web")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	loc, err := resolveLocation(nested)
	if err != nil {
		t.Fatal(err)
	}
	if !samePath(loc.RepositoryRoot, repo) {
		t.Fatalf("RepositoryRoot = %q, want equivalent to %q", loc.RepositoryRoot, repo)
	}

	outside := t.TempDir()
	loc, err = resolveLocation(outside)
	if err != nil {
		t.Fatal(err)
	}
	if loc.RepositoryRoot != "" {
		t.Fatalf("RepositoryRoot outside Git = %q", loc.RepositoryRoot)
	}
}

func TestDiscoverNestedProjectRoot(t *testing.T) {
	root := t.TempDir()
	write(t, root, "package.json", `{"scripts":{"dev":"vite"}}`)
	nested := filepath.Join(root, "src", "components")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	project, err := Discover(nested, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !samePath(project.Location.ProjectRoot, root) {
		t.Fatalf("ProjectRoot = %q, want equivalent to %q", project.Location.ProjectRoot, root)
	}
}

func samePath(a, b string) bool {
	canonical := func(path string) string {
		resolved, err := filepath.EvalSymlinks(path)
		if err == nil {
			path = resolved
		}
		return filepath.Clean(path)
	}
	return canonical(a) == canonical(b)
}
