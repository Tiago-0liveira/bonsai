package agents

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type SessionStore interface {
	Create(Account, string) (Session, error)
	Get(SessionID) (Session, error)
	Cleanup(Session) error
}

// SessionLister is implemented by stores that can enumerate their records.
type SessionLister interface {
	List() ([]Session, error)
}

type FileSessionStore struct {
	root string
}

func NewFileSessionStore(root string) (*FileSessionStore, error) {
	if root == "" {
		var err error
		root, err = DefaultSessionsRoot()
		if err != nil {
			return nil, err
		}
	}
	s := &FileSessionStore{root: filepath.Clean(root)}
	if err := ensurePrivateDir(s.root); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *FileSessionStore) Root() string { return s.root }

func (s *FileSessionStore) Create(account Account, workDir string) (Session, error) {
	for attempts := 0; attempts < 8; attempts++ {
		id, err := NewSessionID()
		if err != nil {
			return Session{}, err
		}
		runtimeDir := filepath.Join(s.root, string(id))
		if err := os.Mkdir(runtimeDir, 0o700); errors.Is(err, os.ErrExist) {
			continue
		} else if err != nil {
			return Session{}, err
		}
		_ = os.Chmod(runtimeDir, 0o700)
		homeDir := filepath.Join(runtimeDir, "home")
		if err := ensurePrivateDir(homeDir); err != nil {
			_ = os.RemoveAll(runtimeDir)
			return Session{}, err
		}
		if workDir != "" {
			if abs, err := filepath.Abs(workDir); err == nil {
				workDir = filepath.Clean(abs)
			}
		}
		session := Session{
			ID: id, Provider: account.Provider, AccountID: account.ID,
			WorkDir: workDir, RuntimeDir: runtimeDir, HomeDir: homeDir,
			CreatedAt: time.Now().UTC(),
		}
		rec := sessionRecord{
			Version: 1, ID: session.ID, Provider: session.Provider,
			AccountID: session.AccountID, WorkDir: session.WorkDir, CreatedAt: session.CreatedAt,
		}
		if err := atomicWriteJSON(filepath.Join(runtimeDir, "session.json"), rec); err != nil {
			_ = os.RemoveAll(runtimeDir)
			return Session{}, err
		}
		return session, nil
	}
	return Session{}, fmt.Errorf("unable to allocate unique session id")
}

func (s *FileSessionStore) Get(id SessionID) (Session, error) {
	if !validOpaqueID(string(id)) {
		return Session{}, fmt.Errorf("%w: %s", ErrSessionNotFound, id)
	}
	runtimeDir := filepath.Join(s.root, string(id))
	data, err := os.ReadFile(filepath.Join(runtimeDir, "session.json"))
	if errors.Is(err, os.ErrNotExist) {
		return Session{}, fmt.Errorf("%w: %s", ErrSessionNotFound, id)
	}
	if err != nil {
		return Session{}, err
	}
	var rec sessionRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return Session{}, err
	}
	if rec.Version != 1 || rec.ID != id {
		return Session{}, fmt.Errorf("invalid session metadata")
	}
	return Session{
		ID: rec.ID, Provider: rec.Provider, AccountID: rec.AccountID,
		WorkDir: rec.WorkDir, RuntimeDir: runtimeDir,
		HomeDir: filepath.Join(runtimeDir, "home"), CreatedAt: rec.CreatedAt,
	}, nil
}

