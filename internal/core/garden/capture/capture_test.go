package capture

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	gardenfake "github.com/Tiago-0liveira/bonsai/internal/core/garden/fake"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/ops"
)

const (
	repo   = garden.RepoID(9)
	viewer = "fake-authenticated-id"
)

type harness struct {
	t     *testing.T
	fake  *gardenfake.Fake
	store *ops.Store
	svc   *Service
	now   time.Time
	n     int
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{t: t, fake: gardenfake.New(), store: &ops.Store{Path: filepath.Join(t.TempDir(), "ops.json")}, now: time.Unix(1_000_000, 0).UTC()}
	h.svc = h.service(h.fake, h.fake)
	return h
}

func (h *harness) service(c garden.IssueCreator, r garden.RepositoryIssueReader) *Service {
	svc, err := New(Deps{Creator: c, Reader: r, Store: h.store, Now: func() time.Time { return h.now }, NewNonce: func() string {
		h.n++
		return fmt.Sprintf("%032x", h.n)
	}})
	if err != nil {
		h.t.Fatal(err)
	}
	return svc
}

func (h *harness) req(op string) Request {
	return Request{OpID: op, Local: "local", Repo: repo, Viewer: viewer, Title: "Add dark mode", Description: "Because night.", Type: garden.TypeFeature}
}

func (h *harness) creates() int {
	n := 0
	for _, l := range h.fake.WriteLog() {
		if l == "create" {
			n++
		}
	}
	return n
}

func (h *harness) issueCount() int {
	p, err := h.fake.ListManagedIssues(context.Background(), repo, "")
	if err != nil {
		h.t.Fatal(err)
	}
	return len(p.Items)
}

func TestCreateMakesOneDraftIssueAndReplayNeverCreatesAgain(t *testing.T) {
	h := newHarness(t)
	res, err := h.svc.Create(context.Background(), h.req("op1"))
	if err != nil {
		t.Fatal(err)
	}
	it := res.Issue
	if it.ID == 0 || res.Record.State != ops.StateDone || res.Record.IssueID != it.ID {
		t.Fatalf("result = %+v", res)
	}
	c := garden.Classify(it.Labels)
	if len(c.Problems) != 0 || c.Stage != garden.StageDraft || c.Type != garden.TypeFeature {
		t.Fatalf("labels = %v (%+v)", it.Labels, c)
	}
	meta, err := garden.DecodeMetadata(it.Metadata)
	if err != nil || meta.Accepted || meta.Provenance != garden.ProvenanceCapture || meta.Owner != viewer {
		t.Fatalf("metadata = %+v, %v", meta, err)
	}
	if garden.CaptureNonce(it.Body) != res.Record.Nonce || garden.DisplayBody(it.Body) != "Because night." {
		t.Fatalf("body = %q", it.Body)
	}
	again, err := h.svc.Create(context.Background(), h.req("op1"))
	if err != nil || again.Record.IssueID != it.ID || h.creates() != 1 {
		t.Fatalf("replay = %+v, %v, creates %d", again, err, h.creates())
	}
}

func TestAmbiguousFailureBlocksRetryUntilReconcileFindsTheIssue(t *testing.T) {
	h := newHarness(t)
	h.fake.FailNextCreateAfterApply(garden.ErrOffline) // the server created it, the answer was lost
	res, err := h.svc.Create(context.Background(), h.req("op1"))
	if !errors.Is(err, ErrNeedsReconcile) || res.Record.State != ops.StateUnknown {
		t.Fatalf("create = %+v, %v", res, err)
	}
	if _, err = h.svc.Create(context.Background(), h.req("op1")); !errors.Is(err, ErrNeedsReconcile) || h.creates() != 1 {
		t.Fatalf("retry before reconcile: %v, creates %d", err, h.creates())
	}
	un, _ := h.svc.Unsettled(context.Background(), "local")
	if len(un) != 1 || un[0].State != ops.StateUnknown {
		t.Fatalf("unsettled = %+v", un)
	}
	got, err := h.svc.Reconcile(context.Background(), "op1", viewer)
	if err != nil || got.Record.State != ops.StateDone || got.Issue.ID == 0 || h.issueCount() != 1 {
		t.Fatalf("reconcile = %+v, %v, issues %d", got, err, h.issueCount())
	}
	final, err := h.svc.Create(context.Background(), h.req("op1"))
	if err != nil || final.Record.IssueID != got.Issue.ID || h.creates() != 1 {
		t.Fatalf("after reconcile: %+v, %v, creates %d", final, err, h.creates())
	}
	if un, _ = h.svc.Unsettled(context.Background(), "local"); len(un) != 0 {
		t.Fatalf("still unsettled: %+v", un)
	}
}

