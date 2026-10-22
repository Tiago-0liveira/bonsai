package garden_test

import (
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
)

func event(t *testing.T, id int64, actor string, e garden.CommentEnvelope, text string) garden.Comment {
	t.Helper()
	if e.Kind == garden.CommentApproval && e.RevisionCommentID == 0 {
		e.RevisionCommentID = 1
	}
	body, err := garden.EncodeCommentEnvelope(e, text)
	if err != nil {
		t.Fatal(err)
	}
	return garden.Comment{ID: id, Author: actor, AuthorID: "stable-" + actor, Body: body}
}
func TestVerificationContractCases(t *testing.T) {
	id := garden.IssueIdentity{Repo: 7, Issue: 9}
	spec := "spec v1"
	h := garden.ContentHash(spec)
	rev := event(t, 1, "lead", garden.CommentEnvelope{SchemaVersion: 1, Kind: garden.CommentRevision, RepoID: 7, IssueID: 9, RevisionKind: garden.RevisionSpec, ContentHash: h}, spec)
	approval := event(t, 2, "lead", garden.CommentEnvelope{SchemaVersion: 1, Kind: garden.CommentApproval, RepoID: 7, IssueID: 9, RevisionHash: h, SourceSpecFingerprint: h}, "accepted")
	policy := garden.ApprovalPolicy{AllowedActorIDs: []string{"stable-lead"}}
	tests := []struct {
		name                                        string
		comments                                    []garden.Comment
		policy                                      garden.ApprovalPolicy
		valid, revoked, superseded, heads, problems int
	}{
		{"wrong author", []garden.Comment{rev, event(t, 2, "stranger", garden.CommentEnvelope{SchemaVersion: 1, Kind: garden.CommentApproval, RepoID: 7, IssueID: 9, RevisionHash: h, SourceSpecFingerprint: h}, "ok")}, policy, 0, 0, 0, 1, 1},
		{"foreign issue", []garden.Comment{rev, event(t, 2, "lead", garden.CommentEnvelope{SchemaVersion: 1, Kind: garden.CommentApproval, RepoID: 8, IssueID: 9, RevisionHash: h, SourceSpecFingerprint: h}, "ok")}, policy, 0, 0, 0, 1, 1},
		{"foreign issue id", []garden.Comment{rev, event(t, 2, "lead", garden.CommentEnvelope{SchemaVersion: 1, Kind: garden.CommentApproval, RepoID: 7, IssueID: 10, RevisionHash: h, SourceSpecFingerprint: h}, "ok")}, policy, 0, 0, 0, 1, 1},
		{"revision is not approval", []garden.Comment{rev}, policy, 0, 0, 0, 1, 0},
		{"edited approval", []garden.Comment{rev, {ID: 2, Author: "lead", Body: approval.Body + " edited"}}, policy, 0, 0, 0, 1, 1},
		{"deleted approval", []garden.Comment{rev}, policy, 0, 0, 0, 1, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			v := garden.Verify(id, tc.comments, tc.policy)
			if len(v.ValidApprovals) != tc.valid || len(v.RevokedApprovals) != tc.revoked || len(v.SupersededApprovals) != tc.superseded || len(v.Heads) != tc.heads || len(v.Problems) != tc.problems {
				t.Fatalf("%+v", v)
			}
		})
	}
	allowedRevocation := event(t, 3, "lead", garden.CommentEnvelope{SchemaVersion: 1, Kind: garden.CommentRevocation, RepoID: 7, IssueID: 9, TargetCommentID: 2}, "revoke")
	deniedRevocation := event(t, 3, "stranger", garden.CommentEnvelope{SchemaVersion: 1, Kind: garden.CommentRevocation, RepoID: 7, IssueID: 9, TargetCommentID: 2}, "revoke")
	if v := garden.Verify(id, []garden.Comment{rev, approval, allowedRevocation}, policy); len(v.RevokedApprovals) != 1 || len(v.ValidApprovals) != 0 {
		t.Fatalf("allowed revoke: %+v", v)
	}
	if v := garden.Verify(id, []garden.Comment{rev, approval, deniedRevocation}, policy); len(v.ValidApprovals) != 1 {
		t.Fatalf("denied revoke: %+v", v)
	}
	newSpec := "spec v2"
	h2 := garden.ContentHash(newSpec)
	rev2 := event(t, 4, "lead", garden.CommentEnvelope{SchemaVersion: 1, Kind: garden.CommentRevision, RepoID: 7, IssueID: 9, RevisionKind: garden.RevisionSpec, ContentHash: h2, Parents: []string{h}}, newSpec)
	if v := garden.Verify(id, []garden.Comment{rev, approval, rev2}, policy); len(v.SupersededApprovals) != 1 {
		t.Fatalf("supersession: %+v", v)
	}
	fork := event(t, 5, "lead", garden.CommentEnvelope{SchemaVersion: 1, Kind: garden.CommentRevision, RepoID: 7, IssueID: 9, RevisionKind: garden.RevisionSpec, ContentHash: garden.ContentHash("fork")}, "fork")
	v := garden.Verify(id, []garden.Comment{rev, approval, rev2, fork}, policy)
	if len(v.ConflictingHeads) != 2 {
		t.Fatalf("competing heads: %+v", v)
	}
	meta := garden.Metadata{SchemaVersion: 1, Provenance: "p", Owner: "o", Accepted: true, AcceptedSpecHash: h, SpecFingerprint: h}
	labels := garden.Classify([]string{garden.ManagedLabel, garden.FormatTypeLabel(garden.TypeFeature), garden.FormatStageLabel(garden.StageReady)})
	if lane := garden.Lane(labels, meta, v, nil); lane.Lane != garden.LaneAttention || len(v.ConflictingHeads) != 2 {
		t.Fatalf("conflict lane=%+v heads=%+v", lane, v.ConflictingHeads)
	}
	if deleted := garden.Verify(id, []garden.Comment{approval}, policy); len(deleted.Problems) == 0 || deleted.Problems[0].Code != "deleted-referenced-comment" {
		t.Fatalf("deleted referenced revision: %+v", deleted)
	}
}

