package gym

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/Tiago-0liveira/bonsai/internal/core/git"
)

var (
	ErrBindingNotFound = errors.New("binding not found")
)

// Store manages durable Bonsai gym state on disk under the Git common directory.
type Store struct {
	commonDir string
	baseDir   string
	repoID    string
	mu        sync.Mutex
}

// NewStore initializes a Store anchored to the repository containing repoDir.
func NewStore(repoDir string) (*Store, error) {
	commonDir, err := git.CommonDir(repoDir)
	if err != nil {
		return nil, fmt.Errorf("resolving git common dir for %s: %w", repoDir, err)
	}

	baseDir := filepath.Join(commonDir, "bonsai", "gym")
	dirs := []string{
		baseDir,
		filepath.Join(baseDir, "worktrees"),
		filepath.Join(baseDir, "bindings"),
		filepath.Join(baseDir, "history"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, fmt.Errorf("creating gym directory %s: %w", d, err)
		}
	}

	s := &Store{
		commonDir: commonDir,
		baseDir:   baseDir,
	}

	repoID, err := s.initRepoID()
	if err != nil {
		return nil, err
	}
	s.repoID = repoID

	return s, nil
}

func (s *Store) lockPath() string {
	return filepath.Join(s.baseDir, "gym.lock")
}

// WithLock acquires the repository-level advisory lock while executing fn.
func (s *Store) WithLock(fn func() error) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	fl, err := AcquireFileLock(s.lockPath())
	if err != nil {
		return fmt.Errorf("acquiring gym file lock: %w", err)
	}
	defer func() {
		_ = fl.Release()
	}()

	return fn()
}

func (s *Store) initRepoID() (string, error) {
	path := filepath.Join(s.baseDir, "repo_id.json")
	var id string
	err := s.WithLock(func() error {
		data, err := os.ReadFile(path)
		if err == nil {
			var payload struct {
				RepoID string `json:"repo_id"`
			}
			if err := json.Unmarshal(data, &payload); err == nil && payload.RepoID != "" {
				id = payload.RepoID
				return nil
			}
		}

		id = NewUUID()
		payload := struct {
			RepoID string `json:"repo_id"`
		}{RepoID: id}
		return atomicWriteJSON(path, payload)
	})
	if err != nil {
		return "", fmt.Errorf("initializing repo ID: %w", err)
	}
	return id, nil
}

// RepoID returns the stable repository identifier.
func (s *Store) RepoID() string {
	return s.repoID
}

// GetClientID returns the installation client UUID from the user config directory.
func GetClientID() (string, error) {
	cfgDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locating user config dir: %w", err)
	}

	dir := filepath.Join(cfgDir, "bonsai", "gym")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("creating gym user config dir: %w", err)
	}

	path := filepath.Join(dir, "client.json")
	data, err := os.ReadFile(path)
	if err == nil {
		var cfg ClientConfig
		if err := json.Unmarshal(data, &cfg); err == nil && cfg.ClientID != "" {
			return cfg.ClientID, nil
		}
	}

	id := NewUUID()
	cfg := ClientConfig{ClientID: id}
	if err := atomicWriteJSON(path, cfg); err != nil {
		return "", fmt.Errorf("persisting client ID: %w", err)
	}
	return id, nil
}

// ResolveWorkspaceIdentity finds or creates a stable WorkspaceIdentity for worktreePath.
func (s *Store) ResolveWorkspaceIdentity(worktreePath string) (*WorkspaceIdentity, error) {
	var ws *WorkspaceIdentity
	err := s.WithLock(func() error {
		var err error
		ws, err = s.ResolveWorkspaceIdentityLocked(worktreePath)
		return err
	})
	return ws, err
}

