package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestCheckoutInspectorReportsCleanAndDirty(t *testing.T) {
	dir := gitInit(t)
	in := CheckoutInspector{Dir: dir}
	clean, err := in.Inspect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(clean.Commit) < 40 || clean.Branch != "main" || clean.Dirty() || clean.DirtyDigest != "" {
		t.Fatalf("clean = %+v", clean)
	}
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("changed"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dirty, err := in.Inspect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if dirty.Commit != clean.Commit || dirty.DirtyFiles != 2 || dirty.DirtyDigest == "" {
		t.Fatalf("dirty = %+v", dirty)
	}
	if err := os.WriteFile(filepath.Join(dir, "third.txt"), []byte("n"), 0o644); err != nil {
		t.Fatal(err)
	}
	more, _ := in.Inspect(context.Background())
	if more.DirtyDigest == dirty.DirtyDigest || more.DirtyFiles != 3 {
		t.Fatalf("a new changed path must change the digest: %+v vs %+v", more, dirty)
	}
}

func TestCheckoutInspectorDetachedHead(t *testing.T) {
	dir := gitInit(t)
	runGit(t, dir, "checkout", "--detach")
	c, err := CheckoutInspector{Dir: dir}.Inspect(context.Background())
	if err != nil || c.Branch != "" || c.Commit == "" {
		t.Fatalf("detached = %+v, %v", c, err)
	}
}

func TestCheckoutInspectorUnbornHeadFails(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "-b", "main")
	if _, err := (CheckoutInspector{Dir: dir}).Inspect(context.Background()); err == nil {
		t.Fatal("a repository without commits has nothing to plan against")
	}
}

func TestCheckoutInspectorSurfacesRunnerError(t *testing.T) {
	boom := errors.New("boom")
	_, err := CheckoutInspector{Dir: "x", Run: func(context.Context, string, ...string) (string, error) { return "", boom }}.Inspect(context.Background())
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
}

func TestCountStatusEntriesHandlesRenames(t *testing.T) {
	// "R  new\0old\0" is one change; the source path is not another.
	if got := countStatusEntries(" M a.txt\x00R  new.txt\x00old.txt\x00?? u.txt\x00"); got != 3 {
		t.Fatalf("count = %d", got)
	}
	if got := countStatusEntries(""); got != 0 {
		t.Fatalf("count = %d", got)
	}
}