func TestApprovalRequiresExactRevisionComment(t *testing.T) {
	identity := garden.IssueIdentity{Repo: 7, Issue: 9}
	h := garden.ContentHash("same")
	otherHash := garden.ContentHash("other")
	rev := event(t, 1, "lead", garden.CommentEnvelope{SchemaVersion: 1, Kind: garden.CommentRevision, RepoID: 7, IssueID: 9, RevisionKind: garden.RevisionSpec, ContentHash: h}, "same")
	sibling := event(t, 3, "lead", garden.CommentEnvelope{SchemaVersion: 1, Kind: garden.CommentRevision, RepoID: 7, IssueID: 9, RevisionKind: garden.RevisionSpec, ContentHash: h}, "same")
	other := event(t, 4, "lead", garden.CommentEnvelope{SchemaVersion: 1, Kind: garden.CommentRevision, RepoID: 7, IssueID: 9, RevisionKind: garden.RevisionSpec, ContentHash: otherHash}, "other")
	nonRevision := event(t, 5, "lead", garden.CommentEnvelope{SchemaVersion: 1, Kind: garden.CommentRevocation, RepoID: 7, IssueID: 9, TargetCommentID: 1}, "revoke")
	approval := func(id, revisionID int64, hash string) garden.Comment {
		return event(t, id, "lead", garden.CommentEnvelope{SchemaVersion: 1, Kind: garden.CommentApproval, RepoID: 7, IssueID: 9, RevisionHash: hash, RevisionCommentID: revisionID, SourceSpecFingerprint: h}, "approved")
	}
	rawApproval := func(id int64, revisionID int64, includeID bool) garden.Comment {
		e := garden.CommentEnvelope{SchemaVersion: 1, Kind: garden.CommentApproval, RepoID: 7, IssueID: 9, RevisionHash: h, SourceSpecFingerprint: h}
		if includeID {
			e.RevisionCommentID = revisionID
		}
		body, err := garden.EncodeCommentEnvelope(e, "approved")
		if err != nil {
			t.Fatal(err)
		}
		return garden.Comment{ID: id, Author: "lead", AuthorID: "stable-lead", Body: body}
	}
	foreign := event(t, 6, "lead", garden.CommentEnvelope{SchemaVersion: 1, Kind: garden.CommentRevision, RepoID: 7, IssueID: 10, RevisionKind: garden.RevisionSpec, ContentHash: h}, "same")
	foreignApproval := event(t, 7, "lead", garden.CommentEnvelope{SchemaVersion: 1, Kind: garden.CommentApproval, RepoID: 7, IssueID: 9, RevisionHash: h, RevisionCommentID: 6, SourceSpecFingerprint: h}, "approved")
	tests := []struct {
		name     string
		comments []garden.Comment
		approval garden.Comment
		valid    bool
	}{
		{"missing id", []garden.Comment{rev}, rawApproval(2, 0, false), false},
		{"zero id", []garden.Comment{rev}, rawApproval(2, 0, true), false},
		{"non revision id", []garden.Comment{rev, nonRevision}, approval(6, 5, h), false},
		{"different hash", []garden.Comment{rev, other}, approval(6, 4, h), false},
		{"identical sibling binds exact id", []garden.Comment{rev, sibling}, approval(4, 3, h), false},
		{"other issue id", []garden.Comment{rev, foreign}, foreignApproval, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			comments := append(append([]garden.Comment{}, tc.comments...), tc.approval)
			v := garden.Verify(identity, comments, garden.ApprovalPolicy{AllowedActorIDs: []string{"stable-lead"}})
			if (len(v.ValidApprovals) == 1) != tc.valid {
				t.Fatalf("verification: %+v", v)
			}
			if tc.name == "identical sibling binds exact id" && (len(v.Approvals) != 1 || len(v.Problems) != 0) {
				t.Fatalf("identical sibling reference was not accepted as the exact revision: %+v", v)
			}
			if !tc.valid && tc.name != "identical sibling binds exact id" && len(v.Problems) == 0 {
				t.Fatalf("invalid approval had no per-comment problem: %+v", v)
			}
		})
	}
}

