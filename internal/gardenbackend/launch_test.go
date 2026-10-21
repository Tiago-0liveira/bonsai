package gardenbackend

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/connect"
	gardenfake "github.com/Tiago-0liveira/bonsai/internal/core/garden/fake"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/launch"
	domain "github.com/Tiago-0liveira/bonsai/internal/git"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
)

type fakeLaunchBackendClock struct{}

func (fakeLaunchBackendClock) Now() time.Time { return time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC) }

type fakeLaunchBackendIDs struct {
	mu sync.Mutex
	n  int
}

func (f *fakeLaunchBackendIDs) NewID() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.n++
	return fmt.Sprintf("backend-%08x", f.n)
}
func (f *fakeLaunchBackendIDs) NewSessionID() (agents.SessionID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.n++
	return agents.SessionID(fmt.Sprintf("sess_%08x", f.n)), nil
}

type fakeLaunchBackendRuntime struct {
	starts       []launch.StartSession
	observations map[launch.StartIdentity]launch.SessionObservation
	stops        int
}

func (f *fakeLaunchBackendRuntime) ResolveLaunch(_ context.Context, id agents.AccountID, l agents.LaunchOptions) (agents.LaunchResolution, error) {
	return agents.LaunchResolution{AccountID: id, AccountName: "personal", Provider: "codex", Launch: agents.LaunchOptions{Model: "gpt-6.1-sol", Effort: "high", Sandbox: "workspace-write", Approval: "on-request"}}, nil
}
func (f *fakeLaunchBackendRuntime) Start(_ context.Context, r launch.StartSession) (launch.SessionObservation, error) {
	f.starts = append(f.starts, r)
	started := fakeLaunchBackendClock{}.Now()
	o := launch.SessionObservation{Identity: r.Identity, State: "running", Alive: true, Acknowledged: true, StartedAt: &started}
	f.observations[r.Identity] = o
	return o, nil
}
func (f *fakeLaunchBackendRuntime) Query(_ context.Context, id launch.StartIdentity) (launch.SessionObservation, error) {
	return f.observations[id], nil
}
func (f *fakeLaunchBackendRuntime) Stop(_ context.Context, id launch.StartIdentity) error {
	f.stops++
	return nil
}

type fakeLaunchBackendWorktrees struct {
	root      string
	checks    map[string]domain.IsolationCheck
	creates   int
	createErr error
}

