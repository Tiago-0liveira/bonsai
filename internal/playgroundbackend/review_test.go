package playgroundbackend

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/launch"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/review"
	coregit "github.com/Tiago-0liveira/bonsai/internal/core/git"
	"github.com/Tiago-0liveira/bonsai/internal/git/github"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/playgroundapi"
)

var bg = context.Background()

func manual(session, head, id string) playgroundapi.StartReviewRequest {
	return playgroundapi.StartReviewRequest{SessionID: session, ExpectHead: head, Confirmed: true, RequestID: id}
}

func TestReviewPolicyView(t *testing.T) {
	rr := newReviewRig(t)
	v, err := rr.b.ReviewPolicy(bg)
	if err != nil || !v.Enabled || v.AccountName != "Reviewer acct" || v.Role != playgroundapi.ReviewRole || !v.Capability.Supported || v.Trigger != review.TriggerAwaitingReview {
		t.Fatalf("policy = %+v err=%v", v, err)
	}
	rr.accts.list = []agentsapi.Account{account("acct", agents.ReadOnlySupport{Reason: "no sandbox in this provider"})}
	v, _ = rr.b.ReviewPolicy(bg)
	if v.Capability.Supported || v.Capability.ReadOnly || !strings.Contains(v.Capability.Reason, "no sandbox") {
		t.Fatalf("unsupported capability = %+v", v.Capability)
	}
	rr.accts.list = []agentsapi.Account{account("acct", agents.ReadOnlySupport{Supported: true})}
	v, _ = rr.b.ReviewPolicy(bg)
	if v.Capability.Supported || !v.Capability.ReadOnly || v.Capability.PlanFile || v.Capability.Reason == "" {
		t.Fatalf("no plan-file capability = %+v", v.Capability)
	}
	rr.accts.err = errors.New("boom")
	v, _ = rr.b.ReviewPolicy(bg)
	if v.AccountErr == nil || !v.Enabled {
		t.Fatalf("account failure keeps the policy: %+v", v)
	}
	rr.accts.err = nil
	rr.prefs = nil
	v, _ = rr.b.ReviewPolicy(bg)
	if v.Enabled || v.Trigger != review.TriggerManual {
		t.Fatalf("nil prefs must be off: %+v", v)
	}
	rr.host.supported = false
	if v, _ = rr.b.ReviewPolicy(bg); v.Capability.HostingSupported {
		t.Fatal("hosting reported while unsupported")
	}
}

func TestPolicyOnlyFromLocalPrefs(t *testing.T) {
	r := newRig(t, "main") // sets HOME and XDG_CONFIG_HOME to temp dirs
	r.b.deps.Prefs = loadReviewPrefs
	if p, err := loadReviewPrefs(); err != nil || p != nil {
		t.Fatalf("empty state = %+v, %v", p, err)
	}
	pol, _, perr := r.b.policy()
	if perr != nil || pol.Enabled {
		t.Fatalf("default policy must be off: %+v %v", pol, perr)
	}
	st, err := config.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	prefs := st.PrefsSnapshot()
	prefs.GardenReview = &config.GardenReviewPrefs{Enabled: true, AccountID: "a", Trigger: config.GardenReviewTriggerNewCommit}
	if err := st.SetPrefs(prefs); err != nil {
		t.Fatal(err)
	}
	if pol, _, _ = r.b.policy(); !pol.Enabled || pol.AccountID != "a" || pol.Trigger != review.TriggerNewCommit {
		t.Fatalf("saved policy = %+v", pol)
	}
	// Disabling takes effect on the next read: nothing is cached.
	prefs.GardenReview.Enabled = false
	if err := st.SetPrefs(prefs); err != nil {
		t.Fatal(err)
	}
	if pol, _, _ = r.b.policy(); pol.Enabled {
		t.Fatal("a disabled consent must take effect at once")
	}
}

