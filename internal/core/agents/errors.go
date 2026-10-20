package agents

import (
	"errors"
	"fmt"
)

var (
	ErrAccountNotFound    = errors.New("account not found")
	ErrAccountExists      = errors.New("account already exists")
	ErrAccountAmbiguous   = errors.New("account selector is ambiguous")
	ErrInvalidAccountName = errors.New("invalid account name")
	ErrProviderNotFound   = errors.New("provider not found")
	ErrSessionNotFound    = errors.New("session not found")
	ErrNotAuthenticated   = errors.New("account is not authenticated")
	ErrUsageUnsupported   = errors.New("provider does not support usage")
	ErrInvalidLaunch      = errors.New("invalid launch options")
	ErrAccountBusy        = errors.New("account already has an active session")
	ErrProviderBusy       = errors.New("provider already has an active session on another account")
	ErrAccountInUse       = errors.New("account has active sessions")
	ErrInvalidProfile     = errors.New("invalid profile defaults")
	// ErrReadOnlyUnsupported means a read-only launch was asked of a provider whose
	// CLI cannot enforce it. The launch is refused; it never runs unrestricted.
	ErrReadOnlyUnsupported = errors.New("provider cannot enforce read-only access")
)

// AccountInUseError lists the active sessions that keep an account from being
// removed. It matches ErrAccountInUse.
type AccountInUseError struct {
	SessionIDs []string
}

func (e *AccountInUseError) Error() string {
	return fmt.Sprintf("%v (%d running); stop them first", ErrAccountInUse, len(e.SessionIDs))
}

func (e *AccountInUseError) Is(target error) bool { return target == ErrAccountInUse }

// ProfileError is a provider's rejection of profile defaults. Its message is the
// provider's own and never carries raw settings. It matches ErrInvalidProfile.
type ProfileError struct {
	Err error
}

func (e *ProfileError) Error() string { return e.Err.Error() }

func (e *ProfileError) Unwrap() error { return e.Err }

func (e *ProfileError) Is(target error) bool { return target == ErrInvalidProfile }
