package agentsbackend

import "errors"

// ErrSessionNotFound means no agent session record has the requested ID.
var ErrSessionNotFound = errors.New("agent session not found")

// ErrSessionUnavailable hides holder, filesystem and provider details from UI errors.
var ErrSessionUnavailable = errors.New("agent session unavailable")

// ErrSessionEnded means the retained session has no holder to control.
var ErrSessionEnded = errors.New("agent session has ended")

// ErrBackendClosed means the TUI has released its attachments.
var ErrBackendClosed = errors.New("agent backend closed")

// ErrAccountUnavailable hides provider, cache and filesystem details from UI errors.
var ErrAccountUnavailable = errors.New("agent account unavailable")

var errInputUnavailable = errors.New("terminal input unavailable")
var errResizeUnavailable = errors.New("terminal resize unavailable")
var errReadOnly = errors.New("read-only attachment")
