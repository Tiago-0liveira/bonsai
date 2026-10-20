package gardenbackend

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	gardenfake "github.com/Tiago-0liveira/bonsai/internal/core/garden/fake"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/plan"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
	agentfake "github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi/fake"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
)

type stubCheckout struct{ base plan.Base }

func (s *stubCheckout) Inspect(context.Context) (plan.Base, error) { return s.base, nil }

type stubAgents struct {
	mu      sync.Mutex
	started []plan.StartSession
	stopped []string
	status  map[string]plan.SessionStatus
}

func (a *stubAgents) Account(_ context.Context, id agents.AccountID) (plan.Account, error) {
	switch id {
	case "claude-acct":
		return plan.Account{ID: id, Name: "work", Provider: "claude", ReadOnly: agents.ReadOnlySupport{Supported: true, Mechanism: "tool allowlist", Allows: []string{"read files"}}}, nil
	case "agy-acct":
		return plan.Account{ID: id, Name: "agy", Provider: "antigravity", ReadOnly: agents.ReadOnlySupport{Reason: "no sandbox"}}, nil
	}
	return plan.Account{}, agents.ErrAccountNotFound
}

func (a *stubAgents) Start(_ context.Context, s plan.StartSession) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.started = append(a.started, s)
	return "sess-1", nil
}

func (a *stubAgents) Stop(_ context.Context, id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.stopped = append(a.stopped, id)
	return nil
}

func (a *stubAgents) Status(_ context.Context, id string) (plan.SessionStatus, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if st, ok := a.status[id]; ok {
		return st, nil
	}
	return plan.SessionStatus{}, plan.ErrSessionGone
}

func plannerHarness(t *testing.T) (*harness, *gardenfake.Fake, uint64, *stubAgents, *stubCheckout) {
	t.Helper()
	h, f, id := validateHarness(t)
	ag := &stubAgents{status: map[string]plan.SessionStatus{}}
	co := &stubCheckout{base: plan.Base{Commit: "c0ffee"}}
	h.b.deps.Plans = &plan.Store{Path: filepath.Join(t.TempDir(), "planner.json")}
	h.b.deps.Checkout = co
	h.b.deps.Agents = ag
	return h, f, id, ag, co
}

