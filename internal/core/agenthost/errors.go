package agenthost

import "errors"

// ErrAdmissionBusy means another process held the start lock for too long.
var ErrAdmissionBusy = errors.New("another agent is starting; try again")