// List returns every readable session record, oldest first. Unreadable entries
// are skipped: a record is being written or was left half-removed.
func (s *FileSessionStore) List() ([]Session, error) {
	entries, err := os.ReadDir(s.root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []Session
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		session, err := s.Get(SessionID(entry.Name()))
		if err != nil {
			continue
		}
		out = append(out, session)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.Before(out[j].CreatedAt)
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func (s *FileSessionStore) Cleanup(session Session) error {
	if !validOpaqueID(string(session.ID)) {
		return fmt.Errorf("invalid session id")
	}
	expected := filepath.Join(s.root, string(session.ID))
	expAbs, err := filepath.Abs(expected)
	if err != nil {
		return err
	}
	gotAbs, err := filepath.Abs(session.RuntimeDir)
	if err != nil {
		return err
	}
	if filepath.Clean(expAbs) != filepath.Clean(gotAbs) {
		return fmt.Errorf("refusing to clean session outside configured root")
	}
	rel, err := filepath.Rel(s.root, gotAbs)
	if err != nil || rel == "." || rel == ".." || filepath.IsAbs(rel) || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("refusing unsafe session cleanup")
	}
	if err := os.RemoveAll(gotAbs); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// reservedIDOK is the host's session ID rule: sess_ and 1..64 lowercase hex.
func reservedIDOK(id SessionID) bool {
	rest, ok := strings.CutPrefix(string(id), "sess_")
	if !ok || rest == "" || len(rest) > 64 {
		return false
	}
	for _, r := range rest {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

// canonicalWorkDir resolves symlinks when the directory exists, so two spellings
// of one directory compare equal.
func canonicalWorkDir(dir string) string {
	if dir == "" {
		return ""
	}
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	dir = filepath.Clean(dir)
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		return filepath.Clean(resolved)
	}
	return dir
}

// CreateWithID creates the session id names, or returns it unchanged when it
// already exists for the same provider, account and working directory. It lets
// a caller record a start before the holder runs: the ID is known up front and
// a retry finds the same runtime instead of allocating another.
func (s *FileSessionStore) CreateWithID(account Account, workDir string, id SessionID) (Session, error) {
	if !reservedIDOK(id) {
		return Session{}, fmt.Errorf("%w: malformed session id", ErrInvalidLaunch)
	}
	lock, err := LockFile(filepath.Join(s.root, ".reserve.lock"))
	if err != nil {
		return Session{}, err
	}
	defer lock.Unlock()
	runtimeDir := filepath.Join(s.root, string(id))
	wantDir := canonicalWorkDir(workDir)
	if existing, err := s.Get(id); err == nil {
		if existing.Provider != account.Provider || existing.AccountID != account.ID || canonicalWorkDir(existing.WorkDir) != wantDir {
			return Session{}, fmt.Errorf("%w: %s", ErrSessionConflict, id)
		}
		return existing, nil
	} else if !errors.Is(err, ErrSessionNotFound) {
		return Session{}, fmt.Errorf("%w: %s has unreadable metadata", ErrSessionConflict, id)
	}
	created := false
	if err := os.Mkdir(runtimeDir, 0o700); err == nil {
		created = true
	} else if errors.Is(err, os.ErrExist) {
		// A directory without metadata may be a half-made session of ours, but
		// anything beyond the home directory is not ours to overwrite.
		entries, readErr := os.ReadDir(runtimeDir)
		if readErr != nil {
			return Session{}, readErr
		}
		for _, entry := range entries {
			if entry.Name() != "home" || !entry.IsDir() {
				return Session{}, fmt.Errorf("%w: %s holds unknown files", ErrSessionConflict, id)
			}
		}
	} else {
		return Session{}, err
	}
	_ = os.Chmod(runtimeDir, 0o700)
	fail := func(err error) (Session, error) {
		if created {
			_ = os.RemoveAll(runtimeDir)
		}
		return Session{}, err
	}
	homeDir := filepath.Join(runtimeDir, "home")
	if err := ensurePrivateDir(homeDir); err != nil {
		return fail(err)
	}
	session := Session{
		ID: id, Provider: account.Provider, AccountID: account.ID,
		WorkDir: wantDir, RuntimeDir: runtimeDir, HomeDir: homeDir,
		CreatedAt: time.Now().UTC(),
	}
	rec := sessionRecord{
		Version: 1, ID: session.ID, Provider: session.Provider,
		AccountID: session.AccountID, WorkDir: session.WorkDir, CreatedAt: session.CreatedAt,
	}
	if err := atomicWriteJSON(filepath.Join(runtimeDir, "session.json"), rec); err != nil {
		return fail(err)
	}
	return session, nil
}

var _ ReservedSessionStore = (*FileSessionStore)(nil)
