package launch

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
	"regexp"
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
	SchemaVersion int                `json:"schema_version"`
	Attempts      map[string]Attempt `json:"attempts"`
}

// FileStore is the locked, atomically replaced JSON file every Bonsai process
// shares. It is local execution history: attempts are never evicted, so a full
// store refuses new attempts instead of forgetting old ones.
type FileStore struct {
	Path string
}

var _ Store = (*FileStore)(nil)

// Open resolves the default location in the user's Bonsai config directory.
func Open() (*FileStore, error) {
	p, err := config.GardenLaunchPath()
	if err != nil {
		return nil, err
	}
	return &FileStore{Path: p}, nil
}

var (
	hashRE = regexp.MustCompile(`^[0-9a-f]{64}$`)
	idRE   = regexp.MustCompile(`^[A-Za-z0-9._:/-]{1,128}$`)
)

func validStage(s garden.Stage) bool {
	switch s {
	case "", garden.StageDraft, garden.StageReady, garden.StageInProgress, garden.StageReview, garden.StageBlocked, garden.StageDone:
		return true
	}
	return false
}

func validRunState(s string) bool {
	switch s {
	case "starting", "running", "stopping", "exited", "failed", "not-started":
		return true
	}
	return false
}

func terminalRun(s string) bool { return s == "exited" || s == "failed" || s == "not-started" }

var (
	sessionRE = regexp.MustCompile(`^sess_[0-9a-f]{1,64}$`)
	shaRE     = regexp.MustCompile(`^[0-9a-f]{40}([0-9a-f]{24})?$`)
)

var fixedCodes = map[string]bool{
	"": true, codePreflight: true, codeWorktreeFailed: true, codeHooksFailed: true, codeHooksUnknown: true,
	codeHolderStartFailed: true, codeHolderStartUnknown: true, codeHolderLost: true, codeHolderExitFailed: true,
	codeStopRequested: true, codeRemotePending: true, codeRemoteConflict: true, codePushPending: true,
	codePushUnknown: true, codePRPending: true, codePRUnknown: true, codePRUnavailable: true,
	codeCleanupUnsafe: true, codeCleanupUnknown: true,
}

// valid is the structural and consistency check applied to every record read and
// written. A record that fails it is corruption, never repaired: it could
// otherwise become an unclosable active reservation.
func (a Attempt) valid() bool {
	in, p := a.Inputs, a.Proposal
	if !idRE.MatchString(a.ID) || !hashRE.MatchString(a.RequestHash) || a.Local == "" || a.Repo == 0 || a.Issue == 0 || a.ActorID == "" ||
		p.OperationID != a.ID || p.Branch == "" || p.BaseRef == "" || !hashRE.MatchString(p.ConfigHash) || in.Spec.Ordinal < 1 || in.Plan.Ordinal < 1 || !filepath.IsAbs(p.Path) || p.RepositoryID == "" || p.RemoteName == "" || p.Repository == "" ||
		!shaRE.MatchString(p.BaseSHA) || p.BaseSHA != in.Plan.BaseCommit ||
		!hashRE.MatchString(in.Spec.RevisionHash) || !hashRE.MatchString(in.Spec.Fingerprint) || in.Spec.CommentID <= 0 || in.SpecApprovalID <= 0 ||
		!hashRE.MatchString(in.Plan.RevisionHash) || in.Plan.CommentID <= 0 || in.Plan.DecisionID <= 0 ||
		a.Provider == "" || a.AccountID == "" || a.Name == "" || a.Cols < 1 || a.Cols > 500 || a.Rows < 1 || a.Rows > 500 ||
		!a.State.valid() || !a.PR.State.valid() || !fixedCodes[a.PR.Code] || a.RequestedAt.IsZero() || a.UpdatedAt.IsZero() || a.Revision < 1 ||
		!validStage(a.DesiredStage) || !validStage(a.MirroredStage) || a.Steps == nil || a.Runs == nil {
		return false
	}
	if a.PR.State == PRLinked && a.PR.Number <= 0 {
		return false
	}
	known := map[Step]bool{}
	for _, s := range allSteps {
		known[s] = true
	}
	for name, rec := range a.Steps {
		if !known[name] || !rec.State.valid() || !fixedCodes[rec.Code] || rec.UpdatedAt.IsZero() {
			return false
		}
	}
	for i, r := range a.Runs {
		if r.Number != i+1 || r.OperationID != fmt.Sprintf("%s/start/%d", a.ID, r.Number) || !sessionRE.MatchString(r.SessionID) || !validRunState(r.State) {
			return false
		}
		switch r.State {
		case "exited", "failed", "running":
			if !r.Acknowledged {
				return false
			}
		case "not-started":
			if r.Acknowledged {
				return false
			}
		}
		if !terminalRun(r.State) && r.EndedAt != nil {
			return false
		}
	}
	if len(a.Runs) == 0 {
		if a.SessionID != "" {
			return false
		}
		switch a.State {
		case StateStarting, StateStartUnknown, StateRunning, StateAwaitingReview, StateFailed:
			return false
		}
		return true
	}
	last := a.Runs[len(a.Runs)-1]
	if a.SessionID != last.SessionID {
		return false
	}
	switch a.State {
	case StateRunning:
		// Running means the provider itself started: an unacknowledged run is not.
		return (last.State == "running" || last.State == "stopping") && last.Acknowledged
	case StateStarting, StateStartUnknown:
		return !terminalRun(last.State)
	case StateAwaitingReview:
		return last.State == "exited" && last.Acknowledged
	case StateFailed:
		return terminalRun(last.State) && last.Acknowledged
	case StateStopped, StateRecoverable, StatePreparing:
		return terminalRun(last.State)
	}
	return true
}

