package ui

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/approve"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/launch"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/plan"
	domain "github.com/Tiago-0liveira/bonsai/internal/git"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
	gardenfake "github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi/fake"
	tea "github.com/charmbracelet/bubbletea"
)

type fakeLaunchUIAccounts struct {
	agentsapi.Accounts
	resolution agents.LaunchResolution
}

func (f *fakeLaunchUIAccounts) ResolveLaunch(context.Context, agents.AccountID, agents.LaunchOptions) (agents.LaunchResolution, error) {
	return f.resolution, nil
}

type fakeLaunchUIBackend struct {
	*gardenfake.Backend
	mu                                                   sync.Mutex
	previews                                             []gardenapi.LaunchPreviewRequest
	starts                                               []gardenapi.LaunchStartRequest
	attempts                                             []launch.Attempt
	stops, retries, publishes, cleanupPreviews, cleanups int
	previewErr, startErr                                 error
	startEntered, startRelease                           chan struct{}
	lastPreview                                          gardenapi.LaunchPreviewView
	lastCleanup                                          gardenapi.LaunchCleanupView
	resolution                                           agents.LaunchResolution
	attemptsReads                                        int
}

var _ gardenapi.LaunchBackend = (*fakeLaunchUIBackend)(nil)

func (f *fakeLaunchUIBackend) PreviewLaunch(ctx context.Context, r gardenapi.LaunchPreviewRequest) (gardenapi.LaunchPreviewView, error) {
	rv, err := f.Backend.PlanReview(ctx, r.IssueID)
	if err != nil {
		return gardenapi.LaunchPreviewView{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.previews = append(f.previews, r)
	if f.previewErr != nil {
		return gardenapi.LaunchPreviewView{}, f.previewErr
	}
	if !rv.Review.Ready {
		return gardenapi.LaunchPreviewView{}, &gardenapi.Error{Code: gardenapi.CodeLaunchBlocked, Message: "Verified spec and plan approvals are required."}
	}
	g := rv.Review.Gate
	approved := rv.Review.Approved
	id := fmt.Sprintf("impl-ui-%d", len(f.previews))
	if r.Branch == "" {
		r.Branch = "bonsai/101-" + approved.Hash[:8]
	}
	if r.BaseRef == "" {
		r.BaseRef = "main"
	}
	if r.RemoteName == "" {
		r.RemoteName = "origin"
	}
	if r.Name == "" {
		r.Name = "Implement #101"
	}
	if r.Cols == 0 {
		r.Cols = 100
	}
	if r.Rows == 0 {
		r.Rows = 25
	}
	if r.AccountID == "" {
		r.AccountID = "c1"
	}
	p := launch.Preview{ID: id, Request: launch.Request{Target: approveTargetForFakeLaunch(rv), Branch: r.Branch, BaseRef: r.BaseRef, RemoteName: r.RemoteName, AccountID: r.AccountID, Launch: r.Launch, Name: r.Name, Cols: r.Cols, Rows: r.Rows}, Inputs: launch.Inputs{Spec: plan.SpecRef{RevisionHash: g.Head.Hash, CommentID: g.Head.CommentID, Ordinal: g.Ordinal, Fingerprint: g.Fingerprint}, SpecApprovalID: g.Approval.CommentID, Plan: launch.PlanRef{RevisionHash: approved.Hash, CommentID: approved.CommentID, Ordinal: approved.Ordinal, DecisionID: approved.Decisions[len(approved.Decisions)-1].CommentID, BaseCommit: approved.BaseCommit}}, Account: f.resolution, Attempts: append([]launch.Attempt(nil), f.attempts...)}
	it := f.Backend.WorkView.Snapshot.Items[0]
	p.Remote = launch.RemoteSnapshot{Target: p.Request.Target, Local: garden.LocalRepoID(rv.Stamp.LocalRepoID), Epoch: rv.Stamp.Epoch, Issue: it.Issue, FullName: "acme/my-app", Review: rv.Review, Live: true, VerifiedAt: time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)}
	p.Proposal = domain.IsolationProposal{IsolationRequest: domain.IsolationRequest{OperationID: id, RepositoryID: "local", Branch: r.Branch, BaseRef: r.BaseRef, BaseSHA: approved.BaseCommit, RemoteName: r.RemoteName, Repository: "acme/my-app"}, Path: filepath.Join("/authorized/worktrees", strings.ReplaceAll(r.Branch, "/", "-")), RootRevision: 1, ConfigHash: strings.Repeat("d", 64)}
	p.Escalated = p.Account.Launch.PermissionMode == "bypassPermissions" || p.Account.Launch.Sandbox == "bypass" || p.Account.Launch.FullAccess != nil && *p.Account.Launch.FullAccess
	f.lastPreview = gardenapi.LaunchPreviewView{Stamp: rv.Stamp, IssueID: r.IssueID, Preview: p}
	return f.lastPreview, nil
}
func (f *fakeLaunchUIBackend) StartLaunch(ctx context.Context, r gardenapi.LaunchStartRequest) (gardenapi.LaunchAttemptView, error) {
	f.mu.Lock()
	f.starts = append(f.starts, r)
	entered, release := f.startEntered, f.startRelease
	f.mu.Unlock()
	if entered != nil {
		close(entered)
		<-release
	}
	if err := ctx.Err(); err != nil {
		return gardenapi.LaunchAttemptView{}, err
	}
	p := r.Preview.Preview
	a := fakeLaunchUIAttempt(p)
	f.mu.Lock()
	f.attempts = append(f.attempts, a)
	f.mu.Unlock()
	return gardenapi.LaunchAttemptView{Stamp: r.Preview.Stamp, IssueID: r.Preview.IssueID, Attempt: a}, f.startErr
}
func fakeLaunchUIAttempt(p launch.Preview) launch.Attempt {
	return launch.Attempt{ID: p.ID, RequestHash: strings.Repeat("a", 64), Local: p.Remote.Local, Repo: p.Request.Target.Repo, Issue: p.Request.Target.Issue, Number: 101, ActorID: p.Request.Target.Viewer, Epoch: p.Remote.Epoch, Inputs: p.Inputs, Proposal: p.Proposal, Provider: p.Account.Provider, AccountID: p.Account.AccountID, AccountName: p.Account.AccountName, Options: launch.Options{Model: p.Account.Launch.Model, Effort: p.Account.Launch.Effort, PermissionMode: p.Account.Launch.PermissionMode, Sandbox: p.Account.Launch.Sandbox, Approval: p.Account.Launch.Approval, FullAccess: p.Account.Launch.FullAccess}, Name: p.Request.Name, Cols: p.Request.Cols, Rows: p.Request.Rows, State: launch.StateRunning, WorktreeID: "wt-impl", SessionID: "sess_aabb", Runs: []launch.Run{{Number: 1, OperationID: p.ID + "/start/1", SessionID: "sess_aabb", State: "running", Acknowledged: true}}, PR: launch.PRLink{State: launch.PRNotCreated}, Steps: map[launch.Step]launch.StepRecord{launch.StepMirror: {State: launch.StepPending, Code: "remote-pending"}}, Revision: 1, RequestedAt: p.Remote.VerifiedAt, UpdatedAt: p.Remote.VerifiedAt}
}
func (f *fakeLaunchUIBackend) LaunchAttempts(ctx context.Context, id uint64) (gardenapi.LaunchAttemptsView, error) {
	st, err := f.Backend.Status(ctx)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.attemptsReads++
	return gardenapi.LaunchAttemptsView{Stamp: st.Stamp, IssueID: id, Attempts: append([]launch.Attempt(nil), f.attempts...)}, err
}
func (f *fakeLaunchUIBackend) RetryLaunch(_ context.Context, r launch.RetryRequest) (gardenapi.LaunchAttemptView, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.retries++
	return gardenapi.LaunchAttemptView{Stamp: f.lastPreview.Stamp, IssueID: 1, Attempt: f.attempts[0]}, nil
}
func (f *fakeLaunchUIBackend) StopLaunch(_ context.Context, id string) (gardenapi.LaunchAttemptView, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stops++
	return gardenapi.LaunchAttemptView{Stamp: f.lastPreview.Stamp, IssueID: 1, Attempt: f.attempts[0]}, nil
}
func (f *fakeLaunchUIBackend) PublishLaunch(_ context.Context, r launch.PublishRequest) (gardenapi.LaunchAttemptView, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.publishes++
	if !r.Confirmed {
		panic("UI sent unconfirmed publish")
	}
	return gardenapi.LaunchAttemptView{Stamp: f.lastPreview.Stamp, IssueID: 1, Attempt: f.attempts[0]}, nil
}
func (f *fakeLaunchUIBackend) PreviewLaunchCleanup(_ context.Context, id string) (gardenapi.LaunchCleanupView, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cleanupPreviews++
	a := f.attempts[0]
	f.lastCleanup = gardenapi.LaunchCleanupView{Stamp: f.lastPreview.Stamp, IssueID: 1, Preview: launch.CleanupPreview{AttemptID: id, Revision: a.Revision, Allowed: true, Check: domain.IsolationCheck{Proposal: a.Proposal, HeadSHA: a.Proposal.BaseSHA, Owned: true, Exists: true, Clean: true, Hooks: "done", Fingerprint: strings.Repeat("e", 64)}}}
	return f.lastCleanup, nil
}
func (f *fakeLaunchUIBackend) CleanupLaunch(_ context.Context, p gardenapi.LaunchCleanupView, confirmed bool) (gardenapi.LaunchAttemptView, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cleanups++
	if !confirmed || p.Preview.Revision != f.lastCleanup.Preview.Revision || p.Preview.Check.Fingerprint != f.lastCleanup.Preview.Check.Fingerprint {
		panic("cleanup confirmation changed")
	}
	return gardenapi.LaunchAttemptView{Stamp: f.lastPreview.Stamp, IssueID: 1, Attempt: f.attempts[0]}, nil
}
func fakeLaunchUIEnv(t *testing.T, w, h int) (Model, plannerRig, *fakeLaunchUIBackend) {
	t.Helper()
	m, rig := plannerEnv(t, w, h)
	revision := scriptPlan(t, rig, longPlan("approved implementation"))
	rig.garden.AddPlanDecision(1, revision, garden.DecisionApproved, "Approve exact plan.")
	rv, err := rig.garden.PlanReview(context.Background(), 1)
	if err != nil || !rv.Review.Ready {
		t.Fatalf("UI fixture not Ready: %+v %v", rv, err)
	}
	resolution := agents.LaunchResolution{AccountID: "c1", AccountName: "work", Provider: "claude", Launch: agents.LaunchOptions{Model: "opus", Effort: "high", PermissionMode: "default"}, Fields: []agents.LaunchField{{ID: "permission_mode", Label: "Permission mode", Kind: agents.LaunchFieldSelect, Options: []agents.FieldOption{{Value: "default", Label: "Default"}, {Value: "bypassPermissions", Label: "Bypass permissions"}}}}, Inherited: map[string]bool{"model": true, "effort": true, "permission_mode": true}}
	fb := &fakeLaunchUIBackend{Backend: rig.garden, resolution: resolution}
	m.garden.api = fb
	rig.agents.Accounts.Items = rig.agents.Accounts.Items[:1]
	m.agents.api.Accounts = &fakeLaunchUIAccounts{Accounts: m.agents.api.Accounts, resolution: resolution}
	return m, rig, fb
}
func fakeLaunchUIReview(t *testing.T, m Model) Model {
	t.Helper()
	m = plannerPress(t, m, "enter", "s")
	for i := 0; i < 3 && !strings.Contains(view(m), "Confirm the proposed change"); i++ {
		m = plannerPress(t, m, "ctrl+s")
		if !strings.Contains(view(m), "Confirm the proposed change") && i == 0 {
			m = plannerPress(t, m, "enter")
		}
	}
	if !strings.Contains(view(m), "Confirm the proposed change") {
		t.Fatalf("launch did not reach review:\n%s", view(m))
	}
	return m
}
func TestGardenLaunchReadyActionAndApprovalReadsNeverStart(t *testing.T) {
	m, _, fb := fakeLaunchUIEnv(t, 120, 35)
	m = plannerPress(t, m, "enter")
	mustContain(t, "Ready detail", view(m), "Start implementation locally", "Implementation attempts")
	m = plannerPress(t, m, "r", "R", "esc", "i", "esc", "esc")
	if len(fb.starts) != 0 {
		t.Fatal("read/refresh/history/navigation started holder")
	}
	m = plannerPress(t, m, "enter", "s")
	if len(fb.starts) != 0 {
		t.Fatal("opening launch form started holder")
	}
}
func TestGardenLaunchConfirmationIsCancelFirstAndCtrlSCannotBypass(t *testing.T) {
	for _, size := range [][2]int{{120, 35}, {80, 24}} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			m, _, fb := fakeLaunchUIEnv(t, size[0], size[1])
			m = fakeLaunchUIReview(t, m)
			if len(fb.starts) != 0 {
				t.Fatal("Ctrl+S skipped confirmation")
			}
			mustContain(t, "consequence", view(m), "Cancel", "Confirm", "local holder", "Draft PR")
			mustFit(t, "confirmation", m)
			m = plannerPress(t, m, "enter")
			if len(fb.starts) != 0 {
				t.Fatal("initial Enter started work instead of canceling")
			}
		})
	}
}
func TestGardenLaunchExplicitConfirmFreezesExactPreviewOnce(t *testing.T) {
	m, _, fb := fakeLaunchUIEnv(t, 120, 35)
	m = fakeLaunchUIReview(t, m)
	p := fb.lastPreview
	mustContain(t, "frozen facts", view(m), p.Preview.Proposal.Branch, "opus", "high", "default")
	m = plannerPress(t, m, "tab", "enter")
	if len(fb.starts) != 1 {
		t.Fatalf("confirmed starts=%d\n%s", len(fb.starts), view(m))
	}
	req := fb.starts[0]
	if !req.Confirmed || req.Preview.Preview.ID != p.Preview.ID || req.Preview.Preview.Inputs != p.Preview.Inputs || req.Preview.Preview.Proposal != p.Preview.Proposal {
		t.Fatalf("confirmation replaced accepted facts: %+v", req)
	}
	m = plannerPress(t, m, "enter", "ctrl+s")
	if len(fb.starts) != 1 {
		t.Fatal("double submit started again")
	}
}
func TestGardenLaunchBlockedPermissionsHaveNoActiveConfirmation(t *testing.T) {
	m, _, fb := fakeLaunchUIEnv(t, 120, 35)
	fb.previewErr = &gardenapi.Error{Code: gardenapi.CodeLaunchBlocked, Message: "Native permissions unresolved. Choose an explicit permission mode."}
	m = plannerPress(t, m, "enter", "s", "ctrl+s", "tab", "enter")
	if len(fb.starts) != 0 {
		t.Fatal("blocked permissions reached StartLaunch")
	}
	mustContain(t, "permission blocker", view(m), "permission")
}
func TestGardenLaunchAcceptedLateResultRetainsHistoryWithoutFocusTheft(t *testing.T) {
	m, _, fb := fakeLaunchUIEnv(t, 120, 35)
	m = fakeLaunchUIReview(t, m)
	fb.startEntered, fb.startRelease = make(chan struct{}), make(chan struct{})
	m = plannerPress(t, m, "tab")
	var cmd tea.Cmd
	m, cmd = send(t, m, keyFor("enter"))
	beforeReads := fb.attemptsReads
	messages := make(chan []tea.Msg, 1)
	go func() { messages <- runCmds(cmd) }()
	fakeLaunchUIReceive(t, fb.startEntered)
	m = plannerPress(t, m, "esc")
	m, cmd = m.selectWorkspace(wsAccounts)
	m = plannerRun(t, m, cmd)
	close(fb.startRelease)
	for _, msg := range fakeLaunchUIReceive(t, messages) {
		m, cmd = send(t, m, msg)
		m = plannerRun(t, m, cmd)
	}
	if m.workspace != wsAccounts || len(fb.starts) != 1 || len(fb.attempts) != 1 || fb.attemptsReads <= beforeReads {
		t.Fatalf("late result lost operation/stole focus: workspace=%v starts=%d attempts=%d", m.workspace, len(fb.starts), len(fb.attempts))
	}
	m, cmd = m.selectWorkspace(wsGarden)
	m = plannerRun(t, m, cmd)
	m = plannerPress(t, m, "enter", "i")
	mustContain(t, "late history", view(m), fb.attempts[0].ID)
}