func TestStartReviewManual(t *testing.T) {
	t.Run("refusals before anything starts", func(t *testing.T) {
		rr := newReviewRig(t)
		wt, s := rr.attempt("att-1", 7, launch.StateAwaitingReview)
		head := rr.head(wt)
		_, err := rr.b.StartReview(bg, playgroundapi.StartReviewRequest{SessionID: s, ExpectHead: head, RequestID: "r1"})
		wantCode(t, err, playgroundapi.CodeNotConfirmed)
		_, err = rr.b.StartReview(bg, manual(s, head, ""))
		wantCode(t, err, playgroundapi.CodeInvalidInput)
		_, err = rr.b.StartReview(bg, manual(s, strings.Repeat("0", 40), "r1"))
		wantCode(t, err, playgroundapi.CodeStale)
		rr.prefs = &config.GardenReviewPrefs{}
		_, err = rr.b.StartReview(bg, manual(s, head, "r1"))
		wantCode(t, err, playgroundapi.CodePolicyIncomplete)
		rr.prefs = &config.GardenReviewPrefs{AccountID: "gone"}
		_, err = rr.b.StartReview(bg, manual(s, head, "r1"))
		wantCode(t, err, playgroundapi.CodePolicyIncomplete)
		rr.host.supported = false
		_, err = rr.b.StartReview(bg, manual(s, head, "r1"))
		wantCode(t, err, playgroundapi.CodeUnavailable)
		if rr.host.startCount() != 0 || len(rr.snapshot().Reports) != 0 {
			t.Fatal("a refused manual run must start and record nothing")
		}
	})

	t.Run("read-only unsupported fails closed before any process", func(t *testing.T) {
		for name, ro := range map[string]agents.ReadOnlySupport{
			"no read-only": {Reason: "fixed provider reason"},
			"no plan file": {Supported: true},
		} {
			rr := newReviewRig(t)
			wt, s := rr.attempt("att-1", 7, launch.StateAwaitingReview)
			rr.accts.list = []agentsapi.Account{account("acct", ro)}
			_, err := rr.b.StartReview(bg, manual(s, rr.head(wt), "r1"))
			wantCode(t, err, playgroundapi.CodeReadOnlyUnsupported)
			if rr.host.startCount() != 0 || len(rr.snapshot().Reports) != 0 {
				t.Fatalf("%s: process started or run recorded", name)
			}
		}
	})

	t.Run("a runtime refusal blocks the run", func(t *testing.T) {
		rr := newReviewRig(t)
		wt, s := rr.attempt("att-1", 7, launch.StateAwaitingReview)
		rr.host.startErr = agents.ErrReadOnlyUnsupported
		_, err := rr.b.StartReview(bg, manual(s, rr.head(wt), "r1"))
		wantCode(t, err, playgroundapi.CodeReadOnlyUnsupported)
		if reps := rr.snapshot().Reports; len(reps) != 1 || reps[0].State != review.StateBlocked {
			t.Fatalf("reports = %+v", reps)
		}
	})

	t.Run("launch is read-only, scoped to one file outside the worktree", func(t *testing.T) {
		rr := newReviewRig(t)
		rr.prefs = &config.GardenReviewPrefs{AccountID: "acct"} // not Enabled: a confirmed manual run is still allowed
		wt, s := rr.attempt("att-1", 7, launch.StateAwaitingReview)
		head := rr.head(wt)
		v, err := rr.b.StartReview(bg, manual(s, head, "r1"))
		if err != nil || !v.Created || v.Report.State != review.StateRunning || v.Report.Commit != head || v.Report.Trigger != review.TriggerManual || v.Report.Auto {
			t.Fatalf("view = %+v err=%v", v, err)
		}
		if rr.host.startCount() != 1 {
			t.Fatalf("starts = %d", rr.host.startCount())
		}
		req := rr.host.starts[0]
		l := req.Launch
		if !l.ReadOnly || l.PlanFile == "" || !filepath.IsAbs(l.PlanFile) || l.PermissionMode != "" || l.Sandbox != "" || l.Approval != "" || l.FullAccess != nil {
			t.Fatalf("launch = %+v", l)
		}
		for _, bad := range []string{wt, rr.main} {
			if strings.HasPrefix(l.PlanFile, bad+string(filepath.Separator)) {
				t.Fatalf("report path %q is inside %q", l.PlanFile, bad)
			}
		}
		if !strings.HasPrefix(l.PlanFile, rr.reports.Root) {
			t.Fatalf("report path %q is outside the report root %q", l.PlanFile, rr.reports.Root)
		}
		if resolved, _ := filepath.EvalSymlinks(filepath.Dir(l.PlanFile)); resolved != filepath.Dir(l.PlanFile) {
			t.Fatal("report directory is behind a symlink")
		}
		if !strings.Contains(l.Prompt, l.PlanFile) || !strings.Contains(l.Prompt, head) || strings.Contains(l.Prompt, "Issue") {
			t.Fatalf("prompt = %q", l.Prompt)
		}
		if req.Task.Role != "reviewer" || req.Task.AttemptID != "att-1" || req.Task.IssueID != 7 || req.Task.RepositoryID != 99 || req.WorkDir != wt || string(req.AccountID) != "acct" {
			t.Fatalf("request = %+v", req)
		}
		if req.SessionID == "" || req.OperationID == "" || string(req.SessionID) != v.Report.RunSessionID {
			t.Fatalf("identity = %q %q vs %q", req.SessionID, req.OperationID, v.Report.RunSessionID)
		}
	})

	t.Run("double submit, in flight and retry", func(t *testing.T) {
		rr := newReviewRig(t)
		wt, s := rr.attempt("att-1", 7, launch.StateAwaitingReview)
		head := rr.head(wt)
		if _, err := rr.b.StartReview(bg, manual(s, head, "r1")); err != nil {
			t.Fatal(err)
		}
		v, err := rr.b.StartReview(bg, manual(s, head, "r1"))
		if err != nil || v.Created || rr.host.startCount() != 1 {
			t.Fatalf("double submit: created=%v starts=%d err=%v", v.Created, rr.host.startCount(), err)
		}
		_, err = rr.b.StartReview(bg, manual(s, head, "r2"))
		wantCode(t, err, playgroundapi.CodeReviewRunning)
		rr.finishWithReport(0, validReport)
		if _, err := rr.b.Reviews(bg, playgroundapi.ReviewsRequest{SessionID: s}); err != nil {
			t.Fatal(err)
		}
		v, err = rr.b.StartReview(bg, manual(s, head, "r2")) // an explicit retry is a new run
		if err != nil || !v.Created || v.Report.Revision != 2 || rr.host.startCount() != 2 {
			t.Fatalf("retry: %+v starts=%d err=%v", v.Report, rr.host.startCount(), err)
		}
	})

	t.Run("manual, foreign and reviewer sessions", func(t *testing.T) {
		rr := newReviewRig(t)
		wt, _ := rr.attempt("att-1", 7, launch.StateAwaitingReview)
		rr.add("manual", wt) // same worktree, no task
		_, err := rr.b.StartReview(bg, manual("manual", rr.head(wt), "r1"))
		wantCode(t, err, playgroundapi.CodeNotReviewable)
		v, err := rr.b.Reviews(bg, playgroundapi.ReviewsRequest{SessionID: "manual"})
		if err != nil || v.Reviewable || v.Reason == "" {
			t.Fatalf("manual session = %+v err=%v", v, err)
		}
		rr.add("rev", wt)
		rr.sessions.list[len(rr.sessions.list)-1].Task = &agents.TaskRef{IssueID: 7, AttemptID: "att-1", Role: "reviewer"}
		if v, _ = rr.b.Reviews(bg, playgroundapi.ReviewsRequest{SessionID: "rev"}); v.Reviewable {
			t.Fatal("a reviewer session is not itself reviewed")
		}
		// A session of another repository is refused outright.
		other := t.TempDir()
		rr.sessions.list = append(rr.sessions.list, session("alien", other, "project-other"))
		rr.sessions.list[len(rr.sessions.list)-1].Task = &agents.TaskRef{IssueID: 7, AttemptID: "att-1", Role: "implementation"}
		_, err = rr.b.Reviews(bg, playgroundapi.ReviewsRequest{SessionID: "alien"})
		wantCode(t, err, playgroundapi.CodeNotInRepository)
		_, err = rr.b.StartReview(bg, manual("alien", rr.head(wt), "r1"))
		wantCode(t, err, playgroundapi.CodeNotInRepository)
		_, err = rr.b.DismissFinding(bg, playgroundapi.DismissRequest{SessionID: "alien", ReportRevision: 1, FindingID: "f-0000000000000000", Reason: "x"})
		wantCode(t, err, playgroundapi.CodeNotInRepository)
		_, err = rr.b.PreviewAccept(bg, playgroundapi.AcceptPreviewRequest{SessionID: "alien"})
		wantCode(t, err, playgroundapi.CodeNotInRepository)
		// An attempt of another clone is not trusted.
		a := rr.attempts.byID["att-1"]
		a.Local = "other-clone"
		rr.attempts.byID["att-1"] = a
		if v, _ = rr.b.Reviews(bg, playgroundapi.ReviewsRequest{SessionID: "s-att-1"}); v.Reviewable {
			t.Fatal("a foreign attempt must not be reviewable")
		}
	})
}

