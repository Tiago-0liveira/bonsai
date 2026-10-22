// Package capture creates a Draft Issue exactly once. The operation record is
// persisted before the remote create, an ambiguous failure blocks retry until a
// reconcile read has looked for the Issue by its nonce, and a completed
// operation never creates again.
package capture

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/ops"
	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
)

// MaxTitleRunes is GitHub's Issue title limit.
const MaxTitleRunes = 256

// reconcileSettle is how long after an attempt an absent Issue is still treated as "maybe not indexed yet".
// The listing is not documented as read-after-write consistent, and a wrong "not created" would allow a duplicate.
const reconcileSettle = 15 * time.Second

// maxReconcilePages bounds the scan for the nonce.
const maxReconcilePages = 50

// Request is one capture. OpID comes from the preview and stays the same across retries.
type Request struct {
	OpID        string
	Local       garden.LocalRepoID
	Repo        garden.RepoID
	Viewer      string // numeric ID of the authenticated account
	Title       string
	Description string
	Type        garden.ItemType
}

// Result is the outcome of an operation.
type Result struct {
	Record ops.Record
	Issue  garden.Issue // set once the Issue is known
}

// Deps are the seams of Service.
type Deps struct {
	Creator garden.IssueCreator
	Reader  garden.RepositoryIssueReader
	Store   *ops.Store
	Now     func() time.Time
	// NewNonce returns 128 random bits as lowercase hex; nil uses crypto/rand.
	NewNonce func() string
}

// Service runs captures. It is safe for concurrent use.
type Service struct {
	d Deps

	mu       sync.Mutex
	inflight map[string]bool
}

// New builds a Service. Store, Creator and Reader are required.
func New(d Deps) (*Service, error) {
	if d.Store == nil || d.Creator == nil || d.Reader == nil {
		return nil, errors.New("capture needs a store, a creator and a reader")
	}
	if d.Now == nil {
		d.Now = time.Now
	}
	if d.NewNonce == nil {
		d.NewNonce = randomNonce
	}
	return &Service{d: d, inflight: map[string]bool{}}, nil
}

func randomNonce() string {
	var b [16]byte
	// crypto/rand.Read cannot fail: the runtime aborts when the system has no entropy.
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// Validate checks a request without touching any store or network.
func Validate(r Request) error {
	title := strings.TrimSpace(r.Title)
	switch {
	case title == "":
		return fmt.Errorf("%w: a title is required", ErrInvalid)
	case utf8.RuneCountInString(title) > MaxTitleRunes:
		return fmt.Errorf("%w: the title is longer than %d characters", ErrInvalid, MaxTitleRunes)
	case !utf8.ValidString(r.Title) || !utf8.ValidString(r.Description):
		return fmt.Errorf("%w: text must be valid UTF-8", ErrInvalid)
	case len(r.Description) > garden.MaxDescriptionBytes:
		return fmt.Errorf("%w: the description is too long", ErrInvalid)
	case strings.IndexFunc(title, unicode.IsControl) >= 0:
		return fmt.Errorf("%w: the title has control characters", ErrInvalid)
	case !validType(r.Type):
		return fmt.Errorf("%w: unknown type", ErrInvalid)
	case r.Viewer == "":
		return fmt.Errorf("%w: the GitHub account is unknown", ErrInvalid)
	case r.OpID == "" || r.Local == "" || r.Repo == 0:
		return fmt.Errorf("%w: operation identity", ErrInvalid)
	}
	return nil
}

func validType(t garden.ItemType) bool {
	switch t {
	case garden.TypeFeature, garden.TypeBug, garden.TypeChore, garden.TypeResearch:
		return true
	}
	return false
}

// Labels are the bonsai: labels a captured Issue is created with: managed, typed, at Draft.
func Labels(t garden.ItemType) []string {
	return []string{garden.ManagedLabel, garden.FormatTypeLabel(t), garden.FormatStageLabel(garden.StageDraft)}
}

// ContentHash fingerprints what the operation will send.
func ContentHash(r Request) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s\x00%d\x00%s\x00%s\x00%s\x00%s", r.Local, r.Repo, r.Type, strings.TrimSpace(r.Title), r.Description, r.Viewer)
	return hex.EncodeToString(h.Sum(nil))
}

