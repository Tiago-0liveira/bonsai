package review

import (
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// OriginLocal is the only decision origin: acceptance is a human act on this machine.
const OriginLocal = "local"

// MaxCIAge is how old a CI snapshot may be before a required CI check treats it as stale.
const MaxCIAge = 5 * time.Minute

// Ack is an acknowledgment code the human must give before accepting.
type Ack string

const (
	AckOpenHigh    Ack = "open-high"
	AckCINotPassed Ack = "ci-not-passed"
	AckReportStale Ack = "report-stale"
	AckNoReport    Ack = "no-report"
	AckCIStale     Ack = "ci-stale"
)

// CIState is the independent truth about CI, never inferred from a review.
type CIState string

const (
	CIPending     CIState = "pending"
	CIPassed      CIState = "passed"
	CIFailed      CIState = "failed"
	CINone        CIState = "none"
	CIUnavailable CIState = "unavailable"
)

func (s CIState) valid() bool {
	switch s {
	case CIPending, CIPassed, CIFailed, CINone, CIUnavailable:
		return true
	}
	return false
}

// CISnapshot is CI as last fetched, with the commit it describes and when.
type CISnapshot struct {
	State     CIState   `json:"state"`
	HeadSHA   string    `json:"head_sha,omitempty"`
	FetchedAt time.Time `json:"fetched_at,omitempty"`
}

// Dismissal binds a human reason to one finding of one report revision at one commit. It never carries over to a
// later report.
type Dismissal struct {
	AttemptID      string    `json:"attempt_id"`
	ReportRevision int       `json:"report_revision"`
	FindingID      string    `json:"finding_id"`
	Commit         string    `json:"commit"`
	Reason         string    `json:"reason"`
	At             time.Time `json:"at"`
}

// MaxReasonRunes bounds a dismissal reason.
const MaxReasonRunes = 500

// CleanReason trims and validates a dismissal reason: 1..500 runes, valid UTF-8, no control characters.
func CleanReason(s string) (string, error) {
	s = strings.TrimSpace(s)
	if n := utf8.RuneCountInString(s); n == 0 || n > MaxReasonRunes || !utf8.ValidString(s) ||
		strings.IndexFunc(s, unicode.IsControl) >= 0 {
		return "", fmt.Errorf("%w: reason", ErrInvalid)
	}
	return s, nil
}

// PriorDismissal finds the latest dismissal of the same finding fingerprint in an earlier revision of the attempt.
// The finding is shown as previously dismissed but is not dismissed: only a dismissal for this exact revision is.
func PriorDismissal(ds []Dismissal, attempt, findingID string, beforeRevision int) (Dismissal, bool) {
	var best Dismissal
	found := false
	for _, d := range ds {
		if d.AttemptID == attempt && d.FindingID == findingID && d.ReportRevision < beforeRevision &&
			(!found || d.ReportRevision > best.ReportRevision) {
			best, found = d, true
		}
	}
	return best, found
}

// Label is the text for a prior dismissal, for example "previously dismissed in r2".
func (d Dismissal) Label() string {
	return fmt.Sprintf("previously dismissed in r%d", d.ReportRevision)
}

// IsDismissed reports whether the finding is dismissed for exactly this report revision and commit.
func IsDismissed(ds []Dismissal, r Report, findingID string) bool {
	for _, d := range ds {
		if d.AttemptID == r.AttemptID && d.ReportRevision == r.Revision && d.Commit == r.Commit && d.FindingID == findingID {
			return true
		}
	}
	return false
}

// FindingRef names a finding left open at a decision.
type FindingRef struct {
	ID       string   `json:"id"`
	Severity Severity `json:"severity"`
	Title    string   `json:"title"`
	Path     string   `json:"path"`
	Line     int      `json:"line"`
}

// PolicyFlags are the policy flags as they applied when the decision was made.
type PolicyFlags struct {
	RequireCI   bool `json:"require_ci"`
	BlockOnHigh bool `json:"block_on_high"`
}

// Decision records that a human accepted an attempt at a commit. It is not a merge, a close or a cleanup.
type Decision struct {
	ID                   string           `json:"id"`
	AttemptID            string           `json:"attempt_id"`
	ReportRevision       int              `json:"report_revision"`
	ReportCommit         string           `json:"report_commit,omitempty"`
	ReviewedHead         string           `json:"reviewed_head"`
	UnresolvedAtDecision []FindingRef     `json:"unresolved_at_decision,omitempty"`
	UnresolvedCounts     map[Severity]int `json:"unresolved_counts,omitempty"`
	CI                   CISnapshot       `json:"ci"`
	PolicyFlags          PolicyFlags      `json:"policy_flags"`
	Acknowledged         []string         `json:"acknowledged,omitempty"`
	DecidedAt            time.Time        `json:"decided_at"`
	Origin               string           `json:"origin"`
}

// ForOlderCommit marks a decision made for a commit that is no longer HEAD. It stays in history.
func (d Decision) ForOlderCommit(head string) bool { return d.ReviewedHead != head }

// DecisionID is deterministic: one decision per attempt, HEAD and report revision.
func DecisionID(attempt, head string, revision int) string {
	return "dc-" + hashHex(attempt, head, fmt.Sprint(revision))[:20]
}

// PreviewInput is everything AcceptPreview needs; the caller reads each part fresh.
type PreviewInput struct {
	Policy     Policy
	AttemptID  string
	Report     *Report // latest ready report, or nil
	HeadSHA    string
	CI         CISnapshot
	Dismissals []Dismissal
	Now        time.Time
}

// Preview is what a human sees before accepting.
type Preview struct {
	AttemptID      string
	HeadSHA        string
	ReportRevision int
	ReportCommit   string
	// ReportStale: the report was made for a commit other than HEAD. The new code was not reviewed.
	ReportStale bool
	Unresolved  []Finding
	Counts      map[Severity]int
	CI          CISnapshot
	Policy      PolicyFlags
	// Required lists the acknowledgments the human must give; nothing is accepted without them.
	Required      []Ack
	AcceptAllowed bool
}

// Needs reports whether the acknowledgment is required.
func (p Preview) Needs(a Ack) bool {
	for _, r := range p.Required {
		if r == a {
			return true
		}
	}
	return false
}

// AcceptPreview computes the preview. It never accepts anything and never claims a stale report reviewed HEAD.
func AcceptPreview(in PreviewInput) Preview {
	p := Preview{AttemptID: in.AttemptID, HeadSHA: in.HeadSHA, CI: in.CI, Counts: map[Severity]int{},
		Policy:        PolicyFlags{RequireCI: in.Policy.RequireCI, BlockOnHigh: in.Policy.BlockOnHigh},
		AcceptAllowed: ValidSHA(in.HeadSHA) && in.AttemptID != ""}
	if in.Report == nil {
		p.Required = append(p.Required, AckNoReport)
	} else {
		r := *in.Report
		p.ReportRevision, p.ReportCommit = r.Revision, r.Commit
		for _, f := range r.Findings {
			if !IsDismissed(in.Dismissals, r, f.ID) {
				p.Unresolved = append(p.Unresolved, f)
				p.Counts[f.Severity]++
			}
		}
		if r.Commit != in.HeadSHA {
			p.ReportStale = true
			p.Required = append(p.Required, AckReportStale)
		}
		if in.Policy.BlockOnHigh && p.Counts[SeverityHigh] > 0 {
			p.Required = append(p.Required, AckOpenHigh)
		}
	}
	if in.Policy.RequireCI {
		if in.CI.State != CIPassed {
			p.Required = append(p.Required, AckCINotPassed)
		}
		if ciStale(in.CI, in.HeadSHA, in.Now) {
			p.Required = append(p.Required, AckCIStale)
		}
	}
	return p
}

func ciStale(c CISnapshot, head string, now time.Time) bool {
	switch c.State {
	case CIPending, CIPassed, CIFailed:
		return c.HeadSHA != head || c.FetchedAt.IsZero() || now.Sub(c.FetchedAt) > MaxCIAge
	}
	return false
}

// NewDecision validates an acceptance against the HEAD read now and builds the record. It checks, in order: the
// preview is for the current HEAD (ErrStale), the human confirmed, and every required acknowledgment was given.
func NewDecision(p Preview, currentHead string, acks []string, confirmed bool, now time.Time) (Decision, error) {
	if !p.AcceptAllowed {
		return Decision{}, fmt.Errorf("%w: preview", ErrInvalid)
	}
	if p.HeadSHA != currentHead {
		return Decision{}, ErrStale
	}
	if !confirmed {
		return Decision{}, ErrNotConfirmed
	}
	given := map[string]bool{}
	for _, a := range acks {
		given[a] = true
	}
	for _, r := range p.Required {
		if !given[string(r)] {
			return Decision{}, fmt.Errorf("%w: %s", ErrNeedsAcknowledgment, r)
		}
	}
	if p.CI.State == "" {
		p.CI.State = CINone
	}
	d := Decision{ID: DecisionID(p.AttemptID, p.HeadSHA, p.ReportRevision), AttemptID: p.AttemptID,
		ReportRevision: p.ReportRevision, ReportCommit: p.ReportCommit, ReviewedHead: p.HeadSHA, CI: p.CI,
		PolicyFlags: p.Policy, DecidedAt: now, Origin: OriginLocal}
	for _, f := range p.Unresolved {
		d.UnresolvedAtDecision = append(d.UnresolvedAtDecision, FindingRef{ID: f.ID, Severity: f.Severity, Title: f.Title, Path: f.Path, Line: f.Line})
	}
	if len(p.Counts) > 0 {
		d.UnresolvedCounts = map[Severity]int{}
		for k, v := range p.Counts {
			d.UnresolvedCounts[k] = v
		}
	}
	for _, r := range p.Required { // record only the acknowledgments that were required and given
		d.Acknowledged = append(d.Acknowledged, string(r))
	}
	sort.Strings(d.Acknowledged)
	return d, nil
}
