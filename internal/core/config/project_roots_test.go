package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

func TestProjectRootsTransactions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "project-roots.json")
	root := t.TempDir()
	initial, err := ReadProjectRoots(path)
	if err != nil || len(initial.Roots) != 0 || initial.Version != 1 {
		t.Fatal(initial, err)
	}
	cfg, err := UpdateProjectRoots(path, "add", 0, root, "")
	if err != nil {
		t.Fatal(err)
	}
	replay, err := UpdateProjectRoots(path, "add", 0, root, "")
	if err != nil || replay.Revision != cfg.Revision {
		t.Fatal(replay, err)
	}
	if _, err = UpdateProjectRoots(path, "add", 1, root, ""); !errors.Is(err, ErrRootIdempotency) {
		t.Fatal(err)
	}
	if _, err = UpdateProjectRoots(path, "stale", 0, root, ""); !errors.Is(err, ErrRootRevision) {
		t.Fatal(err)
	}
	cfg, err = UpdateProjectRoots(path, "duplicate", 1, root, "")
	if err != nil || len(cfg.Roots) != 1 {
		t.Fatal(cfg, err)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	success := 0
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, e := UpdateProjectRoots(path, fmt.Sprint(i), 2, root, "")
			mu.Lock()
			defer mu.Unlock()
			if e == nil {
				success++
			} else if !errors.Is(e, ErrRootRevision) {
				t.Error(e)
			}
		}(i)
	}
	wg.Wait()
	if success != 1 {
		t.Fatal(success)
	}
	cfg, err = ReadProjectRoots(path)
	if err != nil || cfg.Revision != 3 || len(cfg.Roots) != 1 {
		t.Fatal(cfg, err)
	}
	cfg, err = UpdateProjectRoots(path, "remove", 3, "", cfg.Roots[0].ID)
	if err != nil || len(cfg.Roots) != 0 {
		t.Fatal(cfg, err)
	}
	replay, err = UpdateProjectRoots(path, "add", 0, root, "")
	if err != nil || len(replay.Roots) != 1 || replay.Revision != 1 {
		t.Fatal(replay, err)
	}
	disk, _ := ReadProjectRoots(path)
	if len(disk.Roots) != 0 || disk.Revision != 4 {
		t.Fatal("retry reapplied operation", disk)
	}
}
func TestProjectRootsInvalidAndCorrupt(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "settings.json")
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, []byte("file"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"relative", "~someone/projects", "$HOME/projects", file, filepath.Join(root, "missing")} {
		if _, err := UpdateProjectRoots(path, "key", 0, value, ""); err == nil {
			t.Errorf("accepted %q", value)
		}
	}
	for _, content := range []string{`{`, `{}`, `null`, `{"version":2,"revision":0,"roots":[]}`, `{"version":1,"roots":[{"id":"bad","path":"relative"}]}`} {
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := UpdateProjectRoots(path, "key", 0, root, ""); err == nil {
			t.Fatal("overwrote corrupt configuration")
		}
		after, _ := os.ReadFile(path)
		if !bytes.Equal(after, []byte(content)) {
			t.Fatal("changed corrupt file")
		}
	}
}
func TestProjectRootAliasesAndOwnership(t *testing.T) {
	root := t.TempDir()
	child := filepath.Join(root, "nested")
	if err := os.Mkdir(child, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "settings.json")
	cfg, err := UpdateProjectRoots(path, "first", 0, root, "")
	if err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(root, alias); err == nil {
		cfg, err = UpdateProjectRoots(path, "alias", 1, alias, "")
		if err != nil || len(cfg.Roots) != 1 {
			t.Fatal(cfg, err)
		}
	}
	roots := []ProjectRoot{{ID: "outer", Path: root}, {ID: "inner", Path: child}}
	for _, rs := range [][]ProjectRoot{roots, {roots[1], roots[0]}} {
		owner, ok := OwningRoot(rs, filepath.Join(child, "repo"))
		if !ok || owner.ID != "inner" {
			t.Fatal(owner)
		}
	}
	if ContainsPath(root, root+"-other") {
		t.Fatal("prefix treated as containment")
	}
	owner, ok := OwningRoot(roots, t.TempDir(), filepath.Join(child, "linked"))
	if !ok || owner.ID != "inner" {
		t.Fatal(owner)
	}
	if runtime.GOOS == "windows" {
		if !ContainsPath(`C:\Projects`, `C:\Projects\Repo`) || ContainsPath(`C:\Projects`, `D:\Projects\Repo`) {
			t.Fatal("Windows volume containment")
		}
		if !ContainsPath(`\\host\share\projects`, `\\host\share\projects\repo`) {
			t.Fatal("UNC containment")
		}
	}
}
func TestMissingSavedRootSurvivesRead(t *testing.T) {
	root := filepath.Join(t.TempDir(), "root")
	os.Mkdir(root, 0700)
	path := filepath.Join(t.TempDir(), "settings.json")
	if _, err := UpdateProjectRoots(path, "add", 0, root, ""); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(root); err != nil {
		t.Fatal(err)
	}
	cfg, err := ReadProjectRoots(path)
	if err != nil || len(cfg.Roots) != 1 {
		t.Fatal(cfg, err)
	}
}

func TestOwningRootDepthAndPathTieBreak(t *testing.T) {
	base := t.TempDir()
	roots := []ProjectRoot{{ID: "long", Path: filepath.Join(base, "long-folder-name")}, {ID: "deep", Path: filepath.Join(base, "a", "b")}, {ID: "tie", Path: filepath.Join(base, "a", "c")}}
	paths := []string{filepath.Join(roots[0].Path, "wt"), filepath.Join(roots[1].Path, "wt"), filepath.Join(roots[2].Path, "wt")}
	owner, ok := OwningRoot(roots, filepath.Join(t.TempDir(), "external"), paths...)
	if !ok || owner.ID != "deep" {
		t.Fatal(owner)
	}
}

func TestBrowserDestinationResolvesBeforeContainment(t *testing.T) {
	root, err := CanonicalDirectory(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Skip(err)
	}
	result, err := prepareBrowserDirectory([]ProjectRoot{{Path: root}}, filepath.Join(alias, "new"))
	if err != nil || result != filepath.Join(root, "new") {
		t.Fatal(result, err)
	}
	outside := t.TempDir()
	if _, err = prepareBrowserDirectory([]ProjectRoot{{Path: root}}, filepath.Join(outside, "new")); err == nil {
		t.Fatal("accepted unconfigured destination")
	}
	if _, err = os.Stat(filepath.Join(outside, "new")); !os.IsNotExist(err) {
		t.Fatal("created unconfigured destination", err)
	}
}
