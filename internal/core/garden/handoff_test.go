package garden

import (
	"strings"
	"testing"
	"time"
)

func hx(c string, n int) string { return strings.Repeat(c, n) }

func codeRecord(d HandoffDecision) HandoffRecord {
	return HandoffRecord{Decision: d, Deliverable: DeliverableCode, AttemptID: "att-1", SpecRevision: hx("a", 64), SpecApprovalID: 3,
		PlanRevision: hx("b", 64), PlanDecisionID: 5, BaseSHA: hx("1", 40), FinalHead: hx("2", 40), FinalTree: hx("3", 40)}
}

func TestNewHandoffValidation(t *testing.T) {
	id := IssueIdentity{Repo: 1, Issue: 2}
	nonCode := codeRecord(HandoffCompleted)
	nonCode.Deliverable, nonCode.AttemptID, nonCode.FinalHead, nonCode.FinalTree = DeliverableNonCode, "", nonCode.BaseSHA, ""
	mut := func(f func(*HandoffRecord)) HandoffRecord { r := codeRecord(HandoffAccepted); f(&r); return r }
	for _, tc := range []struct {
		name   string
		rec    HandoffRecord
		target int64
		human  string
		ok     bool
	}{
		{"accepted", codeRecord(HandoffAccepted), 0, "Accepted.", true},
		{"changes requested with feedback", codeRecord(HandoffChangesRequested), 0, "Header\n\nFix the test.", true},
		{"changes requested without feedback", codeRecord(HandoffChangesRequested), 0, "Header", false},
		{"non-code completed", nonCode, 0, "Done.\n\nShipped docs elsewhere.", true},
		{"non-code completed without evidence", nonCode, 0, "Done.", false},
		{"non-code with code changes", func() HandoffRecord { r := nonCode; r.FinalHead = hx("9", 40); return r }(), 0, "Done.\n\nx", false},
		{"non-code with PR", func() HandoffRecord { r := nonCode; r.PRNumber = 4; return r }(), 0, "Done.\n\nx", false},
		{"non-code accepted", mut(func(r *HandoffRecord) { r.Deliverable = DeliverableNonCode }), 0, "x", false},
		{"code without attempt", mut(func(r *HandoffRecord) { r.AttemptID = "" }), 0, "x", false},
		{"code without head", mut(func(r *HandoffRecord) { r.FinalHead = "" }), 0, "x", false},
		{"short head", mut(func(r *HandoffRecord) { r.FinalHead = "abc" }), 0, "x", false},
		{"uppercase tree", mut(func(r *HandoffRecord) { r.FinalTree = hx("A", 40) }), 0, "x", false},
		{"short spec", mut(func(r *HandoffRecord) { r.SpecRevision = hx("a", 63) }), 0, "x", false},
		{"missing plan id", mut(func(r *HandoffRecord) { r.PlanDecisionID = 0 }), 0, "x", false},
		{"pr head without number", mut(func(r *HandoffRecord) { r.PRHeadSHA = hx("2", 40) }), 0, "x", false},
		{"pr with head", mut(func(r *HandoffRecord) { r.PRNumber, r.PRHeadSHA = 4, hx("2", 40) }), 0, "x", true},
		{"review pass", mut(func(r *HandoffRecord) { r.ReviewPass, r.ReviewPassRevision = "rp-"+hx("c", 32), 2 }), 0, "x", true},
		{"review pass without revision", mut(func(r *HandoffRecord) { r.ReviewPass = "rp-" + hx("c", 32) }), 0, "x", false},
		{"revision without review pass", mut(func(r *HandoffRecord) { r.ReviewPassRevision = 2 }), 0, "x", false},
		{"bad review pass", mut(func(r *HandoffRecord) { r.ReviewPass, r.ReviewPassRevision = "rp-zz", 1 }), 0, "x", false},
		{"report ref", mut(func(r *HandoffRecord) { r.ReportRef = "ev-" + hx("d", 32) }), 0, "x", true},
		{"bad report ref", mut(func(r *HandoffRecord) { r.ReportRef = "ev-1" }), 0, "x", false},
		{"unknown decision", mut(func(r *HandoffRecord) { r.Decision = "maybe" }), 0, "x", false},
		{"target on accepted", codeRecord(HandoffAccepted), 9, "x", false},
		{"revoked", HandoffRecord{Decision: HandoffRevoked}, 9, "Revoked.", true},
		{"revoked without target", HandoffRecord{Decision: HandoffRevoked}, 0, "Revoked.", false},
		{"revoked with fields", func() HandoffRecord { r := codeRecord(HandoffAccepted); r.Decision = HandoffRevoked; return r }(), 9, "x", false},
		{"marker in text", codeRecord(HandoffAccepted), 0, "see bonsai:garden/v1", false},
		{"text too large", codeRecord(HandoffAccepted), 0, strings.Repeat("x", MaxHandoffTextBytes+1), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewHandoff(id, tc.rec, tc.target, tc.human)
			if (err == nil) != tc.ok {
				t.Fatalf("err = %v, want ok=%v", err, tc.ok)
			}
		})
	}
}

var t0 = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func handoffComment(t *testing.T, id int64, author string, rec HandoffRecord, target int64, human string) Comment {
	t.Helper()
	body, err := NewHandoff(IssueIdentity{Repo: 1, Issue: 2}, rec, target, human)
	if err != nil {
		t.Fatal(err)
	}
	return Comment{ID: id, Author: "login-" + author, AuthorID: author, Body: body, CreatedAt: t0.Add(time.Duration(id) * time.Minute)}
}

