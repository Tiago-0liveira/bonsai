package sync

import "errors"

var (
	// ErrForeignGarden prevents data from another remote repository entering a store.
	ErrForeignGarden = errors.New("garden sync result belongs to another garden")
	// ErrInvalidLimits marks a read request that cannot be bounded safely.
	ErrInvalidLimits = errors.New("invalid garden sync limits")
)
