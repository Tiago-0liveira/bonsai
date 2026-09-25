package agym

import (
	"errors"
	"fmt"
)

// Standard AGYM protocol error codes.
const (
	ErrCodeProfileUnavailable    = "PROFILE_UNAVAILABLE"
	ErrCodeNoCapacity            = "NO_CAPACITY"
	ErrCodeQuotaExhausted        = "QUOTA_EXHAUSTED"
	ErrCodeUnsupportedCapability = "UNSUPPORTED_CAPABILITY"
	ErrCodeRunNotFound           = "RUN_NOT_FOUND"
	ErrCodeWorkspaceBusy         = "WORKSPACE_BUSY"
	ErrCodeWorkspaceMissing      = "WORKSPACE_MISSING"
	ErrCodeIdempotencyConflict   = "IDEMPOTENCY_CONFLICT"
	ErrCodeNeedsAttention        = "NEEDS_ATTENTION"
	ErrCodeCursorExpired         = "CURSOR_EXPIRED"
)

// Sentinel errors for client and protocol boundary failures.
var (
	ErrNotInstalled         = errors.New("agym executable not found in PATH")
	ErrProtocolIncompatible = errors.New("unsupported agym protocol version")
	ErrCapabilityMissing    = errors.New("required agym capability missing")
	ErrTransport            = errors.New("agym command execution failed")
	ErrInvalidResponse      = errors.New("invalid or malformed agym response")
	ErrRunNotFound          = errors.New("run not found")
	ErrWorkspaceBusy        = errors.New("workspace is already running an agent")
	ErrQuotaExhausted       = errors.New("quota exhausted")
)

// ProtocolError wraps an ErrorPayload returned by AGYM into a Go error.
type ProtocolError struct {
	Code              string
	Message           string
	Retryable         bool
	RetryAfterSeconds *int
	RunID             string
}

func (e *ProtocolError) Error() string {
	if e.RunID != "" {
		return fmt.Sprintf("agym error [%s]: %s (run %s)", e.Code, e.Message, e.RunID)
	}
	return fmt.Sprintf("agym error [%s]: %s", e.Code, e.Message)
}

// AsProtocolError attempts to extract a *ProtocolError from an error.
func AsProtocolError(err error) (*ProtocolError, bool) {
	var pe *ProtocolError
	if errors.As(err, &pe) {
		return pe, true
	}
	return nil, false
}