func TestVerifyHandoffs(t *testing.T) {
	id := IssueIdentity{Repo: 1, Issue: 2}
	policy := ApprovalPolicy{AllowedActorIDs: []string{"me"}}
	accepted := handoffComment(t, 10, "me", codeRecord(HandoffAccepted), 0, "Accepted.")
	other := codeRecord(HandoffAccepted)
	other.AttemptID = "att-2"
	t.Run("entries oldest first and revocation marks target", func(t *testing.T) {
		revoke := handoffComment(t, 12, "me", HandoffRecord{Decision: HandoffRevoked}, 10, "Revoked.")
		second := handoffComment(t, 11, "me", other, 0, "Accepted.")
		log := VerifyHandoffs(id, []Comment{revoke, second, accepted}, policy)
		if len(log.Problems) != 0 || len(log.Entries) != 3 || log.Entries[0].CommentID != 10 || !log.Entries[0].Revoked || log.Entries[0].RevokedBy != 12 || log.Entries[1].Revoked {
			t.Fatalf("log = %+v", log)
		}
	})
	t.Run("untrusted actor is a problem", func(t *testing.T) {
		log := VerifyHandoffs(id, []Comment{handoffComment(t, 10, "intruder", codeRecord(HandoffAccepted), 0, "x")}, policy)
		if len(log.Entries) != 0 || len(log.Problems) != 1 || log.Problems[0].Code != "wrong-author" {
			t.Fatalf("log = %+v", log)
		}
	})
	t.Run("edited text", func(t *testing.T) {
		c := accepted
		c.Body += " edited"
		log := VerifyHandoffs(id, []Comment{c}, policy)
		if len(log.Entries) != 0 || len(log.Problems) != 1 || log.Problems[0].Code != "edited-hash-mismatch" {
			t.Fatalf("log = %+v", log)
		}
	})
	t.Run("other issue", func(t *testing.T) {
		log := VerifyHandoffs(IssueIdentity{Repo: 1, Issue: 3}, []Comment{accepted}, policy)
		if len(log.Entries) != 0 || len(log.Problems) != 1 || log.Problems[0].Code != "wrong-issue-repo" {
			t.Fatalf("log = %+v", log)
		}
	})
	t.Run("revocation of unknown, later or revoked target", func(t *testing.T) {
		early := handoffComment(t, 9, "me", HandoffRecord{Decision: HandoffRevoked}, 10, "Revoked.")
		unknown := handoffComment(t, 11, "me", HandoffRecord{Decision: HandoffRevoked}, 99, "Revoked.")
		good := handoffComment(t, 12, "me", HandoffRecord{Decision: HandoffRevoked}, 10, "Revoked.")
		again := handoffComment(t, 13, "me", HandoffRecord{Decision: HandoffRevoked}, 10, "Revoked.")
		log := VerifyHandoffs(id, []Comment{early, accepted, unknown, good, again}, policy)
		if len(log.Problems) != 3 || len(log.Entries) != 2 || log.Entries[0].RevokedBy != 12 {
			t.Fatalf("log = %+v", log)
		}
	})
	t.Run("forged envelope with unknown field", func(t *testing.T) {
		c := accepted
		c.Body = strings.Replace(c.Body, `"decision":"accepted"`, `"decision":"accepted","extra":1`, 1)
		if log := VerifyHandoffs(id, []Comment{c}, policy); len(log.Entries) != 0 || len(log.Problems) != 1 {
			t.Fatalf("log = %+v", log)
		}
	})
}

// Old comments must decode and verify exactly as before, and handoff comments must not disturb Verify.
func TestVerifyIgnoresHandoffAndOldRecordsStayValid(t *testing.T) {
	id := IssueIdentity{Repo: 1, Issue: 2}
	policy := ApprovalPolicy{AllowedActorIDs: []string{"me"}}
	spec, err := NewSpecRevision(id, "## Summary\nx", nil)
	if err != nil {
		t.Fatal(err)
	}
	comments := []Comment{{ID: 1, AuthorID: "me", Body: spec, CreatedAt: t0}}
	before := Verify(id, comments, policy)
	if len(before.Problems) != 0 || len(before.Revisions) != 1 {
		t.Fatalf("baseline = %+v", before)
	}
	with := append(comments, handoffComment(t, 2, "me", codeRecord(HandoffAccepted), 0, "Accepted."), handoffComment(t, 3, "intruder", codeRecord(HandoffAccepted), 0, "x"))
	after := Verify(id, with, policy)
	if len(after.Problems) != 0 || len(after.Revisions) != 1 || len(after.Approvals) != 0 {
		t.Fatalf("handoff disturbed Verify: %+v", after)
	}
	if strings.Contains(spec, `"handoff"`) {
		t.Fatal("old envelopes must not carry the handoff field")
	}
}

func TestHandoffFieldRejectedOnOtherKinds(t *testing.T) {
	rec := codeRecord(HandoffAccepted)
	for _, k := range []CommentKind{CommentRevision, CommentApproval, CommentRevocation, CommentDecision} {
		if validEnvelopeFields(CommentEnvelope{Kind: k, Handoff: &rec}) {
			t.Fatalf("kind %s accepted a handoff payload", k)
		}
	}
	if validEnvelopeFields(CommentEnvelope{Kind: CommentHandoff}) {
		t.Fatal("handoff kind without payload accepted")
	}
}
