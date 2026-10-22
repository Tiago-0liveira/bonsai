package planrev

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
)

func approvalRequest(e *env, hash string) DecisionRequest {
	rv := e.review()
	return DecisionRequest{Target: e.tgt, RevisionHash: hash, Fingerprint: rv.Gate.Fingerprint, SpecHash: rv.Gate.Head.Hash, ActorID: viewer, Decision: garden.DecisionApproved}
}

func TestApprovalConfirmationRechecksHeadsAndActor(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*env, *DecisionRequest)
		want   error
	}{
		{"plan head moved", func(e *env, r *DecisionRequest) { e.publish(goodPlan + "\n\nNew revision.") }, ErrHeadsMoved},
		{"spec head moved", func(e *env, r *DecisionRequest) {
			e.postSpec(goodSpec+"\n\n## Constraints\nChanged", e.spec.RevisionHash)
		}, ErrHeadsMoved},
		{"actor changed", func(e *env, r *DecisionRequest) { r.ActorID = other }, ErrActorChanged},
		{"missing actor", func(e *env, r *DecisionRequest) { r.ActorID = "" }, ErrActorChanged},
		{"missing spec head", func(e *env, r *DecisionRequest) { r.SpecHash = "" }, ErrHeadsMoved},
		{"lost authority", func(e *env, r *DecisionRequest) { r.Target.ReadOnly = true }, ErrNotApproved},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			pub := e.publish(goodPlan)
			req := approvalRequest(e, pub.Hash)
			tc.change(e, &req)
			before := e.writes()
			_, err := e.svc.Decide(e.ctx, req)
			// A lost authority invalidates the whole read, including Current.
			if tc.name == "lost authority" {
				if err == nil {
					t.Fatal("lost authority accepted")
				}
			} else if !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
			if e.writes() != before {
				t.Fatal("refused confirmation wrote")
			}
		})
	}
}

func TestApprovedSnapshotSurvivesMovedHeads(t *testing.T) {
	for _, which := range []string{"spec", "plan"} {
		t.Run(which, func(t *testing.T) {
			e := newEnv(t)
			pub := e.publish(goodPlan)
			req := approvalRequest(e, pub.Hash)
			if _, err := e.svc.Decide(e.ctx, req); err != nil {
				t.Fatal(err)
			}
			if !e.review().DownstreamAvailable() {
				t.Fatal("exact pair not Ready")
			}
			before := e.writes()
			if _, err := e.svc.Decide(e.ctx, req); err != nil || e.writes() != before {
				t.Fatalf("replay=%v", err)
			}
			snapshot := e.review().Approved.Content
			if which == "spec" {
				e.postSpec(goodSpec+"\n\n## Constraints\nChanged", e.spec.RevisionHash)
			} else {
				e.publish(goodPlan + "\n\nNew revision.")
			}
			rv := e.review()
			if rv.Ready || rv.DownstreamAvailable() || !rv.StaleApproval || rv.Approved == nil || rv.Approved.Hash != pub.Hash || rv.Approved.Content != snapshot {
				t.Fatalf("lost stale snapshot: %+v", rv)
			}
			before = e.writes()
			if _, err := e.svc.Decide(e.ctx, req); err == nil || e.writes() != before {
				t.Fatalf("stale confirmation accepted: %v", err)
			}
		})
	}
}

