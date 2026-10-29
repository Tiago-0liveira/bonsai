// Package approve is gate one of the Garden workflow: it decides, from verified
// state only, whether an Issue's specification is approved, and records the approval.
// Approval is a signed-by-GitHub comment bound to one specification revision; the
// Issue body, labels and acceptance metadata are only projections of it. Nothing
// here starts a planner, an agent, a worktree or a pull request.
package approve

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/spec"
)

// State is where gate one stands for an Issue.
type State string

const (
	// StateDraft means the specification was never approved.
	StateDraft State = "draft"
	// StateFinishing means a verified approval of the current head exists but the stage label does not reflect it yet. Validating completes the projection.
	StateFinishing State = "finishing"
	// StateApproved means the verified approval exists and the Issue has left Draft.
	StateApproved State = "approved"
	// StateStale means an earlier revision was approved and the head has moved on.
	StateStale State = "stale"
	// StateRevoked means the approval of the head was revoked.
	StateRevoked State = "revoked"
	// StateUnverified means the Issue claims approval (a ready label or acceptance metadata) without a verified
	// record. A claim in the Issue body or labels is never proof.
	StateUnverified State = "unverified"
	// StateAttention means the Issue's records cannot be trusted at all: incomplete or tampered log, bad
	// metadata, conflicting labels or competing heads.
	StateAttention State = "attention"
)

// BlockerKind classifies a reason validation is unavailable.
type BlockerKind string

const (
	BlockIncomplete     BlockerKind = "incomplete"
	BlockIntegrity      BlockerKind = "integrity"
	BlockMetadata       BlockerKind = "metadata"
	BlockLabels         BlockerKind = "labels"
	BlockNoSpec         BlockerKind = "no-specification"
	BlockCompetingHeads BlockerKind = "competing-heads"
	BlockStage          BlockerKind = "stage"
	BlockRequirement    BlockerKind = "requirement"
	BlockQuestion       BlockerKind = "question"
)

// Blocker is one reason the specification cannot be validated now.
type Blocker struct {
	Kind    BlockerKind
	Message string
}

// RecordState is the integrity state of one retained approval record.
type RecordState string

const (
	RecordValid      RecordState = "valid"
	RecordSuperseded RecordState = "superseded"
	RecordRevoked    RecordState = "revoked"
)

// Record is one approval comment, kept for audit even when it no longer counts.
type Record struct {
	CommentID    int64
	Author       string
	AuthorID     string
	At           time.Time
	RevisionHash string
	Fingerprint  string
	State        RecordState
}

// Assessment is the verified gate-one state of one Issue. It is a pure function of the Issue and its
// verification, so a stale cache can never grant approval.
type Assessment struct {
	State State
	// Head is the single current specification revision, nil when there is none or heads compete.
	Head *garden.Revision
	// Ordinal numbers Head among the Issue's specification revisions, oldest first (s1, s2, ...).
	Ordinal     int
	Fingerprint string
	// Approval is the verified approval of Head, nil when there is none.
	Approval *garden.Approval
	// Reason explains an attention state (stale, revoked, unverified, attention); empty otherwise.
	Reason string
	// Blockers is why Validate is unavailable; empty means it may run.
	Blockers []Blocker
	// From is the stage label the Issue carries; validating moves it to ready.
	From    garden.Stage
	Records []Record
}

// CanValidate reports whether the explicit Validate action may run.
func (a Assessment) CanValidate() bool { return len(a.Blockers) == 0 && a.State != StateApproved }

// DownstreamAvailable reports whether actions that build on an approved specification (planning, launching)
// may be offered. Any attention state makes them unavailable; nothing is corrected remotely.
func (a Assessment) DownstreamAvailable() bool { return a.State == StateApproved }