func TestReconcileNotFoundWaitsThenAllowsRetryWithSameNonce(t *testing.T) {
	h := newHarness(t)
	h.fake.FailNext(garden.ErrOffline) // the request never reached the server
	res, err := h.svc.Create(context.Background(), h.req("op1"))
	if !errors.Is(err, ErrNeedsReconcile) {
		t.Fatal(err)
	}
	nonce := res.Record.Nonce
	if _, err = h.svc.Reconcile(context.Background(), "op1", viewer); !errors.Is(err, ErrTooSoon) {
		t.Fatalf("immediate reconcile: %v", err)
	}
	h.now = h.now.Add(reconcileSettle + time.Second)
	got, err := h.svc.Reconcile(context.Background(), "op1", viewer)
	if err != nil || got.Record.State != ops.StateFailed {
		t.Fatalf("reconcile = %+v, %v", got, err)
	}
	retry, err := h.svc.Create(context.Background(), h.req("op1"))
	if err != nil || retry.Record.Nonce != nonce || h.creates() != 1 || h.issueCount() != 1 {
		t.Fatalf("retry = %+v, %v, creates %d", retry, err, h.creates())
	}
}

func TestDefinitiveFailureAllowsImmediateRetry(t *testing.T) {
	for _, cause := range []error{garden.ErrRejected, garden.ErrAuthRequired, garden.ErrPermissionDenied, garden.ErrRateLimited, garden.ErrForbidden} {
		h := newHarness(t)
		h.fake.FailNext(cause)
		res, err := h.svc.Create(context.Background(), h.req("op1"))
		if !errors.Is(err, cause) || errors.Is(err, ErrNeedsReconcile) || res.Record.State != ops.StateFailed {
			t.Fatalf("%v: create = %+v, %v", cause, res, err)
		}
		if _, err = h.svc.Create(context.Background(), h.req("op1")); err != nil || h.issueCount() != 1 {
			t.Fatalf("%v: retry: %v, issues %d", cause, err, h.issueCount())
		}
	}
}

func TestUnknownErrorsAreAmbiguous(t *testing.T) {
	for _, cause := range []error{context.DeadlineExceeded, garden.ErrOffline, errors.New("mystery")} {
		h := newHarness(t)
		h.fake.FailNext(cause)
		res, err := h.svc.Create(context.Background(), h.req("op1"))
		if !errors.Is(err, ErrNeedsReconcile) || res.Record.State != ops.StateUnknown {
			t.Fatalf("%v: create = %+v, %v", cause, res, err)
		}
	}
}

func TestReconcileIgnoresOtherAuthorsAndNonces(t *testing.T) {
	h := newHarness(t)
	h.fake.FailNext(garden.ErrOffline)
	res, _ := h.svc.Create(context.Background(), h.req("op1"))
	body, _ := garden.CaptureBody("someone-else", res.Record.Nonce, "")
	h.fake.SeedIssue(garden.Issue{Repo: repo, ID: 50, Number: 5, AuthorID: "someone-else", Body: body, Labels: []string{garden.ManagedLabel}})
	other, _ := garden.CaptureBody(viewer, strings.Repeat("a", 32), "")
	h.fake.SeedIssue(garden.Issue{Repo: repo, ID: 51, Number: 6, AuthorID: viewer, Body: other, Labels: []string{garden.ManagedLabel}})
	h.fake.SeedIssue(garden.Issue{Repo: repo, ID: 52, Number: 7, AuthorID: viewer, Title: res.Record.Nonce, Body: "no marker", Labels: []string{garden.ManagedLabel}})
	h.now = h.now.Add(time.Minute)
	got, err := h.svc.Reconcile(context.Background(), "op1", viewer)
	if err != nil || got.Record.State != ops.StateFailed {
		t.Fatalf("matched a foreign issue: %+v, %v", got, err)
	}
}

type pagedReader struct {
	garden.RepositoryIssueReader
	failAt     int
	calls      int
	incomplete bool
}

