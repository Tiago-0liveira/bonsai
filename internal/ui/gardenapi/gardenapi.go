// Package gardenapi is the contract between the TUI and the Garden backend.
// The TUI codes against these plain view types only, so it never imports the
// GitHub adapter, the association store or os/exec, and every UI test can run
// on the fake package.
//
// Every reply carries a Stamp. The backend owns the request generation and the
// association epoch; the TUI keeps one Gate per screen and drops any reply the
// Gate does not accept, which is how late and other-clone replies never land.
package gardenapi

import (
	"context"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agentactivity"
	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/approve"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/assist"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/drafts"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/plan"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/planrev"
	gardensync "github.com/Tiago-0liveira/bonsai/internal/core/garden/sync"
)

// Backend is everything the Garden screens and Settings need. Status,
// Work, Candidates, Projects and PreviewPlan only read; Apply, Resume, Skip and
// Unskip are the only calls that change anything, and Apply only runs the plan
// the user previewed.
//
// An error means the call did not do what was asked (always an *Error, except
// context cancellation). Apply and Resume instead report an operation that
// started but did not finish in OperationView.Err.
type Backend interface {
	// Work reads the canonical managed collection; it never writes or launches.
	Work(context.Context) (WorkView, error)
	// Status is the one place the Garden shell, Setup and Settings read state.
	Status(ctx context.Context) (StatusView, error)
	// Candidates lists the repositories the local remotes point at.
	Candidates(ctx context.Context) (CandidatesView, error)
	// Projects lists the Projects an existing-Project setup can pick from.
	Projects(ctx context.Context, target TargetRef) (ProjectsView, error)
	// PreviewPlan builds the step-by-step plan without writing anything.
	PreviewPlan(ctx context.Context, req PlanRequest) (PlanView, error)
	// Apply runs a previewed plan. The backend rebuilds the plan and refuses with
	// CodeStalePlan when GitHub changed since the preview.
	Apply(ctx context.Context, plan PlanView) (OperationView, error)
	// Resume re-observes GitHub and finishes the steps still open.
	Resume(ctx context.Context, operationID string) (OperationView, error)
	// Skip records "Garden off" for this clone. A zero target uses the only
	// candidate and fails with CodeTargetAmbiguous when there are several.
	Skip(ctx context.Context, target TargetRef) (StatusView, error)
	// Unskip reverses Skip.
	Unskip(ctx context.Context) (StatusView, error)

	// PreviewCapture validates a new idea and returns exactly what Capture would
	// create, with a fresh operation ID. It writes nothing and needs Write access
	// (CodeReadOnly otherwise).
	PreviewCapture(ctx context.Context, req CaptureRequest) (CapturePreview, error)
	// Capture creates the previewed Issue at Draft, once. It is the only call that
	// creates an Issue. Calling it again with the same preview never creates a
	// second Issue: a finished operation returns its Issue, and one that may have
	// reached GitHub reports CaptureUnknown until ReconcileCapture settles it.
	Capture(ctx context.Context, preview CapturePreview) (CaptureView, error)
	// ReconcileCapture looks for the Issue of a capture whose outcome is unknown.
	// It only reads, except that it records what it found in the local operation
	// record. A complete read that finds nothing makes the capture retryable.
	ReconcileCapture(ctx context.Context, operationID string) (CaptureView, error)

	// Document reads one Issue afresh for editing: the user-owned body, the
	// verified specification revisions and the base an edit starts from. It
	// only reads.
	Document(ctx context.Context, issue uint64) (DocumentView, error)
	// AssistQuestions reads no GitHub, writes nothing and keeps no record.
	AssistQuestions(ctx context.Context, req AssistQuestionsRequest) (AssistQuestionsView, error)
	// AssistDraft reads no GitHub, writes nothing and keeps no record.
	AssistDraft(ctx context.Context, req AssistDraftRequest) (AssistDraftView, error)
	// PreviewSave verifies the revision log now and reports what saving would
	// append, or the Conflict in the way. It writes nothing and needs Write
	// access.
	PreviewSave(ctx context.Context, req SaveRequest) (SavePreviewView, error)
	// Save appends the previewed specification revision after verifying the log
	// again. It is the only call that writes a revision: it never writes the
	// Issue body, title or labels, and saving the same preview twice returns the
	// first result.
	Save(ctx context.Context, preview SavePreviewView) (SaveView, error)
	// PreviewResolve reports what publishing a conflict's Result would append.
	PreviewResolve(ctx context.Context, req ResolveRequest) (SavePreviewView, error)
	// Resolve appends the Result as a revision superseding every head the
	// conflict showed, after re-checking that those are still the heads.
	Resolve(ctx context.Context, preview SavePreviewView) (SaveView, error)

	// PreviewValidate verifies the revision and approval log now and reports gate one for an Issue: the exact
	// specification revision and fingerprint, what blocks approval, and the Draft to Approved transition
	// validating would record. It writes nothing and needs Write access.
	PreviewValidate(ctx context.Context, issue uint64) (ValidatePreviewView, error)
	// Validate records approval of exactly the previewed revision: one approval comment, then the acceptance
	// metadata line and the stage label of the same Issue. It starts no planner, agent, worktree or pull
	// request. It re-verifies first and refuses with CodeSpecChanged when the head moved; validating the same
	// preview again finds the recorded approval instead of writing a second one.
	Validate(ctx context.Context, preview ValidatePreviewView) (ValidateView, error)

	// PreviewPlanner verifies gate one and inspects the checkout now, and reports whether Request AI plan is
	// offered: only for a verified Approved specification. It also reconciles and returns the Issue's planner
	// attempts. It reads only and starts nothing, so displaying an Issue that changed elsewhere never runs a planner.
	PreviewPlanner(ctx context.Context, issue uint64) (PlannerPreviewView, error)
	// StartPlanner is the explicit Request AI plan. It re-verifies the specification and the checkout against the
	// preview, refuses a provider that cannot enforce read-only access, records the attempt and starts a
	// read-only session in the checkout. It creates no worktree. The caller attaches to the returned session
	// with the agents terminal contract.
	StartPlanner(ctx context.Context, req PlannerStartRequest) (PlannerAttemptView, error)
	// PlannerAttempts reconciles the Issue's attempts with their sessions and returns them, oldest first.
	PlannerAttempts(ctx context.Context, issue uint64) (PlannerAttemptsView, error)
	// CancelPlanner stops a live attempt; the attempt and its history stay.
	CancelPlanner(ctx context.Context, attemptID string) (PlannerAttemptView, error)
	// PlannerPlan returns the text of the attempt's plan file once it is ready (PlanFileReady), for prefilling the
	// plan to publish. It re-checks that the file is the attempt's own plain file within the size cap; the
	// content is never logged. CodePlanNotReady while the planner is still writing or has written nothing,
	// CodePlanFileInvalid when the file is not acceptable.
	PlannerPlan(ctx context.Context, attemptID string) (PlannerPlanView, error)
	// LocalWork summarizes, per Issue, the local agent work the board should flag: a planner attempt that is live,
	// or ended with a ready plan file that was not published from here. It reconciles with one agent session list
	// and one store read, never one call per Issue, and starts nothing.
	LocalWork(ctx context.Context) (LocalWorkView, error)

	// PlanReview verifies the Issue's comment log now and reports its plan revisions: the latest, the one under
	// review (current) and the approved one, separately, each with its integrity re-verified. It only reads.
	// IssueLink opens or copies the canonical GitHub Issue URL.
	IssueLink(ctx context.Context, issue uint64, open bool) error

	PlanReview(ctx context.Context, issue uint64) (PlanReviewView, error)
	// PreviewPlanPublish reports what publishing Text as a plan revision would append, bound to a planner
	// attempt's specification and inspected base. Text is what the user chose to publish; Bonsai reads nothing
	// from the planner's session. It writes nothing and needs Write access.
	PreviewPlanPublish(ctx context.Context, req PlanPublishRequest) (PlanPublishPreviewView, error)
	// PublishPlan appends the previewed revision after verifying the log again, and refuses when the
	// specification or the plan heads moved. It starts no planner and approves nothing.
	PublishPlan(ctx context.Context, preview PlanPublishPreviewView) (PlanPublishView, error)
	// DecidePlan records changes requested, rejection, or exact-pair approval through Ready.
	// It rechecks heads and authority and starts no run.
	DecidePlan(ctx context.Context, req PlanDecisionRequest) (PlanDecisionView, error)
}

