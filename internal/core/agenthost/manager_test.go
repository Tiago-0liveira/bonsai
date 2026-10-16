//go:build linux || darwin

package agenthost

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
	m := NewManager(accounts, sessions, registry, testPaths(t), InProcess(Deps{Accounts: accounts, Sessions: sessions, Registry: registry, Linger: 200 * time.Millisecond}), nil)
	t.Cleanup(func() { m.Close(false, 10*time.Second) })
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
	writer, err := m.Attach("project", s.ID, 0, "", "web")
	if err != nil || !writer.Writer() {
		t.Fatalf("attach %v", err)
	}
	observer, err := m.Attach("project", s.ID, 0, "", "web")
	if err != nil || observer.Writer() {
		t.Fatal("second attachment should observe")
	}
	if observer.Input([]byte("bad\n")) == nil {
		t.Fatal("observer accepted input")
	}
	if err = writer.Resize(101, 31); err != nil {
		t.Fatal(err)
	}
	if err = writer.Input([]byte("hello\n")); err != nil {
		t.Fatal(err)
	}
	var output string
	for _, f := range writer.Replay {
		output += string(f.Data)
	}
	timeout := time.After(5 * time.Second)
	finished := false
	for !finished {
		select {
		case f := <-writer.Frames:
			output += string(f.Data)
			finished = f.Type == "status" && f.Status != nil && !f.Status.Active()
		case <-timeout:
			t.Fatal("no final status")
		}
	}
	writer.Close()
	observer.Close()
	// Once the holder has exited, an attachment replays the final status.
	awaitHolderExit(t, m, s.ID)
	ended, err := m.Attach("project", s.ID, 0, "", "web")
	if err != nil || len(ended.Replay) != 1 || ended.Replay[0].Status.State != "exited" {
		t.Fatalf("ended attach %+v %v", ended, err)
	}
	ended.Close()
	for _, want := range []string{"\x1b[32mREADY", dir, "isolated:unset:unset", "31 101", "INPUT:hello"} {
		if !strings.Contains(output, want) {
			t.Fatalf("missing %q in %q", want, output)
		}
	}
	m.Close(false, 10*time.Second)
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
	m.Close(false, 10*time.Second)
	if p.finalized.Load() != 16 {
		t.Fatalf("finalized %d", p.finalized.Load())
	}
	entries, _ := os.ReadDir(store.Root())
	if len(entries) != 0 {
		t.Fatal("homes leaked")
	}
}
func TestEndedSessionRetention(t *testing.T) {
	m := NewManager(nil, nil, nil, Paths{}, nil, nil)
	for i := 0; i < 40; i++ {
		m.Restore(Summary{ID: strings.Repeat("x", i+1), ProjectID: "project", State: "failed", CreatedAt: time.Now()})
	}
	if len(m.List("project")) != 32 {
		t.Fatal("completed retention limit")
	}
	m.Restore(Summary{ID: "active", ProjectID: "project", State: "running", CreatedAt: time.Now()})
	if s, _ := m.Get("project", "active"); s.State != "failed" || !strings.Contains(s.Error, "interrupted") {
		t.Fatalf("restored active session %+v", s)
	}
}

