package connect

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
	gitstore "github.com/Tiago-0liveira/bonsai/internal/storage/git"
)

// Store holds associations under the Bonsai config directory with cross-process locking.
type Store struct {
	Path      string
	writeJSON func(string, []byte) error
}

// MaxStoreBytes bounds persisted Garden state before JSON parsing.
const MaxStoreBytes = 4 << 20

type diskStore struct {
	Version       int                      `json:"version,omitempty"`
	SchemaVersion int                      `json:"schema_version,omitempty"`
	Revision      uint64                   `json:"revision"`
	Associations  map[string]Association   `json:"associations"`
	Operations    map[string]Operation     `json:"operations"`
	Requests      map[string]requestRecord `json:"requests,omitempty"`
}
type requestRecord struct {
	Identity string          `json:"identity"`
	Result   json.RawMessage `json:"result"`
}

// OpenStore resolves the default config location without reading credentials.
func OpenStore() (*Store, error) {
	p, e := config.GardenAssociationsPath()
	if e != nil {
		return nil, e
	}
	return &Store{Path: p}, nil
}

// ExecutionKey derives local locking identity from LocalRepoID, never GardenID.
func ExecutionKey(id garden.LocalRepoID) string { return "garden-local:" + id.String() }
func (s *Store) read() (diskStore, error) {
	d := diskStore{SchemaVersion: 1, Associations: map[string]Association{}, Operations: map[string]Operation{}}
	f, e := os.Open(s.Path)
	if errors.Is(e, os.ErrNotExist) {
		return d, nil
	}
	if e != nil {
		return d, fmt.Errorf("%w: %v", ErrCorruptStore, e)
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, MaxStoreBytes+1))
	if e != nil {
		return d, fmt.Errorf("%w: %v", ErrCorruptStore, e)
	}
	if len(b) > MaxStoreBytes {
		return d, fmt.Errorf("%w: %w", ErrCorruptStore, ErrStoreTooLarge)
	}
	if !utf8.Valid(b) || garden.RejectDuplicateJSONKeys(b) != nil {
		return d, ErrCorruptStore
	}
	var head struct {
		Version       int `json:"version"`
		SchemaVersion int `json:"schema_version"`
	}
	if json.Unmarshal(b, &head) == nil && (head.SchemaVersion > 1 || (head.SchemaVersion == 0 && head.Version > 1)) {
		return d, ErrUnsupportedSchema
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if e = dec.Decode(&d); e != nil {
		return d, fmt.Errorf("%w: %v", ErrCorruptStore, e)
	}
	if dec.Decode(new(any)) != io.EOF || (d.SchemaVersion != 1 && !(d.SchemaVersion == 0 && d.Version == 1)) || d.Associations == nil || d.Operations == nil {
		return d, ErrCorruptStore
	}
	return d, nil
}

// Snapshot returns the current revision and local associations.
func (s *Store) Snapshot(ctx context.Context) (uint64, []Association, error) {
	var rev uint64
	var out []Association
	e := s.withLock(ctx, func(d diskStore) error {
		rev = d.Revision
		for _, a := range d.Associations {
			out = append(out, a)
		}
		return nil
	})
	return rev, out, e
}

// Operation loads a durable operation by its opaque operation ID.
func (s *Store) Operation(ctx context.Context, id string) (Operation, error) {
	var op Operation
	e := s.withLock(ctx, func(d diskStore) error {
		var ok bool
		op, ok = d.Operations[id]
		if !ok {
			return ErrOperationNotFound
		}
		return nil
	})
	return op, e
}

func (s *Store) operationAccess(ctx context.Context, op Operation) (Access, error) {
	var access Access
	e := s.withLock(ctx, func(d diskStore) error {
		a, ok := d.Associations[op.LocalRepoID.String()]
		if !ok {
			access = op.Access
			return nil
		}
		if a.Access < AccessWrite {
			access = a.Access
			return nil
		}
		if a.ActiveOperationID != op.ID {
			return ErrOperationForeign
		}
		access = a.Access
		return nil
	})
	return access, e
}

func (s *Store) checkAssociationAccess(ctx context.Context, local garden.LocalRepoID, live Access) error {
	return s.withLock(ctx, func(d diskStore) error {
		a, ok := d.Associations[local.String()]
		if ok && (a.Access < AccessWrite || live < AccessWrite) {
			return ErrReadOnly
		}
		return nil
	})
}

