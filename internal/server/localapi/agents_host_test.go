package localapi

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agenthost"
	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	gitlocal "github.com/Tiago-0liveira/bonsai/internal/git/local"
	gitstore "github.com/Tiago-0liveira/bonsai/internal/storage/git"
)

// testAgentHostPaths keeps holder records in a temp dir and sockets under a
// short one: t.TempDir paths can pass the Unix socket path limit.
func testAgentHostPaths(t *testing.T) agenthost.Paths {
	t.Helper()
	sockets, err := os.MkdirTemp("", "bah")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(sockets) })
	return agenthost.Paths{Records: filepath.Join(t.TempDir(), "agents"), Sockets: sockets}
}

// testAgentManager serves holders in the test process, whose providers exist
// only here, and stops them when the test ends.
func testAgentManager(t *testing.T, paths agenthost.Paths, accounts agents.AccountStore, sessions agents.SessionStore, registry *agents.Registry, changed func(string)) *agenthost.Manager {
	t.Helper()
	m := agenthost.NewManager(accounts, sessions, registry, paths, agenthost.InProcess(agenthost.Deps{Accounts: accounts, Sessions: sessions, Registry: registry, Linger: 200 * time.Millisecond}), changed)
	t.Cleanup(func() { m.Close(false, 10*time.Second) })
	return m
}

// An API restart adopts the sessions whose holders still run: the journal
// keeps them active and the next API can stop them.
func TestRecoveryAdoptsRunningHolders(t *testing.T) {
	s, account, provider := terminalTestServer(t)
	paths := testAgentHostPaths(t)
	runtime := s.agents.runtime
	s.agents.manager.Close(false, 10*time.Second)
	s.agents.manager = testAgentManager(t, paths, runtime.Accounts, runtime.Sessions, runtime.Registry, s.publishAgents)
	project := s.registry.Default()
	summary, err := s.agents.manager.Start("repo", gitlocal.ID(localRepositoryID, project.info.Path), agents.AccountID(account), "kept", project.info.Path, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { current, _ := s.agents.manager.Get("repo", summary.ID); return current.State == "running" })
	if err := project.state.Update(func(data gitstore.Data) error {
		return gitstore.Put(data, "api_agent_mutations", "kept", agentMutation{Fingerprint: "request", Summary: summary})
	}); err != nil {
		t.Fatal(err)
	}
	// The API shuts down; its holder keeps the session.
	s.Close()
	starts := provider.starts.Load()

	s.agents.manager = testAgentManager(t, paths, runtime.Accounts, runtime.Sessions, runtime.Registry, s.publishAgents)
	if err := s.recoverAgents(); err != nil {
		t.Fatal(err)
	}
	adopted, ok := s.agents.manager.Get("repo", summary.ID)
	if !ok || adopted.State != "running" || adopted.Name != "kept" {
		t.Fatalf("adopted %+v", adopted)
	}
	if provider.starts.Load() != starts {
		t.Fatal("recovery launched a replacement")
	}
	_ = project.state.View(func(data gitstore.Data) error {
		if record, _ := gitstore.Get[agentMutation](data, "api_agent_mutations", "kept"); record.Summary.State != "running" {
			t.Errorf("journal %+v", record.Summary)
		}
		return nil
	})
	snapshot, _ := s.stateSync.CachedSnapshot("repo")
	if len(snapshot.Agents) != 1 || snapshot.Agents[0].State != "running" {
		t.Fatalf("snapshot %+v", snapshot.Agents)
	}
	if err := s.agents.manager.Stop("repo", summary.ID); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { current, _ := s.agents.manager.Get("repo", summary.ID); return current.State == "exited" })
	_ = project.state.View(func(data gitstore.Data) error {
		if record, _ := gitstore.Get[agentMutation](data, "api_agent_mutations", "kept"); record.Summary.State != "exited" {
			t.Errorf("journal after stop %+v", record.Summary)
		}
		return nil
	})
}
