package approve

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	gardenfake "github.com/Tiago-0liveira/bonsai/internal/core/garden/fake"
)

const (
	repo   = garden.RepoID(9)
	issue  = garden.IssueID(7)
	viewer = "fake-authenticated-id"
)

const goodSpec = "## Summary\nCap spend.\n\n## Requirements\nA monthly ceiling.\n\n## Acceptance criteria\nRequests stop at the ceiling.\n\n## Open questions\nNone"

type harness struct {
	t    *testing.T
	fake *gardenfake.Fake
	svc  *Service
	tgt  Target
	ctx  context.Context
	now  time.Time
}

type failStage struct {
	Writer
	err error
}

func (f failStage) UpdateStage(context.Context, garden.RepoID, garden.IssueID, []string, garden.Stage, garden.Stage) error {
	return f.err
}

func newHarness(t *testing.T, labels ...string) *harness {
	t.Helper()
	f := gardenfake.New()
	body, err := garden.CaptureBody(viewer, strings.Repeat("a", 32), "Teams want limits.")
	if err != nil {
		t.Fatal(err)
	}
	if len(labels) == 0 {
		labels = []string{garden.ManagedLabel, garden.FormatTypeLabel(garden.TypeBug), garden.FormatStageLabel(garden.StageDraft)}
	}
	f.SeedIssue(garden.Issue{Repo: repo, ID: issue, Number: 7, Title: "Budget", Body: body, Metadata: garden.BodyMetadata(body), Labels: labels})
	h := &harness{t: t, fake: f, ctx: context.Background(), now: time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)}
	h.svc, err = New(Deps{Reader: f, Writer: f, Now: func() time.Time { return h.now }})
	if err != nil {
		t.Fatal(err)
	}
	h.tgt = Target{Repo: repo, Issue: issue, Viewer: viewer, Login: "tiago"}
	return h
}

// spec appends a specification revision as the viewer.
func (h *harness) spec(content string, parents ...string) garden.IssueComment {
	h.t.Helper()
	body, err := garden.NewSpecRevision(garden.IssueIdentity{Repo: repo, Issue: issue}, content, parents)
	if err != nil {
		h.t.Fatal(err)
	}
	c, err := h.fake.AppendComment(h.ctx, repo, issue, body)
	if err != nil {
		h.t.Fatal(err)
	}
	return c
}

func (h *harness) snap() snapshot {
	h.t.Helper()
	st, err := h.svc.read(h.ctx, h.tgt)
	if err != nil {
		h.t.Fatal(err)
	}
	return st
}

func (h *harness) assess() Assessment { st := h.snap(); return Assess(st.issue, st.v) }

func (h *harness) preview() Preview {
	h.t.Helper()
	p, err := h.svc.Preview(h.ctx, h.tgt)
	if err != nil {
		h.t.Fatal(err)
	}
	return p
}

func (h *harness) validate() Result {
	h.t.Helper()
	r, err := h.svc.Validate(h.ctx, h.preview())
	if err != nil {
		h.t.Fatalf("validate: %v", err)
	}
	return r
}

func (h *harness) log() string { return strings.Join(h.fake.WriteLog(), "|") }

func (h *harness) countLog(prefix string) int {
	n := 0
	for _, l := range h.fake.WriteLog() {
		if strings.HasPrefix(l, prefix) {
			n++
		}
	}
	return n
}

func (h *harness) approvalComment(rev garden.Revision, human string) string {
	h.t.Helper()
	body, err := garden.NewApproval(garden.IssueIdentity{Repo: repo, Issue: issue}, rev, human)
	if err != nil {
		h.t.Fatal(err)
	}
	return body
}

