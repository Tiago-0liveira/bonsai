package launch

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/approve"
	domain "github.com/Tiago-0liveira/bonsai/internal/git"
	"github.com/Tiago-0liveira/bonsai/internal/git/github"
)

func isSHA(s string) bool {
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

// prBase is the branch a pull request targets, only when the base ref plainly
// names one. A detached commit can be implemented against but is never guessed
// into a pull request base.
func prBase(ref, remote string) (string, bool) {
	if ref == "" || isSHA(ref) {
		return "", false
	}
	return config.BranchOnRemote(ref, remote), true
}

// prMatch classifies the pull requests GitHub lists for the branch. A single
// candidate with exactly this repository and head commit is ours; anything else
// on the same head and base is ambiguous and never authorizes a create.
type prMatch struct {
	one       *github.PullRequest
	ambiguous bool
	related   int
}

func classifyPRs(list []github.PullRequest, repo, branch, base, head string) prMatch {
	var related, exact []github.PullRequest
	for _, pr := range list {
		if pr.Head != branch || pr.Base != base {
			continue
		}
		related = append(related, pr)
		if pr.HeadRepository != "" && strings.EqualFold(pr.HeadRepository, repo) && pr.HeadSHA == head && pr.Number > 0 {
			exact = append(exact, pr)
		}
	}
	m := prMatch{related: len(related)}
	switch {
	case len(related) == 0:
	case len(related) == 1 && len(exact) == 1:
		m.one = &exact[0]
	default:
		m.ambiguous = true
	}
	return m
}

func linkable(url, repo string, n int) bool {
	return strings.EqualFold(url, fmt.Sprintf("https://github.com/%s/pull/%d", repo, n))
}

func definitiveRefusal(err error) bool {
	return errors.Is(err, domain.ErrAuth) || errors.Is(err, domain.ErrForbidden) || errors.Is(err, domain.ErrProtected) ||
		errors.Is(err, garden.ErrRejected) || errors.Is(err, garden.ErrAuthRequired) || errors.Is(err, garden.ErrPermissionDenied) ||
		errors.Is(err, garden.ErrForbidden)
}

// terminalClosed reports an attempt whose newest run ended after a real start,
// the only state whose branch may be published.
func terminalClosed(a Attempt) bool {
	switch a.State {
	case StateAwaitingReview, StateFailed, StateStopped:
	default:
		return false
	}
	if len(a.Runs) == 0 {
		return false
	}
	r := a.Runs[len(a.Runs)-1]
	return terminalRun(r.State) && r.Acknowledged
}

// Publish pushes the retained branch and opens or links one Draft pull request,
// after its own confirmation. It runs only against a closed, acknowledged
// holder and a worktree proven the attempt's, and only for a clean committed
// head that differs from the approved base. Each GitHub effect is preceded by a
// read and recorded as Pending or Unknown before it is made, so a retry finds
// what happened instead of repeating it.
func (s *Service) Publish(ctx context.Context, req PublishRequest) (Attempt, error) {
	if !req.Confirmed {
		return Attempt{}, ErrNotAccepted
	}
	a, err := s.owned(ctx, req.AttemptID)
	if err != nil {
		return Attempt{}, err
	}
	if !terminalClosed(a) {
		return a, ErrNotRetryable
	}
	unlock, err := s.d.Store.Lock(ctx, a.ID)
	if err != nil {
		return s.partial(ctx, a.ID, fmt.Errorf("%w: the attempt lock was not acquired: %w", ErrOutcomeUnknown, err))
	}
	defer unlock()
	if a, err = s.d.Store.Get(ctx, a.ID); err != nil {
		return Attempt{}, err
	}
	if !terminalClosed(a) {
		return a, ErrNotRetryable
	}
	return s.publish(ctx, a)
}

func (s *Service) publish(ctx context.Context, a Attempt) (Attempt, error) {
	check, err := s.d.Worktrees.InspectIsolation(ctx, a.Proposal)
	if err != nil {
		return a, isolationError(err)
	}
	if !check.Exists || !check.Owned {
		return a, fmt.Errorf("%w: the retained worktree is not proven this attempt's", ErrOwnership)
	}
	head, err := s.d.Worktrees.InspectPublishedHead(ctx, a.Proposal)
	if err != nil {
		return a, isolationError(err)
	}
	linked := a.PR.State == PRLinked
	// Unknown means a create may have landed: no blocker may forget it, and it
	// keeps its discovery-only rule.
	keep := linked || a.PR.State == PRUnknown
	switch {
	case head.Dirty:
		if keep {
			return a, nil
		}
		return s.update(ctx, a.ID, func(a *Attempt) bool {
			a.PR = PRLink{State: PRPending, Code: codePushPending}
			s.setStep(a, StepPush, StepPending, codePushPending)
			return true
		})
	case !head.Meaningful:
		if keep {
			return a, nil
		}
		return s.update(ctx, a.ID, func(a *Attempt) bool {
			a.PR = PRLink{State: PRNotCreated}
			s.setStep(a, StepPush, StepSkipped, "")
			s.setStep(a, StepPR, StepSkipped, "")
			return true
		})
	}
	if s.d.PRs == nil {
		if a.PR.State == PRUnknown || linked {
			return a, nil
		}
		return s.update(ctx, a.ID, func(a *Attempt) bool {
			a.PR = PRLink{State: PRUnavailable, Code: codePRUnavailable}
			s.setStep(a, StepPR, StepPending, codePRUnavailable)
			return true
		})
	}
	if err := s.verifyAccess(ctx, a); err != nil {
		return a, err
	}
	base, namesBranch := prBase(a.Proposal.BaseRef, a.Proposal.RemoteName)
	if !namesBranch && !linked {
		return s.update(ctx, a.ID, func(a *Attempt) bool {
			if a.PR.State != PRUnknown {
				a.PR = PRLink{State: PRUnavailable, Code: codePRUnavailable}
			}
			s.setStep(a, StepPR, StepPending, codePRUnavailable)
			return true
		})
	}
	// The intent is durable before the first effect.
	if a, err = s.update(ctx, a.ID, func(a *Attempt) bool {
		if a.PR.State == PRNotCreated || a.PR.State == PRUnavailable {
			a.PR = PRLink{State: PRPending, Code: codePRPending}
		}
		return s.setStep(a, StepPush, StepRunning, "")
	}); err != nil {
		return s.partial(ctx, a.ID, err)
	}
	if a, err = s.pushHead(ctx, a, head); err != nil {
		return s.partial(ctx, a.ID, err)
	}
	repo := a.Proposal.Repository
	branches, err := s.d.PRs.Branches(ctx, repo)
	if err != nil {
		return s.pending(ctx, a, fmt.Errorf("%w: GitHub's branch list: %w", ErrRemoteStale, err))
	}
	proven := false
	for _, b := range branches {
		if b.Name == a.Proposal.Branch && strings.EqualFold(b.RemoteHeadSHA, head.HeadSHA) {
			proven = true
		}
	}
	if !proven {
		return s.pending(ctx, a, fmt.Errorf("%w: GitHub does not show the pushed head yet", ErrRemoteStale))
	}
	return s.openOrLink(ctx, a, head, base)
}

// verifyAccess re-reads the record with current Write access, for the attempt's
// own repository, actor and association.
func (s *Service) verifyAccess(ctx context.Context, a Attempt) error {
	snap, err := s.d.Remote.Verify(ctx, approve.Target{Repo: a.Repo, Issue: a.Issue, Viewer: a.ActorID})
	if err != nil {
		if isContext(err) {
			return err
		}
		return remoteError(err)
	}
	if snap.Local != a.Local || snap.Target.Repo != a.Repo || snap.Target.Issue != a.Issue || snap.Target.Viewer != a.ActorID {
		return fmt.Errorf("%w: identity", ErrOwnership)
	}
	if snap.Epoch != a.Epoch || !strings.EqualFold(snap.FullName, a.Proposal.Repository) {
		return fmt.Errorf("%w: association or repository changed", ErrOwnership)
	}
	return access(snap)
}

// pending records that publishing stopped before the pull request, with the
// cause for the caller and only a fixed code on disk.
func (s *Service) pending(ctx context.Context, a Attempt, cause error) (Attempt, error) {
	next, err := s.update(ctx, a.ID, func(a *Attempt) bool {
		if a.PR.State == PRUnknown || a.PR.State == PRLinked {
			return s.setStep(a, StepPR, StepPending, codePRPending)
		}
		a.PR = PRLink{State: PRPending, Code: codePRPending}
		return s.setStep(a, StepPR, StepPending, codePRPending) || true
	})
	if err != nil {
		return s.partial(ctx, a.ID, err)
	}
	return next, cause
}

// pushHead pushes the exact head once, or skips the push when the remote already
// shows it. An uncertain push is resolved by reading the remote, never repeated
// blind.
func (s *Service) pushHead(ctx context.Context, a Attempt, head domain.PublishedHead) (Attempt, error) {
	if head.RemoteVerified && head.RemoteSHA == head.HeadSHA {
		return s.update(ctx, a.ID, func(a *Attempt) bool { return s.setStep(a, StepPush, StepDone, "") })
	}
	pushed, perr := s.d.Worktrees.PushIsolation(ctx, domain.IsolationPushRequest{Proposal: a.Proposal, HeadSHA: head.HeadSHA})
	if perr != nil {
		qctx, cancel := s.queryCtx(ctx)
		again, ierr := s.d.Worktrees.InspectPublishedHead(qctx, a.Proposal)
		cancel()
		if ierr == nil && again.RemoteVerified && again.RemoteSHA == head.HeadSHA && again.HeadSHA == head.HeadSHA && again.Meaningful {
			pushed, perr = again, nil
		}
	}
	if perr != nil {
		state, code, cause := StepFailed, codePushPending, fmt.Errorf("push: %w", isolationError(perr))
		if errors.Is(perr, domain.ErrUncertain) || isContext(perr) {
			state, code, cause = StepUnknown, codePushUnknown, fmt.Errorf("%w: push: %w", ErrOutcomeUnknown, perr)
		}
		next, err := s.update(ctx, a.ID, func(a *Attempt) bool { return s.setStep(a, StepPush, state, code) })
		if err != nil {
			return s.partial(ctx, a.ID, err)
		}
		return next, cause
	}
	if pushed.HeadSHA != head.HeadSHA || !pushed.RemoteVerified || pushed.RemoteSHA != head.HeadSHA || !pushed.Meaningful {
		next, err := s.update(ctx, a.ID, func(a *Attempt) bool { return s.setStep(a, StepPush, StepFailed, codePushPending) })
		if err != nil {
			return s.partial(ctx, a.ID, err)
		}
		return next, fmt.Errorf("%w: the head moved or the remote does not show it", ErrInputsChanged)
	}
	return s.update(ctx, a.ID, func(a *Attempt) bool { return s.setStep(a, StepPush, StepDone, "") })
}

// openOrLink finds the pull request for the pushed head, or opens one Draft.
// Discovery always precedes a create. An uncertain earlier create is only ever
// discovered, never repeated: an empty list right after it is not absence.
func (s *Service) openOrLink(ctx context.Context, a Attempt, head domain.PublishedHead, base string) (Attempt, error) {
	repo := a.Proposal.Repository
	list, err := s.d.PRs.PullRequests(ctx, repo, github.PRFilter{State: "all", Head: a.Proposal.Branch, Base: base})
	if err != nil {
		return s.pending(ctx, a, fmt.Errorf("%w: GitHub's pull request list: %w", ErrRemoteStale, err))
	}
	if a.PR.State == PRLinked {
		for _, pr := range list {
			if pr.Number == a.PR.Number && strings.EqualFold(pr.HeadSHA, head.HeadSHA) {
				return s.update(ctx, a.ID, func(a *Attempt) bool {
					changed := a.PR.HeadSHA != head.HeadSHA
					a.PR.HeadSHA = head.HeadSHA
					return s.setStep(a, StepPR, StepDone, "") || changed
				})
			}
		}
		return a, nil
	}
	m := classifyPRs(list, repo, a.Proposal.Branch, base, head.HeadSHA)
	switch {
	case m.one != nil:
		return s.link(ctx, a, *m.one)
	case m.ambiguous:
		next, err := s.update(ctx, a.ID, func(a *Attempt) bool {
			a.PR = PRLink{State: PRUnknown, Code: codePRUnknown}
			return s.setStep(a, StepPR, StepUnknown, codePRUnknown) || true
		})
		if err != nil {
			return s.partial(ctx, a.ID, err)
		}
		return next, fmt.Errorf("%w: several or unverifiable pull requests match this branch", ErrOwnership)
	case a.PR.State == PRUnknown:
		return a, fmt.Errorf("%w: an earlier pull request create is not visible yet", ErrOutcomeUnknown)
	}
	return s.create(ctx, a, head, base)
}

func (s *Service) link(ctx context.Context, a Attempt, pr github.PullRequest) (Attempt, error) {
	return s.update(ctx, a.ID, func(a *Attempt) bool {
		a.PR = PRLink{State: PRLinked, Number: pr.Number, URL: pr.URL, HeadSHA: pr.HeadSHA}
		s.setStep(a, StepPR, StepDone, "")
		return true
	})
}

func (s *Service) prBody(a Attempt) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Implements https://github.com/%s/issues/%d\n\n", a.Proposal.Repository, a.Number)
	fmt.Fprintf(&b, "Attempt: %s\n", a.ID)
	fmt.Fprintf(&b, "Specification: revision %s (comment %d), approval %d\n", a.Inputs.Spec.RevisionHash, a.Inputs.Spec.CommentID, a.Inputs.SpecApprovalID)
	fmt.Fprintf(&b, "Plan: revision %s (comment %d), decision %d, base %s\n\n", a.Inputs.Plan.RevisionHash, a.Inputs.Plan.CommentID, a.Inputs.Plan.DecisionID, a.Inputs.Plan.BaseCommit)
	b.WriteString("Awaiting human review; no automatic merge.\n")
	return b.String()
}

