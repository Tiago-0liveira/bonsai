package sync

import "errors"

var (
	// ErrForeignGarden prevents data from another remote repository entering a store.
	ErrForeignGarden = errors.New("garden sync result belongs to another garden")
	// ErrForeignLocal prevents a result for another local clone from entering a store.
	ErrForeignLocal = errors.New("garden sync result belongs to another local repository")
	// ErrInvalidLimits marks a read request that cannot be bounded safely.
	ErrInvalidLimits = errors.New("invalid garden sync limits")
)