func (f *fakeLaunchBackendWorktrees) PreviewIsolation(_ context.Context, r domain.IsolationRequest) (domain.IsolationProposal, error) {
	return domain.IsolationProposal{IsolationRequest: r, Path: filepath.Join(f.root, strings.ReplaceAll(r.Branch, "/", "-")), RootRevision: 1, ConfigHash: strings.Repeat("e", 64)}, nil
}
func (f *fakeLaunchBackendWorktrees) CreateIsolation(_ context.Context, r domain.IsolationCreateRequest) (domain.IsolationCheck, error) {
	f.creates++
	c := domain.IsolationCheck{Proposal: r.Proposal, Worktree: domain.Worktree{ID: "wt-launch", RepositoryID: r.Proposal.RepositoryID, Path: r.Proposal.Path, Branch: r.Proposal.Branch, HeadSHA: r.Proposal.BaseSHA}, Exists: true, Owned: true, Clean: true, HeadSHA: r.Proposal.BaseSHA, Hooks: "done", Fingerprint: strings.Repeat("f", 64)}
	f.checks[r.Proposal.OperationID] = c
	return c, f.createErr
}
func (f *fakeLaunchBackendWorktrees) InspectIsolation(_ context.Context, p domain.IsolationProposal) (domain.IsolationCheck, error) {
	return f.checks[p.OperationID], nil
}
func (f *fakeLaunchBackendWorktrees) RemoveIsolation(context.Context, domain.IsolationRemoveRequest) error {
	return nil
}
func (f *fakeLaunchBackendWorktrees) InspectPublishedHead(context.Context, domain.IsolationProposal) (domain.PublishedHead, error) {
	return domain.PublishedHead{}, nil
}
func (f *fakeLaunchBackendWorktrees) PushIsolation(context.Context, domain.IsolationPushRequest) (domain.PublishedHead, error) {
	return domain.PublishedHead{}, errors.New("unexpected push")
}
func fakeLaunchBackendHarness(t *testing.T) (*harness, uint64, *fakeLaunchBackendRuntime, *fakeLaunchBackendWorktrees) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	h, _, id, _, co := plannerHarness(t)
	co.base.Commit = strings.Repeat("c", 40)
	co.base.Branch = "main"
	approveIssue(t, h, id)
	ctx := context.Background()
	pv, err := h.b.PreviewPlanner(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	att, err := h.b.StartPlanner(ctx, startReq(pv, "claude-acct"))
	if err != nil {
		t.Fatal(err)
	}
	pub, err := h.b.PreviewPlanPublish(ctx, gardenapi.PlanPublishRequest{IssueID: id, AttemptID: att.Attempt.ID, Text: backendPlan})
	if err != nil {
		t.Fatal(err)
	}
	res, err := h.b.PublishPlan(ctx, pub)
	if err != nil {
		t.Fatal(err)
	}
	rv, err := h.b.PlanReview(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	_, err = h.b.DecidePlan(ctx, gardenapi.PlanDecisionRequest{Stamp: rv.Stamp, IssueID: id, RevisionHash: res.Hash, Fingerprint: rv.Review.Gate.Fingerprint, SpecHash: rv.Review.Gate.Head.Hash, ActorID: rv.ActorID, Decision: garden.DecisionApproved})
	if err != nil {
		t.Fatal(err)
	}
	runtime := &fakeLaunchBackendRuntime{observations: map[launch.StartIdentity]launch.SessionObservation{}}
	wt := &fakeLaunchBackendWorktrees{root: t.TempDir(), checks: map[string]domain.IsolationCheck{}}
	h.b.deps.LaunchStore = &launch.FileStore{Path: filepath.Join(t.TempDir(), "launch.json")}
	h.b.deps.LaunchAccounts = runtime
	h.b.deps.LaunchHolder = runtime
	h.b.deps.LaunchSessions = runtime
	h.b.deps.LaunchWorktrees = wt
	h.b.deps.LaunchClock = fakeLaunchBackendClock{}
	h.b.deps.LaunchIDs = &fakeLaunchBackendIDs{}
	return h, id, runtime, wt
}
func fakeLaunchBackendRequest(id uint64) gardenapi.LaunchPreviewRequest {
	return gardenapi.LaunchPreviewRequest{IssueID: id, AccountID: "acct_test", Branch: "bonsai/9-test", BaseRef: "main", RemoteName: "origin", Name: "Implement #9", Cols: 100, Rows: 25}
}
func TestGardenLaunchBackendReadsAndApprovalNeverStartImplementation(t *testing.T) {
	h, id, runtime, wt := fakeLaunchBackendHarness(t)
	var _ gardenapi.LaunchBackend = h.b
	for i := 0; i < 2; i++ {
		if _, err := h.b.Work(context.Background()); err != nil {
			t.Fatal(err)
		}
		if _, err := h.b.PlanReview(context.Background(), id); err != nil {
			t.Fatal(err)
		}
		if _, err := h.b.LaunchAttempts(context.Background(), id); err != nil {
			t.Fatal(err)
		}
	}
	if len(runtime.starts) != 0 || wt.creates != 0 {
		t.Fatal("read/approval implicitly launched implementation")
	}
	p, err := h.b.PreviewLaunch(context.Background(), fakeLaunchBackendRequest(id))
	if err != nil || p.Preview.Inputs.SpecApprovalID == 0 || p.Preview.Inputs.Plan.DecisionID == 0 {
		t.Fatalf("preview=%+v %v", p, err)
	}
	if wt.creates != 0 || len(runtime.starts) != 0 {
		t.Fatal("Preview started work")
	}
}
func TestGardenLaunchBackendRejectsChangedStampAndLiveAccess(t *testing.T) {
	for _, mode := range []string{"epoch", "Garden", "local", "access"} {
		t.Run(mode, func(t *testing.T) {
			h, id, runtime, wt := fakeLaunchBackendHarness(t)
			p, err := h.b.PreviewLaunch(context.Background(), fakeLaunchBackendRequest(id))
			if err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "epoch":
				p.Stamp.Epoch++
			case "Garden":
				p.Stamp.GardenID++
			case "local":
				p.Stamp.LocalRepoID = "foreign"
			case "access":
				h.gh.Mu.Lock()
				r := h.gh.Repositories[acmeURL]
				r.Access = connect.AccessRead
				h.gh.Repositories[acmeURL] = r
				h.gh.Repositories[acmeSSH] = r
				h.gh.Mu.Unlock()
			}
			v, err := h.b.StartLaunch(context.Background(), gardenapi.LaunchStartRequest{Preview: p, Confirmed: true})
			if err == nil || v.Attempt.ID != "" || wt.creates != 0 || len(runtime.starts) != 0 {
				t.Fatalf("changed confirmation=%+v err=%v", v, err)
			}
		})
	}
}
func TestGardenLaunchBackendPartialFailureReturnsSafePopulatedView(t *testing.T) {
	h, id, runtime, wt := fakeLaunchBackendHarness(t)
	p, err := h.b.PreviewLaunch(context.Background(), fakeLaunchBackendRequest(id))
	if err != nil {
		t.Fatal(err)
	}
	wt.createErr = errors.Join(domain.ErrUncertain, errors.New("token=ghp_fixture_secret"))
	v, err := h.b.StartLaunch(context.Background(), gardenapi.LaunchStartRequest{Preview: p, Confirmed: true})
	if err == nil || v.Attempt.ID == "" || code(err) != gardenapi.CodeLaunchUnknown || strings.Contains(err.Error(), "ghp_fixture_secret") || len(runtime.starts) != 0 {
		t.Fatalf("partial=%+v err=%v", v, err)
	}
	s := h.b.deps.LaunchStore.(*launch.FileStore)
	raw, err := os.ReadFile(s.Path)
	if err != nil || strings.Contains(string(raw), "ghp_fixture_secret") {
		t.Fatalf("unsafe disk=%s %v", raw, err)
	}
}
func TestGardenLaunchBackendOfflineHistoryStopAndForeignOwnership(t *testing.T) {
	h, id, runtime, _ := fakeLaunchBackendHarness(t)
	p, err := h.b.PreviewLaunch(context.Background(), fakeLaunchBackendRequest(id))
	if err != nil {
		t.Fatal(err)
	}
	v, err := h.b.StartLaunch(context.Background(), gardenapi.LaunchStartRequest{Preview: p, Confirmed: true})
	if err != nil {
		t.Fatal(err)
	}
	h.gh.Mu.Lock()
	h.gh.Repositories = map[string]connect.Repository{}
	h.gh.Mu.Unlock()
	list, err := h.b.LaunchAttempts(context.Background(), id)
	if err != nil || len(list.Attempts) != 1 || list.Attempts[0].ID != v.Attempt.ID {
		t.Fatalf("offline history=%+v %v", list, err)
	}
	if _, err = h.b.StopLaunch(context.Background(), v.Attempt.ID); err != nil || runtime.stops != 1 {
		t.Fatalf("offline Stop=%v count=%d", err, runtime.stops)
	}
	other := h.clone(t.TempDir())
	other.deps.LaunchStore = h.b.deps.LaunchStore
	other.deps.LaunchAccounts = runtime
	other.deps.LaunchHolder = runtime
	other.deps.LaunchSessions = runtime
	other.deps.LaunchWorktrees = h.b.deps.LaunchWorktrees
	other.deps.LaunchClock = fakeLaunchBackendClock{}
	other.deps.LaunchIDs = &fakeLaunchBackendIDs{}
	if _, err = other.StopLaunch(context.Background(), v.Attempt.ID); err == nil {
		t.Fatal("foreign checkout stopped attempt")
	}
	if runtime.stops != 1 {
		t.Fatal("foreign stop reached holder")
	}
}

