// Package launch is the explicit local half of the Garden workflow (plan 09): it
// turns one exact approved specification and plan into an isolated worktree and
// one coding-agent holder on this machine, and keeps a durable record of every
// effect so a crash, a timeout or a closed screen can be reconciled instead of
// repeated. It never starts anything on its own: every effect needs a
// confirmation of exactly what was previewed, and observing or reconciling never
// starts a holder, creates a worktree, pushes or opens a pull request.
package launch

import (
	"context"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/approve"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/plan"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/planrev"
	domain "github.com/Tiago-0liveira/bonsai/internal/git"
	"github.com/Tiago-0liveira/bonsai/internal/git/github"
)

// State is where an attempt's local execution stands.
type State string

const (
	StatePreparing      State = "preparing"
	StateStarting       State = "starting"
	StateStartUnknown   State = "start-unknown"
	StateRecoverable    State = "recoverable"
	StateRunning        State = "running"
	StateAwaitingReview State = "awaiting-human-review"
	StateFailed         State = "failed"
	StateStopped        State = "stopped"
	StateCleaned        State = "cleaned"
)

// Active reports whether the attempt may still be doing work, so a second
// attempt for the same Issue is refused.
func (s State) Active() bool {
	switch s {
	case StatePreparing, StateStarting, StateStartUnknown, StateRecoverable, StateRunning:
		return true
	}
	return false
}

func (s State) valid() bool {
	switch s {
	case StatePreparing, StateStarting, StateStartUnknown, StateRecoverable, StateRunning,
		StateAwaitingReview, StateFailed, StateStopped, StateCleaned:
		return true
	}
	return false
}

// Step is one independently tracked part of an attempt.
type Step string

const (
	StepPreflight Step = "preflight"
	StepWorktree  Step = "worktree"
	StepHolder    Step = "holder"
	StepMirror    Step = "remote-mirror"
	StepPush      Step = "push"
	StepPR        Step = "pr"
	StepCleanup   Step = "cleanup"
)

var allSteps = []Step{StepPreflight, StepWorktree, StepHolder, StepMirror, StepPush, StepPR, StepCleanup}

type StepState string

const (
	StepPending StepState = "pending"
	StepRunning StepState = "running"
	StepDone    StepState = "done"
	StepFailed  StepState = "failed"
	StepUnknown StepState = "unknown"
	StepSkipped StepState = "skipped"
)

func (s StepState) valid() bool {
	switch s {
	case StepPending, StepRunning, StepDone, StepFailed, StepUnknown, StepSkipped:
		return true
	}
	return false
}

