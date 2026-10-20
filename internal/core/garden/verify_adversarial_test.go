package garden_test

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
)

// Helper constructors provide valid baseline models and comment envelopes for adversarial testing.

func testIdentity(repo uint64, issue uint64) garden.IssueIdentity {
	return garden.IssueIdentity{
		Repo:  garden.RepoID(repo),
		Issue: garden.IssueID(issue),
	}
}

func makeComment(t *testing.T, id int64, author string, authorID string, e garden.CommentEnvelope, text string, createdAt time.Time) garden.Comment {
	t.Helper()
	if e.Kind == garden.CommentApproval && e.RevisionCommentID == 0 {
		e.RevisionCommentID = 1
	}
	body, err := garden.EncodeCommentEnvelope(e, text)
	if err != nil {
		t.Fatalf("makeComment failed to encode envelope: %v", err)
	}
	return garden.Comment{
		ID:        id,
		Author:    author,
		AuthorID:  authorID,
		Body:      body,
		CreatedAt: createdAt,
	}
}

func rawComment(id int64, author string, authorID string, body string, createdAt time.Time) garden.Comment {
	return garden.Comment{
		ID:        id,
		Author:    author,
		AuthorID:  authorID,
		Body:      body,
		CreatedAt: createdAt,
	}
}

func randomSHA256Hex(t *testing.T) string {
	t.Helper()
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		t.Fatalf("rand.Read failed: %v", err)
	}
	return hex.EncodeToString(buf)
}

// ---------------------------------------------------------------------------
// 1. Forgery Adversarial Tests
// ---------------------------------------------------------------------------

