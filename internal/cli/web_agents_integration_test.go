//go:build linux || darwin

package cli

import (
	"bytes"
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agenthost"
	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/agentterminal"
	"github.com/Tiago-0liveira/bonsai/internal/core/config"
)

type fakeAgentProvider struct{ agents.Provider }

func (fakeAgentProvider) ID() agents.ProviderID { return "fake" }
func (fakeAgentProvider) Capabilities() agents.Capabilities {
	return agents.Capabilities{Interactive: true}
}
func (fakeAgentProvider) PrepareSession(_ context.Context, r agents.PrepareSessionRequest) (agents.PreparedSession, error) {
	return agents.PreparedSession{Executable: "/bin/sh", Args: []string{"-c", `printf 'FAKE-READY\n'; while IFS= read -r line; do printf 'ECHO:%s\n' "$line"; done`}, Dir: r.Session.WorkDir, EnvSet: map[string]string{"HOME": r.Session.HomeDir}}, nil
}
func (fakeAgentProvider) FinalizeSession(context.Context, agents.FinalizeSessionRequest) error {
	return nil
}

// startFakeAgent serves one agent session from the test process under the
// user's holder paths, as a real `bonsai __agent-host` would.
func startFakeAgent(t *testing.T, name string) agenthost.Record {
	t.Helper()
	paths, err := agenthost.DefaultPaths()
	if err != nil {
		t.Fatal(err)
	}
	accounts, err := agents.NewFileAccountStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	id, _ := agents.NewAccountID()
	account := agents.Account{ID: id, Provider: "fake", Name: "fixture"}
	if err := accounts.Create(account); err != nil {
		t.Fatal(err)
	}
	sessions, err := agents.NewFileSessionStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	registry := agents.NewRegistry()
	if err := registry.Register(fakeAgentProvider{}); err != nil {
		t.Fatal(err)
	}
	runtime, err := sessions.Create(account, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	spec := agenthost.Spec{Summary: agentterminal.Summary{ID: string(runtime.ID), ProjectID: "project", WorktreeID: "tree", AccountID: id, Provider: "fake", ProfileName: "fixture", Name: name, State: "starting", CreatedAt: time.Now().UTC()}, WorkDir: runtime.WorkDir, Cols: 80, Rows: 24}
	ready := make(chan error, 1)
	exited := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		defer close(exited)
		_ = agenthost.Serve(ctx, spec, agenthost.Deps{Accounts: accounts, Sessions: sessions, Registry: registry, Paths: paths, Linger: 200 * time.Millisecond}, func(err error) { ready <- err })
	}()
	if err := <-ready; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cancel()
		<-exited
	})
	if !waitUntil(5*time.Second, func() bool {
		r, err := agenthost.Read(paths, spec.Summary.ID)
		return err == nil && r.Summary.State == "running"
	}) {
		t.Fatal("fake agent never ran")
	}
	r, err := agenthost.Read(paths, spec.Summary.ID)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func agentAlive(t *testing.T, r agenthost.Record) bool {
	t.Helper()
	paths, _ := agenthost.DefaultPaths()
	current, err := agenthost.Read(paths, r.ID)
	return err == nil && current.Alive
}

func TestWebStopAndRunningAgents(t *testing.T) {
	e := newWebEnv(t)
	port := strconv.Itoa(freePort(t))
	start := func() { e.mustRun(t, "web", "--no-open", "--no-setup", "--port", port) }

	// Setting off, on a terminal: the stop asks first and N stops nothing.
	start()
	asked := startFakeAgent(t, "asked")
	w, err := newWebCLI(strings.NewReader("n\n"), &bytes.Buffer{}, &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	w.out, w.errOut, w.tty = &out, &out, true
	if err := w.stop(nil); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"ID", "asked", shortAgentID(asked.ID), "1 agent is still running and will be stopped. Stop anyway? [y/N]", "--keep-agents", "Nothing was stopped."} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("prompt lacks %q:\n%s", want, out.String())
		}
	}
	if !agentAlive(t, asked) || !portListening(t, port) {
		t.Fatal("answering N stopped something")
	}
	out.Reset()
	w.in = strings.NewReader("y\n")
	if err := w.stop(nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "✓ bonsai web stopped") || !strings.Contains(out.String(), "✓ stopped 1 agent") || agentAlive(t, asked) {
		t.Fatalf("confirmed stop:\n%s", out.String())
	}

	// Scripts get a warning and the stop proceeds.
	start()
	scripted := startFakeAgent(t, "scripted")
	stdout, stderr, code := e.run(t, "web", "stop")
	if code != 0 || !strings.Contains(stderr, "warning: stopping 1 agent") || !strings.Contains(stdout, "✓ stopped 1 agent") || agentAlive(t, scripted) {
		t.Fatalf("scripted stop %d:\n%s\n%s", code, stdout, stderr)
	}

	// --keep-agents keeps them for one stop; --stop-agents ends them without asking.
	start()
	kept := startFakeAgent(t, "kept")
	if out := e.mustRun(t, "web", "stop", "--keep-agents"); !strings.Contains(out, "1 agent still running — see `bonsai agents`") || !agentAlive(t, kept) {
		t.Fatalf("--keep-agents:\n%s", out)
	}
	if out := e.mustRun(t, "web", "status"); !strings.Contains(out, "1 agent still running") {
		t.Fatalf("status while stopped:\n%s", out)
	}
	if out := e.mustRun(t, "web", "stop", "--stop-agents"); !strings.Contains(out, "✓ stopped 1 agent") || agentAlive(t, kept) {
		t.Fatalf("--stop-agents without the stack:\n%s", out)
	}

	// Setting on: no prompt, the agent outlives the stack.
	if _, err := config.UpdateWebConfig(w.configPath, ^uint64(0), func(cfg *config.WebConfig) error {
		cfg.Agents.KeepRunning = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	start()
	survivor := startFakeAgent(t, "survivor")
	if out := e.mustRun(t, "web", "status"); !strings.Contains(out, "agents    1 running · kept running by bonsai web stop") {
		t.Fatalf("status:\n%s", out)
	}
	stdout, stderr, code = e.run(t, "web", "stop")
	if code != 0 || strings.Contains(stderr, "warning") || !strings.Contains(stdout, "1 agent still running — see `bonsai agents`") || !agentAlive(t, survivor) {
		t.Fatalf("kept stop %d:\n%s\n%s", code, stdout, stderr)
	}
}
