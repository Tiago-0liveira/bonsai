package agenthost

import (
	"context"
	"fmt"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/agentterminal"
)

// Summary is an agent session as the API and its clients see it.
type Summary = agentterminal.Summary

// StartOptions are the per-launch options forwarded to the provider unchanged.
type StartOptions = agents.LaunchOptions

// ActiveLimit bounds the active sessions, adopted ones included.
const ActiveLimit = 16

// Manager is the API's view of agent sessions. It starts a holder per
// session, follows each holder's summary over a status connection, adopts the
// holders an earlier API started and keeps ended sessions' summaries as
// history. Holders outlive it: closing the manager only disconnects, unless
// asked to stop them.
type Manager struct {
	mu       sync.Mutex
	accounts agents.AccountStore
	sessions agents.SessionStore
	registry *agents.Registry
	paths    Paths
	launch   Launcher
	entries  map[string]*entry
	closed   bool
	quit     chan struct{}
	changed  func(string)
}

type entry struct {
	summary Summary
	workDir string
	// record locates the holder; its ID is empty for history restored from
	// the API journal.
	record      Record
	live        bool
	attachments map[*Attachment]struct{}
	// done closes once the manager stops following the holder.
	done chan struct{}
}

// NewManager returns a manager whose holders keep their records and sockets
// under paths. Zero paths mean no holders: Start fails and nothing is
// adopted. A nil launch starts real holder processes (Launch).
func NewManager(accounts agents.AccountStore, sessions agents.SessionStore, registry *agents.Registry, paths Paths, launch Launcher, changed func(string)) *Manager {
	if launch == nil {
		launch = Launch
	}
	return &Manager{accounts: accounts, sessions: sessions, registry: registry, paths: paths, launch: launch, entries: map[string]*entry{}, quit: make(chan struct{}), changed: changed}
}

// InProcess returns a Launcher that serves each session from this process,
// with deps' stores and providers. Tests use it: their providers exist only
// in the test process.
func InProcess(deps Deps) Launcher {
	return func(p Paths, spec Spec) error {
		deps := deps
		deps.Paths = p
		ready := make(chan error, 1)
		go func() { _ = Serve(context.Background(), spec, deps, func(err error) { ready <- err }) }()
		return <-ready
	}
}

func (m *Manager) notify(project string) {
	if m.changed != nil {
		m.changed(project)
	}
}

