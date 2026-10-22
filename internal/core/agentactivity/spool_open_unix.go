//go:build unix

package agentactivity

import (
	"os"
	"syscall"
)

// openSpoolAppend opens an existing file for appending without following a
// symlink in the final component, so a path swapped after the caller's check
// cannot redirect the write. O_NONBLOCK keeps a swapped-in FIFO without a
// reader from hanging the hook; it is inert on regular files.
func openSpoolAppend(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_WRONLY|os.O_APPEND|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
}
