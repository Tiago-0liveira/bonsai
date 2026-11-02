package launch

import (
	"context"
	"errors"
	"os"
	"reflect"
	"testing"

	domain "github.com/Tiago-0liveira/bonsai/internal/git"
)

func fakeLaunchCleanupRig(t *testing.T) (*fakeLaunchRig, Attempt) {
	t.Helper()
	r := newFakeLaunchRig(t)
	a := r.start(t)
	return r, fakeLaunchFinish(t, r, a, "exited", fakeLaunchInt(0))
}
func TestLaunchCleanupRequiresReadOnlyPreviewAndSeparateConfirmation(t *testing.T) {
	r, a := fakeLaunchCleanupRig(t)
	before, err := os.ReadFile(r.store.Path)
	if err != nil {
		t.Fatal(err)
	}
	p, err := r.svc.PreviewCleanup(context.Background(), a.ID)
	if err != nil || !p.Allowed || p.Revision != a.Revision || p.Check.Proposal.Path != a.Proposal.Path || p.Check.HeadSHA != a.Inputs.Plan.BaseCommit {
		t.Fatalf("preview=%+v %v", p, err)
	}
	after, _ := os.ReadFile(r.store.Path)
	if !reflect.DeepEqual(before, after) {
		t.Fatal("cleanup preview wrote state")
	}
	if _, err = r.svc.Cleanup(context.Background(), CleanupRequest{Preview: p}); !errors.Is(err, ErrNotAccepted) {
		t.Fatal(err)
	}
	_, n, _ := r.worktrees.counts()
	if n != 0 {
		t.Fatal("unconfirmed cleanup removed worktree")
	}
	got, err := r.svc.Cleanup(context.Background(), CleanupRequest{Preview: p, Confirmed: true})
	if err != nil || got.State != StateCleaned || got.ID != a.ID || len(got.Runs) != 1 || got.Proposal.Branch != a.Proposal.Branch {
		t.Fatalf("cleanup=%+v %v", got, err)
	}
	_, n, _ = r.worktrees.counts()
	if n != 1 {
		t.Fatalf("removals=%d", n)
	}
}
func TestLaunchCleanupUnsafeContentAndUnknownHolderAreBlocked(t *testing.T) {
	for _, mode := range []string{"dirty", "ignored", "untracked", "nested repository", "extra commit", "foreign ownership", "unknown holder", "active holder", "hooks unknown"} {
		t.Run(mode, func(t *testing.T) {
			r, a := fakeLaunchCleanupRig(t)
			c := r.worktrees.resources[a.ID]
			switch mode {
			case "dirty", "ignored", "untracked", "nested repository":
				c.Clean = false
			case "extra commit":
				c.HeadSHA = "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
			case "foreign ownership":
				c.Owned = false
			case "unknown holder":
				r.holder.queryErr = context.DeadlineExceeded
			case "active holder":
				r.holder.set(fakeLaunchRunIdentity(a), SessionObservation{Alive: true, Acknowledged: true, State: "running"})
			case "hooks unknown":
				c.Hooks = "unknown"
			}
			r.worktrees.resources[a.ID] = c
			p, err := r.svc.PreviewCleanup(context.Background(), a.ID)
			if err != nil && mode != "foreign ownership" {
				t.Fatal(err)
			}
			if p.Allowed || p.Blocker == "" && err == nil {
				t.Fatalf("unsafe preview=%+v err=%v", p, err)
			}
			if p.AttemptID != "" {
				_, err = r.svc.Cleanup(context.Background(), CleanupRequest{Preview: p, Confirmed: true})
				if !errors.Is(err, ErrCleanupUnsafe) && !errors.Is(err, ErrOwnership) {
					t.Fatalf("unsafe cleanup=%v", err)
				}
			}
			_, n, _ := r.worktrees.counts()
			if n != 0 {
				t.Fatal("unsafe retained content removed")
			}
		})
	}
}
func TestLaunchCleanupChangedPreviewCannotDelete(t *testing.T) {
	for _, mode := range []string{"revision", "fingerprint", "HEAD", "config", "ownership"} {
		t.Run(mode, func(t *testing.T) {
			r, a := fakeLaunchCleanupRig(t)
			p, err := r.svc.PreviewCleanup(context.Background(), a.ID)
			if err != nil {
				t.Fatal(err)
			}
			c := r.worktrees.resources[a.ID]
			switch mode {
			case "revision":
				_, err = r.store.Update(context.Background(), a.ID, func(a Attempt) (Attempt, bool, error) { a.PR.State = PRPending; return a, true, nil })
				if err != nil {
					t.Fatal(err)
				}
			case "fingerprint":
				c.Fingerprint = "changed"
			case "HEAD":
				c.HeadSHA = "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
			case "config":
				c.Proposal.ConfigHash = "changed"
			case "ownership":
				c.Owned = false
			}
			r.worktrees.resources[a.ID] = c
			got, err := r.svc.Cleanup(context.Background(), CleanupRequest{Preview: p, Confirmed: true})
			if !errors.Is(err, ErrCleanupChanged) || got.ID == "" {
				t.Fatalf("changed cleanup=%+v err=%v", got, err)
			}
			_, n, _ := r.worktrees.counts()
			if n != 0 {
				t.Fatal("stale preview removed worktree")
			}
		})
	}
}
func TestLaunchCleanupUnknownRemovalRetainsAudit(t *testing.T) {
	r, a := fakeLaunchCleanupRig(t)
	p, err := r.svc.PreviewCleanup(context.Background(), a.ID)
	if err != nil {
		t.Fatal(err)
	}
	r.worktrees.removeErr = domain.ErrUncertain
	got, err := r.svc.Cleanup(context.Background(), CleanupRequest{Preview: p, Confirmed: true})
	if !errors.Is(err, ErrOutcomeUnknown) || got.ID != a.ID || got.State == StateCleaned || got.Steps[StepCleanup].Code != "cleanup-unknown" {
		t.Fatalf("unknown cleanup=%+v %v", got, err)
	}
	stored, err := r.store.Get(context.Background(), a.ID)
	if err != nil || len(stored.Runs) != 1 || stored.SessionID == "" {
		t.Fatalf("audit lost=%+v %v", stored, err)
	}
}
func TestLaunchCleanupMissingAndForeignAttemptsRefuse(t *testing.T) {
	r, a := fakeLaunchCleanupRig(t)
	if _, err := r.svc.PreviewCleanup(context.Background(), "impl-missing"); !errors.Is(err, ErrNotFound) {
		t.Fatal(err)
	}
	d := r.deps
	d.Local = "foreign-project"
	s, err := New(d)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PreviewCleanup(context.Background(), a.ID); !errors.Is(err, ErrOwnership) {
		t.Fatal(err)
	}
}

