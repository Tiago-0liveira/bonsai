//go:build !unix

package agentactivity

import "os"

// openSpoolAppend has no O_NOFOLLOW to use here; the regular-file check on the
// descriptor still rejects non-files.
func openSpoolAppend(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
}