// StepRecord holds a step's state and a fixed code, never provider or Git text.
type StepRecord struct {
	State     StepState `json:"state"`
	Code      string    `json:"code,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

// PRState is where the optional pull request stands. Unknown means a create
// request may have reached GitHub; it always wins over Unavailable.
type PRState string

const (
	PRNotCreated  PRState = "not-created"
	PRPending     PRState = "pending"
	PRUnknown     PRState = "unknown"
	PRUnavailable PRState = "unavailable"
	PRLinked      PRState = "linked"
)

func (s PRState) valid() bool {
	switch s {
	case PRNotCreated, PRPending, PRUnknown, PRUnavailable, PRLinked:
		return true
	}
	return false
}

// PlanRef names the approved plan revision and the decision that approved it.
type PlanRef struct {
	RevisionHash string `json:"revision_hash"`
	CommentID    int64  `json:"comment_id"`
	Ordinal      int    `json:"ordinal"`
	DecisionID   int64  `json:"decision_id"`
	BaseCommit   string `json:"base_commit"`
}

// Inputs are the exact approved records an attempt runs: the specification
// revision with its approval, and the plan revision with its decision.
type Inputs struct {
	Spec           plan.SpecRef `json:"spec"`
	SpecApprovalID int64        `json:"spec_approval_id"`
	Plan           PlanRef      `json:"plan"`
}

// Options are the native launch choices frozen for an attempt. They never
// include the prompt, which stays in memory.
type Options struct {
	Model          string `json:"model,omitempty"`
	Effort         string `json:"effort,omitempty"`
	Sandbox        string `json:"sandbox,omitempty"`
	Approval       string `json:"approval,omitempty"`
	PermissionMode string `json:"permission_mode,omitempty"`
	FullAccess     *bool  `json:"full_access,omitempty"`
}

// Run is one holder start of an attempt. Its identity is written before the
// holder is asked to start, so a lost answer can still be queried.
type Run struct {
	Number       int        `json:"number"`
	OperationID  string     `json:"operation_id"`
	SessionID    string     `json:"session_id"`
	State        string     `json:"state"` // starting|running|stopping|exited|failed|not-started
	Acknowledged bool       `json:"acknowledged"`
	ExitCode     *int       `json:"exit_code,omitempty"`
	StartedAt    *time.Time `json:"started_at,omitempty"`
	EndedAt      *time.Time `json:"ended_at,omitempty"`
}

// Rework records that a human's requested changes started one more run. It holds
// only the verified comment's ID, never its text, so the same feedback can never
// start a second run and the store stays free of free-form text.
type Rework struct {
	FeedbackCommentID int64     `json:"feedback_comment_id"`
	Run               int       `json:"run"`
	RequestedAt       time.Time `json:"requested_at"`
}

// PRLink is the pull request state of an attempt.
type PRLink struct {
	State   PRState `json:"state"`
	Number  int     `json:"number,omitempty"`
	URL     string  `json:"url,omitempty"`
	HeadSHA string  `json:"head_sha,omitempty"`
	Code    string  `json:"code,omitempty"`
}

// Attempt is one local implementation attempt. It holds no spec or plan text,
// prompt, argv, environment, terminal output or token: the numeric references
// let the text be read again from the verified remote records.
type Attempt struct {
	ID          string                   `json:"id"`
	RequestHash string                   `json:"request_hash"`
	Local       garden.LocalRepoID       `json:"local_repo_id"`
	Repo        garden.RepoID            `json:"repo_id"`
	Issue       garden.IssueID           `json:"issue_id"`
	Number      int64                    `json:"number"`
	ActorID     string                   `json:"actor_id"`
	Epoch       uint64                   `json:"epoch"`
	Inputs      Inputs                   `json:"inputs"`
	Proposal    domain.IsolationProposal `json:"proposal"`
	Provider    agents.ProviderID        `json:"provider"`
	AccountID   agents.AccountID         `json:"account_id"`
	AccountName string                   `json:"account_name"`
	Options     Options                  `json:"options"`
	Name        string                   `json:"name"`
	Cols        int                      `json:"cols"`
	Rows        int                      `json:"rows"`
	State       State                    `json:"state"`
	Steps       map[Step]StepRecord      `json:"steps"`
	WorktreeID  string                   `json:"worktree_id,omitempty"`
	// SessionID is the newest run's session, kept after it exits.
	SessionID     string       `json:"session_id,omitempty"`
	Runs          []Run        `json:"runs"`
	Reworks       []Rework     `json:"reworks,omitempty"`
	DesiredStage  garden.Stage `json:"desired_stage,omitempty"`
	MirroredStage garden.Stage `json:"mirrored_stage,omitempty"`
	PR            PRLink       `json:"pr"`
	StopRequested bool         `json:"stop_requested,omitempty"`
	Revision      uint64       `json:"revision"`
	RequestedAt   time.Time    `json:"requested_at"`
	UpdatedAt     time.Time    `json:"updated_at"`
	EndedAt       *time.Time   `json:"ended_at,omitempty"`
}

// Request is what a caller asks to launch. The prompt, display name and
// read-only flag of Launch are forbidden: the prompt is built from the exact
// approved records, never supplied.
type Request struct {
	ReviewPolicyHash string // empty means no automatic review
	Target           approve.Target
	Branch           string
	BaseRef          string
	RemoteName       string
	AccountID        agents.AccountID
	Launch           agents.LaunchOptions // overrides
	Name             string
	Cols, Rows       int
}

// RemoteSnapshot is one live verified read of the Issue's records. Live is true
// only for a fresh, complete read with current identity and access; a retained
// or cached snapshot is never Live.
type RemoteSnapshot struct {
	Target     approve.Target
	Local      garden.LocalRepoID
	Epoch      uint64
	Issue      garden.Issue
	FullName   string // verified owner/repo, never a credential URL
	Review     planrev.Review
	Live       bool
	VerifiedAt time.Time
}

// MirrorRequest asks the remote to move the Issue's stage label for an attempt.
type MirrorRequest struct {
	AttemptID string
	Target    approve.Target
	Local     garden.LocalRepoID
	Epoch     uint64
	Inputs    Inputs
	Stage     garden.Stage
}

// Preview is exactly what a confirmation covers. It is handed back unchanged to
// Start, which derives and compares every part again from fresh data.
type Preview struct {
	ID        string // "impl-" + IDs.NewID(), unchanged at confirmation
	Request   Request
	Remote    RemoteSnapshot
	Inputs    Inputs
	Proposal  domain.IsolationProposal
	Account   agents.LaunchResolution
	Escalated bool
	Attempts  []Attempt
}

type StartRequest struct {
	Preview             Preview
	Confirmed           bool
	EscalationConfirmed bool
}

type RetryRequest struct {
	AttemptID           string
	Confirmed           bool
	RetryHooks          bool // explicit acknowledgment of possibly partial earlier hooks
	EscalationConfirmed bool
}

// ReworkRequest asks for one more run of an attempt awaiting human review. The
// feedback text is held in memory for the prompt only. The caller must pass a
// comment ID and text taken from a verified changes_requested handoff entry for
// this attempt's current identity; launch does not verify their authenticity.
type ReworkRequest struct {
	AttemptID           string
	FeedbackCommentID   int64
	Feedback            string
	Confirmed           bool
	EscalationConfirmed bool
}

type PublishRequest struct {
	AttemptID string
	Confirmed bool
}

// CleanupPreview is what removing the retained worktree would do. Blocker is a
// fixed phrase when Allowed is false.
type CleanupPreview struct {
	AttemptID string
	Revision  uint64
	Check     domain.IsolationCheck
	Allowed   bool
	Blocker   string
}

type CleanupRequest struct {
	Preview   CleanupPreview
	Confirmed bool
}

// Remote is the verified Garden side: a fresh read of the records, and the
// expected-label stage mirror.
type Remote interface {
	Verify(context.Context, approve.Target) (RemoteSnapshot, error)
	Mirror(context.Context, MirrorRequest) error
}

// Accounts resolves the launch a profile would run with the overrides applied.
type Accounts interface {
	ResolveLaunch(context.Context, agents.AccountID, agents.LaunchOptions) (agents.LaunchResolution, error)
}

// Worktrees is the daemon's isolation authority.
type Worktrees interface {
	PreviewIsolation(context.Context, domain.IsolationRequest) (domain.IsolationProposal, error)
	CreateIsolation(context.Context, domain.IsolationCreateRequest) (domain.IsolationCheck, error)
	InspectIsolation(context.Context, domain.IsolationProposal) (domain.IsolationCheck, error)
	RemoveIsolation(context.Context, domain.IsolationRemoveRequest) error
	InspectPublishedHead(context.Context, domain.IsolationProposal) (domain.PublishedHead, error)
	PushIsolation(context.Context, domain.IsolationPushRequest) (domain.PublishedHead, error)
}

// StartIdentity names one holder start: the operation that asked for it and the
// session it will run as.
type StartIdentity struct {
	OperationID string
	SessionID   string
}

type StartSession struct {
	Identity                         StartIdentity
	ProjectID, WorktreeID, Dir, Name string
	AccountID                        agents.AccountID
	Cols, Rows                       int
	Task                             agents.TaskRef
	Launch                           agents.LaunchOptions
}

// SessionObservation is what the holder says about one start identity.
type SessionObservation struct {
	// Activity is transient supervision data, never folded into persisted runs.
	Activity string `json:"-"`
	Identity StartIdentity
	State    string
	Alive    bool
	// Acknowledged is the provider's actual start, not socket readiness.
	Acknowledged bool
	// Closed means the start is settled or fenced: it cannot acquire a holder later.
	Closed    bool
	ExitCode  *int
	StartedAt *time.Time
	EndedAt   *time.Time
}

// Observed is the latest session answer read during one reconcile, never persisted.
type Observed struct {
	AttemptID  string
	Session    SessionObservation
	ObservedAt time.Time
}

type Holder interface {
	Start(context.Context, StartSession) (SessionObservation, error)
	Stop(context.Context, StartIdentity) error
}

type Sessions interface {
	Query(context.Context, StartIdentity) (SessionObservation, error)
}

// DirectorySessions is implemented by a Sessions that can say whether any agent
// session, started by anything, is active or unsettled in a directory. Cleanup
// uses it when present, so a session started later in a retained worktree by
// the web, the CLI or the TUI is never removed from under its agent.
type DirectorySessions interface {
	ActiveAt(ctx context.Context, dir string) (bool, error)
}

// PullRequests is the narrow GitHub surface publishing needs: read branches and
// pull requests, and open one. It has no merge, review or delete.
type PullRequests interface {
	Branches(context.Context, string) ([]github.RemoteBranch, error)
	PullRequests(context.Context, string, github.PRFilter) ([]github.PullRequest, error)
	CreatePullRequest(context.Context, github.CreatePullRequestRequest) (github.PullRequest, error)
}

type Clock interface{ Now() time.Time }

type IDs interface {
	NewID() string
	NewSessionID() (agents.SessionID, error)
}

// Store is the durable attempt record.
type Store interface {
	Get(context.Context, string) (Attempt, error)
	List(context.Context, garden.LocalRepoID, garden.IssueID) ([]Attempt, error)
	// Reserve atomically records a new attempt; the bool is true when it was created.
	Reserve(context.Context, Attempt) (Attempt, bool, error)
	// Update applies fn under the store transaction; fn reports whether it changed anything.
	Update(context.Context, string, func(Attempt) (Attempt, bool, error)) (Attempt, error)
	// Lock is a cross-process lock on one attempt.
	Lock(context.Context, string) (func(), error)
}

// Deps are every seam of Service. All are required except PRs.
type Deps struct {
	Remote    Remote
	Accounts  Accounts
	Worktrees Worktrees
	Holder    Holder
	Sessions  Sessions
	PRs       PullRequests // optional; nil means unavailable and never blocks a launch
	Store     Store
	Clock     Clock
	IDs       IDs
	Local     garden.LocalRepoID
	// StartTimeout bounds one holder start; zero means 60s.
	StartTimeout time.Duration
	// QueryTimeout bounds each follow-up query; zero means 5s.
	QueryTimeout time.Duration
}

const (
	// MaxStoreBytes bounds the execution file before it is decoded.
	MaxStoreBytes = 8 << 20
	// MaxAttempts caps the store. Nothing is evicted: audit and idempotency win.
	MaxAttempts = 2000
	// MaxPromptBytes bounds the approved prompt; a larger one is refused, never truncated.
	MaxPromptBytes = 96 << 10
	// MaxReworkFeedbackBytes bounds the requested-changes text; a larger one is refused.
	MaxReworkFeedbackBytes = 8 << 10
)

// The codes persisted instead of any error text.
const (
	codePreflight          = "preflight"
	codeWorktreeFailed     = "worktree-failed"
	codeHooksFailed        = "hooks-failed"
	codeHooksUnknown       = "hooks-unknown"
	codeHolderStartFailed  = "holder-start-failed"
	codeHolderStartUnknown = "holder-start-unknown"
	codeHolderLost         = "holder-lost"
	codeHolderExitFailed   = "holder-exit-failed"
	codeStopRequested      = "stop-requested"
	codeRemotePending      = "remote-pending"
	codeRemoteConflict     = "remote-conflict"
	codePushPending        = "push-pending"
	codePushUnknown        = "push-unknown"
	codePRPending          = "pr-pending"
	codePRUnknown          = "pr-unknown"
	codePRUnavailable      = "pr-unavailable"
	codeCleanupUnsafe      = "cleanup-unsafe"
	codeCleanupUnknown     = "cleanup-unknown"
)
