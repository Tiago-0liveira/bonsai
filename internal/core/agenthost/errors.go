package agenthost

import "errors"

// ErrAdmissionBusy means another process held the start lock for too long.
var ErrAdmissionBusy = errors.New("another agent is starting; try again")

var (
	// ErrOperationConflict means an operation or session identity is already
	// bound to a different start request.
	ErrOperationConflict = errors.New("agent operation identity reused with a different request")
	// ErrOperationClosed means the operation was fenced or ended and can no
	// longer start a holder; a new attempt needs a new identity.
	ErrOperationClosed = errors.New("agent operation is closed")
	// ErrOperationUnknown means the operation's receipt or holder cannot be read
	// well enough to say what happened. It is never evidence of absence.
	ErrOperationUnknown = errors.New("agent operation outcome unknown")
)
