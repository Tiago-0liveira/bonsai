package planrev

import (
	"errors"
	"strings"
)

var (
	// ErrNotApproved means the specification is not a verified approved one, so no plan revision may be published
	// or decided. A stage label or a claim in the Issue never counts.
	ErrNotApproved = errors.New("garden specification is not approved")
	// ErrSpecChanged means the approved specification head or fingerprint is not the one the plan was written for.
	ErrSpecChanged = errors.New("garden specification changed since the plan was written")
	// ErrIncomplete means the plan lacks required sections; IncompleteError lists them.
	ErrIncomplete = errors.New("garden plan is incomplete")
	// ErrEmpty and ErrTooLarge refuse a plan that is blank or cannot travel as one comment.
	ErrEmpty    = errors.New("garden plan is empty")
	ErrTooLarge = errors.New("garden plan is too large")
	// ErrMatchesEarlier means the text equals a superseded revision, or one by another author. A revision is
	// addressed by its content, so the same text cannot be a second revision.
	ErrMatchesEarlier = errors.New("garden plan matches an existing revision")
	// ErrHeadsMoved means a specification or plan head moved since review; nothing was written.
	ErrHeadsMoved = errors.New("garden specification or plan heads changed since review")
	// ErrUnverified means a write succeeded but the log did not verify it afterwards.
	ErrUnverified = errors.New("garden plan record could not be verified after writing")
	// ErrAppendFailed wraps a failed append: the comment may or may not have reached GitHub. Doing the same
	// thing again finds a recorded revision or decision instead of appending a second one.
	ErrAppendFailed = errors.New("garden plan record append did not complete")
	// ErrNoActor means the authenticated account is unknown, so nothing can be attributed.
	ErrNoActor = errors.New("garden plan needs an authenticated account")
	// ErrNoRevision means no verified plan revision has the named hash.
	ErrNoRevision = errors.New("garden plan revision not found")
	// ErrNotCurrent means the named revision is not the one under review (superseded, stale, rejected, untrusted,
	// unverifiable or already decided), so a decision on it would mean nothing.
	ErrNotCurrent = errors.New("garden plan revision is not the one under review")
	// ErrReasonRequired means requesting changes needs a reason.
	ErrReasonRequired = errors.New("garden plan decision needs a reason")
	// ErrInvalidReason refuses a reason that is too long or carries Bonsai's record marker.
	ErrInvalidReason = errors.New("garden plan decision reason is not acceptable")
	// ErrActorChanged refuses confirmation by a different authenticated account.
	ErrActorChanged = errors.New("garden approval actor changed since review")
	// ErrWrongAttempt means the planner attempt belongs to another Issue or clone.
	ErrWrongAttempt = errors.New("planner attempt belongs to another issue")
)

// IncompleteError lists the sections a plan still needs.
type IncompleteError struct{ Missing []string }

func (e IncompleteError) Error() string {
	return ErrIncomplete.Error() + ": missing " + strings.Join(e.Missing, ", ")
}

func (e IncompleteError) Is(target error) bool { return target == ErrIncomplete }
