// Package agentterminal owns API-lifetime provider processes and bounded terminal streams.
package agentterminal

import (
	"context"
	"errors"
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
}

func (s Summary) Active() bool {
	return s.State == "starting" || s.State == "running" || s.State == "stopping"
}

type Frame struct {
	Type   string   `json:"type"`
	Offset uint64   `json:"offset"`
	Data   []byte   `json:"data,omitempty"`
	Status *Summary `json:"status,omitempty"`
}
type session struct {
	summary     Summary
	workDir     string
	proc        *process
	cancel      context.CancelFunc
	done        chan struct{}
	output      []byte
	head        int
	offset      uint64
	subscribers map[uint64]chan Frame
	writer      uint64
	next        uint64
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
	entry := &session{summary: Summary{ID: string(runtime.ID), ProjectID: project, WorktreeID: tree, AccountID: accountID, Provider: account.Provider, ProfileName: account.Name, Name: name, State: "starting", CreatedAt: time.Now().UTC()}, workDir: dir, cancel: cancel, done: make(chan struct{}), subscribers: map[uint64]chan Frame{}}
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
	prepared, err := p.PrepareSession(ctx, agents.PrepareSessionRequest{Account: a, Session: r, Launch: options})
	preparedOK := err == nil
	code := -1
	if err == nil {
		m.mu.Lock()
		e.summary.ProviderSessionID = prepared.ProviderSessionID
		if ctx.Err() != nil {
			err = ctx.Err()
		} else {
			e.proc, err = launch(prepared, cols, rows)
		}
		if err == nil {
			e.summary.State = "running"
			now := time.Now().UTC()
			e.summary.StartedAt = &now
			status := e.summary
			m.broadcast(e, Frame{Type: "status", Offset: e.offset, Status: &status})
		}
		m.mu.Unlock()
		m.notify(e.summary.ProjectID)
		if err == nil {
			drained := make(chan struct{})
			go func() {
				defer close(drained)
				buf := make([]byte, 16384)
				for {
					n, readErr := e.proc.file.Read(buf)
					if n > 0 {
						m.output(e, buf[:n])
					}
					if readErr != nil {
						return
					}
				}
			}()
			waited := make(chan struct{})
			monitorDone := make(chan struct{})
			go func() {
				defer close(monitorDone)
				select {
				case <-ctx.Done():
					select {
					case <-waited:
						return
					default:
					}
					e.proc.signal(false)
					select {
					case <-waited:
					case <-time.After(2 * time.Second):
						e.proc.signal(true)
					}
				case <-waited:
				}
			}()
			code, err = e.proc.wait()
			close(waited)
			<-monitorDone
			// Descendants must not retain the slave terminal after the provider exits.
			e.proc.signal(true)
			select {
			case <-drained:
			case <-time.After(time.Second):
				_ = e.proc.file.Close()
				<-drained
			}
			_ = e.proc.file.Close()
		}
	}
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var finalErr error
	if preparedOK {
		finalErr = p.FinalizeSession(cleanupCtx, agents.FinalizeSessionRequest{Account: a, Session: r})
	}
	cleanupErr := m.sessions.Cleanup(r)
	m.mu.Lock()
	now := time.Now().UTC()
	e.summary.EndedAt = &now
	e.summary.ExitCode = &code
	e.summary.State = "exited"
	if e.summary.Error != "" {
		e.summary.State = "failed"
	}
	if err != nil && ctx.Err() == nil {
		e.summary.State = "failed"
		e.summary.Error = "Agent failed to start or exited unsuccessfully. Check the " + agents.Label(p) + " installation and profile setup."
	}
	if errors.Join(finalErr, cleanupErr) != nil {
		e.summary.Error = "Profile finalization or session cleanup failed."
	}
	status := e.summary
	m.broadcast(e, Frame{Type: "status", Offset: e.offset, Status: &status})
	m.evict()
	m.mu.Unlock()
	m.notify(e.summary.ProjectID)
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
		for _, ch := range e.subscribers {
			close(ch)
		}
		delete(m.entries, e.summary.ID)
		ended = ended[1:]
	}
}
func (m *Manager) broadcast(e *session, f Frame) {
	for id, ch := range e.subscribers {
		select {
		case ch <- f:
		default:
			close(ch)
			delete(e.subscribers, id)
			if e.writer == id {
				e.writer = 0
			}
		}
	}
}
func (m *Manager) output(e *session, b []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	data := append([]byte(nil), b...)
	offset := e.offset
	e.offset += uint64(len(b))
	if len(b) >= OutputLimit {
		e.output = append(e.output[:0], b[len(b)-OutputLimit:]...)
		e.head = 0
	} else {
		if len(e.output) < OutputLimit {
			n := min(OutputLimit-len(e.output), len(b))
			e.output = append(e.output, b[:n]...)
			b = b[n:]
		}
		if len(b) > 0 {
			n := copy(e.output[e.head:], b)
			copy(e.output, b[n:])
			e.head = (e.head + len(b)) % OutputLimit
		}
	}

	m.broadcast(e, Frame{Type: "output", Offset: offset, Data: data})
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
		status := e.summary
		m.broadcast(e, Frame{Type: "status", Offset: e.offset, Status: &status})
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
		for id, ch := range e.subscribers {
			close(ch)
			delete(e.subscribers, id)
		}
		e.writer = 0
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
	e.next++
	subscriber := e.next
	ch := make(chan Frame, 64)
	e.subscribers[subscriber] = ch
	if e.writer == 0 {
		e.writer = subscriber
	}
	start := e.offset - uint64(len(e.output))
	frames := []Frame{}
	if cursor < start || cursor > e.offset {
		frames = append(frames, Frame{Type: "gap", Offset: start})
		cursor = start
	}
	if cursor < e.offset {
		frames = append(frames, Frame{Type: "output", Offset: cursor, Data: e.replay(cursor - start)})
	}
	status := e.summary
	frames = append(frames, Frame{Type: "status", Offset: e.offset, Status: &status})
	return subscriber, e.writer == subscriber, frames, ch, nil
}
func (m *Manager) Detach(id string, subscriber uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e := m.entries[id]; e != nil {
		if ch, ok := e.subscribers[subscriber]; ok {
			close(ch)
			delete(e.subscribers, subscriber)
		}
		if e.writer == subscriber {
			e.writer = 0
		}
	}
}
func (m *Manager) Input(id string, subscriber uint64, data []byte) error {
	m.mu.Lock()
	e := m.entries[id]
	if e == nil || e.writer != subscriber || e.proc == nil || e.summary.State != "running" || len(data) > InputLimit {
		m.mu.Unlock()
		return fmt.Errorf("terminal input unavailable")
	}
	p := e.proc
	m.mu.Unlock()
	_ = p.file.SetWriteDeadline(time.Now().Add(time.Second))
	_, err := p.file.Write(data)
	return err
}
func (m *Manager) Resize(id string, subscriber uint64, cols, rows int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.entries[id]
	if e != nil && e.writer == subscriber && Dimensions(cols, rows) && (e.proc == nil || !e.summary.Active()) {
		return nil
	}
	if e == nil || e.writer != subscriber || e.proc == nil || !Dimensions(cols, rows) {
		return fmt.Errorf("terminal resize unavailable")
	}
	return e.proc.resize(cols, rows)
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
	m.entries[summary.ID] = &session{summary: summary, done: done, cancel: func() {}, subscribers: map[uint64]chan Frame{}}
	m.evict()
}

func (e *session) replay(skip uint64) []byte {
	out := make([]byte, 0, len(e.output))
	out = append(out, e.output[e.head:]...)
	out = append(out, e.output[:e.head]...)
	return out[skip:]
}
