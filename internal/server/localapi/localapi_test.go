package localapi

import (
	"bytes"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	s, err := New(Config{
		RepoDir:          t.TempDir(),
		ProjectRootsPath: t.TempDir() + "/project-roots.json",
		Address:          "127.0.0.1:7001",
		BrowserOrigin:    ProductionBrowserOrigin,
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestRequireLoopback(t *testing.T) {
	for _, address := range []string{"127.0.0.1:7001", "[::1]:7001"} {
		if err := requireLoopback(address); err != nil {
			t.Fatalf("requireLoopback(%q): %v", address, err)
		}
	}
	for _, address := range []string{"0.0.0.0:7001", "192.168.1.20:7001", "localhost:7001"} {
		if err := requireLoopback(address); err == nil {
			t.Fatalf("requireLoopback(%q) unexpectedly succeeded", address)
		}
	}
}

func TestValidateBrowserOrigin(t *testing.T) {
	if err := validateBrowserOrigin(ProductionBrowserOrigin, BrowserSecurityProduction); err != nil {
		t.Fatal(err)
	}
	if err := validateBrowserOrigin("https://app.bonsai.tiagoliv.com", BrowserSecurityProduction); err != nil {
		t.Fatalf("custom production origin rejected: %v", err)
	}
	// Empty means the hosted interface is disabled: only the API's own origin.
	if err := validateBrowserOrigin("", BrowserSecurityProduction); err != nil {
		t.Fatalf("production rejected the disabled hosted origin: %v", err)
	}
	if err := validateBrowserOrigin("", BrowserSecurityDevelopment); err == nil {
		t.Fatal("development accepted an empty origin")
	}
	for _, origin := range []string{"null", "http://app.example.com", "https://app.example.com/path", "https://user@app.example.com"} {
		if err := validateBrowserOrigin(origin, BrowserSecurityProduction); err == nil {
			t.Fatalf("production accepted invalid origin %q", origin)
		}
	}
	for _, origin := range []string{"http://localhost:7003", "http://127.0.0.1:7003"} {
		if err := validateBrowserOrigin(origin, BrowserSecurityDevelopment); err != nil {
			t.Fatalf("development origin %q rejected: %v", origin, err)
		}
		if err := validateBrowserOrigin(origin, BrowserSecurityProduction); err == nil {
			t.Fatalf("production accepted development origin %q", origin)
		}
	}
	for _, origin := range []string{"https://evil.example", "https://fake.app.bonsai.dev", "null", "http://0.0.0.0:7003", "http://localhost"} {
		if err := validateBrowserOrigin(origin, BrowserSecurityDevelopment); err == nil {
			t.Fatalf("development accepted %q", origin)
		}
	}
	if err := validateBrowserOrigin(ProductionBrowserOrigin, BrowserSecurityDevelopment); err == nil {
		t.Fatal("development mode accepted hosted production origin")
	}
}

func TestSecurityHostOriginCORSAndSession(t *testing.T) {
	s := newTestServer(t)
	handler := s.Handler()

	request := func(method, path, host, origin, token string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "http://127.0.0.1:7001"+path, nil)
		r.Host = host
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		if token != "" {
			r.Header.Set("X-Bonsai-Session", token)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}

	if got := request(http.MethodGet, "/api/projects", "evil.example:7001", ProductionBrowserOrigin, "").Code; got != http.StatusForbidden {
		t.Fatalf("wrong Host status = %d", got)
	}
	for _, origin := range []string{"https://evil.example", "https://fake.app.bonsai.dev", "null"} {
		if got := request(http.MethodGet, "/api/projects", "127.0.0.1:7001", origin, "").Code; got != http.StatusForbidden {
			t.Fatalf("origin %q status = %d", origin, got)
		}
	}
	if got := request(http.MethodGet, "/api/protected-missing", "127.0.0.1:7001", ProductionBrowserOrigin, "").Code; got != http.StatusUnauthorized {
		t.Fatalf("missing session status = %d", got)
	}
	if got := request(http.MethodGet, "/api/protected-missing", "127.0.0.1:7001", ProductionBrowserOrigin, "invalid").Code; got != http.StatusUnauthorized {
		t.Fatalf("invalid session status = %d", got)
	}

	session, err := s.sessions.create()
	if err != nil {
		t.Fatal(err)
	}
	authorized := request(http.MethodGet, "/api/protected-missing", "127.0.0.1:7001", ProductionBrowserOrigin, session.Token)
	if authorized.Code != http.StatusNotFound {
		t.Fatalf("valid session status = %d", authorized.Code)
	}
	if got := authorized.Header().Get("Access-Control-Allow-Origin"); got != ProductionBrowserOrigin {
		t.Fatalf("allow origin = %q", got)
	}
	if got := authorized.Header().Get("Access-Control-Allow-Origin"); got == "*" {
		t.Fatal("wildcard CORS returned")
	}
}

func TestSessionCreationAndRestartInvalidation(t *testing.T) {
	s := newTestServer(t)
	var logs bytes.Buffer
	old := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(old)

	r := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:7001/api/session", nil)
	r.Host = "127.0.0.1:7001"
	r.Header.Set("Origin", ProductionBrowserOrigin)
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("session status = %d body=%s", w.Code, w.Body.String())
	}
	var token string
	for _, part := range strings.Fields(w.Body.String()) {
		if strings.Contains(part, "token") {
			token = w.Body.String()
			break
		}
	}
	if token == "" {
		t.Fatal("session response did not include token")
	}

	session, err := s.sessions.create()
	if err != nil {
		t.Fatal(err)
	}
	if !s.sessions.valid(session.Token) {
		t.Fatal("new session is not valid")
	}
	restarted := newSessionStore()
	if restarted.valid(session.Token) {
		t.Fatal("session survived server restart")
	}
	if strings.Contains(logs.String(), session.Token) {
		t.Fatal("raw session token appeared in logs")
	}
}

func TestExpiredSessionRejected(t *testing.T) {
	store := newSessionStore()
	now := time.Now().UTC()
	store.now = func() time.Time { return now }
	session, err := store.create()
	if err != nil {
		t.Fatal(err)
	}
	store.now = func() time.Time { return now.Add(sessionTTL + time.Second) }
	if store.valid(session.Token) {
		t.Fatal("expired session remained valid")
	}
}

func TestPreflightIsStrict(t *testing.T) {
	s := newTestServer(t)
	r := httptest.NewRequest(http.MethodOptions, "http://127.0.0.1:7001/api/projects", nil)
	r.Host = "127.0.0.1:7001"
	r.Header.Set("Origin", ProductionBrowserOrigin)
	r.Header.Set("Access-Control-Request-Method", "GET")
	r.Header.Set("Access-Control-Request-Headers", "X-Bonsai-Session")
	r.Header.Set("Access-Control-Request-Private-Network", "true")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusNoContent {
		t.Fatalf("preflight status = %d body=%s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != ProductionBrowserOrigin {
		t.Fatalf("allow origin = %q", got)
	}
	if got := w.Header().Get("Access-Control-Allow-Private-Network"); got != "true" {
		t.Fatalf("private network = %q", got)
	}
	if strings.Contains(w.Header().Get("Access-Control-Allow-Headers"), "Authorization") {
		t.Fatal("Authorization unexpectedly allowed")
	}
}

func TestRequestArgumentsRejectsUnknownExtraAndOversized(t *testing.T) {
	check := func(body io.Reader, allowed ...string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:7001/api/worktrees", body)
		w := httptest.NewRecorder()
		_, _ = requestArguments(w, r, allowed...)
		return w
	}

	if got := check(strings.NewReader(`{"branch":"x","surprise":true}`), "branch").Code; got != http.StatusBadRequest {
		t.Fatalf("unknown field status = %d", got)
	}
	if got := check(strings.NewReader(`{"branch":"x"} {"branch":"y"}`), "branch").Code; got != http.StatusBadRequest {
		t.Fatalf("extra value status = %d", got)
	}
	oversized := strings.NewReader(`{"message":"` + strings.Repeat("x", maxRequestBody) + `"}`)
	if got := check(oversized, "message").Code; got != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized body status = %d", got)
	}
}

func TestWebSocketAuthentication(t *testing.T) {
	s := newTestServer(t)
	oldTimeout := websocketAuthTimeout
	websocketAuthTimeout = 100 * time.Millisecond
	defer func() { websocketAuthTimeout = oldTimeout }()

	ts := httptest.NewServer(s.Handler())
	defer ts.Close()
	host := strings.TrimPrefix(ts.URL, "http://")
	s.expectedHost = host
	wsURL := "ws://" + host + "/events"

	dial := func(url, origin string, host ...string) (*websocket.Conn, *http.Response, error) {
		header := http.Header{}
		header.Set("Origin", origin)
		if len(host) > 0 {
			header.Set("Host", host[0])
		}
		return websocket.DefaultDialer.Dial(url, header)
	}

	if conn, resp, err := dial(wsURL, "https://evil.example"); err == nil {
		conn.Close()
		t.Fatal("wrong Origin websocket unexpectedly connected")
	} else if resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("wrong Origin response = %#v err=%v", resp, err)
	}

	_, port, _ := strings.Cut(host, ":")
	if conn, _, err := dial(wsURL, ProductionBrowserOrigin, "evil.example:"+port); err == nil {
		conn.Close()
		t.Fatal("wrong Host websocket unexpectedly connected")
	}
	// The embedded UI's own origins, on either loopback name, may connect.
	for _, self := range []string{"http://" + host, "http://localhost:" + port} {
		selfHost := strings.TrimPrefix(self, "http://")
		conn, _, err := dial(wsURL, self, selfHost)
		if err != nil {
			t.Fatalf("own origin %s websocket rejected: %v", self, err)
		}
		conn.Close()
	}
	if conn, _, err := dial(wsURL, "http://localhost:1", "localhost:"+port); err == nil {
		conn.Close()
		t.Fatal("localhost origin on another port unexpectedly connected")
	}

	conn, _, err := dial(wsURL, ProductionBrowserOrigin)
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	if _, _, err := conn.ReadMessage(); err == nil {
		t.Fatal("unauthenticated websocket remained open")
	}
	conn.Close()

	conn, _, err = dial(wsURL, ProductionBrowserOrigin)
	if err != nil {
		t.Fatal(err)
	}
	if err := conn.WriteJSON(websocketAuth{Type: "authenticate", Token: "bad"}); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	if _, _, err := conn.ReadMessage(); err == nil {
		t.Fatal("bad-token websocket remained open")
	}
	conn.Close()

	session, err := s.sessions.create()
	if err != nil {
		t.Fatal(err)
	}
	conn, _, err = dial(wsURL, ProductionBrowserOrigin)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.WriteJSON(websocketAuth{Type: "authenticate", Token: session.Token}); err != nil {
		t.Fatal(err)
	}
	var ready map[string]any
	if err := conn.ReadJSON(&ready); err != nil {
		t.Fatal(err)
	}
	if ready["type"] != "ready" || ready["epoch"] != s.stateSync.epoch || ready["epoch"] == "" {
		t.Fatalf("first event = %#v", ready)
	}
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	for {
		var event localEvent
		if err := conn.ReadJSON(&event); err != nil {
			t.Fatal(err)
		}
		if event.Type == "bootstrap_complete" {
			break
		}
	}
	// Updates must continue on the same socket after the initial catalog.
	s.eventHub.publish(localEvent{Type: "project_update", ProjectID: "live", Epoch: s.stateSync.epoch, Sequence: 42})
	for {
		var event localEvent
		if err := conn.ReadJSON(&event); err != nil {
			t.Fatal(err)
		}
		if event.ProjectID == "live" {
			if event.Type != "project_update" || event.Sequence != 42 {
				t.Fatalf("unexpected live update: %+v", event)
			}
			break
		}
	}
}

func TestDevelopmentModeStillRequiresCapabilityAndExactOrigin(t *testing.T) {
	const devOrigin = "http://127.0.0.1:7003"
	s, err := New(Config{
		RepoDir:       t.TempDir(),
		Address:       "127.0.0.1:7001",
		BrowserOrigin: devOrigin,
		SecurityMode:  BrowserSecurityDevelopment,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := func(origin, token string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:7001/api/protected-missing", nil)
		r.Host = "127.0.0.1:7001"
		r.Header.Set("Origin", origin)
		if token != "" {
			r.Header.Set("X-Bonsai-Session", token)
		}
		w := httptest.NewRecorder()
		s.Handler().ServeHTTP(w, r)
		return w
	}
	if got := request(ProductionBrowserOrigin, "").Code; got != http.StatusForbidden {
		t.Fatalf("production origin in development mode status = %d", got)
	}
	if got := request(devOrigin, "").Code; got != http.StatusUnauthorized {
		t.Fatalf("missing development capability status = %d", got)
	}
	session, err := s.sessions.create()
	if err != nil {
		t.Fatal(err)
	}
	authorized := request(devOrigin, session.Token)
	if authorized.Code != http.StatusNotFound {
		t.Fatalf("valid development capability status = %d", authorized.Code)
	}
	if got := authorized.Header().Get("Access-Control-Allow-Origin"); got != devOrigin {
		t.Fatalf("development allow origin = %q", got)
	}
}