func TestReconcile(t *testing.T) {
	t.Run("idempotent across calls and restarts", func(t *testing.T) {
		rr := newReviewRig(t)
		rr.attempt("att-1", 7, launch.StateAwaitingReview)
		v, err := rr.b.ReconcileReviews(bg)
		if err != nil || !v.Enabled || len(v.Started) != 1 || rr.host.startCount() != 1 {
			t.Fatalf("first = %+v starts=%d err=%v", v, rr.host.startCount(), err)
		}
		req := rr.host.starts[0]
		if !req.Launch.ReadOnly || req.Task.Role != "reviewer" || !strings.HasPrefix(req.Launch.PlanFile, rr.reports.Root) {
			t.Fatalf("auto launch = %+v", req)
		}
		if reps := rr.snapshot().Reports; len(reps) != 1 || !reps[0].Auto || reps[0].Trigger != review.TriggerAwaitingReview {
			t.Fatalf("reports = %+v", reps)
		}
		for i := 0; i < 2; i++ {
			if v, _ = rr.b.ReconcileReviews(bg); len(v.Started) != 0 {
				t.Fatalf("repeat %d started %+v", i, v.Started)
			}
		}
		// Finish it, then a "restart": new store handle and new backend on the same files.
		rr.finishWithReport(0, validReport)
		if v, _ = rr.b.ReconcileReviews(bg); v.Settled != 1 || len(v.Started) != 0 {
			t.Fatalf("settle = %+v", v)
		}
		store2 := &review.FileStore{Path: rr.storeAt, Now: rr.store.Now}
		b2, err := newBackend(rr.main, rr.b.deps)
		if err != nil {
			t.Fatal(err)
		}
		b2.deps.Review = store2
		if v, _ = b2.ReconcileReviews(bg); len(v.Started) != 0 || rr.host.startCount() != 1 {
			t.Fatalf("after restart: %+v starts=%d", v, rr.host.startCount())
		}
	})

	t.Run("a crash between record and start never starts twice", func(t *testing.T) {
		rr := newReviewRig(t)
		rr.attempt("att-1", 7, launch.StateAwaitingReview)
		rr.host.startErr = errors.New("transport broke")
		v, err := rr.b.ReconcileReviews(bg)
		if err != nil || len(v.Started) != 0 {
			t.Fatalf("view = %+v err=%v", v, err)
		}
		rr.host.startErr = nil
		// The holder answers that the identity never started: the run ends interrupted, and the recorded key is
		// not started again.
		if v, _ = rr.b.ReconcileReviews(bg); len(v.Started) != 0 || v.Settled != 1 || rr.host.startCount() != 1 {
			t.Fatalf("a recorded key must not start again: %+v starts=%d", v, rr.host.startCount())
		}
		if v, _ = rr.b.ReconcileReviews(bg); len(v.Started) != 0 || rr.host.startCount() != 1 {
			t.Fatalf("settled key restarted: %+v", v)
		}
		if reps := rr.snapshot().Reports; reps[0].State != review.StateFailed || reps[0].FailCode != review.FailInterrupted {
			t.Fatalf("report = %+v", reps[0])
		}
	})

	t.Run("policy gates", func(t *testing.T) {
		for name, prefs := range map[string]*config.GardenReviewPrefs{
			"nil":      nil,
			"disabled": {Enabled: false, AccountID: "acct", Trigger: config.GardenReviewTriggerAwaitingReview},
			"manual":   {Enabled: true, AccountID: "acct", Trigger: config.GardenReviewTriggerManual},
		} {
			rr := newReviewRig(t)
			rr.attempt("att-1", 7, launch.StateAwaitingReview)
			rr.prefs = prefs
			v, err := rr.b.ReconcileReviews(bg)
			if err != nil || v.Enabled || len(v.Started) != 0 || rr.host.startCount() != 0 {
				t.Fatalf("%s: %+v starts=%d err=%v", name, v, rr.host.startCount(), err)
			}
		}
		rr := newReviewRig(t)
		rr.attempt("att-1", 7, launch.StateAwaitingReview)
		rr.host.supported = false
		if v, _ := rr.b.ReconcileReviews(bg); !v.Unsupported || rr.host.startCount() != 0 {
			t.Fatalf("unsupported runtime: %+v", v)
		}
		rr = newReviewRig(t)
		rr.attempt("att-1", 7, launch.StateAwaitingReview)
		rr.prefsErrBackend()
		if v, _ := rr.b.ReconcileReviews(bg); v.Err == nil || rr.host.startCount() != 0 {
			t.Fatalf("unreadable prefs must start nothing: %+v", v)
		}
	})

	t.Run("attempts of other states and clones are ignored", func(t *testing.T) {
		rr := newReviewRig(t)
		rr.attempt("att-run", 7, launch.StateRunning)
		rr.attempt("att-failed", 8, launch.StateFailed)
		rr.attempt("att-clean", 9, launch.StateCleaned)
		rr.attempt("att-foreign", 10, launch.StateAwaitingReview)
		for i := range rr.lister.list {
			if rr.lister.list[i].ID == "att-foreign" {
				rr.lister.list[i].Local = "other-clone"
			}
		}
		if v, _ := rr.b.ReconcileReviews(bg); len(v.Started) != 0 {
			t.Fatalf("started %+v", v.Started)
		}
		// new-commit also reviews a running attempt, but only once a run exists for it: not on the first sight.
		rr.prefs.Trigger = config.GardenReviewTriggerNewCommit
		if v, _ := rr.b.ReconcileReviews(bg); len(v.Started) != 0 {
			t.Fatalf("new-commit on a never-reviewed running attempt started %+v", v.Started)
		}
	})

	t.Run("start cap and unsupported account", func(t *testing.T) {
		rr := newReviewRig(t)
		for i, id := range []string{"a1", "a2", "a3"} {
			rr.attempt(id, int64(10+i), launch.StateAwaitingReview)
		}
		v, _ := rr.b.ReconcileReviews(bg)
		if len(v.Started) != 2 || v.Deferred != 1 || rr.host.startCount() != 2 {
			t.Fatalf("cap: %+v starts=%d", v, rr.host.startCount())
		}
		// The third waits behind the others finishing; it is never started in the same call.
		for i := range rr.host.starts {
			rr.finishWithReport(i, validReport)
		}
		if v, _ = rr.b.ReconcileReviews(bg); len(v.Started) != 1 || rr.host.startCount() != 3 {
			t.Fatalf("third: %+v starts=%d", v, rr.host.startCount())
		}

		rr = newReviewRig(t)
		rr.attempt("att-1", 7, launch.StateAwaitingReview)
		rr.accts.list = []agentsapi.Account{account("acct", agents.ReadOnlySupport{Reason: "none"})}
		if v, _ = rr.b.ReconcileReviews(bg); v.Blocked != 1 || len(v.Started) != 0 || rr.host.startCount() != 0 {
			t.Fatalf("unsupported: %+v starts=%d", v, rr.host.startCount())
		}
		reps := rr.snapshot().Reports
		if len(reps) != 1 || reps[0].State != review.StateBlocked || reps[0].FailCode == "" {
			t.Fatalf("blocked report = %+v", reps)
		}
		rr.accts.list = []agentsapi.Account{account("acct", enforced)}
		if v, _ = rr.b.ReconcileReviews(bg); len(v.Started) != 0 {
			t.Fatal("a blocked key is not retried automatically")
		}
	})
}

