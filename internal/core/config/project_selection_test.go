package config

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sync"
	"testing"
)

func TestProjectSelectionRoundTrip(t *testing.T) {
	path := ProjectSelectionPath(filepath.Join(t.TempDir(), "project-roots.json"))
	empty, err := ReadProjectSelection(path)
	if err != nil || empty.Version != 1 || empty.Revision != 0 || len(empty.Selected) != 0 {
		t.Fatalf("missing file = %+v, %v", empty, err)
	}
	got, err := UpdateProjectSelection(path, func(ProjectSelection) ([]string, error) {
		return []string{"b", "a", "b"}, nil
	})
	if err != nil || got.Revision != 1 || !reflect.DeepEqual(got.Selected, []string{"a", "b"}) {
		t.Fatalf("update = %+v, %v", got, err)
	}
	read, err := ReadProjectSelection(path)
	if err != nil || !reflect.DeepEqual(read, got) {
		t.Fatalf("read back = %+v, %v", read, err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("mode = %v, %v", info.Mode(), err)
		}
	}
}

func TestProjectSelectionSerializesWriters(t *testing.T) {
	path := ProjectSelectionPath(filepath.Join(t.TempDir(), "project-roots.json"))
	const writers = 8
	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := UpdateProjectSelection(path, func(current ProjectSelection) ([]string, error) {
				return append(current.Selected, string(rune('a'+i))), nil
			})
			if err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	got, err := ReadProjectSelection(path)
	if err != nil || got.Revision != writers || len(got.Selected) != writers {
		t.Fatalf("lost an update: %+v, %v", got, err)
	}
}

func TestSuggestedProjectRoots(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "code", "repo"), 0o755); err != nil {
		t.Fatal(err)
	}
	repo, _ := CanonicalDirectory(filepath.Join(home, "code", "repo"))
	code, _ := CanonicalDirectory(filepath.Join(home, "code"))
	got := SuggestedProjectRoots(home, repo)
	if !reflect.DeepEqual(got, []string{repo, code}) {
		t.Fatalf("got %v, want [%s %s] (missing ~/projects and ~/dev skipped, ~/code deduped)", got, repo, code)
	}
}
