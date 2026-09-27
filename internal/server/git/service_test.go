package git

import (
	"context"
	"encoding/json"
	bridge "github.com/Tiago-0liveira/bonsai/internal/daemon/gitbridge"
	domain "github.com/Tiago-0liveira/bonsai/internal/git"
	"github.com/Tiago-0liveira/bonsai/internal/git/local"
	"github.com/Tiago-0liveira/bonsai/internal/server/githubapp"
	store "github.com/Tiago-0liveira/bonsai/internal/storage/git"
	"github.com/gorilla/websocket"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func setupAPI(t *testing.T) (*Service, *http.ServeMux) {
	t.Helper()
	db, e := store.Open(filepath.Join(t.TempDir(), "db.json"))
	if e != nil {
		t.Fatal(e)
	}
	auth := &githubapp.Manager{Store: db}
	s, e := New(db, auth, nil, []Repository{{ID: "repo", WorkspaceID: "workspace", Members: map[string]string{"writer": "write", "reader": "read"}}, {ID: "other", WorkspaceID: "other", Members: map[string]string{"outsider": "write"}}}, "https://bonsai.test")
	if e != nil {
		t.Fatal(e)
	}
	db.Update(func(d store.Data) error {
		for _, user := range []string{"writer", "reader", "outsider"} {
			store.Put(d, "sessions", githubapp.Hash(user), githubapp.Session{UserID: user, ExpiresAt: time.Now().Add(time.Hour)})
		}
		return nil
	})
	t.Cleanup(s.Close)
	mux := http.NewServeMux()
	s.Register(mux)
	return s, mux
}
func apiRequest(t *testing.T, mux *http.ServeMux, method, path, user, origin, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	if user != "" {
		r.AddCookie(&http.Cookie{Name: "bonsai_session", Value: user})
	}
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	return w
}
func TestAuthorizationAndOffline(t *testing.T) {
	s, mux := setupAPI(t)
	tests := []struct {
		method, path, user, origin, body string
		status                           int
	}{{"GET", "/api/projects/repo/git", "", "", "", 401}, {"GET", "/api/projects/repo/git", "outsider", "", "", 403}, {"GET", "/api/projects/repo/git", "reader", "", "", 200}, {"POST", "/api/projects/repo/worktrees", "reader", s.Origin, `{}`, 403}, {"POST", "/api/projects/repo/worktrees", "writer", "https://evil.test", `{}`, 403}, {"POST", "/api/projects/repo/worktrees", "writer", s.Origin, `{}`, 503}, {"POST", "/api/devices", "writer", s.Origin, `{"repository_ids":["other"]}`, 403}}
	for _, tt := range tests {
		w := apiRequest(t, mux, tt.method, tt.path, tt.user, tt.origin, tt.body)
		if w.Code != tt.status {
			t.Errorf("%s %s as %s: %d %s", tt.method, tt.path, tt.user, w.Code, w.Body)
		}
	}
}
func TestDeviceBridgeRoutesOnlyGitAndReconciles(t *testing.T) {
	s, mux := setupAPI(t)
	enrolled := apiRequest(t, mux, "POST", "/api/devices", "writer", s.Origin, `{"repository_ids":["repo"]}`)
	if enrolled.Code != 201 {
		t.Fatal(enrolled.Body)
	}
	var enrollment struct{ Credential string }
	json.Unmarshal(enrolled.Body.Bytes(), &enrollment)
	server := httptest.NewServer(mux)
	defer server.Close()
	ws, _, e := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/api/daemon/connect", http.Header{"Authorization": []string{"Bearer " + enrollment.Credential}})
	if e != nil {
		t.Fatal(e)
	}
	defer ws.Close()
	dir := t.TempDir()
	for _, args := range [][]string{{"init", "-b", "main"}, {"config", "user.name", "Test"}, {"config", "user.email", "test@example.com"}, {"commit", "--allow-empty", "-m", "initial"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if b, e := cmd.CombinedOutput(); e != nil {
			t.Fatalf("%v %s", e, b)
		}
	}
	svc, e := local.New([]local.Config{{ID: "repo", Root: dir, WorktreeRoot: t.TempDir()}})
	if e != nil {
		t.Fatal(e)
	}
	journal, _ := store.Open(filepath.Join(t.TempDir(), "journal.json"))
	executor := bridge.Executor{Local: svc, Journal: journal}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ws.WriteJSON(bridge.Frame{Type: "hello", Repositories: []string{"repo"}})
	snapshot, e := svc.Repository(ctx, "repo")
	if e != nil {
		t.Fatal(e)
	}
	ws.WriteJSON(bridge.Frame{Type: "snapshot", RepositoryID: "repo", Snapshot: &snapshot})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			var f bridge.Frame
			if ws.ReadJSON(&f) != nil {
				return
			}
			if f.Command != nil {
				result := executor.Execute(ctx, *f.Command)
				if ws.WriteJSON(bridge.Frame{Type: "result", Result: &result}) != nil {
					return
				}
			}
		}
	}()
	for !s.snapshot("repo").Online {
		select {
		case <-ctx.Done():
			t.Fatal("daemon not connected")
		case <-time.After(time.Millisecond):
		}
	}
	c := bridge.Command{ID: "branch", UserID: "writer", RepositoryID: "repo", Type: "git.worktree.create", Arguments: json.RawMessage(`{"mode":"new","branch":"test","base":"main"}`)}
	result, e := s.execute(ctx, c)
	if e != nil || result.Error != nil {
		t.Fatal(result, e)
	}
	again, e := s.execute(ctx, c)
	if e != nil || string(result.Payload) != string(again.Payload) {
		t.Fatal("server dedupe", again, e)
	}
	c.ID = "unauthorized"
	c.UserID = "outsider"
	if _, e = s.execute(ctx, c); e != domain.ErrForbidden {
		t.Fatal(e)
	}
	ws.Close()
	<-done
}
