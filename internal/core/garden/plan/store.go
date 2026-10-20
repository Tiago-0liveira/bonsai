package plan

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"
	"unicode/utf8"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
	gitstore "github.com/Tiago-0liveira/bonsai/internal/storage/git"
)

// MaxStoreBytes bounds persisted state before JSON parsing.
const MaxStoreBytes = 4 << 20

// MaxAttempts caps the store. Finished attempts are dropped oldest-first to make room; live ones never are.
const MaxAttempts = 2000

const schemaVersion = 1

type disk struct {
	SchemaVersion int                `json:"schema_version"`
	Attempts      map[string]Attempt `json:"attempts"`
}

// Store is a locked, atomically replaced JSON file shared by every Bonsai process.
type Store struct {
	Path string
}

// Open resolves the default config location.
func Open() (*Store, error) {
	p, err := config.GardenPlannerPath()
	if err != nil {
		return nil, err
	}
	return &Store{Path: p}, nil
}

func (s *Store) read() (disk, error) {
	d := disk{SchemaVersion: schemaVersion, Attempts: map[string]Attempt{}}
	f, err := os.Open(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return d, nil
	}
	if err != nil {
		return d, fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, MaxStoreBytes+1))
	if err != nil {
		return d, fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	if len(b) > MaxStoreBytes {
		return d, fmt.Errorf("%w: file", ErrTooLarge)
	}
	if !utf8.Valid(b) || garden.RejectDuplicateJSONKeys(b) != nil {
		return d, ErrCorrupt
	}
	var head struct {
		SchemaVersion int `json:"schema_version"`
	}
	if json.Unmarshal(b, &head) == nil && head.SchemaVersion > schemaVersion {
		return d, ErrUnsupportedSchema
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	d.Attempts = nil
	if err = dec.Decode(&d); err != nil {
		return disk{}, fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	if dec.Decode(new(any)) != io.EOF || d.SchemaVersion != schemaVersion || d.Attempts == nil || len(d.Attempts) > MaxAttempts {
		return disk{}, ErrCorrupt
	}
	for id, a := range d.Attempts {
		if id != a.ID || !a.valid() {
			return disk{}, ErrCorrupt
		}
	}
	return d, nil
}

func (s *Store) withLock(ctx context.Context, fn func(*disk) (bool, error)) error {
	if err := os.MkdirAll(filepath.Dir(s.Path), 0700); err != nil {
		return err
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		l, err := procstore.TryLock(s.Path + ".lock")
		if err == nil {
			defer l.Unlock()
			d, err := s.read()
			if err != nil {
				return err
			}
			write, err := fn(&d)
			if err != nil || !write {
				return err
			}
			return s.write(d)
		}
		if !errors.Is(err, procstore.ErrLocked) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func (s *Store) write(d disk) error {
	d.SchemaVersion = schemaVersion
	if len(d.Attempts) > MaxAttempts {
		trim(&d)
		if len(d.Attempts) > MaxAttempts {
			return ErrTooLarge
		}
	}
	b, err := json.Marshal(d)
	if err != nil {
		return err
	}
	if len(b) > MaxStoreBytes {
		return ErrTooLarge
	}
	return gitstore.WriteJSON(s.Path, b)
}

// trim drops the oldest finished attempts until the cap holds.
func trim(d *disk) {
	var done []Attempt
	for _, a := range d.Attempts {
		if !a.State.Live() {
			done = append(done, a)
		}
	}
	sort.Slice(done, func(i, j int) bool { return done[i].UpdatedAt.Before(done[j].UpdatedAt) })
	for _, a := range done {
		if len(d.Attempts) <= MaxAttempts {
			return
		}
		delete(d.Attempts, a.ID)
	}
}

// Get returns one attempt.
func (s *Store) Get(ctx context.Context, id string) (Attempt, error) {
	var out Attempt
	err := s.withLock(ctx, func(d *disk) (bool, error) {
		a, ok := d.Attempts[id]
		if !ok {
			return false, ErrNotFound
		}
		out = a
		return false, nil
	})
	return out, err
}

// List returns the attempts of one Issue of one local clone, oldest first.
func (s *Store) List(ctx context.Context, local garden.LocalRepoID, issue garden.IssueID) ([]Attempt, error) {
	var out []Attempt
	err := s.withLock(ctx, func(d *disk) (bool, error) {
		for _, a := range d.Attempts {
			if a.Local == local && a.Issue == issue {
				out = append(out, a)
			}
		}
		return false, nil
	})
	sort.Slice(out, func(i, j int) bool {
		if !out[i].RequestedAt.Equal(out[j].RequestedAt) {
			return out[i].RequestedAt.Before(out[j].RequestedAt)
		}
		return out[i].ID < out[j].ID
	})
	return out, err
}

// Create stores a new attempt unless a live one already exists for the Issue, which the check and the write
// share one lock for, so two processes cannot both start.
func (s *Store) Create(ctx context.Context, a Attempt) error {
	if !a.valid() || a.State != StateStarting {
		return fmt.Errorf("%w: attempt", ErrCorrupt)
	}
	return s.withLock(ctx, func(d *disk) (bool, error) {
		if _, dup := d.Attempts[a.ID]; dup {
			return false, fmt.Errorf("%w: duplicate attempt ID", ErrCorrupt)
		}
		for _, o := range d.Attempts {
			if o.Local == a.Local && o.Issue == a.Issue && o.State.Live() {
				return false, ErrAttemptActive
			}
		}
		d.Attempts[a.ID] = a
		return true, nil
	})
}

// Update runs fn on the attempt under the lock and stores the result unless fn returns changed=false. A
// returned error aborts without writing.
func (s *Store) Update(ctx context.Context, id string, fn func(cur Attempt) (next Attempt, changed bool, err error)) (Attempt, error) {
	var out Attempt
	err := s.withLock(ctx, func(d *disk) (bool, error) {
		cur, ok := d.Attempts[id]
		if !ok {
			return false, ErrNotFound
		}
		next, changed, err := fn(cur)
		if err != nil {
			return false, err
		}
		out = cur
		if !changed {
			return false, nil
		}
		next.ID = id
		if !next.valid() {
			return false, fmt.Errorf("%w: attempt", ErrCorrupt)
		}
		d.Attempts[id] = next
		out = next
		return true, nil
	})
	return out, err
}
