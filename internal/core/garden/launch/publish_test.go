package launch

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	domain "github.com/Tiago-0liveira/bonsai/internal/git"
	"github.com/Tiago-0liveira/bonsai/internal/git/github"
)

type fakeLaunchPRs struct {
	mu                            sync.Mutex
	branches                      []github.RemoteBranch
	prs                           []github.PullRequest
	branchErr, listErr, createErr error
	createResult                  github.PullRequest
	mutateOnError                 bool
	calls                         []string
	filters                       []github.PRFilter
	creates                       []github.CreatePullRequestRequest
}

func (f *fakeLaunchPRs) Branches(_ context.Context, repo string) ([]github.RemoteBranch, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "branches:"+repo)
	return append([]github.RemoteBranch(nil), f.branches...), f.branchErr
}
func (f *fakeLaunchPRs) PullRequests(_ context.Context, repo string, filter github.PRFilter) ([]github.PullRequest, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "list:"+repo)
	f.filters = append(f.filters, filter)
	return append([]github.PullRequest(nil), f.prs...), f.listErr
}
func (f *fakeLaunchPRs) CreatePullRequest(_ context.Context, r github.CreatePullRequestRequest) (github.PullRequest, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, "create:"+r.Repository)
	f.creates = append(f.creates, r)
	if f.createErr == nil || f.mutateOnError {
		f.prs = append(f.prs, f.createResult)
	}
	return f.createResult, f.createErr
}
func fakeLaunchPublishRig(t *testing.T) (*fakeLaunchRig, Attempt, *fakeLaunchPRs) {
	t.Helper()
	r := newFakeLaunchRig(t)
	a := r.start(t)
	a = fakeLaunchFinish(t, r, a, "exited", fakeLaunchInt(0))
	sha := strings.Repeat("f", 40)
	r.worktrees.head = domain.PublishedHead{HeadSHA: sha, Meaningful: true}
	p := &fakeLaunchPRs{branches: []github.RemoteBranch{{Name: a.Proposal.Branch, RemoteHeadSHA: sha}}, createResult: github.PullRequest{Number: 42, URL: "https://github.com/owner/repo/pull/42", State: "open", Draft: true, Head: a.Proposal.Branch, Base: "main", HeadRepository: "owner/repo", HeadSHA: sha}}
	d := r.deps
	d.PRs = p
	var err error
	r.svc, err = New(d)
	if err != nil {
		t.Fatal(err)
	}
	return r, a, p
}
func TestLaunchPublishRequiresSeparateConsentAndClosedExecution(t *testing.T) {
	r, a, p := fakeLaunchPublishRig(t)
	if a.PR.State != PRNotCreated || len(p.creates) != 0 {
		t.Fatal("PR created by completion")
	}
	if _, err := r.svc.Publish(context.Background(), PublishRequest{AttemptID: a.ID}); !errors.Is(err, ErrNotAccepted) {
		t.Fatal(err)
	}
	for _, state := range []State{StateRunning, StateStarting, StateStartUnknown} {
		_, err := r.store.Update(context.Background(), a.ID, func(cur Attempt) (Attempt, bool, error) {
			cur.State = state
			cur.EndedAt = nil
			cur.Runs[0].State = "running"
			cur.Runs[0].EndedAt = nil
			return cur, true, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err = r.svc.Publish(context.Background(), PublishRequest{AttemptID: a.ID, Confirmed: true}); !errors.Is(err, ErrNotRetryable) {
			t.Fatalf("state %s err=%v", state, err)
		}
	}
	if len(p.creates) != 0 {
		t.Fatal("blocked publish created PR")
	}
	_, _, pushes := r.worktrees.counts()
	if pushes != 0 {
		t.Fatal("blocked publish pushed")
	}
}
func TestLaunchPublishMeaningfulCleanCommitGate(t *testing.T) {
	for _, tc := range []struct {
		name              string
		meaningful, dirty bool
		want              PRState
	}{{"no commits", false, false, PRNotCreated}, {"empty commit", false, false, PRNotCreated}, {"same tree", false, false, PRNotCreated}, {"dirty only", false, true, PRPending}, {"commits plus dirty", true, true, PRPending}} {
		t.Run(tc.name, func(t *testing.T) {
			r, a, p := fakeLaunchPublishRig(t)
			r.worktrees.head.Meaningful = tc.meaningful
			r.worktrees.head.Dirty = tc.dirty
			got, err := r.svc.Publish(context.Background(), PublishRequest{AttemptID: a.ID, Confirmed: true})
			if err != nil || got.PR.State != tc.want || got.State != StateAwaitingReview {
				t.Fatalf("publish=%+v err=%v", got, err)
			}
			_, _, n := r.worktrees.counts()
			if n != 0 || len(p.creates) != 0 {
				t.Fatal("no clean meaningful commit pushed/posted")
			}
		})
	}
}
func TestLaunchPublishVerifiesBothRemotesAndCreatesExactDraft(t *testing.T) {
	r, a, p := fakeLaunchPublishRig(t)
	got, err := r.svc.Publish(context.Background(), PublishRequest{AttemptID: a.ID, Confirmed: true})
	if err != nil || got.PR.State != PRLinked || got.PR.Number != 42 || got.State != StateAwaitingReview {
		t.Fatalf("publish=%+v err=%v", got, err)
	}
	if len(p.creates) != 1 || len(p.filters) == 0 {
		t.Fatalf("calls=%v", p.calls)
	}
	req := p.creates[0]
	if req.Repository != "owner/repo" || req.Head != a.Proposal.Branch || req.Base != "main" || !req.Draft || req.Title != "Implement #9" {
		t.Fatalf("request=%+v", req)
	}
	for _, want := range []string{"https://github.com/owner/repo/issues/9", a.ID, a.Inputs.Spec.RevisionHash, a.Inputs.Plan.RevisionHash, a.Inputs.Plan.BaseCommit, "Awaiting human review; no automatic merge."} {
		if !strings.Contains(req.Body, want) {
			t.Errorf("body lacks %q: %s", want, req.Body)
		}
	}
	filter := p.filters[0]
	if filter.State != "all" || filter.Head != a.Proposal.Branch || filter.Base != "main" {
		t.Fatalf("filter=%+v", filter)
	}
	for i, call := range p.calls {
		if strings.HasPrefix(call, "create:") && (i == 0 || !strings.HasPrefix(p.calls[i-1], "list:")) {
			t.Fatalf("POST without immediately preceding discovery: %v", p.calls)
		}
	}
	_, _, n := r.worktrees.counts()
	if n != 1 {
		t.Fatalf("pushes=%d", n)
	}
	if _, err = r.svc.Publish(context.Background(), PublishRequest{AttemptID: a.ID, Confirmed: true}); err != nil || len(p.creates) != 1 {
		t.Fatalf("linked replay posted again: %v", err)
	}
}
func TestLaunchPublishRequiresIndependentGitHubHeadProof(t *testing.T) {
	for _, mode := range []string{"stale GitHub SHA", "branch unavailable", "push unknown", "fork mismatch", "invalid base"} {
		t.Run(mode, func(t *testing.T) {
			r, a, p := fakeLaunchPublishRig(t)
			switch mode {
			case "stale GitHub SHA":
				p.branches[0].RemoteHeadSHA = strings.Repeat("0", 40)
			case "branch unavailable":
				p.branchErr = domain.ErrOffline
			case "push unknown":
				r.worktrees.pushErr = domain.ErrUncertain
			case "fork mismatch":
				r.worktrees.headErr = domain.E("ownership", "safe")
			case "invalid base":
				_, err := r.store.Update(context.Background(), a.ID, func(cur Attempt) (Attempt, bool, error) {
					cur.Proposal.BaseRef = cur.Proposal.BaseSHA
					return cur, true, nil
				})
				if err == nil {
					t.Fatal("immutable base mutation accepted")
				}
				return
			}
			got, err := r.svc.Publish(context.Background(), PublishRequest{AttemptID: a.ID, Confirmed: true})
			if got.ID == "" || got.State != StateAwaitingReview || got.PR.State == PRLinked || len(p.creates) != 0 {
				t.Fatalf("unsafe head publish=%+v err=%v creates=%d", got, err, len(p.creates))
			}
			if mode == "push unknown" && !errors.Is(err, ErrOutcomeUnknown) {
				t.Fatalf("unknown push=%v", err)
			}
		})
	}
}
func TestLaunchPublishExistingPRDiscoveryAndHistoricalStates(t *testing.T) {
	for _, state := range []string{"open", "closed", "merged"} {
		for _, draft := range []bool{true, false} {
			t.Run(state+"/"+map[bool]string{true: "draft", false: "ready"}[draft], func(t *testing.T) {
				r, a, p := fakeLaunchPublishRig(t)
				existing := p.createResult
				existing.State = state
				existing.Draft = draft
				p.prs = []github.PullRequest{existing}
				got, err := r.svc.Publish(context.Background(), PublishRequest{AttemptID: a.ID, Confirmed: true})
				if err != nil || got.PR.Number != existing.Number || len(p.creates) != 0 {
					t.Fatalf("discovery=%+v err=%v creates=%d", got, err, len(p.creates))
				}
			})
		}
	}
}
func TestLaunchPublishAmbiguousPOSTDiscoversWithoutDuplicate(t *testing.T) {
	for _, visible := range []bool{true, false} {
		t.Run(map[bool]string{true: "visible", false: "not yet visible"}[visible], func(t *testing.T) {
			r, a, p := fakeLaunchPublishRig(t)
			p.createErr = context.DeadlineExceeded
			p.mutateOnError = visible
			got, err := r.svc.Publish(context.Background(), PublishRequest{AttemptID: a.ID, Confirmed: true})
			if !errors.Is(err, ErrOutcomeUnknown) || got.PR.State != PRUnknown || got.ID == "" {
				t.Fatalf("unknown POST=%+v err=%v", got, err)
			}
			p.createErr = nil
			got, err = r.svc.Publish(context.Background(), PublishRequest{AttemptID: a.ID, Confirmed: true})
			if len(p.creates) != 1 {
				t.Fatal("uncertain POST repeated")
			}
			if visible {
				if err != nil || got.PR.State != PRLinked {
					t.Fatalf("discovery=%+v %v", got, err)
				}
			} else if got.PR.State != PRUnknown {
				t.Fatalf("empty list erased uncertain intent: %+v %v", got, err)
			}
		})
	}
}
func TestLaunchPublishAmbiguousCandidatesNeverAuthorizeCreate(t *testing.T) {
	for _, mode := range []string{"multiple", "wrong repository", "missing repository", "wrong SHA", "incomplete list"} {
		t.Run(mode, func(t *testing.T) {
			r, a, p := fakeLaunchPublishRig(t)
			candidate := p.createResult
			switch mode {
			case "multiple":
				p.prs = []github.PullRequest{candidate, candidate}
			case "wrong repository":
				candidate.HeadRepository = "fork/repo"
				p.prs = []github.PullRequest{candidate}
			case "missing repository":
				candidate.HeadRepository = ""
				p.prs = []github.PullRequest{candidate}
			case "wrong SHA":
				candidate.HeadSHA = strings.Repeat("0", 40)
				p.prs = []github.PullRequest{candidate}
			case "incomplete list":
				p.listErr = garden.ErrOffline
			}
			got, _ := r.svc.Publish(context.Background(), PublishRequest{AttemptID: a.ID, Confirmed: true})
			if got.ID == "" || got.PR.State == PRLinked || len(p.creates) != 0 {
				t.Fatalf("ambiguous candidate=%+v", got)
			}
		})
	}
}
func TestLaunchPublishDefinitiveRejectionCanDiscoverBeforeRetry(t *testing.T) {
	r, a, p := fakeLaunchPublishRig(t)
	p.createErr = domain.ErrForbidden
	got, err := r.svc.Publish(context.Background(), PublishRequest{AttemptID: a.ID, Confirmed: true})
	if !errors.Is(err, domain.ErrForbidden) || got.ID == "" || got.PR.State == PRUnknown {
		t.Fatalf("definitive rejection=%+v %v", got, err)
	}
	p.createErr = nil
	p.prs = []github.PullRequest{p.createResult}
	got, err = r.svc.Publish(context.Background(), PublishRequest{AttemptID: a.ID, Confirmed: true})
	if err != nil || got.PR.State != PRLinked || len(p.creates) != 1 {
		t.Fatalf("retry discovery=%+v %v", got, err)
	}
}
func TestLaunchPublishMissingCapabilityIsOrthogonal(t *testing.T) {
	r, a, _ := fakeLaunchPublishRig(t)
	d := r.deps
	d.PRs = nil
	var err error
	r.svc, err = New(d)
	if err != nil {
		t.Fatal(err)
	}
	got, err := r.svc.Publish(context.Background(), PublishRequest{AttemptID: a.ID, Confirmed: true})
	if err != nil || got.PR.State != PRUnavailable || got.State != a.State || got.SessionID != a.SessionID {
		t.Fatalf("missing PR adapter=%+v %v", got, err)
	}
}

func TestLaunchPublishChangingHeadDuringPushCannotCreatePR(t *testing.T) {
	r, a, p := fakeLaunchPublishRig(t)
	original := r.worktrees.head.HeadSHA
	r.worktrees.pushResult = &domain.PublishedHead{HeadSHA: strings.Repeat("0", 40), RemoteSHA: original, RemoteVerified: true, Meaningful: true}
	got, err := r.svc.Publish(context.Background(), PublishRequest{AttemptID: a.ID, Confirmed: true})
	if got.ID == "" || got.PR.State == PRLinked || len(p.creates) != 0 {
		t.Fatalf("moving push head authorized PR: %+v %v", got, err)
	}
}
func TestLaunchPublishTimedOutPushUsesObservedExactRemoteHead(t *testing.T) {
	r, a, p := fakeLaunchPublishRig(t)
	r.worktrees.pushErr = domain.ErrUncertain
	r.worktrees.pushAfterEffect = true
	got, err := r.svc.Publish(context.Background(), PublishRequest{AttemptID: a.ID, Confirmed: true})
	if err != nil && !errors.Is(err, ErrOutcomeUnknown) {
		t.Fatalf("ambiguous push=%+v %v", got, err)
	}
	r.worktrees.pushErr = nil
	got, err = r.svc.Publish(context.Background(), PublishRequest{AttemptID: a.ID, Confirmed: true})
	if err != nil || got.PR.State != PRLinked || len(p.creates) != 1 {
		t.Fatalf("observed push recovery=%+v %v", got, err)
	}
	_, _, pushes := r.worktrees.counts()
	if pushes != 1 {
		t.Fatalf("blind retry repeated successful push %d times", pushes)
	}
}
func TestLaunchPublishLinkedNewHeadKeepsSamePR(t *testing.T) {
	r, a, p := fakeLaunchPublishRig(t)
	first, err := r.svc.Publish(context.Background(), PublishRequest{AttemptID: a.ID, Confirmed: true})
	if err != nil {
		t.Fatal(err)
	}
	next := strings.Repeat("1", 40)
	r.worktrees.head = domain.PublishedHead{HeadSHA: next, Meaningful: true}
	p.branches[0].RemoteHeadSHA = next
	p.prs[0].HeadSHA = next
	again, err := r.svc.Publish(context.Background(), PublishRequest{AttemptID: a.ID, Confirmed: true})
	if err != nil || again.PR.Number != first.PR.Number || len(p.creates) != 1 || again.State != StateAwaitingReview {
		t.Fatalf("new head replaced existing PR: %+v %v", again, err)
	}
}
func TestLaunchPublishReadOutageKeepsUnknownIntentAndLocalHistory(t *testing.T) {
	r, a, p := fakeLaunchPublishRig(t)
	p.createErr = context.DeadlineExceeded
	_, _ = r.svc.Publish(context.Background(), PublishRequest{AttemptID: a.ID, Confirmed: true})
	p.listErr = garden.ErrOffline
	all, err := r.svc.Reconcile(context.Background(), a.Issue)
	if err != nil || len(all) != 1 || all[0].PR.State != PRUnknown || all[0].State != StateAwaitingReview || len(p.creates) != 1 {
		t.Fatalf("read outage erased local/unknown state: %+v %v", all, err)
	}
}
