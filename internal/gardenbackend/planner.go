package gardenbackend

import (
	"context"
	"fmt"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/approve"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/connect"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/plan"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/planrev"
	coregit "github.com/Tiago-0liveira/bonsai/internal/core/git"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
)

// startTimeout bounds creating a planner session: the holder has to launch and the provider binary to start.
const startTimeout = 60 * time.Second

// unavailablePlanner is shown when this build or host cannot run planners at all.
var unavailablePlanner = &gardenapi.Error{Code: gardenapi.CodeFailed, Message: "Requesting a plan is unavailable here.",
	Next: "Planner sessions need the agent runtime and terminal sessions, which this host or build does not have."}

// planService builds the planner service on first use. It needs gate one, the attempt store, the checkout
// inspector and the agent runtime; without any of them there is no planner role and nothing falls back.
func (b *Backend) planService() (*plan.Service, error) {
	gate, err := b.approveService()
	if err != nil {
		return nil, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.planner != nil {
		return b.planner, nil
	}
	if b.deps.Plans == nil || b.deps.Checkout == nil || b.deps.Agents == nil {
		return nil, unavailablePlanner
	}
	svc, err := plan.New(plan.Deps{Feedback: func(ctx context.Context, t approve.Target) (string, error) {
		log, err := gate.Read(ctx, t)
		if err != nil {
			return "", err
		}
		rv := planrev.Derive(log, t.Viewer)
		if rv.Current != nil && rv.Current.State == planrev.StateChangesRequested {
			return rv.Current.Reason(), nil
		}
		return "", nil
	}, Gate: gate, Repo: b.deps.Checkout, Agents: b.deps.Agents, Store: b.deps.Plans, Local: b.local, Dir: b.dir, NewID: b.deps.NewID})
	if err != nil {
		return nil, err
	}
	b.planner = svc
	return svc, nil
}

// plannerTarget resolves the Issue and the authenticated viewer. Planning is local, so Read access is enough.
func (b *Backend) plannerTarget(ctx context.Context, issue uint64) (approve.Target, garden.RepoID, error) {
	t, id, err := b.target(ctx, false)
	if err != nil {
		return approve.Target{}, 0, err
	}
	if issue == 0 {
		return approve.Target{}, 0, apiError(approve.ErrWrongIssue)
	}
	return approve.Target{Repo: t.Repository.ID, Issue: garden.IssueID(issue), Viewer: id.ID, Login: id.Login, ReadOnly: t.Repository.Access < connect.AccessWrite}, t.Repository.ID, nil
}

// PreviewPlanner implements gardenapi.Backend.
func (b *Backend) PreviewPlanner(ctx context.Context, issue uint64) (gardenapi.PlannerPreviewView, error) {
	gen := b.gen.Add(1)
	svc, err := b.planService()
	if err != nil {
		return gardenapi.PlannerPreviewView{}, apiError(err)
	}
	ctx, cancel := context.WithTimeout(ctx, editTimeout)
	defer cancel()
	t, repo, err := b.plannerTarget(ctx, issue)
	if err != nil {
		return gardenapi.PlannerPreviewView{}, err
	}
	p, err := svc.Preview(ctx, t)
	if err != nil {
		return gardenapi.PlannerPreviewView{}, apiError(err)
	}
	return gardenapi.PlannerPreviewView{Stamp: b.stamp(gen, repo), IssueID: issue, Number: p.Number, Title: p.Title, Actor: t.Login,
		Gate: p.Gate, Spec: p.Spec(), Base: p.Base, Attempts: p.Attempts, Overlay: liveViews(p.Observed), Unavailable: p.Unavailable}, nil
}

// StartPlanner implements gardenapi.Backend.
func (b *Backend) StartPlanner(ctx context.Context, req gardenapi.PlannerStartRequest) (gardenapi.PlannerAttemptView, error) {
	gen := b.gen.Add(1)
	svc, err := b.planService()
	if err != nil {
		return gardenapi.PlannerAttemptView{}, apiError(err)
	}
	p := req.Preview
	if p.Stamp.LocalRepoID != b.local.String() || p.IssueID == 0 || p.Spec.RevisionHash == "" {
		return gardenapi.PlannerAttemptView{}, apiError(plan.ErrNotApproved)
	}
	ctx, cancel := context.WithTimeout(ctx, startTimeout)
	defer cancel()
	t, repo, err := b.plannerTarget(ctx, p.IssueID)
	if err != nil {
		return gardenapi.PlannerAttemptView{}, err
	}
	if uint64(repo) != p.Stamp.GardenID {
		return gardenapi.PlannerAttemptView{}, apiError(ErrRemoteChanged)
	}
	a, err := svc.Start(ctx, plan.StartRequest{Target: t, Spec: p.Spec, Base: p.Base, IncludeDirty: req.IncludeDirty, AcceptReadOnly: req.AcceptReadOnly,
		AccountID: req.AccountID, Model: req.Model, Effort: req.Effort, Name: req.Name, Cols: req.Cols, Rows: req.Rows})
	if err != nil {
		return gardenapi.PlannerAttemptView{}, apiError(err)
	}
	return gardenapi.PlannerAttemptView{Stamp: b.stamp(gen, repo), IssueID: p.IssueID, Attempt: a}, nil
}

// PlannerAttempts implements gardenapi.Backend.
func (b *Backend) PlannerAttempts(ctx context.Context, issue uint64) (gardenapi.PlannerAttemptsView, error) {
	gen := b.gen.Add(1)
	svc, err := b.planService()
	if err != nil {
		return gardenapi.PlannerAttemptsView{}, apiError(err)
	}
	ctx, cancel := context.WithTimeout(ctx, editTimeout)
	defer cancel()
	t, repo, err := b.plannerTarget(ctx, issue)
	if err != nil {
		return gardenapi.PlannerAttemptsView{}, err
	}
	obs, err := svc.Observations(ctx, t.Issue)
	if err != nil {
		return gardenapi.PlannerAttemptsView{}, apiError(err)
	}
	list := make([]plan.Attempt, len(obs))
	for i, o := range obs {
		list[i] = o.Attempt
	}
	return gardenapi.PlannerAttemptsView{Stamp: b.stamp(gen, repo), IssueID: issue, Attempts: list, Overlay: liveViews(obs)}, nil
}

// CancelPlanner implements gardenapi.Backend.
func (b *Backend) CancelPlanner(ctx context.Context, attemptID string) (gardenapi.PlannerAttemptView, error) {
	gen := b.gen.Add(1)
	svc, err := b.planService()
	if err != nil {
		return gardenapi.PlannerAttemptView{}, apiError(err)
	}
	ctx, cancel := context.WithTimeout(ctx, editTimeout)
	defer cancel()
	a, err := svc.Cancel(ctx, attemptID)
	if err != nil {
		return gardenapi.PlannerAttemptView{}, apiError(err)
	}
	return gardenapi.PlannerAttemptView{Stamp: b.stamp(gen, a.Repo), IssueID: uint64(a.Issue), Attempt: a, Overlay: gardenapi.PlannerLiveView{PlanFile: svc.InspectPlanFile(a, nil)}}, nil
}

// liveViews keys the live overlay of observed attempts by attempt ID.
func liveViews(obs []plan.Observation) map[string]gardenapi.PlannerLiveView {
	if len(obs) == 0 {
		return nil
	}
	out := make(map[string]gardenapi.PlannerLiveView, len(obs))
	for _, o := range obs {
		out[o.Attempt.ID] = gardenapi.PlannerLiveView{Activity: o.Activity, PlanFile: o.File}
	}
	return out
}

// PlannerPlan implements gardenapi.Backend.
func (b *Backend) PlannerPlan(ctx context.Context, attemptID string) (gardenapi.PlannerPlanView, error) {
	gen := b.gen.Add(1)
	svc, err := b.planService()
	if err != nil {
		return gardenapi.PlannerPlanView{}, apiError(err)
	}
	ctx, cancel := context.WithTimeout(ctx, editTimeout)
	defer cancel()
	text, info, err := svc.Plan(ctx, attemptID)
	if err != nil {
		return gardenapi.PlannerPlanView{}, apiError(err)
	}
	// Plan already proved the attempt belongs to this clone; the store read below only supplies its Issue.
	a, err := b.deps.Plans.Get(ctx, attemptID)
	if err != nil {
		return gardenapi.PlannerPlanView{}, apiError(err)
	}
	return gardenapi.PlannerPlanView{Stamp: b.stamp(gen, a.Repo), IssueID: uint64(a.Issue), AttemptID: a.ID, Text: text, File: info}, nil
}

// LocalWork implements gardenapi.Backend.
func (b *Backend) LocalWork(ctx context.Context) (gardenapi.LocalWorkView, error) {
	gen := b.gen.Add(1)
	svc, err := b.planService()
	if err != nil {
		return gardenapi.LocalWorkView{}, apiError(err)
	}
	ctx, cancel := context.WithTimeout(ctx, editTimeout)
	defer cancel()
	// The stamp names the repository even when nothing is flagged, so an empty answer can clear old badges.
	t, _, err := b.target(ctx, false)
	if err != nil {
		return gardenapi.LocalWorkView{}, err
	}
	obs, err := svc.Overview(ctx)
	if err != nil {
		return gardenapi.LocalWorkView{}, apiError(err)
	}
	view := gardenapi.LocalWorkView{Issues: map[uint64]gardenapi.IssueLocalWork{}}
	// Newest request first, so the first qualifying attempt of an Issue is the one to show; a live one always wins.
	for _, o := range obs {
		a := o.Attempt
		live := a.State.Live()
		unpublished := a.State == plan.StateEnded && o.File.State == plan.PlanFileReady && a.PlanPublishedAt == nil
		if !live && !unpublished {
			continue
		}
		cur, seen := view.Issues[uint64(a.Issue)]
		if seen && (cur.Planner.Live || !live) {
			continue
		}
		view.Issues[uint64(a.Issue)] = gardenapi.IssueLocalWork{Planner: &gardenapi.LocalRunSummary{AttemptID: a.ID, State: string(a.State), Live: live,
			Activity: o.Activity, PlanFile: o.File.State, Updated: a.UpdatedAt}}
	}
	view.Stamp = b.stamp(gen, t.Repository.ID)
	return view, nil
}

// checkoutInspector adapts the Git checkout reader to the planner seam.
type checkoutInspector struct{ in coregit.CheckoutInspector }

func (c checkoutInspector) Inspect(ctx context.Context) (plan.Base, error) {
	co, err := c.in.Inspect(ctx)
	if err != nil {
		return plan.Base{}, err
	}
	return plan.Base{Commit: co.Commit, Branch: co.Branch, Dirty: co.Dirty(), DirtyFiles: co.DirtyFiles, DirtyDigest: co.DirtyDigest}, nil
}

// plannerAgents adapts the TUI's agents contract to the planner seam, so a planner session is an ordinary
// holder-backed session the same terminal controller attaches to. The Garden backend decides nothing about
// providers: the account's read-only verdict and the launch refusal both come from the agent runtime.
type plannerAgents struct {
	api agentsapi.Backend
	dir string
}

func (p plannerAgents) Account(ctx context.Context, id agents.AccountID) (plan.Account, error) {
	if p.api.Accounts == nil {
		return plan.Account{}, unavailablePlanner
	}
	list, err := p.api.Accounts.List(ctx)
	if err != nil {
		return plan.Account{}, err
	}
	for _, a := range list {
		if a.ID == id {
			return plan.Account{ID: a.ID, Name: a.Name, Provider: a.Provider, ReadOnly: a.ReadOnly}, nil
		}
	}
	return plan.Account{}, agents.ErrAccountNotFound
}

func (p plannerAgents) Start(ctx context.Context, s plan.StartSession) (string, error) {
	if p.api.Sessions == nil || !p.api.Sessions.Supported() {
		return "", unavailablePlanner
	}
	// No WorktreeID: the planner belongs to no worktree, and none is created.
	sess, err := p.api.Sessions.Start(ctx, agentsapi.StartRequest{ProjectID: config.ProjectID(p.dir), WorkDir: s.Dir, AccountID: s.AccountID, Name: s.Name, Cols: s.Cols, Rows: s.Rows, Launch: s.Launch})
	if err != nil {
		return "", err
	}
	return sess.ID, nil
}

func (p plannerAgents) Stop(ctx context.Context, id string) error {
	if p.api.Sessions == nil {
		return unavailablePlanner
	}
	return p.api.Sessions.Stop(ctx, id)
}

func (p plannerAgents) Status(ctx context.Context, id string) (plan.SessionStatus, error) {
	if p.api.Sessions == nil || !p.api.Sessions.Supported() {
		return plan.SessionStatus{}, unavailablePlanner
	}
	list, err := p.api.Sessions.List(ctx)
	if err != nil {
		return plan.SessionStatus{}, fmt.Errorf("list agent sessions: %w", err)
	}
	for _, s := range list {
		if s.ID == id {
			return sessionStatus(s), nil
		}
	}
	return plan.SessionStatus{}, plan.ErrSessionGone
}

func (p plannerAgents) Statuses(ctx context.Context) (map[string]plan.SessionStatus, error) {
	if p.api.Sessions == nil || !p.api.Sessions.Supported() {
		return nil, unavailablePlanner
	}
	list, err := p.api.Sessions.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("list agent sessions: %w", err)
	}
	out := make(map[string]plan.SessionStatus, len(list))
	for _, s := range list {
		out[s.ID] = sessionStatus(s)
	}
	return out, nil
}

// sessionStatus copies the activity so the overlay never aliases the session summary.
func sessionStatus(s agentsapi.Session) plan.SessionStatus {
	st := plan.SessionStatus{State: s.State, ExitCode: s.ExitCode}
	if s.Activity != nil {
		a := *s.Activity
		st.Activity = &a
	}
	return st
}

// WithAgents lets Garden start planner sessions through the agent runtime the TUI already uses. Without it the
// planner role is unavailable.
func WithAgents(api agentsapi.Backend) Option { return func(o *options) { o.agents = &api } }

var _ plan.Agents = plannerAgents{}
