package git

import "errors"

// ErrInvalidWorktreeBase rejects option-like user input before invoking Git.
var ErrInvalidWorktreeBase = errors.New("invalid worktree base")

// Branch name rejections. The text is the short reason shown inline, so it
// names the problem and not the operation.
var (
	ErrBranchEmpty         = errors.New("name is empty")
	ErrBranchAt            = errors.New("a single @ is not allowed")
	ErrBranchHEAD          = errors.New("HEAD is reserved")
	ErrBranchLeadingDash   = errors.New("cannot start with -")
	ErrBranchEdgeSlash     = errors.New("cannot start or end with /")
	ErrBranchDoubleSlash   = errors.New("// is not allowed")
	ErrBranchDoubleDot     = errors.New(".. is not allowed")
	ErrBranchAtBrace       = errors.New("@{ is not allowed")
	ErrBranchTrailingDot   = errors.New("cannot end with .")
	ErrBranchSpace         = errors.New("spaces are not allowed")
	ErrBranchControl       = errors.New("control characters are not allowed")
	ErrBranchForbiddenChar = errors.New(`~ ^ : ? * [ \ are not allowed`)
	ErrBranchLeadingDot    = errors.New("a path part cannot start with .")
	ErrBranchLockSuffix    = errors.New("a path part cannot end with .lock")
)