func approveTargetForFakeLaunch(rv gardenapi.PlanReviewView) approve.Target {
	return approve.Target{Repo: garden.RepoID(rv.Stamp.GardenID), Issue: garden.IssueID(rv.IssueID), Viewer: rv.ActorID, Login: rv.Actor}
}

func fakeLaunchUIAction(t *testing.T, m Model, action string) Model {
	t.Helper()
	for i := 0; i < 2; i++ {
		for _, hint := range m.shellHints() {
			if strings.Contains(strings.ToLower(hint.desc), strings.ToLower(action)) {
				return plannerPress(t, m, hint.key)
			}
		}
		if i == 0 {
			m = plannerPress(t, m, "enter")
		}
	}
	t.Fatalf("no keyboard action for %s:\n%s", action, view(m))
	return m
}
func TestGardenLaunchStopCleanupAndPublishUseDistinctCancelFirstConfirmations(t *testing.T) {
	for _, action := range []string{"stop", "cleanup", "push"} {
		t.Run(action, func(t *testing.T) {
			m, _, fb, a := fakeLaunchUIHistory(t, 120, 35, false)
			if action != "stop" {
				fb.attempts[0].State = launch.StateAwaitingReview
				fb.attempts[0].Runs[0].State = "exited"
				fb.attempts[0].Runs[0].ExitCode = fakeLaunchUIExitCode(0)
			}
			m = fakeLaunchUIAction(t, m, action)
			mustContain(t, "separate confirmation", view(m), "Cancel")
			if action == "cleanup" {
				if fb.cleanupPreviews != 1 {
					t.Fatal("cleanup did not first obtain a preview")
				}
				mustContain(t, "cleanup facts", view(m), a.Proposal.Path, a.Proposal.Branch, a.Proposal.BaseSHA, "remain")
			}
			if action == "push" {
				mustContain(t, "publish facts", view(m), "Draft", "origin", a.Proposal.Branch, "main")
			}
			m = plannerPress(t, m, "enter")
			if fb.stops+fb.cleanups+fb.publishes != 0 {
				t.Fatal("initial Enter performed destructive/publish action")
			}
			m = fakeLaunchUIAction(t, m, action)
			m = plannerPress(t, m, "tab", "enter")
			wantStop, wantCleanup, wantPublish := 0, 0, 0
			switch action {
			case "stop":
				wantStop = 1
			case "cleanup":
				wantCleanup = 1
			case "push":
				wantPublish = 1
			}
			if fb.stops != wantStop || fb.cleanups != wantCleanup || fb.publishes != wantPublish {
				t.Fatalf("crossed confirmations: stop=%d cleanup=%d publish=%d", fb.stops, fb.cleanups, fb.publishes)
			}
		})
	}
}

