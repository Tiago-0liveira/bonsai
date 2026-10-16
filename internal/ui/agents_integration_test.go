//go:build !windows

package ui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/agentruntime"
	"github.com/Tiago-0liveira/bonsai/internal/agentsbackend"
	"github.com/Tiago-0liveira/bonsai/internal/core/agenthost"
	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
)

const integrationProvider agents.ProviderID = "fake"

// scriptProvider runs a shell script as the agent.
type scriptProvider struct {
	agents.Provider
	script string
}

func (p *scriptProvider) ID() agents.ProviderID { return integrationProvider }
func (p *scriptProvider) Label() string         { return "Fake Agent" }
func (p *scriptProvider) Capabilities() agents.Capabilities {
	return agents.Capabilities{Interactive: true, ConcurrentSameAccount: true, ConcurrentCrossAccount: true}
}
func (p *scriptProvider) Availability(context.Context) agents.Availability {
	return agents.Availability{Available: true}
}
func (p *scriptProvider) PrepareSession(_ context.Context, r agents.PrepareSessionRequest) (agents.PreparedSession, error) {
	return agents.PreparedSession{Executable: "/bin/sh", Args: []string{"-c", p.script, "agent-fixture"}, Dir: r.Session.WorkDir, EnvSet: map[string]string{"HOME": r.Session.HomeDir}}, nil
}
func (p *scriptProvider) FinalizeSession(context.Context, agents.FinalizeSessionRequest) error {
	return nil
}

// newRealBackend builds an agentsbackend whose holders run in this process, on
// temp stores and paths, with real agent CLIs out of PATH.
func newRealBackend(t *testing.T, script string) (agentsapi.Backend, agents.AccountID) {
	t.Helper()
	t.Setenv("PATH", "/usr/bin:/bin")
	t.Setenv("HOME", t.TempDir())
	accounts, err := agents.NewFileAccountStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sessions, err := agents.NewFileSessionStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	registry := agents.NewRegistry()
	if err := registry.Register(&scriptProvider{script: script}); err != nil {
		t.Fatal(err)
	}
	id, _ := agents.NewAccountID()
	if err := accounts.Create(agents.Account{ID: id, Provider: integrationProvider, Name: "fixture"}); err != nil {
		t.Fatal(err)
	}
	// Unix socket paths are short-limited, so they cannot live under t.TempDir on macOS.
	sockets, err := os.MkdirTemp("/tmp", "uiit")
	if err != nil {
		t.Fatal(err)
	}
	paths := agenthost.Paths{Records: filepath.Join(t.TempDir(), "agents"), Sockets: sockets}
	rt := &agentruntime.Runtime{Accounts: accounts, Sessions: sessions, Registry: registry}
	launch := agenthost.InProcess(agenthost.Deps{Accounts: accounts, Sessions: sessions, Registry: registry, Linger: 200 * time.Millisecond})
	b, err := agentsbackend.New(agentsbackend.WithPaths(paths), agentsbackend.WithLauncher(launch), agentsbackend.WithRuntime(rt))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = b.Close()
		records, _ := agenthost.List(paths)
		for _, r := range records {
			if r.Alive {
				_ = agenthost.StopHolder(paths, r, "")
				agenthost.Wait(paths, r.ID, 10*time.Second)
			}
		}
		_ = os.RemoveAll(sockets)
	})
	return b.API(), id
}

// within runs the blocking command and fails the test if it takes too long.
func within(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	select {
	case m := <-done:
		return m
	case <-time.After(10 * time.Second):
		t.Fatal("command did not finish")
		return nil
	}
}

