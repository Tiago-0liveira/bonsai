package drafts

import "errors"

var (
	ErrNotFound          = errors.New("private draft not found")
	ErrConflict          = errors.New("private draft revision conflict")
	ErrCorrupt           = errors.New("invalid private draft store")
	ErrTooLarge          = errors.New("private draft store too large")
	ErrUnsupportedSchema = errors.New("unsupported private draft schema")
	ErrInvalid           = errors.New("invalid private draft")
	ErrUnavailable       = errors.New("private draft storage unavailable")
)

// ConflictError identifies the current draft. A stale Save also supplies the
// durable recovery copy in Incoming; a stale Archive leaves Incoming empty.
type ConflictError struct{ Current, Incoming Draft }

func (e *ConflictError) Error() string { return ErrConflict.Error() }
func (e *ConflictError) Unwrap() error { return ErrConflict }
