package launch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/approve"
	gardenfake "github.com/Tiago-0liveira/bonsai/internal/core/garden/fake"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/plan"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/planrev"
	domain "github.com/Tiago-0liveira/bonsai/internal/git"
)

const fakeLaunchSpec = "## Summary\nImplement limits.\n\n## Requirements\nEnforce a project ceiling.\n\n## Acceptance criteria\nReject excess requests.\n\n## Open questions\nNone"
const fakeLaunchPlan = "## Assumptions\nProject counters.\n\n## Touched areas\ninternal/budget\n\n## Steps\n1. Add counter.\n2. Enforce ceiling.\n\n## Tests\nConcurrent requests.\n\n## Risks\nCounter races.\n\n## Acceptance mapping\nAC-1 -> steps 1, 2."

var fakeLaunchTime = time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC)

func fakeLaunchClone[T any](v T) T {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	var out T
	if err = json.Unmarshal(b, &out); err != nil {
		panic(err)
	}
	return out
}

type fakeLaunchClock struct{}

func (fakeLaunchClock) Now() time.Time { return fakeLaunchTime }

type fakeLaunchIDs struct {
	mu sync.Mutex
	n  int
}

func (f *fakeLaunchIDs) NewID() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.n++
	return fmt.Sprintf("test-%08x", f.n)
}
func (f *fakeLaunchIDs) NewSessionID() (agents.SessionID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.n++
	return agents.SessionID(fmt.Sprintf("sess_%08x", f.n)), nil
}

type fakeLaunchRemote struct {
	mu                   sync.Mutex
	snapshot             RemoteSnapshot
	verifyErr, mirrorErr error
	mirrors              []MirrorRequest
}

func (f *fakeLaunchRemote) Verify(context.Context, approve.Target) (RemoteSnapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return fakeLaunchClone(f.snapshot), f.verifyErr
}
func (f *fakeLaunchRemote) Mirror(_ context.Context, r MirrorRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.mirrors = append(f.mirrors, r)
	return f.mirrorErr
}

type fakeLaunchAccounts struct {
	resolution agents.LaunchResolution
	err        error
}

func (f *fakeLaunchAccounts) ResolveLaunch(context.Context, agents.AccountID, agents.LaunchOptions) (agents.LaunchResolution, error) {
	return fakeLaunchClone(f.resolution), f.err
}

type fakeLaunchWorktrees struct {
	mu                                                             sync.Mutex
	root                                                           string
	resources                                                      map[string]domain.IsolationCheck
	previewErr, createErr, inspectErr, removeErr, pushErr, headErr error
	hooks                                                          string
	creates, removes, pushes                                       int
	head                                                           domain.PublishedHead
	afterCreate                                                    func()
	pushAfterEffect                                                bool
	pushResult                                                     *domain.PublishedHead
}

