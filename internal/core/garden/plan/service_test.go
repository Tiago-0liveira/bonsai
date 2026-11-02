package plan

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/approve"
)

const (
	testRepo  garden.RepoID  = 7
	testIssue garden.IssueID = 42
)

type fakeGate struct {
	mu sync.Mutex
	p  approve.Preview
	n  int
}

func (g *fakeGate) Preview(context.Context, approve.Target) (approve.Preview, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.n++
	return g.p, nil
}

func (g *fakeGate) set(f func(*approve.Preview)) {
	g.mu.Lock()
	defer g.mu.Unlock()
	f(&g.p)
}

type fakeRepo struct {
	mu   sync.Mutex
	base Base
}

func (r *fakeRepo) Inspect(context.Context) (Base, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.base, nil
}

func (r *fakeRepo) set(b Base) { r.mu.Lock(); r.base = b; r.mu.Unlock() }

type fakeAgents struct {
	mu       sync.Mutex
	accounts map[agents.AccountID]Account
	started  []StartSession
	stopped  []string
	status   map[string]SessionStatus
	gone     map[string]bool
	startErr error
	listErr  error
	next     int
}

func (a *fakeAgents) Account(_ context.Context, id agents.AccountID) (Account, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	acct, ok := a.accounts[id]
	if !ok {
		return Account{}, agents.ErrAccountNotFound
	}
	return acct, nil
}

func (a *fakeAgents) Start(_ context.Context, s StartSession) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.startErr != nil {
		return "", a.startErr
	}
	a.started = append(a.started, s)
	a.next++
	return fmt.Sprintf("sess-%d", a.next), nil
}

func (a *fakeAgents) Stop(_ context.Context, id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.stopped = append(a.stopped, id)
	return nil
}

func (a *fakeAgents) Status(_ context.Context, id string) (SessionStatus, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.gone[id] {
		return SessionStatus{}, ErrSessionGone
	}
	return a.status[id], nil
}

func (a *fakeAgents) Statuses(context.Context) (map[string]SessionStatus, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := map[string]SessionStatus{}
	for id, st := range a.status {
		out[id] = st
	}
	return out, a.listErr
}

type fixture struct {
	t      *testing.T
	svc    *Service
	gate   *fakeGate
	repo   *fakeRepo
	agents *fakeAgents
	store  *Store
	clock  time.Time
	target approve.Target
}

const (
	claudeID agents.AccountID = "acct_claude"
	agyID    agents.AccountID = "acct_agy"
)

func approvedPreview() approve.Preview {
	rev := garden.Revision{Hash: "rev-hash-1234567890", CommentID: 11, Content: "Build the thing.\nAcceptance: it works.", Version: 1}
	return approve.Preview{
		Number: 12, Title: "Add the thing",
		Assessment: approve.Assessment{State: approve.StateApproved, Head: &rev, Ordinal: 2, Fingerprint: "fp-aaaa",
			Approval: &garden.Approval{RevisionHash: rev.Hash, CommentID: 13}},
	}
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	// The plan directory lives under the home directory; never the real one.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	f := &fixture{
		t: t, gate: &fakeGate{p: approvedPreview()},
		repo: &fakeRepo{base: Base{Commit: "c0ffee", Branch: "main"}},
		agents: &fakeAgents{
			accounts: map[agents.AccountID]Account{
				claudeID: {ID: claudeID, Name: "work", Provider: "claude", ReadOnly: agents.ReadOnlySupport{Supported: true, PlanFile: true, Mechanism: "tool allowlist", Allows: []string{"read files"}}},
				agyID:    {ID: agyID, Name: "agy", Provider: "antigravity", ReadOnly: agents.ReadOnlySupport{Reason: "no sandbox"}},
			},
			status: map[string]SessionStatus{}, gone: map[string]bool{},
		},
		store:  &Store{Path: filepath.Join(t.TempDir(), "planner.json")},
		clock:  time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC),
		target: approve.Target{Repo: testRepo, Issue: testIssue, Viewer: "1001", Login: "ana"},
	}
	n := 0
	svc, err := New(Deps{Gate: f.gate, Repo: f.repo, Agents: f.agents, Store: f.store, Local: "local-1", Dir: "/work/repo",
		Now: func() time.Time { return f.clock }, NewID: func() string { n++; return fmt.Sprintf("%04d", n) }})
	if err != nil {
		t.Fatal(err)
	}
	f.svc = svc
	return f
}

