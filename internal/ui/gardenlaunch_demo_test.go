package ui

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/agentterminal"
	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/approve"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/launch"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/reviewpass"
	domain "github.com/Tiago-0liveira/bonsai/internal/git"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
	agentfake "github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi/fake"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
)

// The demo uses the real launch state machine and durable store. Only the
// machine boundaries (GitHub, worktrees and the holder) are in-memory fakes.
type launchDemoMachine struct {
	snapshot      launch.RemoteSnapshot
	proposal      domain.IsolationProposal
	resource      domain.IsolationCheck
	sessions      *agentfake.Sessions
	observation   launch.SessionObservation
	starts, stops int
	now           time.Time
}

func (f *launchDemoMachine) Now() time.Time                          { return f.now }
func (f *launchDemoMachine) NewID() string                           { return "demo-launch" }
func (f *launchDemoMachine) NewSessionID() (agents.SessionID, error) { return "sess_ded0", nil }
func (f *launchDemoMachine) Verify(context.Context, approve.Target) (launch.RemoteSnapshot, error) {
	return f.snapshot, nil
}
func (f *launchDemoMachine) Mirror(context.Context, launch.MirrorRequest) error { return nil }
func (f *launchDemoMachine) PreviewIsolation(_ context.Context, r domain.IsolationRequest) (domain.IsolationProposal, error) {
	return domain.IsolationProposal{IsolationRequest: r, Path: f.proposal.Path, RootRevision: 1, ConfigHash: strings.Repeat("d", 64)}, nil
}
func (f *launchDemoMachine) CreateIsolation(_ context.Context, r domain.IsolationCreateRequest) (domain.IsolationCheck, error) {
	f.resource = domain.IsolationCheck{Proposal: r.Proposal, Worktree: domain.Worktree{ID: "demo-worktree", RepositoryID: "local", Path: r.Proposal.Path, Branch: r.Proposal.Branch, HeadSHA: r.Proposal.BaseSHA}, Exists: true, Owned: true, Clean: true, Hooks: "done", HeadSHA: r.Proposal.BaseSHA}
	return f.resource, nil
}
func (f *launchDemoMachine) InspectIsolation(context.Context, domain.IsolationProposal) (domain.IsolationCheck, error) {
	return f.resource, nil
}
func (*launchDemoMachine) RemoveIsolation(context.Context, domain.IsolationRemoveRequest) error {
	return errors.New("demo never cleans")
}
func (*launchDemoMachine) InspectPublishedHead(context.Context, domain.IsolationProposal) (domain.PublishedHead, error) {
	return domain.PublishedHead{}, errors.New("demo never publishes")
}
func (*launchDemoMachine) PushIsolation(context.Context, domain.IsolationPushRequest) (domain.PublishedHead, error) {
	return domain.PublishedHead{}, errors.New("demo never publishes")
}
func (f *launchDemoMachine) Start(_ context.Context, r launch.StartSession) (launch.SessionObservation, error) {
	f.starts++
	started := f.now
	f.observation = launch.SessionObservation{Identity: r.Identity, State: "running", Alive: true, Acknowledged: true, StartedAt: &started}
	task := r.Task
	f.sessions.Add(agentsapi.Session{Summary: agentterminal.Summary{ID: r.Identity.SessionID, ProjectID: r.ProjectID, WorktreeID: r.WorktreeID, Provider: "claude", AccountID: r.AccountID, Name: r.Name, State: "running", OperationID: r.Identity.OperationID, Task: &task}})
	return f.observation, nil
}
func (f *launchDemoMachine) Stop(_ context.Context, id launch.StartIdentity) error {
	if id != f.observation.Identity {
		return errors.New("wrong holder")
	}
	f.stops++
	f.observation.State = "stopping"
	return nil
}
func (f *launchDemoMachine) Query(_ context.Context, id launch.StartIdentity) (launch.SessionObservation, error) {
	if id != f.observation.Identity {
		return launch.SessionObservation{}, errors.New("wrong holder")
	}
	return f.observation, nil
}

// This adapter replaces only the local launch path of the existing Garden fake.
// The UI still talks solely to gardenapi.LaunchBackend and its session backend.
type launchDemoBackend struct {
	*fakeLaunchUIBackend
	service *launch.Service
	machine *launchDemoMachine
	passes  reviewpass.Store
}

