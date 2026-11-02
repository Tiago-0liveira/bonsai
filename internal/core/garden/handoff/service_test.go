package handoff

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/approve"
	gardenfake "github.com/Tiago-0liveira/bonsai/internal/core/garden/fake"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/plan"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/planrev"
)

const (
	repo   = garden.RepoID(9)
	issue  = garden.IssueID(7)
	viewer = "fake-authenticated-id"
)

const goodSpec = "## Summary\nCap spend.\n\n## Requirements\nA monthly ceiling.\n\n## Acceptance criteria\nRequests stop at the ceiling.\n\n## Open questions\nNone"

const goodPlan = "## Assumptions\nPer project.\n\n## Touched areas\ninternal/budget\n\n## Steps\n1. Add it.\n\n## Tests\nTable test.\n\n## Risks\nRaces.\n\n## Acceptance mapping\nAC-1 -> step 1."

type env struct {
	t    *testing.T
	fake *gardenfake.Fake
	ap   *approve.Service
	pr   *planrev.Service
	svc  *Service
	tgt  approve.Target
	ctx  context.Context
	id   Identity
}

func newEnv(t *testing.T) *env {
	t.Helper()
	f := gardenfake.New()
	body, err := garden.CaptureBody(viewer, strings.Repeat("a", 32), "Teams want limits.")
	if err != nil {
		t.Fatal(err)
	}
	f.SeedIssue(garden.Issue{Repo: repo, ID: issue, Number: 7, Title: "Budget", Body: body, Metadata: garden.BodyMetadata(body),
		Labels: []string{garden.ManagedLabel, garden.FormatTypeLabel(garden.TypeBug), garden.FormatStageLabel(garden.StageDraft)}})
	e := &env{t: t, fake: f, ctx: context.Background(), tgt: approve.Target{Repo: repo, Issue: issue, Viewer: viewer, Login: "tiago"}}
	if e.ap, err = approve.New(approve.Deps{Reader: f, Writer: f}); err != nil {
		t.Fatal(err)
	}
	store := &plan.Store{Path: filepath.Join(t.TempDir(), "planner.json")}
	if e.pr, err = planrev.New(planrev.Deps{Log: e.ap, Writer: f, Attempts: store, Local: "local", Now: func() time.Time { return time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC) }}); err != nil {
		t.Fatal(err)
	}
	if e.svc, err = New(Deps{Reader: e.ap, Writer: f}); err != nil {
		t.Fatal(err)
	}
	e.approveSpecAndPlan(store)
	return e
}

