package handoff

import (
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
)

// AwaitingReviewState is the launch state of an attempt that finished and waits for its human.
const AwaitingReviewState = "awaiting-human-review"

// CleanedState is the launch state of an attempt whose worktree was removed. Accepted work stays done after
// cleanup, because the merged pull request, not the worktree, is its evidence.
const CleanedState = "cleaned"

// CompletionState is whether the work counts as done, and if not, what it waits for.
type CompletionState string

const (
	CompletionNotReady         CompletionState = "not-ready"
	CompletionAwaitingDecision CompletionState = "awaiting-decision"
	CompletionChangesRequested CompletionState = "changes-requested"
	CompletionAccepted         CompletionState = "accepted"
	CompletionDone             CompletionState = "done"
	CompletionStale            CompletionState = "stale"
	CompletionBlocked          CompletionState = "blocked"
	CompletionUnknown          CompletionState = "unknown"
)

// Reason is a stable machine-readable code explaining a state.
type Reason string

const (
	ReasonDirtyWorktree     Reason = "dirty-worktree"
	ReasonAttemptNotDone    Reason = "attempt-not-finished"
	ReasonHeadUnknown       Reason = "head-unknown"
	ReasonAcceptanceStale   Reason = "acceptance-stale"
	ReasonAcceptanceRevoked Reason = "acceptance-revoked"
	ReasonPRNotPublished    Reason = "pr-not-published"
	ReasonPRReadFailed      Reason = "pr-read-failed"
	ReasonPROffline         Reason = "offline"
	ReasonPRNotFound        Reason = "pr-not-found"
	ReasonWrongPR           Reason = "wrong-pr"
	ReasonPRClosedUnmerged  Reason = "pr-closed-unmerged"
	ReasonPRNotMerged       Reason = "pr-not-merged"
	ReasonMergedHeadDiffers Reason = "merged-head-differs"
	ReasonHasCodeChanges    Reason = "has-code-changes"
	ReasonPRHeadChanged     Reason = "pr-head-changed"
	ReasonLogUnreadable     Reason = "log-unreadable"
	ReasonWrongBase         Reason = "wrong-base"
	ReasonPRReadStale       Reason = "pr-read-stale"
)

// ReadError says why a fresh pull request read produced no observation.
type ReadError string

const (
	ReadNone        ReadError = "none"
	ReadOffline     ReadError = "offline"
	ReadUnavailable ReadError = "unavailable"
	ReadNotFound    ReadError = "not-found"
)

// MaxPRObservationAge bounds how old a pull request read may be and still prove a merge.
const MaxPRObservationAge = 2 * time.Minute

// LinkedPR is the pull request the attempt published, as recorded by the attempt.
type LinkedPR struct {
	Number  int64
	HeadSHA string
	// BaseRef is the branch the attempt published against; empty when unknown.
	BaseRef string
}

// PRObservation is one fresh read of the pull request on GitHub.
type PRObservation struct {
	Number         int64
	Open, Merged   bool
	MergedAt       *time.Time
	MergeCommitSHA string
	HeadSHA        string
	BaseRef        string
	FetchedAt      time.Time
}

// Input is everything Derive decides from; it observes nothing itself.
type Input struct {
	Identity Identity
	// Dirty means the attempt's worktree has uncommitted changes.
	Dirty        bool
	AttemptState string
	PR           *LinkedPR
	Observed     *PRObservation
	ReadErr      ReadError
	Decision     DecisionStatus
	Now          time.Time
}

// Evidence is what the completion rests on.
type Evidence struct {
	Source         string // "github-pr" or "handoff-record"
	CommentID      int64
	PRNumber       int64
	MergeCommitSHA string
	MergedAt       *time.Time
	VerifiedAt     time.Time
}

// Completion is the derived completion state of one piece of work.
type Completion struct {
	State    CompletionState
	Reasons  []Reason
	Evidence Evidence
}

