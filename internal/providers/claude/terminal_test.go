//go:build linux || darwin

package claude

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/agentterminal"
)

// stubAntigravity is a second provider with the Antigravity ID and no real agy.
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

func TestConcurrentClaudeSessionsAndAntigravityThroughTerminal(t *testing.T) {
	e := newTestEnv(t)
	work := e.addLogin("work")
	agy := agents.Account{ID: "acct_agy", Provider: "antigravity", Name: "agy"}
	if err := e.accounts.Create(agy); err != nil {
		t.Fatal(err)
	}
	registry := agents.NewRegistry()
	for _, p := range []agents.Provider{e.provider, stubAntigravity{}} {
		if err := registry.Register(p); err != nil {
			t.Fatal(err)
		}
	}
	m := agentterminal.New(e.accounts, e.sessions, registry, nil)
	t.Cleanup(m.Close)

	start := func(account agents.AccountID, name string, options ...agentterminal.StartOptions) agentterminal.Summary {
		t.Helper()
		s, err := m.Start("project", "tree", account, name, t.TempDir(), 80, 24, options...)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	one := start(work.ID, "first")
	two := start(work.ID, "second")
	other := start(agy.ID, "")
	for _, s := range []agentterminal.Summary{one, two, other} {
		awaitRunning(t, m, s.ID)
	}
	t1, t2, t3 := attach(t, m, one.ID), attach(t, m, two.ID), attach(t, m, other.ID)
	t1.waitFor("READY")
	t2.waitFor("READY")
	t3.waitFor("AGY-READY")

	t1.send("hello one")
	t2.send("hello two")
	t3.send("hello agy")
	t1.waitFor("REPLY:hello one")
	t2.waitFor("REPLY:hello two")
	t3.waitFor("AGY:hello agy")
	for _, wrong := range []struct {
		term *terminal
		text string
	}{{t1, "hello two"}, {t2, "hello one"}, {t1, "AGY:"}, {t3, "REPLY:"}} {
		if strings.Contains(wrong.term.output.String(), wrong.text) {
			t.Errorf("terminal %s received %q:\n%s", wrong.term.id, wrong.text, wrong.term.output.String())
		}
	}

	s1, _ := m.Get("project", one.ID)
	s2, _ := m.Get("project", two.ID)
	if s1.ProviderSessionID == "" || s1.ProviderSessionID == s2.ProviderSessionID {
		t.Fatalf("provider session IDs = %q, %q", s1.ProviderSessionID, s2.ProviderSessionID)
	}
	t1.waitFor("ARG:--session-id\r\nARG:" + s1.ProviderSessionID)
	t2.waitFor("ARG:--session-id\r\nARG:" + s2.ProviderSessionID)
	t1.waitFor("CONFIG_DIR=" + configDir(e.accounts, work))
	t2.waitFor("CONFIG_DIR=" + configDir(e.accounts, work))
	t1.waitFor("SESSION=" + one.ID)
	t2.waitFor("SESSION=" + two.ID)
	t1.waitFor("HOME=" + e.home)
	t1.waitFor("HAS_API_KEY=0")

	if err := m.Stop("project", one.ID); err != nil {
		t.Fatal(err)
	}
	awaitState(t, m, one.ID, "exited", "stopped")
	for _, term := range []*terminal{t2, t3} {
		s, _ := m.Get("project", term.id)
		if s.State != "running" {
			t.Fatalf("stopping one session changed %s to %s", term.id, s.State)
		}
	}
	t2.send("still alive")
	t2.waitFor("REPLY:still alive")
}

func awaitRunning(t *testing.T, m *agentterminal.Manager, id string) {
	t.Helper()
	awaitState(t, m, id, "running")
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
