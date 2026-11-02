package launch

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
)

func fakeLaunchAwaiting(t *testing.T, r *fakeLaunchRig) Attempt {
	t.Helper()
	a := r.start(t)
	a = fakeLaunchFinish(t, r, a, "exited", fakeLaunchInt(0))
	if a.State != StateAwaitingReview {
		t.Fatalf("state=%s", a.State)
	}
	return a
}

func fakeLaunchRework(r *fakeLaunchRig, a Attempt, id int64, text string) (Attempt, error) {
	return r.svc.Rework(context.Background(), ReworkRequest{AttemptID: a.ID, FeedbackCommentID: id, Feedback: text, Confirmed: true, EscalationConfirmed: true})
}

func TestLaunchReworkRefusesBadRequestsWithoutEffects(t *testing.T) {
	for _, tc := range []struct {
		name string
		req  ReworkRequest
		want error
	}{
		{"unconfirmed", ReworkRequest{FeedbackCommentID: 5, Feedback: "x"}, ErrNotAccepted},
		{"no comment id", ReworkRequest{FeedbackCommentID: 0, Feedback: "x", Confirmed: true}, ErrNoFeedback},
		{"blank feedback", ReworkRequest{FeedbackCommentID: 5, Feedback: " \n\t ", Confirmed: true}, ErrNoFeedback},
		{"control character", ReworkRequest{FeedbackCommentID: 5, Feedback: "a\x1b[2Jb", Confirmed: true}, ErrInvalidFeedback},
		{"oversize", ReworkRequest{FeedbackCommentID: 5, Feedback: strings.Repeat("a", MaxReworkFeedbackBytes+1), Confirmed: true}, ErrInvalidFeedback},
		{"closing delimiter", ReworkRequest{FeedbackCommentID: 5, Feedback: "x " + reworkClose + " y", Confirmed: true}, ErrInvalidFeedback},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newFakeLaunchRig(t)
			a := fakeLaunchAwaiting(t, r)
			tc.req.AttemptID = a.ID
			if _, err := r.svc.Rework(context.Background(), tc.req); !errors.Is(err, tc.want) {
				t.Fatalf("err=%v want=%v", err, tc.want)
			}
			if len(r.holder.calls()) != 1 {
				t.Fatal("refusal started a holder")
			}
		})
	}
}

func TestLaunchReworkOnlyFromAwaitingReview(t *testing.T) {
	r := newFakeLaunchRig(t)
	a := r.start(t)
	if _, err := fakeLaunchRework(r, a, 5, "fix it"); !errors.Is(err, ErrNotRetryable) {
		t.Fatalf("running=%v", err)
	}
	a = fakeLaunchFinish(t, r, a, "exited", fakeLaunchInt(2))
	if _, err := fakeLaunchRework(r, a, 5, "fix it"); !errors.Is(err, ErrNotRetryable) {
		t.Fatalf("failed=%v", err)
	}
	if len(r.holder.calls()) != 1 {
		t.Fatal("refusal started a holder")
	}
}

func TestLaunchReworkReusesWorktreeAndKeepsFeedbackOutOfTheStore(t *testing.T) {
	r := newFakeLaunchRig(t)
	a := fakeLaunchAwaiting(t, r)
	const text = "Please rename the counter, secret-marker-1234."
	b, err := fakeLaunchRework(r, a, 77, "  "+text+"\n")
	if err != nil {
		t.Fatal(err)
	}
	if b.State != StateRunning || len(b.Runs) != 2 || b.WorktreeID != a.WorktreeID || b.Runs[0].SessionID != a.Runs[0].SessionID ||
		len(b.Reworks) != 1 || b.Reworks[0].FeedbackCommentID != 77 || b.Reworks[0].Run != 2 || b.Reworks[0].RequestedAt.IsZero() {
		t.Fatalf("rework=%+v", b)
	}
	if creates, removes, pushes := r.worktrees.counts(); creates != 1 || removes+pushes != 0 {
		t.Fatalf("worktree effects create=%d remove=%d push=%d", creates, removes, pushes)
	}
	calls := r.holder.calls()
	if len(calls) != 2 || !strings.Contains(calls[1].Launch.Prompt, "Requested changes") || !strings.Contains(calls[1].Launch.Prompt, text) ||
		!strings.Contains(calls[1].Launch.Prompt, reworkOpen) || strings.Contains(calls[0].Launch.Prompt, text) {
		t.Fatalf("prompts=%+v", calls)
	}
	raw, err := os.ReadFile(r.store.Path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "secret-marker-1234") || !strings.Contains(string(raw), `"feedback_comment_id": 77`) && !strings.Contains(string(raw), `"feedback_comment_id":77`) {
		t.Fatalf("store=%s", raw)
	}
}

