package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// Prefs holds personal UI preferences that override the repo's .bonsai.yaml
// for the local user: theme preset, keybinding overrides, default sort, and
// the prune merge default. Project settings (hooks, aliases, upstream) stay
// in .bonsai.yaml.
type Prefs struct {
	// Theme names a palette preset chosen in-app ("" = use .bonsai.yaml).
	Theme string `json:"theme,omitempty"`
	// Sort is the default worktree list ordering (name/ahead/behind/pr/
	// activity/dirty; "" = name).
	Sort string `json:"sort,omitempty"`
	// Keys maps an action name to a personal key override.
	Keys map[string]string `json:"keys,omitempty"`
	// PruneMerge turns the prune modal's merge-PR step on by default.
	PruneMerge bool `json:"prune_merge,omitempty"`
}

// State is persisted mutable data: how often each main-repo file has been copied
// into a worktree, plus user-recorded aliases.
type State struct {
	// CopyCounts maps a main-repo relative file path to its copy frequency.
	CopyCounts map[string]int `json:"copy_counts"`
	Aliases    []Alias        `json:"aliases"`
	Prefs      Prefs          `json:"prefs"`

	mu   sync.Mutex `json:"-"`
	path string     `json:"-"`
}

// statePath returns ~/.config/bonsai/state.json (honoring $XDG_CONFIG_HOME).
func statePath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "bonsai", "state.json"), nil
}

// LoadState reads the persisted state, returning an empty (but usable) State if
// the file does not yet exist.
func LoadState() (*State, error) {
	p, err := statePath()
	if err != nil {
		return nil, err
	}
	st := &State{CopyCounts: map[string]int{}, path: p}

	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return st, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(data, st); err != nil {
		return nil, err
	}
	if st.CopyCounts == nil {
		st.CopyCounts = map[string]int{}
	}
	st.path = p
	return st, nil
}

// Save writes the state to disk atomically.
func (s *State) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// RecordCopy increments the copy frequency for relPath and persists.
func (s *State) RecordCopy(relPath string) error {
	s.mu.Lock()
	s.CopyCounts[relPath]++
	s.mu.Unlock()
	return s.Save()
}

// CopyRank returns the recorded copy frequency for relPath (0 if never copied).
func (s *State) CopyRank(relPath string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.CopyCounts[relPath]
}

// AddAlias appends an alias and persists.
func (s *State) AddAlias(a Alias) error {
	s.mu.Lock()
	s.Aliases = append(s.Aliases, a)
	s.mu.Unlock()
	return s.Save()
}

// RemoveAlias deletes the alias with the given name and persists. Returns false
// if no such alias existed.
func (s *State) RemoveAlias(name string) (bool, error) {
	s.mu.Lock()
	out := s.Aliases[:0]
	found := false
	for _, a := range s.Aliases {
		if a.Name == name {
			found = true
			continue
		}
		out = append(out, a)
	}
	s.Aliases = out
	s.mu.Unlock()
	if !found {
		return false, nil
	}
	return true, s.Save()
}

// SortedAliases returns aliases ordered by name.
func (s *State) SortedAliases() []Alias {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Alias, len(s.Aliases))
	copy(out, s.Aliases)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// SetPrefs replaces the stored UI preferences and persists them.
func (s *State) SetPrefs(p Prefs) error {
	s.mu.Lock()
	s.Prefs = p
	s.mu.Unlock()
	return s.Save()
}
