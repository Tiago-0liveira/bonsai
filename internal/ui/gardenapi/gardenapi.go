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

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
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
