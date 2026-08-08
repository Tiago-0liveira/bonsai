package exec

import "os"

// envShell returns the user's login shell from $SHELL, or "" if unset.
func envShell() string { return os.Getenv("SHELL") }

// envEditor returns the user's preferred editor from $VISUAL or $EDITOR, or ""
// if neither is set.
func envEditor() string {
	if e := os.Getenv("VISUAL"); e != "" {
		return e
	}
	return os.Getenv("EDITOR")
}
