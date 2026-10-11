package localapi

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	domain "github.com/Tiago-0liveira/bonsai/internal/git"
	"github.com/gorilla/websocket"
)

func TestBootstrapOrderPutsTheActiveProjectFirst(t *testing.T) {
	projects := []ProjectInfo{{ID: "a"}, {ID: "b"}, {ID: "c"}}
	order := func(active string) string {
		var ids []string
		for _, project := range bootstrapOrder(projects, active) {
			ids = append(ids, project.ID)
		}
		return strings.Join(ids, ",")
	}
	for active, want := range map[string]string{"c": "c,a,b", "a": "a,b,c", "": "a,b,c", "unknown": "a,b,c"} {
		if got := order(active); got != want {
			t.Fatalf("active %q: order %s, want %s", active, got, want)
		}
	}
}

// The browser's open project must not wait behind large background projects
// during the WebSocket bootstrap.
func TestEventBootstrapSendsTheActiveProjectBeforeLargeBackgroundProjects(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	registry := &syncTestRegistry{entries: map[string]projectServices{}}
	for _, id := range []string{"a", "b", "c"} {
		root := t.TempDir()
		registry.entries[id] = projectServices{info: ProjectInfo{ID: id, Path: root, Available: true}, daemon: &syncTestDaemon{root: root}, github: &syncTestGitHub{}}
	}
	s, err := newServer(Config{Address: "127.0.0.1:7001"}, registry, filepath.Join(t.TempDir(), "project-roots.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.agents.manager.Close()
	// Background projects with thousands of worktrees each.
	for _, id := range []string{"a", "b"} {
		s.stateSync.commit(id, "local", func(snapshot *browserSnapshot) {
			local := &domain.RepositoryState{ID: id}
			for i := 0; i < 4000; i++ {
				local.Worktrees = append(local.Worktrees, domain.Worktree{ID: fmt.Sprintf("%s-%d", id, i), Branch: fmt.Sprintf("feature/%d", i), Path: fmt.Sprintf("/repo/%s/%d", id, i)})
			}
			now := time.Now().UTC()
			snapshot.Local = local
			snapshot.Freshness["local"] = browserFreshness{State: "ready", UpdatedAt: &now}
		})
	}
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	host := strings.TrimPrefix(ts.URL, "http://")
	s.expectedHost = host
	session, err := s.sessions.create()
	if err != nil {
		t.Fatal(err)
	}
	conn, _, err := websocket.DefaultDialer.Dial("ws://"+host+"/events", http.Header{"Origin": []string{"http://" + host}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	start := time.Now()
	if err := conn.WriteJSON(websocketAuth{Type: "authenticate", Token: session.Token, ActiveProject: "c"}); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	var order []string
	var activeAt, completeAt time.Duration
	for {
		var event localEvent
		if err := conn.ReadJSON(&event); err != nil {
			t.Fatal(err)
		}
		if event.Type == "project_snapshot" {
			order = append(order, event.ProjectID)
			if event.ProjectID == "c" {
				activeAt = time.Since(start)
			}
		}
		if event.Type == "bootstrap_complete" {
			completeAt = time.Since(start)
			break
		}
	}
	if strings.Join(order, ",") != "c,a,b" {
		t.Fatalf("bootstrap order = %v, want the active project first", order)
	}
	t.Logf("active project snapshot after %v; bootstrap complete after %v", activeAt, completeAt)
}