func TestGardenLaunchObsoletePreviewAndResizeCannotReuseConfirmation(t *testing.T) {
	m, _, fb := fakeLaunchUIEnv(t, 120, 35)
	m = fakeLaunchUIReview(t, m)
	before := fb.lastPreview
	var cmd tea.Cmd
	m, cmd = send(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m = plannerRun(t, m, cmd)
	m = plannerPress(t, m, "tab", "enter")
	if len(fb.starts) > 0 && fb.starts[0].Preview.Preview.ID == before.Preview.ID {
		if fb.starts[0].Preview.Preview.Request.Cols != before.Preview.Request.Cols || fb.starts[0].Preview.Preview.Request.Rows != before.Preview.Request.Rows {
			t.Fatal("resized dimensions started under an old confirmation")
		}
	}
}

func TestGardenLaunchOptionalRuntimeUnavailableIsTruthful(t *testing.T) {
	m, rig := plannerEnv(t, 120, 35)
	rev := scriptPlan(t, rig, longPlan("approved"))
	rig.garden.AddPlanDecision(1, rev, garden.DecisionApproved, "Approved.")
	m = plannerPress(t, m, "enter", "s")
	out := strings.ToLower(view(m))
	if !strings.Contains(out, "unavailable") && !strings.Contains(out, "unsupported") {
		t.Fatalf("missing optional launch runtime not explained:\n%s", view(m))
	}
	m = plannerPress(t, m, "ctrl+s", "tab", "enter")
	if len(rig.agents.Sessions.Starts()) != 0 || rig.garden.Count("StartPlanner") != 0 {
		t.Fatal("missing launch runtime fell back to another start")
	}
}

func fakeLaunchUIReceive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for UI operation synchronization")
		var zero T
		return zero
	}
}