// Derive decides completion. Only a merge of exactly the accepted head, or an explicit non-code completion,
// yields done: missing or unreadable evidence never does.
func Derive(in Input) Completion {
	if in.Identity.Deliverable == garden.DeliverableNonCode {
		return deriveNonCode(in)
	}
	var notReady []Reason
	if in.Dirty {
		notReady = append(notReady, ReasonDirtyWorktree)
	}
	if in.AttemptState != AwaitingReviewState && in.AttemptState != CleanedState {
		notReady = append(notReady, ReasonAttemptNotDone)
	}
	if in.Identity.FinalHead == "" {
		notReady = append(notReady, ReasonHeadUnknown)
	}
	if len(notReady) > 0 {
		return Completion{State: CompletionNotReady, Reasons: notReady}
	}
	if c, ok := undecided(in); ok {
		return c
	}
	e := in.Decision.Entry
	record := Evidence{Source: "handoff-record", CommentID: e.CommentID, VerifiedAt: in.Now}
	switch e.Record.Decision {
	case garden.HandoffChangesRequested:
		return Completion{State: CompletionChangesRequested, Evidence: record}
	case garden.HandoffAccepted:
	default:
		return Completion{State: CompletionAwaitingDecision}
	}
	if in.PR == nil {
		return Completion{State: CompletionAccepted, Reasons: []Reason{ReasonPRNotPublished}, Evidence: record}
	}
	o := in.Observed
	if in.ReadErr != "" && in.ReadErr != ReadNone || o == nil {
		reason := ReasonPRReadFailed
		switch in.ReadErr {
		case ReadOffline:
			reason = ReasonPROffline
		case ReadNotFound:
			reason = ReasonPRNotFound
		}
		return Completion{State: CompletionUnknown, Reasons: []Reason{reason}, Evidence: record}
	}
	if o.FetchedAt.IsZero() || in.Now.Sub(o.FetchedAt) > MaxPRObservationAge {
		return Completion{State: CompletionUnknown, Reasons: []Reason{ReasonPRReadStale}, Evidence: record}
	}
	rec := e.Record
	switch {
	case o.Number != in.PR.Number || rec.PRNumber != 0 && o.Number != rec.PRNumber:
		return Completion{State: CompletionBlocked, Reasons: []Reason{ReasonWrongPR}, Evidence: record}
	case !o.Merged && !o.Open:
		return Completion{State: CompletionBlocked, Reasons: []Reason{ReasonPRClosedUnmerged}, Evidence: record}
	case !o.Merged && (o.HeadSHA != rec.FinalHead || in.PR.HeadSHA != rec.FinalHead || rec.PRHeadSHA != "" && o.HeadSHA != rec.PRHeadSHA):
		// Someone pushed to the PR after the acceptance, so the human never saw what would merge.
		return Completion{State: CompletionStale, Reasons: []Reason{ReasonPRHeadChanged}, Evidence: record}
	case !o.Merged:
		return Completion{State: CompletionAccepted, Reasons: []Reason{ReasonPRNotMerged}, Evidence: record}
	case in.PR.BaseRef != "" && o.BaseRef != "" && in.PR.BaseRef != o.BaseRef:
		return Completion{State: CompletionBlocked, Reasons: []Reason{ReasonWrongBase}, Evidence: record}
	case o.HeadSHA != rec.FinalHead || in.PR.HeadSHA != rec.FinalHead || rec.PRHeadSHA != "" && o.HeadSHA != rec.PRHeadSHA:
		return Completion{State: CompletionBlocked, Reasons: []Reason{ReasonMergedHeadDiffers}, Evidence: record}
	}
	// A squash or rebase merge makes a new commit, so MergeCommitSHA is recorded rather than compared.
	return Completion{State: CompletionDone, Evidence: Evidence{Source: "github-pr", CommentID: e.CommentID, PRNumber: o.Number,
		MergeCommitSHA: o.MergeCommitSHA, MergedAt: o.MergedAt, VerifiedAt: in.Now}}
}

// undecided handles the decision states that need no further evidence; ok is false for a current decision.
func undecided(in Input) (Completion, bool) {
	switch in.Decision.State {
	case DecisionCurrent:
		if in.Decision.Entry != nil {
			return Completion{}, false
		}
	case DecisionStale:
		return Completion{State: CompletionStale, Reasons: append([]Reason{ReasonAcceptanceStale}, staleReasons(in.Decision.Stale)...)}, true
	case DecisionRevoked:
		return Completion{State: CompletionAwaitingDecision, Reasons: []Reason{ReasonAcceptanceRevoked}}, true
	}
	return Completion{State: CompletionAwaitingDecision}, true
}

func staleReasons(in []StaleReason) []Reason {
	out := make([]Reason, len(in))
	for i, r := range in {
		out[i] = Reason(r)
	}
	return out
}

func deriveNonCode(in Input) Completion {
	id := in.Identity
	if in.Dirty || id.BaseSHA != "" && id.FinalHead != "" && id.FinalHead != id.BaseSHA {
		return Completion{State: CompletionBlocked, Reasons: []Reason{ReasonHasCodeChanges}}
	}
	if id.AttemptID != "" && (id.BaseSHA == "" || id.FinalHead == "") {
		// An attempt ran: without both commits observed, "nothing changed" is a guess, not a fact.
		return Completion{State: CompletionUnknown, Reasons: []Reason{ReasonHeadUnknown}}
	}
	if c, ok := undecided(in); ok {
		return c
	}
	e := in.Decision.Entry
	if e.Record.Decision != garden.HandoffCompleted {
		return Completion{State: CompletionAwaitingDecision}
	}
	return Completion{State: CompletionDone, Evidence: Evidence{Source: "handoff-record", CommentID: e.CommentID, VerifiedAt: in.Now}}
}
