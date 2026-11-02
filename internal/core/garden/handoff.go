package garden

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// MaxHandoffTextBytes bounds the human text of a handoff comment (header plus the reviewer's words).
const MaxHandoffTextBytes = 8 << 10

// HandoffDecision is what the human decided about a finished attempt.
type HandoffDecision string

const (
	// HandoffAccepted accepts exactly the recorded head and tree of a code attempt.
	HandoffAccepted HandoffDecision = "accepted"
	// HandoffChangesRequested sends the attempt back with feedback.
	HandoffChangesRequested HandoffDecision = "changes_requested"
	// HandoffCompleted is the explicit completion of work that has no code deliverable.
	HandoffCompleted HandoffDecision = "completed"
	// HandoffRevoked withdraws the earlier handoff comment named by the envelope's TargetCommentID.
	HandoffRevoked HandoffDecision = "revoked"
)

func (d HandoffDecision) valid() bool {
	return d == HandoffAccepted || d == HandoffChangesRequested || d == HandoffCompleted || d == HandoffRevoked
}

// Deliverable says what a piece of work produces.
type Deliverable string

const (
	DeliverableCode    Deliverable = "code"
	DeliverableNonCode Deliverable = "non-code"
)

// HandoffRecord binds a decision to the exact specification, plan, code and review it was made about.
type HandoffRecord struct {
	Decision           HandoffDecision `json:"decision"`
	Deliverable        Deliverable     `json:"deliverable,omitempty"`
	AttemptID          string          `json:"attempt_id,omitempty"`
	SpecRevision       string          `json:"spec_revision,omitempty"`
	SpecApprovalID     int64           `json:"spec_approval_id,omitempty"`
	PlanRevision       string          `json:"plan_revision,omitempty"`
	PlanDecisionID     int64           `json:"plan_decision_id,omitempty"`
	BaseSHA            string          `json:"base_sha,omitempty"`
	FinalHead          string          `json:"final_head,omitempty"`
	FinalTree          string          `json:"final_tree,omitempty"`
	ReviewPass         string          `json:"review_pass,omitempty"`
	ReviewPassRevision uint64          `json:"review_pass_revision,omitempty"`
	ReportRef          string          `json:"report_ref,omitempty"`
	PRNumber           int64           `json:"pr_number,omitempty"`
	PRHeadSHA          string          `json:"pr_head_sha,omitempty"`
}

// HandoffEntry is one verified handoff comment.
type HandoffEntry struct {
	CommentID int64
	AuthorID  string
	Author    string
	CreatedAt time.Time
	Record    HandoffRecord
	// Text is the whole human text, covered by the record hash.
	Text string
	// Revoked is set when a verified revocation names this entry; RevokedBy is that comment.
	Revoked   bool
	RevokedBy int64
}

// Detail is the human's own words: the text after the one-line header.
func (e HandoffEntry) Detail() string { return handoffDetail(e.Text) }

func handoffDetail(human string) string {
	_, rest, _ := strings.Cut(strings.TrimSpace(human), "\n")
	return strings.TrimSpace(rest)
}

// HandoffLog is the verified handoff history of one Issue, oldest first.
type HandoffLog struct {
	Entries  []HandoffEntry
	Problems []VerificationProblem
}

// NewHandoff renders the comment body that records rec. targetCommentID names the revoked comment and is only
// for HandoffRevoked. The caller posts it; nothing here writes.
func NewHandoff(id IssueIdentity, rec HandoffRecord, targetCommentID int64, human string) (string, error) {
	if id.Repo == 0 || id.Issue == 0 {
		return "", fmt.Errorf("%w: identity", ErrInvalidApproval)
	}
	e := CommentEnvelope{Kind: CommentHandoff, RepoID: id.Repo, IssueID: id.Issue, TargetCommentID: targetCommentID, Handoff: &rec}
	if !validHandoffEnvelope(e) || !validHandoffText(rec, human) {
		return "", fmt.Errorf("%w: handoff record", ErrInvalidApproval)
	}
	return EncodeCommentEnvelope(e, human)
}

func validHandoffText(rec HandoffRecord, human string) bool {
	if len(human) > MaxHandoffTextBytes || strings.Contains(human, "bonsai:garden/") {
		return false
	}
	switch rec.Decision {
	case HandoffChangesRequested, HandoffCompleted:
		return handoffDetail(human) != ""
	}
	return true
}