func TestDraftToApprovedRecordsTheGateAndLaunchesNothing(t *testing.T) {
	h := newHarness(t)
	head := h.spec(goodSpec)
	p := h.preview()
	if p.State != StateDraft || !p.CanValidate() || p.Head.CommentID != head.ID || p.Ordinal != 1 || p.From != garden.StageDraft ||
		p.Fingerprint != garden.ContentHash(goodSpec) || len(p.Steps) != 2 || p.Type != garden.TypeBug {
		t.Fatalf("preview = %+v", p)
	}
	if h.countLog("comment:")+h.countLog("labels:")+h.countLog("metadata:") != 1 {
		t.Fatalf("preview wrote: %s", h.log())
	}
	res, err := h.svc.Validate(h.ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Done) != 2 || res.RevisionHash != p.Head.Hash {
		t.Fatalf("result = %+v", res)
	}
	// Only the approval comment and stage projection were written.
	if got, want := h.log(), "comment:7|comment:7|labels:7:bonsai:managed,bonsai:type/bug,bonsai:stage/ready"; got != want {
		t.Fatalf("writes = %s, want %s", got, want)
	}
	st := h.snap()
	a := Assess(st.issue, st.v)
	if a.State != StateApproved || !a.DownstreamAvailable() || a.CanValidate() || a.Approval == nil {
		t.Fatalf("after = %+v", a)
	}
	if a.Approval.AuthorID != viewer || a.Approval.CreatedAt.IsZero() || a.Approval.RevisionHash != p.Head.Hash {
		t.Fatalf("approval record = %+v", a.Approval)
	}
	// The comment is the explicit record: actor, revision reference and the human statement.
	var found bool
	cs, _ := h.fake.ListComments(h.ctx, repo, issue)
	for _, c := range cs {
		if strings.Contains(c.Body, `"kind":"approval"`) {
			found = true
			for _, want := range []string{"approved by tiago", p.Head.Hash, "2026-10-20T12:00:00Z", "starts no planner"} {
				if !strings.Contains(c.Body, want) {
					t.Errorf("approval comment lacks %q:\n%s", want, c.Body)
				}
			}
		}
	}
	if !found {
		t.Fatal("no approval comment")
	}
}

func TestValidateIsIdempotent(t *testing.T) {
	h := newHarness(t)
	h.spec(goodSpec)
	h.validate()
	writes := h.log()
	res, err := h.svc.Validate(h.ctx, h.preview()) // an approved preview has nothing to do
	if err != nil || len(res.Done) != 0 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if h.log() != writes {
		t.Fatalf("second validate wrote: %s", h.log())
	}
}

func TestInterruptedProjectionIsFinishedWithoutASecondApproval(t *testing.T) {
	h := newHarness(t)
	h.spec(goodSpec)
	svc := h.svc
	svc.d.Writer = failStage{Writer: h.fake, err: errors.New("boom")}
	_, err := svc.Validate(h.ctx, h.preview())
	if !errors.Is(err, ErrProjection) {
		t.Fatalf("err = %v", err)
	}
	a := h.assess()
	if a.State != StateFinishing || a.DownstreamAvailable() || !a.CanValidate() || a.Approval == nil {
		t.Fatalf("half-done state = %+v", a)
	}
	// Half-done work must stay a draft: acceptance metadata and the label were never written.
	st := h.snap()
	if lane := garden.Lane(garden.Classify(st.issue.Labels), mustMeta(t, st.issue), st.v, nil); lane.Lane != garden.LaneDraft {
		t.Fatalf("lane = %+v", lane)
	}
	h.svc.d.Writer = h.fake
	res := h.validate()
	if len(res.Done) != 1 || h.countLog("comment:") != 2 {
		t.Fatalf("res=%+v log=%s", res, h.log())
	}
	if h.assess().State != StateApproved {
		t.Fatal("not approved after resume")
	}
}

