//go:build linux || darwin

package localapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/agentruntime"
	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/agentterminal"
	gitlocal "github.com/Tiago-0liveira/bonsai/internal/git/local"
	gitstore "github.com/Tiago-0liveira/bonsai/internal/storage/git"
	"github.com/gorilla/websocket"
)

type terminalTestProvider struct {
	agents.Provider
	starts    atomic.Int32
	finalizes atomic.Int32
}

func (p *terminalTestProvider) ID() agents.ProviderID { return "antigravity" }
func (p *terminalTestProvider) PrepareSession(_ context.Context, r agents.PrepareSessionRequest) (agents.PreparedSession, error) {
	p.starts.Add(1)
	return agents.PreparedSession{Executable: "/bin/sh", Args: []string{"-c", `printf '\033[32mBONSAI_PTY_READY\033[0m\n'; while IFS= read -r line; do printf 'REPLY:%s\n' "$line"; done`}, Dir: r.Session.WorkDir, EnvSet: map[string]string{"HOME": r.Session.HomeDir}}, nil
}
func (p *terminalTestProvider) FinalizeSession(ctx context.Context, _ agents.FinalizeSessionRequest) error {
	p.finalizes.Add(1)
	return ctx.Err()
}
func terminalTestServer(t *testing.T) (*Server, string, *terminalTestProvider) {
	t.Helper()
	root := t.TempDir()
	accounts, err := agents.NewFileAccountStore(filepath.Join(t.TempDir(), "accounts"))
	if err != nil {
		t.Fatal(err)
	}
	id, _ := agents.NewAccountID()
	if err := accounts.Create(agents.Account{ID: id, Provider: "antigravity", Name: "Fixture"}); err != nil {
		t.Fatal(err)
	}
	sessions, err := agents.NewFileSessionStore(filepath.Join(t.TempDir(), "sessions"))
	if err != nil {
		t.Fatal(err)
	}
	registry := agents.NewRegistry()
	provider := &terminalTestProvider{}
	if err := registry.Register(provider); err != nil {
		t.Fatal(err)
	}
	metadata, err := gitstore.Open(filepath.Join(t.TempDir(), "metadata.json"))
	if err != nil {
		t.Fatal(err)
	}
	projects := &syncTestRegistry{entries: map[string]projectServices{"repo": {info: ProjectInfo{ID: "repo", Name: "PTY fixture", Path: root, Available: true, DefaultBranch: "main", FullName: "fixture/repo"}, state: metadata, daemon: &syncTestDaemon{root: root}, github: &syncTestGitHub{}}}}
	events := newEventHub()
	s := &Server{expectedHost: "127.0.0.1:7001", browserOrigin: ProductionBrowserOrigin, registry: projects, sessions: newSessionStore(), eventHub: events, rootsPath: filepath.Join(t.TempDir(), "roots.json")}
	s.stateSync = newStateSync(projects, events)
	s.stateSync.ReconcileCatalog()
	s.agents = &agentAPI{runtime: &agentruntime.Runtime{Accounts: accounts, Sessions: sessions, Registry: registry}}
	s.agents.manager = agentterminal.New(accounts, sessions, registry, s.publishAgents)
	s.publishAgents("repo")
	t.Cleanup(s.agents.manager.Close)
	return s, string(id), provider
}
func TestAgentAPIAndTerminal(t *testing.T) {
	s, account, provider := terminalTestServer(t)
	httpServer := httptest.NewServer(s.Handler())
	defer httpServer.Close()
	s.expectedHost = strings.TrimPrefix(httpServer.URL, "http://")
	capability, _ := s.sessions.create()
	tree := gitlocal.ID(localRepositoryID, s.registry.Default().info.Path)
	request := func(method, path, key, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, httpServer.URL+path, strings.NewReader(body))
		r.Host = s.expectedHost
		r.Header.Set("Origin", s.browserOrigin)
		r.Header.Set("X-Bonsai-Session", capability.Token)
		r.Header.Set("Idempotency-Key", key)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	body := fmt.Sprintf(`{"worktree_id":%q,"account_id":%q,"cols":80,"rows":24}`, tree, account)
	var wg sync.WaitGroup
	results := make(chan string, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := request("POST", "/api/projects/repo/agents", "same", body)
			if w.Code != 202 && w.Code != 200 {
				t.Errorf("start: %s", w.Body.String())
				return
			}
			var summary agentterminal.Summary
			_ = json.Unmarshal(w.Body.Bytes(), &summary)
			results <- summary.ID
		}()
	}
	wg.Wait()
	close(results)
	id := ""
	for result := range results {
		if id != "" && id != result {
			t.Fatal("duplicate session")
		}
		id = result
	}
	if w := request("POST", "/api/projects/repo/agents", "same", strings.Replace(body, "80", "81", 1)); w.Code != 409 {
		t.Fatal("changed payload accepted")
	}
	if w := request("GET", "/api/agents/accounts", "", ""); strings.Contains(w.Body.String(), "settings") || strings.Contains(w.Body.String(), "home") {
		t.Fatal("unsafe account summary")
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		a, _ := s.agents.manager.Get("repo", id)
		if a.State == "running" {
			break
		}
		time.Sleep(time.Millisecond)
	}
	url := "ws" + strings.TrimPrefix(httpServer.URL, "http") + "/api/projects/repo/agents/" + id + "/terminal"
	conn, _, err := websocket.DefaultDialer.Dial(url, http.Header{"Origin": []string{s.browserOrigin}})
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.WriteJSON(map[string]any{"type": "authenticate", "token": capability.Token})
	_ = conn.WriteJSON(map[string]any{"type": "attach", "offset": 0})
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	var ready map[string]any
	if err := conn.ReadJSON(&ready); err != nil || ready["writer"] != true {
		t.Fatalf("ready %v %v", ready, err)
	}
	_ = conn.WriteJSON(map[string]any{"type": "resize", "cols": 90, "rows": 30})
	_ = conn.WriteJSON(map[string]any{"type": "input", "data": []byte("hello\n")})
	var output bytes.Buffer
	for !strings.Contains(output.String(), "REPLY:hello") {
		var frame agentterminal.Frame
		if err = conn.ReadJSON(&frame); err != nil {
			t.Fatal(err)
		}
		output.Write(frame.Data)
	}
	conn.Close()
	if !s.hasLiveAgents("repo", tree) {
		t.Fatal("disconnect stopped process")
	}
	if provider.starts.Load() != 1 {
		t.Fatal("duplicate process")
	}
	snapshot, _ := s.stateSync.CachedSnapshot("repo")
	if len(snapshot.Agents) != 1 || snapshot.Agents[0].ID != id {
		t.Fatal("missing authoritative agent")
	}
	w := request("DELETE", "/api/projects/repo/agents/"+id, "stop", "")
	if w.Code != 202 {
		t.Fatal(w.Body.String())
	}
	s.agents.manager.Close()
	if provider.finalizes.Load() != 1 {
		t.Fatal("not finalized exactly once")
	}
	if w = request("DELETE", "/api/projects/repo/agents/"+id, "stop", ""); w.Code != 200 {
		t.Fatal("stop not idempotent")
	}
}
func TestTerminalRouteAuthenticationExceptionIsExact(t *testing.T) {
	for _, path := range []string{"/api/projects/p/agents/a/terminal/", "/api/projects/p/agents/a/terminal/extra", "/api/projects//agents/a/terminal", "/api/projects/p/agents/a"} {
		if isAgentTerminalRoute(path) {
			t.Fatalf("broad exception: %s", path)
		}
	}
	if !isAgentTerminalRoute("/api/projects/p/agents/a/terminal") {
		t.Fatal("precise route rejected")
	}
}