func TestAgentsIntegrationLiveReadOnlyView(t *testing.T) {
	// No newline after the text: the observer view keeps the cursor row visible, so
	// a cursor on row 1 would scroll the first row out of this short pane.
	api, account := newRealBackend(t, `printf 'KNOWN SCREEN'; cat`)
	e := newAgentsEnv(t, WithAgents(api))

	s, err := api.Sessions.Start(t.Context(), agentsapi.StartRequest{
		ProjectID: e.project, WorkDir: e.wtA, AccountID: account, Name: "Fixture", Cols: 80, Rows: 24,
	})
	if err != nil {
		t.Fatal(err)
	}
	e.apply(t, e.m.relistAgents())
	e.openTab(t)

	v := e.m.agents.view
	if v == nil || v.sessionID != s.ID {
		t.Fatalf("view = %+v, want an attach of %s", v, s.ID)
	}
	// The command the update returned would run these two steps: dial, then pump.
	nm, _ := e.m.Update(within(t, attachCmd(e.m.agents.ctx, api.Sessions, v.sessionID, v.token, v.attachOptions())))
	e.m = nm.(Model)
	if v.err != "" || v.stream == nil {
		t.Fatalf("attach failed: %q", v.err)
	}
	if v.stream.Writer() {
		t.Error("the live view must be read-only")
	}

	pump := func(until func() bool) {
		t.Helper()
		for i := 0; i < 50 && !until(); i++ {
			msg := within(t, waitFramesCmd(v.token, v.stream))
			nm, _ := e.m.Update(msg)
			e.m = nm.(Model)
		}
		if !until() {
			t.Fatalf("condition not reached; screen:\n%s", ansi.Strip(e.m.View()))
		}
	}
	screen := func() string { return ansi.Strip(e.m.View()) }
	pump(func() bool { return strings.Contains(screen(), "KNOWN SCREEN") })
	if v.replaying {
		t.Error("replay should have ended at the first status frame")
	}

	// Stopping the agent ends the stream with a banner and keeps the screen.
	if err := api.Sessions.Stop(t.Context(), s.ID); err != nil {
		t.Fatal(err)
	}
	pump(func() bool { return v.state == attachEnded })
	if got := screen(); !strings.Contains(got, "KNOWN SCREEN") || !strings.Contains(got, "exited") {
		t.Errorf("ended screen:\n%s", got)
	}
}

func TestAgentsIntegrationTerminalMode(t *testing.T) {
	api, account := newRealBackend(t, `printf 'KNOWN SCREEN'; cat`)
	e := newAgentsEnv(t, WithAgents(api))
	s, err := api.Sessions.Start(t.Context(), agentsapi.StartRequest{
		ProjectID: e.project, WorkDir: e.wtA, AccountID: account, Name: "Fixture", Cols: 80, Rows: 24,
	})
	if err != nil {
		t.Fatal(err)
	}
	e.apply(t, e.m.relistAgents())
	e.openTab(t)
	v := e.m.agents.view
	deliver := func(msg tea.Msg) {
		nm, cmd := e.m.Update(msg)
		e.m, e.lastCmd = nm.(Model), cmd
		e.run(t, cmd)
	}
	deliver(within(t, attachCmd(e.m.agents.ctx, api.Sessions, v.sessionID, v.token, v.attachOptions())))
	pump := func(until func() bool) {
		t.Helper()
		for i := 0; i < 50 && !until(); i++ {
			deliver(within(t, waitFramesCmd(v.token, v.stream)))
		}
		if !until() {
			t.Fatalf("condition not reached; screen:\n%s", ansi.Strip(e.m.View()))
		}
	}
	screen := func() string { return ansi.Strip(e.m.View()) }
	pump(func() bool { return strings.Contains(screen(), "KNOWN SCREEN") && !v.replaying })

	// enter: terminal mode as the writer, at the offset the viewer reached.
	deliver(tea.KeyMsg{Type: tea.KeyEnter})
	// deliver also ran the commands the key produced: release, dial, nudge.
	if !v.control || v.opts.ReadOnly {
		t.Fatalf("control=%v opts=%+v", v.control, v.opts)
	}
	pump(func() bool { return v.state == attachControlling && !v.replaying })
	if !v.stream.Writer() {
		t.Fatal("terminal mode must hold the write role")
	}

	// Typed keys travel to the script (the pty echoes them back).
	for _, r := range "hi there" {
		deliver(runes(string(r)))
	}
	e.pump(t)
	pump(func() bool { return strings.Contains(screen(), "hi there") })

	// ctrl+] d: leave terminal mode; the holder keeps running.
	deliver(ctrlBracket)
	deliver(runes("d"))
	if v.control {
		t.Fatal("still in terminal mode")
	}
	pump(func() bool { return v.state == attachObserving && !v.replaying })
	if v.stream.Writer() {
		t.Error("detach must give up the write role")
	}
	list, err := api.Sessions.List(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, got := range list {
		if got.ID == s.ID && got.State != "running" {
			t.Errorf("holder state = %q after detach, want running", got.State)
		}
	}
	if !strings.Contains(screen(), "hi there") {
		t.Errorf("the screen lost the typed text:\n%s", screen())
	}
}