func (f *fixture) request() StartRequest {
	f.t.Helper()
	p, err := f.svc.Preview(context.Background(), f.target)
	if err != nil {
		f.t.Fatal(err)
	}
	return StartRequest{Target: f.target, Spec: p.Spec(), Base: p.Base, AcceptReadOnly: true, AccountID: claudeID, Model: "opus", Effort: "high", Cols: 100, Rows: 30}
}

func (f *fixture) start() Attempt {
	f.t.Helper()
	a, err := f.svc.Start(context.Background(), f.request())
	if err != nil {
		f.t.Fatalf("Start: %v", err)
	}
	return a
}

func exit(code int) *int { return &code }

func TestPreviewGatesOnVerifiedApprovedSpecOnly(t *testing.T) {
	for name, mutate := range map[string]func(*approve.Preview){
		"draft":          func(p *approve.Preview) { p.State = approve.StateDraft },
		"finishing":      func(p *approve.Preview) { p.State = approve.StateFinishing },
		"stale":          func(p *approve.Preview) { p.State = approve.StateStale },
		"revoked":        func(p *approve.Preview) { p.State = approve.StateRevoked },
		"unverified":     func(p *approve.Preview) { p.State = approve.StateUnverified },
		"attention":      func(p *approve.Preview) { p.State = approve.StateAttention },
		"no head":        func(p *approve.Preview) { p.Head = nil },
		"no approval":    func(p *approve.Preview) { p.Approval = nil },
		"no fingerprint": func(p *approve.Preview) { p.Fingerprint = "" },
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.gate.set(mutate)
			p, err := f.svc.Preview(context.Background(), f.target)
			if err != nil {
				t.Fatal(err)
			}
			if p.Eligible() || p.Unavailable == "" {
				t.Fatalf("offered a planner for %s", name)
			}
			req := StartRequest{Target: f.target, Spec: SpecRef{RevisionHash: "rev-hash-1234567890", CommentID: 11, Ordinal: 2, Fingerprint: "fp-aaaa"},
				Base: Base{Commit: "c0ffee", Branch: "main"}, AcceptReadOnly: true, AccountID: claudeID}
			if _, err := f.svc.Start(context.Background(), req); !errors.Is(err, ErrNotApproved) {
				t.Fatalf("Start = %v, want ErrNotApproved", err)
			}
			if len(f.agents.started) != 0 {
				t.Fatal("a session started for an ungated spec")
			}
		})
	}
	f := newFixture(t)
	if p, err := f.svc.Preview(context.Background(), f.target); err != nil || !p.Eligible() || p.Base.Commit != "c0ffee" {
		t.Fatalf("approved preview = %+v, %v", p, err)
	}
}

func TestStartBindsAttemptAndRunsReadOnlyWithoutWorktree(t *testing.T) {
	f := newFixture(t)
	a := f.start()
	if a.Role != RolePlanner || a.State != StateStarting || a.SessionID != "sess-1" || a.ID != "plan-0001" {
		t.Fatalf("attempt = %+v", a)
	}
	if a.Spec != (SpecRef{RevisionHash: "rev-hash-1234567890", CommentID: 11, Ordinal: 2, Fingerprint: "fp-aaaa"}) || a.Base.Commit != "c0ffee" {
		t.Fatalf("binding = %+v %+v", a.Spec, a.Base)
	}
	if a.Provider != "claude" || a.AccountID != claudeID || a.AccountName != "work" || a.Model != "opus" || a.Effort != "high" {
		t.Fatalf("account = %+v", a)
	}
	if a.Isolation.Mechanism != "tool allowlist" || len(a.Isolation.Allows) != 1 || a.Isolation.AcceptedBy != "1001" || !a.Isolation.AcceptedAt.Equal(f.clock) || a.Isolation.DirtyAccepted {
		t.Fatalf("isolation = %+v", a.Isolation)
	}
	if len(f.agents.started) != 1 {
		t.Fatalf("started %d sessions", len(f.agents.started))
	}
	s := f.agents.started[0]
	if !s.Launch.ReadOnly || s.Launch.PermissionMode != "" || s.Launch.Sandbox != "" || s.Launch.Approval != "" || s.Launch.FullAccess != nil {
		t.Fatalf("launch = %+v", s.Launch)
	}
	if s.Dir != "/work/repo" {
		t.Fatalf("planner ran in %q, want the inspected checkout (no worktree)", s.Dir)
	}
	if !strings.Contains(s.Launch.Prompt, "Build the thing.") || !strings.Contains(s.Launch.Prompt, "#12") {
		t.Fatalf("prompt lacks the specification: %q", s.Launch.Prompt)
	}
	// The record must not carry the prompt or any text of the specification.
	got, err := f.store.Get(context.Background(), a.ID)
	if err != nil || got.SessionID != "sess-1" {
		t.Fatalf("stored = %+v, %v", got, err)
	}
	raw, err := os.ReadFile(f.store.Path)
	if err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{"Build the thing", "Add the thing", "prompt"} {
		if strings.Contains(string(raw), leak) {
			t.Errorf("persisted attempt contains %q", leak)
		}
	}
}