func approveIssue(t *testing.T, h *harness, id uint64) {
	t.Helper()
	p, err := h.b.PreviewValidate(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if v, err := h.b.Validate(context.Background(), p); err != nil || v.State != gardenapi.ValidateDone {
		t.Fatalf("validate = %+v, %v", v, err)
	}
}

func code(err error) gardenapi.Code {
	var ae *gardenapi.Error
	if errors.As(err, &ae) {
		return ae.Code
	}
	return ""
}

func startReq(p gardenapi.PlannerPreviewView, account agents.AccountID) gardenapi.PlannerStartRequest {
	return gardenapi.PlannerStartRequest{Preview: p, AccountID: account, Model: "opus", AcceptReadOnly: true, Cols: 100, Rows: 30}
}

func TestPlannerIsOfferedOnlyAfterGateOne(t *testing.T) {
	h, f, id, ag, _ := plannerHarness(t)
	ctx := context.Background()
	before := len(f.WriteLog())
	p, err := h.b.PreviewPlanner(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if p.CanRequest() || p.Unavailable == "" {
		t.Fatalf("a draft was offered a planner: %+v", p)
	}
	if _, err := h.b.StartPlanner(ctx, startReq(p, "claude-acct")); code(err) != gardenapi.CodeNotApproved {
		t.Fatalf("StartPlanner on a draft = %v", err)
	}
	if len(ag.started) != 0 || len(f.WriteLog()) != before {
		t.Fatalf("a draft started %d sessions / wrote %v", len(ag.started), f.WriteLog()[before:])
	}
	approveIssue(t, h, id)
	p, err = h.b.PreviewPlanner(ctx, id)
	if err != nil || !p.CanRequest() || p.Spec.RevisionHash == "" || p.Spec.Fingerprint != p.Gate.Fingerprint || p.Base.Commit != "c0ffee" || p.Actor != viewerName {
		t.Fatalf("approved preview = %+v, %v", p, err)
	}
	if len(ag.started) != 0 {
		t.Fatal("previewing started a planner")
	}
}

func TestStartPlannerBindsAttemptAndNeverLaunchesUnrestricted(t *testing.T) {
	h, f, id, ag, _ := plannerHarness(t)
	ctx := context.Background()
	approveIssue(t, h, id)
	writes := len(f.WriteLog())
	p, _ := h.b.PreviewPlanner(ctx, id)

	if _, err := h.b.StartPlanner(ctx, startReq(p, "agy-acct")); code(err) != gardenapi.CodeReadOnlyUnsupported {
		t.Fatalf("unsupported provider = %v", err)
	}
	if len(ag.started) != 0 {
		t.Fatal("an unsupported provider was started")
	}
	v, err := h.b.StartPlanner(ctx, startReq(p, "claude-acct"))
	if err != nil {
		t.Fatal(err)
	}
	a := v.Attempt
	if a.Role != plan.RolePlanner || a.Spec != p.Spec || a.Base.Commit != "c0ffee" || a.SessionID != "sess-1" || a.Provider != "claude" || a.State != plan.StateStarting {
		t.Fatalf("attempt = %+v", a)
	}
	if len(ag.started) != 1 || !ag.started[0].Launch.ReadOnly || ag.started[0].Dir != h.b.dir {
		t.Fatalf("session = %+v", ag.started)
	}
	if len(f.WriteLog()) != writes {
		t.Fatalf("planning wrote to GitHub: %v", f.WriteLog()[writes:])
	}
	// The explicit action is the only thing that starts a planner: reading again does not.
	for i := 0; i < 3; i++ {
		if _, err := h.b.PreviewPlanner(ctx, id); err != nil {
			t.Fatal(err)
		}
		if _, err := h.b.PlannerAttempts(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	if len(ag.started) != 1 {
		t.Fatalf("reading started %d extra planners", len(ag.started)-1)
	}
}

func TestPlannerRefusesChangedSpecificationAndCheckout(t *testing.T) {
	h, _, id, ag, co := plannerHarness(t)
	ctx := context.Background()
	approveIssue(t, h, id)
	p, _ := h.b.PreviewPlanner(ctx, id)

	co.base = plan.Base{Commit: "other"}
	if _, err := h.b.StartPlanner(ctx, startReq(p, "claude-acct")); code(err) != gardenapi.CodeRepoChanged {
		t.Fatalf("moved checkout = %v", err)
	}
	co.base = plan.Base{Commit: "c0ffee"}

	d, _ := h.b.Document(ctx, id)
	if _, sv, err := save(t, h, d, completeSpec+"\n\n## Constraints\nNo daemon."); err != nil || sv.State != gardenapi.SaveDone {
		t.Fatalf("second revision: %+v %v", sv, err)
	}
	_, err := h.b.StartPlanner(ctx, startReq(p, "claude-acct"))
	if c := code(err); c != gardenapi.CodeNotApproved && c != gardenapi.CodeSpecChanged {
		t.Fatalf("an edited specification = %v", err)
	}
	if len(ag.started) != 0 {
		t.Fatal("a planner started for a changed specification or checkout")
	}
}

func TestPlannerAttemptsFollowTheSessionAndCancelKeepsHistory(t *testing.T) {
	h, _, id, ag, _ := plannerHarness(t)
	ctx := context.Background()
	approveIssue(t, h, id)
	p, _ := h.b.PreviewPlanner(ctx, id)
	v, err := h.b.StartPlanner(ctx, startReq(p, "claude-acct"))
	if err != nil {
		t.Fatal(err)
	}
	ag.status["sess-1"] = plan.SessionStatus{State: "starting"}
	if again, err := h.b.PreviewPlanner(ctx, id); err != nil || again.CanRequest() {
		t.Fatalf("a live attempt must block another: %+v, %v", again, err)
	}
	ag.status["sess-1"] = plan.SessionStatus{State: "running"}
	list, err := h.b.PlannerAttempts(ctx, id)
	if err != nil || len(list.Attempts) != 1 || list.Attempts[0].State != plan.StateRunning {
		t.Fatalf("attempts = %+v, %v", list, err)
	}
	if _, err := h.b.CancelPlanner(ctx, v.Attempt.ID); err != nil {
		t.Fatal(err)
	}
	if len(ag.stopped) != 1 || ag.stopped[0] != "sess-1" {
		t.Fatalf("stopped = %v", ag.stopped)
	}
	ag.status["sess-1"] = plan.SessionStatus{State: "exited", ExitCode: new(int)}
	list, _ = h.b.PlannerAttempts(ctx, id)
	if list.Attempts[0].State != plan.StateCancelled || list.Attempts[0].SessionID != "sess-1" {
		t.Fatalf("cancelled history = %+v", list.Attempts[0])
	}
	// Retry is another explicit attempt; the cancelled one stays.
	p2, _ := h.b.PreviewPlanner(ctx, id)
	if !p2.CanRequest() || len(p2.Attempts) != 1 {
		t.Fatalf("retry preview = %+v", p2)
	}
	if _, err := h.b.StartPlanner(ctx, startReq(p2, "claude-acct")); err != nil {
		t.Fatal(err)
	}
	list, _ = h.b.PlannerAttempts(ctx, id)
	if len(list.Attempts) != 2 || list.Attempts[0].State != plan.StateCancelled {
		t.Fatalf("history = %+v", list.Attempts)
	}
}

func TestPlannerUnavailableWithoutRuntime(t *testing.T) {
	h, _, id := validateHarness(t)
	_, err := h.b.PreviewPlanner(context.Background(), id)
	if code(err) != gardenapi.CodeFailed || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("err = %v", err)
	}
}

func TestPlannerAttemptRecordHoldsNoSpecText(t *testing.T) {
	h, _, id, _, _ := plannerHarness(t)
	ctx := context.Background()
	approveIssue(t, h, id)
	p, _ := h.b.PreviewPlanner(ctx, id)
	if _, err := h.b.StartPlanner(ctx, startReq(p, "claude-acct")); err != nil {
		t.Fatal(err)
	}
	raw, err := readFile(h.b.deps.Plans.Path)
	if err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{"Cap spend", "monthly ceiling", "token", "prompt"} {
		if strings.Contains(raw, leak) {
			t.Errorf("persisted attempt contains %q", leak)
		}
	}
}

func readFile(p string) (string, error) {
	b, err := os.ReadFile(p)
	return string(b), err
}

func TestPlannerAgentsAdapterStartsNoWorktreeAndMapsStatus(t *testing.T) {
	fb := agentfake.New()
	fb.Accounts.Items = []agentsapi.Account{{Account: agents.Account{ID: "c1", Name: "work", Provider: "claude"}, ReadOnly: agents.ReadOnlySupport{Supported: true, Mechanism: "tool allowlist"}}}
	pa := plannerAgents{api: fb.API(), dir: t.TempDir()}
	ctx := context.Background()
	acct, err := pa.Account(ctx, "c1")
	if err != nil || !acct.ReadOnly.Supported || acct.Provider != "claude" || acct.Name != "work" {
		t.Fatalf("account = %+v, %v", acct, err)
	}
	if _, err := pa.Account(ctx, "missing"); !errors.Is(err, agents.ErrAccountNotFound) {
		t.Fatalf("missing account = %v", err)
	}
	id, err := pa.Start(ctx, plan.StartSession{AccountID: "c1", Dir: pa.dir, Name: "Plan #1", Cols: 90, Rows: 20, Launch: agents.LaunchOptions{ReadOnly: true, Model: "opus"}})
	if err != nil {
		t.Fatal(err)
	}
	starts := fb.Sessions.Starts()
	if len(starts) != 1 || starts[0].WorktreeID != "" || starts[0].WorkDir != pa.dir || !starts[0].Launch.ReadOnly || starts[0].ProjectID == "" {
		t.Fatalf("starts = %+v", starts)
	}
	st, err := pa.Status(ctx, id)
	if err != nil || st.State != "running" {
		t.Fatalf("status = %+v, %v", st, err)
	}
	if err := pa.Stop(ctx, id); err != nil {
		t.Fatal(err)
	}
	if st, _ := pa.Status(ctx, id); st.State != "exited" {
		t.Fatalf("after stop = %+v", st)
	}
	if _, err := pa.Status(ctx, "gone"); !errors.Is(err, plan.ErrSessionGone) {
		t.Fatalf("missing session = %v", err)
	}
	fb.Sessions.Supports = false
	if _, err := pa.Start(ctx, plan.StartSession{AccountID: "c1"}); code(err) != gardenapi.CodeFailed {
		t.Fatalf("unsupported host = %v", err)
	}
}