func ensureStoreDir(path string) error { return os.MkdirAll(filepath.Dir(path), 0700) }

// Remove clears an explicit association decision, including Skip, using revision checks.
func (s *Store) Remove(ctx context.Context, rev uint64, key string, id garden.LocalRepoID) (uint64, error) {
	var next uint64
	if strings.TrimSpace(key) == "" {
		return 0, fmt.Errorf("idempotency key required")
	}
	e := s.withLock(ctx, func(d diskStore) error {
		identity := "remove:" + id.String()
		if old, ok := d.Requests[key]; ok {
			if old.Identity != identity {
				return ErrIdempotency
			}
			return json.Unmarshal(old.Result, &next)
		}
		if d.Revision != rev {
			return ErrRevision
		}
		if _, ok := d.Associations[id.String()]; !ok {
			return fmt.Errorf("association not found")
		}
		delete(d.Associations, id.String())
		d.Revision++
		next = d.Revision
		if d.Requests == nil {
			d.Requests = map[string]requestRecord{}
		}
		result, _ := json.Marshal(next)
		d.Requests[key] = requestRecord{Identity: identity, Result: result}
		return s.write(d)
	})
	return next, e
}

// Put saves an association using revision and idempotency checks.
func (s *Store) Put(ctx context.Context, rev uint64, key string, a Association) (uint64, error) {
	var next uint64
	if strings.TrimSpace(key) == "" {
		return 0, fmt.Errorf("idempotency key required")
	}
	raw, _ := json.Marshal(a)
	identity := string(raw)
	e := s.withLock(ctx, func(d diskStore) error {
		if old, ok := d.Requests[key]; ok {
			if old.Identity != identity {
				return ErrIdempotency
			}
			return json.Unmarshal(old.Result, &next)
		}
		if d.Revision != rev {
			return ErrRevision
		}
		if a.LocalRepoID == "" || a.GardenID.Repo == 0 {
			return fmt.Errorf("invalid association")
		}
		if a.Mode == Skip && a.ActiveOperationID != "" {
			return fmt.Errorf("skip cannot have active operation")
		}
		if a.CreatedAt.IsZero() {
			a.CreatedAt = time.Now().UTC()
		}
		a.UpdatedAt = time.Now().UTC()
		d.Associations[a.LocalRepoID.String()] = a
		d.Revision++
		next = d.Revision
		result, _ := json.Marshal(next)
		if d.Requests == nil {
			d.Requests = map[string]requestRecord{}
		}
		d.Requests[key] = requestRecord{Identity: identity, Result: result}
		return s.write(d)
	})
	return next, e
}
func (s *Store) saveOperation(ctx context.Context, op Operation) error {
	return s.withLock(ctx, func(d diskStore) error {
		if d.Operations == nil {
			d.Operations = map[string]Operation{}
		}
		d.Operations[op.ID] = op
		if a, ok := d.Associations[op.LocalRepoID.String()]; ok {
			a.ActiveOperationID = op.ID
			d.Associations[op.LocalRepoID.String()] = a
		}
		d.Revision++
		return s.write(d)
	})
}

func (s *Store) finishOperation(ctx context.Context, op Operation) error {
	return s.withLock(ctx, func(d diskStore) error {
		for _, st := range op.Steps {
			if st.Status != Done {
				return nil
			}
		}
		if a, ok := d.Associations[op.LocalRepoID.String()]; ok && a.ActiveOperationID == op.ID {
			a.ActiveOperationID = ""
			d.Associations[op.LocalRepoID.String()] = a
			d.Revision++
			return s.write(d)
		}
		return nil
	})
}
func (s *Store) withLock(ctx context.Context, fn func(diskStore) error) error {
	if e := os.MkdirAll(filepath.Dir(s.Path), 0700); e != nil {
		return e
	}
	for {
		if e := ctx.Err(); e != nil {
			return e
		}
		l, e := procstore.TryLock(s.Path + ".lock")
		if e == nil {
			defer l.Unlock()
			d, e := s.read()
			if e != nil {
				return e
			}
			return fn(d)
		}
		if !errors.Is(e, procstore.ErrLocked) {
			return e
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}
func (s *Store) write(d diskStore) error {
	d.SchemaVersion = 1
	d.Version = 0
	b, e := json.Marshal(d)
	if e != nil {
		return e
	}
	if s.writeJSON != nil {
		return s.writeJSON(s.Path, b)
	}
	return gitstore.WriteJSON(s.Path, b)
}