func (e *env) approveSpecAndPlan(store *plan.Store) {
	t := e.t
	b, err := garden.NewSpecRevision(garden.IssueIdentity{Repo: repo, Issue: issue}, goodSpec, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.fake.AppendComment(e.ctx, repo, issue, b); err != nil {
		t.Fatal(err)
	}
	p, err := e.ap.Preview(e.ctx, e.tgt)
	if err != nil {
		t.Fatal(err)
	}
	res, err := e.ap.Validate(e.ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	p, _ = e.ap.Preview(e.ctx, e.tgt)
	spec := plan.SpecRef{RevisionHash: p.Head.Hash, CommentID: p.Head.CommentID, Ordinal: p.Ordinal, Fingerprint: p.Fingerprint}
	att := plan.Attempt{ID: "plan-1", Local: "local", Repo: repo, Issue: issue, Number: 7, Role: plan.RolePlanner, Spec: spec, Base: plan.Base{Commit: strings.Repeat("c", 40), Branch: "main"},
		Provider: "claude", AccountID: "acct", State: plan.StateStarting, RequestedAt: time.Now(), UpdatedAt: time.Now()}
	if err := store.Create(e.ctx, att); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Update(e.ctx, att.ID, func(cur plan.Attempt) (plan.Attempt, bool, error) {
		cur.State = plan.StateEnded
		return cur, true, nil
	}); err != nil {
		t.Fatal(err)
	}
	pp, err := e.pr.PreviewPublish(e.ctx, e.tgt, att.ID, goodPlan)
	if err != nil {
		t.Fatal(err)
	}
	pub, err := e.pr.Publish(e.ctx, pp)
	if err != nil {
		t.Fatal(err)
	}
	d, err := e.pr.Decide(e.ctx, planrev.DecisionRequest{Target: e.tgt, RevisionHash: pub.Hash, Fingerprint: spec.Fingerprint, SpecHash: spec.RevisionHash, ActorID: viewer, Decision: garden.DecisionApproved})
	if err != nil {
		t.Fatal(err)
	}
	e.id = Identity{AttemptID: "att-1", Deliverable: garden.DeliverableCode, SpecRevision: spec.RevisionHash, SpecApprovalID: res.ApprovalCommentID, PlanRevision: pub.Hash, PlanDecisionID: d.CommentID,
		BaseSHA: hx("1", 40), FinalHead: hx("2", 40), FinalTree: hx("3", 40)}
}

func (e *env) writes() int {
	n := 0
	for _, l := range e.fake.WriteLog() {
		if strings.HasPrefix(l, "comment:") {
			n++
		}
	}
	return n
}

func (e *env) decide(d garden.HandoffDecision, text string) (DecideResult, error) {
	return e.svc.Decide(e.ctx, DecideRequest{Target: e.tgt, Identity: e.id, Decision: d, Text: text, ActorID: viewer})
}

func TestDecideRecordsAcceptanceOnce(t *testing.T) {
	e := newEnv(t)
	first, err := e.decide(garden.HandoffAccepted, "Looks good.")
	if err != nil || first.Replayed || first.CommentID == 0 {
		t.Fatalf("first = %+v, %v", first, err)
	}
	before := e.writes()
	again, err := e.decide(garden.HandoffAccepted, "Looks good.")
	if err != nil || !again.Replayed || again.CommentID != first.CommentID || e.writes() != before {
		t.Fatalf("replay = %+v, %v, writes %d -> %d", again, err, before, e.writes())
	}
	h, err := e.svc.Read(e.ctx, e.tgt)
	if err != nil || len(h.Entries) != 1 || Current(h, e.id).State != DecisionCurrent || h.Entries[0].Detail() != "Looks good." {
		t.Fatalf("log = %+v, %v", h, err)
	}
	if _, err := e.decide(garden.HandoffAccepted, "Different words."); err != nil || e.writes() != before+1 {
		t.Fatalf("a different note is a new record: %v", err)
	}
}

func TestDecideRevokeIsReplaySafe(t *testing.T) {
	e := newEnv(t)
	acc, _ := e.decide(garden.HandoffAccepted, "")
	rev, err := e.svc.Decide(e.ctx, DecideRequest{Target: e.tgt, Decision: garden.HandoffRevoked, TargetCommentID: acc.CommentID, ActorID: viewer, Text: "Mistake."})
	if err != nil || rev.Replayed {
		t.Fatalf("revoke = %+v, %v", rev, err)
	}
	before := e.writes()
	again, err := e.svc.Decide(e.ctx, DecideRequest{Target: e.tgt, Decision: garden.HandoffRevoked, TargetCommentID: acc.CommentID, ActorID: viewer})
	if err != nil || !again.Replayed || again.CommentID != rev.CommentID || e.writes() != before {
		t.Fatalf("again = %+v, %v", again, err)
	}
	h, _ := e.svc.Read(e.ctx, e.tgt)
	if Current(h, e.id).State != DecisionRevoked {
		t.Fatalf("state = %s", Current(h, e.id).State)
	}
	if _, err := e.svc.Decide(e.ctx, DecideRequest{Target: e.tgt, Decision: garden.HandoffRevoked, TargetCommentID: 999, ActorID: viewer}); !errors.Is(err, ErrNoTarget) {
		t.Fatalf("err = %v", err)
	}
	if _, err := e.decide(garden.HandoffAccepted, ""); err != nil {
		t.Fatalf("accepting again after a revocation: %v", err)
	}
}

func TestDecideRefusals(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*env, *DecideRequest)
		want   error
	}{
		{"actor changed", func(e *env, r *DecideRequest) { r.ActorID = "someone-else" }, ErrActorChanged},
		{"no actor", func(e *env, r *DecideRequest) { r.ActorID = "" }, ErrActorChanged},
		{"read only", func(e *env, r *DecideRequest) { r.Target.ReadOnly = true }, ErrReadOnly},
		{"spec changed", func(e *env, r *DecideRequest) { r.Identity.SpecRevision = hx("f", 64) }, ErrInputsChanged},
		{"approval changed", func(e *env, r *DecideRequest) { r.Identity.SpecApprovalID++ }, ErrInputsChanged},
		{"plan changed", func(e *env, r *DecideRequest) { r.Identity.PlanRevision = hx("f", 64) }, ErrInputsChanged},
		{"plan decision changed", func(e *env, r *DecideRequest) { r.Identity.PlanDecisionID++ }, ErrInputsChanged},
		{"changes need text", func(e *env, r *DecideRequest) { r.Decision, r.Text = garden.HandoffChangesRequested, " " }, ErrTextRequired},
		{"marker text", func(e *env, r *DecideRequest) { r.Text = "bonsai:garden/v1" }, ErrInvalidText},
		{"long text", func(e *env, r *DecideRequest) { r.Text = strings.Repeat("x", MaxTextBytes+1) }, ErrInvalidText},
		{"unknown decision", func(e *env, r *DecideRequest) { r.Decision = "maybe" }, ErrInvalidDecision},
		{"revoke without target", func(e *env, r *DecideRequest) { r.Decision = garden.HandoffRevoked }, ErrInvalidDecision},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			req := DecideRequest{Target: e.tgt, Identity: e.id, Decision: garden.HandoffAccepted, ActorID: viewer}
			tc.change(e, &req)
			before := e.writes()
			if _, err := e.svc.Decide(e.ctx, req); !errors.Is(err, tc.want) || e.writes() != before {
				t.Fatalf("err = %v, want %v (writes %d -> %d)", err, tc.want, before, e.writes())
			}
		})
	}
}