// definitive reports an error that proves GitHub did not create the Issue. Everything else is ambiguous.
func definitive(err error) bool {
	return errors.Is(err, garden.ErrRejected) || errors.Is(err, garden.ErrAuthRequired) ||
		errors.Is(err, garden.ErrPermissionDenied) || errors.Is(err, garden.ErrRateLimited) ||
		errors.Is(err, garden.ErrForbidden)
}

// Create runs the operation. A done operation returns its Issue without writing. An operation that may
// have reached GitHub returns ErrNeedsReconcile until Reconcile settles it.
func (s *Service) Create(ctx context.Context, r Request) (Result, error) {
	if err := Validate(r); err != nil {
		return Result{}, err
	}
	if !s.claim(r.OpID) {
		return Result{}, ErrInFlight
	}
	defer s.release(r.OpID)
	lock, err := s.operationLock(ctx, r.OpID)
	if err != nil {
		return Result{}, err
	}
	defer lock.Unlock()
	hash := ContentHash(r)
	now := s.d.Now().UTC()
	nonce := s.d.NewNonce()
	// A flip from pending to unknown is persisted, then reported: the record is the evidence.
	var interrupted bool
	rec, err := s.d.Store.Update(ctx, r.OpID, func(cur ops.Record, found bool) (ops.Record, error) {
		if !found {
			return ops.Record{ID: r.OpID, Kind: ops.KindCapture, Local: r.Local, Repo: r.Repo, State: ops.StatePending, Owner: r.Viewer,
				ContentHash: hash, Nonce: nonce, Title: strings.TrimSpace(r.Title), Type: r.Type, CreatedAt: now, UpdatedAt: now}, nil
		}
		if cur.Kind != ops.KindCapture || cur.Local != r.Local || cur.Repo != r.Repo || cur.ContentHash != hash {
			return cur, ErrMismatch
		}
		switch cur.State {
		case ops.StateDone, ops.StateUnknown:
			return cur, nil
		case ops.StatePending:
			// The operation lock proves an earlier process died mid-call.
			interrupted = true
			cur.State, cur.UpdatedAt = ops.StateUnknown, now
			return cur, nil
		}
		cur.State, cur.UpdatedAt = ops.StatePending, now
		return cur, nil
	})
	if err != nil {
		return Result{}, err
	}
	switch {
	case rec.State == ops.StateDone:
		return Result{Record: rec}, nil
	case rec.State == ops.StateUnknown || interrupted:
		return Result{Record: rec}, ErrNeedsReconcile
	}
	body, err := garden.CaptureBody(r.Viewer, rec.Nonce, r.Description)
	if err != nil {
		return Result{Record: rec}, s.fail(ctx, rec, ops.StateFailed, err)
	}
	issue, err := s.d.Creator.CreateIssue(ctx, r.Repo, garden.NewIssue{Title: strings.TrimSpace(r.Title), Body: body, Labels: Labels(r.Type)})
	if err != nil {
		state := ops.StateUnknown
		if definitive(err) {
			state = ops.StateFailed
		}
		err = s.fail(ctx, rec, state, err)
		if state == ops.StateUnknown {
			err = fmt.Errorf("%w: %w", ErrNeedsReconcile, err)
		}
		out, _ := s.d.Store.Get(context.WithoutCancel(ctx), rec.ID)
		return Result{Record: out}, err
	}
	if issue.ID == 0 || issue.Repo != r.Repo {
		// The create succeeded but cannot be identified; the nonce lets reconcile find it.
		_ = s.fail(ctx, rec, ops.StateUnknown, nil)
		out, _ := s.d.Store.Get(context.WithoutCancel(ctx), rec.ID)
		return Result{Record: out}, ErrNeedsReconcile
	}
	return s.complete(ctx, rec, issue), nil
}

// complete records the Issue. The Issue exists whether or not the record is written, so a store failure
// here still reports success: the record stays pending and the next reconcile resolves it by nonce.
func (s *Service) complete(ctx context.Context, rec ops.Record, issue garden.Issue) Result {
	done, err := s.d.Store.Update(context.WithoutCancel(ctx), rec.ID, func(cur ops.Record, _ bool) (ops.Record, error) {
		cur.State, cur.IssueID, cur.Number, cur.UpdatedAt = ops.StateDone, issue.ID, issue.Number, s.d.Now().UTC()
		return cur, nil
	})
	if err != nil {
		done = rec
	}
	return Result{Record: done, Issue: issue}
}

