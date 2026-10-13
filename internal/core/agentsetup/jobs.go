// Package agentsetup runs profile setup and sign-in for the web: each job owns
// a terminal stream for the provider's login commands and a secret prompt the
// browser answers. Jobs live in memory only; nothing about them is journaled.
package agentsetup

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/agentterminal"
)

// Kind says whether a job creates a profile or signs one in again.
type Kind string

const (
	KindCreate Kind = "create"
	KindLogin  Kind = "login"
)

// State is a job's lifecycle state:
// preparing → running → (waiting_secret ↔ running)* → verifying → succeeded | failed | canceled.
type State string

const (
	StatePreparing     State = "preparing"
	StateRunning       State = "running"
	StateWaitingSecret State = "waiting_secret"
	StateVerifying     State = "verifying"
	StateSucceeded     State = "succeeded"
	StateFailed        State = "failed"
	StateCanceled      State = "canceled"
)

// Limits from the plan: a job never outlives its timeout, and an ended job's
// output is dropped after Retention.
const (
	DefaultMaxActive = 2
	DefaultTimeout   = 15 * time.Minute
	DefaultRetention = 2 * time.Minute
	// cancelWait bounds how long Cancel waits for the job to end.
	cancelWait = 5 * time.Second
	maxError   = 300
)

var (
	ErrNotFound = errors.New("setup job not found")
	ErrLimit    = errors.New("too many setup jobs")
	ErrBusy     = errors.New("a setup job for this profile is already running")
	ErrNoPrompt = errors.New("setup job is not waiting for a secret")
	ErrClosed   = errors.New("setup jobs are shutting down")
)

// Prompt is the secret a job waits for, by label ("OpenAI API key").
type Prompt struct {
	Label string `json:"label"`
}

// Job is the display-safe state of a setup job. It never holds a secret.
type Job struct {
	ID        string            `json:"id"`
	Kind      Kind              `json:"kind"`
	Provider  agents.ProviderID `json:"provider"`
	AccountID agents.AccountID  `json:"account_id,omitempty"`
	Name      string            `json:"name"`
	AuthMode  string            `json:"auth_mode,omitempty"`
	State     State             `json:"state"`
	Prompt    *Prompt           `json:"prompt,omitempty"`
	Error     string            `json:"error,omitempty"`
	Warnings  []string          `json:"warnings"`
	CreatedAt time.Time         `json:"created_at"`
	EndedAt   *time.Time        `json:"ended_at,omitempty"`
}

// Active reports whether the job has not ended.
func (j Job) Active() bool {
	return j.State != StateSucceeded && j.State != StateFailed && j.State != StateCanceled
}

// Frame is one setup terminal message; its status is the job.
type Frame = agentterminal.StreamFrame[Job]

// Service creates and signs in profiles; *agents.AccountService is one.
type Service interface {
	SetupWithIO(context.Context, agents.ProviderID, string, agents.SetupOptions, agents.SetupIO) (agents.SetupResult, error)
	Reauthenticate(context.Context, agents.AccountID, agents.SetupOptions, agents.SetupIO) (agents.SetupResult, error)
}

// Request starts a job. Create jobs name the provider and the new profile;
// login jobs name the account (Provider and Name are shown on the job).
type Request struct {
	Kind      Kind
	Provider  agents.ProviderID
	Name      string
	AccountID agents.AccountID
	// Options.Secret, when set, is read once by the provider.
	Options agents.SetupOptions
}

type job struct {
	job      Job
	stream   *agentterminal.Stream[Job]
	cancel   context.CancelFunc
	done     chan struct{}
	answer   chan string // set while waiting for a secret
	canceled bool
}

// Jobs runs setup jobs. The zero value is not usable; call New.
type Jobs struct {
	// Limits, read when a job starts; tests shorten them.
	MaxActive int
	Timeout   time.Duration
	Retention time.Duration

	mu      sync.Mutex
	service Service
	changed func()
	now     func() time.Time
	after   func(time.Duration, func())
	jobs    map[string]*job
	closed  bool
}

