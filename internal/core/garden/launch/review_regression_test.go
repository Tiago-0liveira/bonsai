package launch

import (
	"context"
	"errors"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"testing"
)

func TestReviewRegressionStartRechecksReady(t *testing.T) {
	r := newFakeLaunchRig(t)
	p := r.preview(t)
	r.remote.snapshot.Issue.Labels = []string{garden.ManagedLabel, garden.FormatTypeLabel(garden.TypeFeature), garden.FormatStageLabel(garden.StageDone)}
	a, err := r.svc.Start(context.Background(), StartRequest{Preview: p, Confirmed: true})
	if !errors.Is(err, ErrOwnership) || len(r.holder.calls()) != 0 {
		t.Fatalf("start after Done: state=%s err=%v holder starts=%d", a.State, err, len(r.holder.calls()))
	}
}
func TestReviewRegressionRetryChecksOtherActiveAttempts(t *testing.T) {
	r := newFakeLaunchRig(t)
	a := r.start(t)
	a = fakeLaunchFinish(t, r, a, "exited", fakeLaunchInt(1))
	r.request.Branch += "-second"
	b := r.start(t)
	retried, err := r.svc.Retry(context.Background(), RetryRequest{AttemptID: a.ID, Confirmed: true})
	if !errors.Is(err, ErrAttemptActive) {
		t.Fatalf("retry state=%s err=%v other=%s holder starts=%d", retried.State, err, b.State, len(r.holder.calls()))
	}
}
func TestReviewRegressionRetrySuccessRaisesReview(t *testing.T) {
	r := newFakeLaunchRig(t)
	a := r.start(t)
	a = fakeLaunchFinish(t, r, a, "exited", fakeLaunchInt(1))
	a, err := r.svc.Retry(context.Background(), RetryRequest{AttemptID: a.ID, Confirmed: true})
	if err != nil {
		t.Fatal(err)
	}
	a = fakeLaunchFinish(t, r, a, "exited", fakeLaunchInt(0))
	if a.DesiredStage != garden.StageReview {
		t.Fatalf("state=%s desired=%s mirrored=%s", a.State, a.DesiredStage, a.MirroredStage)
	}
}
func TestReviewRegressionPublishChecksEpoch(t *testing.T) {
	r, a, p := fakeLaunchPublishRig(t)
	r.remote.snapshot.Epoch++
	got, err := r.svc.Publish(context.Background(), PublishRequest{AttemptID: a.ID, Confirmed: true})
	if !errors.Is(err, ErrOwnership) || len(p.creates) > 0 {
		t.Fatalf("epoch changed: PR=%s err=%v creates=%d", got.PR.State, err, len(p.creates))
	}
}
func TestReviewRegressionStoreRejectsCorruptInputs(t *testing.T) {
	r := newFakeLaunchRig(t)
	a := fakeLaunchRecord(t, "impl-corrupt")
	a.Inputs.SpecApprovalID = 0
	a.Inputs.Plan.BaseCommit = ""
	a.Proposal.BaseSHA = ""
	a.Proposal.Path = "relative"
	a.State = StateRunning
	a.Runs = []Run{{Number: 1, OperationID: a.ID + "/start/1", SessionID: "not-a-session", State: "exited"}}
	a.SessionID = "not-a-session"
	_, _, err := r.store.Reserve(context.Background(), a)
	if err == nil {
		t.Fatal("accepted Running record with exited unacknowledged run, malformed session/path, and absent approval/base identities")
	}
}
func TestReviewRegressionUnknownPRSurvivesDirtyGate(t *testing.T) {
	r, a, p := fakeLaunchPublishRig(t)
	p.createErr = context.DeadlineExceeded
	a, err := r.svc.Publish(context.Background(), PublishRequest{AttemptID: a.ID, Confirmed: true})
	if !errors.Is(err, ErrOutcomeUnknown) {
		t.Fatal(err)
	}
	r.worktrees.head.Dirty = true
	a, err = r.svc.Publish(context.Background(), PublishRequest{AttemptID: a.ID, Confirmed: true})
	if err != nil {
		t.Fatal(err)
	}
	r.worktrees.head.Dirty = false
	p.createErr = nil
	a, err = r.svc.Publish(context.Background(), PublishRequest{AttemptID: a.ID, Confirmed: true})
	if len(p.creates) != 1 {
		t.Fatalf("reposted after unknown create and dirty gate: state=%s err=%v create calls=%d", a.PR.State, err, len(p.creates))
	}
}

