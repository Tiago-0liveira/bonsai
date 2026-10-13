// Package agentterminal owns API-lifetime provider processes and bounded terminal streams.
package agentterminal

import (
	"context"
	"fmt"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

const OutputLimit = 2 << 20
const InputLimit = 64 << 10

type Summary struct {
	ID          string            `json:"id"`
	ProjectID   string            `json:"project_id"`
	WorktreeID  string            `json:"worktree_id"`
	AccountID   agents.AccountID  `json:"account_id"`
	Provider    agents.ProviderID `json:"provider"`
	ProfileName string            `json:"profile_name"`
	Name        string            `json:"name"`
	State       string            `json:"state"`
	StartedAt   *time.Time        `json:"started_at,omitempty"`
	CreatedAt   time.Time         `json:"created_at"`
	EndedAt     *time.Time        `json:"ended_at,omitempty"`
	ExitCode    *int              `json:"exit_code,omitempty"`
	Error       string            `json:"error,omitempty"`
	// ProviderSessionID is the provider's own session ID, for a later resume.
	ProviderSessionID string `json:"provider_session_id,omitempty"`
	// Model and Options are the accepted launch choices (Options keyed by the
	// start request field, such as effort). The prompt is never recorded.
	Model   string            `json:"model,omitempty"`
	Options map[string]string `json:"options,omitempty"`
}

func (s Summary) Active() bool {
	return s.State == "starting" || s.State == "running" || s.State == "stopping"
}

// Frame is one agent terminal message; its status is the session summary.
type Frame = StreamFrame[Summary]

type session struct {
	summary Summary
	workDir string
	cancel  context.CancelFunc
	done    chan struct{}
	stream  *Stream[Summary]
}
type Manager struct {
	mu       sync.Mutex
	accounts agents.AccountStore
	sessions agents.SessionStore
	registry *agents.Registry
	entries  map[string]*session
	closed   bool
	changed  func(string)
}

func New(accounts agents.AccountStore, sessions agents.SessionStore, registry *agents.Registry, changed func(string)) *Manager {
	return &Manager{accounts: accounts, sessions: sessions, registry: registry, entries: map[string]*session{}, changed: changed}
}
func Dimensions(cols, rows int) bool { return cols >= 1 && cols <= 500 && rows >= 1 && rows <= 500 }
func (m *Manager) notify(project string) {
	if m.changed != nil {
		m.changed(project)
	}
}

// StartOptions are the per-launch options forwarded to the provider unchanged.
type StartOptions = agents.LaunchOptions

func (m *Manager) Start(project, tree string, accountID agents.AccountID, name, dir string, cols, rows int, options ...StartOptions) (Summary, error) {
	if !Supported {
		return Summary{}, fmt.Errorf("interactive terminals unsupported on this platform")
	}
	if !Dimensions(cols, rows) {
		return Summary{}, fmt.Errorf("terminal dimensions must be between 1 and 500")
	}
	account, err := m.accounts.Get(accountID)
	if err != nil {
		return Summary{}, fmt.Errorf("profile unavailable")
	}
	provider, err := m.registry.Get(account.Provider)
	if err != nil {
		return Summary{}, fmt.Errorf("provider not available")
	}
	capabilities := provider.Capabilities()
	if !capabilities.Interactive {
		return Summary{}, fmt.Errorf("provider not available")
	}
	var launch agents.LaunchOptions
	if len(options) > 0 {
		launch = options[0]
	}
	launch.DisplayName = name
	if validator, ok := provider.(agents.LaunchValidator); ok {
		if err := validator.ValidateLaunch(account, launch); err != nil {
			return Summary{}, fmt.Errorf("%w: %v", agents.ErrInvalidLaunch, err)
		}
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return Summary{}, fmt.Errorf("server shutting down")
	}
	active := 0
	for _, e := range m.entries {
		if !e.summary.Active() {
			continue
		}
		active++
		if e.summary.Provider != account.Provider {
			continue
		}
		if e.summary.AccountID == accountID && !capabilities.ConcurrentSameAccount {
			m.mu.Unlock()
			return Summary{}, agents.ErrAccountBusy
		}
		if e.summary.AccountID != accountID && !capabilities.ConcurrentCrossAccount {
			m.mu.Unlock()
			return Summary{}, agents.ErrProviderBusy
		}
	}
	if active >= 16 {
		m.mu.Unlock()
		return Summary{}, fmt.Errorf("16 active session limit reached")
	}
	runtime, err := m.sessions.Create(account, dir)
	if err != nil {
		m.mu.Unlock()
		return Summary{}, fmt.Errorf("cannot create session")
	}
	if name == "" {
		name = account.Name
	}
	ctx, cancel := context.WithCancel(context.Background())
	entry := &session{summary: Summary{ID: string(runtime.ID), ProjectID: project, WorktreeID: tree, AccountID: accountID, Provider: account.Provider, ProfileName: account.Name, Name: name, State: "starting", CreatedAt: time.Now().UTC(), Model: launch.Model, Options: launch.Values()}, workDir: dir, cancel: cancel, done: make(chan struct{}), stream: NewStream[Summary](cols, rows)}
	m.entries[entry.summary.ID] = entry
	result := entry.summary
	m.mu.Unlock()
	m.notify(project)
	go m.run(ctx, entry, account, runtime, provider, cols, rows, launch)
	return result, nil
}
func (m *Manager) run(ctx context.Context, e *session, a agents.Account, r agents.Session, p agents.Provider, cols, rows int, options agents.LaunchOptions) {
	defer close(e.done)
	defer e.cancel()
	RunSession(ctx, e.stream, p, a, r, m.sessions, cols, rows, options, func(change func(*Summary) error) error {
		m.mu.Lock()
		if err := change(&e.summary); err != nil {
			m.mu.Unlock()
			return err
		}
		e.stream.Status(e.summary)
		if !e.summary.Active() {
			m.evict()
		}
		project := e.summary.ProjectID
		m.mu.Unlock()
		m.notify(project)
		return nil
	})
}
func (m *Manager) evict() {
	var ended []*session
	for _, e := range m.entries {
		if !e.summary.Active() {
			ended = append(ended, e)
		}
	}
	sort.Slice(ended, func(i, j int) bool { return ended[i].summary.CreatedAt.Before(ended[j].summary.CreatedAt) })
	for len(ended) > 32 {
		e := ended[0]
		e.stream.DetachAll()
		delete(m.entries, e.summary.ID)
		ended = ended[1:]
	}
}
func (m *Manager) List(project string) []Summary {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Summary{}
	for _, e := range m.entries {
		if e.summary.ProjectID == project {
			out = append(out, e.summary)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

// ActiveSessions returns the IDs of the account's active sessions in every
// project, oldest first.
func (m *Manager) ActiveSessions(account agents.AccountID) []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var active []Summary
	for _, e := range m.entries {
		if e.summary.AccountID == account && e.summary.Active() {
			active = append(active, e.summary)
		}
	}
	sort.Slice(active, func(i, j int) bool { return active[i].CreatedAt.Before(active[j].CreatedAt) })
	ids := make([]string, len(active))
	for i, summary := range active {
		ids[i] = summary.ID
	}
	return ids
}
func (m *Manager) Get(project, id string) (Summary, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.entries[id]
	if !ok || e.summary.ProjectID != project {
		return Summary{}, false
	}
	return e.summary, true
}
func (m *Manager) Stop(project, id string) error {
	m.mu.Lock()
	e, ok := m.entries[id]
	if !ok || e.summary.ProjectID != project {
		m.mu.Unlock()
		return fmt.Errorf("session not found")
	}
	if e.summary.Active() {
		e.summary.State = "stopping"
		e.stream.Status(e.summary)
		e.cancel()
	}
	m.mu.Unlock()
	m.notify(project)
	return nil
}
func (m *Manager) Close() {
	m.mu.Lock()
	m.closed = true
	var done []chan struct{}
	for _, e := range m.entries {
		e.cancel()
		done = append(done, e.done)
	}
	m.mu.Unlock()
	for _, ch := range done {
		<-ch
	}
	m.mu.Lock()
	for _, e := range m.entries {
		e.stream.DetachAll()
	}
	m.mu.Unlock()
}
func (m *Manager) Attach(project, id string, cursor uint64) (uint64, bool, []Frame, <-chan Frame, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.entries[id]
	if !ok || e.summary.ProjectID != project {
		return 0, false, nil, nil, fmt.Errorf("session not found")
	}
	subscriber, writer, frames, ch := e.stream.Attach(cursor, e.summary)
	return subscriber, writer, frames, ch, nil
}
func (m *Manager) Detach(id string, subscriber uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e := m.entries[id]; e != nil {
		e.stream.Detach(subscriber)
	}
}
func (m *Manager) Input(id string, subscriber uint64, data []byte) error {
	m.mu.Lock()
	e := m.entries[id]
	if e == nil || e.summary.State != "running" {
		m.mu.Unlock()
		return fmt.Errorf("terminal input unavailable")
	}
	stream := e.stream
	m.mu.Unlock()
	return stream.Input(subscriber, data)
}
func (m *Manager) Resize(id string, subscriber uint64, cols, rows int) error {
	m.mu.Lock()
	e := m.entries[id]
	m.mu.Unlock()
	if e == nil {
		return fmt.Errorf("terminal resize unavailable")
	}
	return e.stream.Resize(subscriber, cols, rows)
}
func (m *Manager) CheckDirectories() {
	m.mu.Lock()
	for _, e := range m.entries {
		if e.summary.Active() {
			if _, err := os.Stat(e.workDir); err != nil {
				e.summary.Error = "Worktree directory disappeared."
				e.cancel()
			}
		}
	}
	m.mu.Unlock()
}

// Restore records an interrupted session without constructing a process or a home.
func (m *Manager) Restore(summary Summary) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if summary.ID == "" {
		return
	}
	if _, ok := m.entries[summary.ID]; ok {
		return
	}
	if summary.Active() {
		now := time.Now().UTC()
		summary.State = "failed"
		summary.EndedAt = &now
		summary.Error = "Agent interrupted by API restart; start explicitly to run again."
	}
	done := make(chan struct{})
	close(done)
	m.entries[summary.ID] = &session{summary: summary, done: done, cancel: func() {}, stream: NewStream[Summary](80, 24)}
	m.evict()
}