func TestStartRefusesUnsupportedProviderWithoutRecord(t *testing.T) {
	f := newFixture(t)
	req := f.request()
	req.AccountID = agyID
	_, err := f.svc.Start(context.Background(), req)
	if !errors.Is(err, ErrReadOnlyUnsupported) || !strings.Contains(err.Error(), "no sandbox") {
		t.Fatalf("Start = %v", err)
	}
	if len(f.agents.started) != 0 {
		t.Fatal("unsupported provider was started")
	}
	if list, _ := f.store.List(context.Background(), "local-1", testIssue); len(list) != 0 {
		t.Fatalf("a refused request left attempts: %+v", list)
	}
}

func TestStartRequiresAcceptanceAndMatchingBinding(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*fixture, *StartRequest)
		want   error
	}{
		{"permissions not accepted", func(_ *fixture, r *StartRequest) { r.AcceptReadOnly = false }, ErrNotAccepted},
		{"spec fingerprint moved", func(f *fixture, _ *StartRequest) {
			f.gate.set(func(p *approve.Preview) { p.Fingerprint = "fp-bbbb" })
		}, ErrSpecChanged},
		{"spec head moved", func(f *fixture, _ *StartRequest) {
			f.gate.set(func(p *approve.Preview) { rev := *p.Head; rev.Hash = "other"; p.Head = &rev })
		}, ErrSpecChanged},
		{"commit moved", func(f *fixture, _ *StartRequest) { f.repo.set(Base{Commit: "deadbeef", Branch: "main"}) }, ErrBaseChanged},
		{"became dirty", func(f *fixture, _ *StartRequest) {
			f.repo.set(Base{Commit: "c0ffee", Branch: "main", Dirty: true, DirtyFiles: 1, DirtyDigest: "d1"})
		}, ErrBaseChanged},
		{"approval revoked", func(f *fixture, _ *StartRequest) {
			f.gate.set(func(p *approve.Preview) { p.State = approve.StateRevoked })
		}, ErrNotApproved},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			req := f.request()
			tc.mutate(f, &req)
			if _, err := f.svc.Start(context.Background(), req); !errors.Is(err, tc.want) {
				t.Fatalf("Start = %v, want %v", err, tc.want)
			}
			if len(f.agents.started) != 0 {
				t.Fatal("a session started")
			}
			if list, _ := f.store.List(context.Background(), "local-1", testIssue); len(list) != 0 {
				t.Fatalf("a refused request left attempts: %+v", list)
			}
		})
	}
}

