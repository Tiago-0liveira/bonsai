package drafts

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
	gitstore "github.com/Tiago-0liveira/bonsai/internal/storage/git"
)

const (
	SchemaVersion = 1
	MaxStoreBytes = 8 << 20
	MaxDrafts     = 1000
)

type disk struct {
	SchemaVersion int          `json:"schema_version"`
	Drafts        map[ID]Draft `json:"drafts"`
}

// Store serializes each read-modify-replace across processes. The last good
// snapshot is returned with read errors so callers can retain known content.
type Store struct {
	mu    sync.Mutex
	path  string
	now   func() time.Time
	newID func() ID
	write func(string, []byte) error
	known *disk
}

// New injects a file in a dedicated private directory, clock and local ID source.
// Nil clock and ID functions select production defaults. No filesystem IO occurs.
func New(path string, now func() time.Time, newID func() ID) (*Store, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) == filepath.Dir(filepath.Clean(path)) {
		return nil, fmt.Errorf("%w: absolute file path required", ErrInvalid)
	}
	if now == nil {
		now = time.Now
	}
	if newID == nil {
		newID = func() ID {
			var b [16]byte
			_, _ = rand.Read(b[:])
			return ID("draft-" + hex.EncodeToString(b[:]))
		}
	}
	return &Store{path: filepath.Clean(path), now: now, newID: newID, write: gitstore.WriteJSON}, nil
}

func Open() (*Store, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return nil, fmt.Errorf("private draft config: %w", err)
	}
	return New(filepath.Join(dir, "bonsai", "garden", "drafts", "drafts.json"), nil, nil)
}

func privatePath(path string, dir bool) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) && !dir {
		return nil
	}
	if err != nil {
		return err
	}
	if (dir && !info.IsDir()) || (!dir && !info.Mode().IsRegular()) {
		return fmt.Errorf("%w: non-regular storage path", ErrInvalid)
	}
	mode := os.FileMode(0600)
	if dir {
		mode = 0700
	}
	return os.Chmod(path, mode)
}

func (s *Store) read() (disk, error) {
	empty := disk{SchemaVersion: SchemaVersion, Drafts: map[ID]Draft{}}
	f, err := os.Open(s.path)
	if errors.Is(err, os.ErrNotExist) {
		if s.known != nil && len(s.known.Drafts) != 0 {
			return disk{}, ErrCorrupt
		}
		return empty, nil
	}
	if err != nil {
		return disk{}, fmt.Errorf("read private drafts: %w", err)
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, MaxStoreBytes+1))
	if err != nil {
		return disk{}, fmt.Errorf("read private drafts: %w", err)
	}
	if len(b) > MaxStoreBytes {
		return disk{}, ErrTooLarge
	}
	if !utf8.Valid(b) || garden.RejectDuplicateJSONKeys(b) != nil {
		return disk{}, ErrCorrupt
	}
	var head struct {
		SchemaVersion int `json:"schema_version"`
	}
	if json.Unmarshal(b, &head) == nil && head.SchemaVersion > SchemaVersion {
		return disk{}, ErrUnsupportedSchema
	}
	var d disk
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&d); err != nil {
		return disk{}, fmt.Errorf("%w: JSON", ErrCorrupt)
	}
	if dec.Decode(new(any)) != io.EOF || d.SchemaVersion != SchemaVersion || d.Drafts == nil || len(d.Drafts) > MaxDrafts {
		return disk{}, ErrCorrupt
	}
	for id, draft := range d.Drafts {
		if id != draft.ID || !draft.valid() {
			return disk{}, ErrCorrupt
		}
	}
	if s.known != nil {
		for id, known := range s.known.Drafts {
			cur, ok := d.Drafts[id]
			if !ok || cur.Scope != known.Scope || cur.Revision < known.Revision ||
				(cur.Revision == known.Revision && !sameDraft(cur, known)) {
				return disk{}, ErrCorrupt
			}
		}
	}
	return d, nil
}

