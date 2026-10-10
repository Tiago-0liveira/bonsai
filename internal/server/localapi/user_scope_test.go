package localapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newUserScopedTestServer builds the API the way `bonsai web` launches it:
// no launch repository, projects only from the configured roots.
func newUserScopedTestServer(t *testing.T) *Server {
	t.Helper()
	s, err := New(Config{
		ProjectRootsPath: t.TempDir() + "/project-roots.json",
		Address:          "127.0.0.1:7001",
		BrowserOrigin:    ProductionBrowserOrigin,
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func userScopedRequest(t *testing.T, h http.Handler, method, path, token string) *httptest.ResponseRecorder {
	t.Helper()
	body := ""
	if method == http.MethodPost || method == http.MethodPatch {
		body = "{}"
	}
	r := httptest.NewRequest(method, "http://127.0.0.1:7001"+path, strings.NewReader(body))
	r.Host = "127.0.0.1:7001"
	r.Header.Set("Origin", ProductionBrowserOrigin)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Idempotency-Key", "user-scope-test")
	if token != "" {
		r.Header.Set("X-Bonsai-Session", token)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestUserScopedAPIHasNoDefaultProject(t *testing.T) {
	s := newUserScopedTestServer(t)
	if s.hasDefaultProject() {
		t.Fatal("user-scoped API reports a default project")
	}
	if got := s.registry.Default(); got.daemon != nil || got.github != nil || got.info.ID != "" {
		t.Fatalf("Default() without a launch repository = %+v", got.info)
	}
	h := s.Handler()

	// The hosted app still connects: session creation and the preflight pass
	// the unchanged hosted-origin check.
	created := userScopedRequest(t, h, http.MethodPost, "/api/session", "")
	if created.Code != http.StatusCreated {
		t.Fatalf("session status = %d body=%s", created.Code, created.Body.String())
	}
	var session struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &session); err != nil || session.Token == "" {
		t.Fatalf("session body = %s (%v)", created.Body.String(), err)
	}

	if w := userScopedRequest(t, h, http.MethodGet, "/api/projects", session.Token); w.Code != http.StatusOK {
		t.Fatalf("project catalog status = %d body=%s", w.Code, w.Body.String())
	}

	legacy := []struct{ method, path string }{
		{http.MethodGet, "/api/repository"},
		{http.MethodPost, "/api/repository/fetch"},
		{http.MethodGet, "/api/branches"},
		{http.MethodGet, "/api/worktrees"},
		{http.MethodPost, "/api/worktrees"},
		{http.MethodGet, "/api/processes"},
		{http.MethodGet, "/api/processes/1/logs"},
		{http.MethodPost, "/api/processes/1/restart"},
		{http.MethodDelete, "/api/processes/1"},
		{http.MethodGet, "/api/github/repository"},
		{http.MethodGet, "/api/github/branches"},
		{http.MethodGet, "/api/github/pull-requests"},
		{http.MethodGet, "/api/github/pull-requests/1"},
		{http.MethodPost, "/api/github/pull-requests"},
		{http.MethodPost, "/api/github/pull-requests/1/comments"},
		{http.MethodPost, "/api/github/pull-requests/1/reviews"},
		{http.MethodPost, "/api/github/pull-requests/1/merge"},
		{http.MethodGet, "/api/github/checks/abc"},
		{http.MethodGet, "/api/github/workflows"},
	}
	for _, route := range legacy {
		w := userScopedRequest(t, h, route.method, route.path, session.Token)
		if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), `"no_default_project"`) {
			t.Fatalf("%s %s = %d %s; want 404 no_default_project", route.method, route.path, w.Code, w.Body.String())
		}
	}

	// Project-scoped routes keep their own error for unknown projects.
	w := userScopedRequest(t, h, http.MethodGet, "/api/projects/project-v1-missing/git", session.Token)
	if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), `"project_unavailable"`) {
		t.Fatalf("unknown project = %d %s", w.Code, w.Body.String())
	}

	// Settings no longer suggest the launch repository or its parent.
	settings := userScopedRequest(t, h, http.MethodGet, "/api/settings/project-roots", session.Token)
	if settings.Code != http.StatusOK {
		t.Fatalf("settings = %d %s", settings.Code, settings.Body.String())
	}
	if strings.Contains(settings.Body.String(), `"."`) {
		t.Fatalf("settings suggest the working directory: %s", settings.Body.String())
	}
}

func TestRepositoryScopedAPIKeepsLegacyRoutes(t *testing.T) {
	s := newTestServer(t)
	if !s.hasDefaultProject() {
		t.Fatal("repository-scoped API lost its default project")
	}
	h := s.Handler()
	created := userScopedRequest(t, h, http.MethodPost, "/api/session", "")
	var session struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(created.Body.Bytes(), &session)
	w := userScopedRequest(t, h, http.MethodGet, "/api/repository", session.Token)
	if strings.Contains(w.Body.String(), "no_default_project") {
		t.Fatalf("repository-scoped legacy route answered no_default_project: %s", w.Body.String())
	}
}