func TestDirtyContextIsExplicitAndCaptured(t *testing.T) {
	f := newFixture(t)
	f.repo.set(Base{Commit: "c0ffee", Branch: "main", Dirty: true, DirtyFiles: 3, DirtyDigest: "d1"})
	req := f.request()
	if !req.Base.Dirty || req.Base.DirtyFiles != 3 {
		t.Fatalf("preview base = %+v", req.Base)
	}
	if _, err := f.svc.Start(context.Background(), req); !errors.Is(err, ErrDirtyContext) {
		t.Fatalf("Start without the decision = %v, want ErrDirtyContext", err)
	}
	if len(f.agents.started) != 0 {
		t.Fatal("started with undeclared dirty context")
	}
	req.IncludeDirty = true
	a, err := f.svc.Start(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !a.Base.Dirty || a.Base.DirtyFiles != 3 || a.Base.DirtyDigest != "d1" || !a.Isolation.DirtyAccepted {
		t.Fatalf("dirty capture = %+v / %+v", a.Base, a.Isolation)
	}
	// A clean checkout never records an inclusion the user was not asked about.
	g := newFixture(t)
	creq := g.request()
	creq.IncludeDirty = true
	ca, err := g.svc.Start(context.Background(), creq)
	if err != nil || ca.Isolation.DirtyAccepted {
		t.Fatalf("clean start = %+v, %v", ca, err)
	}
}

func TestOneLiveAttemptAndRetryIsANewAttempt(t *testing.T) {
	f := newFixture(t)
	first := f.start()
	if _, err := f.svc.Start(context.Background(), f.request()); !errors.Is(err, ErrAttemptActive) {
		t.Fatalf("second live start = %v, want ErrAttemptActive", err)
	}
	p, _ := f.svc.Preview(context.Background(), f.target)
	if p.Eligible() {
		t.Fatal("preview offers a planner while one is live")
	}
	if _, err := f.svc.Observe(context.Background(), first.ID, SessionStatus{State: "failed"}); err != nil {
		t.Fatal(err)
	}
	second := f.start()
	if second.ID == first.ID || second.SessionID == first.SessionID {
		t.Fatalf("retry reused the attempt: %+v", second)
	}
	list, _ := f.svc.Reconcile(context.Background(), testIssue)
	if len(list) != 2 || list[0].State != StateFailed || list[0].Reason == "" {
		t.Fatalf("history = %+v", list)
	}
}

func TestStartFailureIsKeptAsHistory(t *testing.T) {
	f := newFixture(t)
	f.agents.startErr = errors.New("holder exploded with secret=hunter2")
	if _, err := f.svc.Start(context.Background(), f.request()); err == nil {
		t.Fatal("expected failure")
	}
	list, _ := f.store.List(context.Background(), "local-1", testIssue)
	if len(list) != 1 || list[0].State != StateFailed || list[0].SessionID != "" {
		t.Fatalf("history = %+v", list)
	}
	if strings.Contains(list[0].Reason, "hunter2") {
		t.Fatalf("provider text leaked into the record: %q", list[0].Reason)
	}
	f.agents.startErr = nil
	if _, err := f.svc.Start(context.Background(), f.request()); err != nil {
		t.Fatalf("retry after a failed start: %v", err)
	}
}

func TestObserveLifecycleLateAndOutOfOrder(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	a := f.start()
	obs := func(st SessionStatus) Attempt {
		got, err := f.svc.Observe(ctx, a.ID, st)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	if got := obs(SessionStatus{State: "starting"}); got.State != StateStarting {
		t.Fatalf("starting = %v", got.State)
	}
	if got := obs(SessionStatus{State: "running"}); got.State != StateRunning {
		t.Fatalf("running = %v", got.State)
	}
	if got := obs(SessionStatus{State: "starting"}); got.State != StateRunning {
		t.Fatalf("a late starting moved the attempt back: %v", got.State)
	}
	done := obs(SessionStatus{State: "exited", ExitCode: exit(0)})
	if done.State != StateEnded || done.EndedAt == nil || done.ExitCode == nil || *done.ExitCode != 0 {
		t.Fatalf("exited = %+v", done)
	}
	// Anything after a final state is ignored: a stale poll cannot reopen or rewrite history.
	for _, st := range []SessionStatus{{State: "running"}, {State: "failed"}, {State: "exited", ExitCode: exit(2)}} {
		if got := obs(st); got.State != StateEnded || *got.ExitCode != 0 {
			t.Fatalf("late %v rewrote a final attempt: %+v", st, got)
		}
	}
	if _, err := f.svc.Observe(ctx, "plan-missing", SessionStatus{State: "running"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown attempt = %v", err)
	}
}

func TestObserveExitCodes(t *testing.T) {
	for name, tc := range map[string]struct {
		st   SessionStatus
		want State
	}{
		"clean":      {SessionStatus{State: "exited", ExitCode: exit(0)}, StateEnded},
		"error exit": {SessionStatus{State: "exited", ExitCode: exit(3)}, StateFailed},
		"no code":    {SessionStatus{State: "exited"}, StateFailed},
		"failed":     {SessionStatus{State: "failed"}, StateFailed},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			a := f.start()
			got, err := f.svc.Observe(context.Background(), a.ID, tc.st)
			if err != nil || got.State != tc.want {
				t.Fatalf("Observe = %+v, %v; want %v", got, err, tc.want)
			}
		})
	}
}

func TestCancelPreservesHistoryAndWinsOverExitCode(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	a := f.start()
	got, err := f.svc.Cancel(ctx, a.ID)
	if err != nil || !got.CancelRequested || got.State != StateStarting {
		t.Fatalf("Cancel = %+v, %v", got, err)
	}
	if len(f.agents.stopped) != 1 || f.agents.stopped[0] != "sess-1" {
		t.Fatalf("stopped = %v", f.agents.stopped)
	}
	// Whatever the session reports after a requested cancel, the attempt ends cancelled.
	end, err := f.svc.Observe(ctx, a.ID, SessionStatus{State: "exited", ExitCode: exit(137)})
	if err != nil || end.State != StateCancelled || end.SessionID != "sess-1" {
		t.Fatalf("after cancel = %+v, %v", end, err)
	}
	// Cancelling again, or a late failure, changes nothing.
	again, err := f.svc.Cancel(ctx, a.ID)
	if err != nil || again.State != StateCancelled || len(f.agents.stopped) != 1 {
		t.Fatalf("second cancel = %+v, %v (stops %v)", again, err, f.agents.stopped)
	}
	if got, _ := f.svc.Observe(ctx, a.ID, SessionStatus{State: "failed"}); got.State != StateCancelled {
		t.Fatalf("late failure rewrote a cancelled attempt: %v", got.State)
	}
}

func TestCancelBeforeSessionExistsEndsTheAttempt(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	// An attempt recorded by a process that died before its session existed.
	a := Attempt{ID: "plan-orphan", Local: "local-1", Repo: testRepo, Issue: testIssue, Number: 12, Role: RolePlanner,
		Spec: SpecRef{RevisionHash: "h", Fingerprint: "f"}, Base: Base{Commit: "c"}, Provider: "claude", AccountID: claudeID,
		State: StateStarting, RequestedAt: f.clock, UpdatedAt: f.clock}
	if err := f.store.Create(ctx, a); err != nil {
		t.Fatal(err)
	}
	got, err := f.svc.Cancel(ctx, a.ID)
	if err != nil || got.State != StateCancelled || len(f.agents.stopped) != 0 {
		t.Fatalf("Cancel = %+v, %v", got, err)
	}
}

func TestReconcileEndsAbandonedAndVanishedAttempts(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	a := f.start()
	// A running session is observed, not guessed.
	f.agents.status["sess-1"] = SessionStatus{State: "running"}
	list, err := f.svc.Reconcile(ctx, testIssue)
	if err != nil || list[0].State != StateRunning {
		t.Fatalf("reconcile = %+v, %v", list, err)
	}
	// A status that cannot be read leaves the attempt alone.
	delete(f.agents.status, "sess-1")
	f.agents.status["sess-1"] = SessionStatus{}
	list, _ = f.svc.Reconcile(ctx, testIssue)
	if list[0].State != StateRunning {
		t.Fatalf("empty status moved the attempt: %v", list[0].State)
	}
	f.agents.gone["sess-1"] = true
	list, _ = f.svc.Reconcile(ctx, testIssue)
	if list[0].State != StateFailed || list[0].Reason != "session record is gone" {
		t.Fatalf("vanished session = %+v", list[0])
	}
	// A start that never produced a session ends only after the grace period.
	orphan := Attempt{ID: "plan-orphan", Local: "local-1", Repo: testRepo, Issue: testIssue, Number: 12, Role: RolePlanner,
		Spec: SpecRef{RevisionHash: "h", Fingerprint: "f"}, Base: Base{Commit: "c"}, Provider: "claude", AccountID: claudeID,
		State: StateStarting, RequestedAt: f.clock, UpdatedAt: f.clock}
	if err := f.store.Create(ctx, orphan); err != nil {
		t.Fatal(err)
	}
	if list, _ = f.svc.Reconcile(ctx, testIssue); list[len(list)-1].State != StateStarting {
		t.Fatal("a fresh start was abandoned too early")
	}
	f.clock = f.clock.Add(startGrace + time.Second)
	if list, _ = f.svc.Reconcile(ctx, testIssue); list[len(list)-1].State != StateFailed {
		t.Fatalf("abandoned start = %+v", list[len(list)-1])
	}
	_ = a
}

// Displaying an Issue that was edited elsewhere re-reads gate one; it must never start a planner.
func TestPreviewAfterExternalEditStartsNothing(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	f.start()
	before := len(f.agents.started)
	for _, mutate := range []func(*approve.Preview){
		func(p *approve.Preview) { p.Fingerprint = "fp-edited" },
		func(p *approve.Preview) { p.State = approve.StateStale },
		func(p *approve.Preview) { p.State = approve.StateApproved },
	} {
		f.gate.set(mutate)
		if _, err := f.svc.Preview(ctx, f.target); err != nil {
			t.Fatal(err)
		}
		if _, err := f.svc.Reconcile(ctx, testIssue); err != nil {
			t.Fatal(err)
		}
	}
	if len(f.agents.started) != before {
		t.Fatalf("reading a changed Issue started %d sessions", len(f.agents.started)-before)
	}
}

func TestSpecTooLargeIsRefusedNotTruncated(t *testing.T) {
	f := newFixture(t)
	f.gate.set(func(p *approve.Preview) {
		rev := *p.Head
		rev.Content = strings.Repeat("x", MaxPromptBytes)
		p.Head = &rev
	})
	req := f.request()
	if _, err := f.svc.Start(context.Background(), req); !errors.Is(err, ErrSpecTooLarge) {
		t.Fatalf("Start = %v", err)
	}
	if len(f.agents.started) != 0 {
		t.Fatal("started with a truncated specification")
	}
}

func TestStoreRejectsCorruptAndForeignFiles(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	f.start()
	if err := writeFile(f.store.Path, `{"schema_version":1,"attempts":{"a":{"id":"b"}}}`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.List(ctx, "local-1", testIssue); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("mismatched ID = %v", err)
	}
	if err := writeFile(f.store.Path, `{"schema_version":9,"attempts":{}}`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.List(ctx, "local-1", testIssue); !errors.Is(err, ErrUnsupportedSchema) {
		t.Fatalf("newer schema = %v", err)
	}
	if err := writeFile(f.store.Path, `{"schema_version":1,"attempts":{},"attempts":{}}`); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.List(ctx, "local-1", testIssue); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("duplicate keys = %v", err)
	}
}

func writeFile(path, content string) error { return os.WriteFile(path, []byte(content), 0o600) }

func reviseRef(comment int64) *ReviseRequest {
	return &ReviseRequest{RevisionHash: "plan-rev-1", DecisionCommentID: comment}
}

func TestPlainRequestCarriesNoFeedback(t *testing.T) {
	f := newFixture(t)
	calls := 0
	f.svc.d.Feedback = func(context.Context, approve.Target, *ReviseRequest) (Feedback, error) {
		calls++
		return Feedback{Reason: "Cover the race.", PlanText: "old", Ordinal: 1}, nil
	}
	f.start()
	if calls != 0 || strings.Contains(f.agents.started[0].Launch.Prompt, "Cover the race.") {
		t.Fatalf("plain request read feedback: calls=%d", calls)
	}
}

func TestRevisePromptQuotesPredecessorAndReason(t *testing.T) {
	f := newFixture(t)
	var got *ReviseRequest
	f.svc.d.Feedback = func(_ context.Context, _ approve.Target, ref *ReviseRequest) (Feedback, error) {
		got = ref
		return Feedback{Reason: "Cover the race.\n\x1b\x00Keep the limit.\u202e", PlanText: "# Plan\r\nStep one\x07", Ordinal: 3}, nil
	}
	req := f.request()
	req.Revise = reviseRef(901)
	a, err := f.svc.Start(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	prompt := f.agents.started[0].Launch.Prompt
	for _, want := range []string{"untrusted review context", "Predecessor plan r3:\n> # Plan\n> Step one \n", "Reviewer feedback requesting changes:\n> Cover the race.\n>   Keep the limit. \n"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt lacks %q:\n%s", want, prompt)
		}
	}
	if strings.ContainsAny(prompt, "\x1b\x00\x07\r\u202e") || len(prompt) > MaxPromptBytes {
		t.Fatalf("prompt not clean/bounded (%d bytes)", len(prompt))
	}
	if got == nil || got.DecisionCommentID != 901 || a.Revises == nil || *a.Revises != (RevisionRef{RevisionHash: "plan-rev-1", Ordinal: 3, DecisionCommentID: 901}) {
		t.Fatalf("ref = %+v, revises = %+v", got, a.Revises)
	}
}

func TestRevisePromptIsBounded(t *testing.T) {
	f := newFixture(t)
	f.svc.d.Feedback = func(context.Context, approve.Target, *ReviseRequest) (Feedback, error) {
		return Feedback{Reason: "x", PlanText: strings.Repeat("plan line\n", MaxPromptBytes/8), Ordinal: 1}, nil
	}
	req := f.request()
	req.Revise = reviseRef(901)
	if _, err := f.svc.Start(context.Background(), req); !errors.Is(err, ErrSpecTooLarge) {
		t.Fatalf("oversize = %v", err)
	}
	if list, _ := f.store.List(context.Background(), "local-1", testIssue); len(list) != 0 || len(f.agents.started) != 0 {
		t.Fatal("refused revise left a record or a session")
	}
}

func TestReviseFeedbackRefusals(t *testing.T) {
	for name, tc := range map[string]struct {
		resolver func(context.Context, approve.Target, *ReviseRequest) (Feedback, error)
		ref      *ReviseRequest
	}{
		"resolver says changed": {func(context.Context, approve.Target, *ReviseRequest) (Feedback, error) {
			return Feedback{}, ErrFeedbackChanged
		}, reviseRef(901)},
		"no resolver":    {nil, reviseRef(901)},
		"empty revision": {func(context.Context, approve.Target, *ReviseRequest) (Feedback, error) { return Feedback{}, nil }, &ReviseRequest{DecisionCommentID: 901}},
		"zero comment":   {func(context.Context, approve.Target, *ReviseRequest) (Feedback, error) { return Feedback{}, nil }, &ReviseRequest{RevisionHash: "h"}},
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t)
			f.svc.d.Feedback = tc.resolver
			req := f.request()
			req.Revise = tc.ref
			if _, err := f.svc.Start(context.Background(), req); !errors.Is(err, ErrFeedbackChanged) {
				t.Fatalf("err = %v", err)
			}
			if list, _ := f.store.List(context.Background(), "local-1", testIssue); len(list) != 0 || len(f.agents.started) != 0 {
				t.Fatal("refused revise left a record or a session")
			}
		})
	}
}