func TestRunOutcomes(t *testing.T) {
	setup := func(t *testing.T) (*reviewRig, string, string) {
		rr := newReviewRig(t)
		wt, s := rr.attempt("att-1", 7, launch.StateAwaitingReview)
		if _, err := rr.b.StartReview(bg, manual(s, rr.head(wt), "r1")); err != nil {
			t.Fatal(err)
		}
		return rr, wt, s
	}
	reviews := func(t *testing.T, rr *reviewRig, s string) playgroundapi.ReviewsView {
		v, err := rr.b.Reviews(bg, playgroundapi.ReviewsRequest{SessionID: s})
		if err != nil {
			t.Fatal(err)
		}
		return v
	}

	t.Run("valid report", func(t *testing.T) {
		rr, wt, s := setup(t)
		if v := reviews(t, rr, s); !v.RunInFlight || v.Reports[0].State != review.StateRunning {
			t.Fatalf("running = %+v", v)
		}
		rr.finishWithReport(0, validReport)
		v := reviews(t, rr, s)
		if v.RunInFlight || len(v.Reports) != 1 || !v.HeadRead || v.Head != rr.head(wt) {
			t.Fatalf("view = %+v", v)
		}
		r := v.Reports[0]
		if r.State != review.StateReady || r.Summary != "looks fine" || len(r.Findings) != 1 || r.ForOlderCommit || r.Superseded || r.Findings[0].Severity != review.SeverityHigh {
			t.Fatalf("report = %+v", r)
		}
	})
	t.Run("invalid, missing and oversized", func(t *testing.T) {
		for name, tc := range map[string]struct {
			body string
			want review.State
			code review.FailCode
		}{
			"invalid json":   {`{"schema":2}`, review.StateInvalid, review.FailInvalid},
			"unknown field":  {`{"schema":1,"summary":"s","extra":1}`, review.StateInvalid, review.FailInvalid},
			"bad path":       {`{"schema":1,"summary":"s","findings":[{"severity":"low","title":"t","path":"../x","line":1,"evidence":"e"}]}`, review.StateInvalid, review.FailInvalid},
			"missing":        {"", review.StateFailed, review.FailNoReport},
			"oversized file": {strings.Repeat("x", review.MaxReportBytes+5), review.StateInvalid, review.FailInvalid},
		} {
			rr, _, s := setup(t)
			rr.finishWithReport(0, tc.body)
			r := reviews(t, rr, s).Reports[0]
			if r.State != tc.want || r.FailCode != tc.code || len(r.Findings) != 0 || r.Summary != "" {
				t.Fatalf("%s: %+v", name, r)
			}
		}
	})
	t.Run("session failed", func(t *testing.T) {
		rr, _, s := setup(t)
		rr.host.finish(string(rr.host.starts[0].SessionID), "failed", true, true)
		if r := reviews(t, rr, s).Reports[0]; r.State != review.StateFailed || r.FailCode != review.FailInterrupted {
			t.Fatalf("report = %+v", r)
		}
	})
	t.Run("timeout stops only the reviewer session", func(t *testing.T) {
		rr, _, s := setup(t)
		rr.tick = rr.tick.Add(reviewRunTimeout + time.Minute)
		r := reviews(t, rr, s).Reports[0]
		if r.State != review.StateFailed || r.FailCode != review.FailTimeout {
			t.Fatalf("report = %+v", r)
		}
		if len(rr.host.stops) != 1 || rr.host.stops[0] != string(rr.host.starts[0].SessionID) {
			t.Fatalf("stops = %v", rr.host.stops)
		}
	})
	t.Run("lookup failure is unknown, not interrupted", func(t *testing.T) {
		rr, _, s := setup(t)
		rr.host.queryErr = errors.New("holder unreachable")
		if r := reviews(t, rr, s).Reports[0]; r.State != review.StateRunning {
			t.Fatalf("a failed lookup must not fail the run: %+v", r)
		}
		rr.host.queryErr = nil
		delete(rr.host.answers, string(rr.host.starts[0].SessionID)) // holder answers: fenced, never started
		if r := reviews(t, rr, s).Reports[0]; r.State != review.StateFailed || r.FailCode != review.FailInterrupted {
			t.Fatalf("a definitive 'not found' interrupts: %+v", r)
		}
	})
	t.Run("a session of another operation is never this run", func(t *testing.T) {
		rr, _, s := setup(t)
		id := string(rr.host.starts[0].SessionID)
		rr.host.answers[id] = rr.host.session(id, "someone-elses-op", "exited", true, true)
		if r := reviews(t, rr, s).Reports[0]; r.State != review.StateRunning {
			t.Fatalf("report = %+v", r)
		}
	})
	t.Run("new commit keeps the report bound to its own commit", func(t *testing.T) {
		rr, wt, s := setup(t)
		old := rr.head(wt)
		write(t, filepath.Join(wt, "more.txt"), "more\n")
		now := rr.commit(wt, "more")
		rr.finishWithReport(0, validReport)
		v := reviews(t, rr, s)
		r := v.Reports[0]
		if r.State != review.StateReady || r.Commit != old || !r.ForOlderCommit || v.Head != now {
			t.Fatalf("report = %+v head=%s", r, v.Head)
		}
	})
	t.Run("sections fail independently", func(t *testing.T) {
		rr, wt, s := setup(t)
		rr.finishWithReport(0, validReport)
		reviews(t, rr, s)
		if err := writeFileBytes(rr.storeAt, []byte("not json")); err != nil {
			t.Fatal(err)
		}
		v := reviews(t, rr, s)
		if v.ReportsErr == nil || v.ReportsErr.Code != playgroundapi.CodeStore || !v.HeadRead || v.Head != rr.head(wt) || !v.Reviewable {
			t.Fatalf("store failure must leave HEAD: %+v", v)
		}
		rr, wt, s = setup(t)
		rr.b.deps.Git = func(context.Context, string, ...string) (string, error) { return "", errors.New("git broke") }
		_ = wt
		v = reviews(t, rr, s)
		if v.HeadErr == nil || v.HeadRead || v.ReportsErr != nil || len(v.Reports) != 1 {
			t.Fatalf("HEAD failure must leave reports: %+v", v)
		}
	})
	t.Run("other clones are isolated", func(t *testing.T) {
		rr, _, s := setup(t)
		rr.finishWithReport(0, validReport)
		reviews(t, rr, s)
		other := newRepo(t, "main")
		b2, err := newBackend(other.main, rr.b.deps)
		if err != nil {
			t.Fatal(err)
		}
		snap, err := rr.store.Load(bg, b2.local)
		if err != nil || len(snap.Reports) != 0 {
			t.Fatalf("other clone sees %+v err=%v", snap.Reports, err)
		}
	})
}

