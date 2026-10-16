//go:build linux || darwin

package agenthost

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

const (
	admitHelperEnv  = "BONSAI_TEST_ADMIT_HELPER"
	admitHelperPath = "BONSAI_TEST_ADMIT_PATH"
)

// TestMain doubles as the helper process that holds the admission lock: a
// second process is the only way to prove the lock works across processes.
func TestMain(m *testing.M) {
	if os.Getenv(admitHelperEnv) == "1" {
		lock, err := agents.LockFileContext(context.Background(), os.Getenv(admitHelperPath))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println("locked")
		// Held until the parent closes stdin or kills this process.
		_, _ = io.Copy(io.Discard, os.Stdin)
		_ = lock.Unlock()
		return
	}
	os.Exit(m.Run())
}

// testStarter returns a Starter whose holders run in this process.
func testStarter(t *testing.T, script string, capabilities agents.Capabilities) (Starter, *testProvider, agents.AccountID) {
	s, p, id, _, _ := testStarterDirs(t, script, capabilities)
	return s, p, id
}

// testStarterDirs also returns the account and session store directories, so
// a test can build a second Starter that shares nothing in memory.
func testStarterDirs(t *testing.T, script string, capabilities agents.Capabilities) (Starter, *testProvider, agents.AccountID, string, string) {
	t.Helper()
	accountsDir, sessionsDir := t.TempDir(), t.TempDir()
	accounts, err := agents.NewFileAccountStore(accountsDir)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := agents.NewAccountID()
	if err = accounts.Create(agents.Account{ID: id, Provider: testProviderID, Name: "fixture"}); err != nil {
		t.Fatal(err)
	}
	sessions, err := agents.NewFileSessionStore(sessionsDir)
	if err != nil {
		t.Fatal(err)
	}
	provider := &testProvider{script: script, capabilities: capabilities}
	registry := agents.NewRegistry()
	if err = registry.Register(provider); err != nil {
		t.Fatal(err)
	}
	paths := testPaths(t)
	s := Starter{Accounts: accounts, Sessions: sessions, Registry: registry, Paths: paths, Launch: InProcess(Deps{Accounts: accounts, Sessions: sessions, Registry: registry, Linger: 200 * time.Millisecond}), Origin: "tui"}
	t.Cleanup(func() { stopAll(t, paths) })
	return s, provider, id, accountsDir, sessionsDir
}

// otherProcess returns a Starter over the same directories as s with its own
// stores, registry and provider, as a second process would have.
func otherProcess(t *testing.T, s Starter, script string, capabilities agents.Capabilities, accountsDir, sessionsDir string) Starter {
	t.Helper()
	accounts, err := agents.NewFileAccountStore(accountsDir)
	if err != nil {
		t.Fatal(err)
	}
	sessions, err := agents.NewFileSessionStore(sessionsDir)
	if err != nil {
		t.Fatal(err)
	}
	registry := agents.NewRegistry()
	if err := registry.Register(&testProvider{script: script, capabilities: capabilities}); err != nil {
		t.Fatal(err)
	}
	s.Accounts, s.Sessions, s.Registry = accounts, sessions, registry
	s.Launch = InProcess(Deps{Accounts: accounts, Sessions: sessions, Registry: registry, Linger: 200 * time.Millisecond})
	return s
}

// stopAll ends every holder under paths so the temp dirs can go.
func stopAll(t *testing.T, paths Paths) {
	t.Helper()
	records, _ := List(paths)
	for _, r := range records {
		if r.Alive {
			_ = StopHolder(paths, r, "")
			Wait(paths, r.ID, 10*time.Second)
		}
	}
}

func startRequest(t *testing.T, account agents.AccountID) StartRequest {
	t.Helper()
	return StartRequest{Project: "project", Tree: "tree", Dir: t.TempDir(), AccountID: account, Cols: 80, Rows: 24}
}

