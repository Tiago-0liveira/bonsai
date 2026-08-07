package exec

import "os"

// envShell returns the user's login shell from $SHELL, or "" if unset.
func envShell() string { return os.Getenv("SHELL") }
