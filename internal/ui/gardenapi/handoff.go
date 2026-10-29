package gardenapi

import (
	"context"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/handoff"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/launch"
)

// HandoffRequest names the work to read. An empty AttemptID means the Issue's newest attempt; with no attempt
// the work has no code deliverable.
type HandoffRequest struct {
	IssueID   uint64
	AttemptID string
}

// FileChange is one changed file. Binary files carry no line counts. Path is sanitized for display.
type FileChange struct {
	Path     string
	Add, Del int
	Binary   bool
}

// ChangeTotals are the totals over every changed file, also when Files is truncated.
type ChangeTotals struct{ Files, Add, Del int }

// HandoffChanges is what the attempt changed against its base. Available is false when the retained worktree
// could not be read; Reason is then a fixed phrase.
type HandoffChanges struct {
	Available                     bool
	Reason                        string
	BaseSHA, FinalHead, FinalTree string
	Dirty                         bool
	Files                         []FileChange
	Totals                        ChangeTotals
	Truncated                     bool
}

// HandoffReviewState is where the optional automatic review stands.
type HandoffReviewState string

const (
	HandoffReviewNone             HandoffReviewState = "none" // no pass: review was off or never authorized
	HandoffReviewAuthorizedNotRun HandoffReviewState = "authorized-not-run"
	HandoffReviewRunning          HandoffReviewState = "running"
	HandoffReviewChecking         HandoffReviewState = "checking"
	HandoffReviewFailed           HandoffReviewState = "failed"
	HandoffReviewStopped          HandoffReviewState = "stopped"
	HandoffReviewBlocked          HandoffReviewState = "blocked"
	HandoffReviewCompleted        HandoffReviewState = "completed"
	HandoffReviewUnknown          HandoffReviewState = "unknown" // the pass store could not be read
)

// HandoffReview is the review evidence of the newest pass. FixesAfterReview means the reviewer's own fixes moved
// the head after the head it reviewed; NoSecondReview means the head now in the worktree was never reviewed.
type HandoffReview struct {
	State            HandoffReviewState
	PassID           string
	PassRevision     uint64
	Code             string
	ReviewedHead     string
	FinalHead        string
	FixesAfterReview bool
	NoSecondReview   bool
	ReportRef        string
	ReviewUnknown    bool
}

// HandoffCheck is one configured check and what was recorded for it. Outcome is passed, failed, skipped or
// unknown; a check without a recorded result is unknown.
type HandoffCheck struct {
	Name     string
	Outcome  string
	ExitCode *int
	EndedAt  *time.Time
}

// HandoffPR is the linked pull request and one fresh read of it.
type HandoffPR struct {
	Linked    bool
	Number    int
	URL       string
	HeadSHA   string
	Observed  *handoff.PRObservation
	ReadErr   handoff.ReadError
	FetchedAt time.Time
}

// HandoffEntryView is one verified handoff record. Text is the human's words, sanitized.
type HandoffEntryView struct {
	CommentID int64
	Decision  garden.HandoffDecision
	Author    string
	At        time.Time
	Text      string
	Revoked   bool
}

// HandoffDecisionState is the latest decision for this work. State is a handoff.DecisionState or "unknown" when
// the comment log could not be read.
type HandoffDecisionState struct {
	State string
	Entry *HandoffEntryView
	Stale []handoff.StaleReason
}

// HandoffAction says whether an action is possible now; Code and Message explain a refusal.
type HandoffAction struct {
	Allowed bool
	Code    Code
	Message string
}

// HandoffActions is every action the human may take from the handoff screen.
type HandoffActions struct {
	Accept, RequestChanges, Revoke, CompleteNonCode, Rework, OpenPR, Cleanup HandoffAction
}

// HandoffView is the whole handoff of one piece of work. Every section that could not be read says so instead of
// looking green.
type HandoffView struct {
	Stamp     Stamp
	IssueID   uint64
	AttemptID string // empty: no attempt, non-code work only
	FetchedAt time.Time

	SpecLabel string // short revision hash with the approval comment ID
	PlanLabel string
	Outcome   string // one sanitized line built from facts

	Changes HandoffChanges
	Review  HandoffReview
	Checks  []HandoffCheck
	// ChecksRecorded is false when no configured check has a recorded result: "no checks recorded", never green.
	ChecksRecorded bool
	// Concerns lists unresolved findings, only when ConcernsKnown: a report that could not be resolved is unknown.
	Concerns      []string
	ConcernsKnown bool

	PR         HandoffPR
	Decision   HandoffDecisionState
	Completion handoff.Completion
	History    []HandoffEntryView // past decisions, oldest first, at most 20
	Actions    HandoffActions
	// InputsCurrent is false when the approved specification or plan moved since the attempt ran.
	InputsCurrent bool
}

