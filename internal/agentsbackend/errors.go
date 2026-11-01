package agentsbackend

import "errors"

// ErrSessionNotFound means no agent session record has the requested ID.
var ErrSessionNotFound = errors.New("agent session not found")

// ErrSessionUnavailable hides holder, filesystem and provider details from UI errors.
var ErrSessionUnavailable = errors.New("agent session unavailable")

// ErrInvalidName means a rename asked for an empty or over-long name.
var ErrInvalidName = errors.New("invalid agent session name")

// ErrRenameUnsupported means the session's holder predates renaming.
var ErrRenameUnsupported = errors.New("this agent session cannot be renamed until it is restarted")

// ErrSessionEnded means the retained session has no holder to control.
var ErrSessionEnded = errors.New("agent session has ended")

// ErrBackendClosed means the TUI has released its attachments.
var ErrBackendClosed = errors.New("agent backend closed")

// ErrAccountUnavailable hides provider, cache and filesystem details from UI errors.
var ErrAccountUnavailable = errors.New("agent account unavailable")

var errInputUnavailable = errors.New("terminal input unavailable")
var errResizeUnavailable = errors.New("terminal resize unavailable")
var errReadOnly = errors.New("read-only attachment")
