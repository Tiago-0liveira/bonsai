package plan

import "errors"

var (
	// ErrNotApproved means the specification is not a verified approved one, so no planner may start. A
	// stage label or a claim in the Issue body never counts.
	ErrNotApproved = errors.New("garden specification is not approved")
	// ErrSpecChanged means the approved specification head or fingerprint is not the one that was previewed.
	ErrSpecChanged = errors.New("garden specification changed since the preview")
	// ErrBaseChanged means the inspected checkout (commit or uncommitted changes) is not the one previewed.
	ErrBaseChanged = errors.New("repository changed since the preview")
	// ErrDirtyContext means the checkout has uncommitted changes the user did not explicitly include.
	ErrDirtyContext = errors.New("uncommitted changes need an explicit decision")
	// ErrNotAccepted means the read-only permissions were not accepted for this attempt.
	ErrNotAccepted = errors.New("planner permissions were not accepted")
	// ErrReadOnlyUnsupported means the chosen account's provider cannot enforce read-only access. The attempt
	// is refused; it never runs unrestricted.
	ErrReadOnlyUnsupported = errors.New("provider cannot run the planner role read-only")
	// ErrAttemptActive means an earlier attempt for the Issue is still running. A retry is a new attempt, so
	// the earlier one must end first.
	ErrAttemptActive = errors.New("a planner attempt is already running for this issue")
	// ErrSpecTooLarge means the specification cannot be handed to a planner as one prompt.
	ErrSpecTooLarge = errors.New("specification is too large for a planner prompt")
	// ErrNotFound means no attempt has that ID.
	ErrNotFound = errors.New("planner attempt not found")
	// ErrCorrupt reports unreadable or inconsistent persisted attempts.
	ErrCorrupt = errors.New("planner attempt store is corrupt")
	// ErrUnsupportedSchema reports attempts written by a newer Bonsai.
	ErrUnsupportedSchema = errors.New("unsupported planner attempt store schema")
	// ErrTooLarge reports a store past its size or record cap.
	ErrTooLarge = errors.New("planner attempt store exceeds its limit")
)