func TestDismissBinding(t *testing.T) {
	rr := newReviewRig(t)
	wt, s := rr.attempt("att-1", 7, launch.StateAwaitingReview)
	head := rr.head(wt)
	if _, err := rr.b.StartReview(bg, manual(s, head, "r1")); err != nil {
		t.Fatal(err)
	}
	rr.finishWithReport(0, validReport)
	v, _ := rr.b.Reviews(bg, playgroundapi.ReviewsRequest{SessionID: s})
	f := v.Reports[0].Findings[0]
	req := playgroundapi.DismissRequest{SessionID: s, ReportRevision: 1, FindingID: f.ID, Reason: "  accepted risk  "}
	d, err := rr.b.DismissFinding(bg, req)
	if err != nil || !d.Created || d.Dismissal.Reason != "accepted risk" || d.Dismissal.Revision != 1 || d.Dismissal.Commit != head {
		t.Fatalf("dismiss = %+v err=%v", d, err)
	}
	if d, err = rr.b.DismissFinding(bg, req); err != nil || d.Created {
		t.Fatalf("idempotent dismiss = %+v err=%v", d, err)
	}
	for name, bad := range map[string]playgroundapi.DismissRequest{
		"empty reason":   {SessionID: s, ReportRevision: 1, FindingID: f.ID, Reason: "  "},
		"control reason": {SessionID: s, ReportRevision: 1, FindingID: f.ID, Reason: "a\x1bb"},
		"long reason":    {SessionID: s, ReportRevision: 1, FindingID: f.ID, Reason: strings.Repeat("a", 501)},
		"bad revision":   {SessionID: s, ReportRevision: 0, FindingID: f.ID, Reason: "x"},
		"empty finding":  {SessionID: s, ReportRevision: 1, Reason: "x"},
	} {
		_, err := rr.b.DismissFinding(bg, bad)
		wantCode(t, err, playgroundapi.CodeInvalidInput)
		_ = name
	}
	_, err = rr.b.DismissFinding(bg, playgroundapi.DismissRequest{SessionID: s, ReportRevision: 9, FindingID: f.ID, Reason: "x"})
	wantCode(t, err, playgroundapi.CodeStale)
	_, err = rr.b.DismissFinding(bg, playgroundapi.DismissRequest{SessionID: s, ReportRevision: 1, FindingID: "f-0000000000000000", Reason: "x"})
	wantCode(t, err, playgroundapi.CodeStale)

	v, _ = rr.b.Reviews(bg, playgroundapi.ReviewsRequest{SessionID: s})
	if got := v.Reports[0].Findings[0]; got.Dismissed == nil || got.Dismissed.Commit != head || got.Dismissed.Revision != 1 {
		t.Fatalf("finding = %+v", got)
	}
	// A new revision with the same finding is previously dismissed, not dismissed.
	if _, err := rr.b.StartReview(bg, manual(s, head, "r2")); err != nil {
		t.Fatal(err)
	}
	rr.finishWithReport(1, validReport)
	v, _ = rr.b.Reviews(bg, playgroundapi.ReviewsRequest{SessionID: s})
	if len(v.Reports) != 2 || v.Reports[0].Revision != 2 || v.Reports[1].Revision != 1 || !v.Reports[1].Superseded || v.Reports[0].Superseded {
		t.Fatalf("history = %+v", v.Reports)
	}
	if got := v.Reports[0].Findings[0]; got.Dismissed != nil || got.PriorDismissal != "previously dismissed in r1" {
		t.Fatalf("revision 2 finding = %+v", got)
	}
	if v.Reports[1].Findings[0].Dismissed == nil {
		t.Fatal("the old revision keeps its dismissal")
	}
}

