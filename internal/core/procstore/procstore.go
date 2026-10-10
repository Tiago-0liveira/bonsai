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
	"fmt"
	"os"
	"path/filepath"
	"runtime"
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

// ValidatePolicy applies equally to launch and policy updates. Zero means no retries.
func ValidatePolicy(p Policy) error {
	if !ValidMode(p.Mode) {
		return fmt.Errorf("restart mode must be no, on-failure or always")
	}
	if p.MaxRestarts < 0 || p.MaxRestarts > 100 {
		return fmt.Errorf("maximum retries must be between 0 and 100")
	}
	return nil
}

// Process status values.
const (
	StatusStarting = "starting"
	StatusRunning  = "running"
	StatusBackoff  = "backoff"
	StatusStopping = "stopping"
	StatusStopped  = "stopped" // user-killed
	StatusDone     = "done"    // exited zero, not restarting
	StatusFailed   = "failed"  // exited non-zero, not restarting
	StatusLost     = "lost"    // daemon lost authoritative supervision and process exited
	StatusOrphan   = "orphan"  // daemon restarted; process is still alive unmanaged
)

// IsTerminal reports whether status represents a terminal state.
func IsTerminal(status string) bool {
	switch status {
	case StatusStopped, StatusDone, StatusFailed, StatusLost:
		return true
	}
	return false
}

// IsActive reports whether status represents an active (or recovering) state.
func IsActive(status string) bool {
	switch status {
	case StatusStarting, StatusRunning, StatusBackoff, StatusStopping, StatusOrphan:
		return true
	}
	return false
}

// Record is the persisted metadata for one managed process. It is the source of
// truth for discovery: readable without touching the daemon socket.
type Record struct {
	CommandKey     string            `json:"command_key"`
	ExecutionOrder uint64            `json:"execution_order"`
	ID             int               `json:"id"`
	Revision       uint64            `json:"revision"`
	Attempt        int               `json:"attempt"`
	RetryCount     int               `json:"retry_count"`
	RetryAt        *time.Time        `json:"retry_at,omitempty"`
	Label          string            `json:"label"`
	Command        string            `json:"command"` // display/shell command; structured commands also set Program/Args
	Program        string            `json:"program,omitempty"`
	Args           []string          `json:"args,omitempty"`
	Environment    map[string]string `json:"environment,omitempty"`
	Worktree       string            `json:"worktree"` // owning Git worktree root
	WorkingDir     string            `json:"working_dir,omitempty"`
	Branch         string            `json:"branch,omitempty"`
	PID            int               `json:"pid"`
	ProcessGroupID int               `json:"process_group_id,omitempty"`
	ExpectedPort   int               `json:"expected_port,omitempty"`
	Status         string            `json:"status"`
	Policy         Policy            `json:"policy"`
	Restarts       int               `json:"restarts"`
	StartedAt      time.Time         `json:"started_at"`
	ExitCode       *int              `json:"exit_code,omitempty"`
	ExitError      string            `json:"exit_error,omitempty"`
	LastURL        string            `json:"last_url,omitempty"`
	ServeGroup     string            `json:"serve_group,omitempty"`
	ServeName      string            `json:"serve_name,omitempty"`
	ServeRequired  bool              `json:"serve_required,omitempty"`
}

// ServeSidecar describes one generic daemon-supervised companion process.
type ServeSidecar struct {
	Name        string            `json:"name"`
	Command     []string          `json:"command"`
	Cwd         string            `json:"cwd,omitempty"`
	Environment map[string]string `json:"environment,omitempty"`
	Restart     string            `json:"restart,omitempty"`
	MaxRestarts int               `json:"max_restarts,omitempty"`
	Required    bool              `json:"required,omitempty"`
}

type ServeMode string

const (
	ServeModeProduction  ServeMode = "production"
	ServeModeDevelopment ServeMode = "development"
)

// ServeScope says whose projects a serve group's local API owns. The empty
// (repository) scope launches the API for the daemon's own repository, which
// also backs the legacy unscoped routes. The user scope is `bonsai web`: one
// API per user, run by a daemon whose home is not a Git repository, serving
// only the projects found under the configured project roots.
type ServeScope string

const (
	ServeScopeRepository ServeScope = ""
	ServeScopeUser       ServeScope = "user"
)

// WebServeGroupID is the fixed workspace ID of the user-level `bonsai web`
// serve group inside its daemon home.
const WebServeGroupID = "web"