// Stamp scopes one reply. Generation grows with every request of the clone and
// never repeats; Epoch changes when the association does (Skip, Unskip, setup).
type Stamp struct {
	LocalRepoID string // the clone the reply is for
	GardenID    uint64 // numeric repository ID, 0 while no target is known
	Epoch       uint64
	Generation  uint64
}

// Gate accepts only the newest reply. The zero Gate accepts the first reply.
type Gate struct{ last Stamp }

// Accept reports whether a reply is newer than every reply accepted so far and
// is for the same clone. A rejected reply must be dropped unread.
func (g *Gate) Accept(s Stamp) bool {
	if s.LocalRepoID == "" {
		return false
	}
	if g.last.LocalRepoID != "" {
		if s.LocalRepoID != g.last.LocalRepoID || s.Epoch < g.last.Epoch || s.Generation <= g.last.Generation {
			return false
		}
	}
	g.last = s
	return true
}

// Last is the stamp of the newest accepted reply (zero before the first).
func (g *Gate) Last() Stamp { return g.last }

// Reset forgets every accepted reply, for example when the clone changes.
func (g *Gate) Reset() { g.last = Stamp{} }

// State is the association state. Its values are exactly the sync package's
// association states; there is no second state machine.
type State string

const (
	StateNone                State = "none"
	StateSkipped             State = "skipped"
	StateReadOnly            State = "read-only"
	StateConnected           State = "connected"
	StatePartialSetup        State = "partial-setup"
	StateAwaitingManual      State = "awaiting-manual"
	StateOperationInProgress State = "operation-in-progress"
)