func cloneAttempt(a Attempt) Attempt {
	out := a
	out.Steps = make(map[Step]StepRecord, len(a.Steps))
	for k, v := range a.Steps {
		out.Steps[k] = v
	}
	out.Runs = make([]Run, len(a.Runs))
	for i, r := range a.Runs {
		out.Runs[i] = cloneRun(r)
	}
	if a.Options.FullAccess != nil {
		v := *a.Options.FullAccess
		out.Options.FullAccess = &v
	}
	if a.EndedAt != nil {
		v := *a.EndedAt
		out.EndedAt = &v
	}
	return out
}

func cloneRun(r Run) Run {
	out := r
	if r.ExitCode != nil {
		v := *r.ExitCode
		out.ExitCode = &v
	}
	if r.StartedAt != nil {
		v := *r.StartedAt
		out.StartedAt = &v
	}
	if r.EndedAt != nil {
		v := *r.EndedAt
		out.EndedAt = &v
	}
	return out
}

func sameBool(a, b *bool) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// sameIdentity reports whether next keeps everything an attempt was reserved
// with. Only execution state may change after the reservation.
func sameIdentity(cur, next Attempt) bool {
	return cur.ID == next.ID && cur.RequestHash == next.RequestHash && cur.Local == next.Local && cur.Repo == next.Repo &&
		cur.Issue == next.Issue && cur.Number == next.Number && cur.ActorID == next.ActorID && cur.Epoch == next.Epoch &&
		cur.Inputs == next.Inputs && cur.Proposal == next.Proposal && cur.Provider == next.Provider &&
		cur.AccountID == next.AccountID && cur.AccountName == next.AccountName &&
		cur.Options.Model == next.Options.Model && cur.Options.Effort == next.Options.Effort &&
		cur.Options.Sandbox == next.Options.Sandbox && cur.Options.Approval == next.Options.Approval &&
		cur.Options.PermissionMode == next.Options.PermissionMode && sameBool(cur.Options.FullAccess, next.Options.FullAccess) &&
		cur.Name == next.Name && cur.Cols == next.Cols && cur.Rows == next.Rows && cur.RequestedAt.Equal(next.RequestedAt)
}