// New returns a job manager over service. changed runs after every state
// change (the API publishes agents_changed); it may be nil.
func New(service Service, changed func()) *Jobs {
	return &Jobs{
		MaxActive: DefaultMaxActive, Timeout: DefaultTimeout, Retention: DefaultRetention,
		service: service, changed: changed, now: time.Now,
		after: func(d time.Duration, f func()) { time.AfterFunc(d, f) },
		jobs:  map[string]*job{},
	}
}

// SetClock replaces the clock and the timer used for retention (tests).
func (j *Jobs) SetClock(now func() time.Time, after func(time.Duration, func())) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.now, j.after = now, after
}

func (j *Jobs) notify() {
	if j.changed != nil {
		j.changed()
	}
}

// Start begins a job and returns it in state preparing.
func (j *Jobs) Start(req Request) (Job, error) {
	id, err := newJobID()
	if err != nil {
		return Job{}, err
	}
	j.mu.Lock()
	if j.closed {
		j.mu.Unlock()
		return Job{}, ErrClosed
	}
	j.evictLocked()
	active := 0
	for _, e := range j.jobs {
		if !e.job.Active() {
			continue
		}
		active++
		if req.Kind == KindLogin && e.job.AccountID == req.AccountID ||
			req.Kind == KindCreate && e.job.Kind == KindCreate && e.job.Provider == req.Provider && e.job.Name == req.Name {
			j.mu.Unlock()
			return Job{}, ErrBusy
		}
	}
	if active >= j.MaxActive {
		j.mu.Unlock()
		return Job{}, ErrLimit
	}
	timeout := j.Timeout
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	e := &job{
		job: Job{
			ID: id, Kind: req.Kind, Provider: req.Provider, Name: req.Name, AuthMode: req.Options.AuthMode,
			State: StatePreparing, Warnings: []string{}, CreatedAt: j.now().UTC(),
		},
		stream: agentterminal.NewStream[Job](80, 24),
		cancel: cancel,
		done:   make(chan struct{}),
	}
	if req.Kind == KindLogin {
		e.job.AccountID = req.AccountID
	}
	j.jobs[id] = e
	snapshot := e.job
	j.mu.Unlock()
	j.notify()
	go j.run(ctx, e, req, timeout)
	return snapshot, nil
}

func (j *Jobs) run(ctx context.Context, e *job, req Request, timeout time.Duration) {
	defer close(e.done)
	defer e.cancel()
	setupIO := agents.SetupIO{
		Launcher: launcher{jobs: j, e: e},
		Out:      &crlfWriter{w: e.stream},
		Prompt:   prompter{jobs: j, e: e},
	}
	var result agents.SetupResult
	var err error
	switch req.Kind {
	case KindCreate:
		result, err = j.service.SetupWithIO(ctx, req.Provider, req.Name, req.Options, setupIO)
	case KindLogin:
		result, err = j.service.Reauthenticate(ctx, req.AccountID, req.Options, setupIO)
	default:
		err = fmt.Errorf("unknown setup job kind")
	}
	j.mu.Lock()
	now := j.now().UTC()
	e.job.EndedAt = &now
	e.job.Prompt = nil
	e.answer = nil
	// A sign-in that finished before a cancel or the timeout took hold stands.
	switch {
	case err == nil:
		e.job.State = StateSucceeded
		e.job.AccountID = result.Account.ID
		if result.Warnings != nil {
			e.job.Warnings = append([]string(nil), result.Warnings...)
		}
	case e.canceled:
		e.job.State = StateCanceled
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		e.job.State = StateFailed
		e.job.Error = fmt.Sprintf("Sign-in timed out after %s", timeout.Round(time.Second))
	default:
		e.job.State = StateFailed
		e.job.Error = errorMessage(err)
	}
	e.stream.Status(e.job)
	retention, after := j.Retention, j.after
	j.mu.Unlock()
	after(retention, j.evict)
	j.notify()
}

