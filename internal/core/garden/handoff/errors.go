package handoff

import "errors"

var (
	// ErrNoActor means the authenticated account is unknown, so nothing can be attributed.
	ErrNoActor = errors.New("garden handoff needs an authenticated account")
	// ErrActorChanged refuses a decision by an account other than the one the user was shown.
	ErrActorChanged = errors.New("garden handoff actor changed since review")
	// ErrReadOnly refuses to record while the repository grants no write access.
	ErrReadOnly = errors.New("garden handoff needs write access to the repository")
	// ErrInputsChanged means the specification or plan in the request is not the verified approved pair.
	ErrInputsChanged = errors.New("garden specification or plan changed since review")
	// ErrIncompleteLog means the comment log could not be read completely, so nothing is decided from it.
	ErrIncompleteLog = errors.New("garden comment log is incomplete")
	// ErrInvalidDecision refuses an unknown decision or a revocation without a target.
	ErrInvalidDecision = errors.New("garden handoff decision is not valid")
	// ErrTextRequired means changes requested and completion need the human's words.
	ErrTextRequired = errors.New("garden handoff needs a written note")
	// ErrInvalidText refuses text that is too long or carries Bonsai's record marker.
	ErrInvalidText = errors.New("garden handoff text is not acceptable")
	// ErrNoTarget means the comment to revoke is not a live handoff record.
	ErrNoTarget = errors.New("garden handoff record to revoke not found")
	// ErrAppendFailed wraps a failed append: the comment may or may not have reached GitHub. Deciding again
	// finds a recorded decision instead of appending a second one.
	ErrAppendFailed = errors.New("garden handoff append did not complete")
	// ErrUnverified means a write succeeded but the log did not verify it afterwards.
	ErrUnverified = errors.New("garden handoff record could not be verified after writing")
)
