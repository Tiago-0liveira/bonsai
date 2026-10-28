package reviewpass

import (
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
)

func testPass(t *testing.T, n int) Pass {
	t.Helper()
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	snap := Snapshot{Local: "local", Repo: 1, Issue: 2, Number: 3, AttemptID: fmt.Sprintf("impl-%032x", n), Inputs: Inputs{SpecRevision: strings.Repeat("a", 64), SpecFingerprint: strings.Repeat("b", 64), SpecApprovalID: 4, PlanRevision: strings.Repeat("c", 64), PlanDecisionID: 5}, Worktree: Worktree{Path: filepath.Join(t.TempDir(), "tree"), Branch: "garden/test", BaseSHA: strings.Repeat("d", 40)}, Policy: testPolicy()}
	p, err := NewPass(fmt.Sprintf("rp-%032x", n), snap, Authorization{ActorID: "actor", PreviewID: "preview", ConfirmedAt: now}, now)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestNewPassAuthorization(t *testing.T) {
	p := testPass(t, 1)
	for _, change := range []func(*Authorization){func(a *Authorization) { a.ActorID = "" }, func(a *Authorization) { a.PreviewID = "" }, func(a *Authorization) { a.ConfirmedAt = time.Time{} }} {
		a := p.Authorization
		change(&a)
		if _, err := NewPass(p.ID, p.Snapshot, a, p.CreatedAt); !errors.Is(err, ErrNotAuthorized) {
			t.Fatalf("authorization = %v", err)
		}
	}
	snap := p.Snapshot
	snap.Policy.Reviewer.Native.Sandbox = "danger-full-access"
	snap.Policy.Reviewer.Escalated = true
	if _, err := NewPass(p.ID, snap, p.Authorization, p.CreatedAt); !errors.Is(err, ErrNotAuthorized) {
		t.Fatalf("escalation = %v", err)
	}
	a := p.Authorization
	a.EscalationAcknowledged = true
	escalated, err := NewPass(p.ID, snap, a, p.CreatedAt)
	if err != nil || !escalated.valid() {
		t.Fatalf("acknowledged escalation = %+v, %v", escalated, err)
	}
	snap.PolicyHash = "old"
	q, err := NewPass(p.ID, snap, a, p.CreatedAt)
	if err != nil || q.Snapshot.PolicyHash != snap.Policy.Hash() || q.State != StatePending || q.Revision != 1 {
		t.Fatalf("new pass = %+v, %v", q, err)
	}
	snap.Policy.Checks[0].Args[0] = "mutated"
	if q.Snapshot.Policy.Checks[0].Args[0] == "mutated" {
		t.Fatal("snapshot aliased")
	}
	if _, err := NewPass("bad", p.Snapshot, p.Authorization, p.CreatedAt); !errors.Is(err, ErrInvalid) {
		t.Fatal(err)
	}
}

func TestCanMoveMatrix(t *testing.T) {
	states := []State{StatePending, StateStarting, StateStartUnknown, StateRunning, StateChecking, StateAwaitingHumanReview, StateFailed, StateStopped, StateBlocked}
	allowed := map[State][]State{
		StatePending:      {StateStarting, StateBlocked, StateStopped, StateFailed},
		StateStarting:     {StateRunning, StateStartUnknown, StateFailed, StateStopped, StateBlocked},
		StateStartUnknown: {StateRunning, StateFailed, StateStopped},
		StateRunning:      {StateChecking, StateFailed, StateStopped, StateBlocked},
		StateChecking:     {StateAwaitingHumanReview, StateFailed, StateStopped},
	}
	for i, from := range states {
		if from.Active() != (i < 5) || from.Final() != (i >= 5) {
			t.Errorf("state classification %s", from)
		}
		for _, to := range states {
			want := from == to
			for _, s := range allowed[from] {
				want = want || s == to
			}
			if CanMove(from, to) != want {
				t.Errorf("%s -> %s = %v, want %v", from, to, CanMove(from, to), want)
			}
		}
	}
	if State("bad").Active() || State("bad").Final() || CanMove("bad", "bad") || CanMove(StatePending, "bad") {
		t.Fatal("unknown state accepted")
	}
}

func TestPassValidation(t *testing.T) {
	p := testPass(t, 1)
	edits := []struct {
		name string
		edit func(*Pass)
	}{
		{"id", func(p *Pass) { p.ID = "rp-bad" }}, {"revision", func(p *Pass) { p.Revision = 0 }},
		{"local", func(p *Pass) { p.Snapshot.Local = "" }}, {"repo", func(p *Pass) { p.Snapshot.Repo = 0 }}, {"issue", func(p *Pass) { p.Snapshot.Issue = 0 }}, {"number", func(p *Pass) { p.Snapshot.Number = 0 }},
		{"spec", func(p *Pass) { p.Snapshot.Inputs.SpecRevision = "bad" }}, {"fingerprint", func(p *Pass) { p.Snapshot.Inputs.SpecFingerprint = "bad" }},
		{"spec approval", func(p *Pass) { p.Snapshot.Inputs.SpecApprovalID = 0 }}, {"plan", func(p *Pass) { p.Snapshot.Inputs.PlanRevision = "bad" }}, {"plan decision", func(p *Pass) { p.Snapshot.Inputs.PlanDecisionID = 0 }},
		{"attempt", func(p *Pass) { p.Snapshot.AttemptID = "bad space" }}, {"relative path", func(p *Pass) { p.Snapshot.Worktree.Path = "relative" }},
		{"unclean path", func(p *Pass) { p.Snapshot.Worktree.Path += string(filepath.Separator) + ".." }},
		{"branch", func(p *Pass) { p.Snapshot.Worktree.Branch = "" }}, {"base sha", func(p *Pass) { p.Snapshot.Worktree.BaseSHA = "short" }},
		{"policy hash", func(p *Pass) { p.Snapshot.PolicyHash = strings.Repeat("0", 64) }}, {"bad policy", func(p *Pass) { p.Snapshot.Policy.Passes = 2; p.Snapshot.PolicyHash = p.Snapshot.Policy.Hash() }},
		{"auth", func(p *Pass) { p.Authorization.ActorID = "" }}, {"state", func(p *Pass) { p.State = "bad" }}, {"code", func(p *Pass) { p.Code = "private error" }},
		{"created", func(p *Pass) { p.CreatedAt = time.Time{} }}, {"updated", func(p *Pass) { p.UpdatedAt = time.Time{} }},
		{"implementation run", func(p *Pass) { p.ImplementationRun = p.Snapshot.AttemptID + "/start/0" }},
		{"implementation other attempt", func(p *Pass) { p.ImplementationRun = "other/start/1" }},
		{"implementation run too big", func(p *Pass) { p.ImplementationRun = p.Snapshot.AttemptID + "/start/10000" }},
		{"reviewed head", func(p *Pass) { p.ReviewedHead = "short" }}, {"final without reviewed", func(p *Pass) { p.FinalHead = strings.Repeat("a", 40) }},
		{"report", func(p *Pass) { p.Report = "private report prose" }},
		{"final missing end", func(p *Pass) { p.State = StateFailed }}, {"active ended", func(p *Pass) { p.EndedAt = ptr(p.CreatedAt) }},
		{"unknown check", func(p *Pass) { p.Checks = []CheckResult{{CheckID: "unknown", Outcome: OutcomeUnknown}} }},
		{"duplicate results", func(p *Pass) {
			r := CheckResult{CheckID: p.Snapshot.Policy.Checks[0].ID, Outcome: OutcomeUnknown}
			p.Checks = []CheckResult{r, r}
		}},
		{"review operation", func(p *Pass) { p.Reviewer = &Session{OperationID: "bad", SessionID: "sess_a", State: "starting"} }},
		{"review session", func(p *Pass) {
			p.Reviewer = &Session{OperationID: p.ID + "/review", SessionID: "bad", State: "starting"}
		}},
		{"unacknowledged running", func(p *Pass) {
			p.Reviewer = &Session{OperationID: p.ID + "/review", SessionID: "sess_a", State: "running"}
		}},
	}
	for _, tt := range edits {
		t.Run(tt.name, func(t *testing.T) {
			q := clonePass(p)
			tt.edit(&q)
			if q.valid() {
				t.Fatalf("accepted invalid %s", tt.name)
			}
		})
	}
	q := clonePass(p)
	q.ImplementationRun = q.Snapshot.AttemptID + "/start/9999"
	q.ReviewedHead = strings.Repeat("a", 40)
	q.FinalHead = strings.Repeat("b", 64)
	q.State = StateAwaitingHumanReview
	q.EndedAt = ptr(q.CreatedAt)
	q.Report = "ev-" + strings.Repeat("e", 32)
	q.Reviewer = &Session{OperationID: q.ID + "/review", SessionID: "sess_a", State: "exited", Acknowledged: true, ExitCode: ptr(0), StartedAt: ptr(q.CreatedAt), EndedAt: ptr(q.CreatedAt)}
	q.Checks = []CheckResult{{CheckID: q.Snapshot.Policy.Checks[0].ID, Outcome: OutcomePassed, ExitCode: ptr(0), Evidence: q.Report}}
	if !q.valid() {
		t.Fatal("valid final record rejected")
	}
	c := clonePass(q)
	*c.Reviewer.ExitCode = 1
	*c.Reviewer.StartedAt = time.Time{}
	*c.Reviewer.EndedAt = time.Time{}
	*c.Checks[0].ExitCode = 1
	*c.EndedAt = time.Time{}
	c.Snapshot.Policy.Checks[0].Args[0] = "mutated"
	if !q.valid() {
		t.Fatal("clone shares pointers or slices")
	}
}

func TestCheckResultsAndSummary(t *testing.T) {
	cases := []struct {
		outcome Outcome
		exit    *int
		ok      bool
	}{
		{OutcomePassed, ptr(0), true}, {OutcomePassed, nil, false}, {OutcomePassed, ptr(1), false},
		{OutcomeFailed, nil, true}, {OutcomeFailed, ptr(1), true}, {OutcomeFailed, ptr(-1), true}, {OutcomeFailed, ptr(0), false},
		{OutcomeSkipped, nil, true}, {OutcomeSkipped, ptr(0), false}, {OutcomeUnknown, nil, true}, {OutcomeUnknown, ptr(1), false}, {"bad", nil, false},
	}
	for _, tt := range cases {
		r := CheckResult{Outcome: tt.outcome, ExitCode: tt.exit}
		if r.valid() != tt.ok {
			t.Errorf("%s/%v = %v", tt.outcome, tt.exit, r.valid())
		}
	}
	r := CheckResult{Outcome: OutcomeUnknown, Evidence: "private text"}
	if r.valid() {
		t.Fatal("prose evidence accepted")
	}
	r.Evidence = "ev-" + strings.Repeat("f", 32)
	if !r.valid() {
		t.Fatal("evidence ref rejected")
	}
	now := time.Now()
	r.StartedAt = &now
	r.EndedAt = ptr(now.Add(-time.Second))
	if r.valid() {
		t.Fatal("backwards times accepted")
	}
	p := Policy{}
	for i := 0; i < 5; i++ {
		p.Checks = append(p.Checks, CheckFrom(config.GardenCheck{Name: "Test", Executable: "go", Args: []string{fmt.Sprint(i)}}))
	}
	results := []CheckResult{{CheckID: p.Checks[1].ID, Outcome: OutcomeFailed}, {CheckID: p.Checks[0].ID, Outcome: OutcomePassed}, {CheckID: p.Checks[2].ID, Outcome: OutcomeSkipped}, {CheckID: p.Checks[3].ID, Outcome: OutcomeUnknown}, {CheckID: "unknown", Outcome: OutcomeFailed}}
	want := CheckSummary{Passed: []string{p.Checks[0].ID}, Failed: []string{p.Checks[1].ID}, Skipped: []string{p.Checks[2].ID}, Unknown: []string{p.Checks[3].ID, p.Checks[4].ID}}
	if got := Summary(p, results); !reflect.DeepEqual(got, want) {
		t.Fatalf("summary = %+v", got)
	}
	c := config.GardenCheck{Name: "a", Executable: "go", Args: []string{"a", "bc"}, Dir: "dir"}
	first := CheckFrom(c)
	c.Name = "renamed"
	if CheckFrom(c).ID != first.ID {
		t.Fatal("name changes identity")
	}
	c.Args = []string{"ab", "c"}
	if CheckFrom(c).ID == first.ID {
		t.Fatal("argv boundary collision")
	}
	c.Args = []string{"a", "bc"}
	c.Dir = "other"
	if CheckFrom(c).ID == first.ID {
		t.Fatal("dir not hashed")
	}
}

func TestPassReviewerStateMatrix(t *testing.T) {
	states := []State{StatePending, StateStarting, StateStartUnknown, StateRunning, StateChecking, StateAwaitingHumanReview, StateFailed, StateStopped, StateBlocked}
	reviewers := []string{"", "starting", "running", "stopping", "exited", "failed", "not-started"}
	for _, state := range states {
		for _, reviewer := range reviewers {
			for _, ack := range []bool{false, true} {
				for _, ended := range []bool{false, true} {
					t.Run(fmt.Sprintf("%s/%s/ack=%v/ended=%v", state, reviewer, ack, ended), func(t *testing.T) {
						p := testPass(t, 1)
						p.State = state
						p.ReviewedHead, p.FinalHead = strings.Repeat("a", 40), strings.Repeat("b", 40)
						if state.Final() {
							p.EndedAt = ptr(p.CreatedAt)
						}
						if reviewer != "" {
							p.Reviewer = &Session{OperationID: p.ID + "/review", SessionID: "sess_a", State: reviewer, Acknowledged: ack}
							if ended {
								p.Reviewer.EndedAt = ptr(p.CreatedAt)
							}
						}
						want := false
						switch state {
						case StatePending:
							want = reviewer == ""
						case StateStarting:
							want = reviewer == "starting" && !ended
						case StateStartUnknown:
							want = reviewer == "starting" && !ack && !ended
						case StateRunning:
							want = (reviewer == "running" || reviewer == "stopping") && ack && !ended
						case StateChecking, StateAwaitingHumanReview:
							want = reviewer == "exited" && ack && ended
						case StateStopped, StateBlocked, StateFailed:
							want = reviewer == "" || (reviewer == "exited" || reviewer == "failed") && ack && ended || reviewer == "not-started" && !ack
						}
						if got := p.valid(); got != want {
							t.Fatalf("valid=%v, want %v", got, want)
						}
					})
				}
			}
		}
	}
	p := testPass(t, 1)
	p.State, p.EndedAt = StateAwaitingHumanReview, ptr(p.CreatedAt)
	p.Reviewer = &Session{OperationID: p.ID + "/review", SessionID: "sess_a", State: "exited", Acknowledged: true, EndedAt: ptr(p.CreatedAt)}
	if p.valid() {
		t.Fatal("awaiting human review accepted without final head")
	}
}

func TestReviewerTransitionMatrix(t *testing.T) {
	states := []string{"starting", "running", "stopping", "exited", "failed", "not-started"}
	allowed := map[string][]string{
		"starting": {"running", "stopping", "exited", "failed", "not-started"},
		"running":  {"stopping", "exited", "failed"},
		"stopping": {"exited", "failed"},
	}
	for _, from := range states {
		for _, to := range states {
			for _, ack := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/ack=%v", from, to, ack), func(t *testing.T) {
					cur := &Session{OperationID: "op", SessionID: "sess_a", State: from, Acknowledged: ack}
					next := *cur
					next.State = to
					want := from == to
					for _, state := range allowed[from] {
						want = want || state == to
					}
					want = want && !(to == "not-started" && ack)
					if err := validateReviewerTransition(cur, &next); (err == nil) != want {
						t.Fatalf("transition = %v, want allowed=%v", err, want)
					}
				})
			}
		}
	}
}