// Access is the GitHub permission on the target repository.
type Access uint8

const (
	AccessNone Access = iota
	AccessRead
	AccessWrite
	AccessAdmin
)

func (a Access) String() string {
	switch a {
	case AccessRead:
		return "read"
	case AccessWrite:
		return "write"
	case AccessAdmin:
		return "admin"
	}
	return "none"
}

// CanSetup reports whether the level may change Garden setup.
func (a Access) CanSetup() bool { return a >= AccessWrite }

// Presence is a fact GitHub may not report, such as whether the token carries
// the Project scope (fine-grained tokens do not list scopes).
type Presence uint8

const (
	PresenceUnknown Presence = iota
	PresencePresent
	PresenceAbsent
)

func (p Presence) String() string {
	switch p {
	case PresencePresent:
		return "present"
	case PresenceAbsent:
		return "absent"
	}
	return "unknown"
}

// TargetKind says how a candidate relates to the others.
type TargetKind string

const (
	KindRepository TargetKind = "repository"
	KindFork       TargetKind = "fork"     // a fork; Target.ParentFullName names its source
	KindUpstream   TargetKind = "upstream" // the source of a fork the remotes point at
)

// TargetRef picks a candidate by numeric repository ID. The zero value means
// "the only candidate".
type TargetRef struct{ RepoID uint64 }

// Target is one GitHub repository a Garden can track. Remotes that resolve to
// the same ID are one Target and their names are Aliases.
type Target struct {
	RepoID         uint64
	FullName       string
	Aliases        []string // local remote names, never URLs
	Access         Access
	Kind           TargetKind
	ParentFullName string // set for KindFork
}

// Identity is the GitHub account in use. It is never a coding-agent account.
type Identity struct {
	Login string // empty when it could not be read
	ID    string // numeric account ID
}

// Capabilities says which actions the backend would accept right now, so a
// screen renders no action it cannot run. Read-only access has Read only.
type Capabilities struct {
	Read    bool // Board, Table and Drafts can read the Garden
	Setup   bool // PreviewPlan and Apply are allowed
	Resume  bool // an interrupted setup can continue
	Skip    bool
	Unskip  bool
	Refresh bool // GitHub could be reached for this reply
}

// StatusView is the whole connection state of this clone.
type StatusView struct {
	Stamp Stamp
	State State
	// Sync is the sync package's connection status, which refines State once
	// reads exist ("stale", "offline", "rate-limited", "access-revoked", ...).
	// It equals State until then.
	Sync      string
	Target    Target // zero RepoID while unknown or ambiguous
	Ambiguous bool   // several candidates: ask Candidates and let the user choose
	Identity  Identity
	// Access is the live level, or the recorded one when GitHub was unreachable.
	Access       Access
	ProjectScope Presence
	// RemoteGarden says whether labels of a Garden already exist on the target;
	// only read for a read-only clone without a recorded association.
	RemoteGarden Presence
	// ProjectID is the Project recorded by the association, if any.
	ProjectID string
	// Operation is the active or last interrupted setup, nil when none.
	Operation *OperationView
	// RemoteChanged is set when the remotes now resolve to another repository
	// than the one recorded; setup is refused until the user reconnects.
	RemoteChanged bool
	IssueCount    int
	LastSync      time.Time // last read of the Garden, zero when never
	Caps          Capabilities
	// RefreshError is why Refresh is false, nil when GitHub answered.
	RefreshError *Error
}

// CandidatesView lists the repositories the remotes point at.
type CandidatesView struct {
	Stamp Stamp
	// Ambiguous is true when the user must choose (GS-02); false with exactly
	// one Candidate when the target is clear.
	Ambiguous  bool
	Candidates []Target
	// Notes are remote names that were ignored, e.g. "mirror: not a GitHub repository".
	Notes []string
}

// FieldState is whether a Project already has a Garden field.
type FieldState uint8

const (
	FieldMissing FieldState = iota
	FieldPresent
)

