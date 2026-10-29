package ops

import (
	"bytes"
	"context"
	"crypto/sha256"
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
const MaxStoreBytes = 1 << 20

// MaxRecords caps the store; finished records are dropped oldest-first to make room.
const MaxRecords = 500

// Kind says which write a record tracks.
type Kind string

const (
	KindCapture Kind = "capture"
	KindImport  Kind = "import"
	// KindPublish tracks publishing one private draft: create, spec revision, approval, projection.
	KindPublish Kind = "publish"
)

// State is where a write stands. Unknown means GitHub may have applied it: it must be
// reconciled before anything is repeated.
type State string

const (
	StatePending State = "pending"
	StateUnknown State = "unknown"
	StateDone    State = "done"
	StateFailed  State = "failed"
)

// Record is one tracked write. Fields beyond the common ones belong to a Kind.
type Record struct {
	ID    string             `json:"id"`
	Kind  Kind               `json:"kind"`
	Local garden.LocalRepoID `json:"local_repo_id"`
	Repo  garden.RepoID      `json:"repo_id"`
	State State              `json:"state"`
	// Owner is the numeric ID of the account that ran the write.
	Owner string `json:"owner"`
	// ContentHash fingerprints the request, so one operation ID can never be replayed with other content.
	ContentHash string          `json:"content_hash"`
	Nonce       string          `json:"nonce,omitempty"`
	Title       string          `json:"title,omitempty"`
	Type        garden.ItemType `json:"type,omitempty"`
	IssueID     garden.IssueID  `json:"issue_id,omitempty"`
	Number      int64           `json:"number,omitempty"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
	// The fields below belong to KindPublish. Draft and DraftRevision bind the operation to the exact
	// private draft; the rest are the verified results of each step, kept so a resume reuses them.
	Draft             string `json:"draft,omitempty"`
	DraftRevision     uint64 `json:"draft_revision,omitempty"`
	IssueURL          string `json:"issue_url,omitempty"`
	SpecHash          string `json:"spec_hash,omitempty"`
	SpecCommentID     int64  `json:"spec_comment_id,omitempty"`
	ApprovalCommentID int64  `json:"approval_comment_id,omitempty"`
	// Step is the step a publish stopped at: issue, spec, approval or verify.
	Step string `json:"step,omitempty"`
}

type disk struct {
	SchemaVersion int               `json:"schema_version"`
	Records       map[string]Record `json:"records"`
}

// Store is a locked, atomically replaced JSON file shared by every Bonsai process.
type Store struct {
	Path string
}

// Open resolves the default config location.
func Open() (*Store, error) {
	p, err := config.GardenOperationsPath()
	if err != nil {
		return nil, err
	}
	return &Store{Path: p}, nil
}

// TryOperationLock serializes a remote operation across processes without holding
// the JSON store lock during network calls. The OS releases it after a crash.
func (s *Store) TryOperationLock(ctx context.Context, id string) (*procstore.FileLock, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0700); err != nil {
		return nil, err
	}
	return procstore.TryLock(fmt.Sprintf("%s.operation-%x.lock", s.Path, sha256.Sum256([]byte(id))))
}

func (r Record) valid() bool {
	switch r.Kind {
	case KindCapture, KindImport:
	case KindPublish:
		if r.Draft == "" || r.DraftRevision == 0 {
			return false
		}
	default:
		return false
	}
	switch r.State {
	case StatePending, StateUnknown, StateDone, StateFailed:
	default:
		return false
	}
	return r.ID != "" && r.Local != "" && r.Repo != 0
}

func (s *Store) read() (disk, error) {
	d := disk{SchemaVersion: 1, Records: map[string]Record{}}
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
	if json.Unmarshal(b, &head) == nil && head.SchemaVersion > 1 {
		return d, ErrUnsupportedSchema
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	d.Records = nil
	if err = dec.Decode(&d); err != nil {
		return disk{}, fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	if dec.Decode(new(any)) != io.EOF || d.SchemaVersion != 1 || d.Records == nil || len(d.Records) > MaxRecords {
		return disk{}, ErrCorrupt
	}
	for id, r := range d.Records {
		if id != r.ID || !r.valid() {
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
	d.SchemaVersion = 1
	if len(d.Records) > MaxRecords {
		trim(&d)
		if len(d.Records) > MaxRecords {
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

// trim drops the oldest finished records until the cap holds. Unfinished ones are never dropped:
// they are the evidence reconcile needs.
func trim(d *disk) {
	var done []Record
	for _, r := range d.Records {
		if r.State == StateDone || r.State == StateFailed {
			done = append(done, r)
		}
	}
	sort.Slice(done, func(i, j int) bool { return done[i].UpdatedAt.Before(done[j].UpdatedAt) })
	for _, r := range done {
		if len(d.Records) <= MaxRecords {
			return
		}
		delete(d.Records, r.ID)
	}
}

// Get returns one record.
func (s *Store) Get(ctx context.Context, id string) (Record, error) {
	var out Record
	err := s.withLock(ctx, func(d *disk) (bool, error) {
		r, ok := d.Records[id]
		if !ok {
			return false, ErrNotFound
		}
		out = r
		return false, nil
	})
	return out, err
}

// List returns the records of one local clone, oldest first.
func (s *Store) List(ctx context.Context, local garden.LocalRepoID) ([]Record, error) {
	var out []Record
	err := s.withLock(ctx, func(d *disk) (bool, error) {
		for _, r := range d.Records {
			if r.Local == local {
				out = append(out, r)
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

// Update runs fn on the record (found reports whether it exists) under the lock and stores the
// result. A returned error aborts without writing. The new record keeps the ID it was found under.
func (s *Store) Update(ctx context.Context, id string, fn func(cur Record, found bool) (Record, error)) (Record, error) {
	var out Record
	err := s.withLock(ctx, func(d *disk) (bool, error) {
		cur, found := d.Records[id]
		next, err := fn(cur, found)
		if err != nil {
			return false, err
		}
		next.ID = id
		if !next.valid() {
			return false, fmt.Errorf("%w: record", ErrCorrupt)
		}
		d.Records[id] = next
		out = next
		return true, nil
	})
	return out, err
}
