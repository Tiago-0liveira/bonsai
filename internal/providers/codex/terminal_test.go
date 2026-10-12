//go:build linux || darwin

package codex

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/agentterminal"
	"github.com/Tiago-0liveira/bonsai/internal/providers/claude"
)

// stubAntigravity is a provider with the Antigravity ID and no real agy.
type stubAntigravity struct{ agents.Provider }

func (stubAntigravity) ID() agents.ProviderID { return "antigravity" }
func (stubAntigravity) Capabilities() agents.Capabilities {
	return agents.Capabilities{Interactive: true, MultiAccount: true, ConcurrentSameAccount: true, ConcurrentCrossAccount: true}
}
func (stubAntigravity) PrepareSession(_ context.Context, r agents.PrepareSessionRequest) (agents.PreparedSession, error) {
	return agents.PreparedSession{
		Executable: "/bin/sh",
		Args:       []string{"-c", `echo AGY-READY; while IFS= read -r l; do echo "AGY:$l"; done`},
		Dir:        r.Session.WorkDir,
	}, nil
}
func (stubAntigravity) FinalizeSession(context.Context, agents.FinalizeSessionRequest) error {
	return nil
}

// finalizeLog wraps a provider and records every FinalizeSession call.
type finalizeLog struct {
	mu    sync.Mutex
	calls map[agents.SessionID]int
	dirs  map[agents.SessionID]string
}

func (l *finalizeLog) record(s agents.Session) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls[s.ID]++
	l.dirs[s.ID] = s.RuntimeDir
}

type countedCodex struct {
	*Provider
	log *finalizeLog
}

func (c countedCodex) FinalizeSession(ctx context.Context, r agents.FinalizeSessionRequest) error {
	c.log.record(r.Session)
	return c.Provider.FinalizeSession(ctx, r)
}

type countedProvider struct {
	agents.Provider
	log *finalizeLog
}

func (c countedProvider) FinalizeSession(ctx context.Context, r agents.FinalizeSessionRequest) error {
	c.log.record(r.Session)
	return c.Provider.FinalizeSession(ctx, r)
}

type terminal struct {
	t      *testing.T
	m      *agentterminal.Manager
	id     string
	writer uint64
	frames <-chan agentterminal.Frame
	output strings.Builder
}

func attach(t *testing.T, m *agentterminal.Manager, id string) *terminal {
	t.Helper()
	writer, writable, replay, frames, err := m.Attach("project", id, 0)
	if err != nil || !writable {
		t.Fatalf("attach %s: writable=%v err=%v", id, writable, err)
	}
	term := &terminal{t: t, m: m, id: id, writer: writer, frames: frames}
	for _, f := range replay {
		term.output.Write(f.Data)
	}
	return term
}

func (term *terminal) waitFor(want string) {
	term.t.Helper()
	deadline := time.After(10 * time.Second)
	for !strings.Contains(term.output.String(), want) {
		select {
		case f, ok := <-term.frames:
			if !ok {
				term.t.Fatalf("terminal closed before %q; output:\n%s", want, term.output.String())
			}
			term.output.Write(f.Data)
		case <-deadline:
			term.t.Fatalf("timeout waiting for %q; output:\n%s", want, term.output.String())
		}
	}
}

func (term *terminal) send(line string) {
	term.t.Helper()
	if err := term.m.Input(term.id, term.writer, []byte(line+"\n")); err != nil {
		term.t.Fatal(err)
	}
}

// claudeAccount adds a Claude login profile without running a login and puts
// the fake claude on PATH.
func claudeAccount(t *testing.T, e *testEnv) agents.Account {
	t.Helper()
	fixtures, err := filepath.Abs(filepath.Join("..", "claude", "testdata"))
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Dir(must(t, func() (string, error) { return e.provider.binaryResolver.Resolve() }))
	if err := os.Symlink(filepath.Join(fixtures, "fakeclaude.sh"), filepath.Join(bin, "claude")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_CLAUDE_FIXTURES", fixtures)
	account := agents.Account{ID: "acct_claude", Provider: "claude", Name: "claude-work"}
	if err := e.accounts.Create(account); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(e.accounts.AccountDir(account.ID), "config"), 0o700); err != nil {
		t.Fatal(err)
	}
	return account
}