// Invoked only by the full-stack Playwright fixture; never uses personal profiles.
func TestAgentBrowserFixture(t *testing.T) {
	if os.Getenv("BONSAI_AGENT_BROWSER_FIXTURE") != "1" {
		t.Skip("browser fixture disabled")
	}
	s, _, _ := terminalTestServer(t)
	s.browserOrigin = "https://127.0.0.1:4173"
	if err := os.WriteFile(s.rootsPath, []byte(`{"version":1,"revision":1,"roots":[{"id":"fixture","path":"/tmp"}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "agy"), []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	listener, err := net.Listen("tcp", s.expectedHost)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	go s.stateSync.Run(ctx)
	server := &http.Server{Handler: s.Handler(), ReadHeaderTimeout: time.Second}
	go func() { <-ctx.Done(); _ = server.Close() }()
	if err = server.Serve(listener); err != http.ErrServerClosed {
		t.Fatal(err)
	}
}

func TestAgentCrashRecoveryDoesNotRelaunch(t *testing.T) {
	s, account, provider := terminalTestServer(t)
	project := s.registry.Default()
	summary, err := s.agents.manager.Start("repo", gitlocal.ID(localRepositoryID, project.info.Path), agents.AccountID(account), "", project.info.Path, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	if err := project.state.Update(func(data gitstore.Data) error {
		return gitstore.Put(data, "api_agent_mutations", "accepted", agentMutation{Fingerprint: "request", Summary: summary})
	}); err != nil {
		t.Fatal(err)
	}
	s.agents.manager.Close()
	// Simulate a crash leaving the accepted record active on disk.
	if err := project.state.Update(func(data gitstore.Data) error {
		return gitstore.Put(data, "api_agent_mutations", "accepted", agentMutation{Fingerprint: "request", Summary: summary})
	}); err != nil {
		t.Fatal(err)
	}
	starts := provider.starts.Load()
	s.agents.manager = agentterminal.New(s.agents.runtime.Accounts, s.agents.runtime.Sessions, s.agents.runtime.Registry, s.publishAgents)
	defer s.agents.manager.Close()
	if err := s.recoverAgents(); err != nil {
		t.Fatal(err)
	}
	restored, ok := s.agents.manager.Get("repo", summary.ID)
	if !ok || restored.State != "failed" || !strings.Contains(restored.Error, "restart") {
		t.Fatalf("bad recovery: %+v", restored)
	}
	if provider.starts.Load() != starts {
		t.Fatal("recovery launched a replacement")
	}
	snapshot, _ := s.stateSync.CachedSnapshot("repo")
	if len(snapshot.Agents) != 1 || snapshot.Agents[0].State != "failed" {
		t.Fatal("recovery missing from snapshot")
	}
}

func TestAgentOwnershipAndRemovalConflicts(t *testing.T) {
	s, account, _ := terminalTestServer(t)
	project := s.registry.Default()
	tree := gitlocal.ID(localRepositoryID, project.info.Path)
	capability, _ := s.sessions.create()
	request := func(method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://"+s.expectedHost+path, strings.NewReader(body))
		r.Header.Set("Origin", s.browserOrigin)
		r.Header.Set("X-Bonsai-Session", capability.Token)
		r.Header.Set("Idempotency-Key", path)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	for _, body := range []string{
		fmt.Sprintf(`{"worktree_id":"not-owned","account_id":%q,"cols":80,"rows":24}`, account),
		fmt.Sprintf(`{"worktree_id":%q,"account_id":%q,"cols":0,"rows":24}`, tree, account),
		fmt.Sprintf(`{"worktree_id":%q,"account_id":%q,"cols":80,"rows":24,"executable":"sh"}`, tree, account),
	} {
		if w := request("POST", "/api/projects/repo/agents", body); w.Code < 400 {
			t.Fatalf("accepted invalid body: %s", body)
		}
	}
	summary, err := s.agents.manager.Start("repo", tree, agents.AccountID(account), "", project.info.Path, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.agents.manager.Get("different-project", summary.ID); ok {
		t.Fatal("cross-project read")
	}
	if _, _, _, _, err := s.agents.manager.Attach("different-project", summary.ID, 0); err == nil {
		t.Fatal("cross-project attach")
	}
	scoped := *s
	scoped.registry = &scopedProjectRegistry{project: project}
	scoped.state = project.state
	w := httptest.NewRecorder()
	r := httptest.NewRequest("DELETE", "http://"+s.expectedHost+"/api/worktrees/"+tree, nil)
	r.Header.Set("Idempotency-Key", "remove")
	_, ok := scoped.runGit(w, r, "git.worktree.remove", tree, nil, true)
	if ok || w.Code != 409 {
		t.Fatalf("worktree removal status %d", w.Code)
	}
	w = request("DELETE", "/api/settings/project-roots/missing", `{"revision":0}`)
	if w.Code != 409 {
		t.Fatalf("root removal status %d", w.Code)
	}
}

func TestTerminalRejectsUnauthenticatedFirstFrame(t *testing.T) {
	s, account, _ := terminalTestServer(t)
	project := s.registry.Default()
	session, err := s.agents.manager.Start("repo", gitlocal.ID(localRepositoryID, project.info.Path), agents.AccountID(account), "", project.info.Path, 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(s.Handler())
	defer server.Close()
	s.expectedHost = strings.TrimPrefix(server.URL, "http://")
	url := "ws" + strings.TrimPrefix(server.URL, "http") + "/api/projects/repo/agents/" + session.ID + "/terminal"
	for _, first := range []map[string]any{{"type": "authenticate", "token": "invalid"}, {"type": "input", "data": []byte("bad\n")}} {
		conn, _, err := websocket.DefaultDialer.Dial(url, http.Header{"Origin": []string{s.browserOrigin}})
		if err != nil {
			t.Fatal(err)
		}
		_ = conn.SetReadDeadline(time.Now().Add(time.Second))
		_ = conn.WriteJSON(first)
		if _, _, err := conn.ReadMessage(); err == nil {
			t.Fatal("unauthenticated subscriber received a frame")
		}
		conn.Close()
	}
}