// fail stores a non-done state, with a context that survives cancellation. It returns cause, or the
// store error when the state could not be saved.
func (s *Service) fail(ctx context.Context, rec ops.Record, state ops.State, cause error) error {
	_, err := s.d.Store.Update(context.WithoutCancel(ctx), rec.ID, func(cur ops.Record, _ bool) (ops.Record, error) {
		cur.State, cur.UpdatedAt = state, s.d.Now().UTC()
		return cur, nil
	})
	if err != nil {
		return err
	}
	return cause
}

// Reconcile settles a pending or unknown operation by looking for its nonce among all repository Issues,
// authored by the same account. Found: done. A complete read without it, after the settle window:
// failed, which allows a retry of the same operation. An incomplete read changes nothing.
func (s *Service) Reconcile(ctx context.Context, opID, viewer string) (Result, error) {
	if !s.claim(opID) {
		return Result{}, ErrInFlight
	}
	defer s.release(opID)
	lock, err := s.operationLock(ctx, opID)
	if err != nil {
		return Result{}, err
	}
	defer lock.Unlock()
	rec, err := s.d.Store.Get(ctx, opID)
	if err != nil {
		return Result{}, err
	}
	if rec.Kind != ops.KindCapture || rec.Owner != viewer {
		return Result{}, ErrMismatch
	}
	if rec.State == ops.StateDone || rec.State == ops.StateFailed {
		return Result{Record: rec}, nil
	}
	cursor := ""
	for page := 0; page < maxReconcilePages; page++ {
		p, err := s.d.Reader.ListRepositoryIssues(ctx, rec.Repo, cursor)
		if err != nil {
			return Result{Record: rec}, err
		}
		for _, it := range p.Items {
			if it.ID != 0 && it.Repo == rec.Repo && it.AuthorID == viewer && garden.CaptureNonce(it.Body) == rec.Nonce {
				return s.complete(ctx, rec, it), nil
			}
		}
		if p.Complete {
			if s.d.Now().Sub(rec.UpdatedAt) < reconcileSettle {
				return Result{Record: rec}, ErrTooSoon
			}
			out, err := s.d.Store.Update(context.WithoutCancel(ctx), rec.ID, func(cur ops.Record, _ bool) (ops.Record, error) {
				if cur.State == ops.StateDone {
					return cur, nil
				}
				cur.State, cur.UpdatedAt = ops.StateFailed, s.d.Now().UTC()
				return cur, nil
			})
			return Result{Record: out}, err
		}
		if p.NextCursor == "" || p.NextCursor == cursor {
			return Result{Record: rec}, ErrIncomplete
		}
		cursor = p.NextCursor
	}
	return Result{Record: rec}, ErrIncomplete
}

func (s *Service) operationLock(ctx context.Context, id string) (*procstore.FileLock, error) {
	lock, err := s.d.Store.TryOperationLock(ctx, id)
	if errors.Is(err, procstore.ErrLocked) {
		return nil, ErrInFlight
	}
	return lock, err
}

// Unsettled lists this clone's captures that still need a decision: pending or unknown. A pending
// record nobody runs is shown as unknown.
func (s *Service) Unsettled(ctx context.Context, local garden.LocalRepoID) ([]ops.Record, error) {
	all, err := s.d.Store.List(ctx, local)
	if err != nil {
		return nil, err
	}
	var out []ops.Record
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range all {
		if r.Kind != ops.KindCapture || (r.State != ops.StatePending && r.State != ops.StateUnknown) {
			continue
		}
		if r.State == ops.StatePending && !s.inflight[r.ID] {
			r.State = ops.StateUnknown
		}
		out = append(out, r)
	}
	return out, nil
}

func (s *Service) claim(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.inflight[id] {
		return false
	}
	s.inflight[id] = true
	return true
}

func (s *Service) release(id string) {
	s.mu.Lock()
	delete(s.inflight, id)
	s.mu.Unlock()
}