func TestExternalApprovalRequiresVerifiedProtocol(t *testing.T) {
	for _, tc := range []string{"verified", "tampered", "unauthorized", "wrong revision", "wrong spec", "label only", "prose only", "unknown schema", "before gate one", "superseded target"} {
		t.Run(tc, func(t *testing.T) {
			e := newEnv(t)
			pub := e.publish(goodPlan)
			rv := e.review()
			rev := rv.Current.Revision
			actor := viewer
			if tc == "unauthorized" {
				actor = other
			}
			if tc == "wrong revision" {
				rev.Hash = strings.Repeat("f", 64)
			}
			if tc == "wrong spec" {
				rev.SourceSpecFingerprint = strings.Repeat("f", 64)
			}
			if tc == "before gate one" {
				// A decision about a newly changed spec precedes validation of that spec.
				old := e.spec.RevisionHash
				e.postSpec(goodSpec+"\n\n## Constraints\nChanged", old)
				log, _ := e.ap.Read(e.ctx, e.tgt)
				source := log.Verification.CurrentSpecFingerprint
				text := Compose(goodPlan, e.spec, e.base)
				text = strings.ReplaceAll(text, e.spec.Fingerprint, source)
				body, _ := garden.NewPlanRevision(garden.IssueIdentity{Repo: repo, Issue: issue}, text, []string{pub.Hash}, source)
				c, err := e.fake.AppendComment(e.ctx, repo, issue, body)
				if err != nil {
					t.Fatal(err)
				}
				rev = garden.Revision{Kind: garden.RevisionPlan, Hash: garden.ContentHash(text), CommentID: c.ID, SourceSpecFingerprint: source}
			}
			if tc == "superseded target" {
				e.publish(goodPlan + "\n\nNew revision.")
			}
			body, err := garden.NewPlanDecision(garden.IssueIdentity{Repo: repo, Issue: issue}, rev, garden.DecisionApproved, "Approved this exact plan.")
			if err != nil {
				t.Fatal(err)
			}
			switch tc {
			case "tampered":
				body += "\nEdited afterwards."
			case "unknown schema":
				body = strings.Replace(body, `"schema_version":1`, `"schema_version":99`, 1)
			case "label only":
				body = "" // the fixture already carries bonsai:stage/ready from gate one
			case "prose only":
				body = "I approve the latest plan. Ready!"
			}
			if body != "" {
				e.fake.AppendCommentAs(repo, issue, "phone-user", actor, body)
			}
			if tc == "before gate one" {
				p, err := e.ap.Preview(e.ctx, e.tgt)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = e.ap.Validate(e.ctx, p); err != nil {
					t.Fatal(err)
				}
			}
			before := e.writes()
			got := e.review()
			if got.Ready != (tc == "verified") {
				t.Fatalf("Ready=%v (%s)", got.Ready, got.Unavailable)
			}
			if tc == "verified" {
				d := got.Approved.Decisions[0]
				if d.Author != "phone-user" || d.AuthorID != viewer || d.CreatedAt.IsZero() || got.VerifiedAt.IsZero() || d.Source != "" {
					t.Fatalf("external provenance: %+v", d)
				}
			}
			if e.writes() != before {
				t.Fatal("external read wrote")
			}
		})
	}
}

func TestChangedSpecMustPassGateOneAgain(t *testing.T) {
	e := newEnv(t)
	e.publish(goodPlan)
	e.postSpec(goodSpec+"\n\n## Constraints\nChanged", e.spec.RevisionHash)
	if rv := e.review(); rv.Gate.DownstreamAvailable() || rv.CanDecide(garden.DecisionApproved) {
		t.Fatal("new spec reused gate one")
	}
	p, err := e.ap.Preview(e.ctx, e.tgt)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = e.ap.Validate(e.ctx, p); err != nil {
		t.Fatal(err)
	}
	rv := e.review()
	if !rv.Gate.DownstreamAvailable() || rv.Ready || rv.Current != nil {
		t.Fatalf("old plan accepted for new spec: %+v", rv)
	}
}

func TestConcurrentHeadAfterFinalReadNeverGrantsReady(t *testing.T) {
	for _, when := range []string{"before append", "after append"} {
		t.Run(when, func(t *testing.T) {
			e := newEnv(t)
			p := e.publish(goodPlan)
			req := approvalRequest(e, p.Hash)
			change := func() { e.postSpec(goodSpec+"\n\n## Constraints\nNew limit.", e.spec.RevisionHash) }
			if when == "before append" {
				e.fake.BeforeNextAppend(change)
			} else {
				e.fake.AfterNextAppend(change)
			}
			if _, err := e.svc.Decide(e.ctx, req); !errors.Is(err, ErrUnverified) {
				t.Fatalf("got %v", err)
			}
			if e.review().Ready {
				t.Fatal("concurrent head granted Ready")
			}
		})
	}
}