// ResolveWorkspaceIdentityLocked resolves identity assuming the store lock is already held.
func (s *Store) ResolveWorkspaceIdentityLocked(worktreePath string) (*WorkspaceIdentity, error) {
	gitDir, err := git.GitDir(worktreePath)
	if err != nil {
		return nil, fmt.Errorf("resolving git dir: %w", err)
	}
	canonicalPath := git.CanonicalPath(worktreePath)

	wtDir := filepath.Join(s.baseDir, "worktrees")
	entries, err := os.ReadDir(wtDir)
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(wtDir, entry.Name()))
		if err != nil {
			continue
		}
		var id WorkspaceIdentity
		if err := json.Unmarshal(data, &id); err == nil && id.GitDir == gitDir {
			if id.Path != canonicalPath {
				id.Path = canonicalPath
				_ = atomicWriteJSON(filepath.Join(wtDir, entry.Name()), id)
			}
			return &id, nil
		}
	}

	id := WorkspaceIdentity{
		RepositoryID: s.repoID,
		WorktreeID:   NewUUID(),
		GitDir:       gitDir,
		Path:         canonicalPath,
	}
	path := filepath.Join(wtDir, id.WorktreeID+".json")
	if err := atomicWriteJSON(path, id); err != nil {
		return nil, err
	}
	return &id, nil
}

// GetBinding loads the active RunBinding for worktreeID.
func (s *Store) GetBinding(worktreeID string) (*RunBinding, error) {
	var binding *RunBinding
	err := s.WithLock(func() error {
		var err error
		binding, err = s.GetBindingLocked(worktreeID)
		return err
	})
	return binding, err
}

// GetBindingLocked loads the active RunBinding assuming the store lock is already held.
func (s *Store) GetBindingLocked(worktreeID string) (*RunBinding, error) {
	path := filepath.Join(s.baseDir, "bindings", worktreeID+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var b RunBinding
	if err := json.Unmarshal(data, &b); err != nil {
		return nil, err
	}
	return &b, nil
}

// SaveBinding saves an active or pending RunBinding.
func (s *Store) SaveBinding(b *RunBinding) error {
	return s.WithLock(func() error {
		return s.SaveBindingLocked(b)
	})
}

// SaveBindingLocked saves RunBinding assuming the store lock is already held.
func (s *Store) SaveBindingLocked(b *RunBinding) error {
	if b == nil || b.Workspace.WorktreeID == "" {
		return errors.New("cannot save binding without worktree ID")
	}
	path := filepath.Join(s.baseDir, "bindings", b.Workspace.WorktreeID+".json")
	return atomicWriteJSON(path, b)
}

// DeleteBinding removes the active binding for worktreeID.
func (s *Store) DeleteBinding(worktreeID string) error {
	path := filepath.Join(s.baseDir, "bindings", worktreeID+".json")
	return s.WithLock(func() error {
		err := os.Remove(path)
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	})
}

// ArchiveBinding copies binding to history and removes it from active bindings.
func (s *Store) ArchiveBinding(b *RunBinding) error {
	if b == nil || b.RunID == "" {
		return errors.New("cannot archive binding without run ID")
	}
	histPath := filepath.Join(s.baseDir, "history", b.RunID+".json")
	activePath := filepath.Join(s.baseDir, "bindings", b.Workspace.WorktreeID+".json")
	return s.WithLock(func() error {
		if err := atomicWriteJSON(histPath, b); err != nil {
			return err
		}
		_ = os.Remove(activePath)
		return nil
	})
}

// ListBindings lists all active bindings in the repository.
func (s *Store) ListBindings() ([]RunBinding, error) {
	var list []RunBinding
	err := s.WithLock(func() error {
		dir := filepath.Join(s.baseDir, "bindings")
		entries, err := os.ReadDir(dir)
		if err != nil {
			return err
		}
		for _, e := range entries {
			if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
				continue
			}
			data, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				continue
			}
			var b RunBinding
			if err := json.Unmarshal(data, &b); err == nil {
				list = append(list, b)
			}
		}
		return nil
	})
	return list, err
}

func atomicWriteJSON(target string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Dir(target)
	tmpFile, err := os.CreateTemp(dir, "gym-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmpFile.Name()
	defer func() {
		_ = os.Remove(tmpName)
	}()

	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		return err
	}
	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		return err
	}
	if err := tmpFile.Close(); err != nil {
		return err
	}

	return os.Rename(tmpName, target)
}