// Project is one existing Project offered by "Connect existing Project".
type Project struct {
	ID     string // opaque GitHub node ID, what a plan refers to
	Number string
	Title  string
	Stage  FieldState
	Type   FieldState
	// Conflict marks a Project named like the one Create would make. Without
	// verified ownership it is never adopted automatically; picking it here is an
	// explicit choice.
	Conflict bool
}

// Compatible is true when both Garden fields already exist.
func (p Project) Compatible() bool { return p.Stage == FieldPresent && p.Type == FieldPresent }

// ProjectsView lists the Projects of the account that setup writes through.
type ProjectsView struct {
	Stamp    Stamp
	Owner    string // login the Projects belong to
	Projects []Project
}

// Mode is the setup choice.
type Mode string

const (
	ModeCreate          Mode = "create"
	ModeConnectExisting Mode = "connect-existing"
	ModeSkip            Mode = "skip"
)

// ProjectRef selects an existing Project by its ID and number.
type ProjectRef struct{ ID, Number string }

// PlanRequest asks for a preview. Project is required for ModeConnectExisting.
type PlanRequest struct {
	Mode    Mode
	Target  TargetRef
	Project *ProjectRef
}

// Change is what a plan row does.
type Change string

const (
	ChangeCreate Change = "create"
	ChangeReuse  Change = "reuse"
	ChangeAdd    Change = "add"
	ChangeManual Change = "manual" // needs the user: a conflict or browser work
)

// RowKind is what a plan row acts on.
type RowKind string

const (
	RowLabel   RowKind = "label"
	RowLabels  RowKind = "labels" // several labels of one family in one row
	RowProject RowKind = "project"
	RowField   RowKind = "field"
	RowView    RowKind = "view"
)

// PlanRow is one line of the plan. Rows are already grouped for display.
type PlanRow struct {
	Change Change
	Kind   RowKind
	Target string // e.g. "bonsai:managed", "acme / Bonsai Garden", "Stage  (single select, 6 options)"
	Note   string
	Count  int // plan steps the row stands for
	// Conflict rows block Apply: GitHub already has something Bonsai must not overwrite.
	Conflict bool
	// Advisory rows are guidance only (finish the board layout in the browser);
	// Apply neither runs nor tracks them.
	Advisory bool
}

// PlanSummary is the zero-import statement of the plan (GS-04, GS-05).
type PlanSummary struct {
	ImportCount       int // always 0: setup never imports issues
	CreateLabels      int
	ReuseLabels       int
	CreateProject     bool
	ProjectName       string
	ProjectOwner      string
	AddFields         int
	Conflicts         int
	NeedsProjectScope bool // a step writes a Project
	// CanApply is false for a Skip preview, an empty plan or any conflict.
	CanApply bool
}

// PlanView is a previewed plan. Hand it back to Apply unchanged.
type PlanView struct {
	Stamp   Stamp
	Request PlanRequest
	// PlanID fingerprints the exact steps; Apply refuses a plan whose rebuilt
	// steps differ.
	PlanID  string
	Target  Target
	Rows    []PlanRow
	Summary PlanSummary
}

// StepStatus is the progress of one operation step.
type StepStatus string

const (
	StepPending StepStatus = "pending"
	StepDone    StepStatus = "done"
	StepFailed  StepStatus = "failed"
	StepSkipped StepStatus = "skipped"
)

// Safe step error codes. Anything else is shown as StepRemoteFailure.
const (
	StepScopeMissing    = "scope-missing"
	StepAuthRequired    = "auth-required"
	StepRateLimited     = "rate-limited"
	StepOffline         = "offline"
	StepLabelConflict   = "label-conflict"
	StepProjectConflict = "project-conflict"
	StepRemoteFailure   = "remote-failure"
	StepGraphQL         = "github-graphql"
	StepValidation      = "github-validation"
)

// OperationStep is one displayed step of an operation (labels are grouped).
type OperationStep struct {
	Title     string // e.g. `Create 10 labels`, `Create Project "Bonsai Garden"`
	Row       PlanRow
	Status    StepStatus
	ErrorCode string // one of the Step* codes while Status is StepFailed
}

// OperationView is the durable progress of one setup attempt.
type OperationView struct {
	Stamp Stamp
	ID    string // opaque, safe to show ("op-7f3a21c0")
	// State is the association state after the call that produced the view.
	State State
	// Done and Total count the underlying steps, not the grouped ones.
	Done, Total    int
	Steps          []OperationStep
	AwaitingManual []string
	Running        bool
	// Err is why the operation ended incomplete, nil when it finished.
	Err *Error
}

// Code is a stable, safe error category.
type Code string

