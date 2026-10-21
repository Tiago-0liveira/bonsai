package launch

import "errors"

// Sentinels are matched with errors.Is. Their messages are fixed and safe to
// show; the wrapped cause, when there is one, stays reachable through the chain
// but is never persisted.
var (
	ErrInvalid                = errors.New("invalid implementation launch")
	ErrNotAccepted            = errors.New("launch not confirmed")
	ErrRemoteStale            = errors.New("remote launch snapshot stale")
	ErrSpecChanged            = errors.New("approved spec changed")
	ErrPlanChanged            = errors.New("approved plan changed")
	ErrApprovalMissing        = errors.New("verified approvals required")
	ErrAccess                 = errors.New("current Garden write access required")
	ErrAccountUnavailable     = errors.New("implementation account unavailable")
	ErrPermissionsUnresolved  = errors.New("native permissions unresolved")
	ErrPermissionsUnsupported = errors.New("native permissions unsupported")
	ErrEscalationNotAccepted  = errors.New("permission escalation not confirmed")
	ErrOwnership              = errors.New("launch ownership ambiguous")
	ErrBaseChanged            = errors.New("approved base changed")
	ErrBranchCollision        = errors.New("implementation branch exists")
	ErrPathCollision          = errors.New("implementation path exists")
	ErrInputsChanged          = errors.New("confirmed launch inputs changed")
	ErrAttemptActive          = errors.New("implementation attempt active")
	ErrIdempotency            = errors.New("operation identity reused with different inputs")
	ErrOutcomeUnknown         = errors.New("effect needs reconciliation")
	ErrNotRetryable           = errors.New("attempt cannot be retried")
	ErrCleanupUnsafe          = errors.New("worktree unsafe to remove")
	ErrCleanupChanged         = errors.New("cleanup preview changed")
	ErrNotFound               = errors.New("implementation attempt not found")
	ErrCorrupt                = errors.New("implementation store corrupt")
	ErrUnsupportedSchema      = errors.New("unsupported implementation store schema")
	ErrTooLarge               = errors.New("implementation record exceeds limits")
)
