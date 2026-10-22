//go:build linux || darwin

package agenthost

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/agentterminal"
)

func fakeLaunchOperationRequest(t *testing.T, id agents.AccountID) OperationStartRequest {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return OperationStartRequest{StartRequest: StartRequest{Project: "project", Tree: "tree", Dir: dir, AccountID: id, Name: "Implement #9", Cols: 80, Rows: 24, Launch: agents.LaunchOptions{Prompt: "private exact approved task"}}, OperationID: "impl-contract/start/1", SessionID: "sess_aabbcc", Task: agents.TaskRef{RepositoryID: 7, IssueID: 101, AttemptID: "impl-contract", Role: "implementation"}}
}
func TestOperationReservationPrecedesLauncherAndFailureRetainsReceipt(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	s, _, account := testStarter(t, "cat", agents.Capabilities{Interactive: true, ConcurrentSameAccount: true})
	req := fakeLaunchOperationRequest(t, account)
	calls := 0
	s.Launch = func(_ context.Context, p Paths, spec Spec) error {
		calls++
		raw, err := os.ReadFile(p.record(string(req.SessionID)))
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(raw, []byte(req.Launch.Prompt)) {
			t.Fatal("prompt on disk")
		}
		r, err := Read(p, string(req.SessionID))
		if err != nil || r.StartPhase != "reserved" || r.StartHash == "" || spec.StartHash != r.StartHash || r.Summary.OperationID != req.OperationID || r.Summary.Task == nil || *r.Summary.Task != req.Task || r.WorkDir != req.Dir || r.Summary.Origin != "tui" {
			t.Fatalf("reservation=%+v spec=%+v err=%v", r, spec, err)
		}
		return context.DeadlineExceeded
	}
	obs, err := s.StartOperation(context.Background(), req)
	if err == nil || calls != 1 {
		t.Fatalf("failed launch=%+v %v calls=%d", obs, err, calls)
	}
	settled, err := QueryOperation(context.Background(), s.Paths, req.SessionID, req.OperationID)
	if err != nil || !settled.Closed || settled.Acknowledged || settled.Record.Summary.State != "failed" {
		t.Fatalf("query=%+v %v", settled, err)
	}
	_, err = s.StartOperation(context.Background(), req)
	if !errors.Is(err, ErrOperationClosed) || calls != 1 {
		t.Fatalf("closed replay relaunched: %v calls=%d", err, calls)
	}
	if err := Prune(s.Paths); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.Paths.record(string(req.SessionID))); err != nil {
		t.Fatal("Prune erased operation receipt", err)
	}
}
func TestOperationConflictingBindingsNeverLaunchAgain(t *testing.T) {
	for _, field := range []string{"operation", "account", "path", "prompt", "task", "dimensions"} {
		t.Run(field, func(t *testing.T) {
			s, _, account := testStarter(t, "cat", agents.Capabilities{Interactive: true})
			req := fakeLaunchOperationRequest(t, account)
			calls := 0
			s.Launch = func(context.Context, Paths, Spec) error { calls++; return errors.New("launcher failed") }
			_, _ = s.StartOperation(context.Background(), req)
			switch field {
			case "operation":
				req.OperationID = "different/start/1"
			case "account":
				otherID, idErr := agents.NewAccountID()
				if idErr != nil {
					t.Fatal(idErr)
				}
				if createErr := s.Accounts.Create(agents.Account{ID: otherID, Provider: testProviderID, Name: "other"}); createErr != nil {
					t.Fatal(createErr)
				}
				req.AccountID = otherID
			case "path":
				req.Dir = t.TempDir()
			case "prompt":
				req.Launch.Prompt = "changed"
			case "task":
				req.Task.IssueID++
			case "dimensions":
				req.Cols++
			}
			if _, err := s.StartOperation(context.Background(), req); !errors.Is(err, ErrOperationConflict) {
				t.Fatalf("changed %s err=%v", field, err)
			}
			if calls != 1 {
				t.Fatalf("calls=%d", calls)
			}
		})
	}
}
func TestOperationQueryAbsentIdentityFencesDelayedServe(t *testing.T) {
	f := newFixture(t, "cat")
	id := agents.SessionID("sess_fedcba")
	operation := "impl-delayed/start/1"
	obs, err := QueryOperation(context.Background(), f.deps.Paths, id, operation)
	if err != nil || !obs.Closed || obs.Acknowledged || obs.Record.Summary.Error != "Implementation holder did not start." {
		t.Fatalf("fence=%+v %v", obs, err)
	}
	spec := Spec{Summary: agentterminal.Summary{ID: string(id), AccountID: f.account.ID, Provider: f.account.Provider, State: "starting", OperationID: operation, Task: &agents.TaskRef{AttemptID: "impl-delayed", Role: "implementation"}}, StartHash: strings.Repeat("a", 64), WorkDir: t.TempDir(), Cols: 80, Rows: 24}
	if err := Serve(context.Background(), spec, f.deps, nil); err == nil {
		t.Fatal("late Serve ran after closed fence")
	}
	again, err := QueryOperation(context.Background(), f.deps.Paths, id, operation)
	if err != nil || !again.Closed || again.Acknowledged {
		t.Fatalf("late Serve removed fence: %+v %v", again, err)
	}
}
func TestOperationCorruptReceiptIsUnknownAndNeverReplaced(t *testing.T) {
	p := testPaths(t)
	if err := p.ensure(); err != nil {
		t.Fatal(err)
	}
	id := agents.SessionID("sess_dead")
	path := p.record(string(id))
	raw := []byte(`{"version":1,broken`)
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := QueryOperation(context.Background(), p, id, "impl-corrupt/start/1"); !errors.Is(err, ErrOperationUnknown) {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(raw, after) {
		t.Fatal("corrupt receipt interpreted as absent")
	}
}
func TestOperationHeldLifetimeLockPreventsFalseClosure(t *testing.T) {
	p := testPaths(t)
	if err := p.ensure(); err != nil {
		t.Fatal(err)
	}
	id := "sess_aa12"
	r := Record{Version: RecordVersion, ID: id, StartHash: strings.Repeat("a", 64), StartPhase: "claimed", Socket: p.socket(id), Protocol: Version, Summary: agentterminal.Summary{ID: id, State: "starting", OperationID: "impl-live/start/1", CreatedAt: time.Now().UTC()}}
	if err := writeRecord(p, r); err != nil {
		t.Fatal(err)
	}
	lock, err := agents.LockFileContext(context.Background(), p.lock(id))
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	obs, err := QueryOperation(ctx, p, agents.SessionID(id), r.Summary.OperationID)
	if !errors.Is(err, ErrOperationUnknown) || obs.Closed {
		t.Fatalf("held lifetime fenced: %+v %v", obs, err)
	}
	raw, _ := os.ReadFile(filepath.Join(p.Records, id+".json"))
	if bytes.Contains(raw, []byte(`"start_phase":"closed"`)) {
		t.Fatal("live receipt fenced closed")
	}
}

type fakeLaunchBarrierProvider struct {
	*testProvider
	entered, release chan struct{}
	prepares         atomic.Int32
}

func (p *fakeLaunchBarrierProvider) PrepareSession(ctx context.Context, r agents.PrepareSessionRequest) (agents.PreparedSession, error) {
	p.prepares.Add(1)
	close(p.entered)
	select {
	case <-p.release:
	case <-ctx.Done():
		return agents.PreparedSession{}, ctx.Err()
	}
	return p.testProvider.PrepareSession(ctx, r)
}
func TestOperationSocketReadyDoesNotAcknowledgeProviderStart(t *testing.T) {
	s, p, account := testStarter(t, "cat", agents.Capabilities{Interactive: true, ConcurrentSameAccount: true})
	blocked := &fakeLaunchBarrierProvider{testProvider: p, entered: make(chan struct{}), release: make(chan struct{})}
	registry := agents.NewRegistry()
	if err := registry.Register(blocked); err != nil {
		t.Fatal(err)
	}
	s.Registry = registry
	s.Launch = InProcess(Deps{Accounts: s.Accounts, Sessions: s.Sessions, Registry: registry, Linger: time.Millisecond})
	req := fakeLaunchOperationRequest(t, account)
	done := make(chan OperationObservation, 1)
	errs := make(chan error, 1)
	go func() { obs, err := s.StartOperation(context.Background(), req); done <- obs; errs <- err }()
	fakeLaunchOperationReceive(t, blocked.entered)
	r, err := Read(s.Paths, string(req.SessionID))
	if err != nil || r.StartPhase == "acknowledged" || r.Summary.StartedAt != nil {
		close(blocked.release)
		t.Fatalf("socket acknowledged provider: %+v %v", r, err)
	}
	close(blocked.release)
	obs := fakeLaunchOperationReceive(t, done)
	if err := fakeLaunchOperationReceive(t, errs); err != nil {
		t.Fatal(err)
	}
	c, _, err := Dial(obs.Record, Request{Mode: ModeStatus, Client: "test"})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	readUntil(t, c, func(f Frame, _ string) bool { return f.Status != nil && f.Status.State == "running" })
	actual, err := QueryOperation(context.Background(), s.Paths, req.SessionID, req.OperationID)
	if err != nil || !actual.Acknowledged || actual.Record.Summary.OperationID != req.OperationID || actual.Record.Summary.Task == nil {
		t.Fatalf("actual start=%+v %v", actual, err)
	}
	again, replayErr := s.StartOperation(context.Background(), req)
	if replayErr != nil || again.Record.ID != string(req.SessionID) {
		t.Fatalf("successful operation replay=%+v %v", again, replayErr)
	}
	if blocked.prepares.Load() != 1 {
		t.Fatal("provider prepared twice")
	}
}

func TestOperationConcurrentIdentityInvokesLauncherOnce(t *testing.T) {
	s, _, account := testStarter(t, "cat", agents.Capabilities{Interactive: true, ConcurrentSameAccount: true})
	req := fakeLaunchOperationRequest(t, account)
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	s.Launch = func(context.Context, Paths, Spec) error {
		if calls.Add(1) == 1 {
			close(entered)
		}
		<-release
		return context.DeadlineExceeded
	}
	errs := make(chan error, 2)
	go func() { _, err := s.StartOperation(context.Background(), req); errs <- err }()
	fakeLaunchOperationReceive(t, entered)
	secondStarted := make(chan struct{})
	go func() { close(secondStarted); _, err := s.StartOperation(context.Background(), req); errs <- err }()
	fakeLaunchOperationReceive(t, secondStarted)
	close(release)
	for i := 0; i < 2; i++ {
		if err := fakeLaunchOperationReceive(t, errs); err == nil {
			t.Fatal("failed launch reported success")
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("duplicate launcher calls=%d", calls.Load())
	}
	obs, err := QueryOperation(context.Background(), s.Paths, req.SessionID, req.OperationID)
	if err != nil || !obs.Closed {
		t.Fatalf("concurrent outcome=%+v %v", obs, err)
	}
}

func TestOperationPanicAndCancellationLeaveQueryableIdentity(t *testing.T) {
	for _, mode := range []string{"panic", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			s, _, account := testStarter(t, "cat", agents.Capabilities{Interactive: true})
			req := fakeLaunchOperationRequest(t, account)
			calls := 0
			s.Launch = func(context.Context, Paths, Spec) error {
				calls++
				if mode == "panic" {
					panic("fixture launcher crashed")
				}
				return context.Canceled
			}
			func() { defer func() { _ = recover() }(); _, _ = s.StartOperation(context.Background(), req) }()
			if calls != 1 {
				t.Fatalf("launcher fault not reached: %d", calls)
			}
			if _, err := os.Stat(s.Paths.record(string(req.SessionID))); err != nil {
				t.Fatal("crash erased receipt before query", err)
			}
			obs, err := QueryOperation(context.Background(), s.Paths, req.SessionID, req.OperationID)
			if err != nil || !obs.Closed || obs.Acknowledged {
				t.Fatalf("crash identity lost=%+v %v", obs, err)
			}
		})
	}
}

