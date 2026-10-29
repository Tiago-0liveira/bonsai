package planrev

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
)

const (
	repo   = garden.RepoID(9)
	issue  = garden.IssueID(7)
	viewer = "fake-authenticated-id"
	other  = "someone-else-id"
)

const goodSpec = "## Summary\nCap spend.\n\n## Requirements\nA monthly ceiling.\n\n## Acceptance criteria\nRequests stop at the ceiling.\n\n## Open questions\nNone"

const goodPlan = `## Assumptions
The ceiling is per project.

## Touched areas
internal/budget

## Steps
1. Add the ceiling.
2. Enforce it.

## Tests
Table test for the ceiling.

## Risks
Races on the counter.

## Acceptance mapping
AC-1 -> steps 1, 2.`

type env struct {
	t     *testing.T
	fake  *gardenfake.Fake
	ap    *approve.Service
	svc   *Service
	store *plan.Store
	tgt   Target
	ctx   context.Context
	spec  plan.SpecRef
	base  plan.Base
	att   plan.Attempt
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
	e := &env{t: t, fake: f, ctx: context.Background(), tgt: Target{Repo: repo, Issue: issue, Viewer: viewer, Login: "tiago"},
		base: plan.Base{Commit: strings.Repeat("c", 40), Branch: "main"}}
	if e.ap, err = approve.New(approve.Deps{Reader: f, Writer: f}); err != nil {
		t.Fatal(err)
	}
	e.store = &plan.Store{Path: filepath.Join(t.TempDir(), "planner.json")}
	if e.svc, err = New(Deps{Log: e.ap, Writer: f, Attempts: e.store, Local: "local", Now: func() time.Time { return time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC) }}); err != nil {
		t.Fatal(err)
	}
	e.approveSpec(goodSpec)
	return e
}

func (e *env) postSpec(content string, parents ...string) {
	e.t.Helper()
	b, err := garden.NewSpecRevision(garden.IssueIdentity{Repo: repo, Issue: issue}, content, parents)
	if err != nil {
		e.t.Fatal(err)
	}
	if _, err := e.fake.AppendComment(e.ctx, repo, issue, b); err != nil {
		e.t.Fatal(err)
	}
}

// approveSpec posts a spec revision, validates it (gate one) and records an attempt bound to it.
func (e *env) approveSpec(content string, parents ...string) {
	e.t.Helper()
	e.postSpec(content, parents...)
	p, err := e.ap.Preview(e.ctx, e.tgt)
	if err != nil {
		e.t.Fatal(err)
	}
	if _, err := e.ap.Validate(e.ctx, p); err != nil {
		e.t.Fatal(err)
	}
	p, _ = e.ap.Preview(e.ctx, e.tgt)
	e.spec = plan.SpecRef{RevisionHash: p.Head.Hash, CommentID: p.Head.CommentID, Ordinal: p.Ordinal, Fingerprint: p.Fingerprint}
	e.att = plan.Attempt{ID: "plan-" + p.Head.Hash[:6], Local: "local", Repo: repo, Issue: issue, Number: 7, Role: plan.RolePlanner, Spec: e.spec, Base: e.base,
		Provider: "claude", AccountID: "acct", State: plan.StateStarting, RequestedAt: time.Now(), UpdatedAt: time.Now()}
	if err := e.store.Create(e.ctx, e.att); err != nil {
		e.t.Fatal(err)
	}
	// Only one attempt may be live per Issue; this one is over, as a planner session that answered would be.
	if _, err := e.store.Update(e.ctx, e.att.ID, func(cur plan.Attempt) (plan.Attempt, bool, error) {
		cur.State = plan.StateEnded
		return cur, true, nil
	}); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) review() Review {
	e.t.Helper()
	r, err := e.svc.Review(e.ctx, e.tgt)
	if err != nil {
		e.t.Fatal(err)
	}
	return r
}