func TestLaunchCleanupRemovalFailureNeverCompensatesOrDeletesAudit(t *testing.T) {
	r, a := fakeLaunchCleanupRig(t)
	p, err := r.svc.PreviewCleanup(context.Background(), a.ID)
	if err != nil {
		t.Fatal(err)
	}
	r.worktrees.removeErr = domain.E("hooks_failed", "delete hook refused")
	got, err := r.svc.Cleanup(context.Background(), CleanupRequest{Preview: p, Confirmed: true})
	if err == nil || got.ID == "" || got.State == StateCleaned {
		t.Fatalf("delete hook failure=%+v %v", got, err)
	}
	if _, ok := r.worktrees.resources[a.ID]; !ok {
		t.Fatal("hook failure erased retained worktree")
	}
	stored, err := r.store.Get(context.Background(), a.ID)
	if err != nil || stored.ID != a.ID || len(stored.Runs) != 1 {
		t.Fatalf("audit lost: %+v %v", stored, err)
	}
}

func TestLaunchCleanupResourceLessReservationCanCloseWithoutRemoval(t *testing.T) {
	r := newFakeLaunchRig(t)
	d := r.deps
	d.Store = &fakeLaunchFaultStore{Store: r.store, lockErr: context.Canceled}
	s, err := New(d)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := s.Start(context.Background(), StartRequest{Preview: r.preview(t), Confirmed: true})
	if a.ID == "" {
		t.Fatal("reservation not retained")
	}
	a = fakeLaunchReconcileOne(t, r, a.ID)
	p, err := r.svc.PreviewCleanup(context.Background(), a.ID)
	if err != nil || !p.Allowed {
		t.Fatalf("resource-less attempt cannot close: %+v %v", p, err)
	}
	got, err := r.svc.Cleanup(context.Background(), CleanupRequest{Preview: p, Confirmed: true})
	if err != nil || got.State != StateCleaned {
		t.Fatalf("resource-less cleanup=%+v %v", got, err)
	}
	_, removes, _ := r.worktrees.counts()
	if removes != 0 {
		t.Fatal("removed nonexistent/foreign directory")
	}
	if _, err = r.svc.Get(context.Background(), a.ID); err != nil {
		t.Fatal("resource-less audit removed", err)
	}
}