const (
	CodeAuthRequired        Code = "auth-required"
	CodeRateLimited         Code = "rate-limited"
	CodeOffline             Code = "offline"
	CodePermissionDenied    Code = "permission-denied"
	CodeReadOnly            Code = "read-only"
	CodeScopeMissing        Code = "scope-missing"
	CodeTargetAmbiguous     Code = "target-ambiguous"
	CodeNoGitHubRemote      Code = "no-github-remote"
	CodeProjectNotFound     Code = "project-not-found"
	CodeProjectOwner        Code = "project-owner"
	CodeProjectConflict     Code = "project-conflict"
	CodeIdentityChanged     Code = "repository-identity-changed"
	CodeValidation          Code = "github-validation"
	CodeGraphQL             Code = "github-graphql"
	CodeOperationInProgress Code = "operation-in-progress"
	CodeOperationNotFound   Code = "operation-not-found"
	CodeStalePlan           Code = "stale-plan"
	CodeConflict            Code = "conflict"
	// CodeSpecChanged means the specification head moved since the preview, so nothing was approved.
	CodeSpecChanged Code = "specification-changed"
	// CodeValidationBlocked means gate one cannot run: the preview lists the blockers.
	CodeValidationBlocked Code = "validation-blocked"
	// CodeNotApproved means no planner may start: the specification is not a verified approved one.
	CodeNotApproved Code = "not-approved"
	// CodeRepoChanged means the checkout moved since the preview (commit or uncommitted changes).
	CodeRepoChanged Code = "repository-changed"
	// CodeDirtyContext means the checkout has uncommitted changes that need an explicit decision.
	CodeDirtyContext Code = "dirty-context"
	// CodeReadOnlyUnsupported means the account's provider cannot enforce read-only access, so it cannot plan.
	CodeReadOnlyUnsupported Code = "read-only-unsupported"
	// CodePlanNotReady means the planner has not finished writing a plan file yet.
	CodePlanNotReady Code = "plan-not-ready"
	// CodePlanFileInvalid means the plan file is not a plain file of an acceptable size in the attempt's directory.
	CodePlanFileInvalid Code = "plan-file-invalid"
	// CodeAttemptActive means an earlier planner attempt for the Issue is still running.
	CodeAttemptActive Code = "attempt-active"
	// CodeNotFound means the Issue is gone.
	CodeNotFound Code = "not-found"
	// CodeNoChange means there is nothing to save; CodeDocumentIncomplete means the
	// revision log could not be verified, so nothing was written.
	CodeNoChange           Code = "no-change"
	CodeDocumentIncomplete Code = "document-incomplete"
	// CodeSaveUnknown means a revision may have been appended; saving again checks first.
	CodeSaveUnknown Code = "save-unknown"
	// CodeCaptureUnknown means an Issue may have been created; reconcile before retrying.
	CodeCaptureUnknown Code = "capture-unknown"
	// CodeInvalidInput means the request failed validation; nothing was sent.
	CodeInvalidInput Code = "invalid-input"
	// CodeAssistUnsupported means tool-free structured assistance is unavailable.
	CodeAssistUnsupported Code = "assist-unsupported"
	// CodeAssistTimeout means the assistant did not answer before its deadline.
	CodeAssistTimeout Code = "assist-timeout"
	// CodeAssistMalformed means the assistant's reply failed validation.
	CodeAssistMalformed Code = "assist-malformed"
	// CodeAssistTooLarge means the proposed draft exceeds the specification limit.
	CodeAssistTooLarge Code = "assist-too-large"
	// CodeAssistBusy means this request ID is already in flight.
	CodeAssistBusy Code = "assist-busy"
	// CodeAgentAuthRequired means the coding-agent account needs sign-in.
	CodeAgentAuthRequired Code = "agent-auth-required"
	// CodeNotConnected means there is no Garden to write to.
	CodeNotConnected Code = "not-connected"
	CodeSkipped      Code = "skipped"
	CodeStore        Code = "store-unavailable"
	CodeFailed       Code = "failed"
)

// Error is the only error the backend returns. Message and Next are written
// for the user; no field ever carries a token, a URL or a raw response.
type Error struct {
	Code    Code
	Message string
	Next    string // what to do next, may be empty
	// Fix is a command the user runs themselves ("gh auth refresh -s project").
	// Bonsai never runs it and never refreshes a login.
	Fix string
}

func (e *Error) Error() string { return e.Message }

// WorkView carries the same ordered, last-good read model to every Garden view.
type WorkView struct {
	Stamp    Stamp
	Snapshot gardensync.Snapshot
	// PendingCaptures are this clone's captures that still need a decision after a
	// restart or timeout (state pending or unknown), so Drafts can offer Reconcile.
	PendingCaptures []CaptureView
	// PendingCapturesErr means the list is unknown; retain the last good list and block capture.
	PendingCapturesErr *Error
}

// CaptureRequest is what the user typed for a new idea.
type CaptureRequest struct {
	Title       string
	Description string // optional
	Type        garden.ItemType
}