func TestLaunchReworkDedupesAndAllowsNewFeedbackAfterNextReview(t *testing.T) {
	r := newFakeLaunchRig(t)
	a := fakeLaunchAwaiting(t, r)
	b, err := fakeLaunchRework(r, a, 5, "first")
	if err != nil {
		t.Fatal(err)
	}
	c, err := fakeLaunchRework(r, b, 5, "first")
	if err != nil || c.Revision != b.Revision || len(c.Runs) != 2 || len(r.holder.calls()) != 2 {
		t.Fatalf("dedupe=%+v err=%v starts=%d", c, err, len(r.holder.calls()))
	}
	// A different comment while the reworked run is live is refused, not queued.
	if _, err := fakeLaunchRework(r, b, 6, "second"); !errors.Is(err, ErrNotRetryable) {
		t.Fatalf("live rework=%v", err)
	}
	b = fakeLaunchFinish(t, r, b, "exited", fakeLaunchInt(0))
	if b.State != StateAwaitingReview {
		t.Fatalf("state=%s", b.State)
	}
	d, err := fakeLaunchRework(r, b, 6, "second")
	if err != nil || len(d.Runs) != 3 || len(d.Reworks) != 2 || d.Reworks[1].Run != 3 {
		t.Fatalf("second=%+v err=%v", d, err)
	}
	if _, err := fakeLaunchRework(r, d, 5, "first"); err != nil || len(r.holder.calls()) != 3 {
		t.Fatalf("old feedback restarted: err=%v starts=%d", err, len(r.holder.calls()))
	}
}

func TestLaunchReworkProvesOldHolderClosed(t *testing.T) {
	t.Run("alive", func(t *testing.T) {
		r := newFakeLaunchRig(t)
		a := fakeLaunchAwaiting(t, r)
		r.holder.set(fakeLaunchRunIdentity(a), SessionObservation{State: "running", Alive: true, Acknowledged: true})
		b, err := fakeLaunchRework(r, a, 5, "fix")
		if !errors.Is(err, ErrHolderActive) || len(b.Reworks) != 0 || len(r.holder.calls()) != 1 {
			t.Fatalf("alive holder: %+v err=%v", b, err)
		}
	})
	t.Run("unknown", func(t *testing.T) {
		r := newFakeLaunchRig(t)
		a := fakeLaunchAwaiting(t, r)
		r.holder.queryErr = context.DeadlineExceeded
		if _, err := fakeLaunchRework(r, a, 5, "fix"); !errors.Is(err, ErrOutcomeUnknown) {
			t.Fatalf("err=%v", err)
		}
		if len(r.holder.calls()) != 1 {
			t.Fatal("unknown holder allowed a start")
		}
	})
}