func TestStarterValidationErrors(t *testing.T) {
	interactive := agents.Capabilities{Interactive: true, ConcurrentSameAccount: true, ConcurrentCrossAccount: true}
	cases := []struct {
		name   string
		mutate func(*Starter, *testProvider, *StartRequest)
		want   string
		is     error
	}{
		{"no paths", func(s *Starter, _ *testProvider, _ *StartRequest) { s.Paths = Paths{} }, "agent hosts unavailable", nil},
		{"zero cols", func(_ *Starter, _ *testProvider, r *StartRequest) { r.Cols = 0 }, "terminal dimensions must be between 1 and 500", nil},
		{"huge rows", func(_ *Starter, _ *testProvider, r *StartRequest) { r.Rows = 501 }, "terminal dimensions must be between 1 and 500", nil},
		{"unknown account", func(_ *Starter, _ *testProvider, r *StartRequest) { r.AccountID = "acct_missing" }, "profile unavailable", nil},
		{"unregistered provider", func(s *Starter, _ *testProvider, _ *StartRequest) { s.Registry = agents.NewRegistry() }, "provider not available", nil},
		{"not interactive", func(_ *Starter, p *testProvider, _ *StartRequest) { p.capabilities.Interactive = false }, "provider not available", nil},
		{"invalid launch", func(_ *Starter, p *testProvider, _ *StartRequest) {
			p.validate = func(agents.Account, agents.LaunchOptions) error { return errors.New("effort unsupported") }
		}, "effort unsupported", agents.ErrInvalidLaunch},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, p, account := testStarter(t, `exit 0`, interactive)
			req := startRequest(t, account)
			tc.mutate(&s, p, &req)
			_, err := s.Start(context.Background(), req)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
			if tc.is != nil && !errors.Is(err, tc.is) {
				t.Fatalf("err = %v, want %v", err, tc.is)
			}
			if records, _ := List(s.Paths); len(records) != 0 {
				t.Fatal("rejected launch left a record")
			}
		})
	}
}

func TestStarterLaunchFailureCleansUpRuntime(t *testing.T) {
	s, _, account := testStarter(t, `exit 0`, agents.Capabilities{Interactive: true})
	s.Launch = func(context.Context, Paths, Spec) error { return errors.New("boom") }
	if _, err := s.Start(context.Background(), startRequest(t, account)); err == nil || !strings.Contains(err.Error(), "cannot start agent host") {
		t.Fatalf("err = %v", err)
	}
	if entries, _ := os.ReadDir(s.Sessions.(*agents.FileSessionStore).Root()); len(entries) != 0 {
		t.Fatal("failed launch left a runtime dir")
	}
	// A failed start must release the admission lock.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	lock, err := agents.LockFileContext(ctx, filepath.Join(s.Paths.Records, "admit.lock"))
	if err != nil {
		t.Fatalf("lock still held after failure: %v", err)
	}
	_ = lock.Unlock()
}

func TestStarterLaunchErrorAfterHolderReadyStopsIt(t *testing.T) {
	s, _, account := testStarter(t, `while :; do sleep 1; done`, agents.Capabilities{Interactive: true})
	launch := s.Launch
	var id string
	s.Launch = func(ctx context.Context, p Paths, spec Spec) error {
		if err := launch(ctx, p, spec); err != nil {
			return err
		}
		id = spec.Summary.ID
		return errors.New("launcher failed after ready")
	}
	if _, err := s.Start(context.Background(), startRequest(t, account)); err == nil || !strings.Contains(err.Error(), "launcher failed after ready") {
		t.Fatalf("err = %v", err)
	}
	if id == "" || held(s.Paths.lock(id)) {
		t.Fatalf("holder %q survived failed start", id)
	}
	assertNoRuntimeDirs(t, s)
}