func must(t *testing.T, f func() (string, error)) string {
	t.Helper()
	v, err := f()
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestConcurrentCodexClaudeAndAntigravitySessionsThroughTerminal(t *testing.T) {
	e := newTestEnv(t)
	t.Setenv("FAKE_CODEX_EMAIL", "a@example.com")
	work := e.addLogin("work")
	t.Setenv("FAKE_CODEX_EMAIL", "b@example.com")
	other := e.addAPIKey("other")
	claudeWork := claudeAccount(t, e)
	agy := agents.Account{ID: "acct_agy", Provider: "antigravity", Name: "agy"}
	if err := e.accounts.Create(agy); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OPENAI_API_KEY", "sk-parent")
	t.Setenv("CODEX_API_KEY", "parent")

	log := &finalizeLog{calls: map[agents.SessionID]int{}, dirs: map[agents.SessionID]string{}}
	registry := agents.NewRegistry()
	for _, p := range []agents.Provider{
		countedCodex{e.provider, log},
		countedProvider{claude.New(e.accounts, e.sessions), log},
		countedProvider{stubAntigravity{}, log},
	} {
		if err := registry.Register(p); err != nil {
			t.Fatal(err)
		}
	}
	m := agentterminal.New(e.accounts, e.sessions, registry, nil)
	closed := false
	t.Cleanup(func() {
		if !closed {
			m.Close()
		}
	})

	start := func(account agents.AccountID, name string, options agentterminal.StartOptions) agentterminal.Summary {
		t.Helper()
		s, err := m.Start("project", "tree", account, name, t.TempDir(), 80, 24, options)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	type run struct {
		summary agentterminal.Summary
		account agents.Account
		ready   string
		reply   string
	}
	auto := agentterminal.StartOptions{Sandbox: "workspace-write", Approval: "on-request"}
	runs := []run{
		{start(work.ID, "w1", auto), work, "READY", "REPLY:"},
		{start(work.ID, "w2", agentterminal.StartOptions{Sandbox: SandboxBypass, Effort: "high"}), work, "READY", "REPLY:"},
		{start(other.ID, "o1", auto), other, "READY", "REPLY:"},
		{start(other.ID, "o2", agentterminal.StartOptions{Prompt: "--literal"}), other, "READY", "REPLY:"},
		{start(claudeWork.ID, "c1", agentterminal.StartOptions{}), claudeWork, "READY", "REPLY:"},
		{start(agy.ID, "a1", agentterminal.StartOptions{}), agy, "AGY-READY", "AGY:"},
	}
	terms := make([]*terminal, len(runs))
	for i, r := range runs {
		awaitState(t, m, r.summary.ID, "running")
		terms[i] = attach(t, m, r.summary.ID)
		terms[i].waitFor(r.ready)
	}
	for i := range runs {
		terms[i].send("hello " + runs[i].summary.Name)
	}
	for i, r := range runs {
		terms[i].waitFor(r.reply + "hello " + r.summary.Name)
		for j, o := range runs {
			if j != i && strings.Contains(terms[i].output.String(), "hello "+o.summary.Name) {
				t.Fatalf("terminal %s received input for %s:\n%s", r.summary.Name, o.summary.Name, terms[i].output.String())
			}
		}
	}
	for i, r := range runs[:4] {
		terms[i].waitFor("CODEX_HOME=" + e.profileHome(r.account))
		terms[i].waitFor("SESSION=" + r.summary.ID)
		terms[i].waitFor("HAS_OPENAI_KEY=0")
		terms[i].waitFor("HAS_CODEX_KEY=0")
		terms[i].waitFor("HOME=" + e.home)
		terms[i].waitFor("ARG:--no-daemon")
	}
	terms[0].waitFor("ARG:-s\r\nARG:workspace-write\r\nARG:-a\r\nARG:on-request")
	terms[1].waitFor("ARG:model_reasoning_effort=\"high\"\r\nARG:--dangerously-bypass-approvals-and-sandbox")
	terms[3].waitFor("ARG:--\r\nARG:--literal")
	terms[4].waitFor("CONFIG_DIR=" + filepath.Join(e.accounts.AccountDir(claudeWork.ID), "config"))

	if err := m.Stop("project", runs[0].summary.ID); err != nil {
		t.Fatal(err)
	}
	awaitState(t, m, runs[0].summary.ID, "exited", "stopped")
	for i, r := range runs[1:] {
		if s, _ := m.Get("project", r.summary.ID); s.State != "running" {
			t.Fatalf("stopping one session changed %s to %s", r.summary.Name, s.State)
		}
		terms[i+1].send("still alive")
		terms[i+1].waitFor(r.reply + "still alive")
	}

	m.Close()
	closed = true
	log.mu.Lock()
	defer log.mu.Unlock()
	for _, r := range runs {
		id := agents.SessionID(r.summary.ID)
		if log.calls[id] != 1 {
			t.Errorf("%s finalized %d times", r.summary.Name, log.calls[id])
		}
		if _, err := os.Stat(log.dirs[id]); !os.IsNotExist(err) {
			t.Errorf("%s runtime dir left behind: %v", r.summary.Name, err)
		}
	}
}

// Invalid launch options fail before any session or runtime dir exists.
func TestInvalidLaunchCreatesNoSession(t *testing.T) {
	e := newTestEnv(t)
	work := e.addLogin("work")
	m := agentterminal.New(e.accounts, e.sessions, e.registry, nil)
	t.Cleanup(m.Close)
	before, _ := os.ReadDir(e.sessions.Root())
	for _, launch := range []agentterminal.StartOptions{{Sandbox: "yolo"}, {Sandbox: SandboxBypass, Approval: "never"}, {PermissionMode: "plan"}, {Effort: "huge"}} {
		_, err := m.Start("project", "tree", work.ID, "x", t.TempDir(), 80, 24, launch)
		if !errors.Is(err, agents.ErrInvalidLaunch) {
			t.Fatalf("%+v: err = %v", launch, err)
		}
	}
	if after, _ := os.ReadDir(e.sessions.Root()); len(after) != len(before) {
		t.Fatalf("sessions dir changed: %v -> %v", before, after)
	}
	if list := m.List("project"); len(list) != 0 {
		t.Fatalf("sessions = %+v", list)
	}
}

func awaitState(t *testing.T, m *agentterminal.Manager, id string, states ...string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		s, _ := m.Get("project", id)
		for _, want := range states {
			if s.State == want {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	s, _ := m.Get("project", id)
	t.Fatalf("session %s: want %v, got %+v", id, states, s)
}