func hexOf(s string, lens ...int) bool {
	ok := false
	for _, n := range lens {
		ok = ok || len(s) == n
	}
	if !ok {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func hexOrEmpty(s string, lens ...int) bool { return s == "" || hexOf(s, lens...) }

func prefixedHex(s, prefix string) bool {
	return strings.HasPrefix(s, prefix) && hexOf(strings.TrimPrefix(s, prefix), 32)
}

func validHandoffEnvelope(e CommentEnvelope) bool {
	r := e.Handoff
	if r == nil || e.RevisionKind != "" || e.ContentHash != "" || len(e.Parents) != 0 || e.SourceSpecFingerprint != "" ||
		e.RevisionHash != "" || e.RevisionCommentID != 0 || e.Decision != "" || e.Source != "" || !r.Decision.valid() {
		return false
	}
	if r.Decision == HandoffRevoked {
		return e.TargetCommentID > 0 && *r == HandoffRecord{Decision: HandoffRevoked}
	}
	if e.TargetCommentID != 0 {
		return false
	}
	if r.Deliverable != DeliverableCode && r.Deliverable != DeliverableNonCode {
		return false
	}
	if !hexOf(r.SpecRevision, 64) || r.SpecApprovalID <= 0 || !hexOf(r.PlanRevision, 64) || r.PlanDecisionID <= 0 {
		return false
	}
	if !hexOrEmpty(r.BaseSHA, 40, 64) || !hexOrEmpty(r.FinalHead, 40, 64) || !hexOrEmpty(r.FinalTree, 40, 64) || !hexOrEmpty(r.PRHeadSHA, 40, 64) || len(r.AttemptID) > 128 {
		return false
	}
	if (r.ReviewPass != "") != (r.ReviewPassRevision > 0) || r.ReviewPass != "" && !prefixedHex(r.ReviewPass, "rp-") {
		return false
	}
	if r.ReportRef != "" && !prefixedHex(r.ReportRef, "ev-") {
		return false
	}
	if r.PRNumber < 0 || r.PRHeadSHA != "" && r.PRNumber == 0 {
		return false
	}
	if r.Decision == HandoffCompleted {
		return r.Deliverable == DeliverableNonCode && r.PRNumber == 0 && (r.FinalHead == "" || r.FinalHead == r.BaseSHA)
	}
	// Only completion carries a non-code deliverable: accepting or sending back needs a head to be about.
	return r.Deliverable == DeliverableCode && r.AttemptID != "" && r.BaseSHA != "" && r.FinalHead != "" && r.FinalTree != ""
}

// VerifyHandoffs derives the handoff history of one Issue from its ordered comment log. Records by actors the
// policy does not trust, edited records and records of other Issues are problems, never entries.
func VerifyHandoffs(identity IssueIdentity, comments []Comment, policy ApprovalPolicy) HandoffLog {
	var log HandoffLog
	if len(comments) > MaxComments {
		log.Problems = append(log.Problems, VerificationProblem{Code: "too-many-comments", Err: ErrTooManyComments})
		return log
	}
	problem := func(c Comment, code string, err error) {
		log.Problems = append(log.Problems, VerificationProblem{CommentID: c.ID, Code: code, Err: err})
	}
	ordered := append([]Comment(nil), comments...)
	sort.Slice(ordered, func(i, j int) bool {
		if !ordered[i].CreatedAt.Equal(ordered[j].CreatedAt) {
			return ordered[i].CreatedAt.Before(ordered[j].CreatedAt)
		}
		return ordered[i].ID < ordered[j].ID
	})
	allowed := map[string]bool{}
	for _, a := range policy.AllowedActorIDs {
		allowed[a] = true
	}
	seen := map[int64]Comment{}
	index := map[int64]int{}
	for _, c := range ordered {
		if previous, ok := seen[c.ID]; ok {
			if previous != c {
				problem(c, "duplicate-comment-id", ErrInvalidRevision)
			}
			continue
		}
		seen[c.ID] = c
		if len(c.Body) > MaxCommentBodyBytes {
			problem(c, "comment-too-large", ErrInvalidRevision)
			continue
		}
		if !strings.Contains(c.Body, "bonsai:garden/") {
			continue
		}
		e, text, err := decodeEnvelope(c.Body)
		if err != nil {
			code := "malformed-envelope"
			if strings.Contains(err.Error(), ErrUnsupportedSchema.Error()) {
				code = "unsupported-schema"
			}
			problem(c, code, err)
			continue
		}
		if e.RepoID != identity.Repo || e.IssueID != identity.Issue {
			problem(c, "wrong-issue-repo", fmt.Errorf("%w: envelope identity mismatch", ErrInvalidRevision))
			continue
		}
		if e.Kind != CommentHandoff {
			continue
		}
		if !validHandoffEnvelope(e) || !validHandoffText(*e.Handoff, text) {
			problem(c, "malformed-envelope", ErrInvalidApproval)
			continue
		}
		if e.RecordHash == "" || e.RecordHash != ContentHash(text) {
			problem(c, "edited-hash-mismatch", ErrInvalidApproval)
			continue
		}
		if c.AuthorID == "" || !allowed[c.AuthorID] {
			problem(c, "wrong-author", ErrInvalidApproval)
			continue
		}
		if e.Handoff.Decision == HandoffRevoked {
			i, ok := index[e.TargetCommentID]
			if !ok || log.Entries[i].Revoked || log.Entries[i].Record.Decision == HandoffRevoked {
				problem(c, "invalid-revocation-target", ErrInvalidApproval)
				continue
			}
			log.Entries[i].Revoked, log.Entries[i].RevokedBy = true, c.ID
		}
		index[c.ID] = len(log.Entries)
		log.Entries = append(log.Entries, HandoffEntry{CommentID: c.ID, AuthorID: c.AuthorID, Author: c.Author, CreatedAt: c.CreatedAt, Record: *e.Handoff, Text: text})
	}
	return log
}