type reviewPauseReserve struct {
	Store
	ready, release chan struct{}
}

func (s *reviewPauseReserve) Reserve(ctx context.Context, a Attempt) (Attempt, bool, error) {
	a, created, err := s.Store.Reserve(ctx, a)
	if err == nil && created {
		close(s.ready)
		<-s.release
	}
	return a, created, err
}
func TestReviewRegressionReplayCannotFenceActiveDriverBeforeLock(t *testing.T) {
	r := newFakeLaunchRig(t)
	p := r.preview(t)
	paused := &reviewPauseReserve{Store: r.store, ready: make(chan struct{}), release: make(chan struct{})}
	d := r.deps
	d.Store = paused
	first, err := New(d)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan Attempt, 1)
	go func() {
		a, _ := first.Start(context.Background(), StartRequest{Preview: p, Confirmed: true})
		done <- a
	}()
	<-paused.ready
	// The follower must wait for the live driver, not settle its reservation.
	followed := make(chan Attempt, 1)
	go func() {
		a, _ := r.svc.Start(context.Background(), StartRequest{Preview: p, Confirmed: true})
		followed <- a
	}()
	close(paused.release)
	a, f := <-done, <-followed
	if a.SessionID == "" || f.SessionID != a.SessionID || len(r.holder.calls()) != 1 {
		t.Fatalf("live driver suppressed by replay: first=%s follower=%s starts=%d", a.State, f.State, len(r.holder.calls()))
	}
}

type reviewDirectorySessions struct {
	Sessions
	active bool
	err    error
}

func (d reviewDirectorySessions) ActiveAt(context.Context, string) (bool, error) {
	return d.active, d.err
}

func TestReviewRegressionCleanupBlockedByAnyHolderInDirectory(t *testing.T) {
	for _, tc := range []struct {
		name   string
		active bool
		err    error
	}{{"active foreign session", true, nil}, {"unreadable records", false, context.DeadlineExceeded}} {
		t.Run(tc.name, func(t *testing.T) {
			r, a := fakeLaunchCleanupRig(t)
			d := r.deps
			d.Sessions = reviewDirectorySessions{Sessions: r.holder, active: tc.active, err: tc.err}
			s, err := New(d)
			if err != nil {
				t.Fatal(err)
			}
			p, err := s.PreviewCleanup(context.Background(), a.ID)
			if err != nil || p.Allowed || p.Blocker == "" {
				t.Fatalf("preview=%+v %v", p, err)
			}
			if _, err = s.Cleanup(context.Background(), CleanupRequest{Preview: p, Confirmed: true}); !errors.Is(err, ErrCleanupUnsafe) {
				t.Fatalf("cleanup=%v", err)
			}
			if _, removes, _ := r.worktrees.counts(); removes != 0 {
				t.Fatal("removed a worktree with an active session")
			}
		})
	}
}
func TestFinalRegressionRunningUnacknowledgedStartingRun(t *testing.T) {
	r := newFakeLaunchRig(t)
	a := fakeLaunchRecord(t, "impl-impossible")
	a.State = StateRunning
	a.SessionID = "sess_aabb"
	a.Runs = []Run{{Number: 1, OperationID: a.ID + "/start/1", SessionID: a.SessionID, State: "starting"}}
	if _, _, err := r.store.Reserve(context.Background(), a); err == nil {
		t.Fatal("accepted Running with an unacknowledged starting Run")
	}
}
func TestFinalRegressionInvalidBaseRefAndConfigHash(t *testing.T) {
	r := newFakeLaunchRig(t)
	a := fakeLaunchRecord(t, "impl-invalid-binding")
	a.Proposal.BaseRef = ""
	a.Proposal.ConfigHash = ""
	a.Inputs.Spec.Ordinal = -1
	if _, _, err := r.store.Reserve(context.Background(), a); err == nil {
		t.Fatal("accepted missing base ref/config hash and invalid revision ordinal")
	}
}
