//go:build !windows

package agentsbackend

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/agentruntime"
	"github.com/Tiago-0liveira/bonsai/internal/core/agenthost"
	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/agentterminal"
)

const secretPath = "/home/someone/.secret-dir/token.json"

// testProvider implements every optional interface the adapter reads.
type testProvider struct {
	usageErr  error
	usage     agents.UsageSnapshot
	modelErr  error
	updateErr error
	values    map[string]string
}

func (*testProvider) ID() agents.ProviderID             { return "testprov" }
func (*testProvider) Capabilities() agents.Capabilities { return agents.Capabilities{Usage: true} }
func (*testProvider) SetupAccount(context.Context, agents.SetupRequest) (agents.SetupResult, error) {
	return agents.SetupResult{}, nil
}
func (*testProvider) PrepareSession(context.Context, agents.PrepareSessionRequest) (agents.PreparedSession, error) {
	return agents.PreparedSession{}, nil
}
func (*testProvider) FinalizeSession(context.Context, agents.FinalizeSessionRequest) error {
	return nil
}
func (p *testProvider) Usage(context.Context, agents.Account, agents.UsageOptions) (agents.UsageSnapshot, error) {
	if p.usageErr != nil {
		return agents.UsageSnapshot{}, p.usageErr
	}
	return p.usage, nil
}
func (*testProvider) DescribeAccount(context.Context, agents.Account) agents.AccountInfo {
	return agents.AccountInfo{Identity: "me@example.com", Plan: "pro"}
}
func (*testProvider) LaunchFields() []agents.LaunchField {
	return []agents.LaunchField{{ID: "effort", Label: "Effort", Kind: agents.LaunchFieldSelect}}
}
func (*testProvider) ProfileFields() []agents.LaunchField {
	return []agents.LaunchField{{ID: "model", Label: "Model", Kind: agents.LaunchFieldModel}}
}
func (p *testProvider) ProfileDefaults(agents.Account) map[string]string {
	out := map[string]string{}
	for k, v := range p.values {
		out[k] = v
	}
	return out
}
func (p *testProvider) UpdateProfile(a agents.Account, changes map[string]*string) (json.RawMessage, error) {
	if p.updateErr != nil {
		return nil, p.updateErr
	}
	if p.values == nil {
		p.values = map[string]string{}
	}
	for k, v := range changes {
		if v == nil || *v == "" {
			delete(p.values, k)
		} else {
			p.values[k] = *v
		}
	}
	return json.Marshal(p.values)
}

func (p *testProvider) Models(context.Context, agents.Account) ([]agents.ModelOption, error) {
	return []agents.ModelOption{{ID: "static", Label: "Static", Source: "bundled"}}, p.modelErr
}

type fixture struct {
	acc      *accounts
	provider *testProvider
	account  agents.Account
	paths    agenthost.Paths
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	root := t.TempDir()
	store, err := agents.NewFileAccountStore(filepath.Join(root, "accounts"))
	if err != nil {
		t.Fatal(err)
	}
	sessions, err := agents.NewFileSessionStore(filepath.Join(root, "sessions"))
	if err != nil {
		t.Fatal(err)
	}
	cache, err := agents.NewFileUsageCache(filepath.Join(root, "usage"))
	if err != nil {
		t.Fatal(err)
	}
	provider := &testProvider{}
	registry := agents.NewRegistry()
	if err := registry.Register(provider); err != nil {
		t.Fatal(err)
	}
	id, err := agents.NewAccountID()
	if err != nil {
		t.Fatal(err)
	}
	account := agents.Account{ID: id, Provider: "testprov", Name: "work"}
	if err := store.Create(account); err != nil {
		t.Fatal(err)
	}
	rt := &agentruntime.Runtime{
		Accounts: store, Sessions: sessions, Registry: registry,
		AccountService: &agents.AccountService{Store: store, Sessions: sessions, Registry: registry, Cache: cache},
		UsageService:   &agents.UsageService{Accounts: store, Sessions: sessions, Registry: registry, Cache: cache, TTL: time.Minute},
	}
	paths := agenthost.Paths{Records: filepath.Join(root, "records"), Sockets: filepath.Join(root, "socks")}
	if err := os.MkdirAll(paths.Records, 0o700); err != nil {
		t.Fatal(err)
	}
	return &fixture{acc: newAccounts(rt, paths, "/bin/bonsai"), provider: provider, account: account, paths: paths}
}

// hold writes a record for an account and keeps its lock like a live holder.
func (f *fixture) hold(t *testing.T, idHex string, state string) {
	t.Helper()
	id := "sess_" + idHex
	rec := agenthost.Record{Version: 1, ID: id, Summary: agentterminal.Summary{
		ID: id, AccountID: f.account.ID, Provider: f.account.Provider, State: state, CreatedAt: time.Now(),
	}}
	raw, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.paths.Records, id+".json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	lock, err := os.OpenFile(filepath.Join(f.paths.Records, id+".lock"), os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lock.Close() })
}