func TestAcceptFlow(t *testing.T) {
	ready := func(t *testing.T) (*reviewRig, string, string) {
		rr := newReviewRig(t)
		wt, s := rr.attempt("att-1", 7, launch.StateAwaitingReview)
		if _, err := rr.b.StartReview(bg, manual(s, rr.head(wt), "r1")); err != nil {
			t.Fatal(err)
		}
		rr.finishWithReport(0, validReport)
		return rr, wt, s
	}
	pass := []github.Check{{Name: "ci", Status: "completed", Conclusion: "success"}}
	preview := func(t *testing.T, rr *reviewRig, s string) playgroundapi.AcceptPreviewView {
		p, err := rr.b.PreviewAccept(bg, playgroundapi.AcceptPreviewRequest{SessionID: s})
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	needs := func(p playgroundapi.AcceptPreviewView, a review.Ack) bool {
		for _, r := range p.Required {
			if r == a {
				return true
			}
		}
		return false
	}

	t.Run("CI states are independent and never inferred", func(t *testing.T) {
		rr, _, s := ready(t)
		rr.prefs.RequireCI = true
		rr.prefs.BlockOnHigh = true
		rr.gh.checks = pass
		p := preview(t, rr, s)
		if p.CI.State != review.CIPassed || needs(p, review.AckCINotPassed) || !needs(p, review.AckOpenHigh) || p.ReportRevision != 1 || len(p.Unresolved) != 1 || p.Counts[review.SeverityHigh] != 1 || !p.AcceptAllowed {
			t.Fatalf("passed = %+v", p)
		}
		rr.gh.checks = []github.Check{{Name: "ci", Status: "in_progress"}}
		if p = preview(t, rr, s); p.CI.State != review.CIPending || !needs(p, review.AckCINotPassed) {
			t.Fatalf("pending = %+v", p)
		}
		rr.gh.checks = []github.Check{{Name: "ci", Status: "completed", Conclusion: "failure"}}
		if p = preview(t, rr, s); p.CI.State != review.CIFailed || !needs(p, review.AckCINotPassed) {
			t.Fatalf("failed = %+v", p)
		}
		rr.gh.checksErr = errors.New("offline")
		if p = preview(t, rr, s); p.CI.State != review.CIUnavailable || p.CIErr == nil || !needs(p, review.AckCINotPassed) {
			t.Fatalf("unavailable must not read as passed: %+v", p)
		}
		rr.prefs.RequireCI, rr.prefs.BlockOnHigh = false, false
		if p = preview(t, rr, s); needs(p, review.AckCINotPassed) || needs(p, review.AckOpenHigh) {
			t.Fatalf("policy off needs nothing: %+v", p)
		}
	})

	t.Run("acknowledgments, then a decision and nothing else", func(t *testing.T) {
		rr, wt, s := ready(t)
		rr.prefs.RequireCI, rr.prefs.BlockOnHigh = true, true
		rr.gh.checksErr = errors.New("offline")
		p := preview(t, rr, s)
		before := len(rr.gh.calls)
		_, err := rr.b.Accept(bg, playgroundapi.AcceptRequest{Preview: p, Confirmed: false})
		wantCode(t, err, playgroundapi.CodeNotConfirmed)
		_, err = rr.b.Accept(bg, playgroundapi.AcceptRequest{Preview: p, Confirmed: true})
		wantCode(t, err, playgroundapi.CodeNeedsAcknowledgment)
		_, err = rr.b.Accept(bg, playgroundapi.AcceptRequest{Preview: p, Confirmed: true, Acknowledged: []string{"open-high"}})
		wantCode(t, err, playgroundapi.CodeNeedsAcknowledgment)
		if len(rr.snapshot().Decisions) != 0 {
			t.Fatal("a refused accept must record nothing")
		}
		acks := []string{string(review.AckOpenHigh), string(review.AckCINotPassed)}
		v, err := rr.b.Accept(bg, playgroundapi.AcceptRequest{Preview: p, Confirmed: true, Acknowledged: acks})
		if err != nil || !v.Created || v.Decision.ReviewedHead != rr.head(wt) || v.Decision.ReportRevision != 1 || v.Decision.CI.State != review.CIUnavailable || len(v.Decision.Unresolved) != 1 || v.Decision.ForOlderCommit {
			t.Fatalf("accept = %+v err=%v", v, err)
		}
		if v2, err := rr.b.Accept(bg, playgroundapi.AcceptRequest{Preview: p, Confirmed: true, Acknowledged: acks}); err != nil || v2.Created || v2.Decision.ID != v.Decision.ID {
			t.Fatalf("repeat accept = %+v err=%v", v2, err)
		}
		// Only a decision was recorded: no session stopped, no further GitHub traffic beyond the preview's checks,
		// no new session, and the worktree is untouched.
		if len(rr.host.stops) != 0 || rr.host.startCount() != 1 || len(rr.gh.calls) != before {
			t.Fatalf("accept had side effects: stops=%v starts=%d gh=%v", rr.host.stops, rr.host.startCount(), rr.gh.calls)
		}
		for _, c := range rr.gh.calls {
			if !strings.HasPrefix(c, "checks:") {
				t.Fatalf("unexpected GitHub call %q", c)
			}
		}
		if sh(t, wt, "status", "--porcelain") != "" {
			t.Fatal("worktree changed")
		}
		v3, _ := rr.b.Reviews(bg, playgroundapi.ReviewsRequest{SessionID: s})
		if len(v3.Decisions) != 1 || v3.Decisions[0].ForOlderCommit {
			t.Fatalf("decisions = %+v", v3.Decisions)
		}
		write(t, filepath.Join(wt, "later.txt"), "later\n")
		rr.commit(wt, "later")
		if v3, _ = rr.b.Reviews(bg, playgroundapi.ReviewsRequest{SessionID: s}); !v3.Decisions[0].ForOlderCommit || !v3.Reports[0].ForOlderCommit {
			t.Fatalf("a new commit must mark old provenance: %+v", v3)
		}
	})

	t.Run("stale head, report and findings are refused", func(t *testing.T) {
		rr, wt, s := ready(t)
		rr.gh.checks = pass
		p := preview(t, rr, s)
		acks := []string{string(review.AckOpenHigh)}
		_ = acks
		// A finding is dismissed after the preview.
		v, _ := rr.b.Reviews(bg, playgroundapi.ReviewsRequest{SessionID: s})
		if _, err := rr.b.DismissFinding(bg, playgroundapi.DismissRequest{SessionID: s, ReportRevision: 1, FindingID: v.Reports[0].Findings[0].ID, Reason: "fine"}); err != nil {
			t.Fatal(err)
		}
		_, err := rr.b.Accept(bg, playgroundapi.AcceptRequest{Preview: p, Confirmed: true})
		wantCode(t, err, playgroundapi.CodeStale)
		p = preview(t, rr, s)
		write(t, filepath.Join(wt, "later.txt"), "later\n")
		rr.commit(wt, "later")
		_, err = rr.b.Accept(bg, playgroundapi.AcceptRequest{Preview: p, Confirmed: true, Acknowledged: reportAcks(p)})
		wantCode(t, err, playgroundapi.CodeStale)
		// The old report no longer covers HEAD: accepting needs the stale-report acknowledgment.
		p = preview(t, rr, s)
		if !p.ReportStale || !needs(p, review.AckReportStale) {
			t.Fatalf("preview = %+v", p)
		}
		if _, err := rr.b.Accept(bg, playgroundapi.AcceptRequest{Preview: p, Confirmed: true}); err == nil {
			t.Fatal("stale report accepted without acknowledgment")
		}
		if _, err := rr.b.Accept(bg, playgroundapi.AcceptRequest{Preview: p, Confirmed: true, Acknowledged: reportAcks(p)}); err != nil {
			t.Fatal(err)
		}
		// A tampered token cannot lower the requirements: they are recomputed.
		p = preview(t, rr, s)
		p.Required = nil
		if _, err := rr.b.Accept(bg, playgroundapi.AcceptRequest{Preview: p, Confirmed: true}); err == nil {
			t.Fatal("a tampered preview skipped the stale-report acknowledgment")
		}
	})

	t.Run("no report at all", func(t *testing.T) {
		rr := newReviewRig(t)
		_, s := rr.attempt("att-1", 7, launch.StateAwaitingReview)
		p := preview(t, rr, s)
		if p.ReportRevision != 0 || !needs(p, review.AckNoReport) {
			t.Fatalf("preview = %+v", p)
		}
		if _, err := rr.b.Accept(bg, playgroundapi.AcceptRequest{Preview: p, Confirmed: true}); err == nil {
			t.Fatal("accepted without the no-report acknowledgment")
		}
		if v, err := rr.b.Accept(bg, playgroundapi.AcceptRequest{Preview: p, Confirmed: true, Acknowledged: reportAcks(p)}); err != nil || v.Decision.ReportRevision != 0 {
			t.Fatalf("accept = %+v err=%v", v, err)
		}
	})
}

func reportAcks(p playgroundapi.AcceptPreviewView) []string {
	var out []string
	for _, r := range p.Required {
		out = append(out, string(r))
	}
	return out
}

func TestReviewUnavailableParts(t *testing.T) {
	rr := newReviewRig(t)
	wt, s := rr.attempt("att-1", 7, launch.StateAwaitingReview)
	rr.b.deps.Review = nil
	v, err := rr.b.Reviews(bg, playgroundapi.ReviewsRequest{SessionID: s})
	if err != nil || v.ReportsErr == nil || !v.HeadRead {
		t.Fatalf("view = %+v err=%v", v, err)
	}
	_, err = rr.b.StartReview(bg, manual(s, rr.head(wt), "r1"))
	wantCode(t, err, playgroundapi.CodeStore)
	if rv, _ := rr.b.ReconcileReviews(bg); rv.Err == nil {
		t.Fatal("reconcile must report the missing store")
	}
	// Changes keeps working without the review store.
	if _, err := rr.b.Changes(bg, playgroundapi.ChangesRequest{SessionID: s}); err != nil {
		t.Fatal(err)
	}
}

func TestReviewerReceivesCommittedDiffWithoutShellAccess(t *testing.T) {
	rr := newReviewRig(t)
	wt, s := rr.attempt("att-context", 17, launch.StateAwaitingReview)
	write(t, filepath.Join(wt, "context.txt"), "commit evidence\n")
	head := rr.commit(wt, "review context")
	if _, err := rr.b.StartReview(bg, manual(s, head, "context")); err != nil {
		t.Fatal(err)
	}
	req := rr.host.starts[0]
	if !strings.Contains(req.Launch.Prompt, "+commit evidence") || !strings.Contains(req.Launch.Prompt, "no shell tool") || !strings.Contains(req.Launch.Prompt, head) {
		t.Fatalf("missing committed diff context: %q", req.Launch.Prompt)
	}
	if !req.Launch.ReadOnly || filepath.Base(req.Launch.PlanFile) != review.ReportFileName {
		t.Fatal("report launch lost read-only scope")
	}
	base := review.Base{MergeBase: head}
	original := rr.b.deps.Git
	rr.b.deps.Git = func(context.Context, string, ...string) (string, error) {
		return strings.Repeat("é\t", maxReviewDiffBytes), nil
	}
	diff, err := rr.b.reviewDiff(bg, reviewJob{head: head, tree: coregit.Worktree{Path: wt}}, base)
	rr.b.deps.Git = original
	if err != nil || len(diff) > maxReviewDiffBytes+300 || !strings.Contains(diff, "DIFF CONTEXT TRUNCATED") || !utf8.ValidString(diff) {
		t.Fatalf("diff bound: bytes=%d err=%v", len(diff), err)
	}
}
