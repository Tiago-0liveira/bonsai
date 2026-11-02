package playgroundapi

import (
	"context"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden/review"
)

// ReviewBackend is the optional local, read-only verification of a Garden
// attempt. The TUI type-asserts it on the Backend; Backend is not widened, and a
// backend without it simply has no review surface.
//
// Reading reports (ReviewPolicy, Reviews, PreviewAccept) changes nothing except
// that Reviews settles finished reviewer runs into the review store. The writes
// are: StartReview (one explicitly confirmed read-only reviewer session),
// DismissFinding and Accept (a local record each), and ReconcileReviews (the
// automatic starts the local policy authorizes).
//
// Accept records the human decision ONLY. It never merges, closes, pushes,
// comments, changes labels, edits an Issue or pull request, stops a session or
// removes a worktree: acceptance, merge and cleanup are three distinct acts and
// this interface has no method for the other two.
//
// Calls fail with an *Error carrying one of the fixed Codes (no provider, Git or
// store text). Policy editing is not here: it lives in Settings through the
// preferences path, and the policy is re-read from it on every call.
type ReviewBackend interface {
	// ReviewPolicy is the effective local policy and whether the configured
	// account can run an enforced read-only scoped-report reviewer now.
	ReviewPolicy(ctx context.Context) (ReviewPolicyView, error)
	// Reviews reads every report revision of the session's Garden attempt, and
	// its acceptance decisions. A session without a Garden attempt is not an
	// error: Reviewable is false with a reason.
	Reviews(ctx context.Context, req ReviewsRequest) (ReviewsView, error)
	// StartReview starts the explicit, manual read-only review run.
	StartReview(ctx context.Context, req StartReviewRequest) (StartReviewView, error)
	// DismissFinding records a reason bound to one finding of one report revision.
	DismissFinding(ctx context.Context, req DismissRequest) (DismissView, error)
	// PreviewAccept computes what accepting would need to acknowledge.
	PreviewAccept(ctx context.Context, req AcceptPreviewRequest) (AcceptPreviewView, error)
	// Accept records the human decision. See the type doc: nothing else happens.
	Accept(ctx context.Context, req AcceptRequest) (AcceptView, error)
	// ReconcileReviews starts the automatic reviewer runs the local policy
	// authorizes and settles finished runs. It is idempotent across restarts.
	ReconcileReviews(ctx context.Context) (ReconcileView, error)
}

// Review limits.
const (
	// MaxRequestIDLen bounds StartReviewRequest.RequestID.
	MaxRequestIDLen = 128
	// ReviewRole is the fixed role label of every reviewer session.
	ReviewRole = "read-only (enforced)"
)

// Codes of the review calls, beside the shared ones.
const (
	CodeReviewRunning       Code = "review-running"
	CodeReadOnlyUnsupported Code = "read-only-unsupported"
	CodeNeedsAcknowledgment Code = "needs-acknowledgment"
	CodeNotConfirmed        Code = "not-confirmed"
	CodePolicyIncomplete    Code = "policy-incomplete"
	CodeNotReviewable       Code = "not-reviewable"
)

// ---- Policy ----

// ReviewCapability is whether the configured account can run a reviewer now.
type ReviewCapability struct {
	// AccountFound: the configured account exists.
	AccountFound bool
	// ReadOnly and PlanFile are the provider's verdicts: an enforced read-only
	// session, and one that may still write exactly the report file.
	ReadOnly, PlanFile bool
	// Supported: ReadOnly && PlanFile && HostingSupported. Only then can a reviewer start.
	Supported bool
	// HostingSupported: the agent runtime can host sessions here.
	HostingSupported bool
	// Reason is a fixed sentence when not Supported (the provider's own fixed
	// refusal text where it has one). Never runtime provider output.
	Reason string
}

type ReviewPolicyView struct {
	Stamp Stamp
	// Enabled: automatic runs are authorized by the local consent.
	Enabled bool
	// AccountID is empty when no account is named.
	AccountID, AccountName, Provider string
	Role                             string // ReviewRole
	Trigger                          review.Trigger
	Scope                            string
	MaxAutoRuns                      int
	RequireCI, BlockOnHigh           bool
	Capability                       ReviewCapability
	// PrefsErr: the preferences could not be read (the policy shown is then off).
	PrefsErr *Error
	// AccountErr: the accounts could not be listed (Capability is then unknown).
	AccountErr *Error
}

// ---- Reviews ----

type ReviewsRequest struct{ SessionID string }

// ReviewFinding is one finding with its dismissal state at this report revision.
type ReviewFinding struct {
	ID             string
	Severity       review.Severity
	Title          string
	Path           string
	Line           int
	Evidence       string
	Recommendation string
	// Dismissed is set only for a dismissal of exactly this revision and commit.
	Dismissed *FindingDismissal
	// PriorDismissal is a label such as "previously dismissed in r2" when an
	// earlier revision dismissed the same finding. The finding is not dismissed.
	PriorDismissal string
}

// FindingDismissal is a reason with the revision and commit it was bound to.
type FindingDismissal struct {
	Reason   string
	At       time.Time
	Revision int
	Commit   string
}

