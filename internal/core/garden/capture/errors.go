package capture

import "errors"

var (
	// ErrInvalid reports a capture request that fails validation. Nothing was written anywhere.
	ErrInvalid = errors.New("invalid garden capture")
	// ErrNeedsReconcile means GitHub may already hold the Issue: reconcile before any retry.
	ErrNeedsReconcile = errors.New("garden capture may have been created; reconcile before retrying")
	// ErrInFlight means this operation is already running in this process.
	ErrInFlight = errors.New("garden capture is already running")
	// ErrMismatch means the operation ID was used with different content or another clone.
	ErrMismatch = errors.New("garden capture operation was used for different content")
	// ErrIncomplete means the reconcile read did not cover the whole Garden, so absence proves nothing.
	ErrIncomplete = errors.New("garden capture could not be checked completely")
	// ErrTooSoon means the last attempt is too recent to conclude that no Issue exists.
	ErrTooSoon = errors.New("garden capture was attempted moments ago; check again shortly")
)