// CapturePreview is the exact creation Capture will run. Hand it back unchanged.
type CapturePreview struct {
	Stamp       Stamp
	OperationID string // stable across retries of this one capture
	Request     CaptureRequest
	Target      Target
	Labels      []string // the bonsai: labels the Issue is created with
}

// CaptureState is where a capture stands.
type CaptureState string

const (
	CapturePending CaptureState = "pending"
	CaptureUnknown CaptureState = "unknown" // may exist on GitHub: reconcile, do not retry
	CaptureDone    CaptureState = "done"
	CaptureFailed  CaptureState = "failed" // nothing was created; retry is safe
)

// IssueRef names a created Issue.
type IssueRef struct {
	ID     uint64
	Number int64
}

// CaptureView is the durable state of one capture. A capture that started but
// did not finish arrives here with Err set rather than as an error.
type CaptureView struct {
	Stamp       Stamp
	OperationID string
	State       CaptureState
	Title       string
	Issue       *IssueRef
	Err         *Error
}

// DocumentBase is what an edit started from. Hand it back to PreviewSave unchanged.
type DocumentBase struct {
	HeadHash      string // empty while the Issue has no specification revision
	HeadCommentID int64
	BodyHash      string
	Content       string // the text the editor was seeded with
}

// RemoteRevision is a revision, or an edited body, written by someone else.
type RemoteRevision struct {
	Hash      string // empty for an edited body
	CommentID int64
	Author    string // login, a display label
	At        time.Time
	Content   string
	Source    string // "Bonsai/GitHub comment" or the body notice
}

// DocumentView is the fresh read of one Issue for editing.
type DocumentView struct {
	Stamp     Stamp
	IssueID   uint64
	Number    int64
	Title     string
	Type      garden.ItemType
	Body      string // user-owned text, without Bonsai's marker lines
	Revisions []RemoteRevision
	Base      DocumentBase
	Seed      string // canonical editor text; empty while the heads conflict
	// Conflict is set when several heads already exist: there is nothing to edit
	// until they are resolved.
	Conflict *ConflictView
}

// AssistSettings selects the account and preferences for one tool-free call.
type AssistSettings struct {
	AccountID     agents.AccountID
	Model, Effort string
	Questions     int
	Guidance      string
}

// PrivateDraftRef scopes one assistance request to a private local draft. Revision is the
// durable revision the working copy started from and Generation the caller's request counter;
// both come back unchanged so a late reply can be matched to the request that asked for it.
type PrivateDraftRef struct {
	ID         drafts.ID
	Revision   uint64
	Generation uint64
}

// AssistContext carries the caller's working copy; the backend never loads an Issue.
// A request for a private draft sets PrivateDraft and leaves IssueID and Number zero.
type AssistContext struct {
	Stamp
	RequestID          string
	PrivateDraft       *PrivateDraftRef
	IssueID            uint64
	Number             int64
	Title              string
	Type               garden.ItemType
	Description, Draft string
	Answers            []assist.Answer
	Settings           AssistSettings
}

type AssistQuestionsRequest struct {
	AssistContext
	Round int
}

type AssistDraftRequest struct {
	AssistContext
	Feedback string
}

type AssistQuestionsView struct {
	Stamp
	PrivateDraft    *PrivateDraftRef
	RequestID       string
	Round           int
	Set             assist.QuestionSet
	Provider, Model string
}

type AssistDraftView struct {
	Stamp
	PrivateDraft         *PrivateDraftRef
	RequestID, InputHash string
	Proposal             assist.Proposal
}

// ConflictView is a divergence the user resolves visibly (Base, Yours, Remote, Result).
type ConflictView struct {
	IssueID uint64
	Base    DocumentBase
	Yours   string
	// Remote are the current heads, or the edited body when no revision exists.
	// After a lost race it includes our own saved head (see SavedCommentID).
	Remote         []RemoteRevision
	BodyHash       string
	Saved          bool
	SavedCommentID int64
}

// SaveRequest asks for a save preview of Content on top of Base.
type SaveRequest struct {
	IssueID uint64
	Base    DocumentBase
	Content string
}

// ResolveRequest asks to preview publishing Result for a conflict.
type ResolveRequest struct {
	IssueID  uint64
	Conflict ConflictView
	Result   string
}

// SectionDiff counts the lines one section gained and lost.
type SectionDiff struct {
	Section        string
	Added, Removed int
}

