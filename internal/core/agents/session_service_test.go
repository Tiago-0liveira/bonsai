package agents

import (
	"context"
	"errors"
	"testing"
	"time"
)

type lifecycleSessionStore struct {
	events     *[]string
	session    Session
	cleanupErr error
}

func (s *lifecycleSessionStore) Create(account Account, workDir string) (Session, error) {
	*s.events = append(*s.events, "create")
	out := s.session
	out.AccountID = account.ID
	out.Provider = account.Provider
	out.WorkDir = workDir
	return out, nil
}
func (s *lifecycleSessionStore) Get(SessionID) (Session, error) { return s.session, nil }
func (s *lifecycleSessionStore) Cleanup(Session) error {
	*s.events = append(*s.events, "cleanup")
	return s.cleanupErr
}

type lifecycleProvider struct {
	events      *[]string
	prepareErr  error
	finalizeErr error
}

func (p *lifecycleProvider) ID() ProviderID             { return "fake" }
func (p *lifecycleProvider) Capabilities() Capabilities { return Capabilities{Interactive: true} }
func (p *lifecycleProvider) SetupAccount(context.Context, SetupRequest) (SetupResult, error) {
	return SetupResult{}, nil
}
func (p *lifecycleProvider) PrepareSession(_ context.Context, req PrepareSessionRequest) (PreparedSession, error) {
	*p.events = append(*p.events, "prepare")
	if p.prepareErr != nil {
		return PreparedSession{}, p.prepareErr
	}
	return PreparedSession{Executable: "fake", Dir: req.Session.WorkDir}, nil
}
func (p *lifecycleProvider) FinalizeSession(context.Context, FinalizeSessionRequest) error {
	*p.events = append(*p.events, "finalize")
	return p.finalizeErr
}
func (p *lifecycleProvider) Usage(context.Context, Account, UsageOptions) (UsageSnapshot, error) {
	return UsageSnapshot{}, nil
}

type lifecycleLauncher struct {
	events *[]string
	err    error
}

func (l *lifecycleLauncher) RunForeground(context.Context, PreparedSession) error {
	*l.events = append(*l.events, "run")
	return l.err
}

func TestSessionServiceLifecycleOrdering(t *testing.T) {
	store, err := NewFileAccountStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	account := testAccount("acct_run", "fake", "personal")
	if err := store.Create(account); err != nil {
		t.Fatal(err)
	}
	events := []string{}
	sessions := &lifecycleSessionStore{
		events:  &events,
		session: Session{ID: "sess_run", RuntimeDir: t.TempDir(), HomeDir: t.TempDir(), CreatedAt: time.Now()},
	}
	provider := &lifecycleProvider{events: &events}
	registry := NewRegistry()
	if err := registry.Register(provider); err != nil {
		t.Fatal(err)
	}
	service := &SessionService{
		Accounts: store, Sessions: sessions, Registry: registry,
		Launcher: &lifecycleLauncher{events: &events},
	}
	if err := service.RunForeground(context.Background(), account.ID, t.TempDir(), []string{"--x"}); err != nil {
		t.Fatal(err)
	}
	want := []string{"create", "prepare", "run", "finalize", "cleanup"}
	if len(events) != len(want) {
		t.Fatalf("events = %v", events)
	}
	for i := range want {
		if events[i] != want[i] {
			t.Fatalf("events = %v, want %v", events, want)
		}
	}
}

func TestSessionServiceFailurePathsStillCleanup(t *testing.T) {
	store, err := NewFileAccountStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	account := testAccount("acct_fail", "fake", "personal")
	if err := store.Create(account); err != nil {
		t.Fatal(err)
	}
	prepareSentinel := errors.New("prepare failed")
	cleanupSentinel := errors.New("cleanup failed")
	events := []string{}
	sessions := &lifecycleSessionStore{
		events: &events, cleanupErr: cleanupSentinel,
		session: Session{ID: "sess_fail", RuntimeDir: t.TempDir(), HomeDir: t.TempDir()},
	}
	provider := &lifecycleProvider{events: &events, prepareErr: prepareSentinel}
	registry := NewRegistry()
	_ = registry.Register(provider)
	service := &SessionService{
		Accounts: store, Sessions: sessions, Registry: registry,
		Launcher: &lifecycleLauncher{events: &events},
	}
	err = service.RunForeground(context.Background(), account.ID, t.TempDir(), nil)
	if !errors.Is(err, prepareSentinel) || !errors.Is(err, cleanupSentinel) {
		t.Fatalf("joined error = %v", err)
	}
	want := []string{"create", "prepare", "cleanup"}
	if len(events) != len(want) {
		t.Fatalf("events = %v, want %v", events, want)
	}
	for i := range want {
		if events[i] != want[i] {
			t.Fatalf("events = %v, want %v", events, want)
		}
	}
}


func TestSessionServiceRunFinalizeAndCleanupErrorsAreJoined(t *testing.T) {
	store, err := NewFileAccountStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	account := testAccount("acct_joined", "fake", "personal")
	if err := store.Create(account); err != nil {
		t.Fatal(err)
	}
	runSentinel := errors.New("run failed")
	finalizeSentinel := errors.New("finalize failed")
	cleanupSentinel := errors.New("cleanup failed")
	events := []string{}
	sessions := &lifecycleSessionStore{
		events: &events, cleanupErr: cleanupSentinel,
		session: Session{ID: "sess_joined", RuntimeDir: t.TempDir(), HomeDir: t.TempDir()},
	}
	provider := &lifecycleProvider{events: &events, finalizeErr: finalizeSentinel}
	registry := NewRegistry()
	if err := registry.Register(provider); err != nil {
		t.Fatal(err)
	}
	service := &SessionService{
		Accounts: store, Sessions: sessions, Registry: registry,
		Launcher: &lifecycleLauncher{events: &events, err: runSentinel},
	}

	err = service.RunForeground(context.Background(), account.ID, t.TempDir(), nil)
	for _, sentinel := range []error{runSentinel, finalizeSentinel, cleanupSentinel} {
		if !errors.Is(err, sentinel) {
			t.Fatalf("joined error %v does not contain %v", err, sentinel)
		}
	}
	want := []string{"create", "prepare", "run", "finalize", "cleanup"}
	if len(events) != len(want) {
		t.Fatalf("events = %v, want %v", events, want)
	}
	for i := range want {
		if events[i] != want[i] {
			t.Fatalf("events = %v, want %v", events, want)
		}
	}
}
