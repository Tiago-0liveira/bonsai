// Package fake is an in-memory agentsapi backend for TUI tests. It is
// deterministic: nothing runs in the background except closing a Changes
// channel when its context ends.
package fake

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sync"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/agentterminal"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
)

// ErrNotFound is returned for an unknown session or account ID.
var ErrNotFound = errors.New("fake: not found")

// ErrClosed is returned by a closed Stream.
var ErrClosed = errors.New("fake: stream closed")

// Backend holds the fake sessions and accounts.
type Backend struct {
	Sessions *Sessions
	Accounts *Accounts
}

// New returns an empty, supported backend.
func New() *Backend {
	return &Backend{
		Sessions: &Sessions{Supports: true},
		Accounts: &Accounts{
			Infos:          map[agents.AccountID]agents.AccountInfo{},
			Usages:         map[agents.AccountID]agentsapi.Usage{},
			LaunchDefaults: map[agents.AccountID]agentsapi.Defaults{},
		},
	}
}

// API returns the backend as the TUI consumes it.
func (b *Backend) API() agentsapi.Backend {
	return agentsapi.Backend{Sessions: b.Sessions, Accounts: b.Accounts}
}

// Emit fires every open Changes channel.
func (b *Backend) Emit() { b.Sessions.Emit() }

// Sessions is the fake agentsapi.Sessions. Set the exported error fields to
// make the matching call fail.
type Sessions struct {
	Supports  bool
	ListErr   error
	StartErr  error
	StopErr   error
	AttachErr error

	mu       sync.Mutex
	items    []agentsapi.Session
	nextID   int
	subs     []chan struct{}
	streams  map[string][]*Stream
	starts   []agentsapi.StartRequest
	attaches []AttachCall
}

// AttachCall records one Attach.
type AttachCall struct {
	ID   string
	Opts agentsapi.AttachOptions
}

var _ agentsapi.Sessions = (*Sessions)(nil)

// Supported reports Supports.
func (s *Sessions) Supported() bool { return s.Supports }

// Add stores sessions without firing Changes.
func (s *Sessions) Add(items ...agentsapi.Session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items = append(s.items, items...)
}

// Set replaces the session with the same ID, or adds it.
func (s *Sessions) Set(item agentsapi.Session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.items {
		if s.items[i].ID == item.ID {
			s.items[i] = item
			return
		}
	}
	s.items = append(s.items, item)
}

// List returns a copy of the stored sessions.
func (s *Sessions) List(context.Context) ([]agentsapi.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ListErr != nil {
		return nil, s.ListErr
	}
	return append([]agentsapi.Session(nil), s.items...), nil
}

// Changes returns a channel that Emit signals. Signals coalesce: the channel
// holds at most one pending value. Cancel ctx to release it (t.Context works).
func (s *Sessions) Changes(ctx context.Context) (<-chan struct{}, error) {
	ch := make(chan struct{}, 1)
	s.mu.Lock()
	s.subs = append(s.subs, ch)
	s.mu.Unlock()
	go func() {
		<-ctx.Done()
		s.mu.Lock()
		defer s.mu.Unlock()
		for i, c := range s.subs {
			if c == ch {
				s.subs = append(s.subs[:i], s.subs[i+1:]...)
				break
			}
		}
		close(ch)
	}()
	return ch, nil
}

// Emit signals every open Changes channel without blocking.
func (s *Sessions) Emit() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, ch := range s.subs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// Start records the request, adds a running session and fires Changes.
func (s *Sessions) Start(_ context.Context, req agentsapi.StartRequest) (agentsapi.Session, error) {
	s.mu.Lock()
	s.starts = append(s.starts, req)
	if s.StartErr != nil {
		s.mu.Unlock()
		return agentsapi.Session{}, s.StartErr
	}
	s.nextID++
	sess := agentsapi.Session{
		Summary: agentterminal.Summary{
			ID:         fmt.Sprintf("fake-%d", s.nextID),
			ProjectID:  req.ProjectID,
			WorktreeID: req.WorktreeID,
			AccountID:  req.AccountID,
			Name:       req.Name,
			State:      "running",
			Model:      req.Launch.Model,
		},
		WorkDir: req.WorkDir,
	}
	s.items = append(s.items, sess)
	s.mu.Unlock()
	s.Emit()
	return sess, nil
}

// Stop marks the session exited and fires Changes.
func (s *Sessions) Stop(_ context.Context, id string) error {
	s.mu.Lock()
	if s.StopErr != nil {
		s.mu.Unlock()
		return s.StopErr
	}
	found := false
	for i := range s.items {
		if s.items[i].ID == id {
			s.items[i].State = "exited"
			found = true
		}
	}
	s.mu.Unlock()
	if !found {
		return ErrNotFound
	}
	s.Emit()
	return nil
}

