package reviewpass

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"time"
	"unicode/utf8"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
	gitstore "github.com/Tiago-0liveira/bonsai/internal/storage/git"
)

const schemaVersion = 1

type disk struct {
	SchemaVersion int             `json:"schema_version"`
	Records       map[string]Pass `json:"passes"`
}

// FileStore never evicts passes: full, corrupt or newer stores refuse writes.
type FileStore struct{ Path string }

var _ Store = (*FileStore)(nil)

func Open() (*FileStore, error) {
	p, err := config.GardenReviewPassPath()
	if err != nil {
		return nil, err
	}
	return &FileStore{Path: p}, nil
}

func (s *FileStore) read() (disk, error) {
	d := disk{SchemaVersion: schemaVersion, Records: map[string]Pass{}}
	f, err := os.Open(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return d, nil
	}
	if err != nil {
		return disk{}, fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, MaxStoreBytes+1))
	if err != nil {
		return disk{}, fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	if len(b) > MaxStoreBytes {
		return disk{}, fmt.Errorf("%w: file", ErrTooLarge)
	}
	if !utf8.Valid(b) || garden.RejectDuplicateJSONKeys(b) != nil {
		return disk{}, ErrCorrupt
	}
	// Probe only the exact key so a folded spelling cannot hide a future schema.
	var head map[string]json.RawMessage
	if json.Unmarshal(b, &head) != nil {
		return disk{}, ErrCorrupt
	}
	var version int
	if json.Unmarshal(head["schema_version"], &version) == nil && version > schemaVersion {
		return disk{}, ErrUnsupportedSchema
	}
	d = disk{}
	if !canonical(b, &d) || d.SchemaVersion != schemaVersion || d.Records == nil || len(d.Records) > MaxPasses {
		return disk{}, ErrCorrupt
	}
	for id, a := range d.Records {
		if id != a.ID || !a.valid() {
			return disk{}, ErrCorrupt
		}
	}
	return d, nil
}

// canonical rejects parser aliases and discarded fields, including nested ones.
func canonical(raw []byte, v any) bool {
	if garden.RejectDuplicateJSONKeys(raw) != nil || json.Unmarshal(raw, v) != nil {
		return false
	}
	encoded, err := json.Marshal(v)
	if err != nil {
		return false
	}
	var before, after any
	original := json.NewDecoder(bytes.NewReader(raw))
	original.UseNumber()
	roundTrip := json.NewDecoder(bytes.NewReader(encoded))
	roundTrip.UseNumber()
	return original.Decode(&before) == nil && roundTrip.Decode(&after) == nil && reflect.DeepEqual(before, after)
}

func (s *FileStore) write(d disk) error {
	d.SchemaVersion = schemaVersion
	b, err := json.Marshal(d)
	if err != nil {
		return err
	}
	if len(b) > MaxStoreBytes {
		return ErrTooLarge
	}
	return gitstore.WriteJSON(s.Path, b)
}

// withLock runs fn under the short transaction lock, never while an external
// call is in flight. fn reports whether to write the file back.
func (s *FileStore) withLock(ctx context.Context, fn func(*disk) (bool, error)) error {
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return err
	}
	unlock, err := lockFile(ctx, s.Path+".lock")
	if err != nil {
		return err
	}
	defer unlock()
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

