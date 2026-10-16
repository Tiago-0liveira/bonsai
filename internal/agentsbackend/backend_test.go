//go:build !windows

package agentsbackend

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/agentruntime"
	"github.com/Tiago-0liveira/bonsai/internal/core/agenthost"
	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/agentterminal"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
)

const providerID agents.ProviderID = "fake"

// fakeProvider runs a shell script as the agent.
type fakeProvider struct {
	agents.Provider
	script string
}

func (p *fakeProvider) ID() agents.ProviderID { return providerID }
func (p *fakeProvider) Label() string         { return "Fake Agent" }
func (p *fakeProvider) Capabilities() agents.Capabilities {
	return agents.Capabilities{Interactive: true, ConcurrentSameAccount: true, ConcurrentCrossAccount: true}
}
func (p *fakeProvider) Availability(context.Context) agents.Availability {
	return agents.Availability{Available: true}
}
func (p *fakeProvider) PrepareSession(_ context.Context, r agents.PrepareSessionRequest) (agents.PreparedSession, error) {
	return agents.PreparedSession{Executable: "/bin/sh", Args: []string{"-c", p.script, "agent-fixture"}, Dir: r.Session.WorkDir, EnvSet: map[string]string{"HOME": r.Session.HomeDir}}, nil
}
func (p *fakeProvider) FinalizeSession(context.Context, agents.FinalizeSessionRequest) error {
	return nil
}

type env struct {
	backend *Backend
	api     agentsapi.Backend
	rt      *agentruntime.Runtime
	paths   agenthost.Paths
	account agents.AccountID
	launch  agenthost.Launcher
}

// newEnv builds a backend whose holders run in this process. A restricted PATH
// keeps real agent CLIs out of reach.
func newEnv(t *testing.T, script string) *env {
	t.Helper()
	t.Setenv("PATH", "/usr/bin:/bin")
	accounts, err := agents.NewFileAccountStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sessions, err := agents.NewFileSessionStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	registry := agents.NewRegistry()
	if err := registry.Register(&fakeProvider{script: script}); err != nil {
		t.Fatal(err)
	}
	id, _ := agents.NewAccountID()
	if err := accounts.Create(agents.Account{ID: id, Provider: providerID, Name: "fixture"}); err != nil {
		t.Fatal(err)
	}
	// Socket paths must stay short, so they cannot live under t.TempDir on macOS.
	sockets, err := os.MkdirTemp("/tmp", "abt")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(sockets) })
	paths := agenthost.Paths{Records: filepath.Join(t.TempDir(), "agents"), Sockets: sockets}
	rt := &agentruntime.Runtime{Accounts: accounts, Sessions: sessions, Registry: registry}
	launch := agenthost.InProcess(agenthost.Deps{Accounts: accounts, Sessions: sessions, Registry: registry, Linger: 200 * time.Millisecond})
	b, err := New(WithPaths(paths), WithLauncher(launch), WithRuntime(rt))
	if err != nil {
		t.Fatal(err)
	}
	e := &env{backend: b, api: b.API(), rt: rt, paths: paths, account: id, launch: launch}
	t.Cleanup(func() {
		_ = b.Close()
		stopAll(paths)
	})
	return e
}

func stopAll(paths agenthost.Paths) {
	records, _ := agenthost.List(paths)
	for _, r := range records {
		if r.Alive {
			_ = agenthost.StopHolder(paths, r, "")
			agenthost.Wait(paths, r.ID, 10*time.Second)
		}
	}
}