func mustMeta(t *testing.T, i garden.Issue) garden.Metadata {
	t.Helper()
	m, err := garden.DecodeMetadata(i.Metadata)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestPreviewOfAMovedHeadIsRefusedWithoutWriting(t *testing.T) {
	h := newHarness(t)
	first := h.spec(goodSpec)
	p := h.preview()
	h.spec(goodSpec+"\n\n## Constraints\nNo new service.", garden.ContentHash(goodSpec))
	before := h.log()
	_, err := h.svc.Validate(h.ctx, p)
	if !errors.Is(err, ErrStale) || h.log() != before {
		t.Fatalf("err=%v log=%s", err, h.log())
	}
	if h.preview().Head.CommentID == first.ID {
		t.Fatal("preview kept the old head")
	}
}

func TestHeadMovingWhileTheApprovalIsWrittenProjectsNothing(t *testing.T) {
	h := newHarness(t)
	h.spec(goodSpec)
	p := h.preview()
	h.fake.AfterNextAppend(func() {
		body, _ := garden.NewSpecRevision(garden.IssueIdentity{Repo: repo, Issue: issue}, goodSpec+"\n\n## Constraints\nNone.", []string{p.Head.Hash})
		h.fake.AppendCommentAs(repo, issue, "mobile", viewer, body)
	})
	res, err := h.svc.Validate(h.ctx, p)
	if !errors.Is(err, ErrHeadMoved) || res.ApprovalCommentID == 0 {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if h.countLog("metadata:") != 0 || h.countLog("labels:") != 0 {
		t.Fatalf("projected an approval of an old revision: %s", h.log())
	}
	a := h.assess()
	if a.State != StateStale || a.CanValidate() != true || len(a.Records) != 1 || a.Records[0].State != RecordSuperseded {
		t.Fatalf("after = %+v", a)
	}
}

func TestAnApprovalLandingBetweenPreviewAndValidateIsReused(t *testing.T) {
	h := newHarness(t)
	head := h.spec(goodSpec)
	p := h.preview()
	rev := *p.Head
	if rev.CommentID != head.ID {
		t.Fatal("wrong head")
	}
	// The same account approves from a phone while the preview is open.
	h.fake.AppendCommentAs(repo, issue, "tiago", viewer, h.approvalComment(rev, "Approved from my phone."))
	res, err := h.svc.Validate(h.ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	// The write log holds the specification revision only: the phone approval bypasses it, and Validate must
	// not add a second approval.
	if len(res.Done) != 1 || h.countLog("comment:") != 1 {
		t.Fatalf("res=%+v log=%s", res, h.log())
	}
	if h.assess().State != StateApproved {
		t.Fatal("not approved")
	}
}

func TestRemoteApprovalNeedsProjectionAndVerifiedAuthorship(t *testing.T) {
	h := newHarness(t)
	rev := h.assess()
	if rev.State != StateDraft {
		t.Fatal(rev.State)
	}
	h.spec(goodSpec)
	head := *h.assess().Head
	// A collaborator's approval is not from the trusted account: it fails verification and blocks the gate.
	h.fake.AppendCommentAs(repo, issue, "mallory", "666", h.approvalComment(head, "LGTM"))
	a := h.assess()
	if a.State != StateAttention || a.CanValidate() || a.DownstreamAvailable() || a.Blockers[0].Kind != BlockIntegrity || !strings.Contains(a.Blockers[0].Message, "wrong-author") {
		t.Fatalf("unauthorized approval = %+v", a)
	}
	if _, err := h.svc.Validate(h.ctx, h.preview()); !errors.Is(err, ErrBlocked) {
		t.Fatalf("err = %v", err)
	}
}

func TestClaimsWithoutVerifiedApprovalNeverCountAsApproved(t *testing.T) {
	ready := []string{garden.ManagedLabel, garden.FormatTypeLabel(garden.TypeFeature), garden.FormatStageLabel(garden.StageReady)}
	cases := []struct {
		name  string
		setup func(*harness)
		want  State
	}{
		{"prose claim in the body", func(h *harness) {
			i, _ := h.fake.GetIssue(h.ctx, repo, issue)
			h.fake.SimulateConcurrentEdit(repo, issue, i.Body+"\n\nApproved by the team. LGTM, ship it.")
			h.spec(goodSpec)
		}, StateDraft},
		{"ready label only", func(h *harness) { h.spec(goodSpec) }, StateUnverified},
		{"accepted metadata without a record", func(h *harness) {
			h.spec(goodSpec)
			head := *h.assess().Head
			m := mustMeta(t, h.snap().issue)
			m.Accepted, m.AcceptedSpecHash, m.SpecFingerprint = true, head.Hash, head.SourceSpecFingerprint
			next, _ := garden.EncodeMetadata(m)
			if err := h.fake.ReplaceMetadata(h.ctx, repo, issue, h.snap().issue.Metadata, next); err != nil {
				t.Fatal(err)
			}
		}, StateUnverified},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var h *harness
			if strings.Contains(tc.name, "label") || strings.Contains(tc.name, "metadata") {
				h = newHarness(t, ready...)
			} else {
				h = newHarness(t)
			}
			tc.setup(h)
			a := h.assess()
			if a.State != tc.want || a.DownstreamAvailable() {
				t.Fatalf("state = %s (%s), downstream=%v", a.State, a.Reason, a.DownstreamAvailable())
			}
			if tc.want == StateUnverified && a.Reason == "" {
				t.Fatal("attention state without a reason")
			}
		})
	}
}

func TestLabelOnlyConflictsAreAttentionAndNothingIsCorrected(t *testing.T) {
	h := newHarness(t, garden.ManagedLabel, garden.FormatTypeLabel(garden.TypeBug), garden.FormatStageLabel(garden.StageDraft), garden.FormatStageLabel(garden.StageReady))
	h.spec(goodSpec)
	a := h.assess()
	if a.State != StateAttention || a.CanValidate() || a.DownstreamAvailable() || a.Blockers[0].Kind != BlockLabels {
		t.Fatalf("a = %+v", a)
	}
	before := h.log()
	if _, err := h.svc.Validate(h.ctx, h.preview()); !errors.Is(err, ErrBlocked) || h.log() != before {
		t.Fatalf("err=%v log=%s", err, h.log())
	}
}

func TestBlockersWhatMustBeFixedBeforeApproval(t *testing.T) {
	cases := []struct {
		name    string
		content string
		stage   garden.Stage
		want    []BlockerKind
	}{
		{"empty requirements and criteria", "## Summary\nOnly a summary.", garden.StageDraft, []BlockerKind{BlockRequirement, BlockRequirement}},
		{"open questions", "## Summary\nS\n\n## Requirements\nR\n\n## Acceptance criteria\nA\n\n## Open questions\n- Who pays?\n- [x] Which month?\n- [ ] Rollover?", garden.StageDraft, []BlockerKind{BlockQuestion, BlockQuestion}},
		{"already running", goodSpec, garden.StageInProgress, []BlockerKind{BlockStage}},
		{"complete and clean", goodSpec, garden.StageDraft, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, garden.ManagedLabel, garden.FormatTypeLabel(garden.TypeFeature), garden.FormatStageLabel(tc.stage))
			h.spec(tc.content)
			a := h.assess()
			var got []BlockerKind
			for _, b := range a.Blockers {
				got = append(got, b.Kind)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("blockers = %v", a.Blockers)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("blockers = %v", a.Blockers)
				}
			}
			if (len(tc.want) == 0) != a.CanValidate() {
				t.Fatalf("CanValidate = %v", a.CanValidate())
			}
		})
	}
}

