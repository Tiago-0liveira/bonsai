//go:build windows

package agenthost

import (
	"fmt"
	"os"
)

// Agent holders need Unix PTYs; Windows never runs one.
func lockFile(string) (*os.File, error) {
	return nil, fmt.Errorf("agent hosts are unsupported on this platform")
}

func held(string) bool { return false }
