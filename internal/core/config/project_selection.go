package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
	gitstore "github.com/Tiago-0liveira/bonsai/internal/storage/git"
)

// ProjectSelection lists the discovered repositories the user chose to show.
// Discovery under the project roots only finds candidates; a repository is
// loaded only once its project ID is selected here. The local API (web
// settings) and `bonsai web setup` both write it, so every write holds the
// file's cross-process lock.
type ProjectSelection struct {
	Version  int      `json:"version"`
	Revision uint64   `json:"revision"`
	Selected []string `json:"selected"`
}

// ProjectSelectionPath is the selection file that sits next to the project
// roots file (project-roots.json.repositories.json).
func ProjectSelectionPath(rootsPath string) string {
	return rootsPath + ".repositories.json"
}

// ReadProjectSelection loads the selection, returning an empty one when the
// file does not exist.
func ReadProjectSelection(path string) (ProjectSelection, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return ProjectSelection{Version: 1, Selected: []string{}}, nil
	}
	if err != nil {
		return ProjectSelection{}, err
	}
	var cfg ProjectSelection
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return ProjectSelection{}, err
	}
	if cfg.Version == 0 {
		cfg.Version = 1
	}
	if cfg.Version != 1 {
		return ProjectSelection{}, fmt.Errorf("unsupported project selection version %d", cfg.Version)
	}
	if cfg.Selected == nil {
		cfg.Selected = []string{}
	}
	return cfg, nil
}

// UpdateProjectSelection runs fn on the current selection under the
// cross-process lock and writes the result atomically with the revision
// bumped. fn returns the new list of selected IDs; duplicates are dropped and
// the list is stored sorted.
func UpdateProjectSelection(path string, fn func(ProjectSelection) ([]string, error)) (ProjectSelection, error) {
	var out ProjectSelection
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return out, err
	}
	lock, err := procstore.Lock(path + ".lock")
	if err != nil {
		return out, err
	}
	defer lock.Unlock()
	current, err := ReadProjectSelection(path)
	if err != nil {
		return out, err
	}
	ids, err := fn(current)
	if err != nil {
		return out, err
	}
	selected := make([]string, 0, len(ids))
	seen := map[string]bool{}
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			selected = append(selected, id)
		}
	}
	sort.Strings(selected)
	out = ProjectSelection{Version: 1, Revision: current.Revision + 1, Selected: selected}
	raw, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return ProjectSelection{}, err
	}
	if err := gitstore.WriteJSON(path, append(raw, '\n')); err != nil {
		return ProjectSelection{}, err
	}
	return out, nil
}

// SuggestedProjectRoots lists folders worth offering as project roots: the
// launch repository and its parent (when there is one), then the common
// ~/projects, ~/dev and ~/code. Only existing directories are returned,
// canonicalized and without duplicates.
func SuggestedProjectRoots(home, repo string) []string {
	var paths []string
	if repo != "" {
		paths = append(paths, repo, filepath.Dir(repo))
	}
	if home != "" {
		for _, name := range []string{"projects", "dev", "code"} {
			paths = append(paths, filepath.Join(home, name))
		}
	}
	out := []string{}
	seen := map[string]bool{}
	for _, path := range paths {
		canonical, err := CanonicalDirectory(path)
		if err == nil && !seen[canonical] {
			seen[canonical] = true
			out = append(out, canonical)
		}
	}
	return out
}