func (f *fakeLaunchWorktrees) PreviewIsolation(_ context.Context, r domain.IsolationRequest) (domain.IsolationProposal, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.previewErr != nil {
		return domain.IsolationProposal{}, f.previewErr
	}
	return domain.IsolationProposal{IsolationRequest: r, Path: filepath.Join(f.root, strings.ReplaceAll(r.Branch, "/", "-")), RootRevision: 1, ConfigHash: strings.Repeat("d", 64)}, nil
}
func (f *fakeLaunchWorktrees) CreateIsolation(_ context.Context, r domain.IsolationCreateRequest) (domain.IsolationCheck, error) {
	f.mu.Lock()
	f.creates++
	c, ok := f.resources[r.Proposal.OperationID]
	if !ok {
		c = domain.IsolationCheck{Proposal: r.Proposal, Worktree: domain.Worktree{ID: "wt-" + r.Proposal.OperationID, RepositoryID: r.Proposal.RepositoryID, Path: r.Proposal.Path, Branch: r.Proposal.Branch, HeadSHA: r.Proposal.BaseSHA}, Exists: true, Owned: true, Clean: true, HeadSHA: r.Proposal.BaseSHA, Fingerprint: strings.Repeat("e", 64), Hooks: "done"}
	}
	if f.hooks != "" {
		c.Hooks = f.hooks
	}
	f.resources[r.Proposal.OperationID] = c
	err, hook := f.createErr, f.afterCreate
	f.mu.Unlock()
	if hook != nil {
		hook()
	}
	return c, err
}
func (f *fakeLaunchWorktrees) InspectIsolation(_ context.Context, p domain.IsolationProposal) (domain.IsolationCheck, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.resources[p.OperationID]
	if !ok {
		c = domain.IsolationCheck{Proposal: p}
	}
	return c, f.inspectErr
}
func (f *fakeLaunchWorktrees) RemoveIsolation(_ context.Context, r domain.IsolationRemoveRequest) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.removes++
	if f.removeErr == nil {
		delete(f.resources, r.Check.Proposal.OperationID)
	}
	return f.removeErr
}
func (f *fakeLaunchWorktrees) InspectPublishedHead(context.Context, domain.IsolationProposal) (domain.PublishedHead, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.head, f.headErr
}
func (f *fakeLaunchWorktrees) PushIsolation(_ context.Context, r domain.IsolationPushRequest) (domain.PublishedHead, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pushes++
	if f.pushErr == nil || f.pushAfterEffect {
		f.head.RemoteSHA = r.HeadSHA
		f.head.RemoteVerified = true
	}
	if f.pushResult != nil {
		f.head = *f.pushResult
	}
	return f.head, f.pushErr
}
func (f *fakeLaunchWorktrees) counts() (int, int, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.creates, f.removes, f.pushes
}

type fakeLaunchHolder struct {
	mu                          sync.Mutex
	starts                      []StartSession
	stops                       []StartIdentity
	observations                map[StartIdentity]SessionObservation
	startErr, queryErr, stopErr error
	startResult                 *SessionObservation
	onStart                     func(StartSession)
}

func (f *fakeLaunchHolder) Start(_ context.Context, r StartSession) (SessionObservation, error) {
	f.mu.Lock()
	f.starts = append(f.starts, fakeLaunchClone(r))
	o := SessionObservation{Identity: r.Identity, State: "running", Alive: true, Acknowledged: true, StartedAt: &fakeLaunchTime}
	if f.startResult != nil {
		o = *f.startResult
		o.Identity = r.Identity
	}
	f.observations[r.Identity] = o
	err, hook := f.startErr, f.onStart
	f.mu.Unlock()
	if hook != nil {
		hook(r)
	}
	return o, err
}
func (f *fakeLaunchHolder) Query(_ context.Context, id StartIdentity) (SessionObservation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.observations[id], f.queryErr
}
func (f *fakeLaunchHolder) Stop(_ context.Context, id StartIdentity) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stops = append(f.stops, id)
	return f.stopErr
}
func (f *fakeLaunchHolder) set(id StartIdentity, o SessionObservation) {
	f.mu.Lock()
	defer f.mu.Unlock()
	o.Identity = id
	f.observations[id] = o
}
func (f *fakeLaunchHolder) calls() []StartSession {
	f.mu.Lock()
	defer f.mu.Unlock()
	return fakeLaunchClone(f.starts)
}

type fakeLaunchRig struct {
	svc       *Service
	deps      Deps
	store     *FileStore
	remote    *fakeLaunchRemote
	accounts  *fakeLaunchAccounts
	worktrees *fakeLaunchWorktrees
	holder    *fakeLaunchHolder
	request   Request
	log       *gardenfake.Fake
	approval  *approve.Service
}