func TestVerifyAdversarial_Forgery(t *testing.T) {
	t.Parallel()
	identity := testIdentity(10, 20)
	spec := "canonical specification content"
	specHash := garden.ContentHash(spec)
	revComment := makeComment(t, 1, "lead", "lead-id", garden.CommentEnvelope{
		SchemaVersion: 1,
		Kind:          garden.CommentRevision,
		RepoID:        10,
		IssueID:       20,
		RevisionKind:  garden.RevisionSpec,
		ContentHash:   specHash,
	}, spec, time.Unix(100, 0))
	policy := garden.ApprovalPolicy{AllowedActorIDs: []string{"lead-id"}}

	tests := []struct {
		name        string
		comment     garden.Comment
		wantProblem string
	}{
		{
			name: "approval envelope copied verbatim from issue A into issue B (different issue_id)",
			comment: makeComment(t, 2, "lead", "lead-id", garden.CommentEnvelope{
				SchemaVersion:         1,
				Kind:                  garden.CommentApproval,
				RepoID:                10,
				IssueID:               999, // Forged from issue 999
				RevisionHash:          specHash,
				SourceSpecFingerprint: specHash,
			}, "approved", time.Unix(200, 0)),
			wantProblem: "wrong-issue-repo",
		},
		{
			name: "same issue number in a different repo_id",
			comment: makeComment(t, 2, "lead", "lead-id", garden.CommentEnvelope{
				SchemaVersion:         1,
				Kind:                  garden.CommentApproval,
				RepoID:                888, // Forged from different repo
				IssueID:               20,
				RevisionHash:          specHash,
				SourceSpecFingerprint: specHash,
			}, "approved", time.Unix(200, 0)),
			wantProblem: "wrong-issue-repo",
		},
		{
			name: "repo_id as string",
			comment: rawComment(2, "lead", "lead-id",
				fmt.Sprintf("<!-- bonsai:garden/v1 {\"schema_version\":1,\"kind\":\"approval\",\"repo_id\":\"10\",\"issue_id\":20,\"revision_hash\":\"%s\",\"source_spec_fingerprint\":\"%s\",\"record_hash\":\"%s\"} -->\napproved", specHash, specHash, garden.ContentHash("approved")),
				time.Unix(200, 0)),
			wantProblem: "malformed-envelope",
		},
		{
			name: "repo_id negative",
			comment: rawComment(2, "lead", "lead-id",
				fmt.Sprintf("<!-- bonsai:garden/v1 {\"schema_version\":1,\"kind\":\"approval\",\"repo_id\":-10,\"issue_id\":20,\"revision_hash\":\"%s\",\"source_spec_fingerprint\":\"%s\",\"record_hash\":\"%s\"} -->\napproved", specHash, specHash, garden.ContentHash("approved")),
				time.Unix(200, 0)),
			wantProblem: "malformed-envelope",
		},
		{
			name: "repo_id zero",
			comment: rawComment(2, "lead", "lead-id",
				fmt.Sprintf("<!-- bonsai:garden/v1 {\"schema_version\":1,\"kind\":\"approval\",\"repo_id\":0,\"issue_id\":20,\"revision_hash\":\"%s\",\"source_spec_fingerprint\":\"%s\",\"record_hash\":\"%s\"} -->\napproved", specHash, specHash, garden.ContentHash("approved")),
				time.Unix(200, 0)),
			wantProblem: "malformed-envelope",
		},
		{
			name: "repo_id huge overflow",
			comment: rawComment(2, "lead", "lead-id",
				fmt.Sprintf("<!-- bonsai:garden/v1 {\"schema_version\":1,\"kind\":\"approval\",\"repo_id\":99999999999999999999999999999999999999999999999999,\"issue_id\":20,\"revision_hash\":\"%s\",\"source_spec_fingerprint\":\"%s\",\"record_hash\":\"%s\"} -->\napproved", specHash, specHash, garden.ContentHash("approved")),
				time.Unix(200, 0)),
			wantProblem: "malformed-envelope",
		},
		{
			name: "issue_id as string",
			comment: rawComment(2, "lead", "lead-id",
				fmt.Sprintf("<!-- bonsai:garden/v1 {\"schema_version\":1,\"kind\":\"approval\",\"repo_id\":10,\"issue_id\":\"20\",\"revision_hash\":\"%s\",\"source_spec_fingerprint\":\"%s\",\"record_hash\":\"%s\"} -->\napproved", specHash, specHash, garden.ContentHash("approved")),
				time.Unix(200, 0)),
			wantProblem: "malformed-envelope",
		},
		{
			name: "issue_id negative",
			comment: rawComment(2, "lead", "lead-id",
				fmt.Sprintf("<!-- bonsai:garden/v1 {\"schema_version\":1,\"kind\":\"approval\",\"repo_id\":10,\"issue_id\":-20,\"revision_hash\":\"%s\",\"source_spec_fingerprint\":\"%s\",\"record_hash\":\"%s\"} -->\napproved", specHash, specHash, garden.ContentHash("approved")),
				time.Unix(200, 0)),
			wantProblem: "malformed-envelope",
		},
		{
			name: "issue_id zero",
			comment: rawComment(2, "lead", "lead-id",
				fmt.Sprintf("<!-- bonsai:garden/v1 {\"schema_version\":1,\"kind\":\"approval\",\"repo_id\":10,\"issue_id\":0,\"revision_hash\":\"%s\",\"source_spec_fingerprint\":\"%s\",\"record_hash\":\"%s\"} -->\napproved", specHash, specHash, garden.ContentHash("approved")),
				time.Unix(200, 0)),
			wantProblem: "malformed-envelope",
		},
		{
			name: "issue_id huge overflow",
			comment: rawComment(2, "lead", "lead-id",
				fmt.Sprintf("<!-- bonsai:garden/v1 {\"schema_version\":1,\"kind\":\"approval\",\"repo_id\":10,\"issue_id\":99999999999999999999999999999999999999999999999999,\"revision_hash\":\"%s\",\"source_spec_fingerprint\":\"%s\",\"record_hash\":\"%s\"} -->\napproved", specHash, specHash, garden.ContentHash("approved")),
				time.Unix(200, 0)),
			wantProblem: "malformed-envelope",
		},
		{
			name: "envelope not on first line - marker later in body",
			comment: rawComment(2, "lead", "lead-id",
				fmt.Sprintf("Introductory human text here\n<!-- bonsai:garden/v1 {\"schema_version\":1,\"kind\":\"approval\",\"repo_id\":10,\"issue_id\":20,\"revision_hash\":\"%s\",\"source_spec_fingerprint\":\"%s\",\"record_hash\":\"%s\"} -->\napproved", specHash, specHash, garden.ContentHash("approved")),
				time.Unix(200, 0)),
			wantProblem: "malformed-envelope",
		},
		{
			name: "envelope not on first line - marker inside fenced code block",
			comment: rawComment(2, "lead", "lead-id",
				fmt.Sprintf("```\n<!-- bonsai:garden/v1 {\"schema_version\":1,\"kind\":\"approval\",\"repo_id\":10,\"issue_id\":20,\"revision_hash\":\"%s\",\"source_spec_fingerprint\":\"%s\",\"record_hash\":\"%s\"} -->\n```\napproved", specHash, specHash, garden.ContentHash("approved")),
				time.Unix(200, 0)),
			wantProblem: "malformed-envelope",
		},
		{
			name: "envelope not on first line - marker inside quoted reply",
			comment: rawComment(2, "lead", "lead-id",
				fmt.Sprintf("> <!-- bonsai:garden/v1 {\"schema_version\":1,\"kind\":\"approval\",\"repo_id\":10,\"issue_id\":20,\"revision_hash\":\"%s\",\"source_spec_fingerprint\":\"%s\",\"record_hash\":\"%s\"} -->\napproved", specHash, specHash, garden.ContentHash("approved")),
				time.Unix(200, 0)),
			wantProblem: "malformed-envelope",
		},
		{
			name: "two markers in one comment line",
			comment: rawComment(2, "lead", "lead-id",
				fmt.Sprintf("<!-- bonsai:garden/v1 {\"schema_version\":1,\"kind\":\"approval\",\"repo_id\":10,\"issue_id\":20,\"revision_hash\":\"%s\",\"source_spec_fingerprint\":\"%s\",\"record_hash\":\"%s\"} --> <!-- bonsai:garden/v1 {\"schema_version\":1} -->\napproved", specHash, specHash, garden.ContentHash("approved")),
				time.Unix(200, 0)),
			wantProblem: "malformed-envelope",
		},
		{
			name: "marker with trailing garbage on first line",
			comment: rawComment(2, "lead", "lead-id",
				fmt.Sprintf("<!-- bonsai:garden/v1 {\"schema_version\":1,\"kind\":\"approval\",\"repo_id\":10,\"issue_id\":20,\"revision_hash\":\"%s\",\"source_spec_fingerprint\":\"%s\",\"record_hash\":\"%s\"} --> trailing extra garbage\napproved", specHash, specHash, garden.ContentHash("approved")),
				time.Unix(200, 0)),
			wantProblem: "malformed-envelope",
		},
		{
			name: "marker with unknown field",
			comment: rawComment(2, "lead", "lead-id",
				fmt.Sprintf("<!-- bonsai:garden/v1 {\"schema_version\":1,\"kind\":\"approval\",\"repo_id\":10,\"issue_id\":20,\"revision_hash\":\"%s\",\"source_spec_fingerprint\":\"%s\",\"record_hash\":\"%s\",\"injected_field\":\"surprise\"} -->\napproved", specHash, specHash, garden.ContentHash("approved")),
				time.Unix(200, 0)),
			wantProblem: "malformed-envelope",
		},
		{
			name: "duplicate JSON keys in marker",
			comment: rawComment(2, "lead", "lead-id",
				fmt.Sprintf("<!-- bonsai:garden/v1 {\"schema_version\":1,\"schema_version\":1,\"kind\":\"approval\",\"repo_id\":10,\"issue_id\":20,\"revision_hash\":\"%s\",\"source_spec_fingerprint\":\"%s\",\"record_hash\":\"%s\"} -->\napproved", specHash, specHash, garden.ContentHash("approved")),
				time.Unix(200, 0)),
			wantProblem: "malformed-envelope",
		},
		{
			name: "invalid UTF-8 in marker",
			comment: rawComment(2, "lead", "lead-id",
				"<!-- bonsai:garden/v1 {\"schema_version\":1,\"\xff\xfe\":true} -->\napproved",
				time.Unix(200, 0)),
			wantProblem: "malformed-envelope",
		},
		{
			name: "oversize JSON envelope exceeding MaxMetadataBytes",
			comment: rawComment(2, "lead", "lead-id",
				"<!-- bonsai:garden/v1 {\"schema_version\":1,\"padding\":\""+strings.Repeat("x", garden.MaxMetadataBytes+10)+"\"} -->\napproved",
				time.Unix(200, 0)),
			wantProblem: "malformed-envelope",
		},
		{
			name: "schema version 0",
			comment: rawComment(2, "lead", "lead-id",
				fmt.Sprintf("<!-- bonsai:garden/v1 {\"schema_version\":0,\"kind\":\"approval\",\"repo_id\":10,\"issue_id\":20,\"revision_hash\":\"%s\",\"source_spec_fingerprint\":\"%s\",\"record_hash\":\"%s\"} -->\napproved", specHash, specHash, garden.ContentHash("approved")),
				time.Unix(200, 0)),
			wantProblem: "unsupported-schema",
		},
		{
			name: "schema version 2",
			comment: rawComment(2, "lead", "lead-id",
				fmt.Sprintf("<!-- bonsai:garden/v1 {\"schema_version\":2,\"kind\":\"approval\",\"repo_id\":10,\"issue_id\":20,\"revision_hash\":\"%s\",\"source_spec_fingerprint\":\"%s\",\"record_hash\":\"%s\"} -->\napproved", specHash, specHash, garden.ContentHash("approved")),
				time.Unix(200, 0)),
			wantProblem: "unsupported-schema",
		},
		{
			name: "schema version string 1",
			comment: rawComment(2, "lead", "lead-id",
				fmt.Sprintf("<!-- bonsai:garden/v1 {\"schema_version\":\"1\",\"kind\":\"approval\",\"repo_id\":10,\"issue_id\":20,\"revision_hash\":\"%s\",\"source_spec_fingerprint\":\"%s\",\"record_hash\":\"%s\"} -->\napproved", specHash, specHash, garden.ContentHash("approved")),
				time.Unix(200, 0)),
			wantProblem: "malformed-envelope",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			v := garden.Verify(identity, []garden.Comment{revComment, tc.comment}, policy)
			if len(v.ValidApprovals) != 0 {
				t.Fatalf("expected 0 valid approvals for forged envelope, got %d", len(v.ValidApprovals))
			}
			found := false
			for _, p := range v.Problems {
				if p.Code == tc.wantProblem {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("expected problem code %q in v.Problems, got %+v", tc.wantProblem, v.Problems)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 2. Authorship Adversarial Tests
// ---------------------------------------------------------------------------

func TestVerifyAdversarial_Authorship(t *testing.T) {
	t.Parallel()
	identity := testIdentity(10, 20)
	spec := "spec canonical content"
	specHash := garden.ContentHash(spec)
	revComment := makeComment(t, 1, "alice", "alice-author-id-1001", garden.CommentEnvelope{
		SchemaVersion: 1,
		Kind:          garden.CommentRevision,
		RepoID:        10,
		IssueID:       20,
		RevisionKind:  garden.RevisionSpec,
		ContentHash:   specHash,
	}, spec, time.Unix(100, 0))

	t.Run("approval by non-allowed login", func(t *testing.T) {
		t.Parallel()
		approval := makeComment(t, 2, "mallory", "mallory-id", garden.CommentEnvelope{
			SchemaVersion:         1,
			Kind:                  garden.CommentApproval,
			RepoID:                10,
			IssueID:               20,
			RevisionHash:          specHash,
			SourceSpecFingerprint: specHash,
		}, "approved", time.Unix(200, 0))

		v := garden.Verify(identity, []garden.Comment{revComment, approval}, garden.ApprovalPolicy{
			AllowedActorIDs: []string{"alice-author-id-1001"},
		})
		if len(v.ValidApprovals) != 0 {
			t.Fatalf("unauthorized author approval accepted: %+v", v)
		}
		if len(v.Problems) == 0 || v.Problems[0].Code != "wrong-author" {
			t.Fatalf("expected wrong-author problem, got %+v", v.Problems)
		}
	})

	t.Run("allowed login but different stable author ID (login rename / account takeover)", func(t *testing.T) {
		t.Parallel()
		// CONTRACT: "Verification binds every envelope to the requested repo and issue and derives state only from the ordered server comment log, including author IDs and timestamps when supplied."
		// CONTRACT: "Only authenticated actors in the configured allowlist can approve."
		// An attacker who claims or renames their account to "alice" has a different stable author ID than the authentic alice.
		imposterApproval := makeComment(t, 2, "alice", "attacker-imposter-id-9999", garden.CommentEnvelope{
			SchemaVersion:         1,
			Kind:                  garden.CommentApproval,
			RepoID:                10,
			IssueID:               20,
			RevisionHash:          specHash,
			SourceSpecFingerprint: specHash,
		}, "approved by imposter", time.Unix(200, 0))

		v := garden.Verify(identity, []garden.Comment{revComment, imposterApproval}, garden.ApprovalPolicy{
			AllowedActorIDs: []string{"alice-author-id-1001"},
		})
		if len(v.ValidApprovals) != 0 {
			t.Fatalf("// CONTRACT: \"Verification binds every envelope to the requested repo and issue and derives state only from the ordered server comment log, including author IDs and timestamps when supplied.\": imposter approval with different AuthorID was accepted as valid (got %d valid approvals)", len(v.ValidApprovals))
		}
	})

	t.Run("renamed login with allowed stable ID remains authorized", func(t *testing.T) {
		t.Parallel()
		approval := makeComment(t, 2, "ALICE", "alice-author-id-1001", garden.CommentEnvelope{
			SchemaVersion:         1,
			Kind:                  garden.CommentApproval,
			RepoID:                10,
			IssueID:               20,
			RevisionHash:          specHash,
			SourceSpecFingerprint: specHash,
		}, "approved", time.Unix(200, 0))

		v := garden.Verify(identity, []garden.Comment{revComment, approval}, garden.ApprovalPolicy{
			AllowedActorIDs: []string{"alice-author-id-1001"},
		})
		if len(v.ValidApprovals) != 1 {
			t.Fatalf("login label changes should not affect stable-ID authorization, got: %+v", v)
		}
	})

	t.Run("empty author in comment", func(t *testing.T) {
		t.Parallel()
		approval := makeComment(t, 2, "", "some-id", garden.CommentEnvelope{
			SchemaVersion:         1,
			Kind:                  garden.CommentApproval,
			RepoID:                10,
			IssueID:               20,
			RevisionHash:          specHash,
			SourceSpecFingerprint: specHash,
		}, "approved", time.Unix(200, 0))

		v := garden.Verify(identity, []garden.Comment{revComment, approval}, garden.ApprovalPolicy{
			AllowedActorIDs: []string{"alice-author-id-1001"},
		})
		if len(v.ValidApprovals) != 0 {
			t.Fatalf("empty author must not be accepted, got %+v", v)
		}
	})

	t.Run("policy with empty allowed set rejects all approvals", func(t *testing.T) {
		t.Parallel()
		approval := makeComment(t, 2, "alice", "alice-author-id-1001", garden.CommentEnvelope{
			SchemaVersion:         1,
			Kind:                  garden.CommentApproval,
			RepoID:                10,
			IssueID:               20,
			RevisionHash:          specHash,
			SourceSpecFingerprint: specHash,
		}, "approved", time.Unix(200, 0))

		v := garden.Verify(identity, []garden.Comment{revComment, approval}, garden.ApprovalPolicy{
			AllowedActorIDs: []string{},
		})
		if len(v.ValidApprovals) != 0 {
			t.Fatalf("empty allowed policy must reject all approvals, got %+v", v)
		}
	})

	t.Run("revocation with missing stable author ID is rejected", func(t *testing.T) {
		t.Parallel()
		approval := makeComment(t, 2, "alice", "alice-author-id-1001", garden.CommentEnvelope{SchemaVersion: 1, Kind: garden.CommentApproval, RepoID: 10, IssueID: 20, RevisionHash: specHash, SourceSpecFingerprint: specHash}, "approved", time.Unix(200, 0))
		revocation := makeComment(t, 3, "alice", "", garden.CommentEnvelope{SchemaVersion: 1, Kind: garden.CommentRevocation, RepoID: 10, IssueID: 20, TargetCommentID: 2}, "revoke", time.Unix(300, 0))
		v := garden.Verify(identity, []garden.Comment{revComment, approval, revocation}, garden.ApprovalPolicy{AllowedActorIDs: []string{"alice-author-id-1001"}})
		if len(v.ValidApprovals) != 1 || len(v.Problems) == 0 {
			t.Fatalf("missing revocation author ID should fail closed without revoking approval: %+v", v)
		}
	})

	t.Run("self-approval permitted when revision author is in allowed set", func(t *testing.T) {
		t.Parallel()
		// CONTRACT: "Only authenticated actors in the configured allowlist can approve."
		// The contract does not forbid self-approval when the revision author is an allowed actor.
		approval := makeComment(t, 2, "alice", "alice-author-id-1001", garden.CommentEnvelope{
			SchemaVersion:         1,
			Kind:                  garden.CommentApproval,
			RepoID:                10,
			IssueID:               20,
			RevisionHash:          specHash,
			SourceSpecFingerprint: specHash,
		}, "approved by self", time.Unix(200, 0))

		v := garden.Verify(identity, []garden.Comment{revComment, approval}, garden.ApprovalPolicy{
			AllowedActorIDs: []string{"alice-author-id-1001"},
		})
		if len(v.ValidApprovals) != 1 {
			t.Fatalf("self-approval by allowed actor should be valid per contract, got %+v", v)
		}
		v = garden.Verify(identity, []garden.Comment{revComment, approval}, garden.ApprovalPolicy{
			AllowedActorIDs: []string{"alice-author-id-1001"}, ForbidSelfApproval: true,
		})
		if len(v.ValidApprovals) != 0 || len(v.Problems) == 0 {
			t.Fatalf("explicit self-approval prohibition was not enforced: %+v", v)
		}
	})
}

// ---------------------------------------------------------------------------
// 3. Replay and Ordering Adversarial Tests
// ---------------------------------------------------------------------------

func TestVerifyAdversarial_ReplayAndOrdering(t *testing.T) {
	t.Parallel()
	identity := testIdentity(10, 20)
	spec := "spec v1"
	h1 := garden.ContentHash(spec)
	policy := garden.ApprovalPolicy{AllowedActorIDs: []string{"lead-id"}}

	t.Run("approval created BEFORE the revision it approves", func(t *testing.T) {
		t.Parallel()
		// CONTRACT: "Revision, approval, and revocation records are append-only GitHub comments."
		// CONTRACT: "Verification binds every envelope to the requested repo and issue and derives state only from the ordered server comment log, including author IDs and timestamps when supplied."
		prematureApproval := makeComment(t, 1, "lead", "lead-id", garden.CommentEnvelope{
			SchemaVersion:         1,
			Kind:                  garden.CommentApproval,
			RepoID:                10,
			IssueID:               20,
			RevisionHash:          h1,
			SourceSpecFingerprint: h1,
		}, "premature approval", time.Unix(100, 0))

		rev := makeComment(t, 2, "lead", "lead-id", garden.CommentEnvelope{
			SchemaVersion: 1,
			Kind:          garden.CommentRevision,
			RepoID:        10,
			IssueID:       20,
			RevisionKind:  garden.RevisionSpec,
			ContentHash:   h1,
		}, spec, time.Unix(200, 0))

		v := garden.Verify(identity, []garden.Comment{prematureApproval, rev}, policy)
		if len(v.ValidApprovals) != 0 {
			t.Fatalf("// CONTRACT: \"Revision, approval, and revocation records are append-only GitHub comments.\": approval created before the revision it approves was counted as valid")
		}
	})

	t.Run("approval referencing a hash that no revision has", func(t *testing.T) {
		t.Parallel()
		rev := makeComment(t, 1, "lead", "lead-id", garden.CommentEnvelope{
			SchemaVersion: 1,
			Kind:          garden.CommentRevision,
			RepoID:        10,
			IssueID:       20,
			RevisionKind:  garden.RevisionSpec,
			ContentHash:   h1,
		}, spec, time.Unix(100, 0))

		bogusHash := randomSHA256Hex(t)
		approval := makeComment(t, 2, "lead", "lead-id", garden.CommentEnvelope{
			SchemaVersion:         1,
			Kind:                  garden.CommentApproval,
			RepoID:                10,
			IssueID:               20,
			RevisionHash:          bogusHash,
			SourceSpecFingerprint: bogusHash,
		}, "approved", time.Unix(200, 0))

		v := garden.Verify(identity, []garden.Comment{rev, approval}, policy)
		if len(v.ValidApprovals) != 0 {
			t.Fatalf("approval for nonexistent hash accepted: %+v", v)
		}
		if len(v.Problems) == 0 || v.Problems[0].Code != "deleted-referenced-comment" {
			t.Fatalf("expected deleted-referenced-comment problem, got %+v", v.Problems)
		}
	})

	t.Run("approval for revision N after head moved to N+1 is superseded", func(t *testing.T) {
		t.Parallel()
		rev1 := makeComment(t, 1, "lead", "lead-id", garden.CommentEnvelope{
			SchemaVersion: 1,
			Kind:          garden.CommentRevision,
			RepoID:        10,
			IssueID:       20,
			RevisionKind:  garden.RevisionSpec,
			ContentHash:   h1,
		}, spec, time.Unix(100, 0))

		spec2 := "spec v2"
		h2 := garden.ContentHash(spec2)
		rev2 := makeComment(t, 2, "lead", "lead-id", garden.CommentEnvelope{
			SchemaVersion: 1,
			Kind:          garden.CommentRevision,
			RepoID:        10,
			IssueID:       20,
			RevisionKind:  garden.RevisionSpec,
			ContentHash:   h2,
			Parents:       []string{h1},
		}, spec2, time.Unix(200, 0))

		approvalForRev1 := makeComment(t, 3, "lead", "lead-id", garden.CommentEnvelope{
			SchemaVersion:         1,
			Kind:                  garden.CommentApproval,
			RepoID:                10,
			IssueID:               20,
			RevisionHash:          h1,
			SourceSpecFingerprint: h1,
		}, "approved rev1 late", time.Unix(300, 0))

		v := garden.Verify(identity, []garden.Comment{rev1, rev2, approvalForRev1}, policy)
		if len(v.ValidApprovals) != 0 {
			t.Fatalf("expected 0 valid approvals, got %d", len(v.ValidApprovals))
		}
		if len(v.SupersededApprovals) != 1 {
			t.Fatalf("expected 1 superseded approval, got %d", len(v.SupersededApprovals))
		}
	})

	t.Run("approval whose recorded source-spec fingerprint differs from head fingerprint", func(t *testing.T) {
		t.Parallel()
		rev1 := makeComment(t, 1, "lead", "lead-id", garden.CommentEnvelope{
			SchemaVersion: 1,
			Kind:          garden.CommentRevision,
			RepoID:        10,
			IssueID:       20,
			RevisionKind:  garden.RevisionSpec,
			ContentHash:   h1,
		}, spec, time.Unix(100, 0))

		differingFingerprint := randomSHA256Hex(t)
		approval := makeComment(t, 2, "lead", "lead-id", garden.CommentEnvelope{
			SchemaVersion:         1,
			Kind:                  garden.CommentApproval,
			RepoID:                10,
			IssueID:               20,
			RevisionHash:          h1,
			SourceSpecFingerprint: differingFingerprint,
		}, "approved", time.Unix(200, 0))

		v := garden.Verify(identity, []garden.Comment{rev1, approval}, policy)
		if len(v.ValidApprovals) != 0 {
			t.Fatalf("approval with differing source-spec fingerprint accepted: %+v", v)
		}
		if len(v.SupersededApprovals) != 1 {
			t.Fatalf("expected 1 superseded approval, got %d", len(v.SupersededApprovals))
		}
	})

	t.Run("log given out of chronological order (shuffle determinism)", func(t *testing.T) {
		t.Parallel()
		// CONTRACT: "Verification binds every envelope to the requested repo and issue and derives state only from the ordered server comment log, including author IDs and timestamps when supplied."
		// CONTRACT: "Revocation and supersession invalidate approval; they are explicit records and never mutate prior revision content."
		rev := makeComment(t, 1, "lead", "lead-id", garden.CommentEnvelope{
			SchemaVersion: 1,
			Kind:          garden.CommentRevision,
			RepoID:        10,
			IssueID:       20,
			RevisionKind:  garden.RevisionSpec,
			ContentHash:   h1,
		}, spec, time.Unix(100, 0))

		approval := makeComment(t, 2, "lead", "lead-id", garden.CommentEnvelope{
			SchemaVersion:         1,
			Kind:                  garden.CommentApproval,
			RepoID:                10,
			IssueID:               20,
			RevisionHash:          h1,
			SourceSpecFingerprint: h1,
		}, "approved", time.Unix(200, 0))

		revocation := makeComment(t, 3, "lead", "lead-id", garden.CommentEnvelope{
			SchemaVersion:   1,
			Kind:            garden.CommentRevocation,
			RepoID:          10,
			IssueID:         20,
			TargetCommentID: 2,
		}, "revoked", time.Unix(300, 0))

		// In canonical order: rev, approval, revocation => approval is revoked
		canonical := garden.Verify(identity, []garden.Comment{rev, approval, revocation}, policy)
		if len(canonical.ValidApprovals) != 0 || len(canonical.RevokedApprovals) != 1 {
			t.Fatalf("setup failed: expected 0 valid, 1 revoked, got %+v", canonical)
		}

		// Shuffled order: revocation placed before approval
		shuffled := garden.Verify(identity, []garden.Comment{rev, revocation, approval}, policy)
		if len(shuffled.ValidApprovals) != len(canonical.ValidApprovals) || len(shuffled.RevokedApprovals) != len(canonical.RevokedApprovals) {
			t.Fatalf("// CONTRACT: \"Verification binds every envelope to the requested repo and issue and derives state only from the ordered server comment log, including author IDs and timestamps when supplied.\": out-of-order comment slice yielded non-deterministic verification (shuffled has %d valid approvals, canonical has %d)", len(shuffled.ValidApprovals), len(canonical.ValidApprovals))
		}
	})

	t.Run("duplicate comment IDs in log", func(t *testing.T) {
		t.Parallel()
		// CONTRACT: "Verification binds every envelope to the requested repo and issue and derives state only from the ordered server comment log, including author IDs and timestamps when supplied."
		rev := makeComment(t, 1, "lead", "lead-id", garden.CommentEnvelope{
			SchemaVersion: 1,
			Kind:          garden.CommentRevision,
			RepoID:        10,
			IssueID:       20,
			RevisionKind:  garden.RevisionSpec,
			ContentHash:   h1,
		}, spec, time.Unix(100, 0))
		dupRev := rev // Exact duplicate comment ID

		v := garden.Verify(identity, []garden.Comment{rev, dupRev}, policy)
		// Revisions should be deduplicated by comment ID or content; duplicate comment in log should not create competing heads
		if len(v.Heads) > 1 {
			t.Fatalf("// CONTRACT: \"Verification binds every envelope to the requested repo and issue and derives state only from the ordered server comment log, including author IDs and timestamps when supplied.\": duplicate comment created multiple heads: %+v", v.Heads)
		}
	})

	t.Run("equal timestamps in comment log", func(t *testing.T) {
		t.Parallel()
		sameTime := time.Unix(100, 0)
		rev := makeComment(t, 1, "lead", "lead-id", garden.CommentEnvelope{
			SchemaVersion: 1,
			Kind:          garden.CommentRevision,
			RepoID:        10,
			IssueID:       20,
			RevisionKind:  garden.RevisionSpec,
			ContentHash:   h1,
		}, spec, sameTime)

		approval := makeComment(t, 2, "lead", "lead-id", garden.CommentEnvelope{
			SchemaVersion:         1,
			Kind:                  garden.CommentApproval,
			RepoID:                10,
			IssueID:               20,
			RevisionHash:          h1,
			SourceSpecFingerprint: h1,
		}, "approved", sameTime)

		v := garden.Verify(identity, []garden.Comment{rev, approval}, policy)
		if len(v.ValidApprovals) != 1 {
			t.Fatalf("valid approval with equal timestamp rejected: %+v", v)
		}
	})
}

// ---------------------------------------------------------------------------
// 4. Revocation Adversarial Tests
// ---------------------------------------------------------------------------

func TestVerifyAdversarial_Revocation(t *testing.T) {
	t.Parallel()
	identity := testIdentity(10, 20)
	spec := "spec v1"
	h1 := garden.ContentHash(spec)
	policy := garden.ApprovalPolicy{AllowedActorIDs: []string{"lead-id"}}

	rev := makeComment(t, 1, "lead", "lead-id", garden.CommentEnvelope{
		SchemaVersion: 1,
		Kind:          garden.CommentRevision,
		RepoID:        10,
		IssueID:       20,
		RevisionKind:  garden.RevisionSpec,
		ContentHash:   h1,
	}, spec, time.Unix(100, 0))

	approval := makeComment(t, 2, "lead", "lead-id", garden.CommentEnvelope{
		SchemaVersion:         1,
		Kind:                  garden.CommentApproval,
		RepoID:                10,
		IssueID:               20,
		RevisionHash:          h1,
		SourceSpecFingerprint: h1,
	}, "approved", time.Unix(200, 0))

	t.Run("revocation by allowed actor revokes approval", func(t *testing.T) {
		t.Parallel()
		revocation := makeComment(t, 3, "lead", "lead-id", garden.CommentEnvelope{
			SchemaVersion:   1,
			Kind:            garden.CommentRevocation,
			RepoID:          10,
			IssueID:         20,
			TargetCommentID: 2,
		}, "revoke", time.Unix(300, 0))

		v := garden.Verify(identity, []garden.Comment{rev, approval, revocation}, policy)
		if len(v.ValidApprovals) != 0 || len(v.RevokedApprovals) != 1 {
			t.Fatalf("expected 0 valid and 1 revoked approval, got %+v", v)
		}
	})

	t.Run("revocation by non-allowed actor ignored", func(t *testing.T) {
		t.Parallel()
		revocation := makeComment(t, 3, "unauthorized", "unauthorized-id", garden.CommentEnvelope{
			SchemaVersion:   1,
			Kind:            garden.CommentRevocation,
			RepoID:          10,
			IssueID:         20,
			TargetCommentID: 2,
		}, "unauthorized revoke", time.Unix(300, 0))

		v := garden.Verify(identity, []garden.Comment{rev, approval, revocation}, policy)
		if len(v.ValidApprovals) != 1 || len(v.RevokedApprovals) != 0 {
			t.Fatalf("unauthorized revocation should be ignored, got %+v", v)
		}
	})

	t.Run("revocation targeting non-existent comment ID", func(t *testing.T) {
		t.Parallel()
		revocation := makeComment(t, 3, "lead", "lead-id", garden.CommentEnvelope{
			SchemaVersion:   1,
			Kind:            garden.CommentRevocation,
			RepoID:          10,
			IssueID:         20,
			TargetCommentID: 99999, // Does not exist
		}, "bogus revoke", time.Unix(300, 0))

		v := garden.Verify(identity, []garden.Comment{rev, approval, revocation}, policy)
		if len(v.ValidApprovals) != 1 {
			t.Fatalf("bogus revocation invalidated valid approval: %+v", v)
		}
		found := false
		for _, p := range v.Problems {
			if p.Code == "deleted-referenced-comment" {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected deleted-referenced-comment problem for nonexistent target ID, got %+v", v.Problems)
		}
	})

	t.Run("revocation targeting comment belonging to another issue", func(t *testing.T) {
		t.Parallel()
		foreignComment := makeComment(t, 99, "lead", "lead-id", garden.CommentEnvelope{
			SchemaVersion:         1,
			Kind:                  garden.CommentApproval,
			RepoID:                10,
			IssueID:               999, // Foreign issue
			RevisionHash:          h1,
			SourceSpecFingerprint: h1,
		}, "foreign approved", time.Unix(150, 0))

		revocation := makeComment(t, 3, "lead", "lead-id", garden.CommentEnvelope{
			SchemaVersion:   1,
			Kind:            garden.CommentRevocation,
			RepoID:          10,
			IssueID:         20,
			TargetCommentID: 99, // Points to foreign comment
		}, "revoke foreign", time.Unix(300, 0))

		v := garden.Verify(identity, []garden.Comment{rev, foreignComment, approval, revocation}, policy)
		if len(v.ValidApprovals) != 1 {
			t.Fatalf("foreign revocation target improperly revoked current issue approval: %+v", v)
		}
	})

	t.Run("revocation BEFORE the approval it targets", func(t *testing.T) {
		t.Parallel()
		prematureRevocation := makeComment(t, 3, "lead", "lead-id", garden.CommentEnvelope{
			SchemaVersion:   1,
			Kind:            garden.CommentRevocation,
			RepoID:          10,
			IssueID:         20,
			TargetCommentID: 4, // Targets comment 4 before it exists
		}, "premature revoke", time.Unix(150, 0))

		laterApproval := makeComment(t, 4, "lead", "lead-id", garden.CommentEnvelope{
			SchemaVersion:         1,
			Kind:                  garden.CommentApproval,
			RepoID:                10,
			IssueID:               20,
			RevisionHash:          h1,
			SourceSpecFingerprint: h1,
		}, "approved later", time.Unix(250, 0))

		v := garden.Verify(identity, []garden.Comment{rev, prematureRevocation, laterApproval}, policy)
		// A premature revocation cannot revoke an approval created after it
		if len(v.ValidApprovals) != 1 {
			t.Fatalf("premature revocation should not revoke a later approval, got %+v", v)
		}
	})

	t.Run("revocation of a revocation does not restore approval", func(t *testing.T) {
		t.Parallel()
		// CONTRACT: "Revocation and supersession invalidate approval; they are explicit records and never mutate prior revision content."
		revocation1 := makeComment(t, 3, "lead", "lead-id", garden.CommentEnvelope{
			SchemaVersion:   1,
			Kind:            garden.CommentRevocation,
			RepoID:          10,
			IssueID:         20,
			TargetCommentID: 2, // Revokes approval (comment 2)
		}, "revoke approval", time.Unix(300, 0))

		revocation2 := makeComment(t, 4, "lead", "lead-id", garden.CommentEnvelope{
			SchemaVersion:   1,
			Kind:            garden.CommentRevocation,
			RepoID:          10,
			IssueID:         20,
			TargetCommentID: 3, // Targets revocation1
		}, "revoke the revocation", time.Unix(400, 0))

		v := garden.Verify(identity, []garden.Comment{rev, approval, revocation1, revocation2}, policy)
		if len(v.ValidApprovals) != 0 {
			t.Fatalf("revoking a revocation must not restore approval, got %+v", v)
		}
	})

	t.Run("re-approval after revocation succeeds", func(t *testing.T) {
		t.Parallel()
		revocation := makeComment(t, 3, "lead", "lead-id", garden.CommentEnvelope{
			SchemaVersion:   1,
			Kind:            garden.CommentRevocation,
			RepoID:          10,
			IssueID:         20,
			TargetCommentID: 2,
		}, "revoke", time.Unix(300, 0))

		reApproval := makeComment(t, 4, "lead", "lead-id", garden.CommentEnvelope{
			SchemaVersion:         1,
			Kind:                  garden.CommentApproval,
			RepoID:                10,
			IssueID:               20,
			RevisionHash:          h1,
			SourceSpecFingerprint: h1,
		}, "re-approved", time.Unix(400, 0))

		v := garden.Verify(identity, []garden.Comment{rev, approval, revocation, reApproval}, policy)
		if len(v.ValidApprovals) != 1 {
			t.Fatalf("expected 1 valid re-approval, got %d (v=%+v)", len(v.ValidApprovals), v)
		}
		if len(v.RevokedApprovals) != 1 {
			t.Fatalf("expected 1 revoked approval, got %d", len(v.RevokedApprovals))
		}
	})
}

// ---------------------------------------------------------------------------
// 5. Integrity Adversarial Tests
// ---------------------------------------------------------------------------

func TestVerifyAdversarial_Integrity(t *testing.T) {
	t.Parallel()
	identity := testIdentity(10, 20)
	spec := "spec canonical text"
	h1 := garden.ContentHash(spec)
	policy := garden.ApprovalPolicy{AllowedActorIDs: []string{"lead-id"}}

	t.Run("revision declared content hash does not match body text (edited)", func(t *testing.T) {
		t.Parallel()
		tamperedRev := makeComment(t, 1, "lead", "lead-id", garden.CommentEnvelope{
			SchemaVersion: 1,
			Kind:          garden.CommentRevision,
			RepoID:        10,
			IssueID:       20,
			RevisionKind:  garden.RevisionSpec,
			ContentHash:   h1,
		}, spec, time.Unix(100, 0))
		tamperedRev.Body += " altered after the fact"

		v := garden.Verify(identity, []garden.Comment{tamperedRev}, policy)
		if len(v.Revisions) != 0 {
			t.Fatalf("edited revision accepted: %+v", v)
		}
		found := false
		for _, p := range v.Problems {
			if p.Code == "edited-hash-mismatch" {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected edited-hash-mismatch problem, got %+v", v.Problems)
		}
	})

	t.Run("comment deleted while later revision references it as parent", func(t *testing.T) {
		t.Parallel()
		// Rev 1 is omitted from the comment log
		missingParentHash := randomSHA256Hex(t)
		rev2 := makeComment(t, 2, "lead", "lead-id", garden.CommentEnvelope{
			SchemaVersion: 1,
			Kind:          garden.CommentRevision,
			RepoID:        10,
			IssueID:       20,
			RevisionKind:  garden.RevisionSpec,
			ContentHash:   h1,
			Parents:       []string{missingParentHash},
		}, spec, time.Unix(200, 0))

		v := garden.Verify(identity, []garden.Comment{rev2}, policy)
		found := false
		for _, p := range v.Problems {
			if p.Code == "deleted-referenced-comment" {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected deleted-referenced-comment problem when parent missing, got %+v", v.Problems)
		}
	})

	t.Run("comment deleted while later approval references it", func(t *testing.T) {
		t.Parallel()
		// Revision is missing from the log
		approval := makeComment(t, 2, "lead", "lead-id", garden.CommentEnvelope{
			SchemaVersion:         1,
			Kind:                  garden.CommentApproval,
			RepoID:                10,
			IssueID:               20,
			RevisionHash:          h1,
			SourceSpecFingerprint: h1,
		}, "approved", time.Unix(200, 0))

		v := garden.Verify(identity, []garden.Comment{approval}, policy)
		if len(v.ValidApprovals) != 0 {
			t.Fatalf("approval for missing revision counted as valid: %+v", v)
		}
		found := false
		for _, p := range v.Problems {
			if p.Code == "deleted-referenced-comment" {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected deleted-referenced-comment problem, got %+v", v.Problems)
		}
	})

	t.Run("comment deleted while later revocation references it", func(t *testing.T) {
		t.Parallel()
		revocation := makeComment(t, 3, "lead", "lead-id", garden.CommentEnvelope{
			SchemaVersion:   1,
			Kind:            garden.CommentRevocation,
			RepoID:          10,
			IssueID:         20,
			TargetCommentID: 999, // Comment 999 is missing from log
		}, "revoke", time.Unix(300, 0))

		v := garden.Verify(identity, []garden.Comment{revocation}, policy)
		found := false
		for _, p := range v.Problems {
			if p.Code == "deleted-referenced-comment" {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected deleted-referenced-comment problem, got %+v", v.Problems)
		}
	})

	t.Run("revision comment presented as approval", func(t *testing.T) {
		t.Parallel()
		// Envelope declares revision but contains approval-specific fields
		forged := rawComment(1, "lead", "lead-id",
			fmt.Sprintf("<!-- bonsai:garden/v1 {\"schema_version\":1,\"kind\":\"revision\",\"repo_id\":10,\"issue_id\":20,\"revision_kind\":\"spec\",\"content_hash\":\"%s\",\"revision_hash\":\"%s\"} -->\n%s", h1, h1, spec),
			time.Unix(100, 0))

		v := garden.Verify(identity, []garden.Comment{forged}, policy)
		if len(v.Revisions) != 0 || len(v.ValidApprovals) != 0 {
			t.Fatalf("forged hybrid revision/approval was accepted: %+v", v)
		}
		if len(v.Problems) == 0 {
			t.Fatalf("expected problem for invalid envelope fields")
		}
	})

	t.Run("approval comment presented as revision", func(t *testing.T) {
		t.Parallel()
		// Envelope declares approval but contains revision-specific fields
		forged := rawComment(1, "lead", "lead-id",
			fmt.Sprintf("<!-- bonsai:garden/v1 {\"schema_version\":1,\"kind\":\"approval\",\"repo_id\":10,\"issue_id\":20,\"revision_hash\":\"%s\",\"content_hash\":\"%s\",\"record_hash\":\"%s\"} -->\napproved", h1, h1, garden.ContentHash("approved")),
			time.Unix(100, 0))

		v := garden.Verify(identity, []garden.Comment{forged}, policy)
		if len(v.ValidApprovals) != 0 {
			t.Fatalf("forged hybrid approval was accepted: %+v", v)
		}
		if len(v.Problems) == 0 {
			t.Fatalf("expected problem for invalid envelope fields")
		}
	})

	t.Run("competing spec heads lists BOTH and sets LaneAttention", func(t *testing.T) {
		t.Parallel()
		rev1 := makeComment(t, 1, "lead", "lead-id", garden.CommentEnvelope{
			SchemaVersion: 1,
			Kind:          garden.CommentRevision,
			RepoID:        10,
			IssueID:       20,
			RevisionKind:  garden.RevisionSpec,
			ContentHash:   h1,
		}, spec, time.Unix(100, 0))

		specForkA := "spec branch A"
		hA := garden.ContentHash(specForkA)
		revForkA := makeComment(t, 2, "lead", "lead-id", garden.CommentEnvelope{
			SchemaVersion: 1,
			Kind:          garden.CommentRevision,
			RepoID:        10,
			IssueID:       20,
			RevisionKind:  garden.RevisionSpec,
			ContentHash:   hA,
			Parents:       []string{h1},
		}, specForkA, time.Unix(200, 0))

		specForkB := "spec branch B"
		hB := garden.ContentHash(specForkB)
		revForkB := makeComment(t, 3, "lead", "lead-id", garden.CommentEnvelope{
			SchemaVersion: 1,
			Kind:          garden.CommentRevision,
			RepoID:        10,
			IssueID:       20,
			RevisionKind:  garden.RevisionSpec,
			ContentHash:   hB,
			Parents:       []string{h1},
		}, specForkB, time.Unix(250, 0))

		v := garden.Verify(identity, []garden.Comment{rev1, revForkA, revForkB}, policy)
		if len(v.ConflictingHeads) != 2 {
			t.Fatalf("expected 2 conflicting heads, got %+v", v.ConflictingHeads)
		}
		hasA, hasB := false, false
		for _, h := range v.ConflictingHeads {
			if h.Hash == hA {
				hasA = true
			}
			if h.Hash == hB {
				hasB = true
			}
		}
		if !hasA || !hasB {
			t.Fatalf("conflicting heads must list both branches: %+v", v.ConflictingHeads)
		}

		labels := garden.Classify([]string{
			garden.ManagedLabel,
			garden.FormatTypeLabel(garden.TypeFeature),
			garden.FormatStageLabel(garden.StageReady),
		})
		meta := garden.Metadata{
			SchemaVersion:    1,
			Provenance:       "p",
			Owner:            "o",
			Accepted:         true,
			AcceptedSpecHash: hA,
			SpecFingerprint:  hA,
		}
		res := garden.Lane(labels, meta, v, nil)
		if res.Lane != garden.LaneAttention {
			t.Fatalf("competing heads must result in LaneAttention, got %v (%s)", res.Lane, res.Reason)
		}
	})

	t.Run("competing plan heads lists BOTH and sets LaneAttention", func(t *testing.T) {
		t.Parallel()
		// CONTRACT: "A changed body/revision head, missing referenced comment, altered comment, unexpected label, or competing head is external-edit/conflict state requiring attention; adapters must not silently overwrite it."
		// CONTRACT: "competing heads and projection drift are Attention."
		revSpec := makeComment(t, 1, "lead", "lead-id", garden.CommentEnvelope{
			SchemaVersion: 1,
			Kind:          garden.CommentRevision,
			RepoID:        10,
			IssueID:       20,
			RevisionKind:  garden.RevisionSpec,
			ContentHash:   h1,
		}, spec, time.Unix(100, 0))
		appr := makeComment(t, 2, "lead", "lead-id", garden.CommentEnvelope{
			SchemaVersion:         1,
			Kind:                  garden.CommentApproval,
			RepoID:                10,
			IssueID:               20,
			RevisionHash:          h1,
			SourceSpecFingerprint: h1,
		}, "approved", time.Unix(200, 0))

		planA := "plan branch A"
		phA := garden.ContentHash(planA)
		revPlanA := makeComment(t, 3, "lead", "lead-id", garden.CommentEnvelope{
			SchemaVersion:         1,
			Kind:                  garden.CommentRevision,
			RepoID:                10,
			IssueID:               20,
			RevisionKind:          garden.RevisionPlan,
			ContentHash:           phA,
			Parents:               []string{h1},
			SourceSpecFingerprint: h1,
		}, planA, time.Unix(300, 0))

		planB := "plan branch B"
		phB := garden.ContentHash(planB)
		revPlanB := makeComment(t, 4, "lead", "lead-id", garden.CommentEnvelope{
			SchemaVersion:         1,
			Kind:                  garden.CommentRevision,
			RepoID:                10,
			IssueID:               20,
			RevisionKind:          garden.RevisionPlan,
			ContentHash:           phB,
			Parents:               []string{h1},
			SourceSpecFingerprint: h1,
		}, planB, time.Unix(400, 0))

		v := garden.Verify(identity, []garden.Comment{revSpec, appr, revPlanA, revPlanB}, policy)
		if len(v.ConflictingHeads) < 2 {
			t.Fatalf("// CONTRACT: \"A changed body/revision head, missing referenced comment, altered comment, unexpected label, or competing head is external-edit/conflict state requiring attention; adapters must not silently overwrite it.\": competing plan heads not identified in ConflictingHeads (got %d)", len(v.ConflictingHeads))
		}
	})
}

// ---------------------------------------------------------------------------
// 6. Lane Classification Adversarial Tests
// ---------------------------------------------------------------------------

func TestVerifyAdversarial_Lane(t *testing.T) {
	t.Parallel()
	identity := testIdentity(10, 20)
	spec := "canonical spec"
	specHash := garden.ContentHash(spec)
	policy := garden.ApprovalPolicy{AllowedActorIDs: []string{"lead-id"}}

	revSpec := makeComment(t, 1, "lead", "lead-id", garden.CommentEnvelope{
		SchemaVersion: 1,
		Kind:          garden.CommentRevision,
		RepoID:        10,
		IssueID:       20,
		RevisionKind:  garden.RevisionSpec,
		ContentHash:   specHash,
	}, spec, time.Unix(100, 0))

	appr := makeComment(t, 2, "lead", "lead-id", garden.CommentEnvelope{
		SchemaVersion:         1,
		Kind:                  garden.CommentApproval,
		RepoID:                10,
		IssueID:               20,
		RevisionHash:          specHash,
		SourceSpecFingerprint: specHash,
	}, "approved", time.Unix(200, 0))

	validVerification := garden.Verify(identity, []garden.Comment{revSpec, appr}, policy)

	validMetadata := garden.Metadata{
		SchemaVersion:    1,
		Provenance:       "p",
		Owner:            "o",
		Accepted:         true,
		AcceptedSpecHash: specHash,
		SpecFingerprint:  specHash,
	}

	t.Run("Ready label + valid metadata but no approval => Attention", func(t *testing.T) {
		t.Parallel()
		labels := garden.Classify([]string{
			garden.ManagedLabel,
			garden.FormatTypeLabel(garden.TypeFeature),
			garden.FormatStageLabel(garden.StageReady),
		})
		emptyVerification := garden.Verification{}
		res := garden.Lane(labels, validMetadata, emptyVerification, nil)
		if res.Lane != garden.LaneAttention {
			t.Fatalf("expected LaneAttention for Ready stage without approval, got %v (%s)", res.Lane, res.Reason)
		}
		if res.Reason == "" {
			t.Fatalf("expected non-empty reason")
		}
	})

	t.Run("valid approval for head => Work", func(t *testing.T) {
		t.Parallel()
		labels := garden.Classify([]string{
			garden.ManagedLabel,
			garden.FormatTypeLabel(garden.TypeFeature),
			garden.FormatStageLabel(garden.StageReady),
		})
		res := garden.Lane(labels, validMetadata, validVerification, nil)
		if res.Lane != garden.LaneWork {
			t.Fatalf("expected LaneWork for valid approval of head, got %v (%s)", res.Lane, res.Reason)
		}
		if res.Reason == "" {
			t.Fatalf("expected non-empty reason")
		}
	})

	t.Run("approval then plan-only revision => still Work", func(t *testing.T) {
		t.Parallel()
		plan := "plan v1 content"
		planHash := garden.ContentHash(plan)
		revPlan := makeComment(t, 3, "lead", "lead-id", garden.CommentEnvelope{
			SchemaVersion:         1,
			Kind:                  garden.CommentRevision,
			RepoID:                10,
			IssueID:               20,
			RevisionKind:          garden.RevisionPlan,
			ContentHash:           planHash,
			Parents:               []string{specHash},
			SourceSpecFingerprint: specHash,
		}, plan, time.Unix(300, 0))

		v := garden.Verify(identity, []garden.Comment{revSpec, appr, revPlan}, policy)
		labels := garden.Classify([]string{
			garden.ManagedLabel,
			garden.FormatTypeLabel(garden.TypeFeature),
			garden.FormatStageLabel(garden.StageReady),
		})
		res := garden.Lane(labels, validMetadata, v, nil)
		if res.Lane != garden.LaneWork {
			t.Fatalf("plan revision should leave accepted specification in Work, got %v (%s)", res.Lane, res.Reason)
		}
	})

	t.Run("approval then spec revision when stage is Ready => Attention", func(t *testing.T) {
		t.Parallel()
		spec2 := "spec v2 modified"
		specHash2 := garden.ContentHash(spec2)
		revSpec2 := makeComment(t, 3, "lead", "lead-id", garden.CommentEnvelope{
			SchemaVersion: 1,
			Kind:          garden.CommentRevision,
			RepoID:        10,
			IssueID:       20,
			RevisionKind:  garden.RevisionSpec,
			ContentHash:   specHash2,
			Parents:       []string{specHash},
		}, spec2, time.Unix(300, 0))

		v := garden.Verify(identity, []garden.Comment{revSpec, appr, revSpec2}, policy)
		labels := garden.Classify([]string{
			garden.ManagedLabel,
			garden.FormatTypeLabel(garden.TypeFeature),
			garden.FormatStageLabel(garden.StageReady),
		})
		res := garden.Lane(labels, validMetadata, v, nil)
		if res.Lane == garden.LaneWork {
			t.Fatalf("superseded spec approval must not remain in Work, got %v (%s)", res.Lane, res.Reason)
		}
		if res.Lane != garden.LaneAttention {
			t.Fatalf("expected LaneAttention when Ready item spec head moved, got %v (%s)", res.Lane, res.Reason)
		}
	})

	t.Run("approval then spec revision when stage is not Ready (e.g. Draft or In-Progress) => Attention", func(t *testing.T) {
		t.Parallel()
		// CONTRACT: "A changed body/revision head, missing referenced comment, altered comment, unexpected label, or competing head is external-edit/conflict state requiring attention; adapters must not silently overwrite it."
		// CONTRACT: "Work requires a valid, non-revoked approval for the current spec head and matching accepted-spec hash; competing heads and projection drift are Attention."
		spec2 := "spec v2 modified"
		specHash2 := garden.ContentHash(spec2)
		revSpec2 := makeComment(t, 3, "lead", "lead-id", garden.CommentEnvelope{
			SchemaVersion: 1,
			Kind:          garden.CommentRevision,
			RepoID:        10,
			IssueID:       20,
			RevisionKind:  garden.RevisionSpec,
			ContentHash:   specHash2,
			Parents:       []string{specHash},
		}, spec2, time.Unix(300, 0))

		v := garden.Verify(identity, []garden.Comment{revSpec, appr, revSpec2}, policy)
		// Metadata recorded accepted spec as v1, but head is v2 and unapproved
		labels := garden.Classify([]string{
			garden.ManagedLabel,
			garden.FormatTypeLabel(garden.TypeFeature),
			garden.FormatStageLabel(garden.StageInProgress),
		})
		res := garden.Lane(labels, validMetadata, v, nil)
		if res.Lane != garden.LaneAttention {
			t.Fatalf("// CONTRACT: \"A changed body/revision head, missing referenced comment, altered comment, unexpected label, or competing head is external-edit/conflict state requiring attention; adapters must not silently overwrite it.\": expected LaneAttention for external spec head change on accepted item, got %v (reason: %q)", res.Lane, res.Reason)
		}
	})

	t.Run("metadata AcceptedSpecHash != verified accepted hash => Attention", func(t *testing.T) {
		t.Parallel()
		driftedMeta := validMetadata
		driftedMeta.AcceptedSpecHash = garden.ContentHash("different spec")
		labels := garden.Classify([]string{
			garden.ManagedLabel,
			garden.FormatTypeLabel(garden.TypeFeature),
			garden.FormatStageLabel(garden.StageReady),
		})
		res := garden.Lane(labels, driftedMeta, validVerification, nil)
		if res.Lane != garden.LaneAttention {
			t.Fatalf("expected LaneAttention on accepted spec metadata drift, got %v (%s)", res.Lane, res.Reason)
		}
	})

	t.Run("malformed metadata with non-nil metadataErr => Attention", func(t *testing.T) {
		t.Parallel()
		labels := garden.Classify([]string{
			garden.ManagedLabel,
			garden.FormatTypeLabel(garden.TypeFeature),
			garden.FormatStageLabel(garden.StageDraft),
		})
		res := garden.Lane(labels, garden.Metadata{}, validVerification, errors.New("corrupt JSON envelope"))
		if res.Lane != garden.LaneAttention {
			t.Fatalf("expected LaneAttention for metadataErr, got %v (%s)", res.Lane, res.Reason)
		}
		if res.Reason == "" {
			t.Fatalf("expected non-empty reason for metadata error")
		}
	})

	t.Run("every Attention result has a non-empty human reason", func(t *testing.T) {
		t.Parallel()
		testCases := []struct {
			name        string
			c           garden.Classification
			m           garden.Metadata
			v           garden.Verification
			metadataErr error
		}{
			{
				name:        "metadata error",
				c:           garden.Classify([]string{garden.ManagedLabel, garden.FormatTypeLabel(garden.TypeFeature), garden.FormatStageLabel(garden.StageDraft)}),
				metadataErr: errors.New("err"),
			},
			{
				name: "invalid metadata struct",
				c:    garden.Classify([]string{garden.ManagedLabel, garden.FormatTypeLabel(garden.TypeFeature), garden.FormatStageLabel(garden.StageDraft)}),
				m:    garden.Metadata{SchemaVersion: 1}, // Missing owner/provenance
			},
			{
				name: "invalid labels",
				c:    garden.Classify([]string{"bonsai:managed", "bonsai:type/unknown"}),
				m:    validMetadata,
				v:    validVerification,
			},
			{
				name: "competing heads",
				c:    garden.Classify([]string{garden.ManagedLabel, garden.FormatTypeLabel(garden.TypeFeature), garden.FormatStageLabel(garden.StageDraft)}),
				m:    validMetadata,
				v:    garden.Verification{ConflictingHeads: []garden.Revision{{}, {}}},
			},
			{
				name: "ready stage unapproved",
				c:    garden.Classify([]string{garden.ManagedLabel, garden.FormatTypeLabel(garden.TypeFeature), garden.FormatStageLabel(garden.StageReady)}),
				m:    validMetadata,
				v:    garden.Verification{},
			},
			{
				name: "hash drift",
				c:    garden.Classify([]string{garden.ManagedLabel, garden.FormatTypeLabel(garden.TypeFeature), garden.FormatStageLabel(garden.StageDraft)}),
				m:    garden.Metadata{SchemaVersion: 1, Provenance: "p", Owner: "o", Accepted: true, AcceptedSpecHash: "a", SpecFingerprint: "a"},
				v:    garden.Verification{AcceptedSpecHash: "b", ValidApprovals: []garden.Approval{{}}},
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				res := garden.Lane(tc.c, tc.m, tc.v, tc.metadataErr)
				if res.Lane == garden.LaneAttention && strings.TrimSpace(res.Reason) == "" {
					t.Fatalf("Attention result must have a non-empty human reason: %+v", res)
				}
			})
		}
	})
}

// ---------------------------------------------------------------------------
// 7. Property-Style Fuzzing
// ---------------------------------------------------------------------------

func FuzzVerify(f *testing.F) {
	spec := "canonical spec content"
	specHash := garden.ContentHash(spec)
	validRev, _ := garden.EncodeCommentEnvelope(garden.CommentEnvelope{
		SchemaVersion: 1,
		Kind:          garden.CommentRevision,
		RepoID:        10,
		IssueID:       20,
		RevisionKind:  garden.RevisionSpec,
		ContentHash:   specHash,
	}, spec)

	validAppr, _ := garden.EncodeCommentEnvelope(garden.CommentEnvelope{
		SchemaVersion:         1,
		Kind:                  garden.CommentApproval,
		RepoID:                10,
		IssueID:               20,
		RevisionCommentID:     1,
		RevisionHash:          specHash,
		SourceSpecFingerprint: specHash,
	}, "approved")

	validRevoc, _ := garden.EncodeCommentEnvelope(garden.CommentEnvelope{
		SchemaVersion:   1,
		Kind:            garden.CommentRevocation,
		RepoID:          10,
		IssueID:         20,
		TargetCommentID: 2,
	}, "revoked")

	// Seed corpus with valid and invalid envelopes
	f.Add([]byte(""))
	f.Add([]byte("plain human text with no envelope marker"))
	f.Add([]byte(validRev))
	f.Add([]byte(validAppr))
	f.Add([]byte(validRevoc))
	f.Add([]byte("<!-- bonsai:garden/v1 invalid json -->"))
	f.Add([]byte("<!-- bonsai:garden/v1 {\"schema_version\":2} -->"))
	f.Add([]byte("<!-- bonsai:garden/v1 {\"schema_version\":1,\"kind\":\"unknown\"} -->"))
	f.Add([]byte("<!-- bonsai:garden/v1 {\"schema_version\":1,\"kind\":\"approval\",\"repo_id\":0} -->"))
	f.Add([]byte(strings.Repeat("a", garden.MaxMetadataBytes+100)))
	f.Add([]byte("<!-- bonsai:garden/v1 {\"schema_version\":1,\"repo_id\":10,\"issue_id\":20} -->\x00\xff\xfe"))

	f.Fuzz(func(t *testing.T, data []byte) {
		id := testIdentity(10, 20)
		c := garden.Comment{
			ID:        1,
			Author:    "lead",
			AuthorID:  "lead-id",
			Body:      string(data),
			CreatedAt: time.Unix(100, 0),
		}
		policy := garden.ApprovalPolicy{AllowedActorIDs: []string{"lead-id"}}

		// Assert no panic
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("Verify panicked on input: %v", r)
			}
		}()

		v := garden.Verify(id, []garden.Comment{c}, policy)

		// Assert that no single comment body can ever produce a valid approval
		// (a valid approval requires an approved revision head present in the log)
		if len(v.ValidApprovals) > 0 {
			t.Fatalf("single comment body unexpectedly produced a valid approval: %q", string(data))
		}
	})
}

func TestFuzzCorpusSeeds(t *testing.T) {
	t.Parallel()
	spec := "seed spec content"
	specHash := garden.ContentHash(spec)
	validRev, err := garden.EncodeCommentEnvelope(garden.CommentEnvelope{
		SchemaVersion: 1,
		Kind:          garden.CommentRevision,
		RepoID:        10,
		IssueID:       20,
		RevisionKind:  garden.RevisionSpec,
		ContentHash:   specHash,
	}, spec)
	if err != nil {
		t.Fatal(err)
	}

	seeds := []string{
		"",
		"regular comment text",
		validRev,
		"<!-- bonsai:garden/v1 malformed -->",
		"<!-- bonsai:garden/v1 {\"schema_version\":99} -->",
		strings.Repeat("A", 1000),
	}

	id := testIdentity(10, 20)
	policy := garden.ApprovalPolicy{AllowedActorIDs: []string{"lead-id"}}
	for i, seed := range seeds {
		t.Run(fmt.Sprintf("Seed_%d", i), func(t *testing.T) {
			t.Parallel()
			c := garden.Comment{
				ID:        int64(i + 1),
				Author:    "lead",
				AuthorID:  "lead-id",
				Body:      seed,
				CreatedAt: time.Unix(100, 0),
			}
			v := garden.Verify(id, []garden.Comment{c}, policy)
			if len(v.ValidApprovals) > 0 {
				t.Fatalf("single seed produced valid approval")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// 8. Concurrency Safety Test
// ---------------------------------------------------------------------------

func TestVerifyAdversarial_ConcurrentSafety(t *testing.T) {
	t.Parallel()
	identity := testIdentity(10, 20)
	spec := "spec canonical text"
	specHash := garden.ContentHash(spec)
	policy := garden.ApprovalPolicy{AllowedActorIDs: []string{"lead-id"}}

	comments := []garden.Comment{
		makeComment(t, 1, "lead", "lead-id", garden.CommentEnvelope{
			SchemaVersion: 1,
			Kind:          garden.CommentRevision,
			RepoID:        10,
			IssueID:       20,
			RevisionKind:  garden.RevisionSpec,
			ContentHash:   specHash,
		}, spec, time.Unix(100, 0)),
		makeComment(t, 2, "lead", "lead-id", garden.CommentEnvelope{
			SchemaVersion:         1,
			Kind:                  garden.CommentApproval,
			RepoID:                10,
			IssueID:               20,
			RevisionHash:          specHash,
			SourceSpecFingerprint: specHash,
		}, "approved", time.Unix(200, 0)),
		rawComment(3, "lead", "lead-id", "regular human comment without envelope", time.Unix(300, 0)),
		rawComment(4, "lead", "lead-id", "<!-- bonsai:garden/v1 invalid envelope -->", time.Unix(400, 0)),
	}

	// Deep snapshot to ensure input comments slice is never mutated by Verify
	originalComments := make([]garden.Comment, len(comments))
	copy(originalComments, comments)

	const goroutines = 20
	const iterations = 50
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for g := 0; g < goroutines; g++ {
		go func() {
			defer wg.Done()
			for it := 0; it < iterations; it++ {
				v := garden.Verify(identity, comments, policy)
				if len(v.ValidApprovals) != 1 {
					t.Errorf("concurrent verify produced wrong valid approvals: %d", len(v.ValidApprovals))
					return
				}
				if v.AcceptedSpecHash != specHash {
					t.Errorf("concurrent verify produced wrong accepted spec hash: %s", v.AcceptedSpecHash)
					return
				}
			}
		}()
	}

	wg.Wait()

	// Assert input comments slice was not mutated
	if !reflect.DeepEqual(comments, originalComments) {
		t.Fatal("Verify mutated its input comments slice!")
	}
}
