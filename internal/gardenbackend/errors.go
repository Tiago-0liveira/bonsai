package gardenbackend

import (
	"context"
	"errors"
	"strings"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/approve"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/capture"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/connect"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/edit"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/ops"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/plan"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/planrev"
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
	case errors.Is(err, approve.ErrBlocked):
		return &gardenapi.Error{Code: gardenapi.CodeValidationBlocked, Message: "This specification cannot be validated yet.", Next: "Resolve the listed blockers, then preview again."}
	case errors.Is(err, approve.ErrStale):
		return &gardenapi.Error{Code: gardenapi.CodeSpecChanged, Message: "The specification changed since the preview, so nothing was approved.", Next: "Read the new revision and validate it deliberately."}
	case errors.Is(err, approve.ErrHeadMoved):
		return &gardenapi.Error{Code: gardenapi.CodeSpecChanged, Message: "The specification changed while it was being approved. The approval belongs to the older revision and does not count.", Next: "Read the new revision and validate it deliberately."}
	case errors.Is(err, approve.ErrNoActor):
		return &gardenapi.Error{Code: gardenapi.CodeAuthRequired, Message: "GitHub sign-in is required.", Next: "Sign in with the GitHub CLI yourself, then retry.", Fix: fixLogin}
	case errors.Is(err, edit.ErrWrongIssue), errors.Is(err, approve.ErrWrongIssue):
		return e(gardenapi.CodeConflict, "GitHub returned another Issue than the one asked for.")
	case errors.Is(err, plan.ErrNotApproved):
		return &gardenapi.Error{Code: gardenapi.CodeNotApproved, Message: "No planner started: the specification is not a verified approved one.", Next: "Validate the specification first, or refresh if it was approved elsewhere."}
	case errors.Is(err, plan.ErrSpecChanged):
		return &gardenapi.Error{Code: gardenapi.CodeSpecChanged, Message: "The approved specification changed since the preview, so no planner started.", Next: "Read the new revision, validate it deliberately, then request the plan again."}
	case errors.Is(err, plan.ErrBaseChanged):
		return &gardenapi.Error{Code: gardenapi.CodeRepoChanged, Message: "The checkout changed since the preview, so no planner started.", Next: "Review the checkout, then request the plan again."}
	case errors.Is(err, plan.ErrDirtyContext):
		return &gardenapi.Error{Code: gardenapi.CodeDirtyContext, Message: "The checkout has uncommitted changes the planner would read.", Next: "Include them explicitly, or commit or stash them first."}
	case errors.Is(err, plan.ErrNotAccepted):
		return &gardenapi.Error{Code: gardenapi.CodeInvalidInput, Message: "Accept the read-only permissions to request a plan."}
	case errors.Is(err, plan.ErrReadOnlyUnsupported), errors.Is(err, agents.ErrReadOnlyUnsupported):
		return &gardenapi.Error{Code: gardenapi.CodeReadOnlyUnsupported, Message: "That account's provider cannot enforce read-only access, so it cannot plan.", Next: "Choose an account whose provider can run read-only. Bonsai never runs a planner unrestricted."}
	case errors.Is(err, plan.ErrAttemptActive):
		return &gardenapi.Error{Code: gardenapi.CodeAttemptActive, Message: "A planner attempt is already running for this Issue.", Next: "Open it, or cancel it before requesting another."}
	case errors.Is(err, plan.ErrSpecTooLarge):
		return &gardenapi.Error{Code: gardenapi.CodeInvalidInput, Message: "The specification is too large to hand to a planner."}
	case errors.Is(err, planrev.ErrNotApproved):
		return &gardenapi.Error{Code: gardenapi.CodeNotApproved, Message: "The specification is not a verified approved one, so no plan was published or decided.", Next: "Validate the specification first, or refresh if it was approved elsewhere."}
	case errors.Is(err, planrev.ErrSpecChanged):
		return &gardenapi.Error{Code: gardenapi.CodeSpecChanged, Message: "The approved specification is not the one this plan answers, so nothing was recorded.", Next: "Validate the current specification, then request and publish a plan for it."}
	case errors.Is(err, planrev.ErrIncomplete):
		return &gardenapi.Error{Code: gardenapi.CodeInvalidInput, Message: err.Error()}
	case errors.Is(err, planrev.ErrEmpty):
		return &gardenapi.Error{Code: gardenapi.CodeInvalidInput, Message: "The plan is empty."}
	case errors.Is(err, planrev.ErrTooLarge):
		return &gardenapi.Error{Code: gardenapi.CodeInvalidInput, Message: "The plan is too large to publish as one revision."}
	case errors.Is(err, planrev.ErrMatchesEarlier):
		return &gardenapi.Error{Code: gardenapi.CodeNoChange, Message: "This plan matches an existing revision that was superseded or written by someone else.", Next: "Change the text to publish it as a new revision."}
	case errors.Is(err, planrev.ErrHeadsMoved), errors.Is(err, planrev.ErrNotCurrent):
		return &gardenapi.Error{Code: gardenapi.CodeConflict, Message: "The plan revisions changed since you looked, so nothing was recorded.", Next: "Read the current revision, then decide again."}
	case errors.Is(err, planrev.ErrNoRevision), errors.Is(err, planrev.ErrWrongAttempt):
		return &gardenapi.Error{Code: gardenapi.CodeNotFound, Message: "That plan revision or planner attempt does not belong to this Issue."}
	case errors.Is(err, planrev.ErrReasonRequired):
		return &gardenapi.Error{Code: gardenapi.CodeInvalidInput, Message: "Requesting changes needs a reason."}
	case errors.Is(err, planrev.ErrInvalidReason):
		return &gardenapi.Error{Code: gardenapi.CodeInvalidInput, Message: "That reason is too long or contains a Bonsai record marker."}
	case errors.Is(err, planrev.ErrActorChanged):
		return &gardenapi.Error{Code: gardenapi.CodeInvalidInput, Message: "The GitHub account changed since review. Refresh before approving."}
	case errors.Is(err, planrev.ErrNoActor):
		return &gardenapi.Error{Code: gardenapi.CodeAuthRequired, Message: "GitHub sign-in is required.", Next: "Sign in with the GitHub CLI yourself, then retry.", Fix: fixLogin}
	case errors.Is(err, plan.ErrNotFound):
		return &gardenapi.Error{Code: gardenapi.CodeNotFound, Message: "That planner attempt no longer exists."}
	case errors.Is(err, plan.ErrCorrupt), errors.Is(err, plan.ErrUnsupportedSchema), errors.Is(err, plan.ErrTooLarge):
		return e(gardenapi.CodeStore, "Planner attempt records could not be read.")
	case errors.Is(err, agents.ErrAccountBusy), errors.Is(err, agents.ErrProviderBusy):
		return &gardenapi.Error{Code: gardenapi.CodeFailed, Message: "That account or provider already has an active agent session.", Next: "Stop it or choose another account, then request the plan again."}
	case errors.Is(err, agents.ErrAccountNotFound):
		return &gardenapi.Error{Code: gardenapi.CodeNotFound, Message: "That agent account no longer exists.", Next: "Choose another account."}
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