func (b *launchDemoBackend) PreviewLaunch(ctx context.Context, req gardenapi.LaunchPreviewRequest) (gardenapi.LaunchPreviewView, error) {
	view, err := b.fakeLaunchUIBackend.PreviewLaunch(ctx, req)
	if err != nil {
		return view, err
	}
	b.machine.snapshot, b.machine.proposal = view.Preview.Remote, view.Preview.Proposal
	pv, err := b.service.Preview(ctx, view.Preview.Request)
	view.Preview = pv
	b.lastPreview = view
	return view, err
}
func (b *launchDemoBackend) StartLaunch(ctx context.Context, req gardenapi.LaunchStartRequest) (gardenapi.LaunchAttemptView, error) {
	b.calls = append(b.calls, "StartLaunch")
	b.starts = append(b.starts, req)
	p, review := req.Preview.Preview, req.Preview.Review
	snapshot := reviewpass.Snapshot{
		Local: p.Remote.Local, Repo: p.Request.Target.Repo, Issue: p.Request.Target.Issue, Number: p.Remote.Issue.Number,
		Inputs:    reviewpass.Inputs{SpecRevision: p.Inputs.Spec.RevisionHash, SpecFingerprint: p.Inputs.Spec.Fingerprint, SpecApprovalID: p.Inputs.SpecApprovalID, PlanRevision: p.Inputs.Plan.RevisionHash, PlanDecisionID: p.Inputs.Plan.DecisionID},
		AttemptID: p.ID, Worktree: reviewpass.Worktree{Path: p.Proposal.Path, Branch: p.Proposal.Branch, BaseSHA: p.Proposal.BaseSHA}, Policy: review.Policy, PolicyHash: review.Hash,
	}
	pass, err := reviewpass.NewPass("rp-00000000000000000000000000000001", snapshot, reviewpass.Authorization{ActorID: p.Request.Target.Viewer, PreviewID: p.ID, ConfirmedAt: b.machine.now}, b.machine.now)
	if err != nil {
		return gardenapi.LaunchAttemptView{}, err
	}
	pass, _, err = b.passes.Reserve(ctx, pass)
	if err != nil {
		return gardenapi.LaunchAttemptView{}, err
	}
	a, err := b.service.Start(ctx, launch.StartRequest{Preview: req.Preview.Preview, Confirmed: req.Confirmed, EscalationConfirmed: req.EscalationConfirmed})
	return gardenapi.LaunchAttemptView{Stamp: req.Preview.Stamp, IssueID: req.Preview.IssueID, Attempt: a, Review: &pass}, err
}
func (b *launchDemoBackend) LaunchAttempts(ctx context.Context, issue uint64) (gardenapi.LaunchAttemptsView, error) {
	b.calls = append(b.calls, "LaunchAttempts")
	b.attemptsReads++
	st, err := b.Backend.Status(ctx)
	if err != nil {
		return gardenapi.LaunchAttemptsView{}, err
	}
	attempts, observed, err := b.service.ReconcileObserved(ctx, garden.IssueID(issue))
	activity := map[string]gardenapi.LaunchActivity{}
	for _, obs := range observed {
		activity[obs.AttemptID] = gardenapi.LaunchActivity{State: obs.Session.State, Activity: obs.Session.Activity, Alive: obs.Session.Alive, ObservedAt: obs.ObservedAt}
	}
	reviews := map[string]reviewpass.Pass{}
	passes, readErr := b.passes.List(ctx, garden.LocalRepoID(st.Stamp.LocalRepoID), garden.IssueID(issue))
	for _, pass := range passes {
		reviews[pass.Snapshot.AttemptID] = pass
	}
	return gardenapi.LaunchAttemptsView{Stamp: st.Stamp, IssueID: issue, Attempts: attempts, Activity: activity, Reviews: reviews, ReviewsErr: readErr != nil}, err
}
func (b *launchDemoBackend) StopLaunch(ctx context.Context, id string) (gardenapi.LaunchAttemptView, error) {
	b.calls = append(b.calls, "StopLaunch")
	b.stops++
	a, err := b.service.Stop(ctx, id)
	return gardenapi.LaunchAttemptView{Stamp: b.lastPreview.Stamp, IssueID: uint64(a.Issue), Attempt: a}, err
}

