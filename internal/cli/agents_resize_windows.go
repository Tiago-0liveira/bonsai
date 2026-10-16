//go:build windows

package cli

import "os"

// Windows has no agent sessions to attach to.
func notifyResize(chan<- os.Signal) {}
