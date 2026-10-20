package gardenbackend

import (
	"context"
	"errors"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/connect"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
)

var (
	// ErrRemoteChanged means the remotes no longer point at the repository a
	// Garden was recorded for.
	ErrRemoteChanged = errors.New("remotes no longer point at the recorded repository")
	// ErrStalePlan means GitHub changed between preview and apply.
	ErrStalePlan = errors.New("plan changed since preview")
	// ErrSkipped means setup was requested while Garden is off for this clone.
	ErrSkipped = errors.New("garden is off for this clone")
	// ErrAlreadySetUp means an association other than Skip already exists.
	ErrAlreadySetUp = errors.New("garden setup is already recorded for this clone")
	// ErrPlanConflict means the plan holds a conflict Bonsai must not overwrite.
	ErrPlanConflict = errors.New("plan has conflicts")
	// ErrAuthUnavailable means gh has no usable login, so the backend is not built.
	ErrAuthUnavailable = errors.New("GitHub login unavailable")
)

// fixes are the only commands the UI may suggest. They are fixed strings: user
// data never reaches them.
const (
	fixLogin = "gh auth login"
	fixScope = "gh auth refresh -s project"
)

// apiError reduces any error to the contract's safe form. No error text is
// copied: adapters and stores may include URLs or response fragments.
func apiError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return safeAPIError(err)
}

func safeAPIError(err error) *gardenapi.Error {
	var already *gardenapi.Error
	if errors.As(err, &already) {
		return already
	}
	var scope connect.ErrScopeMissing
	var amb *connect.AmbiguousError
	e := func(code gardenapi.Code, msg string) *gardenapi.Error {
		return &gardenapi.Error{Code: code, Message: msg, Next: connect.UserMessage(err)}
	}
	switch {
	case errors.As(err, &scope):
		return &gardenapi.Error{Code: gardenapi.CodeScopeMissing, Message: "The GitHub token lacks the project scope.", Next: "Grant the scope yourself, then resume. Bonsai never refreshes or changes your login.", Fix: fixScope}
	case errors.As(err, &amb):
		return e(gardenapi.CodeTargetAmbiguous, "Your remotes point at several GitHub repositories.")
	case errors.Is(err, connect.ErrNoGitHubRemote):
		return e(gardenapi.CodeNoGitHubRemote, "No remote of this repository points at GitHub.")
	case errors.Is(err, connect.ErrAuthRequired), errors.Is(err, garden.ErrAuthRequired), errors.Is(err, ErrAuthUnavailable):
		return &gardenapi.Error{Code: gardenapi.CodeAuthRequired, Message: "GitHub sign-in is required.", Next: "Sign in with the GitHub CLI yourself, then retry.", Fix: fixLogin}
	case errors.Is(err, connect.ErrRateLimited):
		return e(gardenapi.CodeRateLimited, "GitHub rate limit reached.")
	case errors.Is(err, connect.ErrOffline):
		return e(gardenapi.CodeOffline, "GitHub could not be reached.")
	case errors.Is(err, connect.ErrPermissionDenied):
		return e(gardenapi.CodePermissionDenied, "GitHub denied access to this repository.")
	case errors.Is(err, connect.ErrReadOnly):
		return e(gardenapi.CodeReadOnly, "Setup needs Write access to this repository.")
	case errors.Is(err, connect.ErrProjectNotFound):
		return e(gardenapi.CodeProjectNotFound, "That Project was not found or is not accessible.")
	case errors.Is(err, connect.ErrProjectOwner):
		return e(gardenapi.CodeProjectOwner, "That Project belongs to another owner.")
	case errors.Is(err, connect.ErrProjectConflict), errors.Is(err, ErrPlanConflict):
		return e(gardenapi.CodeProjectConflict, "GitHub already has something Bonsai must not overwrite.")
	case errors.Is(err, connect.ErrRepositoryIdentityChanged), errors.Is(err, ErrRemoteChanged):
		return e(gardenapi.CodeIdentityChanged, "The remotes no longer point at the repository this Garden was set up for.")
	case errors.Is(err, connect.ErrGitHubValidation):
		return e(gardenapi.CodeValidation, "GitHub rejected the request.")
	case errors.Is(err, garden.ErrGraphQL):
		return &gardenapi.Error{Code: gardenapi.CodeGraphQL, Message: "GitHub rejected Bonsai's GraphQL request.", Next: "Update Bonsai, then resume setup. If it persists, report this error."}
	case errors.Is(err, connect.ErrOperationInProgress):
		return e(gardenapi.CodeOperationInProgress, "A setup is already running for this clone.")
	case errors.Is(err, connect.ErrOperationNotFound), errors.Is(err, connect.ErrOperationForeign), errors.Is(err, connect.ErrInvalidOperation) && !errors.Is(err, connect.ErrCorruptStore) && !errors.Is(err, connect.ErrUnsupportedSchema):
		return e(gardenapi.CodeOperationNotFound, "That setup operation does not belong to this clone.")
	case errors.Is(err, connect.ErrCorruptStore), errors.Is(err, connect.ErrUnsupportedSchema), errors.Is(err, connect.ErrStoreTooLarge):
		return e(gardenapi.CodeStore, "Garden setup state could not be read.")
	case errors.Is(err, ErrStalePlan):
		return e(gardenapi.CodeStalePlan, "GitHub changed since the plan was previewed.")
	case errors.Is(err, connect.ErrRevision), errors.Is(err, connect.ErrIdempotency), errors.Is(err, ErrAlreadySetUp):
		return e(gardenapi.CodeConflict, "Garden setup changed elsewhere.")
	case errors.Is(err, ErrSkipped):
		return e(gardenapi.CodeSkipped, "Garden is off for this clone.")
	}
	return e(gardenapi.CodeFailed, "Garden could not complete the request.")
}
