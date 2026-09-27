package agents

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type SessionStore interface {
	Create(Account, string) (Session, error)
	Get(SessionID) (Session, error)
	Cleanup(Session) error
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
