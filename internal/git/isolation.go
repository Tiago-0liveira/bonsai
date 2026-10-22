package git

import "context"

// Isolation errors carry fixed codes so callers can tell a refusal that needs a
// new plan (base_changed) from one that needs a different name (branch_collision)
// without reading any Git output.
var (
	ErrBaseChanged     = E("base_changed", "approved base changed")
	ErrBranchCollision = E("branch_collision", "implementation branch already exists")
	ErrPathCollision   = E("path_collision", "implementation worktree path already exists")
	ErrOwnership       = E("ownership", "implementation worktree ownership could not be proven")
	ErrInputsChanged   = E("inputs_changed", "confirmed isolation inputs changed")
	ErrHooksFailed     = E("hooks_failed", "worktree hooks did not complete")
)

// IsolationRequest asks for a worktree on a new branch cut from one exact commit.
type IsolationRequest struct {
	OperationID  string `json:"operation_id"`
	RepositoryID string `json:"repository_id"`
	Branch       string `json:"branch"`
	BaseRef      string `json:"base_ref"`
	BaseSHA      string `json:"base_sha"`
	RemoteName   string `json:"remote_name"`
	Repository   string `json:"repository"` // verified owner/repo
}

// IsolationProposal is a request resolved against placement. It is what a user
// confirms and what ownership is later proven against. It holds a digest of the
// hook configuration, never the commands.
type IsolationProposal struct {
	IsolationRequest
	Path         string `json:"path"`
	RootRevision uint64 `json:"root_revision"`
	ConfigHash   string `json:"config_hash"`
}

type IsolationCreateRequest struct {
	Proposal IsolationProposal `json:"proposal"`
	// RetryHooks acknowledges that earlier hooks may have partly run.
	RetryHooks bool `json:"retry_hooks"`
}

// IsolationCheck is the proven state of one proposal's resources. Owned without
// Exists means the proposal's branch exists without its worktree, left by an
// interrupted creation. Fingerprint binds everything a later removal confirmed.
type IsolationCheck struct {
	Proposal    IsolationProposal `json:"proposal"`
	Worktree    Worktree          `json:"worktree"`
	Exists      bool              `json:"exists"`
	Owned       bool              `json:"owned"`
	Clean       bool              `json:"clean"`
	HeadSHA     string            `json:"head_sha"`
	Hooks       string            `json:"hooks"` // pending|running|done|failed|unknown
	Fingerprint string            `json:"fingerprint"`
}

type IsolationRemoveRequest struct {
	Check IsolationCheck `json:"check"`
}

// PublishedHead compares the worktree's committed head with the approved base
// and with the branch on the remote.
type PublishedHead struct {
	HeadSHA   string `json:"head_sha"`
	RemoteSHA string `json:"remote_sha"`
	// Meaningful means the committed tree differs from the base tree, not just
	// the commit ID.
	Meaningful bool `json:"meaningful"`
	Dirty      bool `json:"dirty"`
	// RemoteVerified is true only when the remote itself was asked and answered.
	RemoteVerified bool `json:"remote_verified"`
}

type IsolationPushRequest struct {
	Proposal IsolationProposal `json:"proposal"`
	HeadSHA  string            `json:"head_sha"`
}

// IsolationService is the optional Git authority for isolated implementation
// worktrees. LocalGitService is not widened: only services that implement it
// can serve these operations.
type IsolationService interface {
	PreviewIsolation(context.Context, IsolationRequest) (IsolationProposal, error)
	CreateIsolation(context.Context, IsolationCreateRequest) (IsolationCheck, error)
	InspectIsolation(context.Context, IsolationProposal) (IsolationCheck, error)
	RemoveIsolation(context.Context, IsolationRemoveRequest) error
	InspectPublishedHead(context.Context, IsolationProposal) (PublishedHead, error)
	PushIsolation(context.Context, IsolationPushRequest) (PublishedHead, error)
}