func TestOpenQuestions(t *testing.T) {
	for in, want := range map[string]int{"": 0, "None": 0, "none.": 0, "n/a": 0, "- Who pays?": 1, "* a\n* b": 2, "- [x] done\n- [ ] open": 1, "Is it ok?\n\n\nAnd this?": 2} {
		if got := OpenQuestions(in); len(got) != want {
			t.Errorf("OpenQuestions(%q) = %v, want %d", in, got, want)
		}
	}
	if got := OpenQuestions("- [ ] Rollover?"); got[0] != "Rollover?" {
		t.Errorf("marker not stripped: %q", got)
	}
}

func TestNoSpecificationRevisionBlocks(t *testing.T) {
	h := newHarness(t)
	a := h.assess()
	if a.State != StateDraft || a.CanValidate() || a.Blockers[0].Kind != BlockNoSpec {
		t.Fatalf("a = %+v", a)
	}
}

func TestCompetingHeadsBlock(t *testing.T) {
	h := newHarness(t)
	h.spec(goodSpec)
	h.fake.AppendCommentAs(repo, issue, "phone", viewer, mustSpec(t, goodSpec+"\n\nMore."))
	a := h.assess()
	if a.State != StateAttention || a.Blockers[0].Kind != BlockCompetingHeads || a.CanValidate() {
		t.Fatalf("a = %+v", a)
	}
}

func mustSpec(t *testing.T, content string, parents ...string) string {
	t.Helper()
	b, err := garden.NewSpecRevision(garden.IssueIdentity{Repo: repo, Issue: issue}, content, parents)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestTamperedRecordsBlockTheGate(t *testing.T) {
	cases := []struct {
		name   string
		tamper func(h *harness, specID, approvalID int64)
		code   string
	}{
		{"approval text edited", func(h *harness, _, approvalID int64) {
			cs, _ := h.fake.ListComments(h.ctx, repo, issue)
			for _, c := range cs {
				if c.ID == approvalID {
					h.fake.EditComment(repo, issue, approvalID, c.Body+"\nApproved (edited)")
				}
			}
		}, "edited-hash-mismatch"},
		{"specification edited after approval", func(h *harness, specID, _ int64) {
			cs, _ := h.fake.ListComments(h.ctx, repo, issue)
			for _, c := range cs {
				if c.ID == specID {
					h.fake.EditComment(repo, issue, specID, c.Body+"\nsneaky addition")
				}
			}
		}, "edited-hash-mismatch"},
		{"specification deleted", func(h *harness, specID, _ int64) { h.fake.DeleteComment(repo, issue, specID) }, "deleted-referenced-comment"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			spec := h.spec(goodSpec)
			res := h.validate()
			tc.tamper(h, spec.ID, res.ApprovalCommentID)
			a := h.assess()
			if a.State == StateApproved || a.DownstreamAvailable() || a.CanValidate() {
				t.Fatalf("tampered log still approved: %+v", a)
			}
			if a.State != StateAttention || a.Blockers[0].Kind != BlockIntegrity || !strings.Contains(a.Blockers[0].Message, tc.code) {
				t.Fatalf("a = %+v (want %s)", a, tc.code)
			}
		})
	}
}

