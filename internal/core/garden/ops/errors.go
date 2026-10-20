// Package ops persists the durable records of Garden writes that GitHub may have
// applied even when the call failed: capture and import. Each record is written
// before its remote call, so a crash or timeout is always reconcilable.
package ops

import "errors"

var (
	// ErrCorrupt reports unreadable or inconsistent persisted records.
	ErrCorrupt = errors.New("garden operation store is corrupt")
	// ErrUnsupportedSchema reports records written by a newer Bonsai.
	ErrUnsupportedSchema = errors.New("unsupported garden operation store schema")
	// ErrTooLarge reports a store past its size or record cap.
	ErrTooLarge = errors.New("garden operation store exceeds its limit")
	// ErrNotFound reports an unknown operation ID.
	ErrNotFound = errors.New("garden operation not found")
)
