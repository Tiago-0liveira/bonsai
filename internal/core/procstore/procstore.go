// Package procstore owns bonsai's on-disk process state. Each repo keeps its
// runtime under <repo>/.bonsai/ (gitignored): the daemon socket, its pidfile and
// lock, and one JSON record plus one log file per managed process. A separate
// global index (see index.go) lists every live daemon so cross-repo commands can
// fan out. procstore has no socket or process logic — it is pure paths + files,
// so the CLI can read process state even when a daemon is idle or absent.
package procstore

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Restart policy modes.
const (
	PolicyNo        = "no"         // never restart
	PolicyOnFailure = "on-failure" // restart only on non-zero exit
	PolicyAlways    = "always"     // restart on any exit
)

// Policy is a process's restart behavior.
type Policy struct {
	Mode        string `json:"mode"`
	MaxRestarts int    `json:"max_restarts"`
}

// DefaultPolicy is used when neither the spawn request nor .bonsai.yaml names one.
func DefaultPolicy() Policy { return Policy{Mode: PolicyOnFailure, MaxRestarts: 5} }

// Valid reports whether mode is a known policy mode.
func ValidMode(mode string) bool {
	switch mode {
	case PolicyNo, PolicyOnFailure, PolicyAlways:
		return true
	}
	return false
}

// Process status values.
const (
	StatusRunning = "running"
	StatusStopped = "stopped" // user-killed
	StatusFailed  = "failed"  // exited non-zero, not restarting
	StatusDone    = "done"    // exited zero, not restarting
)

// Record is the persisted metadata for one managed process. It is the source of
// truth for discovery: readable without touching the daemon socket.
type Record struct {
	ID        int       `json:"id"`
	Label     string    `json:"label"`
	Command   string    `json:"command"`
	Worktree  string    `json:"worktree"`
	Branch    string    `json:"branch,omitempty"`
	PID       int       `json:"pid"`
	Status    string    `json:"status"`
	Policy    Policy    `json:"policy"`
	Restarts  int       `json:"restarts"`
	StartedAt time.Time `json:"started_at"`
	ExitError string    `json:"exit_error,omitempty"`
	LastURL   string    `json:"last_url,omitempty"`
}

// Store is the on-disk state for a single repo, rooted at its main worktree.
type Store struct {
	root string
}

// New returns a Store for the repo whose main worktree is root.
func New(root string) *Store { return &Store{root: filepath.Clean(root)} }

// Root returns the repo's main worktree path.
func (s *Store) Root() string { return s.root }

// Dir is <repo>/.bonsai.
func (s *Store) Dir() string { return filepath.Join(s.root, ".bonsai") }

// ProcsDir is <repo>/.bonsai/procs.
func (s *Store) ProcsDir() string { return filepath.Join(s.Dir(), "procs") }

// SockDir is a short-pathed directory for daemon sockets. Unix socket paths are
// capped (~104 bytes on macOS), and a repo deep in the tree easily exceeds that,
// so sockets live under $XDG_RUNTIME_DIR (or /tmp), keyed by a hash of the repo
// root rather than inside <repo>/.bonsai.
func SockDir() string {
	base := os.Getenv("XDG_RUNTIME_DIR")
	if base == "" {
		base = "/tmp"
	}
	return filepath.Join(base, "bonsai")
}

// SockPath is the daemon's Unix socket (short-pathed; see SockDir).
func (s *Store) SockPath() string {
	h := sha256.Sum256([]byte(s.root))
	return filepath.Join(SockDir(), hex.EncodeToString(h[:8])+".sock")
}

// PidPath is the daemon's pidfile.
func (s *Store) PidPath() string { return filepath.Join(s.Dir(), "daemon.pid") }

// LockPath is the flock target enforcing a single daemon per repo.
func (s *Store) LockPath() string { return filepath.Join(s.Dir(), "daemon.lock") }

// RecordPath is the JSON metadata file for process id.
func (s *Store) RecordPath(id int) string {
	return filepath.Join(s.ProcsDir(), strconv.Itoa(id)+".json")
}

// LogPath is the combined stdout+stderr log for process id.
func (s *Store) LogPath(id int) string {
	return filepath.Join(s.ProcsDir(), strconv.Itoa(id)+".log")
}

// EnsureDirs creates <repo>/.bonsai/procs and drops a self-ignoring .gitignore so
// none of bonsai's runtime state is ever committed.
func (s *Store) EnsureDirs() error {
	if err := os.MkdirAll(s.ProcsDir(), 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(SockDir(), 0o700); err != nil {
		return err
	}
	gi := filepath.Join(s.Dir(), ".gitignore")
	if _, err := os.Stat(gi); os.IsNotExist(err) {
		return os.WriteFile(gi, []byte("*\n"), 0o644)
	}
	return nil
}

// WriteRecord persists r atomically.
func (s *Store) WriteRecord(r *Record) error {
	if err := os.MkdirAll(s.ProcsDir(), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(s.RecordPath(r.ID), data, 0o644)
}

// ReadRecord loads the record for id.
func (s *Store) ReadRecord(id int) (*Record, error) {
	data, err := os.ReadFile(s.RecordPath(id))
	if err != nil {
		return nil, err
	}
	var r Record
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// ListRecords returns every process record, ordered by id.
func (s *Store) ListRecords() ([]*Record, error) {
	entries, err := os.ReadDir(s.ProcsDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []*Record
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		id, err := strconv.Atoi(strings.TrimSuffix(name, ".json"))
		if err != nil {
			continue
		}
		if r, err := s.ReadRecord(id); err == nil {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// RemoveRecord deletes the record and its log for id.
func (s *Store) RemoveRecord(id int) error {
	_ = os.Remove(s.LogPath(id))
	_ = os.Remove(s.LogPath(id) + ".1")
	err := os.Remove(s.RecordPath(id))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// MaxID returns the highest existing record id (0 if none), so a restarting
// daemon can resume the id counter without reusing numbers.
func (s *Store) MaxID() (int, error) {
	recs, err := s.ListRecords()
	if err != nil {
		return 0, err
	}
	max := 0
	for _, r := range recs {
		if r.ID > max {
			max = r.ID
		}
	}
	return max, nil
}

// writeAtomic writes data to path via a temp file + rename.
func writeAtomic(path string, data []byte, perm os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
