package agents

import (
	"context"
	"time"
)

// NotResumableError carries the user-facing reason a session cannot be resumed.
type NotResumableError struct{ Reason string }

func (e *NotResumableError) Error() string { return "cannot resume: " + e.Reason }
func (e *NotResumableError) Unwrap() error { return ErrNotResumable }

// NotResumable returns an error that wraps ErrNotResumable with reason.
func NotResumable(reason string) error { return &NotResumableError{Reason: reason} }

// Fixed user-facing copy for NotResumableError.Reason.
const (
	ReasonWorktreeGone    = "the worktree no longer exists"
	ReasonProfileGone     = "the profile was removed"
	ReasonProviderMissing = "the provider is not available"
	ReasonUnsupported     = "this provider cannot resume sessions"
	ReasonNoConversation  = "no saved conversation: the agent ended before its first message"
	ReasonNotFound        = "the saved conversation could not be found"
	ReasonGarden          = "started by a Garden task; start it again from Garden"
	ReasonNeverStarted    = "the agent never started"
)

// ResumeTarget is what a provider resumes. An empty ID is valid only for a
// provider that resumes by context (Antigravity --continue in a private HOME).
type ResumeTarget struct {
	ID string `json:"id,omitempty"`
}

type ResumeRequest struct {
	Account           Account
	Session           Session   // runtime (WorkDir, HomeDir)
	ProviderSessionID string    // from the holder record; may be empty
	StartedAt         time.Time // original start, for discovery windows
	Contenders        int       // other unknown-id holders with overlapping lifetimes
	Claimed           []string  // provider ids held by other records of this account
}

// Resumer is optional. ResumeTarget checks the conversation exists on disk and
// returns what to resume; failures wrap ErrNotResumable via NotResumable.
type Resumer interface {
	ResumeTarget(context.Context, ResumeRequest) (ResumeTarget, error)
}

// SessionIDDiscoverer is optional. The holder calls it while the session runs
// until it returns a non-empty id; ("", nil) means "not yet".
type SessionIDDiscoverer interface {
	DiscoverSessionID(context.Context, ResumeRequest) (string, error)
}

// RuntimeRetainer is optional. After FinalizeSession on a normal end, a
// provider that needs its runtime dir for a later resume strips secrets from
// it and returns true; RunSession then skips SessionStore.Cleanup.
type RuntimeRetainer interface {
	RetainRuntime(context.Context, Account, Session) (bool, error)
}
