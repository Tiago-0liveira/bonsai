package launch

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	domain "github.com/Tiago-0liveira/bonsai/internal/git"
)

func fakeLaunchRunIdentity(a Attempt) StartIdentity {
	r := a.Runs[len(a.Runs)-1]
	return StartIdentity{OperationID: r.OperationID, SessionID: r.SessionID}
}
func fakeLaunchReconcileOne(t *testing.T, r *fakeLaunchRig, id string) Attempt {
	t.Helper()
	all, err := r.svc.Reconcile(context.Background(), r.request.Target.Issue)
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range all {
		if a.ID == id {
			return a
		}
	}
	t.Fatalf("lost local attempt %s: %+v", id, all)
	return Attempt{}
}
func fakeLaunchFinish(t *testing.T, r *fakeLaunchRig, a Attempt, state string, code *int) Attempt {
	t.Helper()
	r.holder.set(fakeLaunchRunIdentity(a), SessionObservation{State: state, Acknowledged: true, Closed: true, ExitCode: code, StartedAt: &fakeLaunchTime, EndedAt: &fakeLaunchTime})
	return fakeLaunchReconcileOne(t, r, a.ID)
}
func fakeLaunchInt(n int) *int { return &n }

type fakeLaunchFaultStore struct {
	Store
	mu          sync.Mutex
	lockErr     error
	updateErr   error
	failRunning bool
}

func (f *fakeLaunchFaultStore) Lock(ctx context.Context, id string) (func(), error) {
	f.mu.Lock()
	err := f.lockErr
	f.lockErr = nil
	f.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return f.Store.Lock(ctx, id)
}
func (f *fakeLaunchFaultStore) Update(ctx context.Context, id string, fn func(Attempt) (Attempt, bool, error)) (Attempt, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failRunning {
		cur, err := f.Store.Get(ctx, id)
		if err != nil {
			return cur, err
		}
		next, _, err := fn(fakeLaunchClone(cur))
		if err != nil {
			return cur, err
		}
		if next.State == StateRunning {
			f.failRunning = false
			return cur, f.updateErr
		}
	}
	return f.Store.Update(ctx, id, fn)
}