// errorMessage keeps a failure on one bounded line. Providers never put a
// secret in an error; the message is shown as is.
func errorMessage(err error) string {
	msg := strings.Join(strings.Fields(strings.ReplaceAll(err.Error(), "\n", "; ")), " ")
	if len(msg) > maxError {
		msg = msg[:maxError] + "…"
	}
	if msg == "" {
		msg = "Sign-in failed"
	}
	return msg
}

// setState moves an active job to state and tells subscribers.
func (j *Jobs) setState(e *job, state State) {
	j.mu.Lock()
	if !e.job.Active() || e.job.State == state {
		j.mu.Unlock()
		return
	}
	e.job.State = state
	e.stream.Status(e.job)
	j.mu.Unlock()
	j.notify()
}

func (j *Jobs) evict() {
	j.mu.Lock()
	j.evictLocked()
	j.mu.Unlock()
}

// evictLocked drops jobs that ended more than Retention ago and frees their
// output. Callers hold j.mu.
func (j *Jobs) evictLocked() {
	now := j.now()
	for id, e := range j.jobs {
		if e.job.EndedAt != nil && now.Sub(*e.job.EndedAt) >= j.Retention {
			e.stream.Release()
			delete(j.jobs, id)
		}
	}
}

// Get returns a retained job.
func (j *Jobs) Get(id string) (Job, bool) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.evictLocked()
	e, ok := j.jobs[id]
	if !ok {
		return Job{}, false
	}
	return e.job, true
}

// List returns the retained jobs, oldest first.
func (j *Jobs) List() []Job {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.evictLocked()
	out := make([]Job, 0, len(j.jobs))
	for _, e := range j.jobs {
		out = append(out, e.job)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].CreatedAt.Before(out[b].CreatedAt) })
	return out
}

// Answer hands secret to a job waiting for one.
func (j *Jobs) Answer(id, secret string) error {
	j.mu.Lock()
	e, ok := j.jobs[id]
	if !ok {
		j.mu.Unlock()
		return ErrNotFound
	}
	if e.answer == nil || e.job.State != StateWaitingSecret {
		j.mu.Unlock()
		return ErrNoPrompt
	}
	e.answer <- secret
	e.answer = nil
	e.job.Prompt = nil
	e.job.State = StateRunning
	e.stream.Status(e.job)
	j.mu.Unlock()
	j.notify()
	return nil
}

