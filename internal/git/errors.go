package git

import "errors"

type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string        { return e.Message }
func (e *Error) Is(target error) bool { t, ok := target.(*Error); return ok && e.Code == t.Code }
func E(code, message string) error    { return &Error{code, message} }

var (
	ErrNotFound  = E("not_found", "repository, worktree or ref not found")
	ErrInvalid   = E("invalid", "invalid argument")
	ErrConflict  = E("conflict", "Git operation has conflicts")
	ErrDirty     = E("dirty_worktree", "worktree contains uncommitted changes")
	ErrAuth      = E("unauthorized", "authentication required")
	ErrForbidden = E("forbidden", "access denied")
	ErrOffline   = E("daemon_offline", "daemon is offline")
	ErrProtected = E("protected", "GitHub rejected the protected operation")
	ErrBusy      = E("busy", "an operation is already in progress")
	ErrUncertain = E("outcome_unknown", "command was interrupted; reconcile before retrying")
)

func Code(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return "internal"
}
