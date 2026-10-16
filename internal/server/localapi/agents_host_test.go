//go:build linux || darwin

package localapi

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/Tiago-0liveira/bonsai/internal/core/agenthost"
	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/agentterminal"
	gitlocal "github.com/Tiago-0liveira/bonsai/internal/git/local"
	gitstore "github.com/Tiago-0liveira/bonsai/internal/storage/git"
)

// testAgentHostPaths keeps holder records in a temp dir and sockets under a
// short one: t.TempDir paths can pass the Unix socket path limit.
func testAgentHostPaths(t *testing.T) agenthost.Paths {
	t.Helper()
	// macOS's default temp directory can exceed the Unix socket path limit.
	sockets, err := os.MkdirTemp("/tmp", "bah")
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

// A read-only browser takes control: both sockets hear about it, the old
// writer's input is dropped without closing it, and the new writer types.
func TestAgentTerminalTakeControl(t *testing.T) {
	s, account, _ := terminalTestServer(t)
	httpServer := httptest.NewServer(s.Handler())
	defer httpServer.Close()
	s.expectedHost = strings.TrimPrefix(httpServer.URL, "http://")
	project := s.registry.Default()
	summary, err := s.agents.manager.Start("repo", gitlocal.ID(localRepositoryID, project.info.Path), agents.AccountID(account), "", project.info.Path, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { current, _ := s.agents.manager.Get("repo", summary.ID); return current.State == "running" })
	capability, _ := s.sessions.create()
	open := func() (*websocket.Conn, bool) {
		t.Helper()
		url := "ws" + strings.TrimPrefix(httpServer.URL, "http") + "/api/projects/repo/agents/" + summary.ID + "/terminal"
		conn, _, err := websocket.DefaultDialer.Dial(url, http.Header{"Origin": []string{"http://" + s.expectedHost}})
		if err != nil {
			t.Fatal(err)
		}
		_ = conn.WriteJSON(map[string]any{"type": "authenticate", "token": capability.Token})
		_ = conn.WriteJSON(map[string]any{"type": "attach", "offset": 0})
		var ready map[string]any
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		if err := conn.ReadJSON(&ready); err != nil || ready["type"] != "ready" {
			t.Fatalf("ready %v %v", ready, err)
		}
		return conn, ready["writer"] == true
	}
	// next reads frames until match accepts one, collecting output.
	next := func(conn *websocket.Conn, output *bytes.Buffer, match func(agentterminal.Frame) bool) {
		t.Helper()
		_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		for {
			var frame agentterminal.Frame
			if err := conn.ReadJSON(&frame); err != nil {
				t.Fatalf("read: %v (output %q)", err, output.String())
			}
			output.Write(frame.Data)
			if match(frame) {
				return
			}
		}
	}
	first, writes := open()
	defer first.Close()
	second, observes := open()
	defer second.Close()
	if !writes || observes {
		t.Fatal("first attachment should write, second observe")
	}
	var firstOut, secondOut bytes.Buffer
	_ = second.WriteJSON(map[string]any{"type": "take"})
	next(second, &secondOut, func(f agentterminal.Frame) bool { return f.Type == "writer" && *f.Writer })
	next(first, &firstOut, func(f agentterminal.Frame) bool { return f.Type == "writer" && !*f.Writer })
	_ = first.WriteJSON(map[string]any{"type": "input", "data": []byte("from-first\n")})
	_ = second.WriteJSON(map[string]any{"type": "input", "data": []byte("from-second\n")})
	next(first, &firstOut, func(agentterminal.Frame) bool { return strings.Contains(firstOut.String(), "REPLY:from-second") })
	if strings.Contains(firstOut.String(), "REPLY:from-first") {
		t.Fatal("input without control reached the agent")
	}
	// The first socket is still open and can take control back.
	_ = first.WriteJSON(map[string]any{"type": "take"})
	next(first, &firstOut, func(f agentterminal.Frame) bool { return f.Type == "writer" && *f.Writer })
}

// A session another client starts is listed, announced and journaled without
// any API request, and its external end shows in the list.
func TestWatchRecordsAdoptsSessionsStartedElsewhere(t *testing.T) {
	s, account, _ := terminalTestServer(t)
	runtime := s.agents.runtime
	paths := testAgentHostPaths(t)
	s.agents.manager.Close(false, 10*time.Second)
	s.agents.manager = testAgentManager(t, paths, runtime.Accounts, runtime.Sessions, runtime.Registry, s.publishAgents)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.agents.manager.WatchRecords(ctx)
	subscription, events := s.eventHub.subscribe()
	defer s.eventHub.unsubscribe(subscription)

	project := s.registry.Default()
	tui := agenthost.Starter{Accounts: runtime.Accounts, Sessions: runtime.Sessions, Registry: runtime.Registry, Paths: paths, Origin: "tui",
		Launch: agenthost.InProcess(agenthost.Deps{Accounts: runtime.Accounts, Sessions: runtime.Sessions, Registry: runtime.Registry, Linger: 200 * time.Millisecond})}
	record, err := tui.Start(context.Background(), agenthost.StartRequest{Project: "repo", Tree: gitlocal.ID(localRepositoryID, project.info.Path), Dir: project.info.Path, AccountID: agents.AccountID(account), Cols: 80, Rows: 24})
	if err != nil {
		t.Fatal(err)
	}

	listed := func(state string) func() bool {
		return func() bool {
			w := agentAPIRequest(t, s, "GET", "/api/projects/repo/agents", "", "")
			return strings.Contains(w.Body.String(), record.ID) && strings.Contains(w.Body.String(), `"state":"`+state+`"`)
		}
	}
	waitFor(t, listed("running"))
	for seen := false; !seen; {
		select {
		case event := <-events:
			seen = event.Type == "project_update" && event.ProjectID == "repo" && event.Component == "agents"
		case <-time.After(2 * time.Second):
			t.Fatal("no agents state-sync event")
		}
	}
	journal := func() agentMutation {
		var got agentMutation
		_ = project.state.View(func(data gitstore.Data) error {
			got, _ = gitstore.Get[agentMutation](data, "api_agent_mutations", "adopted-"+record.ID)
			return nil
		})
		return got
	}
	waitFor(t, func() bool { return journal().Summary.ID == record.ID })

	if err := agenthost.StopHolder(paths, record, ""); err != nil {
		t.Fatal(err)
	}
	waitFor(t, listed("exited"))
	waitFor(t, func() bool { return journal().Summary.State == "exited" })
}
