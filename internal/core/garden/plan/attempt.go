// Package plan is the local planner attempt of the Garden workflow (plan 08.2). An attempt is one explicit,
// user-started, read-only agent session bound to an approved specification revision and the inspected
// repository base. It records who ran what under which permissions, and how it ended; it creates no
// worktree and publishes nothing. Plan revisions and gate two build on the attempt ID (plan 08.3, 08.4).
package plan

import (
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
)

// Role is what an attempt's agent was asked to be.
type Role string

// RolePlanner is the only role plan 08.2 starts: it reads the repository and talks; it changes nothing.
const RolePlanner Role = "planner"

// State is where an attempt stands. Starting and Running are live; the rest are final and never change.
type State string

const (
	// StateStarting means the attempt is recorded and its session is being created.
	StateStarting State = "starting"
	// StateRunning means the session is live.
	StateRunning State = "running"
	// StateEnded means the session exited with code 0. It says nothing about whether a plan was published.
	StateEnded State = "ended"
	// StateFailed means the session could not start, failed, or exited with an error. Partial work stays
	// viewable through SessionID for as long as the holder record is retained.
	StateFailed State = "failed"
	// StateCancelled means the user stopped the attempt.
	StateCancelled State = "cancelled"
)

// Live reports whether the attempt can still change.
func (s State) Live() bool { return s == StateStarting || s == StateRunning }

func (s State) valid() bool {
	switch s {
	case StateStarting, StateRunning, StateEnded, StateFailed, StateCancelled:
		return true
	}
	return false
}

// rank orders states so a late or repeated observation can never move an attempt backwards.
func (s State) rank() int {
	switch s {
	case StateStarting:
		return 0
	case StateRunning:
		return 1
	}
	return 2
}

// SpecRef is the approved specification revision an attempt plans for.
type SpecRef struct {
	RevisionHash string `json:"revision_hash"`
	CommentID    int64  `json:"comment_id"`
	Ordinal      int    `json:"ordinal"`
	Fingerprint  string `json:"fingerprint"`
}

// Base is the inspected repository state the planner reads. Only a count and a digest of uncommitted changes
// are kept: paths and contents never reach the record.
type Base struct {
	Commit string `json:"commit"`
	// Branch is empty on a detached HEAD.
	Branch string `json:"branch,omitempty"`
	// Dirty is true when the checkout has uncommitted changes; the planner then reads them too.
	Dirty      bool `json:"dirty,omitempty"`
	DirtyFiles int  `json:"dirty_files,omitempty"`
	// DirtyDigest fingerprints the status entries (paths and states, not contents) so a change between the
	// preview and the start is detected.
	DirtyDigest string `json:"dirty_digest,omitempty"`
}

// Isolation records the read-only guarantee the user accepted.
type Isolation struct {
	// Mechanism names what enforces read-only access below the model.
	Mechanism string   `json:"mechanism"`
	Allows    []string `json:"allows"`
	// DirtyAccepted is true when the user explicitly let the planner read uncommitted changes.
	DirtyAccepted bool      `json:"dirty_accepted,omitempty"`
	AcceptedAt    time.Time `json:"accepted_at"`
	AcceptedBy    string    `json:"accepted_by"`
}

// Attempt is one planner run. Fields hold no token, environment value, prompt or terminal output.
type Attempt struct {
	ID     string             `json:"id"`
	Local  garden.LocalRepoID `json:"local_repo_id"`
	Repo   garden.RepoID      `json:"repo_id"`
	Issue  garden.IssueID     `json:"issue_id"`
	Number int64              `json:"number"`
	Role   Role               `json:"role"`

	Spec      SpecRef   `json:"spec"`
	Base      Base      `json:"base"`
	Isolation Isolation `json:"isolation"`

	Provider    agents.ProviderID `json:"provider"`
	AccountID   agents.AccountID  `json:"account_id"`
	AccountName string            `json:"account_name"`
	Model       string            `json:"model,omitempty"`
	Effort      string            `json:"effort,omitempty"`

	// SessionID names the holder-backed session; the transcript stays with that record, not here.
	SessionID string `json:"session_id,omitempty"`
	State     State  `json:"state"`
	// Reason is a short fixed phrase for a failed or cancelled attempt, never provider text.
	Reason          string `json:"reason,omitempty"`
	ExitCode        *int   `json:"exit_code,omitempty"`
	CancelRequested bool   `json:"cancel_requested,omitempty"`

	RequestedAt time.Time  `json:"requested_at"`
	EndedAt     *time.Time `json:"ended_at,omitempty"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

func (a Attempt) valid() bool {
	return a.ID != "" && a.Local != "" && a.Repo != 0 && a.Issue != 0 && a.Role == RolePlanner && a.State.valid() &&
		a.Spec.RevisionHash != "" && a.Spec.Fingerprint != "" && a.Base.Commit != "" && a.Provider != "" && a.AccountID != ""
}