// HandoffDecisionRequest asks to preview one decision. Text is required for changes requested and completion.
// TargetCommentID names the handoff comment to withdraw, for a revocation only.
type HandoffDecisionRequest struct {
	IssueID         uint64
	AttemptID       string
	Decision        garden.HandoffDecision
	Text            string
	TargetCommentID int64
}

// HandoffDecisionPreview freezes everything a confirmation covers and is handed back unchanged to DecideHandoff.
// Acknowledgments are the codes the UI must show and the human must accept; they are recorded in Body.
type HandoffDecisionPreview struct {
	Stamp           Stamp
	IssueID         uint64
	AttemptID       string
	Actor           string
	ActorID         string
	Identity        handoff.Identity
	Decision        garden.HandoffDecision
	Text            string // the human's own trimmed words
	TargetCommentID int64
	Acknowledgments []string
	Summary         string // the one-line header the record will carry
	Body            string // the exact human text recorded after the header: Text plus the acknowledgments
	PRNumber        int64
	PRHeadSHA       string
	// PROpen, PRMerged and PRBaseRef are the pull request state the preview was computed against, so a merge or
	// retarget between preview and confirmation changes the digest.
	PROpen, PRMerged bool
	PRBaseRef        string
	Digest           string
}

// HandoffDecisionView is the result of DecideHandoff. State is PlanWriteUnknown when the record may exist.
type HandoffDecisionView struct {
	Stamp     Stamp
	IssueID   uint64
	AttemptID string
	State     PlanWriteState
	CommentID int64
	Decision  garden.HandoffDecision
	Replayed  bool
	Err       *Error
}

// ReworkPreview is what a confirmed rework would do: one more run of the retained attempt with the verified
// requested-changes text. Review is the freshly resolved automatic review a new pass would authorize.
type ReworkPreview struct {
	Stamp             Stamp
	IssueID           uint64
	AttemptID         string
	FeedbackCommentID int64
	Feedback          string
	AccountName       string
	Provider          agents.ProviderID
	Native            launch.Options
	// Escalated means the implementer's native mode needs its own acknowledgment.
	Escalated bool
	Review    LaunchReview
}

// ReworkStartRequest confirms a rework preview. EscalationConfirmed acknowledges the implementer's dangerous
// native mode, ReviewEscalationConfirmed the reviewer's.
type ReworkStartRequest struct {
	Preview                   ReworkPreview
	Confirmed                 bool
	EscalationConfirmed       bool
	ReviewEscalationConfirmed bool
}

// HandoffBackend is the optional human handoff. Nothing here merges, closes, cleans, pushes or relabels.
type HandoffBackend interface {
	Handoff(context.Context, HandoffRequest) (HandoffView, error)
	PreviewHandoffDecision(context.Context, HandoffDecisionRequest) (HandoffDecisionPreview, error)
	DecideHandoff(context.Context, HandoffDecisionPreview, bool) (HandoffDecisionView, error)
	PreviewRework(ctx context.Context, issue uint64, attemptID string) (ReworkPreview, error)
	StartRework(context.Context, ReworkStartRequest) (LaunchAttemptView, error)
	// OpenHandoffPR opens the attempt's linked pull request in the desktop browser.
	OpenHandoffPR(ctx context.Context, issue uint64, attemptID string) error
}

const (
	// CodeHandoffChanged means what was previewed no longer matches the work, the review, the pull request or the
	// log, so nothing was recorded.
	CodeHandoffChanged Code = "handoff-changed"
	// CodeHandoffBlocked means the action is not possible in the current state.
	CodeHandoffBlocked Code = "handoff-blocked"
	// CodeHandoffUnknown means a record may have been written; refresh before deciding again.
	CodeHandoffUnknown Code = "handoff-unknown"
)