func (p *pagedReader) ListRepositoryIssues(ctx context.Context, r garden.RepoID, cursor string) (garden.IssuePage, error) {
	p.calls++
	if p.calls == p.failAt {
		return garden.IssuePage{}, garden.ErrOffline
	}
	if p.incomplete {
		return garden.IssuePage{Complete: false}, nil
	}
	return p.RepositoryIssueReader.ListRepositoryIssues(ctx, r, cursor)
}

func TestIncompleteReconcileReadChangesNothing(t *testing.T) {
	h := newHarness(t)
	h.fake.SetPageSize(1)
	for i := 1; i <= 3; i++ {
		h.fake.SeedIssue(garden.Issue{Repo: repo, ID: garden.IssueID(i), Number: int64(i), Labels: []string{garden.ManagedLabel}})
	}
	h.fake.FailNext(garden.ErrOffline)
	if _, err := h.svc.Create(context.Background(), h.req("op1")); !errors.Is(err, ErrNeedsReconcile) {
		t.Fatal(err)
	}
	h.now = h.now.Add(time.Minute)
	svc := h.service(h.fake, &pagedReader{RepositoryIssueReader: h.fake, failAt: 2})
	if _, err := svc.Reconcile(context.Background(), "op1", viewer); !errors.Is(err, garden.ErrOffline) {
		t.Fatalf("err = %v", err)
	}
	rec, _ := h.store.Get(context.Background(), "op1")
	if rec.State != ops.StateUnknown {
		t.Fatalf("state = %s after an incomplete read", rec.State)
	}
	if _, err := h.svc.Create(context.Background(), h.req("op1")); !errors.Is(err, ErrNeedsReconcile) {
		t.Fatalf("retry allowed after incomplete read: %v", err)
	}
}

func TestReconcileScanIsBounded(t *testing.T) {
	h := newHarness(t)
	h.fake.SetPageSize(1)
	for i := 1; i <= maxReconcilePages+2; i++ {
		h.fake.SeedIssue(garden.Issue{Repo: repo, ID: garden.IssueID(i), Number: int64(i), Labels: []string{garden.ManagedLabel}})
	}
	h.fake.FailNext(garden.ErrOffline)
	_, _ = h.svc.Create(context.Background(), h.req("op1"))
	h.now = h.now.Add(time.Minute)
	if _, err := h.svc.Reconcile(context.Background(), "op1", viewer); !errors.Is(err, ErrIncomplete) {
		t.Fatalf("err = %v", err)
	}
}

func TestOperationIdentityIsBoundToItsContent(t *testing.T) {
	h := newHarness(t)
	if _, err := h.svc.Create(context.Background(), h.req("op1")); err != nil {
		t.Fatal(err)
	}
	changed := h.req("op1")
	changed.Title = "Something else"
	if _, err := h.svc.Create(context.Background(), changed); !errors.Is(err, ErrMismatch) {
		t.Fatalf("title swap: %v", err)
	}
	other := h.req("op1")
	other.Local = "another-clone"
	if _, err := h.svc.Create(context.Background(), other); !errors.Is(err, ErrMismatch) {
		t.Fatalf("clone swap: %v", err)
	}
	if _, err := h.svc.Reconcile(context.Background(), "op1", "someone-else"); !errors.Is(err, ErrMismatch) {
		t.Fatalf("viewer swap: %v", err)
	}
	if h.creates() != 1 {
		t.Fatalf("creates = %d", h.creates())
	}
}

func TestPendingRecordFromACrashedProcessNeedsReconcile(t *testing.T) {
	h := newHarness(t)
	r := h.req("op1")
	if _, err := h.store.Update(context.Background(), "op1", func(ops.Record, bool) (ops.Record, error) {
		return ops.Record{ID: "op1", Kind: ops.KindCapture, Local: r.Local, Repo: r.Repo, State: ops.StatePending, Owner: viewer, ContentHash: ContentHash(r), Nonce: strings.Repeat("b", 32), Title: r.Title, Type: r.Type, CreatedAt: h.now, UpdatedAt: h.now}, nil
	}); err != nil {
		t.Fatal(err)
	}
	un, err := h.svc.Unsettled(context.Background(), "local")
	if err != nil || len(un) != 1 || un[0].State != ops.StateUnknown {
		t.Fatalf("unsettled = %+v, %v", un, err)
	}
	if _, err = h.svc.Create(context.Background(), r); !errors.Is(err, ErrNeedsReconcile) || h.creates() != 0 {
		t.Fatalf("create after crash: %v, creates %d", err, h.creates())
	}
}