func (e *env) publish(text string) PublishResult {
	e.t.Helper()
	p, err := e.svc.PreviewPublish(e.ctx, e.tgt, e.att.ID, text)
	if err != nil {
		e.t.Fatalf("preview: %v", err)
	}
	r, err := e.svc.Publish(e.ctx, p)
	if err != nil {
		e.t.Fatalf("publish: %v", err)
	}
	return r
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

func (e *env) decide(d garden.Decision, hash, reason string) (DecisionResult, error) {
	e.t.Helper()
	rev, _ := e.review().ByHash(hash)
	return e.svc.Decide(e.ctx, DecisionRequest{Target: e.tgt, RevisionHash: hash, Fingerprint: rev.SourceSpecFingerprint, Decision: d, Reason: reason, SpecHash: e.spec.RevisionHash, ActorID: viewer})
}

func TestComposeIsContentAddressedAndDeterministic(t *testing.T) {
	spec := plan.SpecRef{RevisionHash: strings.Repeat("a", 64), Ordinal: 2, Fingerprint: strings.Repeat("b", 64)}
	base := plan.Base{Commit: strings.Repeat("c", 40), Branch: "main"}
	a := Compose(goodPlan, spec, base)
	b := Compose(strings.ReplaceAll(goodPlan, "\n", "  \r\n")+"\n\n", spec, base)
	if a != b || garden.ContentHash(a) != garden.ContentHash(b) {
		t.Fatalf("whitespace changed the revision:\n%q\n%q", a, b)
	}
	if again := Compose(a, spec, base); again != a {
		t.Fatalf("Compose is not idempotent:\n%q\n%q", a, again)
	}
	if other := Compose(goodPlan, spec, plan.Base{Commit: strings.Repeat("d", 40)}); garden.ContentHash(other) == garden.ContentHash(a) {
		t.Fatal("another inspected base produced the same revision")
	}
	// A pasted section that pretends to say what was inspected is replaced by the attempt's own record.
	forged := Compose("## Inspected base\nCommit: deadbeef\n\n"+goodPlan, spec, base)
	if forged != a {
		t.Fatalf("pasted base survived:\n%s", forged)
	}
	if got := field(Parse(a), SectionBase, "Commit"); got != base.Commit {
		t.Fatalf("base commit = %q", got)
	}
}

func TestPublishStoresVerifiedContentAddressedRevision(t *testing.T) {
	e := newEnv(t)
	if rv := e.review(); len(rv.Revisions) != 0 || rv.Latest != nil || rv.Current != nil || rv.Approved != nil || !rv.CanPublish() {
		t.Fatalf("empty review = %+v", rv)
	}
	res := e.publish(goodPlan)
	rv := e.review()
	if len(rv.Revisions) != 1 || rv.Latest == nil || rv.Current == nil || rv.Latest.Hash != res.Hash || rv.Current.Hash != res.Hash || rv.Approved != nil {
		t.Fatalf("review = %+v", rv)
	}
	r := rv.Current
	if r.Ordinal != 1 || res.Ordinal != 1 || !r.Trusted || !r.Consistent || !r.Reviewable() || r.State != StateAwaiting || r.Stale || r.Superseded {
		t.Fatalf("revision = %+v", r)
	}
	if r.SourceSpecFingerprint != e.spec.Fingerprint || r.BaseCommit != e.base.Commit || r.Hash != garden.ContentHash(r.Content) {
		t.Fatalf("binding = %+v", r)
	}
	for _, want := range Required {
		if !strings.Contains(r.Content, "## "+want) {
			t.Errorf("section %q missing from %q", want, r.Content)
		}
	}
	// The same publication twice finds the recorded revision.
	before := e.writes()
	again := e.publish(goodPlan)
	if !again.Replayed || again.CommentID != res.CommentID || e.writes() != before {
		t.Fatalf("replay = %+v, writes %d -> %d", again, before, e.writes())
	}
}

func TestPublishRefusals(t *testing.T) {
	e := newEnv(t)
	t.Run("blank text", func(t *testing.T) {
		if _, err := e.svc.PreviewPublish(e.ctx, e.tgt, e.att.ID, "  \n"); !errors.Is(err, ErrEmpty) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("incomplete plan lists what is missing and writes nothing", func(t *testing.T) {
		p, err := e.svc.PreviewPublish(e.ctx, e.tgt, e.att.ID, "## Steps\n1. Do it.")
		if err != nil || p.CanPublish() || !strings.Contains(p.Blockers[0], "Assumptions") || !strings.Contains(p.Blockers[0], "Acceptance mapping") {
			t.Fatalf("preview = %+v, %v", p.Blockers, err)
		}
		before := e.writes()
		_, err = e.svc.Publish(e.ctx, p)
		var inc IncompleteError
		if !errors.Is(err, ErrIncomplete) || !errors.As(err, &inc) || len(inc.Missing) != 5 || e.writes() != before {
			t.Fatalf("publish = %v (%v), writes %d -> %d", err, inc.Missing, before, e.writes())
		}
	})
	t.Run("too large", func(t *testing.T) {
		if _, err := e.svc.PreviewPublish(e.ctx, e.tgt, e.att.ID, goodPlan+"\n"+strings.Repeat("x", MaxBytes)); !errors.Is(err, ErrTooLarge) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("unknown or foreign attempt", func(t *testing.T) {
		if _, err := e.svc.PreviewPublish(e.ctx, e.tgt, "plan-nope", goodPlan); !errors.Is(err, plan.ErrNotFound) {
			t.Fatalf("unknown: %v", err)
		}
		foreign := e.att
		foreign.ID, foreign.Issue = "plan-foreign", 8
		if err := e.store.Create(e.ctx, foreign); err != nil {
			t.Fatal(err)
		}
		if _, err := e.svc.PreviewPublish(e.ctx, e.tgt, "plan-foreign", goodPlan); !errors.Is(err, ErrWrongAttempt) {
			t.Fatalf("foreign: %v", err)
		}
	})
	t.Run("attempt of an older specification", func(t *testing.T) {
		e2 := newEnv(t)
		old := e2.att
		e2.approveSpec(goodSpec+"\n\n## Constraints\nNo new deps.", e2.spec.RevisionHash)
		if _, err := e2.svc.PreviewPublish(e2.ctx, e2.tgt, old.ID, goodPlan); !errors.Is(err, ErrSpecChanged) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("unapproved specification", func(t *testing.T) {
		e2 := newEnv(t)
		e2.postSpec(goodSpec+"\n\n## Constraints\nNo new deps.", e2.spec.RevisionHash)
		if rv := e2.review(); rv.CanPublish() {
			t.Fatal("review offers publishing for a stale approval")
		}
		if _, err := e2.svc.PreviewPublish(e2.ctx, e2.tgt, e2.att.ID, goodPlan); !errors.Is(err, ErrNotApproved) {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestTamperedRevisionIsNeverTrusted(t *testing.T) {
	cases := []struct {
		name   string
		tamper func(e *env, c garden.IssueComment)
		// gone means the revision no longer verifies at all; otherwise it is shown but not reviewable.
		gone bool
	}{
		{"edited body", func(e *env, c garden.IssueComment) {
			e.fake.EditComment(repo, issue, c.ID, strings.Replace(c.Body, "Enforce it.", "Skip the checks.", 1))
		}, true},
		{"unsupported schema", func(e *env, c garden.IssueComment) {
			e.fake.EditComment(repo, issue, c.ID, strings.Replace(c.Body, `"schema_version":1`, `"schema_version":2`, 1))
		}, true},
		{"deleted", func(e *env, c garden.IssueComment) { e.fake.DeleteComment(repo, issue, c.ID) }, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			res := e.publish(goodPlan)
			if _, err := e.decide(garden.DecisionChangesRequested, res.Hash, "tighten"); err != nil {
				t.Fatal(err)
			}
			c := e.commentOf(res.CommentID)
			tc.tamper(e, c)
			rv := e.review()
			if _, ok := rv.ByHash(res.Hash); ok == tc.gone {
				t.Fatalf("revision present = %v", ok)
			}
			if rv.Current != nil || rv.Approved != nil {
				t.Fatalf("tampered snapshot is still current or approved: %+v", rv)
			}
			if _, err := e.decide(garden.DecisionRejected, res.Hash, ""); err == nil {
				t.Fatal("a decision on a tampered revision was recorded")
			}
		})
	}
}

func (e *env) commentOf(id int64) garden.IssueComment {
	e.t.Helper()
	cs, _ := e.fake.ListComments(e.ctx, repo, issue)
	for _, c := range cs {
		if c.ID == id {
			return c
		}
	}
	e.t.Fatalf("comment %d not found", id)
	return garden.IssueComment{}
}

func TestRevisionByAnotherAuthorIsShownButNeverCurrent(t *testing.T) {
	e := newEnv(t)
	mine := e.publish(goodPlan)
	body, err := garden.NewPlanRevision(garden.IssueIdentity{Repo: repo, Issue: issue}, Compose(goodPlan+"\n\nExtra.", e.spec, e.base), []string{mine.Hash}, e.spec.Fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	e.fake.AppendCommentAs(repo, issue, "mallory", other, body)
	rv := e.review()
	if rv.Untrusted != 1 || rv.Latest == nil || rv.Latest.Trusted || rv.Latest.Ordinal != 2 {
		t.Fatalf("latest = %+v", rv.Latest)
	}
	// Latest and Current are different revisions: the newest is not the one under review.
	if rv.Current != nil {
		t.Fatalf("an untrusted head left %v current", rv.Current.Ordinal)
	}
	if _, err := e.decide(garden.DecisionRejected, rv.Latest.Hash, ""); !errors.Is(err, ErrNotCurrent) {
		t.Fatalf("decision on an untrusted revision: %v", err)
	}
}

func TestWrongSpecFingerprintInTheDocumentIsUnverifiable(t *testing.T) {
	e := newEnv(t)
	forged := strings.Replace(Compose(goodPlan, e.spec, e.base), "Fingerprint: "+e.spec.Fingerprint, "Fingerprint: "+strings.Repeat("e", 64), 1)
	body, err := garden.NewPlanRevision(garden.IssueIdentity{Repo: repo, Issue: issue}, forged, nil, e.spec.Fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	e.fake.AppendComment(e.ctx, repo, issue, body)
	rv := e.review()
	if rv.Latest == nil || rv.Latest.Consistent || rv.Latest.Reviewable() || rv.Current == nil {
		// Consistency does not remove the head; it only makes it undecidable.
		t.Fatalf("review = %+v", rv)
	}
	if rv.CanDecide(garden.DecisionRejected) || rv.CanDecide(garden.DecisionChangesRequested) {
		t.Fatal("an inconsistent revision can be decided")
	}
	if _, err := e.decide(garden.DecisionChangesRequested, rv.Latest.Hash, "no"); !errors.Is(err, ErrNotCurrent) {
		t.Fatalf("err = %v", err)
	}
}

func TestEnvelopeFingerprintMismatchOnDecisionIsRejected(t *testing.T) {
	e := newEnv(t)
	res := e.publish(goodPlan)
	if _, err := e.svc.Decide(e.ctx, DecisionRequest{Target: e.tgt, RevisionHash: res.Hash, Fingerprint: strings.Repeat("f", 64), Decision: garden.DecisionRejected}); !errors.Is(err, ErrSpecChanged) {
		t.Fatalf("err = %v", err)
	}
	// A decision comment whose envelope names another fingerprint than the revision it decides is not a decision.
	rev, _ := e.review().ByHash(res.Hash)
	forged := rev.Revision
	forged.SourceSpecFingerprint = strings.Repeat("f", 64)
	body, err := garden.NewPlanDecision(garden.IssueIdentity{Repo: repo, Issue: issue}, forged, garden.DecisionRejected, "x")
	if err != nil {
		t.Fatal(err)
	}
	e.fake.AppendComment(e.ctx, repo, issue, body)
	if r, ok := e.review().ByHash(res.Hash); !ok || len(r.Decisions) != 0 || r.State != StateAwaiting {
		t.Fatalf("forged decision counted: %+v", r)
	}
}

func TestCurrentLatestAndApprovedAreSeparate(t *testing.T) {
	e := newEnv(t)
	r1 := e.publish(goodPlan)
	// Gate two (not built yet) will record this comment: here it is posted directly to prove the reading side.
	rev1, _ := e.review().ByHash(r1.Hash)
	body, err := garden.NewPlanDecision(garden.IssueIdentity{Repo: repo, Issue: issue}, rev1.Revision, garden.DecisionApproved, "approved r1")
	if err != nil {
		t.Fatal(err)
	}
	e.fake.AppendComment(e.ctx, repo, issue, body)
	rv := e.review()
	if rv.Approved == nil || rv.Approved.Hash != r1.Hash || rv.Current == nil || rv.Current.Hash != r1.Hash {
		t.Fatalf("after approval: %+v", rv)
	}
	if rv.CanDecide(garden.DecisionRejected) {
		t.Fatal("an approved revision can be rejected here")
	}
	// A newer revision becomes current and latest; the approved snapshot stays what it was.
	r2 := e.publish(goodPlan + "\n\n## Notes\nSecond pass.")
	rv = e.review()
	if rv.Latest.Hash != r2.Hash || rv.Current.Hash != r2.Hash || rv.Approved == nil || rv.Approved.Hash != r1.Hash || !rv.Approved.Superseded {
		t.Fatalf("after r2: latest %d current %d approved %v", rv.Latest.Ordinal, rv.Current.Ordinal, rv.Approved)
	}
	// Editing the approved snapshot afterwards is detected: nothing is approved any more.
	e.fake.EditComment(repo, issue, r1.CommentID, e.commentOf(r1.CommentID).Body+"\nsneaky")
	rv = e.review()
	if rv.Approved != nil {
		t.Fatal("an edited approved snapshot still counts")
	}
	if rv.Unavailable == "" {
		t.Fatal("tampering did not make downstream actions unavailable")
	}
}

func TestApprovalOfAnOlderSpecificationAcceptsNothing(t *testing.T) {
	e := newEnv(t)
	r1 := e.publish(goodPlan)
	rev1, _ := e.review().ByHash(r1.Hash)
	body, _ := garden.NewPlanDecision(garden.IssueIdentity{Repo: repo, Issue: issue}, rev1.Revision, garden.DecisionApproved, "ok")
	e.fake.AppendComment(e.ctx, repo, issue, body)
	e.approveSpec(goodSpec+"\n\n## Constraints\nNo new deps.", e.spec.RevisionHash)
	rv := e.review()
	r, ok := rv.ByHash(r1.Hash)
	if !ok || !r.Stale || r.State != StateApproved || rv.Approved == nil || rv.Ready || !rv.StaleApproval || rv.Current != nil || rv.Latest == nil {
		t.Fatalf("old approval: stale=%v state=%s approved=%v current=%v", r.Stale, r.State, rv.Approved, rv.Current)
	}
}

func TestRequestChangesRecordsReasonAndKeepsHistory(t *testing.T) {
	e := newEnv(t)
	r1 := e.publish(goodPlan)
	if _, err := e.decide(garden.DecisionChangesRequested, r1.Hash, "  "); !errors.Is(err, ErrReasonRequired) {
		t.Fatalf("blank reason: %v", err)
	}
	if _, err := e.decide(garden.DecisionChangesRequested, r1.Hash, "bonsai:garden/ pls"); !errors.Is(err, ErrInvalidReason) {
		t.Fatalf("marker in reason: %v", err)
	}
	res, err := e.decide(garden.DecisionChangesRequested, r1.Hash, "Cover the counter race.")
	if err != nil || res.Replayed || res.Ordinal != 1 {
		t.Fatalf("decide = %+v, %v", res, err)
	}
	rv := e.review()
	r := rv.Revisions[0]
	if r.State != StateChangesRequested || r.Reason() != "Cover the counter race." || len(r.Decisions) != 1 || rv.Current == nil || rv.Approved != nil {
		t.Fatalf("revision = state %s reason %q decisions %d", r.State, r.Reason(), len(r.Decisions))
	}
	if rv.CanDecide(garden.DecisionChangesRequested) || !rv.CanDecide(garden.DecisionRejected) {
		t.Fatal("decision availability after changes were requested")
	}
	// The same decision again is the recorded one, not a second write.
	before := e.writes()
	if again, err := e.decide(garden.DecisionChangesRequested, r1.Hash, "Cover the counter race."); err != nil || !again.Replayed || again.CommentID != res.CommentID || e.writes() != before {
		t.Fatalf("replay = %+v, %v", again, err)
	}
	// The next revision supersedes the reviewed one; both stay, with the reason.
	r2 := e.publish(goodPlan + "\n\nCounter is now atomic.")
	rv = e.review()
	if len(rv.Revisions) != 2 || !rv.Revisions[0].Superseded || rv.Revisions[0].Reason() != "Cover the counter race." || rv.Current.Hash != r2.Hash || rv.Current.State != StateAwaiting {
		t.Fatalf("history = %+v", rv.Revisions)
	}
	// The reviewed revision is no longer the one under review, so deciding it again is refused.
	if _, err := e.decide(garden.DecisionRejected, r1.Hash, ""); !errors.Is(err, ErrNotCurrent) {
		t.Fatalf("decision on a superseded revision: %v", err)
	}
	// Decision text on GitHub names who, what and why; the reason is covered by the record hash.
	body := e.commentOf(res.CommentID).Body
	if !strings.Contains(body, "Changes requested on plan r1 by tiago") || !strings.Contains(body, "Cover the counter race.") {
		t.Fatalf("comment = %q", body)
	}
	e.fake.EditComment(repo, issue, res.CommentID, strings.Replace(body, "Cover the counter race.", "Looks fine.", 1))
	if rv := e.review(); rv.Unavailable == "" {
		t.Fatal("an edited decision did not surface as an integrity problem")
	}
}

func TestRejectReturnsToApprovedWithNoAcceptedPlan(t *testing.T) {
	e := newEnv(t)
	r1 := e.publish(goodPlan)
	if _, err := e.decide(garden.DecisionRejected, r1.Hash, ""); err != nil {
		t.Fatal(err)
	}
	rv := e.review()
	if rv.Gate.State != approve.StateApproved || rv.Unavailable != "" {
		t.Fatalf("gate = %s, unavailable %q", rv.Gate.State, rv.Unavailable)
	}
	if rv.Current != nil || rv.Approved != nil || rv.Latest == nil || rv.Latest.State != StateRejected || len(rv.Revisions) != 1 {
		t.Fatalf("review = %+v", rv)
	}
	if rv.CanDecide(garden.DecisionRejected) || !rv.CanPublish() {
		t.Fatal("after a rejection a new plan is the only way forward")
	}
	r2 := e.publish(goodPlan + "\n\nTake two.")
	if rv := e.review(); rv.Current == nil || rv.Current.Hash != r2.Hash || !rv.Revisions[0].Superseded {
		t.Fatalf("after a new revision: %+v", rv.Current)
	}
}

func TestApprovingExactPairYieldsReady(t *testing.T) {
	e := newEnv(t)
	r1 := e.publish(goodPlan)
	before := e.writes()
	if _, err := e.decide(garden.DecisionApproved, r1.Hash, "ship it"); err != nil || e.writes() != before+1 {
		t.Fatalf("err = %v, writes %d -> %d", err, before, e.writes())
	}
	if rv := e.review(); !rv.Ready || rv.Approved == nil {
		t.Fatalf("not Ready: %+v", rv)
	}
}

func TestRemoteProseNeverBreaksOrTriggersAnything(t *testing.T) {
	e := newEnv(t)
	r1 := e.publish(goodPlan)
	before := e.writes()
	e.fake.AppendCommentAs(repo, issue, "reviewer", other, "Changes requested: please cover the race.")
	e.fake.AppendCommentAs(repo, issue, "reviewer", other, "<!-- looks like a record but is not -->\nchanges requested")
	rv := e.review()
	if rv.NewerComments != 2 || rv.Unavailable != "" || rv.Current == nil || rv.Current.State != StateAwaiting || rv.Gate.State != approve.StateApproved {
		t.Fatalf("review = newer %d unavailable %q", rv.NewerComments, rv.Unavailable)
	}
	// Nothing was written by reading, and the reviewer can still act locally on the exact revision.
	if e.writes() != before {
		t.Fatalf("writes = %d, was %d", e.writes(), before)
	}
	if _, err := e.decide(garden.DecisionChangesRequested, r1.Hash, "Cover the race."); err != nil {
		t.Fatal(err)
	}
}

func TestStalePreviewsAreRefusedWithoutWriting(t *testing.T) {
	e := newEnv(t)
	e.publish(goodPlan)
	p, err := e.svc.PreviewPublish(e.ctx, e.tgt, e.att.ID, goodPlan+"\n\nVariant A.")
	if err != nil {
		t.Fatal(err)
	}
	// Another revision lands (from a phone) between the preview and the confirmation.
	rv := e.review()
	body, _ := garden.NewPlanRevision(garden.IssueIdentity{Repo: repo, Issue: issue}, Compose(goodPlan+"\n\nVariant B.", e.spec, e.base), []string{rv.Current.Hash}, e.spec.Fingerprint)
	e.fake.AppendComment(e.ctx, repo, issue, body)
	before := e.writes()
	if _, err := e.svc.Publish(e.ctx, p); !errors.Is(err, ErrHeadsMoved) || e.writes() != before {
		t.Fatalf("err = %v, writes %d -> %d", err, before, e.writes())
	}
	// A decision names the hash it was made about; it cannot land on "whatever is latest".
	r1, _ := e.review().ByHash(rv.Current.Hash)
	if _, err := e.svc.Decide(e.ctx, DecisionRequest{Target: e.tgt, RevisionHash: r1.Hash, Fingerprint: r1.SourceSpecFingerprint, Decision: garden.DecisionRejected}); !errors.Is(err, ErrNotCurrent) {
		t.Fatalf("decision on a superseded revision: %v", err)
	}
	if _, err := e.svc.Decide(e.ctx, DecisionRequest{Target: e.tgt, RevisionHash: strings.Repeat("0", 64), Fingerprint: r1.SourceSpecFingerprint, Decision: garden.DecisionRejected}); !errors.Is(err, ErrNoRevision) {
		t.Fatalf("unknown hash: %v", err)
	}
}

func TestSpecChangeMakesPublishedPlansStale(t *testing.T) {
	e := newEnv(t)
	r1 := e.publish(goodPlan)
	e.postSpec(goodSpec+"\n\n## Constraints\nNo new deps.", e.spec.RevisionHash)
	rv := e.review()
	r, _ := rv.ByHash(r1.Hash)
	if !r.Stale || rv.Current != nil || rv.Unavailable == "" {
		t.Fatalf("stale=%v current=%v unavailable=%q", r.Stale, rv.Current, rv.Unavailable)
	}
	if _, err := e.decide(garden.DecisionRejected, r1.Hash, ""); err == nil {
		t.Fatal("decided a plan of a specification that is no longer approved")
	}
}

func TestNeedsAnActor(t *testing.T) {
	e := newEnv(t)
	e.tgt.Viewer = ""
	if _, err := e.svc.Review(e.ctx, e.tgt); !errors.Is(err, ErrNoActor) {
		t.Fatalf("err = %v", err)
	}
}

func TestDuplicateTextIsNotASecondRevision(t *testing.T) {
	e := newEnv(t)
	r1 := e.publish(goodPlan)
	r2 := e.publish(goodPlan + "\n\nAddendum.")
	if p, err := e.svc.PreviewPublish(e.ctx, e.tgt, e.att.ID, goodPlan+"\n\nAddendum."); err != nil || p.Replay != r2.CommentID {
		t.Fatalf("current text: %+v, %v", p.Replay, err)
	}
	if _, err := e.svc.PreviewPublish(e.ctx, e.tgt, e.att.ID, goodPlan); !errors.Is(err, ErrMatchesEarlier) {
		t.Fatalf("earlier text: %v", err)
	}
	if r1.Hash == r2.Hash {
		t.Fatal("distinct plans share a hash")
	}
}

func TestDecisionByAnotherAccountIsNotADecision(t *testing.T) {
	e := newEnv(t)
	r1 := e.publish(goodPlan)
	rev, _ := e.review().ByHash(r1.Hash)
	body, err := garden.NewPlanDecision(garden.IssueIdentity{Repo: repo, Issue: issue}, rev.Revision, garden.DecisionRejected, "go away")
	if err != nil {
		t.Fatal(err)
	}
	e.fake.AppendCommentAs(repo, issue, "mallory", other, body)
	rv := e.review()
	if r, _ := rv.ByHash(r1.Hash); len(r.Decisions) != 0 || r.State != StateAwaiting {
		t.Fatalf("foreign decision counted: %+v", r.Decisions)
	}
	if rv.Unavailable == "" {
		t.Fatal("a forged record did not stop downstream actions")
	}
}
