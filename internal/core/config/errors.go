package config

import "errors"

// ErrSuperseded reports an ordered persist that lost to a newer generation.
var ErrSuperseded = errors.New("superseded by a newer write")

// ErrInvalidGardenView rejects values that could persist content instead of a view name.
var ErrInvalidGardenView = errors.New("invalid Garden view")

// ErrInvalidGardenDraft rejects a remembered draft reference that is not an opaque ID and section.
var ErrInvalidGardenDraft = errors.New("invalid Garden draft reference")

// ErrInvalidGardenReview rejects a local review policy that could not be applied safely.
var ErrInvalidGardenReview = errors.New("invalid Garden review policy")