func okFeedback(context.Context, approve.Target, *ReviseRequest) (Feedback, error) {
	return Feedback{Reason: "Cover the race.", PlanText: "# Plan", Ordinal: 1}, nil
}

func TestReviseIsIdempotentPerDecision(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	f.svc.d.Feedback = okFeedback
	req := f.request()
	req.Revise = reviseRef(901)
	first, err := f.svc.Start(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	// Live, then ended: every non-failed state answers a repeat with the same attempt.
	for _, step := range []func(){
		func() {},
		func() { f.agents.status[first.SessionID] = SessionStatus{State: "exited", ExitCode: exit(0)} },
	} {
		step()
		_, err := f.svc.Start(ctx, req)
		var dup *RevisionRequestedError
		if !errors.As(err, &dup) || dup.AttemptID != first.ID || !errors.Is(err, ErrRevisionRequested) {
			t.Fatalf("repeat = %v", err)
		}
	}
	if len(f.agents.started) != 1 {
		t.Fatalf("sessions started = %d", len(f.agents.started))
	}
}

func TestReviseRetryAllowedAfterFailure(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	f.svc.d.Feedback = okFeedback
	req := f.request()
	req.Revise = reviseRef(901)
	f.agents.startErr = errors.New("holder down")
	if _, err := f.svc.Start(ctx, req); err == nil {
		t.Fatal("start should fail")
	}
	f.agents.startErr = nil
	retry, err := f.svc.Start(ctx, req)
	if err != nil {
		t.Fatalf("retry after failure = %v", err)
	}
	if retry.Revises == nil || retry.Revises.DecisionCommentID != 901 {
		t.Fatalf("retry = %+v", retry)
	}
	// A different decision on the same Issue is a different request, and is blocked only by the live attempt.
	other := req
	other.Revise = reviseRef(902)
	if _, err := f.svc.Start(ctx, other); !errors.Is(err, ErrAttemptActive) {
		t.Fatalf("other decision while live = %v", err)
	}
}

func TestReviseRetryAllowedAfterCancel(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	f.svc.d.Feedback = okFeedback
	req := f.request()
	req.Revise = reviseRef(901)
	first, err := f.svc.Start(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Cancel(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	f.agents.status[first.SessionID] = SessionStatus{State: "exited", ExitCode: exit(1)}
	retry, err := f.svc.Start(ctx, req)
	if err != nil || retry.ID == first.ID {
		t.Fatalf("retry after cancel = %+v, %v", retry, err)
	}
	if old, _ := f.store.Get(ctx, first.ID); old.State != StateCancelled {
		t.Fatalf("first = %+v", old)
	}
}

func TestReviseBlockedByAnyLiveAttempt(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	f.svc.d.Feedback = okFeedback
	plain := f.start()
	req := f.request()
	req.Revise = reviseRef(901)
	if _, err := f.svc.Start(ctx, req); !errors.Is(err, ErrAttemptActive) {
		t.Fatalf("revise while plain attempt live = %v", err)
	}
	f.agents.status[plain.SessionID] = SessionStatus{State: "exited", ExitCode: exit(0)}
	if _, err := f.svc.Start(ctx, req); err != nil {
		t.Fatalf("revise after it ended = %v", err)
	}
}

func TestReviseCrashBetweenRecordAndSessionReconciles(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	f.svc.d.Feedback = okFeedback
	req := f.request()
	req.Revise = reviseRef(901)
	// The process died after the record and before the session: only a record exists.
	a := Attempt{ID: "plan-crashed", Local: "local-1", Repo: testRepo, Issue: testIssue, Number: 12, Role: RolePlanner, Spec: req.Spec, Base: req.Base,
		Provider: "claude", AccountID: claudeID, State: StateStarting, RequestedAt: f.clock, UpdatedAt: f.clock,
		Revises: &RevisionRef{RevisionHash: "plan-rev-1", Ordinal: 1, DecisionCommentID: 901}}
	if err := f.store.Create(ctx, a); err != nil {
		t.Fatal(err)
	}
	// Within the grace the record still answers the request.
	var dup *RevisionRequestedError
	if _, err := f.svc.Start(ctx, req); !errors.As(err, &dup) || dup.AttemptID != a.ID {
		t.Fatalf("within grace = %v", err)
	}
	// Past it the abandoned start is reconciled to failed, which allows the explicit retry.
	f.clock = f.clock.Add(startGrace + time.Second)
	retry, err := f.svc.Start(ctx, req)
	if err != nil || retry.ID == a.ID {
		t.Fatalf("retry = %+v, %v", retry, err)
	}
	if old, _ := f.store.Get(ctx, a.ID); old.State != StateFailed {
		t.Fatalf("crashed record = %+v", old)
	}
}

func TestSourceAttemptReturnsNewestPublisher(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	first := f.start()
	f.agents.status[first.SessionID] = SessionStatus{State: "exited", ExitCode: exit(0)}
	if _, err := f.svc.Reconcile(ctx, testIssue); err != nil {
		t.Fatal(err)
	}
	f.clock = f.clock.Add(time.Minute)
	second := f.start()
	for _, a := range []Attempt{first, second} {
		if err := f.svc.MarkPublished(ctx, a.ID, "plan-rev-1"); err != nil {
			t.Fatal(err)
		}
	}
	got, ok, err := f.svc.SourceAttempt(ctx, testIssue, "plan-rev-1")
	if err != nil || !ok || got.ID != second.ID {
		t.Fatalf("source = %+v ok=%v err=%v", got, ok, err)
	}
	for _, hash := range []string{"", "other"} {
		if _, ok, err := f.svc.SourceAttempt(ctx, testIssue, hash); ok || err != nil {
			t.Fatalf("hash %q: ok=%v err=%v", hash, ok, err)
		}
	}
	if _, ok, _ := f.svc.SourceAttempt(ctx, testIssue+1, "plan-rev-1"); ok {
		t.Fatal("another Issue matched")
	}
}

func TestAttemptJSONCompatibility(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	// An attempt recorded before the new fields existed still loads, with them empty.
	old := `{"schema_version":1,"attempts":{"plan-old":{"id":"plan-old","local_repo_id":"local-1","repo_id":7,"issue_id":42,"number":12,"role":"planner",
"spec":{"revision_hash":"h","comment_id":1,"ordinal":1,"fingerprint":"fp"},"base":{"commit":"c0ffee"},"isolation":{"mechanism":"m","allows":null,"accepted_at":"2026-05-01T10:00:00Z","accepted_by":"1001"},
"provider":"claude","account_id":"acct_claude","account_name":"work","state":"ended","requested_at":"2026-05-01T10:00:00Z","updated_at":"2026-05-01T10:00:00Z"}}}`
	if err := writeFile(f.store.Path, old); err != nil {
		t.Fatal(err)
	}
	a, err := f.store.Get(ctx, "plan-old")
	if err != nil || a.Revises != nil || a.PublishedRevision != "" {
		t.Fatalf("old record = %+v, %v", a, err)
	}
	for name, bad := range map[string]*RevisionRef{
		"no hash":    {DecisionCommentID: 5},
		"no comment": {RevisionHash: "h"},
	} {
		a.Revises = bad
		if a.valid() {
			t.Fatalf("%s: invalid ref accepted", name)
		}
	}
	a.Revises = &RevisionRef{RevisionHash: "h", DecisionCommentID: 5}
	if !a.valid() {
		t.Fatal("valid ref refused")
	}
}
