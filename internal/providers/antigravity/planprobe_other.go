//go:build !linux && !darwin && !freebsd && !openbsd && !netbsd && !dragonfly

package antigravity

import (
	"context"
	"errors"
)

// planProbeSupported is false where creack/pty has no PTY (Windows); the plan
// stays blank there.
const planProbeSupported = false

var errUnsupported = errors.New("plan probe unsupported on this platform")

func probePlan(context.Context, string, string, []string, string) (string, error) {
	return "", errUnsupported
}
