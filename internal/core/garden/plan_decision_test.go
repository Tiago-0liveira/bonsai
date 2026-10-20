package garden_test

import (
	"strings"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
)

func TestVerifyPlanRevisionsAndDecisions(t *testing.T) {
	id := garden.IssueIdentity{Repo: 10, Issue: 20}
	policy := garden.ApprovalPolicy{AllowedActorIDs: []string{"lead-id"}}
	fp := garden.ContentHash("spec")
	at := func(n int64) time.Time { return time.Unix(n, 0) }
	post := func(c int64, author, authorID, body string) garden.Comment {
		return garden.Comment{ID: c, Author: author, AuthorID: authorID, Body: body, CreatedAt: at(c)}
	}
	must := func(s string, err error) string {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	planBody := must(garden.NewPlanRevision(id, "## Steps\n1. a", nil, fp))
	rev := garden.Revision{Kind: garden.RevisionPlan, Hash: garden.ContentHash("## Steps\n1. a"), CommentID: 1, SourceSpecFingerprint: fp}

	t.Run("a decision verifies and keeps its reason", func(t *testing.T) {
		v := garden.Verify(id, []garden.Comment{post(1, "lead", "lead-id", planBody),
			post(2, "lead", "lead-id", must(garden.NewPlanDecision(id, rev, garden.DecisionChangesRequested, "Why not?"))),
			post(3, "someone", "other-id", "Looks good to me, ship it.")}, policy)
		if len(v.Problems) != 0 || len(v.PlanDecisions) != 1 || v.PlanDecisions[0].Decision != garden.DecisionChangesRequested || !strings.Contains(v.PlanDecisions[0].Reason, "Why not?") {
			t.Fatalf("problems %v decisions %+v", v.Problems, v.PlanDecisions)
		}
	})
	t.Run("only an allowed actor decides", func(t *testing.T) {
		v := garden.Verify(id, []garden.Comment{post(1, "lead", "lead-id", planBody),
			post(2, "eve", "eve-id", must(garden.NewPlanDecision(id, rev, garden.DecisionRejected, "")))}, policy)
		if len(v.PlanDecisions) != 0 || len(v.Problems) != 1 || v.Problems[0].Code != "wrong-author" {
			t.Fatalf("problems %v decisions %+v", v.Problems, v.PlanDecisions)
		}
	})
	t.Run("a decision before its revision, or about a specification revision, is nothing", func(t *testing.T) {
		d := must(garden.NewPlanDecision(id, rev, garden.DecisionRejected, ""))
		v := garden.Verify(id, []garden.Comment{post(1, "lead", "lead-id", d), post(2, "lead", "lead-id", planBody)}, policy)
		if len(v.PlanDecisions) != 0 || len(v.Problems) == 0 {
			t.Fatalf("decisions %+v problems %v", v.PlanDecisions, v.Problems)
		}
		spec := garden.Revision{Kind: garden.RevisionSpec, Hash: fp, CommentID: 1, SourceSpecFingerprint: fp}
		if _, err := garden.NewPlanDecision(id, spec, garden.DecisionRejected, ""); err == nil {
			t.Fatal("a decision could be written about a specification revision")
		}
	})
	t.Run("an edited reason breaks the record", func(t *testing.T) {
		d := must(garden.NewPlanDecision(id, rev, garden.DecisionChangesRequested, "Why not?"))
		v := garden.Verify(id, []garden.Comment{post(1, "lead", "lead-id", planBody), post(2, "lead", "lead-id", strings.Replace(d, "Why not?", "Fine.", 1))}, policy)
		if len(v.PlanDecisions) != 0 || len(v.Problems) != 1 || v.Problems[0].Code != "edited-hash-mismatch" {
			t.Fatalf("problems %v decisions %+v", v.Problems, v.PlanDecisions)
		}
	})
	t.Run("unknown decisions and stray fields are malformed", func(t *testing.T) {
		d := must(garden.NewPlanDecision(id, rev, garden.DecisionRejected, ""))
		bad := strings.Replace(d, `"decision":"rejected"`, `"decision":"maybe"`, 1)
		stray := strings.Replace(planBody, `"kind":"revision"`, `"kind":"revision","decision":"approved"`, 1)
		v := garden.Verify(id, []garden.Comment{post(1, "lead", "lead-id", planBody), post(2, "lead", "lead-id", bad), post(3, "lead", "lead-id", stray)}, policy)
		if len(v.PlanDecisions) != 0 || len(v.Problems) != 2 {
			t.Fatalf("problems %v", v.Problems)
		}
	})
}

func TestPlanApprovalRespectsSelfApprovalPolicy(t *testing.T) {
	id := garden.IssueIdentity{Repo: 7, Issue: 9}
	text := "Plan for the issue."
	body, err := garden.NewPlanRevision(id, text, nil, "source")
	if err != nil {
		t.Fatal(err)
	}
	rev := garden.Revision{Kind: garden.RevisionPlan, Hash: garden.ContentHash(text), CommentID: 1, SourceSpecFingerprint: "source"}
	decision, err := garden.NewPlanDecision(id, rev, garden.DecisionApproved, "Approved.", "external")
	if err != nil {
		t.Fatal(err)
	}
	for _, forbid := range []bool{false, true} {
		v := garden.Verify(id, []garden.Comment{{ID: 1, AuthorID: "1001", Body: body}, {ID: 2, AuthorID: "1001", Body: decision}}, garden.ApprovalPolicy{AllowedActorIDs: []string{"1001"}, ForbidSelfApproval: forbid})
		if (len(v.PlanDecisions) == 0) != forbid {
			t.Fatalf("forbid=%v decisions=%v problems=%v", forbid, v.PlanDecisions, v.Problems)
		}
	}
}

func TestSpecificationFingerprintIsDerivedFromContent(t *testing.T) {
	for _, fp := range []string{"", garden.ContentHash("spec"), garden.ContentHash("other"), "forged"} {
		t.Run(fp, func(t *testing.T) {
			id := garden.IssueIdentity{Repo: 10, Issue: 20}
			body, err := garden.EncodeCommentEnvelope(garden.CommentEnvelope{Kind: garden.CommentRevision, RepoID: id.Repo, IssueID: id.Issue, RevisionKind: garden.RevisionSpec, ContentHash: garden.ContentHash("spec"), SourceSpecFingerprint: fp}, "spec")
			if err != nil {
				t.Fatal(err)
			}
			v := garden.Verify(id, []garden.Comment{{ID: 1, AuthorID: "lead", Body: body}}, garden.ApprovalPolicy{})
			valid := fp == "" || fp == garden.ContentHash("spec")
			if valid {
				if len(v.Problems) != 0 || len(v.Revisions) != 1 || v.Revisions[0].SourceSpecFingerprint != garden.ContentHash("spec") {
					t.Fatalf("valid: %+v", v)
				}
			} else if len(v.Problems) == 0 || len(v.Revisions) != 0 {
				t.Fatalf("forged: %+v", v)
			}
		})
	}
}