type blockingCreator struct {
	garden.IssueCreator
	started chan struct{}
	release chan struct{}
	calls   atomic.Int32
}

func (b *blockingCreator) CreateIssue(ctx context.Context, r garden.RepoID, n garden.NewIssue) (garden.Issue, error) {
	b.calls.Add(1)
	b.started <- struct{}{}
	<-b.release
	return b.IssueCreator.CreateIssue(ctx, r, n)
}

func TestConcurrentCreatesOfOneOperationCreateOnce(t *testing.T) {
	h := newHarness(t)
	bc := &blockingCreator{IssueCreator: h.fake, started: make(chan struct{}, 8), release: make(chan struct{})}
	svc := h.service(bc, h.fake)
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := svc.Create(context.Background(), h.req("op1"))
			errs <- err
		}()
	}
	<-bc.started
	// Everyone else lost the claim while the first create is still in flight.
	for i := 0; i < 7; i++ {
		if err := <-errs; !errors.Is(err, ErrInFlight) {
			t.Fatalf("err = %v", err)
		}
	}
	un, _ := svc.Unsettled(context.Background(), "local")
	if len(un) != 1 || un[0].State != ops.StatePending {
		t.Fatalf("in-flight op = %+v", un)
	}
	close(bc.release)
	wg.Wait()
	if err := <-errs; err != nil || bc.calls.Load() != 1 || h.issueCount() != 1 {
		t.Fatalf("err %v, calls %d, issues %d", err, bc.calls.Load(), h.issueCount())
	}
}

func TestValidate(t *testing.T) {
	h := newHarness(t)
	ok := h.req("op")
	bad := map[string]func(*Request){
		"empty title":    func(r *Request) { r.Title = "   " },
		"long title":     func(r *Request) { r.Title = strings.Repeat("é", MaxTitleRunes+1) },
		"control title":  func(r *Request) { r.Title = "a\x1b[31mb" },
		"huge body":      func(r *Request) { r.Description = strings.Repeat("a", garden.MaxDescriptionBytes+1) },
		"bad utf8":       func(r *Request) { r.Description = "\xff" },
		"unknown type":   func(r *Request) { r.Type = "epic" },
		"no viewer":      func(r *Request) { r.Viewer = "" },
		"no op":          func(r *Request) { r.OpID = "" },
		"no repo":        func(r *Request) { r.Repo = 0 },
		"no local clone": func(r *Request) { r.Local = "" },
	}
	if err := Validate(ok); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range bad {
		r := ok
		mutate(&r)
		if err := Validate(r); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
		if _, err := h.svc.Create(context.Background(), r); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: Create: %v", name, err)
		}
	}
	if h.creates() != 0 {
		t.Fatal("invalid request reached GitHub")
	}
	if got := Labels(garden.TypeBug); len(got) != 3 || got[0] != garden.ManagedLabel || got[1] != "bonsai:type/bug" || got[2] != "bonsai:stage/draft" {
		t.Fatalf("labels = %v", got)
	}
}

func TestCorruptStoreStopsBeforeAnyCreate(t *testing.T) {
	h := newHarness(t)
	if err := writeFile(h.store.Path, "{broken"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.Create(context.Background(), h.req("op1")); !errors.Is(err, ops.ErrCorrupt) || h.creates() != 0 {
		t.Fatalf("err = %v, creates %d", err, h.creates())
	}
}

func TestReconcileFindsCreatedIssueAfterManagedLabelRemoved(t *testing.T) {
	h := newHarness(t)
	h.fake.FailNextCreateAfterApply(garden.ErrOffline)
	_, _ = h.svc.Create(t.Context(), h.req("op1"))
	page, err := h.fake.ListRepositoryIssues(t.Context(), repo, "")
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("issues = %+v, %v", page, err)
	}
	it := page.Items[0]
	it.Labels = nil
	h.fake.PutIssue(it)
	h.now = h.now.Add(time.Minute)
	res, err := h.svc.Reconcile(t.Context(), "op1", viewer)
	if err != nil || res.Record.State != ops.StateDone || res.Issue.ID != it.ID {
		t.Fatalf("reconcile = %+v, %v", res, err)
	}
	if _, err := h.svc.Create(t.Context(), h.req("op1")); err != nil || h.creates() != 1 {
		t.Fatalf("replay = %v, creates %d", err, h.creates())
	}
}

