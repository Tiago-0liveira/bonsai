//go:build !windows

package agentsbackend

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/agenthost"
	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
)

type fakeLaunchResolvingProvider struct {
	*testProvider
	available  bool
	prepares   int
	resolveErr error
}

func (p *fakeLaunchResolvingProvider) Capabilities() agents.Capabilities {
	return agents.Capabilities{Interactive: true, ConcurrentSameAccount: true, ConcurrentCrossAccount: true}
}
func (p *fakeLaunchResolvingProvider) Availability(context.Context) agents.Availability {
	return agents.Availability{Available: p.available, Reason: "fixture unavailable"}
}
func (p *fakeLaunchResolvingProvider) ResolveLaunch(_ agents.Account, l agents.LaunchOptions) (agents.LaunchOptions, error) {
	if p.resolveErr != nil {
		return agents.LaunchOptions{}, p.resolveErr
	}
	if l.Model == "" {
		l.Model = "static"
	}
	if l.PermissionMode == "" {
		l.PermissionMode = "default"
	}
	return l, nil
}
func (p *fakeLaunchResolvingProvider) DescribeAccount(context.Context, agents.Account) agents.AccountInfo {
	return agents.AccountInfo{Options: map[string]any{"auth_status": "logged_in"}}
}
func (p *fakeLaunchResolvingProvider) PrepareSession(context.Context, agents.PrepareSessionRequest) (agents.PreparedSession, error) {
	p.prepares++
	return agents.PreparedSession{}, errors.New("must not start during resolution")
}
func TestGardenLaunchResolverUsesOptionalProviderWithoutStartingOrRefreshing(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	home := os.Getenv("HOME")
	f := newFixture(t)
	p := &fakeLaunchResolvingProvider{testProvider: f.provider, available: true}
	registry := agents.NewRegistry()
	if err := registry.Register(p); err != nil {
		t.Fatal(err)
	}
	f.acc.rt.Registry = registry
	resolver, ok := any(f.acc).(agentsapi.LaunchResolver)
	if !ok {
		t.Fatal("accounts does not expose optional launch resolver")
	}
	got, err := resolver.ResolveLaunch(context.Background(), f.account.ID, agents.LaunchOptions{})
	if err != nil || got.AccountID != f.account.ID || got.AccountName != f.account.Name || got.Provider != f.account.Provider || got.Launch.Model != "static" || got.Launch.PermissionMode != "default" {
		t.Fatalf("resolution=%+v %v", got, err)
	}
	if p.prepares != 0 || p.calls != 0 {
		t.Fatalf("resolution started provider/refreshed usage: prepares=%d usage=%d", p.prepares, p.calls)
	}
	if os.Getenv("HOME") != home {
		t.Fatal("HOME replaced")
	}
	got.Inherited["model"] = false
	again, err := resolver.ResolveLaunch(context.Background(), f.account.ID, agents.LaunchOptions{})
	if err != nil || !again.Inherited["model"] {
		t.Fatalf("resolution aliased defaults: %+v %v", again, err)
	}
}
func TestGardenLaunchResolverUnavailableAndInvalidOptionsAreSafe(t *testing.T) {
	f := newFixture(t)
	p := &fakeLaunchResolvingProvider{testProvider: f.provider, available: false}
	registry := agents.NewRegistry()
	if err := registry.Register(p); err != nil {
		t.Fatal(err)
	}
	f.acc.rt.Registry = registry
	resolver, ok := any(f.acc).(agentsapi.LaunchResolver)
	if !ok {
		t.Fatal("resolver unavailable")
	}
	if _, err := resolver.ResolveLaunch(context.Background(), f.account.ID, agents.LaunchOptions{}); err == nil {
		t.Fatal("missing binary accepted")
	}
	p.available = true
	p.resolveErr = errors.Join(agents.ErrInvalidLaunch, errors.New("token=ghp_fixture_secret"))
	_, err := resolver.ResolveLaunch(context.Background(), f.account.ID, agents.LaunchOptions{Model: "bad"})
	if !errors.Is(err, agents.ErrInvalidLaunch) || strings.Contains(err.Error(), "ghp_fixture_secret") {
		t.Fatalf("unsafe invalid option error=%v", err)
	}
	if p.prepares != 0 {
		t.Fatal("unavailable account started")
	}
}
func TestGardenLaunchOperationSessionsPreserveTaskOriginAndConflicts(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	e := newEnv(t, "cat")
	ops, ok := e.api.Sessions.(agentsapi.OperationSessions)
	if !ok {
		t.Fatal("sessions lacks operation interface")
	}
	req := agentsapi.OperationStartRequest{StartRequest: agentsapi.StartRequest{ProjectID: "project", WorktreeID: "tree", WorkDir: t.TempDir(), AccountID: e.account, Name: "Implement #9", Cols: 80, Rows: 24}, OperationID: "impl-backend/start/1", SessionID: "sess_123abc", Task: agents.TaskRef{RepositoryID: 7, IssueID: 101, AttemptID: "impl-backend", Role: "implementation"}}
	result, err := ops.StartOperation(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	s := result.Session
	if s.ID != string(req.SessionID) || s.OperationID != req.OperationID || s.Task == nil || *s.Task != req.Task || s.Origin != "tui" || s.WorkDir != req.WorkDir {
		t.Fatalf("identity lost=%+v", s)
	}
	list, err := e.api.Sessions.List(context.Background())
	if err != nil || len(list) != 1 || list[0].Task == nil || list[0].OperationID != req.OperationID {
		t.Fatalf("list=%+v %v", list, err)
	}
	req.Task.IssueID++
	if _, err = ops.StartOperation(context.Background(), req); !errors.Is(err, agenthost.ErrOperationConflict) {
		t.Fatalf("foreign operation accepted: %v", err)
	}
	req.Task.IssueID--
	if _, err = ops.QueryOperation(context.Background(), req.SessionID, "other/start/1"); !errors.Is(err, agenthost.ErrOperationConflict) {
		t.Fatalf("wrong operation query=%v", err)
	}
}