// Attach returns a new Stream for a known session, with the holder's control
// rules: read-only never writes, Take demotes the current writer, and a plain
// attach writes only when no other open stream of the session does.
func (s *Sessions) Attach(_ context.Context, id string, opts agentsapi.AttachOptions) (agentsapi.Stream, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.attaches = append(s.attaches, AttachCall{ID: id, Opts: opts})
	if s.AttachErr != nil {
		return nil, s.AttachErr
	}
	found := false
	for _, it := range s.items {
		found = found || it.ID == id
	}
	if !found {
		return nil, ErrNotFound
	}
	st := NewStream(80, 24)
	st.readOnly = opts.ReadOnly
	if !opts.ReadOnly {
		taken := false
		for _, other := range s.streams[id] {
			if opts.Take {
				other.SetWriter(false)
			} else if other.Writer() && !other.Closed() {
				taken = true
			}
		}
		st.writer = opts.Take || !taken
	}
	if s.streams == nil {
		s.streams = map[string][]*Stream{}
	}
	s.streams[id] = append(s.streams[id], st)
	return st, nil
}

// Starts returns the recorded start requests.
func (s *Sessions) Starts() []agentsapi.StartRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]agentsapi.StartRequest(nil), s.starts...)
}

// Attaches returns the recorded attach calls.
func (s *Sessions) Attaches() []AttachCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]AttachCall(nil), s.attaches...)
}

// Streams returns the streams attached to a session, oldest first.
func (s *Sessions) Streams(id string) []*Stream {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]*Stream(nil), s.streams[id]...)
}

// Stream is the fake agentsapi.Stream. Push queues frames; Inputs, Resizes
// and Takes report what the TUI sent.
type Stream struct {
	mu       sync.Mutex
	frames   chan agentterminal.Frame
	closed   bool
	writer   bool
	readOnly bool
	cols     int
	rows     int
	inputs   [][]byte
	resizes  [][2]int
	takes    int
}

var _ agentsapi.Stream = (*Stream)(nil)

// NewStream returns an open stream of the given holder size with room for
// 256 queued frames.
func NewStream(cols, rows int) *Stream {
	return &Stream{frames: make(chan agentterminal.Frame, 256), cols: cols, rows: rows}
}

// Push queues a frame. It reports false when the stream is closed or the
// buffer is full, instead of blocking the test.
func (s *Stream) Push(f agentterminal.Frame) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	select {
	case s.frames <- f:
		return true
	default:
		return false
	}
}

// SetWriter changes control, as a writer frame from the holder would.
func (s *Stream) SetWriter(w bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.writer = w
}

// SetSize changes the holder size Size reports.
func (s *Stream) SetSize(cols, rows int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cols, s.rows = cols, rows
}

func (s *Stream) Frames() <-chan agentterminal.Frame { return s.frames }

func (s *Stream) Writer() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writer
}

func (s *Stream) Size() (int, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cols, s.rows
}

func (s *Stream) Input(p []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrClosed
	}
	if !s.writer {
		return errors.New("fake: not the writer")
	}
	s.inputs = append(s.inputs, append([]byte(nil), p...))
	return nil
}

func (s *Stream) Resize(cols, rows int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrClosed
	}
	if !s.writer {
		return errors.New("fake: not the writer")
	}
	s.resizes = append(s.resizes, [2]int{cols, rows})
	s.cols, s.rows = cols, rows
	return nil
}

func (s *Stream) Take() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrClosed
	}
	if s.readOnly {
		return errors.New("fake: read-only attachment")
	}
	s.takes++
	s.writer = true
	return nil
}

// Close closes Frames once; later calls are no-ops.
func (s *Stream) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.closed = true
		close(s.frames)
	}
	return nil
}

// Inputs returns a copy of every Input payload, oldest first.
func (s *Stream) Inputs() [][]byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([][]byte(nil), s.inputs...)
}

// Resizes returns every accepted Resize as {cols, rows}.
func (s *Stream) Resizes() [][2]int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([][2]int(nil), s.resizes...)
}

// Takes counts accepted Take calls.
func (s *Stream) Takes() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.takes
}

// Closed reports whether Close ran.
func (s *Stream) Closed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

// Accounts is the fake agentsapi.Accounts. Fill the exported maps and items;
// set the error fields to make calls fail.
type Accounts struct {
	Items          []agentsapi.Account
	Infos          map[agents.AccountID]agents.AccountInfo
	Usages         map[agents.AccountID]agentsapi.Usage
	LaunchDefaults map[agents.AccountID]agentsapi.Defaults

	ListErr, DescribeErr, UsageErr, DefaultsErr, UpdateErr, RemoveErr error

	mu         sync.Mutex
	usageCalls []UsageCall
	updates    []DefaultsUpdate
	removed    []agents.AccountID
}

// UsageCall records one Usage request.
type UsageCall struct {
	ID      agents.AccountID
	Refresh bool
}

// DefaultsUpdate records one UpdateDefaults request.
type DefaultsUpdate struct {
	ID     agents.AccountID
	Values map[string]*string
}

var _ agentsapi.Accounts = (*Accounts)(nil)

func (a *Accounts) List(context.Context) ([]agentsapi.Account, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.ListErr != nil {
		return nil, a.ListErr
	}
	return append([]agentsapi.Account(nil), a.Items...), nil
}