// SavePreviewView is exactly what Save or Resolve will append, or the Conflict
// that stands in the way (then nothing else is set).
type SavePreviewView struct {
	Stamp       Stamp
	OperationID string // stable trace ID; replay safety comes from the revision itself
	IssueID     uint64
	Number      int64
	Base        DocumentBase
	Content     string
	NewHash     string
	Parents     []string
	Diff        []SectionDiff
	// BodyChanged says the Issue body was edited on GitHub since the base. It is
	// not part of the specification and does not block the save.
	BodyChanged    bool
	AlreadySaved   bool
	SavedCommentID int64
	// Resolution is set for a Resolve preview, which carries the conflict it answers.
	Resolution *ConflictView
	Conflict   *ConflictView
}

// SaveState is how a save ended.
type SaveState string

const (
	SaveDone     SaveState = "done"
	SaveConflict SaveState = "conflict" // nothing written, or both heads exist after a lost race
	SaveUnknown  SaveState = "unknown"  // the revision may exist; saving again checks first
)

// SaveView is the result of Save or Resolve.
type SaveView struct {
	Stamp       Stamp
	OperationID string
	IssueID     uint64
	State       SaveState
	CommentID   int64
	Hash        string
	Replayed    bool
	Conflict    *ConflictView
	Err         *Error
}

// ValidatePreviewView is exactly what Validate will record for one Issue, or why it cannot. Hand it back to
// Validate unchanged.
type ValidatePreviewView struct {
	Stamp       Stamp
	OperationID string // stable trace ID; replay safety comes from the approval record itself
	IssueID     uint64
	Number      int64
	Title       string
	Type        garden.ItemType // never changed by validation
	Actor       string          // login of the authenticated account that will approve
	// Gate is the verified state: the head revision (Gate.Head, numbered Gate.Ordinal), its fingerprint, the
	// approval records retained for audit, the blockers and the stage the Issue moves from.
	Gate approve.Assessment
	// Steps are the writes still needed, in order. Empty means nothing is left to write.
	Steps  []approve.Step
	Labels []string
}

// CanValidate reports whether the Validate action is offered.
func (v ValidatePreviewView) CanValidate() bool { return v.Gate.CanValidate() }

// ValidateState is how a validation ended.
type ValidateState string

const (
	ValidateDone ValidateState = "done"
	// ValidatePartial means the approval is recorded but the Issue's metadata or label write did not finish.
	// The Issue stays a draft; validating again finishes it.
	ValidatePartial ValidateState = "partial"
	// ValidateUnknown means the approval comment may exist; validating again checks before it appends.
	ValidateUnknown ValidateState = "unknown"
)

// ValidateView is the result of Validate.
type ValidateView struct {
	Stamp             Stamp
	OperationID       string
	IssueID           uint64
	State             ValidateState
	ApprovalCommentID int64
	RevisionHash      string
	Done              []approve.Step
	Err               *Error
}

// PlannerPreviewView is what Request AI plan would bind, or why it is unavailable. Hand it back to StartPlanner
// unchanged inside the request.
type PlannerPreviewView struct {
	Stamp   Stamp
	IssueID uint64
	Number  int64
	Title   string
	Actor   string // login of the authenticated account that will request the plan
	// Gate is the verified gate-one state; the planner is offered only for an Approved head.
	Gate approve.Assessment
	// Spec is the approved revision the attempt is bound to.
	Spec plan.SpecRef
	// Base is the inspected checkout. A Dirty base needs an explicit IncludeDirty decision.
	Base plan.Base
	// Attempts is the Issue's planner history, oldest first.
	Attempts []plan.Attempt
	// Overlay is the live view of those attempts by attempt ID: session activity and plan file state.
	Overlay map[string]PlannerLiveView
	// Unavailable says why Request AI plan is not offered; empty when it is.
	Unavailable string
}

// CanRequest reports whether the Request AI plan action is offered.
func (v PlannerPreviewView) CanRequest() bool { return v.Unavailable == "" }

// Live returns the attempt that is still running, if any.
func (v PlannerPreviewView) Live() (plan.Attempt, bool) { return liveAttempt(v.Attempts) }

// PlannerStartRequest is the user's explicit Request AI plan: the preview they saw plus their choices.
type PlannerStartRequest struct {
	Preview PlannerPreviewView
	// AccountID is the profile that plans. Model and Effort are its launch choices; nothing else about the launch
	// (permissions, sandbox, approval) can be chosen, because the planner role fixes it read-only.
	AccountID     agents.AccountID
	Model, Effort string
	Name          string
	// IncludeDirty lets the planner read uncommitted changes. Required when Preview.Base.Dirty.
	IncludeDirty bool
	// AcceptReadOnly confirms the read-only permissions shown for the account.
	AcceptReadOnly bool
	Cols, Rows     int
}

// PlannerLiveView is what Bonsai sees of one attempt right now. It is read on every call and never stored: not
// part of the attempt record, no terminal output.
type PlannerLiveView struct {
	// Activity is the session's live activity (waiting, working or idle, with a reason); nil when the attempt is
	// not live or the session reports none.
	Activity *agentactivity.Activity
	// PlanFile is the state of the attempt's plan file: none, writing, ready or invalid.
	PlanFile plan.PlanFileInfo
}

