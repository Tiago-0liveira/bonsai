package publish

import "errors"

var (
	// ErrStale means the draft, the target or the account changed since the preview. Nothing was written.
	ErrStale = errors.New("garden draft or target changed since the preview")
	// ErrNotReady means the draft cannot be approved yet: readiness or the approval blockers fail.
	ErrNotReady = errors.New("garden draft is not ready to publish")
	// ErrInFlight means this publication is already running, here or in another Bonsai process.
	ErrInFlight = errors.New("garden publication is already running")
	// ErrMismatch means the operation is bound to a different draft, content, target or account.
	ErrMismatch = errors.New("garden publication was used for different content")
	// ErrAlreadyPublished means the draft was published by another operation.
	ErrAlreadyPublished = errors.New("garden draft is already published")
	// ErrPublishing means an earlier publication of the draft has not finished: resume it instead.
	ErrPublishing = errors.New("garden draft is already being published; resume it")
	// ErrConflict means the new Issue holds a specification revision or head that is not ours.
	// Nothing further is written: approving someone else's revision is never an option.
	ErrConflict = errors.New("garden issue has a specification revision that is not this draft's")
	// ErrUnverified means a finished step could not be read back as expected.
	ErrUnverified = errors.New("garden publication could not be verified on GitHub")
)
