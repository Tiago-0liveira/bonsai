package procstore

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// IndexEntry records one live daemon in the global index.
type IndexEntry struct {
	Root string `json:"root"` // repo main worktree
	Sock string `json:"sock"` // that daemon's Unix socket
	PID  int    `json:"pid"`  // daemon pid
}

// indexPath is ~/.config/bonsai/daemons.json (honors $XDG_CONFIG_HOME).
func indexPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "bonsai", "daemons.json"), nil
}

// withIndex runs fn against the decoded index under an exclusive lock, then
// persists the (possibly mutated) map atomically. A nil map is passed as empty.
func withIndex(mutate bool, fn func(m map[string]IndexEntry) error) ([]IndexEntry, error) {
	path, err := indexPath()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}

	lk, err := Lock(path + ".lock")
	if err != nil {
		return nil, err
	}
	defer lk.Unlock()

	m := map[string]IndexEntry{}
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &m)
	}

	// Prune dead daemons on every access so readers never see stale entries.
	changed := false
	for root, e := range m {
		if !pidAlive(e.PID) || !sockExists(e.Sock) {
			delete(m, root)
			changed = true
		}
	}

	if fn != nil {
		if err := fn(m); err != nil {
			return nil, err
		}
	}

	if mutate || changed {
		data, err := json.MarshalIndent(m, "", "  ")
		if err != nil {
			return nil, err
		}
		if err := writeAtomic(path, data, 0o644); err != nil {
			return nil, err
		}
	}

	out := make([]IndexEntry, 0, len(m))
	for _, e := range m {
		out = append(out, e)
	}
	return out, nil
}

// Register adds or updates this daemon's entry in the global index.
func Register(root, sock string, pid int) error {
	_, err := withIndex(true, func(m map[string]IndexEntry) error {
		m[filepath.Clean(root)] = IndexEntry{Root: filepath.Clean(root), Sock: sock, PID: pid}
		return nil
	})
	return err
}

// Deregister removes this daemon's entry from the global index.
func Deregister(root string) error {
	_, err := withIndex(true, func(m map[string]IndexEntry) error {
		delete(m, filepath.Clean(root))
		return nil
	})
	return err
}

// ListDaemons returns every live daemon, pruning stale entries as a side effect.
func ListDaemons() ([]IndexEntry, error) {
	return withIndex(false, nil)
}

func sockExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