// ServeSpec is the daemon request for one workspace serve group. Production
// specs contain the local API; the user-level one may add the live-updates
// webhook port (served by the API) and one "tunnel" sidecar pointed at it.
// Development specs may add the local webhook relay, Vite frontend, and
// explicitly configured development sidecars.
type ServeSpec struct {
	Mode                   ServeMode      `json:"mode,omitempty"`
	Scope                  ServeScope     `json:"scope,omitempty"`
	WorkspaceID            string         `json:"workspace_id"`
	WorkspacePath          string         `json:"workspace_path"`
	Executable             string         `json:"executable"`
	APIPort                int            `json:"api_port"`
	WebhookPort            int            `json:"webhook_port,omitempty"`
	WebPort                int            `json:"web_port,omitempty"`
	BrowserOrigin          string         `json:"browser_origin"`
	Sidecars               []ServeSidecar `json:"sidecars,omitempty"`
	StartupTimeoutSeconds  int            `json:"startup_timeout_seconds,omitempty"`
	ShutdownTimeoutSeconds int            `json:"shutdown_timeout_seconds,omitempty"`
}

// ServeProcess is the public status view for one process in a ServeGroup.
type ServeProcess struct {
	Name           string    `json:"name"`
	CommandKey     string    `json:"command_key"`
	ExecutionOrder uint64    `json:"execution_order"`
	ID             int       `json:"id"`
	PID            int       `json:"pid"`
	ProcessGroupID int       `json:"process_group_id,omitempty"`
	ExpectedPort   int       `json:"expected_port,omitempty"`
	State          string    `json:"state"`
	Required       bool      `json:"required"`
	StartedAt      time.Time `json:"started_at,omitempty"`
	ExitCode       *int      `json:"exit_code,omitempty"`
	ExitError      string    `json:"exit_error,omitempty"`
}

// ServeGroup is the daemon-owned status snapshot for one workspace stack.
type ServeGroup struct {
	ID            string     `json:"id"`
	Mode          ServeMode  `json:"mode,omitempty"`
	Scope         ServeScope `json:"scope,omitempty"`
	WorkspaceID   string     `json:"workspace_id"`
	WorkspacePath string     `json:"workspace_path"`
	State         string     `json:"state"`
	StartedAt     time.Time  `json:"started_at"`
	APIPort       int        `json:"api_port"`
	WebhookPort   int        `json:"webhook_port,omitempty"`
	WebPort       int        `json:"web_port,omitempty"`
	BrowserOrigin string     `json:"browser_origin,omitempty"`
	// Tunnel is the argv of the live-updates tunnel sidecar, if any.
	Tunnel    []string       `json:"tunnel,omitempty"`
	Reused    bool           `json:"reused,omitempty"`
	Processes []ServeProcess `json:"processes"`
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
		if runtime.GOOS == "windows" {
			base = os.TempDir()
		} else {
			base = "/tmp"
		}
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
	r.Revision++
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(s.RecordPath(r.ID), data, 0o600)
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
	for _, path := range []string{s.LogPath(id), s.LogPath(id) + ".1", s.LogPath(id) + ".cursor", s.RecordPath(id) + ".tmp", s.LogPath(id) + ".cursor.tmp"} {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	err := os.Remove(s.RecordPath(id))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// ReadCombinedLog returns the process log history in chronological order,
// concatenating the rotated backup (<id>.log.1) before the current log.
// Missing files are tolerated as long as at least one log generation exists.
func (s *Store) ReadCombinedLog(id int) ([]byte, error) {
	path := s.LogPath(id)
	var combined []byte

	oldData, oldErr := os.ReadFile(path + ".1")
	if oldErr == nil {
		combined = append(combined, oldData...)
	} else if !os.IsNotExist(oldErr) {
		return nil, oldErr
	}

	data, err := os.ReadFile(path)
	if err == nil {
		combined = append(combined, data...)
		return combined, nil
	}
	if !os.IsNotExist(err) {
		return nil, err
	}
	if len(combined) > 0 {
		return combined, nil
	}
	return nil, err
}

// LastAllocatedID survives explicit record removal so a later daemon never reuses
// a browser process identity. Legacy stores without a counter return zero.
func (s *Store) LastAllocatedID() (int, error) {
	data, err := os.ReadFile(filepath.Join(s.Dir(), "process-id"))
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	id, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || id < 0 {
		return 0, fmt.Errorf("invalid persisted process ID counter")
	}
	return id, nil
}

// ReserveID must be called under the daemon's allocation lock, before execution.
func (s *Store) ReserveID(id int) error {
	return writeAtomic(filepath.Join(s.Dir(), "process-id"), []byte(strconv.Itoa(id)), 0600)
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
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, perm)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	return syncDir(filepath.Dir(path))
}
func syncDir(path string) error {
	if runtime.GOOS == "windows" {
		return nil
	} // Windows cannot fsync directory handles.
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