func TestLaneProjectionDriftAndDuplicateLabels(t *testing.T) {
	h := garden.ContentHash("spec")
	e := func(id int64, k garden.CommentKind, kind garden.RevisionKind, hash string, parents []string, revhash string) garden.Comment {
		return event(t, id, "lead", garden.CommentEnvelope{SchemaVersion: 1, Kind: k, RepoID: 7, IssueID: 9, RevisionKind: kind, ContentHash: hash, Parents: parents, RevisionHash: revhash, SourceSpecFingerprint: h}, map[bool]string{true: "spec", false: "accepted"}[k == garden.CommentRevision])
	}
	v := garden.Verify(garden.IssueIdentity{Repo: 7, Issue: 9}, []garden.Comment{e(1, garden.CommentRevision, garden.RevisionSpec, h, nil, ""), e(2, garden.CommentApproval, "", "", nil, h)}, garden.ApprovalPolicy{AllowedActorIDs: []string{"stable-lead"}})
	labels := []string{garden.ManagedLabel, garden.FormatTypeLabel(garden.TypeFeature), garden.FormatTypeLabel(garden.TypeFeature), garden.FormatStageLabel(garden.StageReady), garden.FormatStageLabel(garden.StageReady)}
	c := garden.Classify(labels)
	if len(c.Problems) != 0 {
		t.Fatalf("duplicates counted: %+v", c)
	}
	m := garden.Metadata{SchemaVersion: 1, Provenance: "p", Owner: "o", Accepted: true, AcceptedSpecHash: h, SpecFingerprint: h}
	if got := garden.Lane(c, m, v, nil); got.Lane != garden.LaneWork {
		t.Fatalf("stale plan/verified acceptance should remain work: %+v", got)
	}
	plan := event(t, 3, "lead", garden.CommentEnvelope{SchemaVersion: 1, Kind: garden.CommentRevision, RepoID: 7, IssueID: 9, RevisionKind: garden.RevisionPlan, ContentHash: garden.ContentHash("plan"), Parents: []string{h}, SourceSpecFingerprint: h}, "plan")
	v = garden.Verify(garden.IssueIdentity{Repo: 7, Issue: 9}, []garden.Comment{e(1, garden.CommentRevision, garden.RevisionSpec, h, nil, ""), e(2, garden.CommentApproval, "", "", nil, h), plan}, garden.ApprovalPolicy{AllowedActorIDs: []string{"stable-lead"}})
	if got := garden.Lane(c, m, v, nil); got.Lane != garden.LaneWork {
		t.Fatalf("stale plan removed work: %+v verification=%+v", got, v)
	}
	m.AcceptedSpecHash = garden.ContentHash("drift")
	if got := garden.Lane(c, m, v, nil); got.Lane != garden.LaneWork {
		t.Fatalf("legacy projection overrode verified acceptance: %+v", got)
	}
}