func TestDocumentedExternalRecord(t *testing.T) {
	e := newEnv(t)
	p := e.publish(goodPlan)
	// The fixed human text/hash is the phone-postable protocol documented in docs/garden.md.
	body := `<!-- bonsai:garden/v1 {"schema_version":1,"kind":"decision","repo_id":9,"issue_id":7,"revision_hash":"PLAN_HASH","revision_comment_id":PLAN_COMMENT_ID,"source_spec_fingerprint":"SPEC_FINGERPRINT","record_hash":"7e0019a3769b3e37d8a83d04a70720b100041b63e8000b03eafc85071a7a67bf","decision":"approved","source":"external"} -->` + "\nApproved."
	body = strings.ReplaceAll(body, "PLAN_HASH", p.Hash)
	body = strings.ReplaceAll(body, "PLAN_COMMENT_ID", fmt.Sprint(p.CommentID))
	body = strings.ReplaceAll(body, "SPEC_FINGERPRINT", e.spec.Fingerprint)
	e.fake.AppendCommentAs(repo, issue, "phone-user", viewer, body)
	if rv := e.review(); !rv.Ready || rv.Approved.Decisions[0].Source != "external" {
		t.Fatalf("documented protocol failed: %+v", rv)
	}
}

func TestReadOnlyAuthorityPreservesApprovedSnapshotButNotReady(t *testing.T) {
	e := newEnv(t)
	pub := e.publish(goodPlan)
	if _, err := e.svc.Decide(e.ctx, approvalRequest(e, pub.Hash)); err != nil {
		t.Fatal(err)
	}
	target := e.tgt
	target.ReadOnly = true
	rv, err := e.svc.Review(e.ctx, target)
	if err != nil || rv.Ready || rv.DownstreamAvailable() || rv.Approved == nil || rv.Approved.Hash != pub.Hash || rv.CanDecide(garden.DecisionApproved) {
		t.Fatalf("read-only state=%+v err=%v", rv, err)
	}
}

func TestPlanSourceMustNameAVerifiedSpecification(t *testing.T) {
	for _, changed := range []string{"revision hash", "fingerprint", "specification ordinal"} {
		t.Run(changed, func(t *testing.T) {
			e := newEnv(t)
			source := e.spec
			switch changed {
			case "revision hash":
				source.RevisionHash = strings.Repeat("f", 64)
			case "fingerprint":
				source.Fingerprint = strings.Repeat("f", 64)
			case "specification ordinal":
				source.Ordinal++
			}
			text := Compose(goodPlan, source, e.base)
			body, err := garden.NewPlanRevision(garden.IssueIdentity{Repo: repo, Issue: issue}, text, nil, source.Fingerprint)
			if err != nil {
				t.Fatal(err)
			}
			c, _ := e.fake.AppendComment(e.ctx, repo, issue, body)
			rev := garden.Revision{Kind: garden.RevisionPlan, Hash: garden.ContentHash(text), CommentID: c.ID, SourceSpecFingerprint: source.Fingerprint}
			body, _ = garden.NewPlanDecision(garden.IssueIdentity{Repo: repo, Issue: issue}, rev, garden.DecisionApproved, "Approved.")
			e.fake.AppendComment(e.ctx, repo, issue, body)
			rv := e.review()
			if rv.Ready || rv.Approved != nil || len(rv.Revisions) != 1 || rv.Revisions[0].Reviewable() {
				t.Fatalf("unverified source accepted: %+v", rv)
			}
		})
	}
}

