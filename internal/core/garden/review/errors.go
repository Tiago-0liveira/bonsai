package review

import "errors"

var (
	ErrCorrupt           = errors.New("review store is corrupt")
	ErrUnsupportedSchema = errors.New("review store has a newer schema")
	ErrFull              = errors.New("review store is full")
	ErrInvalid           = errors.New("invalid review record")
	// ErrInvalidReport: the report file broke the contract; the run ends in state invalid.
	ErrInvalidReport = errors.New("invalid review report")
	ErrNotFound      = errors.New("review record not found")
	// ErrConflict: a stale revision, or a run that already finished.
	ErrConflict = errors.New("review record changed")
	// ErrRunInFlight: the attempt already has a running reviewer.
	ErrRunInFlight = errors.New("a review run is already in flight for this attempt")
	// ErrAutoLimit: the locally authorized automatic run budget is exhausted.
	ErrAutoLimit = errors.New("automatic review run limit reached")
	// ErrStale: HEAD moved since the preview; preview again.
	ErrStale = errors.New("attempt head changed since the preview")
	// ErrNeedsAcknowledgment: a required acknowledgment is missing.
	ErrNeedsAcknowledgment = errors.New("acknowledgment required")
	ErrNotConfirmed        = errors.New("acceptance not confirmed")
	// ErrReportMissing: the reviewer left no report file.
	ErrReportMissing = errors.New("review report file is missing")
	// ErrReportUnsafe: the report path is not a private, unlinked, plain file of the run's own directory.
	ErrReportUnsafe = errors.New("review report path is not safe")
)