// ReviewReport is one reviewer run. Reports of every revision are listed.
type ReviewReport struct {
	Revision  int
	Commit    string
	Short     string
	Base      review.Base
	State     review.State
	FailCode  review.FailCode // fixed wording key; set for failed, invalid and blocked
	Trigger   review.Trigger
	Auto      bool
	Summary   string
	CreatedAt time.Time
	ReadyAt   time.Time
	Findings  []ReviewFinding // most severe first
	// Superseded: a later ready report of the attempt exists.
	Superseded bool
	// ForOlderCommit: Commit is not the worktree's HEAD (false when HEAD was not read).
	ForOlderCommit bool
	// RunSessionID is the reviewer's own agent session, to open its terminal.
	RunSessionID string
}

// ReviewDecision is one recorded human acceptance. It is not a merge.
type ReviewDecision struct {
	ID             string
	ReportRevision int // 0: accepted without a report
	ReportCommit   string
	ReviewedHead   string
	Short          string
	Unresolved     []review.FindingRef
	Counts         map[review.Severity]int
	CI             review.CISnapshot
	Policy         review.PolicyFlags
	Acknowledged   []string
	DecidedAt      time.Time
	// ForOlderCommit: ReviewedHead is not the worktree's HEAD (false when HEAD was not read).
	ForOlderCommit bool
}

// ReviewsView: the sections fail independently. CI is not part of it; read it
// with Backend.Checks.
type ReviewsView struct {
	Stamp     Stamp
	SessionID string
	// Reviewable is false for sessions without a verified Garden attempt (manual
	// sessions, reviewer sessions, unreadable attempts); Reason says why. Not an error.
	Reviewable bool
	Reason     string
	AttemptID  string

	// Head is the worktree's HEAD; HeadRead is false (HeadErr set, or the
	// worktree is gone) when it was not read.
	Head     string
	HeadRead bool
	HeadErr  *Error

	// Reports are all revisions, newest first; Decisions newest first. ReportsErr
	// is set when the review store could not be read (both are then unknown).
	Reports    []ReviewReport
	Decisions  []ReviewDecision
	ReportsErr *Error
	// RunInFlight: a reviewer run is still running.
	RunInFlight bool
}

// ---- Start ----

// StartReviewRequest is the explicit local "Retry review". RequestID makes a
// double submit one run and a deliberate retry (a new ID) another.
type StartReviewRequest struct {
	SessionID string
	// ExpectHead is the HEAD the human saw; a moved HEAD is CodeStale.
	ExpectHead string
	Confirmed  bool
	RequestID  string
}

type StartReviewView struct {
	Stamp   Stamp
	Report  ReviewReport // state running unless the same request already finished
	Created bool         // false: the same RequestID already existed
}

// ---- Dismiss ----

type DismissRequest struct {
	SessionID      string
	ReportRevision int
	FindingID      string
	Reason         string // 1..500 characters
}

type DismissView struct {
	Stamp     Stamp
	FindingID string
	Dismissal FindingDismissal
	Created   bool // false: it was already dismissed
}

// ---- Accept ----

type AcceptPreviewRequest struct{ SessionID string }

// AcceptPreviewView is what a human sees before accepting, and the token Accept
// takes back unchanged: Accept recomputes the requirements itself and refuses
// (CodeStale) when HEAD, the latest report or the unresolved findings moved.
type AcceptPreviewView struct {
	Stamp          Stamp
	SessionID      string
	AttemptID      string
	HeadSHA        string
	ReportRevision int // 0: no ready report
	ReportCommit   string
	// ReportStale: the report was made for a commit other than HEAD.
	ReportStale bool
	Unresolved  []ReviewFinding
	Counts      map[review.Severity]int
	// CI is the independent CI truth; CIUnavailable when the read failed, never "passed".
	CI review.CISnapshot
	// CIErr is set when CI could not be read.
	CIErr  *Error
	Policy review.PolicyFlags
	// Required lists the acknowledgment codes that must be given.
	Required      []review.Ack
	AcceptAllowed bool
}

type AcceptRequest struct {
	Preview      AcceptPreviewView
	Acknowledged []string
	Confirmed    bool
}

type AcceptView struct {
	Stamp    Stamp
	Decision ReviewDecision
	Created  bool // false: the same decision already existed
}

// ---- Reconcile ----

type ReconcileView struct {
	Stamp Stamp
	// Enabled: the local policy authorizes automatic runs (and has a trigger).
	Enabled bool
	// Unsupported: the agent runtime cannot host sessions here; nothing was started.
	Unsupported bool
	Started     []ReconcileRun
	// Blocked counts runs recorded as blocked (for example an account that cannot run read-only).
	Blocked int
	// Deferred counts planned runs left for a later reconcile (start cap).
	Deferred int
	// Settled counts finished runs whose result was read.
	Settled int
	Err     *Error
}

// ReconcileRun is one run an automatic trigger started.
type ReconcileRun struct {
	AttemptID string
	Revision  int
	Commit    string
}
