package config

import "errors"

// ErrSuperseded reports an ordered persist that lost to a newer generation.
var ErrSuperseded = errors.New("superseded by a newer write")