// create records Unknown before the POST, then opens the Draft. A definitive
// refusal leaves the next try free to discover first; anything ambiguous stays
// Unknown.
func (s *Service) create(ctx context.Context, a Attempt, head domain.PublishedHead, base string) (Attempt, error) {
	a, err := s.update(ctx, a.ID, func(a *Attempt) bool {
		a.PR = PRLink{State: PRUnknown, Code: codePRUnknown}
		s.setStep(a, StepPR, StepRunning, "")
		return true
	})
	if err != nil {
		return s.partial(ctx, a.ID, err)
	}
	repo := a.Proposal.Repository
	pr, cerr := s.d.PRs.CreatePullRequest(ctx, github.CreatePullRequestRequest{
		Repository: repo, Title: fmt.Sprintf("Implement #%d", a.Number), Body: s.prBody(a), Head: a.Proposal.Branch, Base: base, Draft: true,
	})
	if cerr != nil {
		if definitiveRefusal(cerr) {
			next, err := s.update(ctx, a.ID, func(a *Attempt) bool {
				a.PR = PRLink{State: PRUnavailable, Code: codePRUnavailable}
				return s.setStep(a, StepPR, StepPending, codePRUnavailable) || true
			})
			if err != nil {
				return s.partial(ctx, a.ID, err)
			}
			return next, fmt.Errorf("pull request: %w", cerr)
		}
		next, err := s.update(ctx, a.ID, func(a *Attempt) bool { return s.setStep(a, StepPR, StepUnknown, codePRUnknown) })
		if err != nil {
			return s.partial(ctx, a.ID, err)
		}
		return next, fmt.Errorf("%w: pull request create: %w", ErrOutcomeUnknown, cerr)
	}
	if pr.Number <= 0 || !linkable(pr.URL, repo, pr.Number) || pr.Head != a.Proposal.Branch || pr.Base != base || !pr.Draft ||
		(pr.HeadRepository != "" && !strings.EqualFold(pr.HeadRepository, repo)) || (pr.HeadSHA != "" && !strings.EqualFold(pr.HeadSHA, head.HeadSHA)) {
		next, err := s.update(ctx, a.ID, func(a *Attempt) bool { return s.setStep(a, StepPR, StepUnknown, codePRUnknown) })
		if err != nil {
			return s.partial(ctx, a.ID, err)
		}
		return next, fmt.Errorf("%w: GitHub's answer does not match the request", ErrOutcomeUnknown)
	}
	pr.HeadSHA = head.HeadSHA
	return s.link(ctx, a, pr)
}

// reconcilePR discovers, never creates: it links a pull request whose create may
// have landed. A failed or empty read leaves the state alone.
func (s *Service) reconcilePR(ctx context.Context, a Attempt) Attempt {
	if s.d.PRs == nil || a.PR.State != PRUnknown || a.State == StateCleaned {
		return a
	}
	base, ok := prBase(a.Proposal.BaseRef, a.Proposal.RemoteName)
	if !ok {
		return a
	}
	qctx, cancel := s.queryCtx(ctx)
	defer cancel()
	head, err := s.d.Worktrees.InspectPublishedHead(qctx, a.Proposal)
	if err != nil || head.HeadSHA == "" {
		return a
	}
	list, err := s.d.PRs.PullRequests(qctx, a.Proposal.Repository, github.PRFilter{State: "all", Head: a.Proposal.Branch, Base: base})
	if err != nil {
		return a
	}
	if m := classifyPRs(list, a.Proposal.Repository, a.Proposal.Branch, base, head.HeadSHA); m.one != nil {
		if next, err := s.link(ctx, a, *m.one); err == nil {
			return next
		}
	}
	return a
}