func (e *env) start(t *testing.T) agentsapi.Session {
	t.Helper()
	s, err := e.api.Sessions.Start(context.Background(), agentsapi.StartRequest{ProjectID: "p", WorktreeID: "w", WorkDir: t.TempDir(), AccountID: e.account, Name: "n", Cols: 80, Rows: 24})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func attach(t *testing.T, e *env, id string, opts agentsapi.AttachOptions) agentsapi.Stream {
	t.Helper()
	st, err := e.api.Sessions.Attach(context.Background(), id, opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

// waitFrame reads frames until match, failing on timeout or channel close.
func waitFrame(t *testing.T, st agentsapi.Stream, match func(agentterminal.Frame) bool) agentterminal.Frame {
	t.Helper()
	timeout := time.After(10 * time.Second)
	for {
		select {
		case f, ok := <-st.Frames():
			if !ok {
				t.Fatal("frames closed before the expected frame")
			}
			if match(f) {
				return f
			}
		case <-timeout:
			t.Fatal("timed out waiting for frame")
		}
	}
}

const chatty = `echo hello-replay; cat`

func TestReadOnlyAttachReplaysAndRejectsInput(t *testing.T) {
	e := newEnv(t, chatty)
	s := e.start(t)
	st := attach(t, e, s.ID, agentsapi.AttachOptions{ReadOnly: true})
	if st.Writer() {
		t.Fatal("read-only attachment is the writer")
	}
	got := ""
	waitFrame(t, st, func(f agentterminal.Frame) bool {
		got += string(f.Data)
		return f.Type == "output" && containsStr(got, "hello-replay")
	})
	if err := st.Input([]byte("x")); err == nil {
		t.Fatal("Input accepted on read-only attachment")
	}
	if err := st.Take(); err == nil {
		t.Fatal("Take accepted on read-only attachment")
	}
	if cols, rows := st.Size(); cols != 80 || rows != 24 {
		t.Fatalf("size = %dx%d", cols, rows)
	}
}

func containsStr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestAutoAttachAloneBecomesWriter(t *testing.T) {
	e := newEnv(t, chatty)
	s := e.start(t)
	st := attach(t, e, s.ID, agentsapi.AttachOptions{})
	if !st.Writer() {
		t.Fatal("sole attachment is not the writer")
	}
	if err := st.Input([]byte("ping\n")); err != nil {
		t.Fatal(err)
	}
	if err := st.Resize(100, 30); err != nil {
		t.Fatal(err)
	}
}

func TestTakeDemotesPreviousWriter(t *testing.T) {
	e := newEnv(t, chatty)
	s := e.start(t)
	first := attach(t, e, s.ID, agentsapi.AttachOptions{})
	if !first.Writer() {
		t.Fatal("first attachment is not the writer")
	}
	second := attach(t, e, s.ID, agentsapi.AttachOptions{Take: true})
	if !second.Writer() {
		t.Fatal("taking attachment is not the writer")
	}
	waitFrame(t, first, func(f agentterminal.Frame) bool { return f.Type == "writer" && f.Writer != nil && !*f.Writer })
	if first.Writer() {
		t.Fatal("demoted attachment still reports writer")
	}
}

func TestObserverResizeRejected(t *testing.T) {
	e := newEnv(t, chatty)
	s := e.start(t)
	writer := attach(t, e, s.ID, agentsapi.AttachOptions{})
	observer := attach(t, e, s.ID, agentsapi.AttachOptions{ReadOnly: true})
	if err := observer.Resize(90, 30); err == nil {
		t.Fatal("observer Resize accepted")
	}
	if err := writer.Resize(90, 30); err != nil {
		t.Fatal(err)
	}
}

func TestSizeFollowsResize(t *testing.T) {
	old := sizePollInterval
	sizePollInterval = 20 * time.Millisecond
	t.Cleanup(func() { sizePollInterval = old })
	e := newEnv(t, chatty)
	s := e.start(t)
	writer := attach(t, e, s.ID, agentsapi.AttachOptions{})
	observer := attach(t, e, s.ID, agentsapi.AttachOptions{ReadOnly: true})
	if err := writer.Resize(101, 33); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		if c, r := observer.Size(); c == 101 && r == 33 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("observer size never followed the resize")
		}
		runtime.Gosched()
	}
}

// settle waits for the goroutine count to fall to at most want.
func settle(t *testing.T, want int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for runtime.NumGoroutine() > want {
		if time.Now().After(deadline) {
			buf := make([]byte, 1<<16)
			t.Fatalf("goroutines = %d, want <= %d\n%s", runtime.NumGoroutine(), want, buf[:runtime.Stack(buf, true)])
		}
		runtime.Gosched()
	}
}

type floodConn struct {
	sent    atomic.Int32
	allRead chan struct{}
	closed  chan struct{}
	once    sync.Once
}

func (c *floodConn) Recv() (agenthost.Frame, error) {
	n := c.sent.Add(1)
	if n <= 257 {
		if n == 257 {
			close(c.allRead)
		}
		return agenthost.Frame{Type: "output", Data: []byte("x")}, nil
	}
	<-c.closed
	return agenthost.Frame{}, io.EOF
}
func (c *floodConn) Send(agenthost.Request) error { return nil }
func (c *floodConn) Close() error {
	c.once.Do(func() { close(c.closed) })
	return nil
}

func TestCloseWithBlockedReaderLeavesNoGoroutine(t *testing.T) {
	base := runtime.NumGoroutine()
	b := &Backend{streams: map[*stream]struct{}{}}
	c := &floodConn{allRead: make(chan struct{}), closed: make(chan struct{})}
	st := newStream(b, agenthost.Record{}, c, agenthost.Frame{}, true, agenthost.Info{Cols: 80, Rows: 24})
	if !b.register(st) {
		t.Fatal("register failed")
	}
	t.Cleanup(func() { _ = st.Close() })
	select {
	case <-c.allRead:
	case <-time.After(10 * time.Second):
		t.Fatal("reader did not reach the blocking send")
	}
	if len(st.Frames()) != cap(st.frames) {
		t.Fatalf("frames = %d, want full channel", len(st.Frames()))
	}
	done := make(chan struct{})
	go func() { _ = st.Close(); _ = st.Close(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Close blocked")
	}
	for range st.Frames() {
	}
	settle(t, base)
}

func TestChangesFiresWhenAnotherStarterStarts(t *testing.T) {
	e := newEnv(t, chatty)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	changes, err := e.api.Sessions.Changes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	other := agenthost.Starter{Accounts: e.rt.Accounts, Sessions: e.rt.Sessions, Registry: e.rt.Registry, Paths: e.paths, Launch: e.launch, Origin: "cli"}
	if _, err := other.Start(ctx, agenthost.StartRequest{Project: "p", Tree: "w", Dir: t.TempDir(), AccountID: e.account, Cols: 80, Rows: 24}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-changes:
	case <-time.After(10 * time.Second):
		t.Fatal("no change signal")
	}
	list, err := e.api.Sessions.List(ctx)
	if err != nil || len(list) != 1 || list[0].Origin != "cli" {
		t.Fatalf("list = %+v, %v", list, err)
	}
}

func TestStartRecordsTUIOriginAndWorkDir(t *testing.T) {
	e := newEnv(t, chatty)
	dir := t.TempDir()
	s, err := e.api.Sessions.Start(context.Background(), agentsapi.StartRequest{ProjectID: "p", WorktreeID: "w", WorkDir: dir, AccountID: e.account, Cols: 80, Rows: 24})
	if err != nil {
		t.Fatal(err)
	}
	if s.Origin != "tui" || s.ProjectID != "p" || s.WorktreeID != "w" {
		t.Fatalf("summary = %+v", s.Summary)
	}
	if s.WorkDir != filepath.Clean(dir) {
		t.Fatalf("WorkDir = %q, want %q", s.WorkDir, dir)
	}
	record, err := agenthost.Read(e.paths, s.ID)
	if err != nil || record.Summary.Origin != "tui" {
		t.Fatalf("record = %+v, %v", record.Summary, err)
	}
}

func TestStopUnknownSession(t *testing.T) {
	e := newEnv(t, chatty)
	if err := e.api.Sessions.Stop(context.Background(), "sess_deadbeef"); err != ErrSessionNotFound {
		t.Fatalf("err = %v", err)
	}
	if _, err := e.api.Sessions.Attach(context.Background(), "sess_deadbeef", agentsapi.AttachOptions{}); err != ErrSessionNotFound {
		t.Fatalf("attach err = %v", err)
	}
}

func TestStopEndsSession(t *testing.T) {
	e := newEnv(t, chatty)
	s := e.start(t)
	if err := e.api.Sessions.Stop(context.Background(), s.ID); err != nil {
		t.Fatal(err)
	}
	if !agenthost.Wait(e.paths, s.ID, 10*time.Second) {
		t.Fatal("holder still running after Stop")
	}
}

func TestBackendCloseKeepsHoldersRunning(t *testing.T) {
	e := newEnv(t, chatty)
	s := e.start(t)
	st, err := e.api.Sessions.Attach(context.Background(), s.ID, agentsapi.AttachOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.backend.Close(); err != nil {
		t.Fatal(err)
	}
	for range st.Frames() { // closes because Close ended the stream
	}
	record, err := agenthost.Read(e.paths, s.ID)
	if err != nil || !record.Alive || !record.Summary.Active() {
		t.Fatalf("holder stopped by Backend.Close: %+v, %v", record.Summary, err)
	}
	if _, err := e.api.Sessions.Attach(context.Background(), s.ID, agentsapi.AttachOptions{}); err == nil {
		t.Fatal("attach after Close succeeded")
	}
}

func TestUnsupportedWithoutPaths(t *testing.T) {
	rt := &agentruntime.Runtime{Registry: agents.NewRegistry()}
	b, err := New(WithPaths(agenthost.Paths{}), WithRuntime(rt))
	if err != nil {
		t.Fatal(err)
	}
	if b.API().Sessions.Supported() {
		t.Fatal("supported without record paths")
	}
	if list, err := b.API().Sessions.List(context.Background()); err != nil || len(list) != 0 {
		t.Fatalf("list = %v, %v", list, err)
	}
}

func TestAttachSurfacesVersionMismatch(t *testing.T) {
	e := newEnv(t, chatty)
	s := e.start(t)
	path := filepath.Join(e.paths.Records, s.ID+".json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var rec map[string]any
	if err := json.Unmarshal(raw, &rec); err != nil {
		t.Fatal(err)
	}
	rec["protocol"] = agenthost.Version + 1
	mismatched, _ := json.Marshal(rec)
	if err := os.WriteFile(path, mismatched, 0o600); err != nil {
		t.Fatal(err)
	}
	// Cleanup stops the holder, and for a record it cannot dial that falls back
	// to signalling the PID, which here is the test process itself.
	t.Cleanup(func() { _ = os.WriteFile(path, raw, 0o600) })
	_, err = e.api.Sessions.Attach(context.Background(), s.ID, agentsapi.AttachOptions{ReadOnly: true})
	if !errors.Is(err, agentsapi.ErrVersion) {
		t.Fatalf("err = %v, want agentsapi.ErrVersion", err)
	}
}

func TestStreamReportsHolderGeneration(t *testing.T) {
	st := newStream(&Backend{streams: map[*stream]struct{}{}}, agenthost.Record{}, &floodConn{allRead: make(chan struct{}), closed: make(chan struct{})},
		agenthost.Frame{Generation: "gen-7"}, true, agenthost.Info{Cols: 80, Rows: 24})
	if got := st.Generation(); got != "gen-7" {
		t.Errorf("Generation = %q, want the ready frame's", got)
	}
	if got := newEndedStream(agenthost.Record{}).Generation(); got != "" {
		t.Errorf("ended stream Generation = %q, want empty", got)
	}
}