func TestOperationReceiptsSurviveAgeAndEndedCountPruning(t *testing.T) {
	p := testPaths(t)
	if err := p.ensure(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 40; i++ {
		id := fmt.Sprintf("sess_%04x", i)
		old := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
		r := Record{Version: RecordVersion, ID: id, StartHash: strings.Repeat("a", 64), StartPhase: "closed", Summary: agentterminal.Summary{ID: id, State: "failed", OperationID: fmt.Sprintf("impl-prune-%d/start/1", i), CreatedAt: old, EndedAt: &old}}
		if err := writeRecord(p, r); err != nil {
			t.Fatal(err)
		}
	}
	if err := Prune(p); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 40; i++ {
		id := fmt.Sprintf("sess_%04x", i)
		if _, err := os.Stat(p.record(id)); err != nil {
			t.Fatalf("Prune erased receipt %s: %v", id, err)
		}
	}
}

func TestOperationUnheldReservationStaysStartingUntilQueryFences(t *testing.T) {
	p := testPaths(t)
	if err := p.ensure(); err != nil {
		t.Fatal(err)
	}
	id := "sess_9988"
	r := Record{Version: RecordVersion, ID: id, StartHash: strings.Repeat("a", 64), StartPhase: "reserved", Summary: agentterminal.Summary{ID: id, State: "starting", OperationID: "impl-reserved/start/1", CreatedAt: time.Now().UTC()}}
	if err := writeRecord(p, r); err != nil {
		t.Fatal(err)
	}
	before, err := Read(p, id)
	if err != nil || before.Summary.State != "starting" {
		t.Fatalf("ordinary Read fabricated lost status: %+v %v", before, err)
	}
	if err := Remove(p, id); err == nil {
		t.Fatal("Remove erased unfenced reservation")
	}
	after, err := QueryOperation(context.Background(), p, agents.SessionID(id), r.Summary.OperationID)
	if err != nil || !after.Closed || after.Record.Summary.State != "failed" {
		t.Fatalf("fenced reservation=%+v %v", after, err)
	}
}

func fakeLaunchOperationReceive[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for operation synchronization")
		var zero T
		return zero
	}
}
