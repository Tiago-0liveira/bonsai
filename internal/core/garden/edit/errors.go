package edit

import (
	"errors"
	"fmt"
)

var (
	// ErrIncomplete means the comment log could not be read completely, so heads cannot be trusted. Nothing was written.
	ErrIncomplete = errors.New("garden revision log could not be read completely")
	// ErrIntegrity means the log holds records that fail verification; editing on top of it could hide the problem.
	ErrIntegrity = errors.New("garden revision log has integrity problems")
	// ErrNoChange means the content equals the current specification.
	ErrNoChange = errors.New("garden specification is unchanged")
	// ErrEmpty means every section is empty.
	ErrEmpty = errors.New("garden specification is empty")
	// ErrTooLarge means the rendered specification does not fit in one comment.
	ErrTooLarge = errors.New("garden specification is too large")
	// ErrUnverified means a revision was appended but could not be found again.
	ErrUnverified = errors.New("garden revision was saved but could not be verified")
	// ErrAppendFailed wraps a failed append: the revision may or may not have reached GitHub. Saving the same
	// preview again looks for it before appending.
	ErrAppendFailed = errors.New("garden revision append did not complete")
	// ErrWrongIssue means the read returned another Issue than the one asked for.
	ErrWrongIssue = errors.New("garden issue identity mismatch")
)

// MatchesEarlierError means the content equals an earlier revision. Appending it would collide with that
// revision's hash, so it is refused until reverting is a supported operation.
type MatchesEarlierError struct{ CommentID int64 }

func (e MatchesEarlierError) Error() string {
	return fmt.Sprintf("garden specification matches earlier revision %d; revert is not supported yet", e.CommentID)
}