func TestDeletedApprovalLeavesAnUnverifiedClaim(t *testing.T) {
	h := newHarness(t)
	h.spec(goodSpec)
	res := h.validate()
	if !h.fake.DeleteComment(repo, issue, res.ApprovalCommentID) {
		t.Fatal("delete")
	}
	a := h.assess()
	if a.State != StateUnverified || a.DownstreamAvailable() || !a.CanValidate() {
		t.Fatalf("a = %+v", a)
	}
	// Acceptance without a valid approval is Attention, never Work.
	st := h.snap()
	if lane := garden.Lane(garden.Classify(st.issue.Labels), mustMeta(t, st.issue), st.v, nil); lane.Lane != garden.LaneAttention {
		t.Fatalf("lane = %+v", lane)
	}
}

func TestSupersededApprovalIsRetainedAndReapprovalIsExplicit(t *testing.T) {
	h := newHarness(t)
	first := h.spec(goodSpec)
	h.validate()
	second := h.spec(goodSpec+"\n\n## Constraints\nNo new service.", garden.ContentHash(goodSpec))
	a := h.assess()
	if a.State != StateStale || a.DownstreamAvailable() || a.Reason == "" || a.Head.CommentID != second.ID || a.Ordinal != 2 {
		t.Fatalf("a = %+v", a)
	}
	if len(a.Records) != 1 || a.Records[0].State != RecordSuperseded || a.Records[0].RevisionHash != garden.ContentHash(goodSpec) {
		t.Fatalf("records = %+v", a.Records)
	}
	_ = first
	// The accepted item left Work: it is Attention until the new head is validated explicitly.
	st := h.snap()
	if lane := garden.Lane(garden.Classify(st.issue.Labels), mustMeta(t, st.issue), st.v, nil); lane.Lane != garden.LaneAttention {
		t.Fatalf("lane = %+v", lane)
	}
	if !a.CanValidate() || len(h.preview().Steps) != 1 {
		t.Fatalf("steps = %v", h.preview().Steps)
	}
	h.validate()
	a = h.assess()
	if a.State != StateApproved || len(a.Records) != 2 || a.Records[0].State != RecordSuperseded || a.Records[1].State != RecordValid {
		t.Fatalf("after reapproval = %+v", a)
	}
}

func TestRevokedApprovalIsRetainedAndBlocksDownstream(t *testing.T) {
	h := newHarness(t)
	h.spec(goodSpec)
	res := h.validate()
	body, err := garden.EncodeCommentEnvelope(garden.CommentEnvelope{Kind: garden.CommentRevocation, RepoID: repo, IssueID: issue, TargetCommentID: res.ApprovalCommentID}, "Revoked.")
	if err != nil {
		t.Fatal(err)
	}
	rev := h.fake.AppendCommentAs(repo, issue, "tiago", viewer, body)
	a := h.assess()
	if a.State != StateRevoked || a.DownstreamAvailable() || a.Approval != nil || len(a.Records) != 1 || a.Records[0].State != RecordRevoked || a.Reason == "" {
		t.Fatalf("a = %+v", a)
	}
	// Removing the revocation does not restore the approval once metadata remembers it.
	m := mustMeta(t, h.snap().issue)
	m.Revocations = []garden.RevocationRecord{{ApprovalCommentID: res.ApprovalCommentID, RevocationCommentID: rev.ID}}
	next, _ := garden.EncodeMetadata(m)
	if err := h.fake.ReplaceMetadata(h.ctx, repo, issue, h.snap().issue.Metadata, next); err != nil {
		t.Fatal(err)
	}
	h.fake.DeleteComment(repo, issue, rev.ID)
	if a := h.assess(); a.State != StateAttention || a.DownstreamAvailable() {
		t.Fatalf("deleted revocation = %+v", a)
	}
}

