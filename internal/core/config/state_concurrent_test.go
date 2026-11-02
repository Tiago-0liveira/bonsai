package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func TestStateIndependentWritersNeverCollide(t *testing.T) {
	previous := runtime.GOMAXPROCS(4)
	defer runtime.GOMAXPROCS(previous)
	file := filepath.Join(t.TempDir(), "state.json")
	a := &State{path: file, Aliases: []Alias{{Name: "a", Command: strings.Repeat("a", 512<<10)}}}
	b := &State{path: file, Aliases: []Alias{{Name: "b", Command: strings.Repeat("b", 128<<10)}}}
	for round := 0; round < 30; round++ {
		gate := make(chan struct{})
		errs := make(chan error, 2)
		var wg sync.WaitGroup
		for _, s := range []*State{a, b} {
			wg.Add(1)
			go func(s *State) {
				defer wg.Done()
				<-gate
				errs <- s.Save()
			}(s)
		}
		close(gate)
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatalf("round %d: concurrent save failed: %v", round, err)
			}
		}
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if !json.Valid(data) {
			t.Fatalf("round %d: state.json is not valid JSON", round)
		}
	}
	leftovers, _ := filepath.Glob(file + ".tmp*")
	if len(leftovers) != 0 {
		t.Errorf("staging files left behind: %v", leftovers)
	}
}

func TestStateFieldUpdatesMergeAcrossInstances(t *testing.T) {
	file := filepath.Join(t.TempDir(), "state.json")
	a := &State{path: file, CopyCounts: map[string]int{}}
	b := &State{path: file, CopyCounts: map[string]int{}}

	if err := a.AddAlias(Alias{Name: "dev", Command: "npm run dev"}); err != nil {
		t.Fatal(err)
	}
	// b loaded before a's write and knows nothing of it.
	if err := b.AddAlias(Alias{Name: "test", Command: "npm test"}); err != nil {
		t.Fatal(err)
	}
	if err := b.RecordCopy(".env"); err != nil {
		t.Fatal(err)
	}
	if err := a.RecordCopy(".env"); err != nil {
		t.Fatal(err)
	}
	if err := a.PersistLastWorkspace(1, "/repo/one", "ws1"); err != nil {
		t.Fatal(err)
	}
	if err := b.PersistLastWorkspace(1, "/repo/two", "ws2"); err != nil {
		t.Fatal(err)
	}
	if ok, err := a.RemoveAlias("test"); !ok || err != nil {
		t.Fatalf("RemoveAlias = %v, %v; want it to find b's alias", ok, err)
	}

	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var got stateDisk
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Aliases) != 1 || got.Aliases[0].Name != "dev" {
		t.Errorf("aliases = %+v, want only dev", got.Aliases)
	}
	if got.CopyCounts[".env"] != 2 {
		t.Errorf("copy count = %d, want 2", got.CopyCounts[".env"])
	}
	if len(got.Workspaces) != 2 {
		t.Errorf("workspaces = %v, want both repos", got.Workspaces)
	}
}