func TestReconcileIncompletePageWithoutCursorNeverAllowsRetry(t *testing.T) {
	h := newHarness(t)
	h.fake.FailNext(garden.ErrOffline)
	_, _ = h.svc.Create(t.Context(), h.req("op1"))
	h.now = h.now.Add(time.Minute)
	svc := h.service(h.fake, &pagedReader{RepositoryIssueReader: h.fake, incomplete: true})
	res, err := svc.Reconcile(t.Context(), "op1", viewer)
	if !errors.Is(err, ErrIncomplete) || res.Record.State != ops.StateUnknown {
		t.Fatalf("reconcile = %+v, %v", res, err)
	}
	if _, err := h.svc.Create(t.Context(), h.req("op1")); !errors.Is(err, ErrNeedsReconcile) {
		t.Fatalf("retry after incomplete page = %v", err)
	}
}

type blockingReader struct {
	garden.RepositoryIssueReader
	started chan struct{}
	release chan struct{}
}

func (b blockingReader) ListRepositoryIssues(ctx context.Context, r garden.RepoID, cursor string) (garden.IssuePage, error) {
	close(b.started)
	<-b.release
	return b.RepositoryIssueReader.ListRepositoryIssues(ctx, r, cursor)
}

func TestOperationLockAcrossProcesses(t *testing.T) {
	if path := os.Getenv("BONSAI_CAPTURE_LOCK_TEST_STORE"); path != "" {
		f := gardenfake.New()
		svc, err := New(Deps{Creator: f, Reader: f, Store: &ops.Store{Path: path}})
		if err != nil {
			t.Fatal(err)
		}
		// The parent's operation is deliberately older than the settle window.
		// Neither entry point may reinterpret its pending record as abandoned.
		r := Request{OpID: "op1", Local: "local", Repo: repo, Viewer: viewer, Title: "Add dark mode", Description: "Because night.", Type: garden.TypeFeature}
		if _, err := svc.Create(t.Context(), r); !errors.Is(err, ErrInFlight) {
			t.Fatalf("create during another process's operation = %v", err)
		}
		if _, err := svc.Reconcile(t.Context(), "op1", viewer); !errors.Is(err, ErrInFlight) {
			t.Fatalf("reconcile during another process's operation = %v", err)
		}
		return
	}
	for _, action := range []string{"create", "reconcile"} {
		t.Run(action, func(t *testing.T) {
			h := newHarness(t)
			started, release := make(chan struct{}, 1), make(chan struct{})
			var run func() error
			if action == "create" {
				svc := h.service(&blockingCreator{IssueCreator: h.fake, started: started, release: release}, h.fake)
				run = func() error { _, err := svc.Create(t.Context(), h.req("op1")); return err }
			} else {
				h.fake.FailNext(garden.ErrOffline)
				_, _ = h.svc.Create(t.Context(), h.req("op1"))
				h.now = h.now.Add(time.Minute)
				svc := h.service(h.fake, blockingReader{h.fake, started, release})
				run = func() error { _, err := svc.Reconcile(t.Context(), "op1", viewer); return err }
			}
			finished := make(chan error, 1)
			go func() { finished <- run() }()
			<-started
			bin, err := os.Executable()
			if err != nil {
				close(release)
				<-finished
				t.Fatal(err)
			}
			cmd := exec.CommandContext(t.Context(), bin, "-test.run=^TestOperationLockAcrossProcesses$")
			cmd.Env = append(os.Environ(), "BONSAI_CAPTURE_LOCK_TEST_STORE="+h.store.Path)
			out, childErr := cmd.CombinedOutput()
			close(release)
			if err := <-finished; err != nil {
				t.Fatal(err)
			}
			if childErr != nil {
				t.Fatalf("second process: %v\n%s", childErr, out)
			}
			if action == "create" {
				if _, err := h.svc.Create(t.Context(), h.req("op1")); err != nil || h.creates() != 1 {
					t.Fatalf("completed replay = %v, creates %d", err, h.creates())
				}
			} else if _, err := h.svc.Create(t.Context(), h.req("op1")); err != nil {
				t.Fatalf("retry after confirmed absence = %v", err)
			}
		})
	}
}