func (a *Accounts) Describe(_ context.Context, id agents.AccountID) (agents.AccountInfo, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.DescribeErr != nil {
		return agents.AccountInfo{}, a.DescribeErr
	}
	info, ok := a.Infos[id]
	if !ok {
		return agents.AccountInfo{}, ErrNotFound
	}
	return info, nil
}

func (a *Accounts) Usage(_ context.Context, id agents.AccountID, refresh bool) (agentsapi.Usage, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.usageCalls = append(a.usageCalls, UsageCall{ID: id, Refresh: refresh})
	if a.UsageErr != nil {
		return agentsapi.Usage{}, a.UsageErr
	}
	u, ok := a.Usages[id]
	if !ok {
		return agentsapi.Usage{}, ErrNotFound
	}
	return u, nil
}

func (a *Accounts) Defaults(_ context.Context, id agents.AccountID) (agentsapi.Defaults, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.DefaultsErr != nil {
		return agentsapi.Defaults{}, a.DefaultsErr
	}
	d, ok := a.LaunchDefaults[id]
	if !ok {
		return agentsapi.Defaults{}, ErrNotFound
	}
	values := make(map[string]string, len(d.Values))
	for k, v := range d.Values {
		values[k] = v
	}
	d.Values = values
	if d.LaunchFields == nil {
		d.LaunchFields = SampleLaunchFields()
	}
	return d, nil
}

// UpdateDefaults records the change and applies it to LaunchDefaults; a nil
// value removes the key (back to the provider default).
func (a *Accounts) UpdateDefaults(_ context.Context, id agents.AccountID, values map[string]*string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	copied := make(map[string]*string, len(values))
	for k, v := range values {
		if v != nil {
			s := *v
			v = &s
		}
		copied[k] = v
	}
	a.updates = append(a.updates, DefaultsUpdate{ID: id, Values: copied})
	if a.UpdateErr != nil {
		return a.UpdateErr
	}
	if a.LaunchDefaults == nil {
		a.LaunchDefaults = map[agents.AccountID]agentsapi.Defaults{}
	}
	d := a.LaunchDefaults[id]
	if d.Values == nil {
		d.Values = map[string]string{}
	}
	for k, v := range copied {
		if v == nil {
			delete(d.Values, k)
		} else {
			d.Values[k] = *v
		}
	}
	a.LaunchDefaults[id] = d
	return nil
}

// Remove deletes the account from Items.
func (a *Accounts) Remove(_ context.Context, id agents.AccountID) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.RemoveErr != nil {
		return a.RemoveErr
	}
	for i, it := range a.Items {
		if it.ID == id {
			a.Items = append(a.Items[:i], a.Items[i+1:]...)
			a.removed = append(a.removed, id)
			return nil
		}
	}
	return ErrNotFound
}

// AddCommand returns the CLI argv the real backend would run; tests inspect
// Args and never start it.
func (a *Accounts) AddCommand(provider agents.ProviderID, name string) *exec.Cmd {
	return exec.Command("bonsai", "agent", "account", "add", string(provider), name)
}

// LoginCommand returns the CLI argv the real backend would run.
func (a *Accounts) LoginCommand(id agents.AccountID) *exec.Cmd {
	return exec.Command("bonsai", "agent", "account", "login", string(id))
}

// UsageCalls returns the recorded Usage requests.
func (a *Accounts) UsageCalls() []UsageCall {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]UsageCall(nil), a.usageCalls...)
}

// Updates returns the recorded UpdateDefaults requests.
func (a *Accounts) Updates() []DefaultsUpdate {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]DefaultsUpdate(nil), a.updates...)
}

// Removed returns the IDs removed so far.
func (a *Accounts) Removed() []agents.AccountID {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]agents.AccountID(nil), a.removed...)
}

// SampleLaunchFields is a small provider-neutral launch schema: a preset
// select, a per-model effort select and a toggle, covering every kind the
// launch form handles.
func SampleLaunchFields() []agents.LaunchField {
	return []agents.LaunchField{
		{ID: "permissions", Label: "Permissions", Kind: agents.LaunchFieldSelect, Default: "auto", Options: []agents.FieldOption{
			{Value: "read-only", Label: "Read only", Set: map[string]string{"sandbox": "read-only", "approval": "on-request"}},
			{Value: "auto", Label: "Auto", Set: map[string]string{"sandbox": "workspace-write", "approval": "on-request"}},
			{Value: "full-access", Label: "Full access", Tone: agents.ToneDanger, Help: "Can change any file without asking.",
				Set: map[string]string{"sandbox": "danger-full-access", "approval": "never"}},
		}},
		{ID: "effort", Label: "Effort", Kind: agents.LaunchFieldSelect, PerModel: true, Options: []agents.FieldOption{
			{Value: "", Label: "Profile default"}, {Value: "low", Label: "low"}, {Value: "high", Label: "high"},
		}},
		{ID: "full_access", Label: "Full access", Kind: agents.LaunchFieldToggle, Options: []agents.FieldOption{
			{Value: "false", Label: "Ask permissions"}, {Value: "true", Label: "Full access", Tone: agents.ToneDanger},
		}},
	}
}