// Cancel stops a job: its login process group is killed and a new profile's
// directory removed. It waits a few seconds for the job to end and returns it.
func (j *Jobs) Cancel(id string) (Job, error) {
	j.mu.Lock()
	e, ok := j.jobs[id]
	if !ok {
		j.mu.Unlock()
		return Job{}, ErrNotFound
	}
	if e.job.Active() {
		e.canceled = true
		e.cancel()
	}
	j.mu.Unlock()
	select {
	case <-e.done:
	case <-time.After(cancelWait):
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	return e.job, nil
}

// Close cancels every job and waits for them to end. Later starts fail.
func (j *Jobs) Close() {
	j.mu.Lock()
	j.closed = true
	var done []chan struct{}
	for _, e := range j.jobs {
		if e.job.Active() {
			e.canceled = true
			e.cancel()
		}
		done = append(done, e.done)
	}
	j.mu.Unlock()
	for _, ch := range done {
		<-ch
	}
	j.mu.Lock()
	for _, e := range j.jobs {
		e.stream.DetachAll()
	}
	j.mu.Unlock()
}

// Attach subscribes to a job's terminal, as agentterminal.Manager.Attach does.
func (j *Jobs) Attach(id string, cursor uint64) (uint64, bool, []Frame, <-chan Frame, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	e, ok := j.jobs[id]
	if !ok {
		return 0, false, nil, nil, ErrNotFound
	}
	subscriber, writer, frames, ch := e.stream.Attach(cursor, e.job)
	return subscriber, writer, frames, ch, nil
}

func (j *Jobs) stream(id string) *agentterminal.Stream[Job] {
	j.mu.Lock()
	defer j.mu.Unlock()
	if e := j.jobs[id]; e != nil {
		return e.stream
	}
	return nil
}

// Detach ends a subscription.
func (j *Jobs) Detach(id string, subscriber uint64) {
	if s := j.stream(id); s != nil {
		s.Detach(subscriber)
	}
}

// Input sends keystrokes to the job's current login command. Keystrokes
// between commands are dropped, so the connection stays open.
func (j *Jobs) Input(id string, subscriber uint64, data []byte) error {
	s := j.stream(id)
	if s == nil {
		return ErrNotFound
	}
	if err := s.Input(subscriber, data); err != nil && !errors.Is(err, agentterminal.ErrNoProcess) {
		return err
	}
	return nil
}

// Resize changes the job terminal's size, now and for later commands.
func (j *Jobs) Resize(id string, subscriber uint64, cols, rows int) error {
	if s := j.stream(id); s != nil {
		return s.Resize(subscriber, cols, rows)
	}
	return ErrNotFound
}

// launcher runs each login command in the job terminal, one after another,
// with a separator line between them.
type launcher struct {
	jobs *Jobs
	e    *job
}

func (l launcher) RunForeground(ctx context.Context, prepared agents.PreparedSession) error {
	if l.e.stream.Offset() > 0 {
		_, _ = l.e.stream.Write([]byte("\r\n── " + stepLabel(prepared) + " ──\r\n"))
	}
	l.jobs.setState(l.e, StateRunning)
	cols, rows := l.e.stream.Size()
	code, err := agentterminal.RunInStream(ctx, l.e.stream, prepared, cols, rows)
	l.jobs.setState(l.e, StateVerifying)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err == nil && code != 0 {
		err = fmt.Errorf("exit status %d", code)
	}
	return err
}

// stepLabel names a command by its executable and arguments. Login commands
// never carry a secret in their arguments.
func stepLabel(p agents.PreparedSession) string {
	return strings.Join(append([]string{filepath.Base(p.Executable)}, p.Args...), " ")
}

// prompter asks the browser for a secret: the job waits in waiting_secret
// until Answer or cancellation.
type prompter struct {
	jobs *Jobs
	e    *job
}

func (p prompter) Secret(ctx context.Context, label string) (string, error) {
	answer := make(chan string, 1)
	j := p.jobs
	j.mu.Lock()
	if !p.e.job.Active() {
		j.mu.Unlock()
		return "", ctx.Err()
	}
	p.e.answer = answer
	p.e.job.State = StateWaitingSecret
	p.e.job.Prompt = &Prompt{Label: label}
	p.e.stream.Status(p.e.job)
	j.mu.Unlock()
	j.notify()
	select {
	case secret := <-answer:
		return secret, nil
	case <-ctx.Done():
		j.mu.Lock()
		if p.e.answer == answer {
			p.e.answer = nil
			p.e.job.Prompt = nil
		}
		j.mu.Unlock()
		return "", ctx.Err()
	}
}

// crlfWriter turns bare "\n" into "\r\n" so progress lines render in xterm.
type crlfWriter struct {
	w      interface{ Write([]byte) (int, error) }
	lastCR bool
}

func (c *crlfWriter) Write(b []byte) (int, error) {
	out := make([]byte, 0, len(b)+8)
	for _, ch := range b {
		if ch == '\n' && !c.lastCR {
			out = append(out, '\r')
		}
		out = append(out, ch)
		c.lastCR = ch == '\r'
	}
	if _, err := c.w.Write(out); err != nil {
		return 0, err
	}
	return len(b), nil
}

func newJobID() (string, error) {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "setup_" + hex.EncodeToString(b), nil
}