// PlannerAttemptView is one attempt after a start, a cancel or a refresh.
type PlannerAttemptView struct {
	Stamp   Stamp
	IssueID uint64
	Attempt plan.Attempt
	Overlay PlannerLiveView
}

// PlannerAttemptsView is an Issue's reconciled planner history.
type PlannerAttemptsView struct {
	Stamp    Stamp
	IssueID  uint64
	Attempts []plan.Attempt
	// Overlay is the live view by attempt ID; an attempt missing from it has no activity and no plan file.
	Overlay map[string]PlannerLiveView
}

// PlannerPlanView is the text of a ready plan file.
type PlannerPlanView struct {
	Stamp     Stamp
	IssueID   uint64
	AttemptID string
	// Text is the plan exactly as the planner wrote it, at most plan.MaxPlanBytes.
	Text string
	File plan.PlanFileInfo
}

// LocalRunSummary is one local agent run of an Issue in the compact form the board shows.
type LocalRunSummary struct {
	AttemptID string
	// State is the attempt state: starting, running, ended, failed or cancelled.
	State string
	// Live is true while the attempt can still change.
	Live bool
	// Activity is the live session activity; nil when not live or unreported.
	Activity *agentactivity.Activity
	PlanFile plan.PlanFileState
	Updated  time.Time
}

// IssueLocalWork is the local work to flag on one Issue.
type IssueLocalWork struct {
	// Planner is the live planner attempt, else the newest ended one whose plan is ready and not published from
	// here.
	Planner *LocalRunSummary
}

// LocalWorkView maps an Issue ID (the one PlannerAttempts takes) to its local work. Issues with nothing to
// flag are absent. Implementation attempts are not included: they live in a separate store the board reads
// per Issue through LaunchAttempts.
type LocalWorkView struct {
	Stamp  Stamp
	Issues map[uint64]IssueLocalWork
}

// Live returns the attempt that is still running, if any.
func (v PlannerAttemptsView) Live() (plan.Attempt, bool) { return liveAttempt(v.Attempts) }

// PlanReviewView is the verified plan-review state of one Issue.
type PlanReviewView struct {
	ActorID string
	Stamp   Stamp
	IssueID uint64
	Actor   string // login of the authenticated account that would publish or decide
	// Review carries Latest, Current and Approved separately, the full revision history, and why publishing or
	// deciding is unavailable.
	Review planrev.Review
}

// PlanPublishRequest names the planner attempt a plan came from and the plan text the user chose to publish.
type PlanPublishRequest struct {
	IssueID   uint64
	AttemptID string
	Text      string
}

// PlanPublishPreviewView is exactly what PublishPlan will append. Hand it back unchanged.
type PlanPublishPreviewView struct {
	Stamp       Stamp
	OperationID string
	IssueID     uint64
	Number      int64
	Title       string
	Actor       string
	Preview     planrev.PublishPreview
}

// CanPublish reports whether PublishPlan is offered.
func (v PlanPublishPreviewView) CanPublish() bool { return v.Preview.CanPublish() }

// PlanWriteState is how a plan write ended.
type PlanWriteState string

const (
	PlanWriteDone PlanWriteState = "done"
	// PlanWriteUnknown means the comment may exist; repeating the action finds it instead of writing twice.
	PlanWriteUnknown PlanWriteState = "unknown"
)

// PlanPublishView is the result of PublishPlan.
type PlanPublishView struct {
	Stamp       Stamp
	OperationID string
	IssueID     uint64
	State       PlanWriteState
	CommentID   int64
	Hash        string
	Ordinal     int
	Replayed    bool
	Err         *Error
}

// PlanDecisionRequest is a decision about one exact plan revision.
type PlanDecisionRequest struct {
	Stamp        Stamp
	SpecHash     string
	ActorID      string
	IssueID      uint64
	RevisionHash string
	// Fingerprint is the specification fingerprint the reviewer saw the revision answer.
	Fingerprint string
	Decision    garden.Decision
	Reason      string
}

// PlanDecisionView is the result of DecidePlan.
type PlanDecisionView struct {
	Stamp     Stamp
	IssueID   uint64
	State     PlanWriteState
	CommentID int64
	Ordinal   int
	Decision  garden.Decision
	Replayed  bool
	Err       *Error
}

func liveAttempt(list []plan.Attempt) (plan.Attempt, bool) {
	for i := len(list) - 1; i >= 0; i-- {
		if list[i].State.Live() {
			return list[i], true
		}
	}
	return plan.Attempt{}, false
}
