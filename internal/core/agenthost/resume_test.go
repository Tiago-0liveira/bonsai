//go:build linux || darwin

package agenthost

import (
	"context"
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

type resumeProvider struct {
	*testProvider
	resolve func(context.Context, agents.ResumeRequest) (agents.ResumeTarget, error)
}

func (p *resumeProvider) ResumeTarget(ctx context.Context, req agents.ResumeRequest) (agents.ResumeTarget, error) {
	return p.resolve(ctx, req)
}

func resumeFixture(t *testing.T) (Starter, *resumeProvider, Record) {
	t.Helper()
	s, base, accountID := testStarter(t, "exit 0", agents.Capabilities{Interactive: true, ConcurrentSameAccount: true, ConcurrentCrossAccount: true})
	p := &resumeProvider{testProvider: base, resolve: func(context.Context, agents.ResumeRequest) (agents.ResumeTarget, error) {
		return agents.ResumeTarget{ID: "conversation"}, nil
	}}
	s.Registry = agents.NewRegistry()
	if err := s.Registry.Register(p); err != nil {
		t.Fatal(err)
	}
	if err := s.Paths.ensure(); err != nil {
		t.Fatal(err)
	}
	account, err := s.Accounts.Get(accountID)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := s.Sessions.Create(account, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Add(-time.Hour)
	exit := 1
	r := Record{Version: RecordVersion, ID: string(runtime.ID), WorkDir: runtime.WorkDir, Generation: "old", Summary: Summary{ID: string(runtime.ID), AccountID: accountID, Provider: testProviderID, ProjectID: "project", WorktreeID: "tree", Name: "kept name", Model: "kept model", Options: map[string]string{"effort": "high", "read_only": "true"}, CreatedAt: now, StartedAt: &now, EndedAt: &now, ExitCode: &exit, Error: LostError, State: "failed", ProviderSessionID: "conversation", Origin: "web"}}
	if err := writeRecord(s.Paths, r); err != nil {
		t.Fatal(err)
	}
	return s, p, r
}

// A fake holder takes the real liveness lock and writes its record before ready.
func resumeLauncher(t *testing.T, seen func(Spec), result error) Launcher {
	t.Helper()
	return func(_ context.Context, paths Paths, spec Spec) error {
		lock, err := lockFile(paths.lock(spec.Summary.ID))
		if err != nil {
			return err
		}
		t.Cleanup(func() { _ = lock.Close() })
		if seen != nil {
			seen(spec)
		}
		if err := writeRecord(paths, Record{Version: RecordVersion, ID: spec.Summary.ID, WorkDir: spec.WorkDir, Generation: "new", Summary: spec.Summary}); err != nil {
			return err
		}
		return result
	}
}

func TestResumeRelaunchesSameID(t *testing.T) {
	s, p, r := resumeFixture(t)
	other := r
	other.ID = "sess_abc"
	other.Summary.ID = other.ID
	other.Summary.ProviderSessionID = "claimed"
	if err := writeRecord(s.Paths, other); err != nil {
		t.Fatal(err)
	}
	p.resolve = func(_ context.Context, req agents.ResumeRequest) (agents.ResumeTarget, error) {
		if string(req.Session.ID) != r.ID || req.Account.ID != r.Summary.AccountID || req.ProviderSessionID != "conversation" || !req.StartedAt.Equal(*r.Summary.StartedAt) || !reflect.DeepEqual(req.Claimed, []string{"claimed"}) {
			t.Errorf("request = %+v", req)
		}
		return agents.ResumeTarget{ID: "discovered"}, nil
	}
	s.Launch = resumeLauncher(t, func(spec Spec) {
		want := r.Summary
		want.State = "starting"
		want.Error = ""
		want.StartedAt = nil
		want.EndedAt = nil
		want.ExitCode = nil
		want.Activity = nil
		want.ProviderSessionID = "discovered"
		want.Origin = "tui"
		if !reflect.DeepEqual(spec.Summary, want) || spec.WorkDir != r.WorkDir || spec.Cols != 80 || spec.Rows != 24 || spec.Resume == nil || spec.Resume.ID != "discovered" || spec.StartHash != "" {
			t.Errorf("spec = %+v", spec)
		}
		if want := agents.LaunchOptionsFromValues(r.Summary.Model, r.Summary.Name, r.Summary.Options); !reflect.DeepEqual(spec.Launch, want) || spec.Launch.Prompt != "" {
			t.Errorf("launch = %+v", spec.Launch)
		}
	}, nil)
	got, err := s.Resume(context.Background(), r.ID, 80, 24)
	if err != nil || !got.Alive || got.ID != r.ID || got.Generation == r.Generation {
		t.Fatalf("resume = %+v, %v", got, err)
	}
}

type renameResumeProvider struct {
	*resumeProvider
	launchNames chan string
}

func (p *renameResumeProvider) PrepareSession(_ context.Context, req agents.PrepareSessionRequest) (agents.PreparedSession, error) {
	p.launchNames <- req.Launch.DisplayName
	return agents.PreparedSession{}, errors.New("stop after observing launch name")
}

func TestResumePreservesRenameDuringResolution(t *testing.T) {
	s, p, r := resumeFixture(t)
	resolving, release := make(chan struct{}), make(chan struct{})
	p.resolve = func(ctx context.Context, _ agents.ResumeRequest) (agents.ResumeTarget, error) {
		close(resolving)
		select {
		case <-release:
			return agents.ResumeTarget{ID: "conversation"}, nil
		case <-ctx.Done():
			return agents.ResumeTarget{}, ctx.Err()
		}
	}
	provider := &renameResumeProvider{resumeProvider: p, launchNames: make(chan string, 1)}
	s.Registry = agents.NewRegistry()
	if err := s.Registry.Register(provider); err != nil {
		t.Fatal(err)
	}
	s.Launch = InProcess(Deps{Accounts: s.Accounts, Sessions: s.Sessions, Registry: s.Registry, Linger: time.Millisecond})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	type result struct {
		record Record
		err    error
	}
	done := make(chan result, 1)
	go func() {
		record, err := s.Resume(ctx, r.ID, 80, 24)
		done <- result{record, err}
	}()
	select {
	case <-resolving:
	case <-ctx.Done():
		t.Fatal("resume never reached provider resolution")
	}
	const name = "renamed during resume"
	if renamed, err := Rename(s.Paths, r.ID, name); err != nil || renamed.Name != name {
		t.Fatalf("rename = %+v, %v", renamed, err)
	}
	close(release)
	select {
	case got := <-done:
		if got.err != nil || got.record.Summary.Name != name {
			t.Fatalf("resume = %+v, %v", got.record, got.err)
		}
	case <-ctx.Done():
		t.Fatal("resume did not finish")
	}
	select {
	case got := <-provider.launchNames:
		if got != name {
			t.Fatalf("launch DisplayName = %q, want %q", got, name)
		}
	case <-ctx.Done():
		t.Fatal("holder never prepared the provider session")
	}
	if got, err := Read(s.Paths, r.ID); err != nil || got.Summary.Name != name {
		t.Fatalf("resumed record = %+v, %v", got, err)
	}
}

func TestResumeAliveReturnsRecord(t *testing.T) {
	s, _, r := resumeFixture(t)
	lock, err := lockFile(s.Paths.lock(r.ID))
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	s.Accounts = nil
	s.Registry = nil
	s.Sessions = nil
	got, err := s.Resume(context.Background(), r.ID, 0, 0)
	r.Alive = true
	if err != nil || !reflect.DeepEqual(got, r) {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestResumeRefusals(t *testing.T) {
	cases := []struct {
		name, reason string
		mutate       func(*testing.T, *Starter, *resumeProvider, *Record)
	}{
		{"garden", agents.ReasonGarden, func(_ *testing.T, _ *Starter, _ *resumeProvider, r *Record) { r.Summary.OperationID = "operation" }},
		{"reserved", agents.ReasonNeverStarted, func(_ *testing.T, _ *Starter, _ *resumeProvider, r *Record) { r.StartPhase = phaseReserved }},
		{"claimed", agents.ReasonNeverStarted, func(_ *testing.T, _ *Starter, _ *resumeProvider, r *Record) { r.StartPhase = phaseClaimed }},
		{"worktree", agents.ReasonWorktreeGone, func(_ *testing.T, _ *Starter, _ *resumeProvider, r *Record) { r.WorkDir += "/missing" }},
		{"worktree file", agents.ReasonWorktreeGone, func(t *testing.T, _ *Starter, _ *resumeProvider, r *Record) {
			r.WorkDir += "/file"
			if err := os.WriteFile(r.WorkDir, nil, 0600); err != nil {
				t.Fatal(err)
			}
		}},
		{"profile", agents.ReasonProfileGone, func(_ *testing.T, _ *Starter, _ *resumeProvider, r *Record) { r.Summary.AccountID = "acct_missing" }},
		{"provider", agents.ReasonProviderMissing, func(_ *testing.T, s *Starter, _ *resumeProvider, _ *Record) { s.Registry = agents.NewRegistry() }},
		{"unsupported", agents.ReasonUnsupported, func(t *testing.T, s *Starter, p *resumeProvider, _ *Record) {
			s.Registry = agents.NewRegistry()
			if err := s.Registry.Register(p.testProvider); err != nil {
				t.Fatal(err)
			}
		}},
		{"no conversation", agents.ReasonNoConversation, func(_ *testing.T, _ *Starter, p *resumeProvider, _ *Record) {
			p.resolve = func(context.Context, agents.ResumeRequest) (agents.ResumeTarget, error) {
				return agents.ResumeTarget{}, agents.NotResumable(agents.ReasonNoConversation)
			}
		}},
		{"not found", agents.ReasonNotFound, func(_ *testing.T, _ *Starter, p *resumeProvider, _ *Record) {
			p.resolve = func(context.Context, agents.ResumeRequest) (agents.ResumeTarget, error) {
				return agents.ResumeTarget{}, agents.NotResumable(agents.ReasonNotFound)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, p, r := resumeFixture(t)
			tc.mutate(t, &s, p, &r)
			if err := writeRecord(s.Paths, r); err != nil {
				t.Fatal(err)
			}
			s.Launch = func(context.Context, Paths, Spec) error { t.Fatal("launched refused session"); return nil }
			_, err := s.Resume(context.Background(), r.ID, 80, 24)
			var refusal *agents.NotResumableError
			if !errors.As(err, &refusal) || refusal.Reason != tc.reason {
				t.Fatalf("err = %v", err)
			}
			got, err := loadRecord(s.Paths, r.ID)
			if err != nil || !reflect.DeepEqual(got, r) {
				t.Fatalf("record changed: %+v, %v", got, err)
			}
		})
	}
}

func TestResumeRefusesOperationSession(t *testing.T) {
	s, _, r := resumeFixture(t)
	r.Summary.OperationID = "garden"
	r.StartHash = "receipt"
	r.StartPhase = phaseClosed
	if err := writeRecord(s.Paths, r); err != nil {
		t.Fatal(err)
	}
	_, err := s.Resume(context.Background(), r.ID, 80, 24)
	got, readErr := loadRecord(s.Paths, r.ID)
	if !errors.Is(err, agents.ErrNotResumable) || readErr != nil || !reflect.DeepEqual(got, r) {
		t.Fatalf("record %+v, errors %v / %v", got, err, readErr)
	}
}

func TestResumeRecreatesRuntime(t *testing.T) {
	s, _, r := resumeFixture(t)
	runtime, _ := s.Sessions.Get(agents.SessionID(r.ID))
	if err := s.Sessions.Cleanup(runtime); err != nil {
		t.Fatal(err)
	}
	s.Launch = resumeLauncher(t, func(Spec) {
		got, err := s.Sessions.Get(agents.SessionID(r.ID))
		if err != nil || got.WorkDir != r.WorkDir || got.ID != runtime.ID {
			t.Errorf("runtime = %+v, %v", got, err)
		}
	}, nil)
	if _, err := s.Resume(context.Background(), r.ID, 80, 24); err != nil {
		t.Fatal(err)
	}
}

func TestResumeAfterCrashMidResume(t *testing.T) {
	s, _, r := resumeFixture(t)
	r.Summary.State = "starting"
	r.Summary.EndedAt = nil
	r.Summary.Error = ""
	if err := writeRecord(s.Paths, r); err != nil {
		t.Fatal(err)
	}
	s.Launch = resumeLauncher(t, nil, nil)
	if got, err := s.Resume(context.Background(), r.ID, 80, 24); err != nil || !got.Alive {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestResumeLegacyRecord(t *testing.T) {
	s, p, r := resumeFixture(t)
	runtime, _ := s.Sessions.Get(agents.SessionID(r.ID))
	if err := s.Sessions.Cleanup(runtime); err != nil {
		t.Fatal(err)
	}
	r.Summary.ProviderSessionID = ""
	r.Summary.StartedAt = nil
	r.Generation = ""
	if err := writeRecord(s.Paths, r); err != nil {
		t.Fatal(err)
	}
	p.resolve = func(_ context.Context, req agents.ResumeRequest) (agents.ResumeTarget, error) {
		if req.ProviderSessionID != "" || !req.StartedAt.Equal(r.Summary.CreatedAt) {
			t.Errorf("request = %+v", req)
		}
		return agents.ResumeTarget{ID: "found"}, nil
	}
	s.Launch = resumeLauncher(t, func(spec Spec) {
		if spec.Resume.ID != "found" {
			t.Error("lost target")
		}
	}, nil)
	if _, err := s.Resume(context.Background(), r.ID, 80, 24); err != nil {
		t.Fatal(err)
	}
}

func TestResumeConcurrent(t *testing.T) {
	s, p, r := resumeFixture(t)
	p.capabilities.ConcurrentSameAccount = false
	var launches atomic.Int32
	s.Launch = resumeLauncher(t, func(Spec) { launches.Add(1) }, nil)
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	begin := make(chan struct{})
	for range 2 {
		wg.Go(func() { <-begin; _, err := s.Resume(context.Background(), r.ID, 80, 24); errs <- err })
	}
	close(begin)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if launches.Load() != 1 {
		t.Fatalf("launches = %d", launches.Load())
	}
}

func TestResumeLauncherErrorWithLiveHolderSucceeds(t *testing.T) {
	s, _, r := resumeFixture(t)
	s.Launch = resumeLauncher(t, nil, errors.New("arbitrary launcher failure"))
	if got, err := s.Resume(context.Background(), r.ID, 80, 24); err != nil || !got.Alive {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestResumeFailureRuntimeOwnership(t *testing.T) {
	for _, created := range []bool{false, true} {
		for _, failure := range []string{"resolve", "launch", "panic", "cancel"} {
			t.Run(failure+map[bool]string{false: " existing", true: " recreated"}[created], func(t *testing.T) {
				s, p, r := resumeFixture(t)
				runtime, _ := s.Sessions.Get(agents.SessionID(r.ID))
				if created {
					if err := s.Sessions.Cleanup(runtime); err != nil {
						t.Fatal(err)
					}
				}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				if failure == "resolve" {
					p.resolve = func(context.Context, agents.ResumeRequest) (agents.ResumeTarget, error) {
						return agents.ResumeTarget{}, agents.NotResumable(agents.ReasonNotFound)
					}
				}
				if failure == "cancel" {
					p.resolve = func(context.Context, agents.ResumeRequest) (agents.ResumeTarget, error) {
						cancel()
						return agents.ResumeTarget{ID: "conversation"}, nil
					}
				}
				s.Launch = func(context.Context, Paths, Spec) error {
					if failure == "panic" {
						panic("boom")
					}
					return errors.New("boom")
				}
				func() {
					if failure == "panic" {
						defer func() {
							if recover() == nil {
								t.Error("panic swallowed")
							}
						}()
					}
					if _, err := s.Resume(ctx, r.ID, 80, 24); err == nil {
						t.Error("expected failure")
					}
				}()
				_, err := s.Sessions.Get(agents.SessionID(r.ID))
				if created && !errors.Is(err, agents.ErrSessionNotFound) || !created && err != nil {
					t.Fatalf("runtime ownership: %v", err)
				}
				got, err := loadRecord(s.Paths, r.ID)
				if err != nil || !reflect.DeepEqual(got, r) {
					t.Fatalf("record changed: %+v, %v", got, err)
				}
				assertAdmissionFree(t, s)
			})
		}
	}
}

func TestResumeMissingRecord(t *testing.T) {
	s, _, _ := resumeFixture(t)
	if _, err := s.Resume(context.Background(), "sess_abcdef", 80, 24); !errors.Is(err, agents.ErrSessionNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestResumeAdmission(t *testing.T) {
	for _, kind := range []string{"account", "provider", "limit"} {
		t.Run(kind, func(t *testing.T) {
			s, p, r := resumeFixture(t)
			other := r
			other.ID = "sess_abc"
			other.Summary.ID = other.ID
			other.Summary.State = "running"
			want := agents.ErrAccountBusy
			switch kind {
			case "account":
				p.capabilities.ConcurrentSameAccount = false
			case "provider":
				p.capabilities.ConcurrentCrossAccount = false
				other.Summary.AccountID = "acct_other"
				want = agents.ErrProviderBusy
			case "limit":
				want = nil
			}
			count := 1
			if kind == "limit" {
				count = ActiveLimit
			}
			for i := range count {
				other.ID = fmt.Sprintf("sess_%x", i)
				other.Summary.ID = other.ID
				lock, err := lockFile(s.Paths.lock(other.ID))
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = lock.Close() })
				if err := writeRecord(s.Paths, other); err != nil {
					t.Fatal(err)
				}
			}
			p.resolve = func(context.Context, agents.ResumeRequest) (agents.ResumeTarget, error) {
				t.Fatal("admission did not refuse before provider resolution")
				return agents.ResumeTarget{}, nil
			}
			_, err := s.Resume(context.Background(), r.ID, 80, 24)
			if want != nil && !errors.Is(err, want) || want == nil && (err == nil || !strings.Contains(err.Error(), "active session limit reached")) {
				t.Fatalf("err = %v", err)
			}
			assertAdmissionFree(t, s)
		})
	}
}

func TestResumeEmptyTargetKeepsKnownID(t *testing.T) {
	s, p, r := resumeFixture(t)
	p.resolve = func(context.Context, agents.ResumeRequest) (agents.ResumeTarget, error) {
		return agents.ResumeTarget{}, nil
	}
	s.Launch = resumeLauncher(t, func(spec Spec) {
		if spec.Resume == nil || spec.Resume.ID != "" || spec.Summary.ProviderSessionID != r.Summary.ProviderSessionID {
			t.Errorf("spec = %+v", spec)
		}
	}, nil)
	if _, err := s.Resume(context.Background(), r.ID, 80, 24); err != nil {
		t.Fatal(err)
	}
}

func TestResumeFailedHolderKeepsRecreatedRuntime(t *testing.T) {
	s, _, r := resumeFixture(t)
	runtime, err := s.Sessions.Get(agents.SessionID(r.ID))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Sessions.Cleanup(runtime); err != nil {
		t.Fatal(err)
	}
	boom := errors.New("launcher lost readiness")
	s.Launch = func(_ context.Context, paths Paths, spec Spec) error {
		ended := Record{Version: RecordVersion, ID: r.ID, WorkDir: spec.WorkDir, Generation: "new", Summary: spec.Summary}
		ended.Summary.State = "failed"
		ended.Summary.Error = "provider exited"
		if err := writeRecord(paths, ended); err != nil {
			t.Fatal(err)
		}
		return boom
	}
	if _, err := s.Resume(context.Background(), r.ID, 80, 24); !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
	if _, err := s.Sessions.Get(agents.SessionID(r.ID)); err != nil {
		t.Fatalf("holder's runtime removed: %v", err)
	}
	got, err := Read(s.Paths, r.ID)
	if err != nil || got.Summary.Error != "provider exited" {
		t.Fatalf("record = %+v, %v", got, err)
	}
}
