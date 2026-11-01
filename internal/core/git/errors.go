package git

import "errors"

// ErrInvalidWorktreeBase rejects option-like user input before invoking Git.
var ErrInvalidWorktreeBase = errors.New("invalid worktree base")