// lockFile waits for a cross-process lock until ctx ends.
func lockFile(ctx context.Context, path string) (func(), error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		l, err := procstore.TryLock(path)
		if err == nil {
			return func() { _ = l.Unlock() }, nil
		}
		if !errors.Is(err, procstore.ErrLocked) {
			return nil, err
		}
		timer := time.NewTimer(10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func (s *FileStore) Get(ctx context.Context, id string) (Pass, error) {
	var out Pass
	err := s.withLock(ctx, func(d *disk) (bool, error) {
		p, ok := d.Records[id]
		if !ok {
			return false, ErrNotFound
		}
		out = clonePass(p)
		return false, nil
	})
	return out, err
}

// List returns oldest first; issue zero includes every issue in the local repo.
func (s *FileStore) List(ctx context.Context, local garden.LocalRepoID, issue garden.IssueID) ([]Pass, error) {
	return s.list(ctx, func(p Pass) bool { return p.Snapshot.Local == local && (issue == 0 || p.Snapshot.Issue == issue) })
}
func (s *FileStore) ForAttempt(ctx context.Context, attemptID string) ([]Pass, error) {
	return s.list(ctx, func(p Pass) bool { return p.Snapshot.AttemptID == attemptID })
}
func (s *FileStore) list(ctx context.Context, match func(Pass) bool) ([]Pass, error) {
	var out []Pass
	err := s.withLock(ctx, func(d *disk) (bool, error) {
		for _, p := range d.Records {
			if match(p) {
				out = append(out, clonePass(p))
			}
		}
		return false, nil
	})
	sort.Slice(out, func(i, j int) bool {
		if !out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].CreatedAt.Before(out[j].CreatedAt)
		}
		return out[i].ID < out[j].ID
	})
	return out, err
}
func (s *FileStore) Reserve(ctx context.Context, p Pass) (Pass, bool, error) {
	var out Pass
	created := false
	err := s.withLock(ctx, func(d *disk) (bool, error) {
		if prior, ok := d.Records[p.ID]; ok {
			if !sameJSON(prior, p) {
				return false, ErrConflict
			}
			out = clonePass(prior)
			return false, nil
		}
		if !p.valid() {
			return false, ErrInvalid
		}
		if len(d.Records) >= MaxPasses {
			return false, ErrFull
		}
		for _, prior := range d.Records {
			if prior.Snapshot.AttemptID == p.Snapshot.AttemptID && prior.State.Active() {
				return false, ErrActive
			}
		}
		d.Records[p.ID] = clonePass(p)
		out = clonePass(p)
		created = true
		return true, nil
	})
	if err != nil {
		return Pass{}, false, err
	}
	return out, created, nil
}

func sameIdentity(a, b Pass) bool {
	return a.ID == b.ID && sameJSON(a.Snapshot, b.Snapshot) && a.Authorization.ActorID == b.Authorization.ActorID && a.Authorization.PreviewID == b.Authorization.PreviewID && a.Authorization.ConfirmedAt.Equal(b.Authorization.ConfirmedAt) && a.Authorization.EscalationAcknowledged == b.Authorization.EscalationAcknowledged && a.CreatedAt.Equal(b.CreatedAt)
}
func writeOnce(a, b string) bool { return a == "" || a == b }
func sameReferences(a, b Pass) bool {
	if !writeOnce(a.ImplementationRun, b.ImplementationRun) || !writeOnce(a.ReviewedHead, b.ReviewedHead) || !writeOnce(a.FinalHead, b.FinalHead) || !writeOnce(a.Report, b.Report) {
		return false
	}
	return true
}
func (s *FileStore) Update(ctx context.Context, id string, fn func(Pass) (Pass, bool, error)) (Pass, error) {
	var out Pass
	err := s.withLock(ctx, func(d *disk) (bool, error) {
		cur, ok := d.Records[id]
		if !ok {
			return false, ErrNotFound
		}
		next, changed, err := fn(clonePass(cur))
		if err != nil {
			return false, err
		}
		if !changed {
			out = clonePass(cur)
			return false, nil
		}
		if !sameIdentity(cur, next) || !sameReferences(cur, next) {
			return false, ErrImmutable
		}
		if err := validateReviewerTransition(cur.Reviewer, next.Reviewer); err != nil {
			return false, err
		}
		if !CanMove(cur.State, next.State) {
			return false, ErrInvalid
		}
		if cur.Revision == math.MaxUint64 {
			return false, ErrFull
		}
		next.Revision = cur.Revision + 1
		next.UpdatedAt = time.Now().UTC()
		if !next.valid() {
			return false, ErrInvalid
		}
		d.Records[id] = clonePass(next)
		out = clonePass(next)
		return true, nil
	})
	if err != nil {
		return Pass{}, err
	}
	return out, nil
}

// Lock is independent of the transaction lock, so a caller may hold it during
// later external work without blocking record updates.
func (s *FileStore) Lock(ctx context.Context, id string) (func(), error) {
	// Check the schema on this entry point too, without holding the transaction
	// lock while waiting for the per-pass lock.
	if err := s.withLock(ctx, func(*disk) (bool, error) { return false, nil }); err != nil {
		return nil, err
	}
	sum := sha256.Sum256([]byte(id))
	return lockFile(ctx, filepath.Join(filepath.Dir(s.Path), ".review-pass-"+hex.EncodeToString(sum[:])+".lock"))
}

func sameJSON(a, b any) bool {
	x, err := json.Marshal(a)
	if err != nil {
		return false
	}
	y, err := json.Marshal(b)
	return err == nil && bytes.Equal(x, y)
}
