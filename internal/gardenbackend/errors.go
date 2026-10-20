package gardenbackend

import (
	"context"
	"errors"
	"strings"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/capture"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/connect"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/edit"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/ops"
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
	var earlier edit.MatchesEarlierError
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
	case errors.Is(err, edit.ErrNoChange):
		return &gardenapi.Error{Code: gardenapi.CodeNoChange, Message: "There is nothing to save: the specification already says this."}
	case errors.As(err, &earlier):
		return &gardenapi.Error{Code: gardenapi.CodeNoChange, Message: "This text matches an earlier revision of the specification.", Next: "Reverting to an earlier revision is not supported yet. Change the text a little to save it as new."}
	case errors.Is(err, edit.ErrIncomplete):
		return &gardenapi.Error{Code: gardenapi.CodeDocumentIncomplete, Message: "GitHub's revision log could not be read completely, so nothing was saved.", Next: "Refresh and try again."}
	case errors.Is(err, edit.ErrIntegrity):
		return &gardenapi.Error{Code: gardenapi.CodeDocumentIncomplete, Message: "The revision log holds records that fail verification, so nothing was saved.", Next: "Fix or remove the broken comment on GitHub, then refresh."}
	case errors.Is(err, edit.ErrEmpty):
		return &gardenapi.Error{Code: gardenapi.CodeInvalidInput, Message: "The specification is empty."}
	case errors.Is(err, edit.ErrTooLarge), errors.Is(err, garden.ErrCommentTooLarge):
		return &gardenapi.Error{Code: gardenapi.CodeInvalidInput, Message: "The specification is too large to save as one revision."}
	case errors.Is(err, edit.ErrUnverified):
		return &gardenapi.Error{Code: gardenapi.CodeSaveUnknown, Message: "The revision was saved but could not be found again.", Next: "Refresh to read the current state."}
	case errors.Is(err, edit.ErrWrongIssue):
		return e(gardenapi.CodeConflict, "GitHub returned another Issue than the one asked for.")
	case errors.Is(err, garden.ErrNotFound):
		return &gardenapi.Error{Code: gardenapi.CodeNotFound, Message: "That Issue no longer exists on GitHub.", Next: "Refresh the Garden."}
	case errors.Is(err, capture.ErrInvalid):
		return &gardenapi.Error{Code: gardenapi.CodeInvalidInput, Message: strings.TrimPrefix(err.Error(), capture.ErrInvalid.Error()+": ")}
	case errors.Is(err, capture.ErrInFlight):
		return e(gardenapi.CodeOperationInProgress, "That capture is already running.")
	case errors.Is(err, capture.ErrMismatch):
		return e(gardenapi.CodeConflict, "That capture belongs to different content or another clone.")
	case errors.Is(err, ops.ErrCorrupt), errors.Is(err, ops.ErrUnsupportedSchema), errors.Is(err, ops.ErrTooLarge):
		return e(gardenapi.CodeStore, "Garden operation records could not be read.")
	case errors.Is(err, garden.ErrRejected), errors.Is(err, connect.ErrGitHubValidation):
		return e(gardenapi.CodeValidation, "GitHub rejected the request.")
	case errors.Is(err, garden.ErrForbidden):
		return e(gardenapi.CodePermissionDenied, "GitHub refused the request.")
	case errors.Is(err, connect.ErrRateLimited), errors.Is(err, garden.ErrRateLimited):
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