func TestSpecificationCannotReuseAnotherRevisionFingerprint(t *testing.T) {
	e := newEnv(t)
	pub := e.publish(goodPlan)
	if _, err := e.svc.Decide(e.ctx, approvalRequest(e, pub.Hash)); err != nil {
		t.Fatal(err)
	}
	content := goodSpec + "\n\n## Constraints\nChanged meaning."
	body, err := garden.EncodeCommentEnvelope(garden.CommentEnvelope{Kind: garden.CommentRevision, RepoID: repo, IssueID: issue, RevisionKind: garden.RevisionSpec, ContentHash: garden.ContentHash(content), Parents: []string{e.spec.RevisionHash}, SourceSpecFingerprint: e.spec.Fingerprint}, content)
	if err != nil {
		t.Fatal(err)
	}
	e.fake.AppendComment(e.ctx, repo, issue, body)
	p, err := e.ap.Preview(e.ctx, e.tgt)
	if err != nil {
		t.Fatal(err)
	}
	if p.CanValidate() {
		t.Fatal("forged specification was validatable")
	}
	if _, err := e.ap.Validate(e.ctx, p); err == nil {
		t.Fatal("forged specification revalidated")
	}
	if e.review().Ready {
		t.Fatal("old plan approval accepted a different specification")
	}
}

func TestDuplicateContentDoesNotRetargetDecision(t *testing.T) {
	e := newEnv(t)
	pub := e.publish(goodPlan)
	if _, err := e.svc.Decide(e.ctx, approvalRequest(e, pub.Hash)); err != nil {
		t.Fatal(err)
	}
	rev := e.review().Approved.Revision
	body, _ := garden.NewPlanRevision(garden.IssueIdentity{Repo: repo, Issue: issue}, rev.Content, rev.Parents, rev.SourceSpecFingerprint)
	duplicate, _ := e.fake.AppendComment(e.ctx, repo, issue, body)
	rv := e.review()
	if rv.Ready || rv.Approved == nil || rv.Approved.CommentID != rev.CommentID || rv.Latest.CommentID != duplicate.ID || len(rv.Latest.Decisions) != 0 {
		t.Fatalf("decision retargeted to duplicate: %+v", rv)
	}
}

func TestPlanApprovalRevocationIntegrityAndAudit(t *testing.T) {
	for _, mode := range []string{"valid", "unauthorized", "tampered", "deleted expected", "edited expected", "changed target expected"} {
		t.Run(mode, func(t *testing.T) {
			e := newEnv(t)
			pub := e.publish(goodPlan)
			approval, err := e.svc.Decide(e.ctx, approvalRequest(e, pub.Hash))
			if err != nil {
				t.Fatal(err)
			}
			body, err := garden.EncodeCommentEnvelope(garden.CommentEnvelope{Kind: garden.CommentRevocation, RepoID: repo, IssueID: issue, TargetCommentID: approval.CommentID}, "Revoked.")
			if err != nil {
				t.Fatal(err)
			}
			author := viewer
			if mode == "unauthorized" {
				author = other
			}
			if mode == "tampered" {
				body += "\nEdited"
			}
			revocation := e.fake.AppendCommentAs(repo, issue, "reviewer", author, body)
			if strings.Contains(mode, "expected") {
				i, _ := e.fake.GetIssue(e.ctx, repo, issue)
				m, _ := garden.DecodeMetadata(i.Metadata)
				m.Revocations = []garden.RevocationRecord{{ApprovalCommentID: approval.CommentID, RevocationCommentID: revocation.ID}}
				next, _ := garden.EncodeMetadata(m)
				if err := e.fake.ReplaceMetadata(e.ctx, repo, issue, i.Metadata, next); err != nil {
					t.Fatal(err)
				}
				switch mode {
				case "deleted expected":
					e.fake.DeleteComment(repo, issue, revocation.ID)
				case "edited expected":
					e.fake.EditComment(repo, issue, revocation.ID, body+"\nEdited")
				case "changed target expected":
					body, _ = garden.EncodeCommentEnvelope(garden.CommentEnvelope{Kind: garden.CommentRevocation, RepoID: repo, IssueID: issue, TargetCommentID: 1}, "Revoked.")
					e.fake.EditComment(repo, issue, revocation.ID, body)
				}
			}
			rv := e.review()
			if rv.Ready || rv.DownstreamAvailable() {
				t.Fatal("revocation left plan Ready")
			}
			if len(rv.Revisions) != 1 || len(rv.Revisions[0].Decisions) != 1 || rv.Revisions[0].Decisions[0].CommentID != approval.CommentID {
				t.Fatalf("approval audit lost: %+v", rv)
			}
			if mode == "valid" && (rv.Approved != nil || rv.Revisions[0].State == StateApproved) {
				t.Fatalf("revoked plan still approved: %+v", rv)
			}
		})
	}
}

