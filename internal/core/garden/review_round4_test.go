package garden_test

import (
	"errors"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
)

func TestRound4IncompleteAndRevocationProjection(t *testing.T) {
	id := garden.IssueIdentity{Repo: 7, Issue: 9}
	h := garden.ContentHash("spec")
	rev := event(t, 1, "lead", garden.CommentEnvelope{SchemaVersion: 1, Kind: garden.CommentRevision, RepoID: 7, IssueID: 9, RevisionKind: garden.RevisionSpec, ContentHash: h}, "spec")
	approval := event(t, 2, "lead", garden.CommentEnvelope{SchemaVersion: 1, Kind: garden.CommentApproval, RepoID: 7, IssueID: 9, RevisionHash: h, SourceSpecFingerprint: h}, "accepted")
	policy := garden.ApprovalPolicy{AllowedActorIDs: []string{"lead-id"}}
	v := garden.Verify(id, []garden.Comment{rev, approval}, policy, garden.VerifyOptions{Complete: false})
	if v.Complete || garden.Lane(garden.Classification{Stage: garden.StageReady}, garden.Metadata{SchemaVersion: 1, Provenance: "p", Owner: "o"}, v, nil).Lane != garden.LaneAttention {
		t.Fatal("incomplete log must require attention")
	}
	v = garden.Verify(id, []garden.Comment{rev, approval}, policy, garden.VerifyOptions{Complete: true, Revocations: []garden.RevocationRecord{{ApprovalCommentID: 2, RevocationCommentID: 3}}})
	if len(v.ValidApprovals) != 0 || len(v.Problems) == 0 {
		t.Fatal("missing projected revocation must invalidate approval and report problem")
	}
	if !errors.Is(garden.ErrTooManyComments, garden.ErrTooManyComments) {
		t.Fatal("typed limit sentinel missing")
	}
}

func TestRound4UnacceptedMetadataFingerprintRejected(t *testing.T) {
	_, err := garden.EncodeMetadata(garden.Metadata{SchemaVersion: 1, Provenance: "p", Owner: "o", SpecFingerprint: garden.ContentHash("x")})
	if !errors.Is(err, garden.ErrInvalidMetadata) {
		t.Fatalf("expected invalid metadata, got %v", err)
	}
}
