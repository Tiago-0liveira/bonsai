package approve

import (
	"errors"
	"strings"
)

var (
	// ErrBlocked means the specification cannot be validated; BlockedError carries the reasons.
	ErrBlocked = errors.New("garden specification cannot be validated")
	// ErrStale means the specification head moved since the preview. Nothing was written.
	ErrStale = errors.New("garden specification changed since the preview")
	// ErrHeadMoved means a newer revision landed while the approval was being recorded. The approval stays in the
	// log as superseded; no acceptance was projected.
	ErrHeadMoved = errors.New("garden specification changed while it was being approved")
	// ErrAppendFailed wraps a failed approval append: the comment may or may not have reached GitHub. Validating
	// again finds a recorded approval instead of appending a second one.
	ErrAppendFailed = errors.New("garden approval append did not complete")
	// ErrProjection means the approval is recorded but the stage label write did not finish. The Issue
	// stays a draft; validating again completes it.
	ErrProjection = errors.New("garden approval is recorded but its projection is incomplete")
	// ErrUnverified means a write succeeded but the log did not verify afterwards.
	ErrUnverified = errors.New("garden approval could not be verified after writing")
	// ErrNoActor means the authenticated account is unknown, so no approval can be attributed.
	ErrNoActor = errors.New("garden approval needs an authenticated account")
	// ErrWrongIssue means the read returned another Issue than the one asked for.
	ErrWrongIssue = errors.New("garden issue identity mismatch")
)

// BlockedError lists why validation is unavailable.
type BlockedError struct{ Blockers []Blocker }

func (e BlockedError) Error() string {
	var parts []string
	for _, b := range e.Blockers {
		parts = append(parts, b.Message)
	}
	return ErrBlocked.Error() + ": " + strings.Join(parts, "; ")
}

func (e BlockedError) Is(target error) bool { return target == ErrBlocked }