// Assess derives gate one from an Issue and its verification.
func Assess(issue garden.Issue, v garden.Verification) Assessment {
	c := garden.Classify(issue.Labels)
	a := Assessment{State: StateDraft, From: c.Stage, Records: records(v)}
	block := func(k BlockerKind, format string, args ...any) {
		a.Blockers = append(a.Blockers, Blocker{k, fmt.Sprintf(format, args...)})
	}
	attention := func(reason string) {
		a.State, a.Reason = StateAttention, reason
	}
	m, merr := garden.DecodeMetadata(issue.Metadata)
	switch {
	case !v.Complete:
		block(BlockIncomplete, "GitHub's comment log could not be read completely.")
		attention("comment log is incomplete")
		return a
	case len(v.Problems) > 0:
		block(BlockIntegrity, "%s fail verification (%s).", plural(len(v.Problems), "record"), problemCodes(v.Problems))
		attention("revision or approval integrity problem")
		return a
	case merr != nil:
		block(BlockMetadata, "The Issue's Bonsai metadata line is missing or invalid.")
		attention("invalid managed metadata")
		return a
	case len(c.Problems) > 0:
		block(BlockLabels, "The Issue's bonsai: labels conflict or are invalid; Bonsai will not correct them for you.")
		attention("invalid managed labels")
		return a
	}
	var heads []garden.Revision
	for _, h := range v.Heads {
		if h.Kind == garden.RevisionSpec {
			heads = append(heads, h)
		}
	}
	switch len(heads) {
	case 0:
		block(BlockNoSpec, "No specification revision exists yet. Edit and save the specification first.")
		if m.Accepted || c.Stage != garden.StageDraft {
			a.State, a.Reason = StateUnverified, "approval is claimed but no specification revision exists"
		}
		return a
	case 1:
	default:
		block(BlockCompetingHeads, "Several specification revisions compete. Resolve the conflict first.")
		attention("competing revision heads")
		return a
	}
	head := heads[0]
	a.Head, a.Fingerprint = &head, head.SourceSpecFingerprint
	for _, r := range v.Revisions {
		if r.Kind != garden.RevisionSpec {
			continue
		}
		a.Ordinal++
		if r.CommentID == head.CommentID {
			break
		}
	}
	for i := range v.ValidApprovals {
		if v.ValidApprovals[i].RevisionHash == head.Hash && v.ValidApprovals[i].RevisionCommentID == head.CommentID && v.ValidApprovals[i].SourceSpecFingerprint == head.SourceSpecFingerprint {
			ap := v.ValidApprovals[i]
			a.Approval = &ap
			break
		}
	}
	projected := c.Stage != garden.StageDraft
	switch {
	case a.Approval != nil && projected:
		a.State = StateApproved
		return a
	case a.Approval != nil:
		a.State = StateFinishing
	case len(v.RevokedApprovals) > 0:
		a.State, a.Reason = StateRevoked, "the approval of this specification was revoked"
	case len(v.SupersededApprovals) > 0 || m.Accepted && m.AcceptedSpecHash != head.Hash:
		a.State, a.Reason = StateStale, "the specification changed after it was approved"
	case m.Accepted || c.Stage != garden.StageDraft:
		a.State, a.Reason = StateUnverified, "approval is claimed but no verified approval record exists"
	}
	if c.Stage != garden.StageDraft && c.Stage != garden.StageReady {
		block(BlockStage, "The Issue is already %s; its approval cannot be changed from here.", c.Stage)
		return a
	}
	if a.State != StateFinishing {
		a.Blockers = append(a.Blockers, contentBlockers(head.Content)...)
	}
	return a
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

func problemCodes(ps []garden.VerificationProblem) string {
	seen := map[string]bool{}
	var codes []string
	for _, p := range ps {
		if !seen[p.Code] {
			seen[p.Code] = true
			codes = append(codes, p.Code)
		}
	}
	sort.Strings(codes)
	return strings.Join(codes, ", ")
}

func records(v garden.Verification) []Record {
	var out []Record
	add := func(as []garden.Approval, state RecordState) {
		for _, ap := range as {
			s := state
			if ap.Revoked {
				s = RecordRevoked
			}
			out = append(out, Record{CommentID: ap.CommentID, Author: ap.Author, AuthorID: ap.AuthorID, At: ap.CreatedAt,
				RevisionHash: ap.RevisionHash, Fingerprint: ap.SourceSpecFingerprint, State: s})
		}
	}
	add(v.ValidApprovals, RecordValid)
	add(v.RevokedApprovals, RecordRevoked)
	add(v.SupersededApprovals, RecordSuperseded)
	sort.Slice(out, func(i, j int) bool { return out[i].CommentID < out[j].CommentID })
	return out
}

// contentBlockers are the requirements a specification must meet before approval: the sections that say what
// is wanted and how it is judged are filled in, and no open question is left standing.
func contentBlockers(content string) []Blocker {
	s := spec.Parse(content)
	var out []Blocker
	for _, req := range []struct{ name, text string }{{"Summary", s.Summary}, {"Requirements", s.Requirements}, {"Acceptance criteria", s.AcceptanceCriteria}} {
		if strings.TrimSpace(req.text) == "" {
			out = append(out, Blocker{BlockRequirement, req.name + " is empty."})
		}
	}
	for _, q := range OpenQuestions(s.OpenQuestions) {
		out = append(out, Blocker{BlockQuestion, "Open question: " + q})
	}
	return out
}

// OpenQuestions lists the unresolved questions of an "Open questions" section: each non-empty line, without
// its list marker. Checked task items ("[x]") and the placeholders "none" and "n/a" are resolved.
func OpenQuestions(text string) []string {
	var out []string
	for _, line := range strings.Split(garden.NormalizeContent(text), "\n") {
		line = strings.TrimSpace(line)
		for _, marker := range []string{"- ", "* ", "+ "} {
			line = strings.TrimSpace(strings.TrimPrefix(line, marker))
		}
		lower := strings.ToLower(line)
		switch {
		case line == "", strings.HasPrefix(lower, "[x]"):
			continue
		case lower == "none", lower == "none.", lower == "n/a", lower == "-":
			continue
		}
		out = append(out, strings.TrimSpace(strings.TrimPrefix(line, "[ ]")))
	}
	return out
}
