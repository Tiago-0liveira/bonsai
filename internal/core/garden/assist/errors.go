package assist

import "errors"

var (
	ErrInvalidInput = errors.New("assist-invalid-input")
	ErrMalformed    = errors.New("assist-malformed")
	ErrTooLarge     = errors.New("assist-too-large")
)
