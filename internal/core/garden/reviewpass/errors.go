package reviewpass

import "errors"

var (
	ErrInvalid           = errors.New("invalid review pass")
	ErrNotAuthorized     = errors.New("review pass is not authorized")
	ErrNotFound          = errors.New("review pass not found")
	ErrConflict          = errors.New("review pass identity conflicts")
	ErrActive            = errors.New("an active review pass already exists for this attempt")
	ErrImmutable         = errors.New("reserved review pass fields are immutable")
	ErrCorrupt           = errors.New("review pass store is corrupt")
	ErrUnsupportedSchema = errors.New("review pass store schema is newer than supported")
	ErrFull              = errors.New("review pass store is full")
	ErrTooLarge          = errors.New("review pass store exceeds its size limit")
)