func TestLaunchConcurrentDoubleSubmitAcrossIndependentStores(t *testing.T) {
	r := newFakeLaunchRig(t)
	p := r.preview(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	r.worktrees.afterCreate = func() { once.Do(func() { close(entered) }); <-release }
	d := r.deps
	d.Store = &FileStore{Path: r.store.Path}
	second, err := New(d)
	if err != nil {
		t.Fatal(err)
	}
	type result struct {
		a   Attempt
		err error
	}
	results := make(chan result, 2)
	go func() {
		a, err := r.svc.Start(context.Background(), StartRequest{Preview: p, Confirmed: true})
		results <- result{a, err}
	}()
	fakeLaunchReceive(t, entered)
	submitted := make(chan struct{})
	go func() {
		close(submitted)
		a, err := second.Start(context.Background(), StartRequest{Preview: p, Confirmed: true})
		results <- result{a, err}
	}()
	fakeLaunchReceive(t, submitted)
	close(release)
	x, y := fakeLaunchReceive(t, results), fakeLaunchReceive(t, results)
	if x.err != nil || y.err != nil || x.a.ID != p.ID || y.a.ID != p.ID || x.a.SessionID == "" || x.a.SessionID != y.a.SessionID || len(x.a.Runs) != 1 || len(y.a.Runs) != 1 {
		t.Fatalf("double submit: %+v / %+v", x, y)
	}
	c, _, _ := r.worktrees.counts()
	if c != 1 || len(r.holder.calls()) != 1 {
		t.Fatalf("effects: add=%d holder=%d", c, len(r.holder.calls()))
	}
}
func TestLaunchDifferentPreviewIDsCannotConsentToParallelAttempts(t *testing.T) {
	r := newFakeLaunchRig(t)
	first, second := r.preview(t), r.preview(t)
	if first.ID == second.ID {
		t.Fatal("preview identities reused")
	}
	a, err := r.svc.Start(context.Background(), StartRequest{Preview: first, Confirmed: true})
	if err != nil {
		t.Fatal(err)
	}
	b, err := r.svc.Start(context.Background(), StartRequest{Preview: second, Confirmed: true})
	if !errors.Is(err, ErrAttemptActive) || b.ID != "" {
		t.Fatalf("second=%+v err=%v first=%s", b, err, a.ID)
	}
	if len(r.holder.calls()) != 1 {
		t.Fatal("parallel holder started")
	}
}
func TestLaunchExactReplayWorksOfflineButChangedPayloadDoesNot(t *testing.T) {
	r := newFakeLaunchRig(t)
	p := r.preview(t)
	a, err := r.svc.Start(context.Background(), StartRequest{Preview: p, Confirmed: true})
	if err != nil {
		t.Fatal(err)
	}
	r.remote.verifyErr = garden.ErrOffline
	b, err := r.svc.Start(context.Background(), StartRequest{Preview: p, Confirmed: true})
	if err != nil || a.ID != b.ID || a.SessionID != b.SessionID {
		t.Fatalf("offline replay=%+v %v", b, err)
	}
	p.Request.Name = "changed"
	if _, err = r.svc.Start(context.Background(), StartRequest{Preview: p, Confirmed: true}); !errors.Is(err, ErrIdempotency) {
		t.Fatalf("changed replay=%v", err)
	}
	if len(r.holder.calls()) != 1 {
		t.Fatal("replay restarted holder")
	}
}
func TestLaunchStartTimeoutQueriesExactIdentity(t *testing.T) {
	for _, tc := range []struct {
		name        string
		observation SessionObservation
		queryErr    error
		want        State
		wantErr     error
	}{
		{"acknowledged", SessionObservation{State: "running", Alive: true, Acknowledged: true, StartedAt: &fakeLaunchTime}, nil, StateRunning, nil},
		{"closed never started", SessionObservation{State: "failed", Closed: true}, nil, StateRecoverable, context.DeadlineExceeded},
		{"unknown", SessionObservation{}, context.DeadlineExceeded, StateStartUnknown, ErrOutcomeUnknown},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newFakeLaunchRig(t)
			r.holder.startErr = context.DeadlineExceeded
			r.holder.queryErr = tc.queryErr
			r.holder.onStart = func(s StartSession) { r.holder.set(s.Identity, tc.observation) }
			a, err := r.svc.Start(context.Background(), StartRequest{Preview: r.preview(t), Confirmed: true})
			if a.ID == "" || a.State != tc.want || len(a.Runs) != 1 {
				t.Fatalf("attempt=%+v err=%v", a, err)
			}
			if tc.wantErr == nil && err != nil || tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Fatalf("err=%v want=%v", err, tc.wantErr)
			}
			if len(r.holder.calls()) != 1 {
				t.Fatal("timeout blindly restarted holder")
			}
		})
	}
}
func TestLaunchReopenAfterDurableReservationNeverStarts(t *testing.T) {
	r := newFakeLaunchRig(t)
	fault := &fakeLaunchFaultStore{Store: r.store, lockErr: context.Canceled}
	d := r.deps
	d.Store = fault
	s, err := New(d)
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.Start(context.Background(), StartRequest{Preview: r.preview(t), Confirmed: true})
	if err == nil || a.ID == "" {
		t.Fatalf("partial result=%+v err=%v", a, err)
	}
	d.Store = &FileStore{Path: r.store.Path}
	r.svc, err = New(d)
	if err != nil {
		t.Fatal(err)
	}
	a = fakeLaunchReconcileOne(t, r, a.ID)
	if a.State != StateRecoverable || len(r.holder.calls()) != 0 {
		t.Fatalf("reopen=%+v", a)
	}
	c, _, _ := r.worktrees.counts()
	if c != 0 {
		t.Fatal("reconcile created worktree")
	}
	retried, err := r.svc.Retry(context.Background(), RetryRequest{AttemptID: a.ID, Confirmed: true})
	if err != nil || retried.ID != a.ID || retried.State != StateRunning || len(retried.Runs) != 1 {
		t.Fatalf("retry=%+v err=%v", retried, err)
	}
}
func TestLaunchPartialWorktreeAndHooksRequireExplicitRetry(t *testing.T) {
	for _, hooks := range []string{"done", "failed", "unknown"} {
		t.Run(hooks, func(t *testing.T) {
			r := newFakeLaunchRig(t)
			r.worktrees.hooks = hooks
			r.worktrees.createErr = domain.ErrUncertain
			p := r.preview(t)
			a, err := r.svc.Start(context.Background(), StartRequest{Preview: p, Confirmed: true})
			if a.ID == "" || err == nil || len(r.holder.calls()) != 0 {
				t.Fatalf("partial=%+v err=%v", a, err)
			}
			if !errors.Is(err, ErrOutcomeUnknown) || !errors.Is(err, domain.ErrUncertain) {
				t.Fatalf("lost uncertain cause: %v", err)
			}
			r.worktrees.createErr = nil
			a = fakeLaunchReconcileOne(t, r, a.ID)
			before, _, _ := r.worktrees.counts()
			if len(r.holder.calls()) != 0 {
				t.Fatal("reconciliation started holder")
			}
			if hooks != "done" {
				b, err := r.svc.Retry(context.Background(), RetryRequest{AttemptID: a.ID, Confirmed: true})
				if err == nil || b.ID == "" || len(r.holder.calls()) != 0 {
					t.Fatalf("partial hooks retried silently: %+v %v", b, err)
				}
				r.worktrees.hooks = "done"
			}
			b, err := r.svc.Retry(context.Background(), RetryRequest{AttemptID: a.ID, Confirmed: true, RetryHooks: hooks != "done"})
			if err != nil || b.ID != a.ID || b.Proposal.Path != a.Proposal.Path || b.State != StateRunning {
				t.Fatalf("retry=%+v err=%v", b, err)
			}
			after, _, _ := r.worktrees.counts()
			if hooks == "done" && after != before {
				t.Fatal("completed worktree/hooks rerun")
			}
		})
	}
}
func TestLaunchFailedPostStartWriteRecoversPrewrittenIdentity(t *testing.T) {
	r := newFakeLaunchRig(t)
	d := r.deps
	d.Store = &fakeLaunchFaultStore{Store: r.store, failRunning: true, updateErr: errors.New("disk failure")}
	s, err := New(d)
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.Start(context.Background(), StartRequest{Preview: r.preview(t), Confirmed: true})
	if err == nil || a.ID == "" || len(a.Runs) != 1 || a.Runs[0].SessionID == "" {
		t.Fatalf("lost durable identity: %+v %v", a, err)
	}
	d.Store = &FileStore{Path: r.store.Path}
	r.svc, err = New(d)
	if err != nil {
		t.Fatal(err)
	}
	a = fakeLaunchReconcileOne(t, r, a.ID)
	if a.State != StateRunning || len(r.holder.calls()) != 1 {
		t.Fatalf("recovery restarted/lost holder: %+v", a)
	}
}
func TestLaunchRemoteFailureDoesNotOverrideLocalTruth(t *testing.T) {
	r := newFakeLaunchRig(t)
	r.remote.mirrorErr = garden.ErrOffline
	a := r.start(t)
	if a.State != StateRunning || a.Steps[StepMirror].Code != "remote-pending" || a.MirroredStage == garden.StageInProgress {
		t.Fatalf("offline mirror=%+v", a)
	}
	r.remote.mirrorErr = nil
	a = fakeLaunchReconcileOne(t, r, a.ID)
	if a.State != StateRunning || a.MirroredStage != garden.StageInProgress || len(r.holder.calls()) != 1 {
		t.Fatalf("mirror recovery=%+v", a)
	}
}
func TestLaunchLifecycleFollowsHolderAndEndsAtHumanReview(t *testing.T) {
	for _, tc := range []struct {
		name, state   string
		alive, closed bool
		code          *int
		want          State
	}{{"idle", "idle", true, false, nil, StateRunning}, {"completed turn", "turn-completed", true, false, nil, StateRunning}, {"zero exit", "exited", false, true, fakeLaunchInt(0), StateAwaitingReview}, {"nonzero exit", "exited", false, true, fakeLaunchInt(2), StateFailed}, {"unknown exit", "exited", false, true, nil, StateFailed}, {"lost", "failed", false, true, nil, StateFailed}} {
		t.Run(tc.name, func(t *testing.T) {
			r := newFakeLaunchRig(t)
			a := r.start(t)
			r.holder.set(fakeLaunchRunIdentity(a), SessionObservation{State: tc.state, Alive: tc.alive, Acknowledged: true, Closed: tc.closed, ExitCode: tc.code, StartedAt: &fakeLaunchTime})
			a = fakeLaunchReconcileOne(t, r, a.ID)
			if a.State != tc.want || a.SessionID == "" || len(a.Runs) != 1 || a.PR.State != PRNotCreated {
				t.Fatalf("lifecycle=%+v", a)
			}
			for _, m := range r.remote.mirrors {
				if m.Stage == garden.StageDone {
					t.Fatal("execution marked Done")
				}
			}
			if len(r.holder.calls()) != 1 {
				t.Fatal("observation restarted holder")
			}
			_, removes, pushes := r.worktrees.counts()
			if removes+pushes != 0 {
				t.Fatal("observation published/cleaned work")
			}
		})
	}
}
func TestLaunchTerminalStateAbsorbsStaleRunningAndRetryPreservesRuns(t *testing.T) {
	r := newFakeLaunchRig(t)
	a := r.start(t)
	old := fakeLaunchRunIdentity(a)
	a = fakeLaunchFinish(t, r, a, "exited", fakeLaunchInt(2))
	b, err := r.svc.Retry(context.Background(), RetryRequest{AttemptID: a.ID, Confirmed: true})
	if err != nil || len(b.Runs) != 2 || b.Runs[0].SessionID == b.Runs[1].SessionID || b.Runs[0].EndedAt == nil {
		t.Fatalf("retry=%+v err=%v", b, err)
	}
	r.holder.set(old, SessionObservation{State: "running", Alive: true, Acknowledged: true})
	b = fakeLaunchReconcileOne(t, r, b.ID)
	if b.State != StateRunning || b.SessionID != b.Runs[1].SessionID || b.Runs[0].State == "running" {
		t.Fatalf("late first run overwrote newer run: %+v", b)
	}
}
func TestLaunchStopIsOfflineAndWaitsForClosure(t *testing.T) {
	r := newFakeLaunchRig(t)
	a := r.start(t)
	r.remote.verifyErr = garden.ErrOffline
	a, err := r.svc.Stop(context.Background(), a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !a.StopRequested || a.State == StateStopped || a.Runs[0].State != "stopping" {
		t.Fatalf("invented closure: %+v", a)
	}
	a = fakeLaunchFinish(t, r, a, "exited", fakeLaunchInt(0))
	if a.State != StateStopped {
		t.Fatalf("stop + zero exit=%+v", a)
	}
	r.holder.mu.Lock()
	n := len(r.holder.stops)
	r.holder.mu.Unlock()
	if _, err = r.svc.Stop(context.Background(), a.ID); err != nil {
		t.Fatal(err)
	}
	r.holder.mu.Lock()
	defer r.holder.mu.Unlock()
	if len(r.holder.stops) != n {
		t.Fatal("closed Stop repeated effect")
	}
}
func TestLaunchRetryRefusesUnknownAndFreshGateChanges(t *testing.T) {
	for _, mode := range []string{"unknown holder", "base moved", "account defaults", "approval revoked"} {
		t.Run(mode, func(t *testing.T) {
			r := newFakeLaunchRig(t)
			a := r.start(t)
			a = fakeLaunchFinish(t, r, a, "failed", nil)
			want := ErrOutcomeUnknown
			switch mode {
			case "unknown holder":
				r.holder.queryErr = context.DeadlineExceeded
			case "base moved":
				r.worktrees.previewErr = domain.E("base_changed", "safe")
				want = ErrBaseChanged
			case "account defaults":
				r.accounts.resolution.Launch.Model = "other"
				want = ErrInputsChanged
			case "approval revoked":
				r.remote.snapshot.Review.Ready = false
				r.remote.snapshot.Review.Gate.Approval = nil
				want = ErrApprovalMissing
			}
			_, err := r.svc.Retry(context.Background(), RetryRequest{AttemptID: a.ID, Confirmed: true})
			if !errors.Is(err, want) {
				t.Fatalf("err=%v want=%v", err, want)
			}
			if len(r.holder.calls()) != 1 {
				t.Fatal("blocked retry started holder")
			}
		})
	}
}
func TestLaunchGetHasNoHiddenEffectsAndHistorySurvivesOffline(t *testing.T) {
	r := newFakeLaunchRig(t)
	a := r.start(t)
	r.remote.verifyErr = garden.ErrOffline
	r.holder.queryErr = agents.ErrAccountNotFound
	for i := 0; i < 2; i++ {
		got, err := r.svc.Get(context.Background(), a.ID)
		if err != nil || got.ID != a.ID {
			t.Fatalf("offline Get=%+v %v", got, err)
		}
	}
	if len(r.holder.calls()) != 1 {
		t.Fatal("Get started holder")
	}
	if _, err := r.svc.Get(context.Background(), "impl-missing"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
}

func TestLaunchFastAcknowledgedExitMirrorsReviewNeverDone(t *testing.T) {
	r := newFakeLaunchRig(t)
	r.holder.startResult = &SessionObservation{State: "exited", Acknowledged: true, Closed: true, ExitCode: fakeLaunchInt(0), StartedAt: &fakeLaunchTime, EndedAt: &fakeLaunchTime}
	a := r.start(t)
	if a.State != StateAwaitingReview || a.DesiredStage != garden.StageReview || a.PR.State != PRNotCreated {
		t.Fatalf("fast completion=%+v", a)
	}
	for _, m := range r.remote.mirrors {
		if m.Stage == garden.StageDone {
			t.Fatal("fast success marked Done")
		}
	}
	if len(r.holder.calls()) != 1 {
		t.Fatal("fast completion started twice")
	}
}

func TestLaunchRetryPreservesAdvancedDirtyOwnedWork(t *testing.T) {
	r := newFakeLaunchRig(t)
	a := r.start(t)
	a = fakeLaunchFinish(t, r, a, "failed", nil)
	c := r.worktrees.resources[a.ID]
	c.HeadSHA = "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
	c.Clean = false
	r.worktrees.resources[a.ID] = c
	before, _, _ := r.worktrees.counts()
	b, err := r.svc.Retry(context.Background(), RetryRequest{AttemptID: a.ID, Confirmed: true})
	if err != nil || b.State != StateRunning || b.ID != a.ID || len(b.Runs) != 2 {
		t.Fatalf("retained retry=%+v %v", b, err)
	}
	after, _, _ := r.worktrees.counts()
	if before != after || r.worktrees.resources[a.ID].HeadSHA != c.HeadSHA || r.worktrees.resources[a.ID].Clean {
		t.Fatal("retry recreated/reset retained edits")
	}
	calls := r.holder.calls()
	prompt := calls[len(calls)-1].Launch.Prompt
	if !strings.Contains(strings.ToLower(prompt), "retained") {
		t.Fatalf("retry prompt omits retained-work warning: %s", prompt)
	}
}

type fakeLaunchRefuseReserve struct {
	Store
	err error
}

func (f fakeLaunchRefuseReserve) Reserve(context.Context, Attempt) (Attempt, bool, error) {
	return Attempt{}, false, f.err
}
func TestLaunchDurableReservationFailureHasNoEffects(t *testing.T) {
	r := newFakeLaunchRig(t)
	d := r.deps
	diskErr := errors.New("disk unavailable")
	d.Store = fakeLaunchRefuseReserve{Store: r.store, err: diskErr}
	s, err := New(d)
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.Start(context.Background(), StartRequest{Preview: r.preview(t), Confirmed: true})
	if !errors.Is(err, diskErr) || a.ID != "" {
		t.Fatalf("reservation failure=%+v %v", a, err)
	}
	r.noEffects(t)
}

func TestLaunchInterruptedBranchOnlyAndForeignResourcesAreNotStartedByReconcile(t *testing.T) {
	for _, owned := range []bool{true, false} {
		t.Run(fmt.Sprint(owned), func(t *testing.T) {
			r := newFakeLaunchRig(t)
			fault := &fakeLaunchFaultStore{Store: r.store, lockErr: context.Canceled}
			d := r.deps
			d.Store = fault
			s, err := New(d)
			if err != nil {
				t.Fatal(err)
			}
			a, _ := s.Start(context.Background(), StartRequest{Preview: r.preview(t), Confirmed: true})
			if a.ID == "" {
				t.Fatal("missing reservation")
			}
			r.worktrees.resources[a.ID] = domain.IsolationCheck{Proposal: a.Proposal, Owned: owned, Exists: false, HeadSHA: a.Proposal.BaseSHA, Hooks: "pending"}
			all, err := r.svc.Reconcile(context.Background(), a.Issue)
			if owned && err != nil {
				t.Fatal(err)
			}
			if len(r.holder.calls()) != 0 {
				t.Fatal("Reconcile started branch-only attempt")
			}
			for _, record := range all {
				if record.State == StateRunning {
					t.Fatal("branch-only resource became Running")
				}
			}
		})
	}
}

func TestLaunchOwnedWorktreeSurvivesHolderValidationRefusal(t *testing.T) {
	r := newFakeLaunchRig(t)
	r.holder.startResult = &SessionObservation{State: "failed", Closed: true}
	r.holder.startErr = agents.ErrInvalidLaunch
	p := r.preview(t)
	a, err := r.svc.Start(context.Background(), StartRequest{Preview: p, Confirmed: true})
	if a.ID == "" || !errors.Is(err, agents.ErrInvalidLaunch) || a.State != StateRecoverable || a.WorktreeID == "" || len(a.Runs) != 1 {
		t.Fatalf("start validation lost partial work/cause: %+v %v", a, err)
	}
	creates, removes, pushes := r.worktrees.counts()
	if creates != 1 || removes+pushes != 0 || len(r.remote.mirrors) != 0 {
		t.Fatalf("never-started refusal compensated/mirrored: create=%d remove=%d push=%d mirror=%d", creates, removes, pushes, len(r.remote.mirrors))
	}
}
func TestLaunchSuccessfulAttemptCannotRetryOrAutoStart(t *testing.T) {
	r := newFakeLaunchRig(t)
	a := r.start(t)
	a = fakeLaunchFinish(t, r, a, "exited", fakeLaunchInt(0))
	if _, err := r.svc.Retry(context.Background(), RetryRequest{AttemptID: a.ID}); !errors.Is(err, ErrNotAccepted) {
		t.Fatalf("unconfirmed retry=%v", err)
	}
	if _, err := r.svc.Retry(context.Background(), RetryRequest{AttemptID: a.ID, Confirmed: true}); !errors.Is(err, ErrNotRetryable) {
		t.Fatalf("completed attempt restarted: %v", err)
	}
	if len(r.holder.calls()) != 1 {
		t.Fatal("human-review result launched again")
	}
}