func sameDraft(a, b Draft) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return bytes.Equal(x, y)
}

func (s *Store) transaction(ctx context.Context, fn func(*disk) (bool, error)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("private draft directory: %w", err)
	}
	if err := privatePath(dir, true); err != nil {
		return fmt.Errorf("private draft directory: %w", err)
	}
	if err := privatePath(s.path+".lock", false); err != nil {
		return fmt.Errorf("private draft lock: %w", err)
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		lock, err := procstore.TryLock(s.path + ".lock")
		if err == nil {
			defer lock.Unlock()
			if err := privatePath(s.path+".lock", false); err != nil {
				return fmt.Errorf("private draft lock: %w", err)
			}
			if err := privatePath(s.path, false); err != nil {
				return fmt.Errorf("private draft file: %w", err)
			}
			d, err := s.read()
			if err != nil {
				return err
			}
			s.remember(d)
			write, result := fn(&d)
			if !write {
				return result
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			b, err := json.Marshal(d)
			if err != nil {
				return fmt.Errorf("encode private drafts: %w", err)
			}
			if len(b) > MaxStoreBytes || len(d.Drafts) > MaxDrafts {
				return ErrTooLarge
			}
			if err := s.write(s.path, b); err != nil {
				return fmt.Errorf("replace private drafts: %w", err)
			}
			s.remember(d)
			return result
		}
		if !errors.Is(err, procstore.ErrLocked) {
			return fmt.Errorf("lock private drafts: %w", err)
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func clone(d Draft) Draft {
	d.ConfirmedAnswers = append([]Answer(nil), d.ConfirmedAnswers...)
	d.Spec.Extra = append(d.Spec.Extra[:0:0], d.Spec.Extra...)
	if d.Publication.Issue != nil {
		issue := *d.Publication.Issue
		d.Publication.Issue = &issue
	}
	return d
}

func (s *Store) remember(d disk) {
	next := disk{SchemaVersion: d.SchemaVersion, Drafts: make(map[ID]Draft, len(d.Drafts))}
	for id, draft := range d.Drafts {
		next.Drafts[id] = clone(draft)
	}
	s.known = &next
}

// List returns active drafts, including unresolved publishing intents and conflict
// copies. includeInactive also returns archived and verified published drafts.
// On a failed read the last good scoped snapshot accompanies the error.
func (s *Store) List(ctx context.Context, scope Scope, includeInactive bool) ([]Draft, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !scope.valid() {
		return nil, ErrInvalid
	}
	err := s.transaction(ctx, func(*disk) (bool, error) { return false, nil })
	var out []Draft
	if s.known != nil {
		for _, d := range s.known.Drafts {
			if d.Scope == scope && (includeInactive || (d.ArchivedAt.IsZero() && d.Publication.State != Published)) {
				out = append(out, clone(d))
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID < out[j].ID
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out, err
}

func (s *Store) Read(ctx context.Context, scope Scope, id ID) (Draft, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !scope.valid() || id == "" {
		return Draft{}, ErrInvalid
	}
	err := s.transaction(ctx, func(d *disk) (bool, error) {
		if cur, ok := d.Drafts[id]; !ok || cur.Scope != scope {
			return false, ErrNotFound
		}
		return false, nil
	})
	if s.known != nil {
		if d, ok := s.known.Drafts[id]; ok && d.Scope == scope {
			return clone(d), err
		}
	}
	return Draft{}, err
}

// Save uses expected=0 and an empty ID to create. Revision and timestamps are
// store-owned. A stale save writes a separate unpublished recovery draft before
// returning ConflictError; failure to persist that copy returns the write error.
func (s *Store) Save(ctx context.Context, scope Scope, draft Draft, expected uint64) (Draft, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !scope.valid() || (draft.Scope != (Scope{}) && draft.Scope != scope) {
		return Draft{}, ErrInvalid
	}
	draft = clone(draft)
	var out Draft
	err := s.transaction(ctx, func(d *disk) (bool, error) {
		now := s.now().UTC()
		if now.IsZero() {
			return false, ErrInvalid
		}
		if draft.ID == "" {
			if expected != 0 {
				return false, ErrInvalid
			}
			id, err := s.freshID(d)
			if err != nil {
				return false, err
			}
			draft.ID, draft.Scope, draft.Revision = id, scope, 1
			draft.CreatedAt, draft.UpdatedAt = now, now
			draft.ArchivedAt, draft.ConflictOf = time.Time{}, ""
			if draft.Publication.State == "" {
				draft.Publication.State = Unpublished
			}
			if draft.Publication.State != Unpublished || !draft.valid() {
				return false, ErrInvalid
			}
		} else {
			cur, ok := d.Drafts[draft.ID]
			if !ok || cur.Scope != scope {
				return false, ErrNotFound
			}
			if cur.Revision != expected {
				id, err := s.freshID(d)
				if err != nil {
					return false, err
				}
				draft.ID, draft.ConflictOf, draft.Scope, draft.Revision = id, cur.ID, scope, 1
				draft.CreatedAt, draft.UpdatedAt, draft.ArchivedAt = now, now, time.Time{}
				// A recovery edit cannot duplicate a remote publication identity.
				draft.Publication = Publication{State: Unpublished}
				if !draft.valid() {
					return false, ErrInvalid
				}
				d.Drafts[id] = draft
				out = draft
				return true, &ConflictError{Current: clone(cur), Incoming: clone(draft)}
			}
			if cur.Revision == math.MaxUint64 || !transition(cur.Publication, draft.Publication) {
				return false, ErrInvalid
			}
			if cur.Publication.State != Unpublished && !sameContent(cur, draft) {
				return false, ErrInvalid
			}
			draft.Scope, draft.Revision, draft.CreatedAt = scope, cur.Revision+1, cur.CreatedAt
			draft.ArchivedAt, draft.ConflictOf = cur.ArchivedAt, cur.ConflictOf
			if now.Before(cur.UpdatedAt) {
				now = cur.UpdatedAt
			}
			draft.UpdatedAt = now
			if !draft.valid() {
				return false, ErrInvalid
			}
		}
		d.Drafts[draft.ID] = draft
		out = draft
		return true, nil
	})
	if err != nil && !errors.Is(err, ErrConflict) {
		return Draft{}, err
	}
	return clone(out), err
}

func (s *Store) freshID(d *disk) (ID, error) {
	id := s.newID()
	if id == "" {
		return "", ErrInvalid
	}
	if _, exists := d.Drafts[id]; exists {
		return "", fmt.Errorf("%w: local ID collision", ErrInvalid)
	}
	return id, nil
}

// Archive never discards content. Publishing intents remain discoverable until
// their outcome is verified; a stale archive has no incoming content to fork.
func (s *Store) Archive(ctx context.Context, scope Scope, id ID, expected uint64) (Draft, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !scope.valid() || id == "" {
		return Draft{}, ErrInvalid
	}
	var out Draft
	err := s.transaction(ctx, func(d *disk) (bool, error) {
		cur, ok := d.Drafts[id]
		if !ok || cur.Scope != scope {
			return false, ErrNotFound
		}
		if cur.Revision != expected {
			return false, &ConflictError{Current: clone(cur)}
		}
		if cur.Publication.State == Publishing || cur.Revision == math.MaxUint64 {
			return false, ErrInvalid
		}
		now := s.now().UTC()
		if now.IsZero() {
			return false, ErrInvalid
		}
		if now.Before(cur.UpdatedAt) {
			now = cur.UpdatedAt
		}
		cur.Revision++
		cur.UpdatedAt, cur.ArchivedAt = now, now
		d.Drafts[id], out = cur, cur
		return true, nil
	})
	return clone(out), err
}
