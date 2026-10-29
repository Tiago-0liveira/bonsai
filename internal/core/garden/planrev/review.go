package planrev

import (
	"fmt"
	"strings"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/approve"
)

// State is where one plan revision stands after the reviewer's latest decision about it.
type State string

const (
	// StateAwaiting means no decision was recorded.
	StateAwaiting State = "awaiting"
	// StateChangesRequested means the reviewer asked for a new revision; this one stays in the history.
	StateChangesRequested State = "changes-requested"
	// StateRejected means the reviewer discarded it: no plan is accepted.
	StateRejected State = "rejected"
	// StateApproved means gate two accepted exactly this revision for its source specification.
	StateApproved State = "approved"
)

// Revision is one plan revision read back from the Issue's comment log, with its integrity verified again.
type Revision struct {
	// Ordinal numbers the Issue's plan revisions oldest first (r1, r2, ...).
	Ordinal int
	garden.Revision
	Sections []Section
	// BaseCommit is the inspected code base the plan names.
	BaseCommit          string
	SourceSpecHash      string
	SourceSpecCommentID int64
	// Trusted means the authenticated author of the comment is the viewer. A revision by anyone else is shown
	// but can never be current, reviewed or approved.
	Trusted bool
	// Missing lists required sections the revision lacks.
	Missing []string
	// Consistent means the document's own statement of the specification fingerprint and the inspected commit
	// agree with the verified envelope. A revision that says otherwise is unverifiable.
	Consistent bool
	// Stale means the revision was written for a specification other than the current head.
	Stale bool
	// Superseded means a later revision names it as a parent.
	Superseded bool
	State      State
	// Decisions are every verified decision about this revision, oldest first.
	Decisions []garden.PlanDecision
}

// Reviewable reports whether the revision can be decided: verified, complete, the viewer's and consistent.
func (r Revision) Reviewable() bool {
	return r.Trusted && r.Consistent && len(r.Missing) == 0 && r.BaseCommit != ""
}

// Reason is the text of the revision's latest decision, empty when there is none.
func (r Revision) Reason() string {
	if len(r.Decisions) == 0 {
		return ""
	}
	return ReasonOf(r.Decisions[len(r.Decisions)-1])
}

// ReasonOf extracts the reviewer's reason from a decision: the human text after its one-line header.
func ReasonOf(d garden.PlanDecision) string {
	_, reason, _ := strings.Cut(strings.TrimSpace(d.Reason), "\n\n")
	return strings.TrimSpace(reason)
}

// Review is the verified plan-review state of one Issue. Current, Latest and Approved are different questions
// and never collapse into one: Latest is the newest revision anyone published, Current is the single revision
// under review, Approved is the revision gate two accepted. A decision always names an exact revision hash, never
// "whichever is latest".
type Review struct {
	Number int64
	Title  string
	Gate   approve.Assessment
	// Revisions is every verified plan revision, oldest first, superseded ones included.
	Revisions []Revision
	Latest    *Revision
	// Current is the one trusted, non-stale, non-rejected head, nil when there is none or several compete.
	Current *Revision
	// Approved retains the last verified approved snapshot, including when its source is stale.
	Approved *Revision
	// Ready requires both gates, one exact current pair and a complete verified log.
	Ready         bool
	StaleApproval bool
	VerifiedAt    time.Time
	// Competing means several revisions are heads at once; the next published revision supersedes all of them.
	Competing bool
	// Untrusted counts revisions by other authors.
	Untrusted int
	// NewerComments counts prose comments on the Issue after the latest plan revision. Bonsai cannot read them as
	// records and never acts on them: they are a hint to look at the Issue on GitHub.
	NewerComments int
	// Unavailable says why publishing and deciding are not offered; empty when they are.
	Unavailable string
}

// DownstreamAvailable requires the exact verified pair; callers must still offer a separate local launch.
func (r Review) DownstreamAvailable() bool { return r.Ready }

// CanPublish reports whether a plan revision may be published now.
func (r Review) CanPublish() bool { return r.Unavailable == "" }