func TestDecideRefusesWhenSpecMovedOn(t *testing.T) {
	e := newEnv(t)
	b, _ := garden.NewSpecRevision(garden.IssueIdentity{Repo: repo, Issue: issue}, goodSpec+"\n\n## Constraints\nChanged", []string{e.id.SpecRevision})
	if _, err := e.fake.AppendComment(e.ctx, repo, issue, b); err != nil {
		t.Fatal(err)
	}
	if _, err := e.decide(garden.HandoffAccepted, ""); !errors.Is(err, ErrInputsChanged) {
		t.Fatalf("err = %v", err)
	}
}

func TestDecideAppendFailureAndUntrustedRecords(t *testing.T) {
	e := newEnv(t)
	e.fake.FailNext(errors.New("boom"))
	if _, err := e.decide(garden.HandoffAccepted, ""); err == nil {
		t.Fatal("failure swallowed")
	}
	// A forged acceptance by another account is ignored and does not count as a replay.
	body, err := garden.NewHandoff(garden.IssueIdentity{Repo: repo, Issue: issue}, e.id.Record(garden.HandoffAccepted), 0, "Forged.")
	if err != nil {
		t.Fatal(err)
	}
	e.fake.AppendCommentAs(repo, issue, "mallory", "mallory-id", body)
	h, err := e.svc.Read(e.ctx, e.tgt)
	if err != nil || len(h.Entries) != 0 || len(h.Problems) != 1 {
		t.Fatalf("log = %+v, %v", h, err)
	}
	if res, err := e.decide(garden.HandoffAccepted, "Forged."); err != nil || res.Replayed {
		t.Fatalf("forged record replayed: %+v, %v", res, err)
	}
}

func TestDecideChangesRequestedAndNonCode(t *testing.T) {
	e := newEnv(t)
	if _, err := e.decide(garden.HandoffChangesRequested, "Add a test."); err != nil {
		t.Fatal(err)
	}
	h, _ := e.svc.Read(e.ctx, e.tgt)
	if cur := Current(h, e.id); cur.State != DecisionCurrent || cur.Entry.Record.Decision != garden.HandoffChangesRequested {
		t.Fatalf("cur = %+v", cur)
	}
	e.id = Identity{Deliverable: garden.DeliverableNonCode, SpecRevision: e.id.SpecRevision, SpecApprovalID: e.id.SpecApprovalID, PlanRevision: e.id.PlanRevision, PlanDecisionID: e.id.PlanDecisionID}
	if _, err := e.decide(garden.HandoffCompleted, "Documented in the wiki."); err != nil {
		t.Fatal(err)
	}
	h, _ = e.svc.Read(e.ctx, e.tgt)
	if got := Derive(Input{Identity: e.id, Decision: Current(h, e.id), Now: time.Now()}); got.State != CompletionDone {
		t.Fatalf("got %+v", got)
	}
}

type readOnlyReader struct{ Reader }

func (r readOnlyReader) Read(ctx context.Context, t approve.Target) (approve.Log, error) {
	log, err := r.Reader.Read(ctx, t)
	log.Verification.ApprovalReadOnly = true
	return log, err
}

func TestDecideRefusesWhenVerificationIsReadOnly(t *testing.T) {
	e := newEnv(t)
	svc, err := New(Deps{Reader: readOnlyReader{e.ap}, Writer: e.fake})
	if err != nil {
		t.Fatal(err)
	}
	before := e.writes()
	_, err = svc.Decide(e.ctx, DecideRequest{Target: e.tgt, Identity: e.id, Decision: garden.HandoffAccepted, ActorID: viewer})
	if !errors.Is(err, ErrReadOnly) || e.writes() != before {
		t.Fatalf("err = %v (writes %d -> %d)", err, before, e.writes())
	}
}
