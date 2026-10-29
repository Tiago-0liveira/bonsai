package gardenbackend

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agentactivity"
	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/agentterminal"
	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/connect"
	gardenfake "github.com/Tiago-0liveira/bonsai/internal/core/garden/fake"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/launch"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/reviewpass"
	domain "github.com/Tiago-0liveira/bonsai/internal/git"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
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
	queryErr     error
	queries      int
	unknownStart bool
	onStart      func()
}

func (f *fakeLaunchBackendRuntime) ResolveLaunch(_ context.Context, id agents.AccountID, l agents.LaunchOptions) (agents.LaunchResolution, error) {
	if id == "acct_missing" {
		return agents.LaunchResolution{}, agents.ErrAccountNotFound
	}
	opts := agents.LaunchOptions{Model: "gpt-6.1-sol", Effort: "high", Sandbox: "workspace-write", Approval: "on-request"}
	if l.Model != "" {
		opts.Model = l.Model
	}
	if l.Effort != "" {
		opts.Effort = l.Effort
	}
	if l.Sandbox != "" {
		opts.Sandbox = l.Sandbox
	}
	if l.Approval != "" {
		opts.Approval = l.Approval
	}
	opts.FullAccess = l.FullAccess
	return agents.LaunchResolution{AccountID: id, AccountName: "personal", Provider: "codex", Launch: opts}, nil
}
func (f *fakeLaunchBackendRuntime) Start(_ context.Context, r launch.StartSession) (launch.SessionObservation, error) {
	f.starts = append(f.starts, r)
	if f.onStart != nil {
		f.onStart()
	}
	if f.unknownStart {
		return launch.SessionObservation{}, domain.ErrUncertain
	}
	started := fakeLaunchBackendClock{}.Now()
	o := launch.SessionObservation{Identity: r.Identity, State: "running", Alive: true, Acknowledged: true, StartedAt: &started}
	f.observations[r.Identity] = o
	return o, nil
}
func (f *fakeLaunchBackendRuntime) Query(_ context.Context, id launch.StartIdentity) (launch.SessionObservation, error) {
	f.queries++
	return f.observations[id], f.queryErr
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

func fakeLaunchReviewPrefs() *config.GardenAutoReviewPrefs {
	return &config.GardenAutoReviewPrefs{Version: config.GardenAutoReviewVersion, Enabled: true, Account: "acct_reviewer", Model: "gpt-6.1-sol", Effort: "high", Sandbox: "workspace-write", Approval: "on-request", Checks: []config.GardenCheck{{Name: "Go tests", Executable: "go", Args: []string{"test", "./..."}}}}
}

func saveLaunchReviewPrefs(t *testing.T, prefs *config.GardenAutoReviewPrefs) {
	t.Helper()
	state, err := config.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	p := state.PrefsSnapshot()
	p.GardenAutoReview = prefs.Clone()
	if err := state.SetPrefs(p); err != nil {
		t.Fatal(err)
	}
}

type fakeLaunchReviewStore struct {
	reviewpass.Store
	reserves                    int
	reserveErr, listErr, getErr error
}

func (f *fakeLaunchReviewStore) Reserve(ctx context.Context, p reviewpass.Pass) (reviewpass.Pass, bool, error) {
	f.reserves++
	if f.reserveErr != nil {
		return reviewpass.Pass{}, false, f.reserveErr
	}
	return f.Store.Reserve(ctx, p)
}
func (f *fakeLaunchReviewStore) Get(ctx context.Context, id string) (reviewpass.Pass, error) {
	if f.getErr != nil {
		return reviewpass.Pass{}, f.getErr
	}
	return f.Store.Get(ctx, id)
}
func (f *fakeLaunchReviewStore) List(ctx context.Context, local garden.LocalRepoID, issue garden.IssueID) ([]reviewpass.Pass, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.Store.List(ctx, local, issue)
}
func trackLaunchReviews(t *testing.T, h *harness) *fakeLaunchReviewStore {
	t.Helper()
	store, err := reviewpass.Open()
	if err != nil {
		t.Fatal(err)
	}
	configDir, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	if store.Path != filepath.Join(configDir, "bonsai", "garden-review-passes.json") {
		t.Fatalf("unexpected review store: %s", store.Path)
	}
	tracked := &fakeLaunchReviewStore{Store: store}
	h.b.deps.LaunchReviews = tracked
	return tracked
}
func noLaunchReviewEffects(t *testing.T, h *harness, id uint64, runtime *fakeLaunchBackendRuntime, store *fakeLaunchReviewStore) {
	t.Helper()
	attempts, err := h.b.deps.LaunchStore.List(context.Background(), h.b.local, garden.IssueID(id))
	if err != nil {
		t.Fatal(err)
	}
	passes, err := store.List(context.Background(), h.b.local, garden.IssueID(id))
	if err != nil {
		t.Fatal(err)
	}
	if len(attempts) != 0 || len(passes) != 0 || len(runtime.starts) != 0 || store.reserves != 0 {
		t.Fatalf("unexpected effects: attempts=%+v passes=%+v starts=%d reserves=%d", attempts, passes, len(runtime.starts), store.reserves)
	}
}

func TestGardenLaunchReviewDisabledAndUnavailable(t *testing.T) {
	for _, unavailable := range []bool{false, true} {
		t.Run(fmt.Sprint(unavailable), func(t *testing.T) {
			h, id, runtime, _ := fakeLaunchBackendHarness(t)
			store := trackLaunchReviews(t, h)
			if unavailable {
				prefs := fakeLaunchReviewPrefs()
				prefs.Account = "acct_missing"
				saveLaunchReviewPrefs(t, prefs)
			}
			p, err := h.b.PreviewLaunch(context.Background(), fakeLaunchBackendRequest(id))
			if err != nil {
				t.Fatal(err)
			}
			if p.Review.Enabled != unavailable || p.Review.Available || p.Review.Hash != "" || p.Preview.Request.ReviewPolicyHash != "" || unavailable && p.Review.Reason == "" {
				t.Fatalf("review=%+v", p.Review)
			}
			noLaunchReviewEffects(t, h, id, runtime, store)
			v, err := h.b.StartLaunch(context.Background(), gardenapi.LaunchStartRequest{Preview: p, Confirmed: true})
			if err != nil || v.Attempt.State != launch.StateRunning || v.Review != nil || store.reserves != 0 {
				t.Fatalf("start=%+v %v", v, err)
			}
			passes, err := store.List(context.Background(), h.b.local, garden.IssueID(id))
			if err != nil || len(passes) != 0 {
				t.Fatalf("passes=%+v %v", passes, err)
			}
		})
	}
}

func TestGardenLaunchReviewFrozenBeforeStartAndReplay(t *testing.T) {
	h, id, runtime, _ := fakeLaunchBackendHarness(t)
	store := trackLaunchReviews(t, h)
	prefs := fakeLaunchReviewPrefs()
	saveLaunchReviewPrefs(t, prefs)
	// Reading preferences, previewing and supervision never authorize a pass.
	if _, err := config.LoadState(); err != nil {
		t.Fatal(err)
	}
	if _, err := h.b.LaunchAttempts(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	p, err := h.b.PreviewLaunch(context.Background(), fakeLaunchBackendRequest(id))
	if err != nil {
		t.Fatal(err)
	}
	if !p.Review.Enabled || !p.Review.Available || p.Review.Hash != p.Review.Policy.Hash() || p.Preview.Request.ReviewPolicyHash != p.Review.Hash {
		t.Fatalf("preview=%+v", p)
	}
	noLaunchReviewEffects(t, h, id, runtime, store)
	runtime.onStart = func() {
		pass, err := store.Get(context.Background(), launchReviewID(p.Preview.ID))
		if err != nil || pass.State != reviewpass.StatePending {
			t.Fatalf("holder started before pending reservation: %+v %v", pass, err)
		}
	}
	req := gardenapi.LaunchStartRequest{Preview: p, Confirmed: true}
	v, err := h.b.StartLaunch(context.Background(), req)
	if err != nil || v.Review == nil {
		t.Fatalf("start=%+v %v", v, err)
	}
	pass := *v.Review
	preview := p.Preview
	expected := reviewpass.Snapshot{Local: h.b.local, Repo: acmeID, Issue: garden.IssueID(id), Number: preview.Remote.Issue.Number,
		Inputs:    reviewpass.Inputs{SpecRevision: preview.Inputs.Spec.RevisionHash, SpecFingerprint: preview.Inputs.Spec.Fingerprint, SpecApprovalID: preview.Inputs.SpecApprovalID, PlanRevision: preview.Inputs.Plan.RevisionHash, PlanDecisionID: preview.Inputs.Plan.DecisionID},
		AttemptID: preview.ID, Worktree: reviewpass.Worktree{Path: preview.Proposal.Path, Branch: preview.Proposal.Branch, BaseSHA: preview.Proposal.BaseSHA}, Policy: p.Review.Policy, PolicyHash: p.Review.Hash}
	auth := reviewpass.Authorization{ActorID: preview.Request.Target.Viewer, PreviewID: preview.ID, ConfirmedAt: fakeLaunchBackendClock{}.Now()}
	if !reflect.DeepEqual(pass.Snapshot, expected) || pass.Authorization != auth || pass.State != reviewpass.StatePending {
		t.Fatalf("frozen pass=%+v", pass)
	}
	prefs.Model = "updated-model"
	saveLaunchReviewPrefs(t, prefs)
	// Replay must neither re-resolve the edited policy nor reserve another pass.
	h.b.deps.LaunchReviewPrefs = func() (*config.GardenAutoReviewPrefs, error) { t.Fatal("replay resolved preferences"); return nil, nil }
	replay, err := h.b.StartLaunch(context.Background(), req)
	if err != nil || replay.Review == nil || !reflect.DeepEqual(*replay.Review, pass) || len(runtime.starts) != 1 || store.reserves != 1 {
		t.Fatalf("replay=%+v %v, reserves=%d", replay, err, store.reserves)
	}
	delete(runtime.observations, runtime.starts[0].Identity)
	runtime.observations[runtime.starts[0].Identity] = launch.SessionObservation{Identity: runtime.starts[0].Identity, State: "running", Activity: "idle", Alive: true, Acknowledged: true}
	queries := runtime.queries
	list, err := h.b.LaunchAttempts(context.Background(), id)
	activity := list.Activity[v.Attempt.ID]
	if err != nil || len(list.Attempts) != 1 || list.Attempts[0].State != launch.StateRunning || activity.State != "running" || activity.Activity != "idle" || !activity.Alive || !activity.ObservedAt.Equal(fakeLaunchBackendClock{}.Now()) || runtime.queries != queries+1 || !reflect.DeepEqual(list.Reviews[v.Attempt.ID], pass) {
		t.Fatalf("supervision=%+v err=%v queries=%d", list, err, runtime.queries-queries)
	}
	data, err := os.ReadFile(h.b.deps.LaunchStore.(*launch.FileStore).Path)
	if err != nil || strings.Contains(strings.ToLower(string(data)), "activity") || strings.Contains(string(data), "idle") || list.Attempts[0].Runs[0].State != "running" {
		t.Fatalf("activity leaked into attempt store: %s, %v", data, err)
	}
	got, err := store.Get(context.Background(), pass.ID)
	if err != nil || !reflect.DeepEqual(got, pass) {
		t.Fatalf("policy edit mutated pass: %+v %v", got, err)
	}
	runtime.queryErr = errors.New("session query unavailable")
	list, err = h.b.LaunchAttempts(context.Background(), id)
	if err != nil || len(list.Activity) != 0 || list.Attempts[0].State != launch.StateRunning {
		t.Fatalf("failed observation=%+v %v", list, err)
	}
	store.listErr = errors.New("pass store unavailable")
	list, err = h.b.LaunchAttempts(context.Background(), id)
	if err != nil || list.Reviews != nil || len(list.Attempts) != 1 {
		t.Fatalf("pass read failed attempts read=%+v %v", list, err)
	}
	stopped, err := h.b.StopLaunch(context.Background(), v.Attempt.ID)
	if err != nil || stopped.Review == nil || !reflect.DeepEqual(*stopped.Review, pass) {
		t.Fatalf("stop review=%+v %v", stopped, err)
	}
}

func TestGardenLaunchReviewChangedBeforeConfirmation(t *testing.T) {
	for _, mode := range []string{"edited", "enabled", "disabled", "missing account", "now available"} {
		t.Run(mode, func(t *testing.T) {
			h, id, runtime, _ := fakeLaunchBackendHarness(t)
			store := trackLaunchReviews(t, h)
			prefs := fakeLaunchReviewPrefs()
			if mode == "enabled" {
				prefs.Enabled = false
			}
			if mode == "now available" {
				prefs.Account = "acct_missing"
			}
			saveLaunchReviewPrefs(t, prefs)
			p, err := h.b.PreviewLaunch(context.Background(), fakeLaunchBackendRequest(id))
			if err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "edited":
				prefs.Effort = "low"
			case "enabled":
				prefs.Enabled = true
			case "disabled":
				prefs.Enabled = false
			case "missing account":
				prefs.Account = "acct_missing"
			case "now available":
				prefs.Account = "acct_reviewer"
			}
			saveLaunchReviewPrefs(t, prefs)
			_, err = h.b.StartLaunch(context.Background(), gardenapi.LaunchStartRequest{Preview: p, Confirmed: true})
			mustErrCode(t, err, gardenapi.CodeLaunchChanged)
			noLaunchReviewEffects(t, h, id, runtime, store)
		})
	}
}

func TestGardenLaunchReviewerEscalationConsent(t *testing.T) {
	h, id, runtime, _ := fakeLaunchBackendHarness(t)
	store := trackLaunchReviews(t, h)
	prefs := fakeLaunchReviewPrefs()
	prefs.Sandbox, prefs.Approval = "danger-full-access", "never"
	saveLaunchReviewPrefs(t, prefs)
	p, err := h.b.PreviewLaunch(context.Background(), fakeLaunchBackendRequest(id))
	if err != nil || !p.Review.Available || !p.Review.Policy.Reviewer.Escalated {
		t.Fatalf("preview=%+v %v", p, err)
	}
	// The implementer's acknowledgment cannot stand in for the reviewer's.
	req := gardenapi.LaunchStartRequest{Preview: p, Confirmed: true, EscalationConfirmed: true}
	_, err = h.b.StartLaunch(context.Background(), req)
	mustErrCode(t, err, gardenapi.CodeLaunchBlocked)
	noLaunchReviewEffects(t, h, id, runtime, store)
	req.ReviewEscalationConfirmed = true
	v, err := h.b.StartLaunch(context.Background(), req)
	if err != nil || v.Review == nil || !v.Review.Authorization.EscalationAcknowledged {
		t.Fatalf("authorized=%+v %v", v, err)
	}
}

func TestGardenLaunchReviewUnknownStartRetainsPendingPass(t *testing.T) {
	h, id, runtime, _ := fakeLaunchBackendHarness(t)
	store := trackLaunchReviews(t, h)
	saveLaunchReviewPrefs(t, fakeLaunchReviewPrefs())
	p, err := h.b.PreviewLaunch(context.Background(), fakeLaunchBackendRequest(id))
	if err != nil {
		t.Fatal(err)
	}
	runtime.unknownStart = true
	runtime.queryErr = domain.ErrUncertain
	v, err := h.b.StartLaunch(context.Background(), gardenapi.LaunchStartRequest{Preview: p, Confirmed: true})
	mustErrCode(t, err, gardenapi.CodeLaunchUnknown)
	if v.Attempt.State != launch.StateStartUnknown || v.Review == nil || v.Review.State != reviewpass.StatePending {
		t.Fatalf("unknown start=%+v", v)
	}
	list, err := h.b.LaunchAttempts(context.Background(), id)
	if err != nil || len(list.Attempts) != 1 || list.Attempts[0].State != launch.StateStartUnknown || list.Reviews[v.Attempt.ID].State != reviewpass.StatePending || len(list.Activity) != 0 || len(runtime.starts) != 1 || store.reserves != 1 {
		t.Fatalf("unknown reconcile=%+v %v", list, err)
	}
}

func TestGardenLaunchReviewReservationFailureAndResubmit(t *testing.T) {
	h, id, runtime, wt := fakeLaunchBackendHarness(t)
	store := trackLaunchReviews(t, h)
	saveLaunchReviewPrefs(t, fakeLaunchReviewPrefs())
	p, err := h.b.PreviewLaunch(context.Background(), fakeLaunchBackendRequest(id))
	if err != nil {
		t.Fatal(err)
	}
	req := gardenapi.LaunchStartRequest{Preview: p, Confirmed: true}
	store.reserveErr = errors.New("reservation unavailable")
	v, err := h.b.StartLaunch(context.Background(), req)
	if err == nil || v.Attempt.ID != "" || len(runtime.starts) != 0 || wt.creates != 0 {
		t.Fatalf("reservation failure started work: %+v %v", v, err)
	}
	store.reserveErr = nil
	// A failure after pass reservation but before attempt creation must reuse it.
	wtPreview := p
	wtPreview.Preview.Account.Launch.Model = "tampered"
	_, err = h.b.StartLaunch(context.Background(), gardenapi.LaunchStartRequest{Preview: wtPreview, Confirmed: true})
	mustErrCode(t, err, gardenapi.CodeLaunchChanged)
	prior, err := store.Get(context.Background(), launchReviewID(p.Preview.ID))
	if err != nil || len(runtime.starts) != 0 {
		t.Fatalf("pending pass=%+v %v", prior, err)
	}
	reservations := store.reserves
	v, err = h.b.StartLaunch(context.Background(), req)
	if err != nil || v.Review == nil || !reflect.DeepEqual(*v.Review, prior) || store.reserves != reservations || len(runtime.starts) != 1 {
		t.Fatalf("resubmit=%+v %v reserves=%d", v, err, store.reserves)
	}
}

func TestGardenLaunchReviewResolutionFailureKeepsPreview(t *testing.T) {
	h, id, runtime, _ := fakeLaunchBackendHarness(t)
	store := trackLaunchReviews(t, h)
	h.b.deps.LaunchReviewPrefs = func() (*config.GardenAutoReviewPrefs, error) { return nil, errors.New("ghp_fixture_secret") }
	p, err := h.b.PreviewLaunch(context.Background(), fakeLaunchBackendRequest(id))
	if err != nil || p.Review.Available || p.Review.Reason == "" || strings.Contains(p.Review.Reason, "ghp_fixture_secret") || p.Preview.Request.ReviewPolicyHash != "" {
		t.Fatalf("preview=%+v %v", p, err)
	}
	noLaunchReviewEffects(t, h, id, runtime, store)
}

// Both confirmations read absence before either can reserve. This forces the
// timestamp conflict that a sequential duplicate cannot exercise.
type concurrentLaunchReviewStore struct {
	reviewpass.Store
	mu    sync.Mutex
	reads int
	ready chan struct{}
}

func (s *concurrentLaunchReviewStore) Get(ctx context.Context, id string) (reviewpass.Pass, error) {
	p, err := s.Store.Get(ctx, id)
	if !errors.Is(err, reviewpass.ErrNotFound) {
		return p, err
	}
	s.mu.Lock()
	s.reads++
	n := s.reads
	if n == 2 {
		close(s.ready)
	}
	s.mu.Unlock()
	if n <= 2 {
		select {
		case <-s.ready:
		case <-ctx.Done():
			return reviewpass.Pass{}, ctx.Err()
		}
	}
	return p, err
}

type concurrentLaunchReviewClock struct {
	mu sync.Mutex
	n  int
}

func (c *concurrentLaunchReviewClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.n++
	return fakeLaunchBackendClock{}.Now().Add(time.Duration(c.n) * time.Nanosecond)
}

func TestGardenLaunchReviewConcurrentConfirmations(t *testing.T) {
	h, id, runtime, _ := fakeLaunchBackendHarness(t)
	store := trackLaunchReviews(t, h)
	saveLaunchReviewPrefs(t, fakeLaunchReviewPrefs())
	p, err := h.b.PreviewLaunch(context.Background(), fakeLaunchBackendRequest(id))
	if err != nil {
		t.Fatal(err)
	}
	h.b.deps.LaunchReviews = &concurrentLaunchReviewStore{Store: store.Store, ready: make(chan struct{})}
	h.b.deps.LaunchClock = &concurrentLaunchReviewClock{}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	type result struct {
		view gardenapi.LaunchAttemptView
		err  error
	}
	results := make(chan result, 2)
	for range 2 {
		go func() {
			v, err := h.b.StartLaunch(ctx, gardenapi.LaunchStartRequest{Preview: p, Confirmed: true})
			results <- result{v, err}
		}()
	}
	first, second := <-results, <-results
	if first.err != nil || second.err != nil || first.view.Review == nil || second.view.Review == nil || !reflect.DeepEqual(first.view.Review, second.view.Review) {
		t.Fatalf("duplicate confirmations: first=%+v second=%+v", first, second)
	}
	if len(runtime.starts) != 1 || first.view.Attempt.ID != second.view.Attempt.ID {
		t.Fatal("duplicate confirmations started multiple holders")
	}
	_, err = h.b.reserveLaunchReview(ctx, p.Preview, p.Review, "another-actor", false)
	if !errors.Is(err, reviewpass.ErrConflict) {
		t.Fatalf("different consent reused a pass: %v", err)
	}
}

func TestGardenLaunchReviewReadFailureIsUnknown(t *testing.T) {
	h, id, _, _ := fakeLaunchBackendHarness(t)
	store := trackLaunchReviews(t, h)
	saveLaunchReviewPrefs(t, fakeLaunchReviewPrefs())
	p, err := h.b.PreviewLaunch(context.Background(), fakeLaunchBackendRequest(id))
	if err != nil {
		t.Fatal(err)
	}
	v, err := h.b.StartLaunch(context.Background(), gardenapi.LaunchStartRequest{Preview: p, Confirmed: true})
	if err != nil || v.Review == nil || v.ReviewUnknown {
		t.Fatalf("start=%+v %v", v, err)
	}
	store.listErr, store.getErr = errors.New("cannot read passes"), errors.New("cannot read pass")
	list, err := h.b.LaunchAttempts(context.Background(), id)
	if err != nil || !list.ReviewsErr || len(list.Attempts) != 1 {
		t.Fatalf("list=%+v %v", list, err)
	}
	view := h.b.attemptView(context.Background(), 1, v.Attempt)
	if !view.ReviewUnknown || view.Review != nil {
		t.Fatalf("view=%+v", view)
	}
	store.getErr = reviewpass.ErrNotFound
	view = h.b.attemptView(context.Background(), 2, v.Attempt)
	if view.ReviewUnknown {
		t.Fatal("successful absence was reported as unknown")
	}
	store.listErr, store.getErr = nil, nil
	list, err = h.b.LaunchAttempts(context.Background(), id)
	if err != nil || list.ReviewsErr || !reflect.DeepEqual(list.Reviews[v.Attempt.ID], *v.Review) {
		t.Fatalf("recovered=%+v %v", list, err)
	}
}

type fakeLaunchOperationSource struct {
	agentsapi.OperationSessions
	result agentsapi.OperationSession
}

func (f fakeLaunchOperationSource) QueryOperation(context.Context, agents.SessionID, string) (agentsapi.OperationSession, error) {
	return f.result, nil
}

func (f fakeLaunchOperationSource) StartOperation(context.Context, agentsapi.OperationStartRequest) (agentsapi.OperationSession, error) {
	return f.result, nil
}

func TestLaunchHolderActivityMapping(t *testing.T) {
	for _, tc := range []struct {
		name     string
		activity *agentactivity.Activity
		want     string
	}{
		{"working", &agentactivity.Activity{State: agentactivity.Working}, "working"},
		{"idle", &agentactivity.Activity{State: agentactivity.Idle}, "idle"},
		{"permission", &agentactivity.Activity{State: agentactivity.Waiting, Reason: agentactivity.ReasonPermission}, "waiting-permission"},
		{"question", &agentactivity.Activity{State: agentactivity.Waiting, Reason: agentactivity.ReasonQuestion}, "unknown"},
		{"unknown", &agentactivity.Activity{State: agentactivity.Unknown}, "unknown"},
		{"missing", nil, "unknown"},
		{"untrusted", &agentactivity.Activity{State: "secret\x1b", Detail: "private output"}, "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id := launch.StartIdentity{OperationID: "op", SessionID: "session"}
			h := launchHolder{ops: fakeLaunchOperationSource{result: agentsapi.OperationSession{
				Session: agentsapi.Session{Summary: agentterminal.Summary{ID: id.SessionID, OperationID: id.OperationID, State: "running", Activity: tc.activity}}, Acknowledged: true,
			}}}
			for _, method := range []string{"query", "start"} {
				var obs launch.SessionObservation
				var err error
				if method == "query" {
					obs, err = h.Query(context.Background(), id)
				} else {
					obs, err = h.Start(context.Background(), launch.StartSession{Identity: id})
				}
				if err != nil || obs.Activity != tc.want || obs.State != "running" || !obs.Alive || obs.Closed {
					t.Fatalf("%s: %+v, %v", method, obs, err)
				}
			}
			obs, err := h.Query(context.Background(), launch.StartIdentity{OperationID: "other", SessionID: id.SessionID})
			if err != nil || !reflect.DeepEqual(obs, launch.SessionObservation{}) {
				t.Fatalf("foreign receipt: %+v, %v", obs, err)
			}
		})
	}
}