// CanDecide reports whether the current revision may be given decision d now: either decision on one that
// awaits a decision, and a rejection also on one whose changes were requested and never delivered.
func (r Review) CanDecide(d garden.Decision) bool {
	if r.Unavailable != "" || r.Current == nil || !r.Current.Reviewable() {
		return false
	}
	switch r.Current.State {
	case StateAwaiting:
		return d == garden.DecisionChangesRequested || d == garden.DecisionRejected || d == garden.DecisionApproved
	case StateChangesRequested:
		return d == garden.DecisionRejected
	}
	return false
}

// ByHash finds a revision by its content hash.
func (r Review) ByHash(hash string) (Revision, bool) {
	for _, rev := range r.Revisions {
		if rev.Hash == hash {
			return rev, true
		}
	}
	return Revision{}, false
}

// Heads returns the revisions no later revision supersedes.
func (r Review) Heads() []Revision {
	var out []Revision
	for _, rev := range r.Revisions {
		if !rev.Superseded {
			out = append(out, rev)
		}
	}
	return out
}

// Derive computes the review state from one verified read. It is pure: a cached or stale log can only produce a
// state that is as old as the log, never a state the log does not support.
func Derive(log approve.Log, viewer string) Review {
	v := log.Verification
	rv := Review{Number: log.Issue.Number, Title: log.Issue.Title, Gate: approve.Assess(log.Issue, v)}
	switch {
	case v.ApprovalReadOnly:
		rv.Unavailable = "Plan approval requires current Write access to the repository. Historical records are retained."
	case !rv.Gate.DownstreamAvailable():
		rv.Unavailable = "The specification is not approved: validate it first. Bonsai never reviews a plan against a label or a claim in the Issue."
	case rv.Gate.Head == nil || rv.Gate.Fingerprint == "" || rv.Gate.Approval == nil:
		rv.Unavailable = "The approved specification could not be verified."
	}
	fingerprint := v.CurrentSpecFingerprint
	var specs []garden.Revision
	for _, r := range v.Revisions {
		if r.Kind == garden.RevisionSpec {
			specs = append(specs, r)
		}
	}
	byHash := map[string]int{}
	byID := map[int64]int{}
	for _, r := range v.Revisions {
		if r.Kind != garden.RevisionPlan {
			continue
		}
		secs := Parse(r.Content)
		rev := Revision{Ordinal: len(rv.Revisions) + 1, Revision: r, Sections: secs, BaseCommit: field(secs, SectionBase, "Commit"),
			Trusted: viewer != "" && r.AuthorID == viewer, Missing: Missing(secs), State: StateAwaiting,
			Stale: fingerprint == "" || r.SourceSpecFingerprint != fingerprint}
		for i, spec := range specs {
			if field(secs, SectionSpec, "Specification") == fmt.Sprintf("s%d", i+1) && field(secs, SectionSpec, "Revision") == spec.Hash &&
				field(secs, SectionSpec, "Fingerprint") == spec.SourceSpecFingerprint && r.SourceSpecFingerprint == spec.SourceSpecFingerprint &&
				before(spec.CreatedAt, spec.CommentID, r.CreatedAt, r.CommentID) {
				rev.Consistent, rev.SourceSpecHash, rev.SourceSpecCommentID = true, spec.Hash, spec.CommentID
				break
			}
		}
		rev.Stale = rev.Stale || rev.Consistent && (rv.Gate.Head == nil || rev.SourceSpecHash != rv.Gate.Head.Hash || rev.SourceSpecCommentID != rv.Gate.Head.CommentID)
		byHash[r.Hash] = len(rv.Revisions)
		byID[r.CommentID] = len(rv.Revisions)
		rv.Revisions = append(rv.Revisions, rev)
		if !rev.Trusted {
			rv.Untrusted++
		}
	}
	for _, rev := range rv.Revisions {
		for _, p := range rev.Parents {
			if i, ok := byHash[p]; ok {
				rv.Revisions[i].Superseded = true
			}
		}
	}
	for _, d := range v.PlanDecisions {
		i, ok := byID[d.RevisionCommentID]
		if !ok {
			continue
		}
		rev := &rv.Revisions[i]
		if d.Decision == garden.DecisionApproved && !approvedAt(v, *rev, d) {
			continue
		}

		rev.Decisions = append(rev.Decisions, d)
		if d.Revoked {
			rev.State = StateAwaiting
			continue
		}
		switch d.Decision {
		case garden.DecisionChangesRequested:
			rev.State = StateChangesRequested
		case garden.DecisionRejected:
			rev.State = StateRejected
		case garden.DecisionApproved:
			rev.State = StateApproved
		}
	}
	var eligible []int
	for i := range rv.Revisions {
		rev := &rv.Revisions[i]
		// An approval of a revision that no longer verifies, or answers an older specification, stays in the
		// history but accepts nothing.
		if rev.State == StateApproved && rev.Reviewable() {
			rv.Approved = rev
		}
		if !rev.Superseded && rev.Trusted && !rev.Stale && rev.State != StateRejected {
			eligible = append(eligible, i)
		}
	}
	if len(rv.Revisions) > 0 {
		rv.Latest = &rv.Revisions[len(rv.Revisions)-1]
	}
	switch len(eligible) {
	case 0:
	case 1:
		rv.Current = &rv.Revisions[eligible[0]]
	default:
		rv.Competing = true
	}
	if rv.Approved != nil {
		rv.Ready = rv.Unavailable == "" && !rv.Approved.Stale && rv.Current != nil && rv.Current.Hash == rv.Approved.Hash && len(rv.Heads()) == 1
		rv.StaleApproval = !rv.Ready
	}
	var after int64
	if rv.Latest != nil {
		after = rv.Latest.CommentID
	}
	for _, c := range log.Comments {
		if c.ID > after && !strings.Contains(c.Body, "bonsai:garden/") {
			rv.NewerComments++
		}
	}
	return rv
}

