package agents

import "errors"

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
)
