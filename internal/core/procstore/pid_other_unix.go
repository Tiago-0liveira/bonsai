//go:build !windows && !darwin && !linux

package procstore

import "time"

// ProcessMatches fails closed: without a kernel-reported start time a reused
// PID cannot be told apart from the process bonsai started, and orphan
// recovery must not adopt or kill an unrelated process.
func ProcessMatches(int, time.Time, uint64, string, string) bool { return false }

// StartIdentity returns an empty identity because Linux start ticks and boot IDs are unavailable here.
func StartIdentity(int) (uint64, string) { return 0, "" }