// Start validates the launch, creates the session's runtime and starts its
// holder. The session is starting until the holder reports it running.
func (m *Manager) Start(project, tree string, accountID agents.AccountID, name, dir string, cols, rows int, options ...StartOptions) (Summary, error) {
	if !agentterminal.Supported {
		return Summary{}, fmt.Errorf("interactive terminals unsupported on this platform")
	}
	if m.paths.Records == "" {
		return Summary{}, fmt.Errorf("agent hosts unavailable")
	}
	if !agentterminal.Dimensions(cols, rows) {
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
	if active >= ActiveLimit {
		m.mu.Unlock()
		return Summary{}, fmt.Errorf("%d active session limit reached", ActiveLimit)
	}
	runtime, err := m.sessions.Create(account, dir)
	if err != nil {
		m.mu.Unlock()
		return Summary{}, fmt.Errorf("cannot create session")
	}
	if name == "" {
		name = account.Name
	}
	summary := Summary{ID: string(runtime.ID), ProjectID: project, WorktreeID: tree, AccountID: accountID, Provider: account.Provider, ProfileName: account.Name, Name: name, State: "starting", CreatedAt: time.Now().UTC(), Model: launch.Model, Options: launch.Values()}
	// Registered before the launch, so concurrent starts count it.
	e := &entry{summary: summary, workDir: dir, live: true, attachments: map[*Attachment]struct{}{}, done: make(chan struct{})}
	m.entries[summary.ID] = e
	m.mu.Unlock()
	fail := func(err error) (Summary, error) {
		m.mu.Lock()
		delete(m.entries, summary.ID)
		m.mu.Unlock()
		close(e.done)
		_ = m.sessions.Cleanup(runtime)
		return Summary{}, err
	}
	if err := m.launch(m.paths, Spec{Summary: summary, WorkDir: dir, Cols: cols, Rows: rows, Launch: launch}); err != nil {
		return fail(fmt.Errorf("cannot start agent host: %v", err))
	}
	record, err := Read(m.paths, summary.ID)
	if err != nil {
		return fail(fmt.Errorf("cannot start agent host: %v", err))
	}
	conn, ready, err := Dial(record, Request{Mode: ModeStatus})
	m.mu.Lock()
	e.record = record
	if err == nil && ready.Status != nil {
		e.summary = *ready.Status
	}
	result := e.summary
	m.mu.Unlock()
	if err != nil {
		conn = nil
	}
	go m.follow(e, conn)
	m.notify(project)
	return result, nil
}

// follow keeps e's summary current from its holder until the session ends or
// the manager closes. A dropped connection is redialed while the holder runs;
// a holder that cannot be dialed (another protocol version) is watched
// through its lock instead.
func (m *Manager) follow(e *entry, conn *Conn) {
	defer close(e.done)
	for {
		if conn != nil && m.watch(e, conn) {
			return
		}
		select {
		case <-m.quit:
			return
		default:
		}
		r, err := Read(m.paths, e.record.ID)
		if err != nil || !r.Alive {
			m.mu.Lock()
			summary := e.summary
			m.mu.Unlock()
			if err == nil {
				summary = r.Summary
			}
			if summary.Active() {
				summary.State, summary.Error = "failed", LostError
				if summary.EndedAt == nil {
					now := time.Now().UTC()
					summary.EndedAt = &now
				}
			}
			m.apply(e, summary)
			return
		}
		select {
		case <-m.quit:
			return
		case <-time.After(time.Second):
		}
		conn = nil
		if c, ready, err := Dial(r, Request{Mode: ModeStatus}); err == nil {
			conn = c
			if ready.Status != nil && m.apply(e, *ready.Status) {
				_ = conn.Close()
				return
			}
		}
	}
}

// watch applies the summaries conn delivers until the session ends (true),
// or the connection or the manager closes (false).
func (m *Manager) watch(e *entry, conn *Conn) bool {
	done := make(chan struct{})
	defer close(done)
	defer conn.Close()
	go func() {
		select {
		case <-m.quit:
			_ = conn.Close()
		case <-done:
		}
	}()
	for {
		f, err := conn.Recv()
		if err != nil {
			return false
		}
		if f.Type == "status" && f.Status != nil && m.apply(e, *f.Status) {
			return true
		}
	}
}

// apply records a holder's summary and reports whether the session ended.
func (m *Manager) apply(e *entry, summary Summary) bool {
	m.mu.Lock()
	e.summary = summary
	ended := !summary.Active()
	if ended {
		e.live = false
		m.evict()
	}
	m.mu.Unlock()
	m.notify(summary.ProjectID)
	return ended
}

// evict drops the oldest ended sessions beyond 32. Callers hold m.mu.
func (m *Manager) evict() {
	var ended []*entry
	for _, e := range m.entries {
		if !e.summary.Active() && !e.live {
			ended = append(ended, e)
		}
	}
	sort.Slice(ended, func(i, j int) bool { return ended[i].summary.CreatedAt.Before(ended[j].summary.CreatedAt) })
	for len(ended) > keepEnded {
		e := ended[0]
		for a := range e.attachments {
			a.close()
		}
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

// Stop asks the session's holder to stop it; the holder reports the end.
func (m *Manager) Stop(project, id string) error {
	m.mu.Lock()
	e, ok := m.entries[id]
	if !ok || e.summary.ProjectID != project {
		m.mu.Unlock()
		return fmt.Errorf("session not found")
	}
	record, live := e.record, e.live && e.record.ID != ""
	if live && e.summary.Active() {
		e.summary.State = "stopping"
	}
	m.mu.Unlock()
	if live {
		if err := StopHolder(m.paths, record, ""); err != nil {
			return err
		}
	}
	m.notify(project)
	return nil
}

// StopHolder stops r's session: through its socket, or with SIGTERM to the
// holder when it speaks another protocol version or no longer answers.
func StopHolder(p Paths, r Record, reason string) error {
	err := Stop(r, reason)
	if err == nil || !held(p.lock(r.ID)) {
		return nil
	}
	if r.PID > 0 && terminate(r.PID) == nil {
		return nil
	}
	return err
}

// Close disconnects from every holder. With keepRunning false it first stops
// their sessions and waits, at most timeout in all, for their holders to
// report the end and exit.
func (m *Manager) Close(keepRunning bool, timeout time.Duration) {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.closed = true
	var live []*entry
	for _, e := range m.entries {
		if e.live && e.record.ID != "" {
			live = append(live, e)
		}
	}
	m.mu.Unlock()
	if !keepRunning {
		deadline := time.Now().Add(timeout)
		for _, e := range live {
			_ = StopHolder(m.paths, e.record, "")
		}
		expired := time.After(timeout)
	wait:
		for _, e := range live {
			select {
			case <-e.done:
			case <-expired:
				break wait
			}
		}
		for _, e := range live {
			Wait(m.paths, e.record.ID, time.Until(deadline))
		}
	}
	close(m.quit)
	m.mu.Lock()
	var attachments []*Attachment
	for _, e := range m.entries {
		for a := range e.attachments {
			attachments = append(attachments, a)
		}
	}
	m.mu.Unlock()
	for _, a := range attachments {
		a.close()
	}
	for _, e := range live {
		<-e.done
	}
}

// CheckDirectories stops sessions whose worktree directory disappeared.
func (m *Manager) CheckDirectories() {
	m.mu.Lock()
	var gone []Record
	for _, e := range m.entries {
		if e.summary.Active() && e.live && e.record.ID != "" {
			if _, err := os.Stat(e.workDir); err != nil {
				gone = append(gone, e.record)
			}
		}
	}
	m.mu.Unlock()
	for _, r := range gone {
		_ = StopHolder(m.paths, r, "Worktree directory disappeared.")
	}
}

// Restore records a session from the API journal that has no holder, without
// constructing a process or a home. An active one was interrupted.
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
	m.entries[summary.ID] = &entry{summary: summary, attachments: map[*Attachment]struct{}{}, done: done}
	m.evict()
}

// Adopt follows every running holder this manager does not know yet, such as
// those an earlier API started, and returns the summaries of all recorded
// sessions by ID, running or ended.
func (m *Manager) Adopt() map[string]Summary {
	out := map[string]Summary{}
	if m.paths.Records == "" {
		return out
	}
	records, _ := List(m.paths)
	for _, r := range records {
		out[r.ID] = r.Summary
		if !r.Alive {
			continue
		}
		m.mu.Lock()
		_, known := m.entries[r.ID]
		closed := m.closed
		m.mu.Unlock()
		if known || closed {
			continue
		}
		e := &entry{summary: r.Summary, workDir: r.WorkDir, record: r, live: true, attachments: map[*Attachment]struct{}{}, done: make(chan struct{})}
		conn, ready, err := Dial(r, Request{Mode: ModeStatus})
		if err != nil {
			conn = nil
		} else if ready.Status != nil {
			e.summary = *ready.Status
			out[r.ID] = e.summary
		}
		m.mu.Lock()
		m.entries[r.ID] = e
		m.mu.Unlock()
		go m.follow(e, conn)
	}
	return out
}

// Attachment is one terminal client of a session: the replay, then live
// frames until Close. Input and Resize reach the session while the
// attachment writes; a writer frame on Frames reports a change.
type Attachment struct {
	// Generation identifies the holder's stream, for resuming at an offset.
	Generation string
	// Replay is the retained output from the requested offset (a gap first
	// when it is gone), then the current status.
	Replay []agentterminal.Frame
	// Frames carries live output, status and writer frames; it closes when
	// the holder ends the connection or the attachment closes.
	Frames <-chan agentterminal.Frame

	writer  atomic.Bool
	conn    *Conn
	frames  chan agentterminal.Frame
	gone    chan struct{}
	once    sync.Once
	release func()
}

// Writer reports whether the attachment writes now.
func (a *Attachment) Writer() bool { return a.writer.Load() }

// Input sends terminal input; only the writer may.
func (a *Attachment) Input(data []byte) error {
	if !a.writer.Load() || a.conn == nil || len(data) > agentterminal.InputLimit {
		return fmt.Errorf("terminal input unavailable")
	}
	return a.conn.Send(Request{Type: "input", Data: data})
}

// Resize changes the terminal size; only the writer may.
func (a *Attachment) Resize(cols, rows int) error {
	if !a.writer.Load() || a.conn == nil || !agentterminal.Dimensions(cols, rows) {
		return fmt.Errorf("terminal resize unavailable")
	}
	return a.conn.Send(Request{Type: "resize", Cols: cols, Rows: rows})
}

// Take makes this attachment the writer; a writer frame confirms it.
func (a *Attachment) Take() error {
	if a.conn == nil {
		return fmt.Errorf("session has ended")
	}
	return a.conn.Send(Request{Type: "take"})
}

// Close detaches; the session keeps running.
func (a *Attachment) Close() { a.close() }

func (a *Attachment) close() {
	a.once.Do(func() {
		close(a.gone)
		if a.conn != nil {
			_ = a.conn.Close()
		} else {
			close(a.frames)
		}
		if a.release != nil {
			a.release()
		}
	})
}

// Attach connects a terminal client (client labels it: web or cli) to the
// session at cursor. A cursor from another generation replays everything. An
// ended session replays its final status only.
func (m *Manager) Attach(project, id string, cursor uint64, generation, client string) (*Attachment, error) {
	m.mu.Lock()
	e, ok := m.entries[id]
	if !ok || e.summary.ProjectID != project {
		m.mu.Unlock()
		return nil, fmt.Errorf("session not found")
	}
	record, live, summary := e.record, e.live && e.record.ID != "", e.summary
	m.mu.Unlock()
	var a *Attachment
	if live {
		a, _ = attach(record, Request{Mode: ModeTerminal, Offset: cursor, Generation: generation, Client: client})
	}
	if a == nil {
		frames := make(chan agentterminal.Frame)
		a = &Attachment{Replay: []agentterminal.Frame{{Type: "status", Status: &summary}}, Frames: frames, frames: frames, gone: make(chan struct{})}
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		a.close()
		return nil, fmt.Errorf("server shutting down")
	}
	e.attachments[a] = struct{}{}
	a.release = func() {
		m.mu.Lock()
		delete(e.attachments, a)
		m.mu.Unlock()
	}
	m.mu.Unlock()
	return a, nil
}

// attach dials r's holder as a terminal client and reads the replay, which
// ends with the status frame.
func attach(r Record, hello Request) (*Attachment, error) {
	conn, ready, err := Dial(r, hello)
	if err != nil {
		return nil, err
	}
	a := &Attachment{Generation: ready.Generation, conn: conn, gone: make(chan struct{})}
	a.writer.Store(ready.Writer != nil && *ready.Writer)
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	for {
		f, err := conn.Recv()
		if err != nil {
			_ = conn.Close()
			return nil, err
		}
		if f.Type == "error" {
			continue
		}
		a.Replay = append(a.Replay, f.TerminalFrame())
		if f.Type == "status" {
			break
		}
	}
	_ = conn.SetDeadline(time.Time{})
	frames := make(chan agentterminal.Frame, 64)
	a.Frames, a.frames = frames, frames
	go func() {
		defer close(frames)
		for {
			f, err := conn.Recv()
			if err != nil {
				return
			}
			switch f.Type {
			case "error", "info":
				continue
			case "writer":
				a.writer.Store(f.Writer != nil && *f.Writer)
			}
			select {
			case frames <- f.TerminalFrame():
			case <-a.gone:
				return
			}
		}
	}()
	return a, nil
}
