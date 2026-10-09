//go:build linux || darwin

package agentterminal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

// testProvider uses a non-Antigravity ID so these tests prove the manager has
// no provider hardcoding.
type testProvider struct {
	agents.Provider
	finalized    atomic.Int32
	script       string
	prepared     chan agents.PrepareSessionRequest
	capabilities agents.Capabilities
	validate     func(agents.Account, agents.LaunchOptions) error
}

const testProviderID agents.ProviderID = "fake"

func (p *testProvider) ID() agents.ProviderID             { return testProviderID }
func (p *testProvider) Capabilities() agents.Capabilities { return p.capabilities }
func (p *testProvider) Label() string                     { return "Fake Agent" }
func (p *testProvider) Availability(context.Context) agents.Availability {
	return agents.Availability{Available: true}
}
func (p *testProvider) ValidateLaunch(a agents.Account, o agents.LaunchOptions) error {
	if p.validate != nil {
		return p.validate(a, o)
	}
	return nil
}
func (p *testProvider) PrepareSession(_ context.Context, r agents.PrepareSessionRequest) (agents.PreparedSession, error) {
	if p.prepared != nil {
		p.prepared <- r
	}
	return agents.PreparedSession{Executable: "/bin/sh", Args: []string{"-c", p.script, "agent-fixture"}, Dir: r.Session.WorkDir, EnvSet: map[string]string{"HOME": r.Session.HomeDir, "BONSAI_TEST_SET": "isolated"}, EnvUnset: []string{"BONSAI_TEST_UNSET"}, EnvUnsetPrefixes: []string{"BONSAI_TEST_PREFIX_"}, ProviderSessionID: "provider-" + string(r.Session.ID)}, nil
}
func (p *testProvider) FinalizeSession(ctx context.Context, _ agents.FinalizeSessionRequest) error {
	if ctx.Err() != nil {
		panic("cleanup used cancelled context")
	}
	p.finalized.Add(1)
	return nil
}
func testManager(t *testing.T, script string) (*Manager, *testProvider, agents.AccountID, *agents.FileSessionStore) {
	t.Helper()
	accounts, err := agents.NewFileAccountStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	id, _ := agents.NewAccountID()
	if err = accounts.Create(agents.Account{ID: id, Provider: testProviderID, Name: "fixture"}); err != nil {
		t.Fatal(err)
	}
	sessions, err := agents.NewFileSessionStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	provider := &testProvider{script: script, capabilities: agents.Capabilities{Interactive: true, MultiAccount: true, ConcurrentSameAccount: true, ConcurrentCrossAccount: true}}
	registry := agents.NewRegistry()
	if err = registry.Register(provider); err != nil {
		t.Fatal(err)
	}
	m := New(accounts, sessions, registry, nil)
	t.Cleanup(m.Close)
	return m, provider, id, sessions
}
func awaitState(t *testing.T, m *Manager, id, state string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		s, _ := m.Get("project", id)
		if s.State == state {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	s, _ := m.Get("project", id)
	t.Fatalf("wanted %s, got %+v", state, s)
}
func TestPTYLifecycleAndReplay(t *testing.T) {
	t.Setenv("BONSAI_TEST_UNSET", "secret")
	t.Setenv("BONSAI_TEST_PREFIX_TOKEN", "secret")
	m, p, account, store := testManager(t, `printf '\033[32mREADY\033[0m\n'; pwd; printf '%s:%s:%s\n' "$BONSAI_TEST_SET" "${BONSAI_TEST_UNSET-unset}" "${BONSAI_TEST_PREFIX_TOKEN-unset}"; read line; stty size; printf 'INPUT:%s\n' "$line"`)
	dir := t.TempDir()
	s, err := m.Start("project", "tree", account, "", dir, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	awaitState(t, m, s.ID, "running")
	writer, writable, _, frames, err := m.Attach("project", s.ID, 0)
	if err != nil || !writable {
		t.Fatalf("attach %v %v", writable, err)
	}
	observer, writable, _, _, err := m.Attach("project", s.ID, 0)
	if err != nil || writable {
		t.Fatal("second attachment should observe")
	}
	if m.Input(s.ID, observer, []byte("bad\n")) == nil {
		t.Fatal("observer accepted input")
	}
	if err = m.Resize(s.ID, writer, 101, 31); err != nil {
		t.Fatal(err)
	}
	if err = m.Input(s.ID, writer, []byte("hello\n")); err != nil {
		t.Fatal(err)
	}
	timeout := time.After(5 * time.Second)
	finished := false
	for !finished {
		select {
		case f := <-frames:
			finished = f.Type == "status" && f.Status != nil && !f.Status.Active()
		case <-timeout:
			t.Fatal("no final status")
		}
	}
	m.Detach(s.ID, writer)
	m.Detach(s.ID, observer)
	_, writable, replay, _, err := m.Attach("project", s.ID, 0)
	if err != nil || !writable {
		t.Fatal(err)
	}
	var output string
	for _, f := range replay {
		output += string(f.Data)
	}
	for _, want := range []string{"\x1b[32mREADY", dir, "isolated:unset:unset", "31 101", "INPUT:hello"} {
		if !strings.Contains(output, want) {
			t.Fatalf("missing %q in %q", want, output)
		}
	}
	m.Close()
	if p.finalized.Load() != 1 {
		t.Fatal("finalized more than once")
	}
	entries, _ := os.ReadDir(store.Root())
	if len(entries) != 0 {
		t.Fatal("session home leaked")
	}
}
func TestStopConcurrentSameProfileAndLimits(t *testing.T) {
	m, p, account, store := testManager(t, `trap '' TERM; printf ready; while :; do sleep 1; done`)
	var ids []string
	for i := 0; i < 16; i++ {
		s, err := m.Start("project", "tree", account, "", t.TempDir(), 80, 24)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, s.ID)
	}
	if _, err := m.Start("project", "tree", account, "", t.TempDir(), 80, 24); err == nil {
		t.Fatal("missing active limit")
	}
	for _, id := range ids {
		awaitState(t, m, id, "running")
		if err := m.Stop("project", id); err != nil {
			t.Fatal(err)
		}
		_ = m.Stop("project", id)
	}
	m.Close()
	if p.finalized.Load() != 16 {
		t.Fatalf("finalized %d", p.finalized.Load())
	}
	entries, _ := os.ReadDir(store.Root())
	if len(entries) != 0 {
		t.Fatal("homes leaked")
	}
}
func TestBoundedReplaySlowReadersAndRetention(t *testing.T) {
	m := New(nil, nil, nil, nil)
	e := &session{summary: Summary{ID: "one", ProjectID: "project", State: "running"}, subscribers: map[uint64]chan Frame{}}
	m.entries["one"] = e
	id, _, _, ch, _ := m.Attach("project", "one", 0)
	for i := 0; i < 200; i++ {
		m.output(e, make([]byte, 16384))
	}
	if len(e.output) != OutputLimit {
		t.Fatal(len(e.output))
	}
	for range ch {
	}
	if e.writer != 0 {
		t.Fatal("slow writer retained")
	}
	m.Detach("one", id)
	_, _, replay, _, _ := m.Attach("project", "one", 0)
	if replay[0].Type != "gap" || replay[1].Offset != e.offset-OutputLimit {
		t.Fatal("missing gap")
	}
	for i := 0; i < 40; i++ {
		m.Restore(Summary{ID: strings.Repeat("x", i+1), ProjectID: "project", State: "failed", CreatedAt: time.Now()})
	}
	if len(m.List("project")) != 33 {
		t.Fatal("completed retention limit")
	}
}

func TestCtrlCAndDetachWriterHandoff(t *testing.T) {
	m, p, account, _ := testManager(t, `trap 'exit 0' INT; printf ready; while :; do read line; done`)
	summary, err := m.Start("project", "tree", account, "", t.TempDir(), 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	awaitState(t, m, summary.ID, "running")
	first, _, _, _, _ := m.Attach("project", summary.ID, 0)
	m.Detach(summary.ID, first)
	second, writer, _, frames, err := m.Attach("project", summary.ID, 0)
	if err != nil || !writer {
		t.Fatal("writer ownership not released")
	}
	// Wait for the helper to install its handler before sending the interrupt.
	deadline := time.After(5 * time.Second)
	ready := false
	for !ready {
		m.mu.Lock()
		ready = strings.Contains(string(m.entries[summary.ID].replay(0)), "ready")
		m.mu.Unlock()
		if !ready {
			select {
			case <-frames:
			case <-deadline:
				t.Fatal("helper not ready")
			}
		}
	}
	if err := m.Input(summary.ID, second, []byte{3}); err != nil {
		t.Fatal(err)
	}
	awaitState(t, m, summary.ID, "exited")
	m.Close()
	if p.finalized.Load() != 1 {
		t.Fatal("interrupt did not finalize")
	}
}

func TestTwoProfilesAndWorktreesAreScoped(t *testing.T) {
	m, _, first, _ := testManager(t, `pwd; read line`)
	second, _ := agents.NewAccountID()
	if err := m.accounts.Create(agents.Account{ID: second, Provider: testProviderID, Name: "second"}); err != nil {
		t.Fatal(err)
	}
	a, err := m.Start("one", "tree-one", first, "", t.TempDir(), 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	b, err := m.Start("two", "tree-two", second, "", t.TempDir(), 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.List("one")) != 1 || len(m.List("two")) != 1 {
		t.Fatal("project sessions leaked")
	}
	if _, ok := m.Get("two", a.ID); ok {
		t.Fatal("cross-project read")
	}
	if err := m.Stop("one", b.ID); err == nil {
		t.Fatal("cross-project stop")
	}
	m.Close()
}

func TestLaunchOptionsReachProviderUnchanged(t *testing.T) {
	m, p, accountID, _ := testManager(t, `exit 0`)
	account, err := m.accounts.Get(accountID)
	if err != nil {
		t.Fatal(err)
	}
	account.Settings = json.RawMessage(`{"model":"profile-model"}`)
	if err := m.accounts.Update(account); err != nil {
		t.Fatal(err)
	}
	stored, _ := m.accounts.Get(accountID)
	p.prepared = make(chan agents.PrepareSessionRequest, 1)
	fullAccess := true
	options := StartOptions{Model: "chosen-model", Prompt: "--literal task", FullAccess: &fullAccess, PermissionMode: "plan", Effort: "high"}
	summary, err := m.Start("project", "tree", accountID, "named", t.TempDir(), 80, 24, options)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case request := <-p.prepared:
		want := options
		want.DisplayName = "named"
		if !reflect.DeepEqual(request.Launch, want) {
			t.Fatalf("launch = %+v, want %+v", request.Launch, want)
		}
		if len(request.Args) != 0 {
			t.Fatalf("manager built provider args: %v", request.Args)
		}
		if string(request.Account.Settings) != string(stored.Settings) {
			t.Fatalf("manager patched settings: %s", request.Account.Settings)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("provider was not prepared")
	}
	awaitState(t, m, summary.ID, "exited")
	status, _ := m.Get("project", summary.ID)
	if status.ProviderSessionID != "provider-"+summary.ID {
		t.Fatalf("provider session id = %q", status.ProviderSessionID)
	}
	saved, _ := m.accounts.Get(accountID)
	if string(saved.Settings) != string(stored.Settings) {
		t.Fatal("launch changed stored profile settings")
	}
}

func TestValidatorErrorLeavesNoSession(t *testing.T) {
	m, p, account, store := testManager(t, `exit 0`)
	p.validate = func(_ agents.Account, o agents.LaunchOptions) error {
		if o.Effort != "" {
			return fmt.Errorf("effort unsupported")
		}
		return nil
	}
	_, err := m.Start("project", "tree", account, "", t.TempDir(), 80, 24, StartOptions{Effort: "max"})
	if !errors.Is(err, agents.ErrInvalidLaunch) || !strings.Contains(err.Error(), "effort unsupported") {
		t.Fatalf("err = %v", err)
	}
	if len(m.List("project")) != 0 {
		t.Fatal("rejected launch left a session")
	}
	if entries, _ := os.ReadDir(store.Root()); len(entries) != 0 {
		t.Fatal("rejected launch left a runtime dir")
	}
}

func TestNonInteractiveProviderRejected(t *testing.T) {
	m, p, account, _ := testManager(t, `exit 0`)
	p.capabilities.Interactive = false
	if _, err := m.Start("project", "tree", account, "", t.TempDir(), 80, 24); err == nil {
		t.Fatal("non-interactive provider started")
	}
}

func TestConcurrencyCapabilitiesEnforced(t *testing.T) {
	m, p, first, _ := testManager(t, `while :; do sleep 1; done`)
	p.capabilities.ConcurrentSameAccount = false
	second, _ := agents.NewAccountID()
	if err := m.accounts.Create(agents.Account{ID: second, Provider: testProviderID, Name: "second"}); err != nil {
		t.Fatal(err)
	}
	a, err := m.Start("project", "tree", first, "", t.TempDir(), 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Start("project", "tree", first, "", t.TempDir(), 80, 24); !errors.Is(err, agents.ErrAccountBusy) {
		t.Fatalf("same account err = %v", err)
	}
	if _, err := m.Start("project", "tree", second, "", t.TempDir(), 80, 24); err != nil {
		t.Fatalf("other account rejected: %v", err)
	}
	p.capabilities.ConcurrentCrossAccount = false
	third, _ := agents.NewAccountID()
	if err := m.accounts.Create(agents.Account{ID: third, Provider: testProviderID, Name: "third"}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Start("project", "tree", third, "", t.TempDir(), 80, 24); !errors.Is(err, agents.ErrProviderBusy) {
		t.Fatalf("cross account err = %v", err)
	}
	// Once the first session ends, its account can start again.
	p.capabilities.ConcurrentCrossAccount = true
	awaitState(t, m, a.ID, "running")
	if err := m.Stop("project", a.ID); err != nil {
		t.Fatal(err)
	}
	awaitState(t, m, a.ID, "exited")
	if _, err := m.Start("project", "tree", first, "", t.TempDir(), 80, 24); err != nil {
		t.Fatalf("restart after stop: %v", err)
	}
	m.Close()
}

func TestConcurrentSessionsOnOneAccount(t *testing.T) {
	m, p, account, store := testManager(t, `while :; do sleep 1; done`)
	var wg sync.WaitGroup
	ids := make(chan string, 3)
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, err := m.Start("project", "tree", account, "", t.TempDir(), 80, 24)
			if err != nil {
				t.Error(err)
				return
			}
			ids <- s.ID
		}()
	}
	wg.Wait()
	close(ids)
	seen := map[string]bool{}
	for id := range ids {
		awaitState(t, m, id, "running")
		seen[id] = true
	}
	if len(seen) != 3 {
		t.Fatalf("sessions = %d", len(seen))
	}
	for id := range seen {
		if err := m.Stop("project", id); err != nil {
			t.Fatal(err)
		}
	}
	m.Close()
	if p.finalized.Load() != 3 {
		t.Fatalf("finalized %d", p.finalized.Load())
	}
	if entries, _ := os.ReadDir(store.Root()); len(entries) != 0 {
		t.Fatal("runtime dirs leaked")
	}
}

func TestFailureNamesProviderLabel(t *testing.T) {
	m, _, account, _ := testManager(t, `exit 3`)
	summary, err := m.Start("project", "tree", account, "", t.TempDir(), 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	awaitState(t, m, summary.ID, "failed")
	status, _ := m.Get("project", summary.ID)
	if !strings.Contains(status.Error, "Check the Fake Agent installation") {
		t.Fatalf("error = %q", status.Error)
	}
}