func TestUsageStaleKeepsLastSnapshotWithSanitizedError(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.provider.usage = agents.UsageSnapshot{Plan: "pro", Limits: []agents.UsageLimit{{ID: "5h", Label: "5h"}}}
	good, err := f.acc.Usage(ctx, f.account.ID, true)
	if err != nil || good.Stale || len(good.Snapshot.Limits) != 1 {
		t.Fatalf("good usage = %+v, %v", good, err)
	}

	f.provider.usageErr = errors.New("GET https://api.example.com failed reading " + secretPath + ": body {\"token\":\"sk-abc\"}")
	got, err := f.acc.Usage(ctx, f.account.ID, true)
	if err != nil {
		t.Fatalf("stale fallback returned error %v", err)
	}
	if !got.Stale || len(got.Snapshot.Limits) != 1 || got.Snapshot.Plan != "pro" {
		t.Fatalf("stale usage = %+v", got)
	}
	if got.Err == "" {
		t.Fatal("stale usage has no error text")
	}
	for _, bad := range []string{secretPath, "sk-abc", "/", "token", "example.com"} {
		if strings.Contains(got.Err, bad) {
			t.Fatalf("Err %q leaks %q", got.Err, bad)
		}
	}
	// A later success clears the stale flag.
	f.provider.usageErr = nil
	again, err := f.acc.Usage(ctx, f.account.ID, true)
	if err != nil || again.Stale || again.Err != "" {
		t.Fatalf("recovered usage = %+v, %v", again, err)
	}
}

func TestUsageErrorWithoutSnapshotIsSanitized(t *testing.T) {
	f := newFixture(t)
	f.provider.usageErr = errors.New("read " + secretPath)
	_, err := f.acc.Usage(context.Background(), f.account.ID, false)
	if err == nil || strings.Contains(err.Error(), secretPath) {
		t.Fatalf("err = %v", err)
	}
}

func TestDefaultsRoundTrip(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	d, err := f.acc.Defaults(ctx, f.account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Fields) != 1 || len(d.LaunchFields) != 1 || d.LaunchFields[0].ID != "effort" || len(d.Models) != 1 || len(d.Values) != 0 {
		t.Fatalf("defaults = %+v", d)
	}
	model := "opus"
	if err := f.acc.UpdateDefaults(ctx, f.account.ID, map[string]*string{"model": &model}); err != nil {
		t.Fatal(err)
	}
	d, _ = f.acc.Defaults(ctx, f.account.ID)
	if d.Values["model"] != "opus" {
		t.Fatalf("values after update = %v", d.Values)
	}
	if err := f.acc.UpdateDefaults(ctx, f.account.ID, map[string]*string{"model": nil}); err != nil {
		t.Fatal(err)
	}
	d, _ = f.acc.Defaults(ctx, f.account.ID)
	if _, ok := d.Values["model"]; ok {
		t.Fatalf("nil value did not reset: %v", d.Values)
	}
}

func TestDefaultsModelErrorKeepsFallback(t *testing.T) {
	f := newFixture(t)
	f.provider.modelErr = errors.New("api down")
	d, err := f.acc.Defaults(context.Background(), f.account.ID)
	if err != nil || len(d.Models) != 1 {
		t.Fatalf("defaults = %+v, %v", d, err)
	}
}

func TestRemoveRefusedWhileRunning(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.hold(t, "aa01", "running")
	list, err := f.acc.List(ctx)
	if err != nil || len(list) != 1 || list[0].Running != 1 {
		t.Fatalf("list = %+v, %v", list, err)
	}
	if list[0].ConcurrentSameAccount {
		t.Error("testProvider forbids concurrent same-account sessions")
	}
	if err := f.acc.Remove(ctx, f.account.ID); !errors.Is(err, ErrAccountInUse) {
		t.Fatalf("Remove = %v, want ErrAccountInUse", err)
	}
	if _, err := f.acc.rt.Accounts.Get(f.account.ID); err != nil {
		t.Fatalf("account removed despite refusal: %v", err)
	}
}

func TestRemoveIgnoresEndedAndDeadRecords(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.hold(t, "aa02", "exited") // alive lock but ended state
	list, _ := f.acc.List(ctx)
	if list[0].Running != 0 {
		t.Fatalf("running = %d", list[0].Running)
	}
	if err := f.acc.Remove(ctx, f.account.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.acc.rt.Accounts.Get(f.account.ID); !errors.Is(err, agents.ErrAccountNotFound) {
		t.Fatalf("account still present: %v", err)
	}
}

func TestDescribe(t *testing.T) {
	f := newFixture(t)
	info, err := f.acc.Describe(context.Background(), f.account.ID)
	if err != nil || info.Identity != "me@example.com" {
		t.Fatalf("info = %+v, %v", info, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := f.acc.Describe(ctx, f.account.ID); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled Describe = %v", err)
	}
}

func TestCommandArgv(t *testing.T) {
	f := newFixture(t)
	add := f.acc.AddCommand("claude", "work")
	if got := strings.Join(add.Args, " "); got != "/bin/bonsai agent account add claude work" {
		t.Fatalf("add argv = %q", got)
	}
	login := f.acc.LoginCommand("acct_1")
	if got := strings.Join(login.Args, " "); got != "/bin/bonsai agent account login acct_1" {
		t.Fatalf("login argv = %q", got)
	}
	if add.Env != nil || login.Env != nil {
		t.Fatal("commands must not change the environment")
	}
}