func TestLaunchReworkRechecksInputsAndWorktree(t *testing.T) {
	t.Run("approval revoked", func(t *testing.T) {
		r := newFakeLaunchRig(t)
		a := fakeLaunchAwaiting(t, r)
		r.remote.snapshot.Review.Ready = false
		r.remote.snapshot.Review.Gate.Approval = nil
		if _, err := fakeLaunchRework(r, a, 5, "fix"); !errors.Is(err, ErrApprovalMissing) {
			t.Fatalf("err=%v", err)
		}
		if len(r.holder.calls()) != 1 {
			t.Fatal("blocked rework started holder")
		}
	})
	t.Run("plan changed", func(t *testing.T) {
		r := newFakeLaunchRig(t)
		a := fakeLaunchAwaiting(t, r)
		r.remote.snapshot.Review.Approved.CommentID++
		if _, err := fakeLaunchRework(r, a, 5, "fix"); !errors.Is(err, ErrPlanChanged) {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("worktree missing", func(t *testing.T) {
		r := newFakeLaunchRig(t)
		a := fakeLaunchAwaiting(t, r)
		delete(r.worktrees.resources, a.ID)
		b, err := fakeLaunchRework(r, a, 5, "fix")
		creates, removes, _ := r.worktrees.counts()
		if !errors.Is(err, ErrWorktreeMissing) || creates != 1 || removes != 0 || len(r.holder.calls()) != 1 || len(b.Reworks) != 0 || b.State != StateAwaitingReview {
			t.Fatalf("err=%v creates=%d removes=%d attempt=%+v", err, creates, removes, b)
		}
	})
	t.Run("worktree not owned", func(t *testing.T) {
		r := newFakeLaunchRig(t)
		a := fakeLaunchAwaiting(t, r)
		c := r.worktrees.resources[a.ID]
		c.Owned = false
		r.worktrees.resources[a.ID] = c
		if _, err := fakeLaunchRework(r, a, 5, "fix"); !errors.Is(err, ErrWorktreeMissing) {
			t.Fatalf("err=%v", err)
		}
	})
}

func TestLaunchReworkCrashAfterReservationNeverStartsTwice(t *testing.T) {
	r := newFakeLaunchRig(t)
	a := fakeLaunchAwaiting(t, r)
	// The start answer and the follow-up query are both lost.
	// The old holder is queried first, so the query only starts failing once the
	// new start is in flight.
	r.holder.startErr = context.DeadlineExceeded
	r.holder.onStart = func(StartSession) {
		r.holder.mu.Lock()
		r.holder.queryErr = context.DeadlineExceeded
		r.holder.mu.Unlock()
	}
	b, err := fakeLaunchRework(r, a, 5, "fix")
	if !errors.Is(err, ErrOutcomeUnknown) || b.State != StateStartUnknown || len(b.Reworks) != 1 || len(b.Runs) != 2 {
		t.Fatalf("unknown start=%+v err=%v", b, err)
	}
	r.holder.mu.Lock()
	r.holder.startErr, r.holder.queryErr, r.holder.onStart = nil, nil, nil
	r.holder.mu.Unlock()
	c, err := fakeLaunchRework(r, b, 5, "fix")
	if err != nil || len(c.Runs) != 2 || len(r.holder.calls()) != 2 {
		t.Fatalf("repeat=%+v err=%v starts=%d", c, err, len(r.holder.calls()))
	}
	// Reconcile settles the reserved run by its identity, never starting another.
	d := fakeLaunchReconcileOne(t, r, a.ID)
	if d.State != StateRunning || len(d.Runs) != 2 || len(r.holder.calls()) != 2 {
		t.Fatalf("reconciled=%+v starts=%d", d, len(r.holder.calls()))
	}
}

func TestLaunchRetryStillRefusesAwaitingReviewAfterRework(t *testing.T) {
	r := newFakeLaunchRig(t)
	a := fakeLaunchAwaiting(t, r)
	if _, err := r.svc.Retry(context.Background(), RetryRequest{AttemptID: a.ID, Confirmed: true}); !errors.Is(err, ErrNotRetryable) {
		t.Fatalf("err=%v", err)
	}
}

func TestLaunchStoredRecordWithoutReworksLoadsAndReworksAreValidated(t *testing.T) {
	r := newFakeLaunchRig(t)
	a := r.start(t)
	raw, err := os.ReadFile(r.store.Path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "reworks") {
		t.Fatalf("empty reworks written: %s", raw)
	}
	got, err := r.store.Get(context.Background(), a.ID)
	if err != nil || len(got.Reworks) != 0 {
		t.Fatalf("old record: %+v err=%v", got, err)
	}
	for name, rw := range map[string][]Rework{
		"zero id":   {{FeedbackCommentID: 0, Run: 1, RequestedAt: fakeLaunchTime}},
		"duplicate": {{FeedbackCommentID: 1, Run: 1, RequestedAt: fakeLaunchTime}, {FeedbackCommentID: 1, Run: 1, RequestedAt: fakeLaunchTime}},
		"no run":    {{FeedbackCommentID: 1, Run: 2, RequestedAt: fakeLaunchTime}},
		"no time":   {{FeedbackCommentID: 1, Run: 1}},
	} {
		bad := cloneAttempt(got)
		bad.Reworks = rw
		if bad.valid() {
			t.Fatalf("%s accepted", name)
		}
	}
	ok := cloneAttempt(got)
	ok.Reworks = []Rework{{FeedbackCommentID: 1, Run: 1, RequestedAt: fakeLaunchTime}}
	if !ok.valid() {
		t.Fatal("valid rework refused")
	}
}

func TestLaunchReworkAcceptsCRLFAndRejectsFormatCharacters(t *testing.T) {
	r := newFakeLaunchRig(t)
	a := fakeLaunchAwaiting(t, r)
	if _, err := fakeLaunchRework(r, a, 4, "bad \u202e text"); !errors.Is(err, ErrInvalidFeedback) {
		t.Fatalf("bidi=%v", err)
	}
	if _, err := fakeLaunchRework(r, a, 5, "line one\r\nline two\rline three"); err != nil {
		t.Fatal(err)
	}
	calls := r.holder.calls()
	if len(calls) != 2 || !strings.Contains(calls[1].Launch.Prompt, "line one\nline two\nline three") || strings.Contains(calls[1].Launch.Prompt, "\r") {
		t.Fatalf("prompt=%q", calls[1].Launch.Prompt)
	}
}

func TestLaunchReworkRefeedsAfterDefinitiveStartFailureOnly(t *testing.T) {
	t.Run("definitive failure", func(t *testing.T) {
		r := newFakeLaunchRig(t)
		a := fakeLaunchAwaiting(t, r)
		r.holder.startErr = errors.New("spawn failed")
		r.holder.startResult = &SessionObservation{State: "failed", Closed: true}
		b, err := fakeLaunchRework(r, a, 5, "fix")
		if err == nil || b.State != StateRecoverable || len(b.Runs) != 2 || b.Runs[1].State != "not-started" || len(b.Reworks) != 1 {
			t.Fatalf("failed start=%+v err=%v", b, err)
		}
		r.holder.mu.Lock()
		r.holder.startErr, r.holder.startResult = nil, nil
		r.holder.mu.Unlock()
		c, err := fakeLaunchRework(r, b, 5, "fix")
		if err != nil || c.State != StateRunning || len(c.Runs) != 3 || len(c.Reworks) != 1 || c.Reworks[0].Run != 3 || len(r.holder.calls()) != 3 {
			t.Fatalf("refeed=%+v err=%v starts=%d", c, err, len(r.holder.calls()))
		}
		if !strings.Contains(r.holder.calls()[2].Launch.Prompt, "fix") {
			t.Fatal("feedback missing from refeed prompt")
		}
		if d, err := fakeLaunchRework(r, c, 5, "fix"); err != nil || len(d.Runs) != 3 || len(r.holder.calls()) != 3 {
			t.Fatalf("acknowledged run restarted: %+v err=%v", d, err)
		}
	})
	t.Run("unknown start", func(t *testing.T) {
		r := newFakeLaunchRig(t)
		a := fakeLaunchAwaiting(t, r)
		r.holder.startErr = context.DeadlineExceeded
		r.holder.onStart = func(StartSession) {
			r.holder.mu.Lock()
			r.holder.queryErr = context.DeadlineExceeded
			r.holder.mu.Unlock()
		}
		b, err := fakeLaunchRework(r, a, 5, "fix")
		if !errors.Is(err, ErrOutcomeUnknown) {
			t.Fatalf("err=%v", err)
		}
		r.holder.mu.Lock()
		r.holder.startErr, r.holder.queryErr, r.holder.onStart = nil, nil, nil
		r.holder.mu.Unlock()
		if _, err := fakeLaunchRework(r, b, 5, "fix"); err != nil || len(r.holder.calls()) != 2 {
			t.Fatalf("second start: err=%v starts=%d", err, len(r.holder.calls()))
		}
	})
}

func TestLaunchReworkConcurrentSameCommentStartsOnce(t *testing.T) {
	r := newFakeLaunchRig(t)
	a := fakeLaunchAwaiting(t, r)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = fakeLaunchRework(r, a, 9, "fix")
		}()
	}
	wg.Wait()
	if n := len(r.holder.calls()); n != 2 {
		t.Fatalf("starts=%d", n)
	}
}

func TestLaunchCheckReworkRefusesLikeReworkWithoutEffects(t *testing.T) {
	check := func(r *fakeLaunchRig, a Attempt, id int64, text string) error {
		return r.svc.CheckRework(context.Background(), ReworkRequest{AttemptID: a.ID, FeedbackCommentID: id, Feedback: text, Confirmed: true, EscalationConfirmed: true})
	}
	t.Run("startable", func(t *testing.T) {
		r := newFakeLaunchRig(t)
		a := fakeLaunchAwaiting(t, r)
		if err := check(r, a, 5, "fix"); err != nil {
			t.Fatal(err)
		}
		b, err := r.store.Get(context.Background(), a.ID)
		if err != nil || b.Revision != a.Revision || len(r.holder.calls()) != 1 {
			t.Fatalf("check had effects: %+v err=%v starts=%d", b, err, len(r.holder.calls()))
		}
	})
	t.Run("alive holder is not persisted", func(t *testing.T) {
		r := newFakeLaunchRig(t)
		a := fakeLaunchAwaiting(t, r)
		r.holder.set(fakeLaunchRunIdentity(a), SessionObservation{State: "running", Alive: true, Acknowledged: true})
		if err := check(r, a, 5, "fix"); !errors.Is(err, ErrHolderActive) {
			t.Fatalf("err=%v", err)
		}
		if b, _ := r.store.Get(context.Background(), a.ID); b.Revision != a.Revision {
			t.Fatalf("observation persisted: %d != %d", b.Revision, a.Revision)
		}
	})
	t.Run("worktree missing", func(t *testing.T) {
		r := newFakeLaunchRig(t)
		a := fakeLaunchAwaiting(t, r)
		delete(r.worktrees.resources, a.ID)
		if err := check(r, a, 5, "fix"); !errors.Is(err, ErrWorktreeMissing) {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("gates", func(t *testing.T) {
		r := newFakeLaunchRig(t)
		a := fakeLaunchAwaiting(t, r)
		r.remote.snapshot.Review.Approved.CommentID++
		if err := check(r, a, 5, "fix"); !errors.Is(err, ErrPlanChanged) {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("not awaiting, bad feedback, unconfirmed", func(t *testing.T) {
		r := newFakeLaunchRig(t)
		a := r.start(t)
		if err := check(r, a, 5, "fix"); !errors.Is(err, ErrNotRetryable) {
			t.Fatalf("running=%v", err)
		}
		if err := check(r, a, 0, "fix"); !errors.Is(err, ErrNoFeedback) {
			t.Fatalf("no id=%v", err)
		}
		if err := r.svc.CheckRework(context.Background(), ReworkRequest{AttemptID: a.ID, FeedbackCommentID: 5, Feedback: "x"}); !errors.Is(err, ErrNotAccepted) {
			t.Fatalf("unconfirmed=%v", err)
		}
	})
	t.Run("dedupe returns nil", func(t *testing.T) {
		r := newFakeLaunchRig(t)
		a := fakeLaunchAwaiting(t, r)
		b, err := fakeLaunchRework(r, a, 5, "first")
		if err != nil {
			t.Fatal(err)
		}
		if err := check(r, b, 5, "first"); err != nil {
			t.Fatalf("dedupe=%v", err)
		}
	})
}