type fakeLaunchBackendMirrorWriter struct {
	garden.IssueWriter
	fail     bool
	calls    int
	expected [][]string
}

func (f *fakeLaunchBackendMirrorWriter) UpdateStage(ctx context.Context, repo garden.RepoID, issue garden.IssueID, labels []string, from, to garden.Stage) error {
	f.calls++
	f.expected = append(f.expected, append([]string(nil), labels...))
	if f.fail {
		return garden.ErrOffline
	}
	return f.IssueWriter.UpdateStage(ctx, repo, issue, labels, from, to)
}
func TestGardenLaunchMirrorPendingPreservesLocalTruthAndUnrelatedLabels(t *testing.T) {
	h, id, runtime, _ := fakeLaunchBackendHarness(t)
	f, ok := h.b.deps.Issues.(*gardenfake.Fake)
	if !ok {
		t.Fatal("expected existing Garden fake")
	}
	issue, err := f.GetIssue(context.Background(), acmeID, garden.IssueID(id))
	if err != nil {
		t.Fatal(err)
	}
	issue.Labels = append(issue.Labels, "priority:keep")
	f.SeedIssue(issue)
	writer := &fakeLaunchBackendMirrorWriter{IssueWriter: h.b.deps.Writer, fail: true}
	h.b.deps.Writer = writer
	p, err := h.b.PreviewLaunch(context.Background(), fakeLaunchBackendRequest(id))
	if err != nil {
		t.Fatal(err)
	}
	v, err := h.b.StartLaunch(context.Background(), gardenapi.LaunchStartRequest{Preview: p, Confirmed: true})
	if err != nil || v.Attempt.State != launch.StateRunning || v.Attempt.Steps[launch.StepMirror].Code != "remote-pending" {
		t.Fatalf("mirror outage replaced local truth: %+v %v", v, err)
	}
	if writer.calls != 1 || !slices.Contains(writer.expected[0], "priority:keep") {
		t.Fatalf("mirror did not use exact expected labels: %+v", writer.expected)
	}
	writer.fail = false
	history, err := h.b.LaunchAttempts(context.Background(), id)
	if err != nil || len(history.Attempts) != 1 || history.Attempts[0].MirroredStage != garden.StageInProgress || len(runtime.starts) != 1 {
		t.Fatalf("mirror retry=%+v %v", history, err)
	}
	issue, err = f.GetIssue(context.Background(), acmeID, garden.IssueID(id))
	if err != nil || !slices.Contains(issue.Labels, "priority:keep") {
		t.Fatalf("mirror erased unrelated labels: %+v %v", issue, err)
	}
}
func TestGardenLaunchMirrorNeverOverwritesExternalStageOrNewSpec(t *testing.T) {
	for _, mode := range []string{"already desired", "external done", "external review", "new spec"} {
		t.Run(mode, func(t *testing.T) {
			h, id, runtime, _ := fakeLaunchBackendHarness(t)
			f := h.b.deps.Issues.(*gardenfake.Fake)
			writer := &fakeLaunchBackendMirrorWriter{IssueWriter: h.b.deps.Writer, fail: true}
			h.b.deps.Writer = writer
			p, err := h.b.PreviewLaunch(context.Background(), fakeLaunchBackendRequest(id))
			if err != nil {
				t.Fatal(err)
			}
			a, err := h.b.StartLaunch(context.Background(), gardenapi.LaunchStartRequest{Preview: p, Confirmed: true})
			if err != nil {
				t.Fatal(err)
			}
			writer.fail = false
			before := writer.calls
			issue, err := f.GetIssue(context.Background(), acmeID, garden.IssueID(id))
			if err != nil {
				t.Fatal(err)
			}
			if mode == "new spec" {
				raw, err := garden.NewSpecRevision(garden.IssueIdentity{Repo: acmeID, Issue: garden.IssueID(id)}, completeSpec+"\nNew requirement.", []string{a.Attempt.Inputs.Spec.RevisionHash})
				if err != nil {
					t.Fatal(err)
				}
				f.AppendCommentAs(acmeID, garden.IssueID(id), viewerName, a.Attempt.ActorID, raw)
			} else {
				stage := garden.StageInProgress
				if mode == "external done" {
					stage = garden.StageDone
				}
				if mode == "external review" {
					stage = garden.StageReview
				}
				for i, label := range issue.Labels {
					if strings.HasPrefix(label, "bonsai:stage/") {
						issue.Labels[i] = garden.FormatStageLabel(stage)
					}
				}
				f.SeedIssue(issue)
			}
			view, err := h.b.LaunchAttempts(context.Background(), id)
			if err != nil || len(view.Attempts) != 1 || view.Attempts[0].State != launch.StateRunning || len(runtime.starts) != 1 {
				t.Fatalf("external changes erased local truth: %+v %v", view, err)
			}
			if writer.calls != before {
				t.Fatal("mirror overwrote external or already desired state")
			}
		})
	}
}
