package review

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestReportDirPrepareReadAndSafety(t *testing.T) {
	base := t.TempDir()
	d := ReportDir{Root: filepath.Join(base, "reviews")}
	path, err := d.Prepare("local-1", "att-1", 2)
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(path) || filepath.Base(path) != ReportFileName || !strings.HasPrefix(path, d.Root) {
		t.Fatalf("path = %q", path)
	}
	if _, err := d.Read("local-1", "att-1", 2); !errors.Is(err, ErrReportMissing) {
		t.Fatalf("missing file: %v", err)
	}
	if err := os.WriteFile(path, []byte(`{"x":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if b, err := d.Read("local-1", "att-1", 2); err != nil || string(b) != `{"x":1}` {
		t.Fatalf("read = %q, %v", b, err)
	}
	// A new prepare of the same run removes a leftover file.
	if _, err := d.Prepare("local-1", "att-1", 2); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Read("local-1", "att-1", 2); !errors.Is(err, ErrReportMissing) {
		t.Fatalf("leftover not removed: %v", err)
	}
	// Another clone or revision never shares the path.
	other, _ := d.Path("local-2", "att-1", 2)
	next, _ := d.Path("local-1", "att-1", 3)
	if other == path || next == path {
		t.Fatal("paths collide")
	}
	if _, err := d.Path("", "att-1", 1); !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty clone: %v", err)
	}
	if _, err := d.Path("l", "../x", 1); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad attempt: %v", err)
	}
}

func TestReportDirRefusesLinksAndSpecialFiles(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	base := t.TempDir()
	d := ReportDir{Root: filepath.Join(base, "reviews")}
	path, err := d.Prepare("l", "a", 1)
	if err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(base, "secret")
	if err := os.WriteFile(secret, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, path); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Read("l", "a", 1); !errors.Is(err, ErrReportUnsafe) {
		t.Fatalf("symlinked file: %v", err)
	}
	if _, err := d.Prepare("l", "a", 1); !errors.Is(err, ErrReportUnsafe) {
		t.Fatalf("prepare over a symlink: %v", err)
	}

	// A group-writable run directory is not private.
	path2, err := d.Prepare("l", "a", 2)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path2, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Dir(path2), 0o777); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Read("l", "a", 2); !errors.Is(err, ErrReportUnsafe) {
		t.Fatalf("open directory: %v", err)
	}

	// A symlinked run directory is refused.
	path3, err := d.Prepare("l", "a", 3)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Dir(path3)
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	elsewhere := t.TempDir()
	if err := os.Symlink(elsewhere, dir); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Read("l", "a", 3); !errors.Is(err, ErrReportUnsafe) {
		t.Fatalf("symlinked directory: %v", err)
	}

	// An oversized file is read to one byte over the cap, so the parser can reject it.
	path4, err := d.Prepare("l", "a", 4)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path4, make([]byte, MaxReportBytes+10), 0o600); err != nil {
		t.Fatal(err)
	}
	b, err := d.Read("l", "a", 4)
	if err != nil || len(b) != MaxReportBytes+1 {
		t.Fatalf("oversized read = %d, %v", len(b), err)
	}
}