func TestCtrlCAndDetachWriterHandoff(t *testing.T) {
	m, p, account, _ := testManager(t, `trap 'exit 0' INT; printf ready; while :; do read line; done`)
	summary, err := m.Start("project", "tree", account, "", t.TempDir(), 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	awaitState(t, m, summary.ID, "running")
	first, err := m.Attach("project", summary.ID, 0, "", "web")
	if err != nil {
		t.Fatal(err)
	}
	first.Close()
	awaitClients(t, m, summary.ID, 0)
	second, err := m.Attach("project", summary.ID, 0, "", "web")
	if err != nil || !second.Writer() {
		t.Fatal("writer ownership not released")
	}
	defer second.Close()
	// Wait for the helper to install its handler before sending the interrupt.
	output := ""
	for _, f := range second.Replay {
		output += string(f.Data)
	}
	deadline := time.After(5 * time.Second)
	for !strings.Contains(output, "ready") {
		select {
		case f := <-second.Frames:
			output += string(f.Data)
		case <-deadline:
			t.Fatal("helper not ready")
		}
	}
	if err := second.Input([]byte{3}); err != nil {
		t.Fatal(err)
	}
	awaitState(t, m, summary.ID, "exited")
	m.Close(false, 10*time.Second)
	if p.finalized.Load() != 1 {
		t.Fatal("interrupt did not finalize")
	}
}

func TestTwoProfilesAndWorktreesAreScoped(t *testing.T) {
	m, _, first, _ := testManager(t, `pwd; read line`)
	second, _ := agents.NewAccountID()
	if err := m.starter.Accounts.Create(agents.Account{ID: second, Provider: testProviderID, Name: "second"}); err != nil {
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
	m.Close(false, 10*time.Second)
}

func TestLaunchOptionsReachProviderUnchanged(t *testing.T) {
	m, p, accountID, _ := testManager(t, `exit 0`)
	account, err := m.starter.Accounts.Get(accountID)
	if err != nil {
		t.Fatal(err)
	}
	account.Settings = json.RawMessage(`{"model":"profile-model"}`)
	if err := m.starter.Accounts.Update(account); err != nil {
		t.Fatal(err)
	}
	stored, _ := m.starter.Accounts.Get(accountID)
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
	// The summary records the accepted launch details, never the prompt, and
	// they survive the journal's JSON round trip.
	wantOptions := map[string]string{"full_access": "true", "permission_mode": "plan", "effort": "high"}
	if summary.Model != "chosen-model" || !reflect.DeepEqual(summary.Options, wantOptions) || status.Model != summary.Model || !reflect.DeepEqual(status.Options, wantOptions) {
		t.Fatalf("summary = %+v, status = %+v", summary, status)
	}
	encoded, _ := json.Marshal(status)
	var restored Summary
	if err := json.Unmarshal(encoded, &restored); err != nil || restored.Model != "chosen-model" || !reflect.DeepEqual(restored.Options, wantOptions) {
		t.Fatalf("round trip = %+v (%v)", restored, err)
	}
	if strings.Contains(string(encoded), "literal task") {
		t.Fatalf("prompt in summary: %s", encoded)
	}
	saved, _ := m.starter.Accounts.Get(accountID)
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
	if err := m.starter.Accounts.Create(agents.Account{ID: second, Provider: testProviderID, Name: "second"}); err != nil {
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
	if err := m.starter.Accounts.Create(agents.Account{ID: third, Provider: testProviderID, Name: "third"}); err != nil {
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
	m.Close(false, 10*time.Second)
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
	m.Close(false, 10*time.Second)
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

func awaitHolderExit(t *testing.T, m *Manager, id string) {
	t.Helper()
	if !Wait(m.paths, id, 5*time.Second) {
		t.Fatal("holder did not exit")
	}
}

func awaitClients(t *testing.T, m *Manager, id string, n int) {
	t.Helper()
	r, err := Read(m.paths, id)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if info, err := Query(r); err == nil && len(info.Clients) == n {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("holder never had %d clients", n)
}

func TestCloseKeepsHoldersAndNextManagerAdopts(t *testing.T) {
	m, p, account, _ := testManager(t, `printf 'BEFORE\n'; while read line; do printf 'GOT:%s\n' "$line"; done`)
	s, err := m.Start("project", "tree", account, "kept", t.TempDir(), 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	awaitState(t, m, s.ID, "running")
	a, err := m.Attach("project", s.ID, 0, "", "web")
	if err != nil {
		t.Fatal(err)
	}
	generation := a.Generation
	m.Close(true, 0)
	if _, ok := <-a.Frames; ok {
		for range a.Frames {
		}
	}
	r, err := Read(m.paths, s.ID)
	if err != nil || !r.Alive || r.Summary.State != "running" {
		t.Fatalf("holder after keep-running close: %+v %v", r, err)
	}

	next := NewManager(m.starter.Accounts, m.sessions, m.starter.Registry, m.paths, m.starter.Launch, nil)
	t.Cleanup(func() { next.Close(false, 10*time.Second) })
	adopted := next.Adopt()
	if adopted[s.ID].State != "running" {
		t.Fatalf("adopted %+v", adopted)
	}
	if got, ok := next.Get("project", s.ID); !ok || got.Name != "kept" || !got.Active() {
		t.Fatalf("adopted session %+v", got)
	}
	if _, err := next.Start("project", "tree", account, "", t.TempDir(), 80, 24); err != nil {
		t.Fatal(err)
	}
	if n := len(next.ActiveSessions(account)); n != 2 {
		t.Fatalf("adopted sessions count toward the active ones: %d", n)
	}
	resumed, err := next.Attach("project", s.ID, 0, generation, "web")
	if err != nil || resumed.Generation != generation || !resumed.Writer() {
		t.Fatalf("resume %+v %v", resumed, err)
	}
	if err := resumed.Input([]byte("after\n")); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(5 * time.Second)
	output := ""
	for !strings.Contains(output, "GOT:after") {
		select {
		case f := <-resumed.Frames:
			output += string(f.Data)
		case <-deadline:
			t.Fatalf("no echo: %q", output)
		}
	}
	resumed.Close()
	if err := next.Stop("project", s.ID); err != nil {
		t.Fatal(err)
	}
	awaitState(t, next, s.ID, "exited")
	next.Close(false, 10*time.Second)
	if p.finalized.Load() != 2 {
		t.Fatalf("finalized %d", p.finalized.Load())
	}
}

func TestLostHolderFailsItsSession(t *testing.T) {
	m := NewManager(nil, nil, nil, testPaths(t), nil, nil)
	if err := m.paths.ensure(); err != nil {
		t.Fatal(err)
	}
	id := "sess_0abc"
	if err := writeRecord(m.paths, Record{Version: RecordVersion, ID: id, Protocol: Version, Socket: m.paths.socket(id), Summary: Summary{ID: id, ProjectID: "project", State: "running", CreatedAt: time.Now().UTC()}}); err != nil {
		t.Fatal(err)
	}
	adopted := m.Adopt()
	if s := adopted[id]; s.State != "failed" || s.Error != LostError {
		t.Fatalf("dead holder's session %+v", s)
	}
	if _, ok := m.Get("project", id); ok {
		t.Fatal("adopted a dead holder")
	}
}

func TestManagerStartsWithWebOriginAndAdmitsThroughDisk(t *testing.T) {
	m, p, account, _ := testManager(t, `while :; do sleep 1; done`)
	p.capabilities.ConcurrentSameAccount = false
	s, err := m.Start("project", "tree", account, "", t.TempDir(), 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	if s.Origin != "web" {
		t.Fatalf("origin = %q", s.Origin)
	}
	// A session another process started counts against the manager's start.
	other := Starter{Accounts: m.starter.Accounts, Sessions: m.sessions, Registry: m.starter.Registry, Paths: m.paths, Launch: m.starter.Launch, Origin: "tui"}
	if _, err := other.Start(context.Background(), StartRequest{Project: "project", Tree: "tree", Dir: t.TempDir(), AccountID: account, Cols: 80, Rows: 24}); !errors.Is(err, agents.ErrAccountBusy) {
		t.Fatalf("err = %v", err)
	}
}

func TestManagerCloseCancelsStartMidLaunch(t *testing.T) {
	m, _, account, store := testManager(t, `while :; do sleep 1; done`)
	launch := m.starter.Launch
	ready := make(chan string, 1)
	m.starter.Launch = func(ctx context.Context, p Paths, spec Spec) error {
		if err := launch(ctx, p, spec); err != nil {
			return err
		}
		ready <- spec.Summary.ID
		<-ctx.Done()
		return ctx.Err()
	}
	started := make(chan error, 1)
	dir := t.TempDir()
	go func() {
		_, err := m.Start("project", "tree", account, "", dir, 80, 24)
		started <- err
	}()
	var id string
	select {
	case id = <-ready:
	case err := <-started:
		t.Fatalf("launch failed before ready: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("launcher never became ready")
	}
	closed := make(chan struct{})
	go func() {
		m.Close(true, 0)
		close(closed)
	}()
	select {
	case err := <-started:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("start err = %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("start did not cancel on close")
	}
	<-closed
	if held(m.paths.lock(id)) {
		t.Fatal("holder survived cancelled start")
	}
	if entries, _ := os.ReadDir(store.Root()); len(entries) != 0 {
		t.Fatal("cancelled start left a runtime dir")
	}
}

func TestManagerCloseCancelsLaunchBeforeReady(t *testing.T) {
	m, _, account, store := testManager(t, `exit 0`)
	launching := make(chan struct{})
	m.starter.Launch = func(ctx context.Context, _ Paths, _ Spec) error {
		close(launching)
		<-ctx.Done()
		return ctx.Err()
	}
	dir := t.TempDir()
	started := make(chan error, 1)
	go func() {
		_, err := m.Start("project", "tree", account, "", dir, 80, 24)
		started <- err
	}()
	select {
	case <-launching:
	case err := <-started:
		t.Fatalf("start failed before launch: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("start never reached launcher")
	}
	m.Close(true, 0)
	select {
	case err := <-started:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("start err = %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("start did not cancel on close")
	}
	if entries, _ := os.ReadDir(store.Root()); len(entries) != 0 {
		t.Fatal("cancelled start left a runtime dir")
	}
}

func TestCloseStopsOnlyWebStartedSessions(t *testing.T) {
	m, _, account, _ := testManager(t, `while :; do sleep 1; done`)
	web, err := m.Start("project", "tree", account, "web", t.TempDir(), 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	other := Starter{Accounts: m.starter.Accounts, Sessions: m.sessions, Registry: m.starter.Registry, Paths: m.paths, Launch: m.starter.Launch, Origin: "tui"}
	tui, err := other.Start(context.Background(), StartRequest{Project: "project", Tree: "tree", Dir: t.TempDir(), AccountID: account, Cols: 80, Rows: 24})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stopAll(t, m.paths) })
	if adopted := m.Adopt(); adopted[tui.ID].Origin != "tui" {
		t.Fatalf("adopted %+v", adopted)
	}
	awaitState(t, m, web.ID, "running")
	awaitState(t, m, tui.ID, "running")
	m.Close(false, 10*time.Second)
	if r, err := Read(m.paths, web.ID); err != nil || r.Alive || r.Summary.Active() {
		t.Fatalf("web session after close: %+v %v", r.Summary, err)
	}
	if r, err := Read(m.paths, tui.ID); err != nil || !r.Alive || r.Summary.State != "running" {
		t.Fatalf("tui session after close: %+v %v", r.Summary, err)
	}
}

func TestAdoptRacingStartKeepsOneEntry(t *testing.T) {
	m, _, account, _ := testManager(t, `while :; do sleep 1; done`)
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				m.Adopt()
			}
		}
	}()
	s, err := m.Start("project", "tree", account, "", t.TempDir(), 80, 24)
	close(stop)
	wg.Wait()
	if err != nil {
		t.Fatal(err)
	}
	m.Adopt()
	m.mu.Lock()
	n := len(m.entries)
	e := m.entries[s.ID]
	m.mu.Unlock()
	if n != 1 || e == nil || e.record.ID != s.ID {
		t.Fatalf("entries = %d, entry = %+v", n, e)
	}
	awaitState(t, m, s.ID, "running")
	closed := make(chan struct{})
	go func() { m.Close(false, 10*time.Second); close(closed) }()
	select {
	case <-closed:
	case <-time.After(8 * time.Second):
		t.Fatal("Close did not return")
	}
}

// A session another client starts shows up in the manager and a project
// notification, and an external stop shows it ended.
func TestWatchRecordsAdoptsOtherClientsSessions(t *testing.T) {
	s, _, account := testStarter(t, `while :; do sleep 1; done`, agents.Capabilities{Interactive: true})
	var mu sync.Mutex
	notified := 0
	m := NewManager(s.Accounts, s.Sessions, s.Registry, s.Paths, s.Launch, func(project string) {
		if project == "project" {
			mu.Lock()
			notified++
			mu.Unlock()
		}
	})
	t.Cleanup(func() { m.Close(true, 0) })
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); m.WatchRecords(ctx) }()

	record, err := s.Start(context.Background(), startRequest(t, account))
	if err != nil {
		t.Fatal(err)
	}
	awaitState(t, m, record.ID, "running")
	if got, _ := m.Get("project", record.ID); got.Origin != "tui" {
		t.Fatalf("origin %q", got.Origin)
	}
	mu.Lock()
	if notified == 0 {
		t.Error("adoption sent no notification")
	}
	mu.Unlock()

	if err := StopHolder(s.Paths, record, ""); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		got, _ := m.Get("project", record.ID)
		if got.State != "" && !got.Active() {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("still %q after external stop", got.State)
		}
		time.Sleep(10 * time.Millisecond)
	}

	// A session that ended before the manager saw it still shows as history.
	ended, err := s.Start(context.Background(), startRequest(t, account))
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	<-done
	if err := StopHolder(s.Paths, ended, ""); err != nil {
		t.Fatal(err)
	}
	Wait(s.Paths, ended.ID, 10*time.Second)
	late := NewManager(s.Accounts, s.Sessions, s.Registry, s.Paths, s.Launch, nil)
	t.Cleanup(func() { late.Close(true, 0) })
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	go late.WatchRecords(ctx2)
	// A write to any record triggers the first sync.
	if err := writeRecord(s.Paths, mustRead(t, s.Paths, record.ID)); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for {
		if got, ok := late.Get("project", ended.ID); ok && !got.Active() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("ended session never appeared as history")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func mustRead(t *testing.T, p Paths, id string) Record {
	t.Helper()
	r, err := Read(p, id)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