func TestFailedStartReleasesAdmissionBeforeHolderExit(t *testing.T) {
	s, _, account := testStarter(t, `exit 0`, agents.Capabilities{Interactive: true, ConcurrentSameAccount: true})
	ready := make(chan struct{})
	release := make(chan struct{})
	releaseHolder := sync.OnceFunc(func() { close(release) })
	defer releaseHolder()
	first := s
	first.Launch = func(_ context.Context, p Paths, spec Spec) error {
		lock, err := lockFile(p.lock(spec.Summary.ID))
		if err != nil {
			return err
		}
		if err := writeRecord(p, Record{Version: RecordVersion, ID: spec.Summary.ID, Protocol: Version, Socket: p.socket(spec.Summary.ID), Summary: spec.Summary}); err != nil {
			_ = lock.Close()
			return err
		}
		go func() {
			<-release
			_ = lock.Close()
		}()
		close(ready)
		return errors.New("failed after holder record")
	}
	firstDone := make(chan error, 1)
	go func() {
		_, err := first.Start(context.Background(), startRequest(t, account))
		firstDone <- err
	}()
	select {
	case <-ready:
	case err := <-firstDone:
		t.Fatalf("first start failed before launch: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("first start never reached launcher")
	}
	secondLaunched := make(chan struct{})
	second := s
	second.Launch = func(context.Context, Paths, Spec) error {
		close(secondLaunched)
		return errors.New("second launch")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	secondDone := make(chan error, 1)
	go func() {
		_, err := second.Start(ctx, startRequest(t, account))
		secondDone <- err
	}()
	select {
	case <-secondLaunched:
	case err := <-secondDone:
		t.Fatalf("second admission failed before launch: %v", err)
	case <-ctx.Done():
		t.Fatal("second admission waited for failed holder to exit")
	}
	releaseHolder()
	if err := <-firstDone; err == nil || !strings.Contains(err.Error(), "failed after holder record") {
		t.Fatalf("first err = %v", err)
	}
	if err := <-secondDone; err == nil || !strings.Contains(err.Error(), "second launch") {
		t.Fatalf("second err = %v", err)
	}
}

func TestStartersAdmitOneSessionPerAccountAcrossStarters(t *testing.T) {
	for _, name := range []string{"one starter", "starters sharing only paths"} {
		t.Run(name, func(t *testing.T) {
			script, caps := `while :; do sleep 1; done`, agents.Capabilities{Interactive: true}
			first, _, account, accountsDir, sessionsDir := testStarterDirs(t, script, caps)
			second := otherProcess(t, first, script, caps, accountsDir, sessionsDir)
			shared := name != "one starter"
			var wg sync.WaitGroup
			results := make(chan error, 8)
			for i := 0; i < 8; i++ {
				s := first
				if shared && i%2 == 1 {
					s = second
				}
				req := startRequest(t, account)
				wg.Add(1)
				go func() {
					defer wg.Done()
					_, err := s.Start(context.Background(), req)
					results <- err
				}()
			}
			wg.Wait()
			close(results)
			ok := 0
			for err := range results {
				switch {
				case err == nil:
					ok++
				case !errors.Is(err, agents.ErrAccountBusy):
					t.Fatalf("unexpected error: %v", err)
				}
			}
			if ok != 1 {
				t.Fatalf("%d starts succeeded, want 1", ok)
			}
		})
	}
}

func TestStarterEnforcesActiveLimitAcrossStarters(t *testing.T) {
	script, caps := `while :; do sleep 1; done`, agents.Capabilities{Interactive: true, ConcurrentSameAccount: true, ConcurrentCrossAccount: true}
	first, _, account, accountsDir, sessionsDir := testStarterDirs(t, script, caps)
	second := otherProcess(t, first, script, caps, accountsDir, sessionsDir)
	for i := 0; i < ActiveLimit; i++ {
		s := first
		if i%2 == 1 {
			s = second
		}
		if _, err := s.Start(context.Background(), startRequest(t, account)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := second.Start(context.Background(), startRequest(t, account)); err == nil || !strings.Contains(err.Error(), "16 active session limit reached") {
		t.Fatalf("err = %v", err)
	}
}

func TestStarterProviderBusyAcrossAccounts(t *testing.T) {
	s, _, first := testStarter(t, `while :; do sleep 1; done`, agents.Capabilities{Interactive: true, ConcurrentSameAccount: true})
	second, _ := agents.NewAccountID()
	if err := s.Accounts.Create(agents.Account{ID: second, Provider: testProviderID, Name: "second"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Start(context.Background(), startRequest(t, first)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Start(context.Background(), startRequest(t, second)); !errors.Is(err, agents.ErrProviderBusy) {
		t.Fatalf("err = %v", err)
	}
}

func TestStartWaitsOnAnotherProcessHoldingAdmission(t *testing.T) {
	s, _, account := testStarter(t, `exit 0`, agents.Capabilities{Interactive: true})
	if err := os.MkdirAll(s.Paths.Records, 0o700); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), admitHelperEnv+"=1", admitHelperPath+"="+filepath.Join(s.Paths.Records, "admit.lock"))
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = stdin.Close()
		_ = cmd.Wait()
	})
	if line, err := bufio.NewReader(stdout).ReadString('\n'); err != nil || strings.TrimSpace(line) != "locked" {
		t.Fatalf("helper said %q, %v", line, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	_, err = s.Start(ctx, startRequest(t, account))
	if !errors.Is(err, ErrAdmissionBusy) || !errors.Is(err, context.DeadlineExceeded) || !strings.HasPrefix(err.Error(), "another agent is starting; try again") {
		t.Fatalf("err = %v", err)
	}
	if records, _ := List(s.Paths); len(records) != 0 {
		t.Fatal("blocked start left a record")
	}
}

func TestHolderKeepsOriginThroughSummaryRewrites(t *testing.T) {
	s, _, account := testStarter(t, `read line`, agents.Capabilities{Interactive: true})
	s.Origin = "cli"
	started, err := s.Start(context.Background(), startRequest(t, account))
	if err != nil {
		t.Fatal(err)
	}
	if started.Summary.Origin != "cli" {
		t.Fatalf("origin at start = %q", started.Summary.Origin)
	}
	awaitRecordState(t, s.Paths, started.ID, "running")
	if r, _ := Read(s.Paths, started.ID); r.Summary.Origin != "cli" {
		t.Fatalf("origin while running = %q", r.Summary.Origin)
	}
	if err := StopHolder(s.Paths, started, ""); err != nil {
		t.Fatal(err)
	}
	if !Wait(s.Paths, started.ID, 5*time.Second) {
		t.Fatal("holder did not exit")
	}
	r, err := Read(s.Paths, started.ID)
	if err != nil || r.Summary.Active() || r.Summary.Origin != "cli" {
		t.Fatalf("ended record = %+v, %v", r.Summary, err)
	}
}

func awaitRecordState(t *testing.T, paths Paths, id, state string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if r, err := Read(paths, id); err == nil && r.Summary.State == state {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("record %s never reached %s", id, state)
}

func assertAdmissionFree(t *testing.T, s Starter) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	lock, err := agents.LockFileContext(ctx, filepath.Join(s.Paths.Records, "admit.lock"))
	if err != nil {
		t.Fatalf("admission lock still held: %v", err)
	}
	_ = lock.Unlock()
}

func assertNoRuntimeDirs(t *testing.T, s Starter) {
	t.Helper()
	if entries, _ := os.ReadDir(s.Sessions.(*agents.FileSessionStore).Root()); len(entries) != 0 {
		t.Fatal("start left a runtime dir")
	}
}

func TestStartLockErrorsAreDistinguished(t *testing.T) {
	s, _, account := testStarter(t, `exit 0`, agents.Capabilities{Interactive: true})
	t.Run("caller cancel", func(t *testing.T) {
		held, err := agents.LockFileContext(context.Background(), filepath.Join(s.Paths.Records, "admit.lock"))
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = held.Unlock() }()
		ctx, cancel := context.WithCancel(context.Background())
		time.AfterFunc(100*time.Millisecond, cancel)
		_, err = s.Start(ctx, startRequest(t, account))
		if !errors.Is(err, context.Canceled) || errors.Is(err, ErrAdmissionBusy) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("lock file unusable", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), "file")
		if err := os.WriteFile(file, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		broken := s
		broken.Paths.Records = filepath.Join(file, "agents")
		_, err := broken.Start(context.Background(), startRequest(t, account))
		if err == nil || !strings.HasPrefix(err.Error(), "cannot lock agent admission: ") || errors.Is(err, ErrAdmissionBusy) {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestStartPanickingLauncherReleasesLockAndRuntime(t *testing.T) {
	s, _, account := testStarter(t, `exit 0`, agents.Capabilities{Interactive: true})
	s.Launch = func(context.Context, Paths, Spec) error { panic("launcher exploded") }
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("panic was swallowed")
			}
		}()
		_, _ = s.Start(context.Background(), startRequest(t, account))
	}()
	assertAdmissionFree(t, s)
	assertNoRuntimeDirs(t, s)
}

func TestStartCleansUpWhenLaunchLeavesNoRecord(t *testing.T) {
	s, _, account := testStarter(t, `exit 0`, agents.Capabilities{Interactive: true})
	// A launcher that reports ready without a record: Read must fail the
	// start, and the cleanup must still release the lock and the runtime.
	s.Launch = func(context.Context, Paths, Spec) error { return nil }
	if _, err := s.Start(context.Background(), startRequest(t, account)); err == nil || !strings.Contains(err.Error(), "cannot start agent host") {
		t.Fatalf("err = %v", err)
	}
	assertAdmissionFree(t, s)
	assertNoRuntimeDirs(t, s)
}