func fakeLaunchApproved(t *testing.T) (RemoteSnapshot, *gardenfake.Fake, *approve.Service) {
	t.Helper()
	ctx := context.Background()
	f := gardenfake.New()
	f.SetAccount("test", "1001")
	target := approve.Target{Repo: 7, Issue: 101, Viewer: "1001", Login: "test"}
	body, err := garden.CaptureBody(target.Viewer, strings.Repeat("a", 32), "Implement project limits.")
	if err != nil {
		t.Fatal(err)
	}
	f.SeedIssue(garden.Issue{Repo: target.Repo, ID: target.Issue, Number: 9, Title: "Project limits", Body: body, Metadata: garden.BodyMetadata(body), Labels: []string{garden.ManagedLabel, garden.FormatTypeLabel(garden.TypeFeature), garden.FormatStageLabel(garden.StageDraft)}})
	ap, err := approve.New(approve.Deps{Reader: f, Writer: f, Now: func() time.Time { return fakeLaunchTime }})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := garden.NewSpecRevision(garden.IssueIdentity{Repo: 7, Issue: 101}, fakeLaunchSpec, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.AppendComment(ctx, 7, 101, raw); err != nil {
		t.Fatal(err)
	}
	p, err := ap.Preview(ctx, target)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ap.Validate(ctx, p); err != nil {
		t.Fatal(err)
	}
	p, err = ap.Preview(ctx, target)
	if err != nil {
		t.Fatal(err)
	}
	spec := plan.SpecRef{RevisionHash: p.Head.Hash, CommentID: p.Head.CommentID, Ordinal: p.Ordinal, Fingerprint: p.Fingerprint}
	text := planrev.Compose(fakeLaunchPlan, spec, plan.Base{Commit: strings.Repeat("c", 40), Branch: "main"})
	raw, err = garden.NewPlanRevision(garden.IssueIdentity{Repo: 7, Issue: 101}, text, nil, spec.Fingerprint)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.AppendComment(ctx, 7, 101, raw); err != nil {
		t.Fatal(err)
	}
	log, err := ap.Read(ctx, target)
	if err != nil {
		t.Fatal(err)
	}
	rv := planrev.Derive(log, target.Viewer)
	if rv.Current == nil {
		t.Fatalf("fixture plan: %+v", rv)
	}
	raw, err = garden.NewPlanDecision(garden.IssueIdentity{Repo: 7, Issue: 101}, rv.Current.Revision, garden.DecisionApproved, "Approve this exact plan.")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.AppendComment(ctx, 7, 101, raw); err != nil {
		t.Fatal(err)
	}
	log, err = ap.Read(ctx, target)
	if err != nil {
		t.Fatal(err)
	}
	rv = planrev.Derive(log, target.Viewer)
	if !rv.Ready {
		t.Fatalf("fixture not Ready: %+v", rv)
	}
	return RemoteSnapshot{Target: target, Local: "project-v1-launch-test", Epoch: 1, Issue: log.Issue, FullName: "owner/repo", Review: rv, Live: true, VerifiedAt: fakeLaunchTime}, f, ap
}
func newFakeLaunchRig(t *testing.T) *fakeLaunchRig {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	snap, log, ap := fakeLaunchApproved(t)
	r := &fakeLaunchRig{store: &FileStore{Path: filepath.Join(t.TempDir(), "launch.json")}, remote: &fakeLaunchRemote{snapshot: snap}, accounts: &fakeLaunchAccounts{resolution: agents.LaunchResolution{AccountID: "acct_test", AccountName: "personal", Provider: "codex", Launch: agents.LaunchOptions{Model: "gpt-6.1-sol", Effort: "high", Sandbox: "workspace-write", Approval: "on-request"}, Inherited: map[string]bool{"model": true, "effort": true, "sandbox": true, "approval": true}}}, worktrees: &fakeLaunchWorktrees{root: t.TempDir(), resources: map[string]domain.IsolationCheck{}}, holder: &fakeLaunchHolder{observations: map[StartIdentity]SessionObservation{}}, log: log, approval: ap}
	r.deps = Deps{Remote: r.remote, Accounts: r.accounts, Worktrees: r.worktrees, Holder: r.holder, Sessions: r.holder, Store: r.store, Clock: fakeLaunchClock{}, IDs: &fakeLaunchIDs{}, Local: snap.Local, StartTimeout: 20 * time.Millisecond, QueryTimeout: 20 * time.Millisecond}
	var err error
	r.svc, err = New(r.deps)
	if err != nil {
		t.Fatal(err)
	}
	r.request = Request{Target: snap.Target, Branch: "bonsai/9-" + snap.Review.Approved.Hash[:8], BaseRef: "main", RemoteName: "origin", AccountID: "acct_test", Name: "Implement #9", Cols: 100, Rows: 25}
	return r
}
func (r *fakeLaunchRig) preview(t *testing.T) Preview {
	t.Helper()
	p, err := r.svc.Preview(context.Background(), r.request)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func (r *fakeLaunchRig) start(t *testing.T) Attempt {
	t.Helper()
	a, err := r.svc.Start(context.Background(), StartRequest{Preview: r.preview(t), Confirmed: true, EscalationConfirmed: true})
	if err != nil {
		t.Fatal(err)
	}
	return a
}
func (r *fakeLaunchRig) noEffects(t *testing.T) {
	t.Helper()
	c, d, p := r.worktrees.counts()
	if c+d+p+len(r.holder.calls())+len(r.remote.mirrors) != 0 {
		t.Fatalf("unexpected effects: worktree=%d remove=%d push=%d holder=%d mirror=%d", c, d, p, len(r.holder.calls()), len(r.remote.mirrors))
	}
	as, err := r.store.List(context.Background(), r.deps.Local, r.request.Target.Issue)
	if err != nil {
		t.Fatal(err)
	}
	if len(as) != 0 {
		t.Fatalf("refusal reserved %+v", as)
	}
}

func TestLaunchExactApprovedInputsAndFrozenNativeOptions(t *testing.T) {
	r := newFakeLaunchRig(t)
	p := r.preview(t)
	rv := r.remote.snapshot.Review
	if p.ID == "" || p.ID != p.Proposal.OperationID || p.Inputs.Spec.RevisionHash != rv.Gate.Head.Hash || p.Inputs.Spec.CommentID != rv.Gate.Head.CommentID || p.Inputs.SpecApprovalID != rv.Gate.Approval.CommentID || p.Inputs.Plan.RevisionHash != rv.Approved.Hash || p.Inputs.Plan.CommentID != rv.Approved.CommentID || p.Inputs.Plan.DecisionID != rv.Approved.Decisions[len(rv.Approved.Decisions)-1].CommentID || p.Inputs.Plan.BaseCommit != strings.Repeat("c", 40) {
		t.Fatalf("lost exact approved refs: %+v", p)
	}
	r.noEffects(t)
	if _, err := os.Stat(r.store.Path); !os.IsNotExist(err) {
		t.Fatalf("Preview wrote execution store: %v", err)
	}
	a, err := r.svc.Start(context.Background(), StartRequest{Preview: p, Confirmed: true})
	if err != nil {
		t.Fatal(err)
	}
	calls := r.holder.calls()
	if len(calls) != 1 {
		t.Fatalf("starts=%d", len(calls))
	}
	s := calls[0]
	if a.State != StateRunning || a.Inputs != p.Inputs || a.ActorID != "1001" || a.PR.State != PRNotCreated || s.Identity.SessionID != a.SessionID || s.Identity.OperationID != a.ID+"/start/1" || s.Task.AttemptID != a.ID || s.Task.Role != "implementation" || s.Task.RepositoryID != 7 || s.Task.IssueID != 101 || s.Dir != p.Proposal.Path || s.WorktreeID == "" || s.ProjectID == "" || s.AccountID != r.request.AccountID || s.Cols != 100 || s.Rows != 25 {
		t.Fatalf("attempt=%+v start=%+v", a, s)
	}
	if s.Launch.Model != "gpt-6.1-sol" || s.Launch.Effort != "high" || s.Launch.Sandbox != "workspace-write" || s.Launch.Approval != "on-request" || s.Launch.ReadOnly {
		t.Fatalf("native options changed: %+v", s.Launch)
	}
	for _, text := range []string{fakeLaunchSpec, rv.Approved.Content, p.Inputs.Spec.RevisionHash, p.Inputs.Plan.RevisionHash, p.Proposal.BaseSHA, p.Proposal.Path, p.Proposal.Branch, a.ID, "human review"} {
		if !strings.Contains(s.Launch.Prompt, text) {
			t.Errorf("prompt lacks %q", text)
		}
	}
	disk, err := os.ReadFile(r.store.Path)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{fakeLaunchSpec, fakeLaunchPlan, s.Launch.Prompt, `"prompt"`, `"argv"`, `"env"`} {
		if strings.Contains(string(disk), secret) {
			t.Errorf("persisted sensitive content %q", secret)
		}
	}
}
func TestLaunchNewRequiresExplicitDependencies(t *testing.T) {
	r := newFakeLaunchRig(t)
	for _, tc := range []struct {
		name   string
		change func(*Deps)
	}{{"remote", func(d *Deps) { d.Remote = nil }}, {"accounts", func(d *Deps) { d.Accounts = nil }}, {"worktrees", func(d *Deps) { d.Worktrees = nil }}, {"holder", func(d *Deps) { d.Holder = nil }}, {"sessions", func(d *Deps) { d.Sessions = nil }}, {"store", func(d *Deps) { d.Store = nil }}, {"clock", func(d *Deps) { d.Clock = nil }}, {"ids", func(d *Deps) { d.IDs = nil }}, {"local", func(d *Deps) { d.Local = "" }}, {"timeout", func(d *Deps) { d.StartTimeout = -1 }}, {"query timeout", func(d *Deps) { d.QueryTimeout = -1 }}} {
		t.Run(tc.name, func(t *testing.T) {
			d := r.deps
			tc.change(&d)
			if _, err := New(d); !errors.Is(err, ErrInvalid) {
				t.Fatalf("err=%v", err)
			}
		})
	}
}
func TestLaunchPreflightOrderedRefusals(t *testing.T) {
	cases := []struct {
		name        string
		change      func(*fakeLaunchRig, *Preview)
		want, cause error
	}{
		{"malformed dimensions", func(r *fakeLaunchRig, p *Preview) { p.Request.Cols = 501 }, ErrInvalid, nil},
		{"identity mismatch", func(r *fakeLaunchRig, p *Preview) { p.Proposal.OperationID = "impl-other" }, ErrInvalid, nil},
		{"prompt injection", func(r *fakeLaunchRig, p *Preview) { p.Request.Launch.Prompt = "run arbitrary work" }, ErrInvalid, nil},
		{"display name override", func(r *fakeLaunchRig, p *Preview) { p.Request.Launch.DisplayName = "other" }, ErrInvalid, nil},
		{"readonly override", func(r *fakeLaunchRig, p *Preview) { p.Request.Launch.ReadOnly = true }, ErrInvalid, nil},
		{"actor before access", func(r *fakeLaunchRig, p *Preview) {
			r.remote.snapshot.Target.Viewer = "2002"
			r.remote.snapshot.Target.ReadOnly = true
		}, ErrOwnership, nil},
		{"local association", func(r *fakeLaunchRig, p *Preview) { r.remote.snapshot.Local = "other" }, ErrOwnership, nil},
		{"epoch", func(r *fakeLaunchRig, p *Preview) { r.remote.snapshot.Epoch++ }, ErrOwnership, nil},
		{"Garden identity", func(r *fakeLaunchRig, p *Preview) { r.remote.snapshot.Target.Repo++ }, ErrOwnership, nil},
		{"competing heads", func(r *fakeLaunchRig, p *Preview) {
			r.remote.snapshot.Review.Competing = true
			r.remote.snapshot.Review.Ready = false
		}, ErrOwnership, nil},
		{"write access", func(r *fakeLaunchRig, p *Preview) { r.remote.snapshot.Target.ReadOnly = true }, ErrAccess, nil},
		{"offline", func(r *fakeLaunchRig, p *Preview) { r.remote.verifyErr = garden.ErrOffline }, ErrRemoteStale, garden.ErrOffline},
		{"stale snapshot", func(r *fakeLaunchRig, p *Preview) { r.remote.snapshot.Live = false }, ErrRemoteStale, nil},
		{"changed spec before missing approval", func(r *fakeLaunchRig, p *Preview) {
			r.remote.snapshot.Review.Gate.Head.Hash = strings.Repeat("f", 64)
			r.remote.snapshot.Review.Gate.Approval = nil
		}, ErrSpecChanged, nil},
		{"changed plan", func(r *fakeLaunchRig, p *Preview) { r.remote.snapshot.Review.Approved.Hash = strings.Repeat("f", 64) }, ErrPlanChanged, nil},
		{"revoked approval", func(r *fakeLaunchRig, p *Preview) {
			r.remote.snapshot.Review.Ready = false
			r.remote.snapshot.Review.Gate.Approval = nil
		}, ErrApprovalMissing, nil},
		{"missing plan decision", func(r *fakeLaunchRig, p *Preview) {
			r.remote.snapshot.Review.Ready = false
			r.remote.snapshot.Review.Approved.Decisions = nil
		}, ErrApprovalMissing, nil},
		{"new approval same heads", func(r *fakeLaunchRig, p *Preview) { r.remote.snapshot.Review.Gate.Approval.CommentID++ }, ErrInputsChanged, nil},
		{"missing account", func(r *fakeLaunchRig, p *Preview) { r.accounts.err = agents.ErrAccountNotFound }, ErrAccountUnavailable, agents.ErrAccountNotFound},
		{"account busy", func(r *fakeLaunchRig, p *Preview) { r.accounts.err = agents.ErrAccountBusy }, ErrAccountUnavailable, agents.ErrAccountBusy},
		{"incomplete native pair", func(r *fakeLaunchRig, p *Preview) { r.accounts.resolution.Launch.Approval = "" }, ErrPermissionsUnresolved, nil},
		{"cross provider field", func(r *fakeLaunchRig, p *Preview) { r.accounts.resolution.Launch.PermissionMode = "default" }, ErrPermissionsUnsupported, agents.ErrInvalidLaunch},
		{"changed model", func(r *fakeLaunchRig, p *Preview) { r.accounts.resolution.Launch.Model = "changed" }, ErrInputsChanged, nil},
		{"changed account name", func(r *fakeLaunchRig, p *Preview) { r.accounts.resolution.AccountName = "different" }, ErrInputsChanged, nil},
		{"base moved", func(r *fakeLaunchRig, p *Preview) { r.worktrees.previewErr = domain.E("base_changed", "safe") }, ErrBaseChanged, nil},
		{"ownership before collisions", func(r *fakeLaunchRig, p *Preview) { r.worktrees.previewErr = domain.E("ownership", "safe") }, ErrOwnership, nil},
		{"branch collision", func(r *fakeLaunchRig, p *Preview) { r.worktrees.previewErr = domain.E("branch_collision", "safe") }, ErrBranchCollision, nil},
		{"path collision", func(r *fakeLaunchRig, p *Preview) { r.worktrees.previewErr = domain.E("path_collision", "safe") }, ErrPathCollision, nil},
		{"placement changed", func(r *fakeLaunchRig, p *Preview) { r.worktrees.previewErr = domain.E("inputs_changed", "safe") }, ErrInputsChanged, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newFakeLaunchRig(t)
			p := r.preview(t)
			tc.change(r, &p)
			a, err := r.svc.Start(context.Background(), StartRequest{Preview: p, Confirmed: true})
			if !errors.Is(err, tc.want) || (tc.cause != nil && !errors.Is(err, tc.cause)) || !reflect.DeepEqual(a, Attempt{}) {
				t.Fatalf("attempt=%+v err=%v want=%v cause=%v", a, err, tc.want, tc.cause)
			}
			r.noEffects(t)
		})
	}
}
func TestLaunchLabelsAndCommentsNeverAuthorize(t *testing.T) {
	for _, mode := range []string{"label only", "prose only", "unapproved plan"} {
		t.Run(mode, func(t *testing.T) {
			r := newFakeLaunchRig(t)
			s := r.remote.snapshot
			s.Review.Ready = false
			s.Review.Approved = nil
			if mode != "unapproved plan" {
				s.Review.Gate.Approval = nil
			}
			r.remote.snapshot = s
			p, err := r.svc.Preview(context.Background(), r.request)
			if !errors.Is(err, ErrApprovalMissing) || !reflect.DeepEqual(p, Preview{}) {
				t.Fatalf("preview=%+v err=%v", p, err)
			}
			r.noEffects(t)
		})
	}
}
func TestLaunchConfirmationAndInheritedEscalationAreSeparate(t *testing.T) {
	for _, provider := range []string{"codex", "claude", "antigravity"} {
		t.Run(provider, func(t *testing.T) {
			r := newFakeLaunchRig(t)
			yes := true
			r.accounts.resolution.Provider = agents.ProviderID(provider)
			switch provider {
			case "codex":
				r.accounts.resolution.Launch = agents.LaunchOptions{Sandbox: "bypass"}
			case "claude":
				r.accounts.resolution.Launch = agents.LaunchOptions{PermissionMode: "bypassPermissions"}
			case "antigravity":
				r.accounts.resolution.Launch = agents.LaunchOptions{FullAccess: &yes}
			}
			p := r.preview(t)
			if !p.Escalated {
				t.Fatal("inherited escalation hidden")
			}
			for _, req := range []StartRequest{{Preview: p}, {Preview: p, Confirmed: true}} {
				a, err := r.svc.Start(context.Background(), req)
				want := ErrNotAccepted
				if req.Confirmed {
					want = ErrEscalationNotAccepted
				}
				if !errors.Is(err, want) || a.ID != "" {
					t.Fatalf("attempt=%+v err=%v", a, err)
				}
				r.noEffects(t)
			}
		})
	}
}
func TestLaunchOversizePromptIsNeverTruncated(t *testing.T) {
	r := newFakeLaunchRig(t)
	r.remote.snapshot.Review.Approved.Content = strings.Repeat("x", MaxPromptBytes+1)
	p, err := r.svc.Preview(context.Background(), r.request)
	if !errors.Is(err, ErrTooLarge) || p.ID != "" {
		t.Fatalf("preview=%+v err=%v", p, err)
	}
	r.noEffects(t)
}

func TestLaunchMalformedRequestsRefuseBeforeRemoteOrEffects(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*Request)
	}{
		{"empty branch", func(r *Request) { r.Branch = "" }}, {"shell branch", func(r *Request) { r.Branch = "topic;echo injected" }},
		{"empty base", func(r *Request) { r.BaseRef = "" }}, {"empty remote", func(r *Request) { r.RemoteName = "" }},
		{"empty account", func(r *Request) { r.AccountID = "" }}, {"empty name", func(r *Request) { r.Name = "" }},
		{"zero columns", func(r *Request) { r.Cols = 0 }}, {"negative rows", func(r *Request) { r.Rows = -1 }},
		{"missing actor", func(r *Request) { r.Target.Viewer = "" }}, {"missing issue", func(r *Request) { r.Target.Issue = 0 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newFakeLaunchRig(t)
			req := r.request
			tc.change(&req)
			p, err := r.svc.Preview(context.Background(), req)
			if !errors.Is(err, ErrInvalid) || !reflect.DeepEqual(p, Preview{}) {
				t.Fatalf("preview=%+v err=%v", p, err)
			}
			r.noEffects(t)
		})
	}
}