func TestGardenLaunchFakeHolderEndToEnd(t *testing.T) {
	m, rig, fb := fakeLaunchUIEnv(t, 120, 35)
	machine := &launchDemoMachine{sessions: rig.agents.Sessions, now: time.Now().UTC()}
	st, err := fb.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	service, err := launch.New(launch.Deps{Remote: machine, Accounts: m.agents.api.Accounts.(*fakeLaunchUIAccounts), Worktrees: machine, Holder: machine, Sessions: machine, Store: &launch.FileStore{Path: filepath.Join(t.TempDir(), "launch.json")}, Clock: machine, IDs: machine, Local: garden.LocalRepoID(st.Stamp.LocalRepoID)})
	if err != nil {
		t.Fatal(err)
	}
	backend := &launchDemoBackend{fakeLaunchUIBackend: fb, machine: machine, service: service, passes: &reviewpass.FileStore{Path: filepath.Join(t.TempDir(), "reviews.json")}}
	fb.review = launchTestReview(false)
	m.work.GardenAutoReview = &config.GardenAutoReviewPrefs{Enabled: true, Account: "review"}
	acc := m.agents.api.Accounts.(*fakeLaunchUIAccounts)
	reviewer := fb.resolution
	reviewer.AccountID, reviewer.AccountName = "review", "review"
	acc.resolutions = map[agents.AccountID]agents.LaunchResolution{"c1": fb.resolution, "review": reviewer}
	rig.agents.Accounts.Items = append(rig.agents.Accounts.Items, agentsapi.Account{Account: agents.Account{ID: "review", Name: "review", Provider: "claude"}})
	m.garden.api = backend
	m = plannerPress(t, m, "enter")
	mustContain(t, "Ready", view(m), "READY TO BUILD", "Claude / review", "One review + fixes + checks")
	m = plannerPress(t, m, "s")
	mustContain(t, "authorized sequence", m.launchSequence(*m.garden.launch.preview), "Confirm authorizes: implementation by", "then one review by Claude / review", "with fixes and checks", "then a stop for your review.")
	mustContain(t, "confirmation screen", view(m), "Confirm authorizes:", "one review by Claude / review", "stop for your review.")
	if machine.starts != 0 {
		t.Fatal("preview started holder")
	}
	m = plannerPress(t, m, "tab", "enter")
	if machine.starts != 1 {
		t.Fatalf("real service did not start holder: %s", view(m))
	}
	frozen, err := backend.passes.ForAttempt(context.Background(), backend.lastPreview.Preview.ID)
	if err != nil || len(frozen) != 1 || frozen[0].Snapshot.PolicyHash != fb.review.Hash || frozen[0].State != reviewpass.StatePending {
		t.Fatalf("review was not frozen: %+v %v", frozen, err)
	}
	machine.now = machine.now.Add(4 * time.Second)
	machine.observation.Activity = "idle"
	m = plannerPress(t, m, "R")
	mustContain(t, "idle holder", view(m), "IMPLEMENTING THE APPROVED PLAN", "idle is not completion", "Next: one Claude review, fixes, checks, then your review.")
	m.work.GardenAutoReview.Enabled = false
	fb.review = gardenapi.LaunchReview{}
	m = plannerPress(t, m, "R")
	mustContain(t, "frozen sequence after policy edit", view(m), "Next: one Claude review, fixes, checks, then your review.")
	after, err := backend.passes.ForAttempt(context.Background(), backend.lastPreview.Preview.ID)
	if err != nil || !reflect.DeepEqual(frozen, after) {
		t.Fatalf("policy edit changed frozen pass: %+v %v", after, err)
	}
	t.Log("running 120x35:\n" + view(m))
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	mustFit(t, "running compact", m)
	mustContain(t, "running compact", view(m), "Next: one Claude review, fixes, checks, then your review.")
	t.Log("running 80x24:\n" + view(m))
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 120, Height: 35})
	m, observer := fakeLaunchUIOpenTerminal(t, m, rig)
	if observer.Writer() {
		t.Fatal("opening session took control")
	}
	m = plannerPress(t, m, "enter")
	streams := rig.agents.Sessions.Streams(machine.observation.Identity.SessionID)
	writer := streams[len(streams)-1]
	if !writer.Writer() {
		t.Fatal("Enter did not take control")
	}
	m = plannerPress(t, m, "ctrl+]")
	streams = rig.agents.Sessions.Streams(machine.observation.Identity.SessionID)
	if !writer.Closed() || streams[len(streams)-1].Writer() {
		t.Fatal("Ctrl+] retained writer")
	}
	m = plannerPress(t, m, "esc", "x")
	mustContain(t, "stop confirmation", view(m), "Cancel", "Confirm: stop attempt")
	if machine.stops != 0 {
		t.Fatal("opening stop confirmation stopped holder")
	}
	m = plannerPress(t, m, "tab", "enter")
	mustContain(t, "stopping", view(m), "Stopping… waiting for the holder to close")
	machine.now = machine.now.Add(time.Second)
	machine.observation.Alive, machine.observation.Closed = false, true
	machine.observation.State, machine.observation.ExitCode, machine.observation.EndedAt = "exited", fakeLaunchUIExitCode(0), &machine.now
	m = launchTestPoll(t, m)
	mustContain(t, "stopped", view(m), "Stopped", "Next: retry (r) or clean up the worktree (w) from the attempt.")
	t.Log("stopped 120x35:\n" + view(m))
	if machine.starts != 1 || machine.stops != 1 || fb.cleanups != 0 || fb.publishes != 0 {
		t.Fatalf("unexpected effects starts=%d stops=%d calls=%v", machine.starts, machine.stops, fb.calls)
	}
}
