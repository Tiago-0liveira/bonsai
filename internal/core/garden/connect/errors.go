// Package connect resolves and persists an opt-in local connection to a remote Garden.
package connect

import (
	"errors"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
)

var (
	ErrTargetAmbiguous = errors.New("multiple Garden targets need a choice")
	ErrNoGitHubRemote  = errors.New("no GitHub remote found")
	ErrReadOnly        = errors.New("Garden connection is read-only")
	ErrAuthRequired    = errors.New("GitHub authentication required")
	ErrRateLimited     = errors.New("GitHub rate limited")
	// ErrOffline identifies a source outage without exposing adapter response text.
	ErrOffline                   = garden.ErrOffline
	ErrRevision                  = errors.New("Garden associations changed; reload and retry")
	ErrIdempotency               = errors.New("idempotency key was used for a different request")
	ErrOperationInProgress       = errors.New("Garden operation already in progress for this local repository")
	ErrProjectNotFound           = errors.New("selected GitHub Project was not found or is inaccessible")
	ErrProjectOwner              = errors.New("selected GitHub Project belongs to another owner")
	ErrProjectConflict           = errors.New("GitHub Project ownership conflict")
	ErrRepositoryIdentityChanged = errors.New("GitHub repository identity changed")
	ErrGitHubValidation          = errors.New("GitHub rejected the Garden request")
	// ErrPermissionDenied identifies a repository authorization denial.
	ErrPermissionDenied        = garden.ErrPermissionDenied
	ErrInvalidOperation        = errors.New("invalid or inconsistent Garden operation")
	ErrUnsupportedSchema error = compatibleStoreError{"unsupported Garden store schema"}
	ErrCorruptStore      error = compatibleStoreError{"corrupt Garden store"}
	ErrOperationNotFound       = errors.New("Garden operation not found")
	ErrOperationForeign        = errors.New("Garden operation belongs to another local repository")
	ErrStoreTooLarge           = errors.New("Garden store exceeds size limit")
)

// compatibleStoreError retains the early phase-B ErrInvalidOperation classification for callers
// while exposing precise store errors to new callers.
type compatibleStoreError struct{ message string }

func (e compatibleStoreError) Error() string        { return e.message }
func (e compatibleStoreError) Is(target error) bool { return target == ErrInvalidOperation }

// ScopeError identifies the permission scope needed to continue without exposing adapter details.
type ErrScopeMissing struct{ Scope string }

func (e ErrScopeMissing) Error() string { return "required GitHub scope missing" }
func (e ErrScopeMissing) UserMessage() string {
	return "Authorize Bonsai with the GitHub permissions required for Garden setup."
}

// ScopeError preserves the early phase-B spelling while keeping scope failures typed.
type ScopeError = ErrScopeMissing

// SafeError carries a stable, user-safe category and next-step message.
type SafeError struct {
	Code string
	Next string
}

func (e SafeError) Error() string       { return e.Code }
func (e SafeError) UserMessage() string { return e.Next }

// UserMessage returns a safe next step for expected adapter failures.
func UserMessage(err error) string {
	switch {
	case errors.Is(err, ErrAuthRequired):
		return "Sign in to GitHub, then retry."
	case errors.Is(err, ErrRateLimited):
		return "Wait for the GitHub rate limit to reset, then retry."
	case errors.Is(err, ErrOffline):
		return "Reconnect to the network, then retry."
	case errors.Is(err, ErrReadOnly):
		return "Reconnect with Project write access to change Garden setup."
	case errors.Is(err, ErrPermissionDenied):
		return "Ask a repository owner to grant the required GitHub access."
	case errors.Is(err, ErrProjectConflict):
		return "Select that Project explicitly with ConnectExisting if it is yours."
	default:
		var s interface{ UserMessage() string }
		if errors.As(err, &s) {
			return s.UserMessage()
		}
		return "Review the Garden connection and retry."
	}
}
