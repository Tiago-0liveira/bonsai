package garden

import (
	"errors"
	"strings"
	"testing"
)

func envelopeComment(t *testing.T, id int64, actor string, e CommentEnvelope, body string) Comment {
	t.Helper()
	if e.Kind == CommentApproval && e.RevisionCommentID == 0 {
		e.RevisionCommentID = 1
	}
	raw, err := EncodeCommentEnvelope(e, body)
	if err != nil {
		t.Fatal(err)
	}
	return Comment{ID: id, Author: actor, AuthorID: "author-id", Body: raw}
}
func TestVerifyAndLane(t *testing.T) {
	identity := IssueIdentity{Repo: 10, Issue: 20}
	spec := "spec"
	hash := ContentHash(spec)
	revision := envelopeComment(t, 1, "author", CommentEnvelope{SchemaVersion: 1, Kind: CommentRevision, RepoID: 10, IssueID: 20, RevisionKind: RevisionSpec, ContentHash: hash}, spec)
	approval := envelopeComment(t, 2, "author", CommentEnvelope{SchemaVersion: 1, Kind: CommentApproval, RepoID: 10, IssueID: 20, RevisionHash: hash, SourceSpecFingerprint: hash}, "approved")
	v := Verify(identity, []Comment{revision, approval}, ApprovalPolicy{AllowedActorIDs: []string{"author-id"}})
	if len(v.ValidApprovals) != 1 || v.AcceptedSpecHash != hash {
		t.Fatalf("verification: %+v", v)
	}
	m := Metadata{SchemaVersion: 1, Provenance: "p", Owner: "o", Accepted: true, AcceptedSpecHash: hash, SpecFingerprint: hash}
	c := Classify([]string{ManagedLabel, FormatTypeLabel(TypeFeature), FormatStageLabel(StageReady)})
	if got := Lane(c, m, v, nil); got.Lane != LaneWork {
		t.Fatalf("lane: %+v", got)
	}
	if got := Lane(c, m, Verification{}, nil); got.Lane != LaneAttention {
		t.Fatalf("ready without approval: %+v", got)
	}
}
func TestNormalizeExactWhitespace(t *testing.T) {
	if got := NormalizeContent(" \tA \t\n\n"); got != " \tA" {
		t.Fatalf("got %q", got)
	}
	if ContentHash("é") == ContentHash("e\u0301") {
		t.Fatal("Unicode normalization unexpectedly applied")
	}
}
func TestMetadataValidation(t *testing.T) {
	m := Metadata{SchemaVersion: 1, Provenance: "p", Owner: "o", Accepted: true, AcceptedSpecHash: ContentHash("s"), SpecFingerprint: ContentHash("s")}
	if _, err := EncodeMetadata(m); err != nil {
		t.Fatal(err)
	}
	m.AcceptedSpecHash = "bad"
	if _, err := EncodeMetadata(m); !errors.Is(err, ErrInvalidMetadata) {
		t.Fatalf("%v", err)
	}
	if _, err := DecodeMetadata([]byte(`{"schema_version":9}`)); !errors.Is(err, ErrUnsupportedSchema) {
		t.Fatalf("%v", err)
	}
	if _, err := DecodeMetadata([]byte(strings.Repeat("x", MaxMetadataBytes+1))); !errors.Is(err, ErrInvalidMetadata) {
		t.Fatalf("%v", err)
	}
}