func (s *FileStore) read() (disk, error) {
	d := disk{SchemaVersion: schemaVersion, Attempts: map[string]Attempt{}}
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
	var head struct {
		SchemaVersion int `json:"schema_version"`
	}
	if json.Unmarshal(b, &head) == nil && head.SchemaVersion > schemaVersion {
		return disk{}, ErrUnsupportedSchema
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	d.Attempts = nil
	if err := dec.Decode(&d); err != nil {
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

func (s *FileStore) Get(ctx context.Context, id string) (Attempt, error) {
	var out Attempt
	err := s.withLock(ctx, func(d *disk) (bool, error) {
		a, ok := d.Attempts[id]
		if !ok {
			return false, ErrNotFound
		}
		out = cloneAttempt(a)
		return false, nil
	})
	return out, err
}

// List returns the attempts of one local repository, oldest first. Issue zero
// lists every Issue.
func (s *FileStore) List(ctx context.Context, local garden.LocalRepoID, issue garden.IssueID) ([]Attempt, error) {
	var out []Attempt
	err := s.withLock(ctx, func(d *disk) (bool, error) {
		for _, a := range d.Attempts {
			if a.Local == local && (issue == 0 || a.Issue == issue) {
				out = append(out, cloneAttempt(a))
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

// Reserve records a new attempt before any effect. The same ID with the same
// request returns the original; the same ID with another request is refused.
// Another active attempt for the Issue, or a branch or path an uncleaned attempt
// still owns, is refused in the same transaction, so two processes cannot both
// win.
func (s *FileStore) Reserve(ctx context.Context, a Attempt) (Attempt, bool, error) {
	var out Attempt
	created := false
	err := s.withLock(ctx, func(d *disk) (bool, error) {
		if prior, ok := d.Attempts[a.ID]; ok {
			if prior.RequestHash != a.RequestHash {
				return false, ErrIdempotency
			}
			out = cloneAttempt(prior)
			return false, nil
		}
		if !a.valid() {
			return false, ErrInvalid
		}
		if len(d.Attempts) >= MaxAttempts {
			return false, ErrTooLarge
		}
		path := filepath.Clean(a.Proposal.Path)
		for _, o := range d.Attempts {
			if o.Local != a.Local {
				continue
			}
			if o.Repo == a.Repo && o.Issue == a.Issue && o.State.Active() {
				return false, ErrAttemptActive
			}
		}
		for _, o := range d.Attempts {
			if o.Local != a.Local || o.State == StateCleaned {
				continue
			}
			if o.Proposal.Branch == a.Proposal.Branch {
				return false, ErrBranchCollision
			}
			if filepath.Clean(o.Proposal.Path) == path {
				return false, ErrPathCollision
			}
		}
		d.Attempts[a.ID] = cloneAttempt(a)
		out, created = cloneAttempt(a), true
		return true, nil
	})
	if err != nil {
		return Attempt{}, false, err
	}
	return out, created, nil
}

// Update applies fn to the stored attempt. A callback that reports no change, or
// fails, leaves the file untouched. An accepted change bumps the revision and
// must keep the identity the attempt was reserved with.
func (s *FileStore) Update(ctx context.Context, id string, fn func(Attempt) (Attempt, bool, error)) (Attempt, error) {
	var out Attempt
	err := s.withLock(ctx, func(d *disk) (bool, error) {
		cur, ok := d.Attempts[id]
		if !ok {
			return false, ErrNotFound
		}
		next, changed, err := fn(cloneAttempt(cur))
		if err != nil {
			return false, err
		}
		if !changed {
			out = cloneAttempt(cur)
			return false, nil
		}
		if !sameIdentity(cur, next) {
			return false, fmt.Errorf("%w: reserved identity changed", ErrInvalid)
		}
		if cur.Revision == math.MaxUint64 {
			return false, ErrTooLarge
		}
		next.Revision = cur.Revision + 1
		if !next.valid() {
			return false, fmt.Errorf("%w: impossible record", ErrInvalid)
		}
		if next.State.Active() && !cur.State.Active() {
			// Becoming active again, as a retry does, is held to the same
			// one-active-attempt rule as a reservation, in the same transaction.
			for _, o := range d.Attempts {
				if o.ID != id && o.Local == next.Local && o.Repo == next.Repo && o.Issue == next.Issue && o.State.Active() {
					return false, ErrAttemptActive
				}
			}
		}
		d.Attempts[id] = cloneAttempt(next)
		out = cloneAttempt(next)
		return true, nil
	})
	if err != nil {
		return Attempt{}, err
	}
	return out, nil
}

// Lock takes the cross-process lock of one attempt. It is independent of the
// transaction lock, so holding it while calling out is safe.
func (s *FileStore) Lock(ctx context.Context, id string) (func(), error) {
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return nil, err
	}
	sum := sha256.Sum256([]byte(id))
	return lockFile(ctx, filepath.Join(filepath.Dir(s.Path), ".operation-"+hex.EncodeToString(sum[:])+".lock"))
}