// before follows the verifier's server ordering, independent of pagination order.
func before(a time.Time, aid int64, b time.Time, bid int64) bool {
	return a.Before(b) || a.Equal(b) && aid < bid
}

// FromVerification projects the same gates for Board/Table without fetching or writing.
func FromVerification(issue garden.Issue, v garden.Verification) Review {
	viewer := ""
	for _, a := range v.Approvals {
		viewer = a.AuthorID
	}
	return Derive(approve.Log{Issue: issue, Verification: v}, viewer)
}

// approvedAt rejects external decisions that named an old/competing head or arrived
// before gate one. Later validation must never retroactively authorize a decision.
func approvedAt(v garden.Verification, rev Revision, d garden.PlanDecision) bool {
	prior := make(map[string]garden.Revision)
	parents := make(map[string]bool)
	for _, r := range v.Revisions {
		if before(r.CreatedAt, r.CommentID, d.CreatedAt, d.CommentID) {
			prior[r.Hash] = r
			for _, p := range r.Parents {
				parents[p] = true
			}
		}
	}
	var spec garden.Revision
	specs, plans := 0, 0
	for _, r := range prior {
		if parents[r.Hash] {
			continue
		}
		switch r.Kind {
		case garden.RevisionSpec:
			specs++
			spec = r
		case garden.RevisionPlan:
			plans++
			if r.Hash != rev.Hash || r.CommentID != rev.CommentID {
				return false
			}
		}
	}
	if specs != 1 || plans != 1 || spec.SourceSpecFingerprint != d.SourceSpecFingerprint || spec.Hash != rev.SourceSpecHash || spec.CommentID != rev.SourceSpecCommentID {
		return false
	}
	for _, a := range v.Approvals {
		if !a.Revoked && a.RevisionHash == spec.Hash && a.RevisionCommentID == spec.CommentID && a.SourceSpecFingerprint == spec.SourceSpecFingerprint && before(a.CreatedAt, a.CommentID, d.CreatedAt, d.CommentID) {
			return true
		}
	}
	return false
}