func TestLaunchVerifiedLogChangesCannotReuseOriginalApproval(t *testing.T) {
	for _, mode := range []string{"revoked spec approval", "deleted plan decision", "competing spec heads", "competing plan heads", "metadata owner", "conflicting managed labels"} {
		t.Run(mode, func(t *testing.T) {
			r := newFakeLaunchRig(t)
			p := r.preview(t)
			target := r.request.Target
			want := ErrApprovalMissing
			switch mode {
			case "revoked spec approval":
				raw, err := garden.EncodeCommentEnvelope(garden.CommentEnvelope{Kind: garden.CommentRevocation, RepoID: target.Repo, IssueID: target.Issue, TargetCommentID: p.Inputs.SpecApprovalID}, "Revoked.")
				if err != nil {
					t.Fatal(err)
				}
				r.log.AppendCommentAs(target.Repo, target.Issue, target.Login, target.Viewer, raw)
			case "deleted plan decision":
				if !r.log.DeleteComment(target.Repo, target.Issue, p.Inputs.Plan.DecisionID) {
					t.Fatal("fixture decision not found")
				}
			case "competing spec heads":
				raw, err := garden.NewSpecRevision(garden.IssueIdentity{Repo: target.Repo, Issue: target.Issue}, fakeLaunchSpec+"\nIndependent head.", nil)
				if err != nil {
					t.Fatal(err)
				}
				r.log.AppendCommentAs(target.Repo, target.Issue, target.Login, target.Viewer, raw)
				want = ErrOwnership
			case "competing plan heads":
				old := r.remote.snapshot.Review.Approved
				raw, err := garden.NewPlanRevision(garden.IssueIdentity{Repo: target.Repo, Issue: target.Issue}, old.Content+"\nIndependent plan.", nil, p.Inputs.Spec.Fingerprint)
				if err != nil {
					t.Fatal(err)
				}
				r.log.AppendCommentAs(target.Repo, target.Issue, target.Login, target.Viewer, raw)
				want = ErrOwnership
			case "metadata owner":
				issue, err := r.log.GetIssue(context.Background(), target.Repo, target.Issue)
				if err != nil {
					t.Fatal(err)
				}
				meta, err := garden.DecodeMetadata(issue.Metadata)
				if err != nil {
					t.Fatal(err)
				}
				meta.Owner = "2002"
				issue.Metadata, err = garden.EncodeMetadata(meta)
				if err != nil {
					t.Fatal(err)
				}
				r.log.SeedIssue(issue)
				want = ErrOwnership
			case "conflicting managed labels":
				issue, err := r.log.GetIssue(context.Background(), target.Repo, target.Issue)
				if err != nil {
					t.Fatal(err)
				}
				issue.Labels = append(issue.Labels, garden.FormatStageLabel(garden.StageDone))
				r.log.SeedIssue(issue)
				want = ErrOwnership
			}
			log, err := r.approval.Read(context.Background(), target)
			if err != nil {
				t.Fatal(err)
			}
			r.remote.snapshot.Issue = log.Issue
			r.remote.snapshot.Review = planrev.Derive(log, target.Viewer)
			a, err := r.svc.Start(context.Background(), StartRequest{Preview: p, Confirmed: true})
			if !errors.Is(err, want) || a.ID != "" {
				t.Fatalf("changed log accepted: %+v err=%v want=%v", a, err, want)
			}
			r.noEffects(t)
		})
	}
}

func fakeLaunchReceive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for launch synchronization")
		var zero T
		return zero
	}
}
