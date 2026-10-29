//go:build !windows && !darwin && !linux

package procstore

import "time"

// ProcessMatches fails closed: without a kernel-reported start time a reused
// PID cannot be told apart from the process bonsai started, and orphan
// recovery must not adopt or kill an unrelated process.
func ProcessMatches(int, time.Time, string) bool { return false }