func TestPureAssessmentOfUnreadableState(t *testing.T) {
	good := garden.Issue{Repo: repo, ID: issue, Labels: []string{garden.ManagedLabel, garden.FormatTypeLabel(garden.TypeBug), garden.FormatStageLabel(garden.StageDraft)}}
	cases := map[string]struct {
		issue garden.Issue
		v     garden.Verification
		kind  BlockerKind
	}{
		"incomplete log":   {good, garden.Verification{Complete: false}, BlockIncomplete},
		"invalid metadata": {good, garden.Verification{Complete: true}, BlockMetadata},
	}
	for name, tc := range cases {
		a := Assess(tc.issue, tc.v)
		if a.State != StateAttention || a.CanValidate() || a.DownstreamAvailable() || len(a.Blockers) == 0 || a.Blockers[0].Kind != tc.kind {
			t.Errorf("%s: %+v", name, a)
		}
	}
}

func TestNoActorNoApproval(t *testing.T) {
	h := newHarness(t)
	h.spec(goodSpec)
	h.tgt.Viewer = ""
	if _, err := h.svc.Preview(h.ctx, h.tgt); !errors.Is(err, ErrNoActor) {
		t.Fatalf("err = %v", err)
	}
	if _, err := h.svc.Validate(h.ctx, Preview{Target: h.tgt}); !errors.Is(err, ErrNoActor) {
		t.Fatalf("err = %v", err)
	}
}

func TestAnotherAccountCannotUseSomeoneElsesApproval(t *testing.T) {
	h := newHarness(t)
	h.spec(goodSpec)
	h.validate()
	h.tgt.Viewer = "999" // a different authenticated account reads the same Issue
	a := h.assess()
	if a.State == StateApproved || a.DownstreamAvailable() {
		t.Fatalf("approval by another account accepted: %+v", a)
	}
}

type failAppend struct {
	Writer
	err error
}

func (f failAppend) AppendComment(context.Context, garden.RepoID, garden.IssueID, string) (garden.IssueComment, error) {
	return garden.IssueComment{}, f.err
}

func TestFailedAppendIsReportedAsMaybeWritten(t *testing.T) {
	h := newHarness(t)
	h.spec(goodSpec)
	p := h.preview()
	h.svc.d.Writer = failAppend{h.fake, garden.ErrOffline}
	_, err := h.svc.Validate(h.ctx, p)
	if !errors.Is(err, ErrAppendFailed) || !errors.Is(err, garden.ErrOffline) || h.countLog("metadata:") != 0 {
		t.Fatalf("err=%v log=%s", err, h.log())
	}
	if h.assess().State != StateDraft {
		t.Fatal("a failed append changed the gate")
	}
}

func TestUnreadableLogWritesNothing(t *testing.T) {
	h := newHarness(t)
	h.spec(goodSpec)
	p := h.preview()
	before := h.log()
	h.fake.FailNext(garden.ErrOffline)
	if _, err := h.svc.Validate(h.ctx, p); !errors.Is(err, garden.ErrOffline) || h.log() != before {
		t.Fatalf("err=%v log=%s", err, h.log())
	}
}

func TestConcurrentBodyAndLabelsAreNotOverwritten(t *testing.T) {
	for _, changeLabels := range []bool{false, true} {
		t.Run(fmt.Sprint(changeLabels), func(t *testing.T) {
			h := newHarness(t)
			h.spec(goodSpec)
			p := h.preview()
			h.fake.AfterNextAppend(func() {
				i, _ := h.fake.GetIssue(h.ctx, repo, issue)
				i.Body += "\nConcurrent mobile edit."
				if changeLabels {
					i.Labels = []string{garden.ManagedLabel, garden.FormatTypeLabel(garden.TypeFeature), garden.FormatStageLabel(garden.StageBlocked)}
				}
				h.fake.SeedIssue(i)
			})
			other, err := New(Deps{Reader: h.fake, Writer: h.fake})
			if err != nil {
				t.Fatal(err)
			}
			_, err = other.Validate(h.ctx, p)
			if changeLabels && !errors.Is(err, garden.ErrMetadataChanged) {
				t.Fatalf("conflict = %v", err)
			}
			if !changeLabels && err != nil {
				t.Fatal(err)
			}
			i, _ := h.fake.GetIssue(h.ctx, repo, issue)
			if !strings.HasSuffix(i.Body, "Concurrent mobile edit.") || h.countLog("metadata:") != 0 {
				t.Fatalf("body overwritten or patched: %q; %s", i.Body, h.log())
			}
			if changeLabels && garden.Classify(i.Labels).Stage != garden.StageBlocked {
				t.Fatalf("labels overwritten: %v", i.Labels)
			}
		})
	}
}