func TestRevokedApprovalNeedsANewDecision(t *testing.T) {
	e := newEnv(t)
	pub := e.publish(goodPlan)
	req := approvalRequest(e, pub.Hash)
	first, err := e.svc.Decide(e.ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := garden.EncodeCommentEnvelope(garden.CommentEnvelope{Kind: garden.CommentRevocation, RepoID: repo, IssueID: issue, TargetCommentID: first.CommentID}, "Revoked.")
	e.fake.AppendComment(e.ctx, repo, issue, body)
	rv := e.review()
	if rv.Ready || !rv.Revisions[0].Decisions[0].Revoked {
		t.Fatal("revocation authority or audit lost")
	}
	second, err := e.svc.Decide(e.ctx, req)
	if err != nil || second.Replayed || second.CommentID == first.CommentID || !e.review().Ready {
		t.Fatalf("fresh decision = %+v, %v", second, err)
	}
}

func TestRevocationBeforeDecisionDoesNotRevokeIt(t *testing.T) {
	e := newEnv(t)
	e.publish(goodPlan)
	rev := e.review().Current.Revision
	body, _ := garden.EncodeCommentEnvelope(garden.CommentEnvelope{Kind: garden.CommentRevocation, RepoID: repo, IssueID: issue, TargetCommentID: rev.CommentID + 2}, "Too early.")
	e.fake.AppendComment(e.ctx, repo, issue, body)
	body, _ = garden.NewPlanDecision(garden.IssueIdentity{Repo: repo, Issue: issue}, rev, garden.DecisionApproved, "Approved.")
	e.fake.AppendComment(e.ctx, repo, issue, body)
	if rv := e.review(); !rv.Ready || rv.Approved.Decisions[0].Revoked {
		t.Fatalf("future revocation applied: %+v", rv)
	}
}

func TestRevokingLatestDecisionDoesNotRestoreSupersededApproval(t *testing.T) {
	e := newEnv(t)
	pub := e.publish(goodPlan)
	if _, err := e.svc.Decide(e.ctx, approvalRequest(e, pub.Hash)); err != nil {
		t.Fatal(err)
	}
	rev := e.review().Approved.Revision
	body, _ := garden.NewPlanDecision(garden.IssueIdentity{Repo: repo, Issue: issue}, rev, garden.DecisionRejected, "Rejected.")
	rejection, _ := e.fake.AppendComment(e.ctx, repo, issue, body)
	if e.review().Ready {
		t.Fatal("superseded approval retained authority")
	}
	body, _ = garden.EncodeCommentEnvelope(garden.CommentEnvelope{Kind: garden.CommentRevocation, RepoID: repo, IssueID: issue, TargetCommentID: rejection.ID}, "Revoked rejection.")
	e.fake.AppendComment(e.ctx, repo, issue, body)
	rv := e.review()
	if rv.Ready || rv.Approved != nil || rv.Revisions[0].State == StateApproved || len(rv.Revisions[0].Decisions) != 2 {
		t.Fatalf("superseded approval restored: %+v", rv)
	}
}
