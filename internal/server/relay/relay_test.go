package relay

import (
	"bufio"
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"go/parser"
	"go/token"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/server/webhooks"
)

const testSessionSecret = "test-relay-session-secret"

func newTestServer(t *testing.T) (*Server, Config) {
	t.Helper()
	cfg := Config{
		Database:           filepath.Join(t.TempDir(), "relay.json"),
		ExternalURL:        ProductionExternalURL,
		FrontendOrigin:     ProductionFrontendOrigin,
		GitHubClientID:     "client",
		GitHubClientSecret: "secret",
		WebhookSecret:      "webhook-secret",
	}
	s, err := NewServer(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return s, cfg
}

func putTestSession(t *testing.T, s *Server, secret string, user int64, grants ...RepositoryGrant) RelaySession {
	t.Helper()
	now := time.Now().UTC()
	session := RelaySession{GitHubUserID: user, Repositories: grants, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
	if err := s.store.PutSession(SecretHash(secret), session); err != nil {
		t.Fatal(err)
	}
	return session
}

func githubSignature(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func webhookRequest(t *testing.T, h http.Handler, method, delivery, eventName string, body []byte, sign bool) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, "/webhooks/github", bytes.NewReader(body))
	if sign {
		req.Header.Set("X-Hub-Signature-256", githubSignature("webhook-secret", body))
	}
	if delivery != "" {
		req.Header.Set("X-GitHub-Delivery", delivery)
	}
	if eventName != "" {
		req.Header.Set("X-GitHub-Event", eventName)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func TestWebhookVerificationDedupeAndRestart(t *testing.T) {
	s, cfg := newTestServer(t)
	putTestSession(t, s, testSessionSecret, 1, RepositoryGrant{RepositoryID: 123, InstallationID: 456})
	h := s.Handler()
	body := []byte(`{"action":"synchronize","repository":{"id":123},"installation":{"id":456},"number":42,"pull_request":{"number":42}}`)

	if got := webhookRequest(t, h, http.MethodGet, "d1", "pull_request", body, false).Code; got != http.StatusMethodNotAllowed {
		t.Fatalf("GET webhook = %d", got)
	}
	if got := webhookRequest(t, h, http.MethodPost, "d1", "pull_request", body, false).Code; got != http.StatusUnauthorized {
		t.Fatalf("missing HMAC = %d", got)
	}
	bad := httptest.NewRequest(http.MethodPost, "/webhooks/github", bytes.NewReader(body))
	bad.Header.Set("X-Hub-Signature-256", "sha256=bad")
	bad.Header.Set("X-GitHub-Delivery", "d1")
	bad.Header.Set("X-GitHub-Event", "pull_request")
	badW := httptest.NewRecorder()
	h.ServeHTTP(badW, bad)
	if badW.Code != http.StatusUnauthorized {
		t.Fatalf("invalid HMAC = %d", badW.Code)
	}
	oversized := bytes.Repeat([]byte("x"), maxWebhookBody+1)
	if got := webhookRequest(t, h, http.MethodPost, "big", "push", oversized, false).Code; got != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized = %d", got)
	}
	invalid := []byte("{")
	if got := webhookRequest(t, h, http.MethodPost, "invalid", "push", invalid, true).Code; got != http.StatusBadRequest {
		t.Fatalf("invalid JSON = %d", got)
	}
	if got := webhookRequest(t, h, http.MethodPost, "", "pull_request", body, true).Code; got != http.StatusBadRequest {
		t.Fatalf("missing delivery = %d", got)
	}
	unsupported := []byte(`{"action":"not_real","repository":{"id":123},"installation":{"id":456}}`)
	if got := webhookRequest(t, h, http.MethodPost, "unsupported", "pull_request", unsupported, true).Code; got != http.StatusAccepted {
		t.Fatalf("unsupported action = %d", got)
	}
	unknown := []byte(`{"action":"synchronize","repository":{"id":999},"installation":{"id":456}}`)
	if got := webhookRequest(t, h, http.MethodPost, "unknown", "pull_request", unknown, true).Code; got != http.StatusForbidden {
		t.Fatalf("unknown repository = %d", got)
	}
	if got := webhookRequest(t, h, http.MethodPost, "d1", "pull_request", body, true).Code; got != http.StatusAccepted {
		t.Fatalf("valid webhook = %d", got)
	}
	if got := webhookRequest(t, h, http.MethodPost, "d1", "pull_request", body, true).Code; got != http.StatusOK {
		t.Fatalf("duplicate webhook = %d", got)
	}
	changed := []byte(`{"action":"opened","repository":{"id":123},"installation":{"id":456},"number":42}`)
	if got := webhookRequest(t, h, http.MethodPost, "d1", "pull_request", changed, true).Code; got != http.StatusConflict {
		t.Fatalf("delivery conflict = %d", got)
	}
	if s.store.EventCount() != 1 {
		t.Fatalf("event count = %d", s.store.EventCount())
	}

	restarted, err := NewServer(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got := webhookRequest(t, restarted.Handler(), http.MethodPost, "d1", "pull_request", body, true).Code; got != http.StatusOK {
		t.Fatalf("dedupe after restart = %d", got)
	}
	if restarted.store.EventCount() != 1 {
		t.Fatalf("restart event count = %d", restarted.store.EventCount())
	}
}

func TestAuthorizationIsolationAndLogout(t *testing.T) {
	s, _ := newTestServer(t)
	sessionA := putTestSession(t, s, "session-a", 1, RepositoryGrant{RepositoryID: 123, InstallationID: 456})
	putTestSession(t, s, "session-b", 2, RepositoryGrant{RepositoryID: 999, InstallationID: 777})
	eventB := webhooks.Event{DeliveryID: "b", Event: "pull_request", Action: "opened", RepositoryID: 999, InstallationID: 777}
	if _, _, err := s.store.RecordWebhook("b", "digest-b", eventB, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if events, _ := s.store.Replay(0, sessionA); len(events) != 0 {
		t.Fatalf("session A received repository B: %+v", events)
	}

	unauth := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/events", nil)
	req.Header.Set("Origin", ProductionFrontendOrigin)
	s.Handler().ServeHTTP(unauth, req)
	if unauth.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated SSE = %d", unauth.Code)
	}

	logout := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	logout.Header.Set("Origin", ProductionFrontendOrigin)
	logout.AddCookie(&http.Cookie{Name: relayCookieName, Value: "session-a"})
	logoutW := httptest.NewRecorder()
	s.Handler().ServeHTTP(logoutW, logout)
	if logoutW.Code != http.StatusNoContent {
		t.Fatalf("logout = %d", logoutW.Code)
	}
	status := httptest.NewRequest(http.MethodGet, "/auth/session", nil)
	status.Header.Set("Origin", ProductionFrontendOrigin)
	status.AddCookie(&http.Cookie{Name: relayCookieName, Value: "session-a"})
	statusW := httptest.NewRecorder()
	s.Handler().ServeHTTP(statusW, status)
	if statusW.Code != http.StatusUnauthorized {
		t.Fatalf("session survived logout = %d", statusW.Code)
	}
}

func readFrame(t *testing.T, reader *bufio.Reader) string {
	t.Helper()
	var lines []string
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		line = strings.TrimSuffix(line, "\n")
		if line == "" {
			return strings.Join(lines, "\n")
		}
		lines = append(lines, line)
	}
}

func streamRequest(t *testing.T, base, secret string, last uint64, query string) (*http.Response, *bufio.Reader) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, base+"/events"+query, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", ProductionFrontendOrigin)
	req.Header.Set("Cookie", relayCookieName+"="+secret)
	if last != 0 {
		req.Header.Set("Last-Event-ID", strconv.FormatUint(last, 10))
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return res, bufio.NewReader(res.Body)
}

func TestSSEReplayResetHeartbeatAndQuerySpoofing(t *testing.T) {
	s, _ := newTestServer(t)
	s.heartbeat = 15 * time.Millisecond
	putTestSession(t, s, "session-a", 1, RepositoryGrant{RepositoryID: 123, InstallationID: 456})
	putTestSession(t, s, "session-b", 2, RepositoryGrant{RepositoryID: 999, InstallationID: 777})
	for i := 1; i <= 2; i++ {
		event := webhooks.Event{DeliveryID: fmtDelivery(i), Event: "pull_request", Action: "synchronize", RepositoryID: 123, InstallationID: 456, PullRequestNumber: i}
		if _, _, err := s.store.RecordWebhook(event.DeliveryID, "digest-"+strconv.Itoa(i), event, time.Now().UTC()); err != nil {
			t.Fatal(err)
		}
	}
	eventB := webhooks.Event{DeliveryID: "repo-b", Event: "push", RepositoryID: 999, InstallationID: 777, Ref: "refs/heads/main"}
	if _, _, err := s.store.RecordWebhook(eventB.DeliveryID, "digest-b", eventB, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}

	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	res, reader := streamRequest(t, ts.URL, "session-a", 1, "?repository_id=999")
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("stream = %d", res.StatusCode)
	}
	frame := readFrame(t, reader)
	if !strings.Contains(frame, `"repository_id":123`) || strings.Contains(frame, `"repository_id":999`) {
		t.Fatalf("spoofed stream frame = %q", frame)
	}
	if !strings.HasPrefix(frame, "id: 2") {
		t.Fatalf("Last-Event-ID did not replay sequence 2: %q", frame)
	}

	s.store.maxEvents = 1
	event3 := webhooks.Event{DeliveryID: "d3", Event: "push", RepositoryID: 123, InstallationID: 456, Ref: "refs/heads/main"}
	if _, _, err := s.store.RecordWebhook("d3", "digest-3", event3, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	resetRes, resetReader := streamRequest(t, ts.URL, "session-a", 1, "")
	defer resetRes.Body.Close()
	reset := readFrame(t, resetReader)
	if !strings.Contains(reset, "event: reset") {
		t.Fatalf("old cursor did not reset: %q", reset)
	}

	freshRes, freshReader := streamRequest(t, ts.URL, "session-a", s.store.Cursor(), "")
	defer freshRes.Body.Close()
	connected := readFrame(t, freshReader)
	if connected != ": connected" {
		t.Fatalf("connected frame = %q", connected)
	}
	heartbeat := readFrame(t, freshReader)
	if heartbeat != ": heartbeat" {
		t.Fatalf("heartbeat frame = %q", heartbeat)
	}
}

func fmtDelivery(i int) string { return "d" + strconv.Itoa(i) }

func TestHubMultipleSubscribersAndSlowConsumerBound(t *testing.T) {
	hub := newEventHub()
	allows := func(RelayEvent) bool { return true }
	a, ok := hub.subscribe("same-session", allows)
	if !ok {
		t.Fatal("first subscriber rejected")
	}
	b, ok := hub.subscribe("same-session", allows)
	if !ok {
		t.Fatal("second subscriber rejected")
	}
	defer hub.unsubscribe(a)
	defer hub.unsubscribe(b)
	event := RelayEvent{Sequence: 1, RepositoryID: 123, InstallationID: 456}
	hub.publish(event)
	if got := <-a.ch; got.Sequence != 1 {
		t.Fatalf("tab A = %+v", got)
	}
	if got := <-b.ch; got.Sequence != 1 {
		t.Fatalf("tab B = %+v", got)
	}

	slow, ok := hub.subscribe("slow", allows)
	if !ok {
		t.Fatal("slow subscriber rejected")
	}
	for i := 0; i <= subscriberQueueSize; i++ {
		hub.publish(RelayEvent{Sequence: uint64(i + 2), RepositoryID: 123, InstallationID: 456})
	}
	for range slow.ch {
	}
	hub.mu.Lock()
	_, exists := hub.subs[slow.id]
	hub.mu.Unlock()
	if exists {
		t.Fatal("slow subscriber remained registered")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func jsonResponse(status int, value any) *http.Response {
	body, _ := json.Marshal(value)
	return &http.Response{StatusCode: status, Status: http.StatusText(status), Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(body))}
}

func TestGitHubOAuthCreatesAuthorizedSecureSession(t *testing.T) {
	s, _ := newTestServer(t)
	s.httpClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		switch {
		case r.URL.Host == "github.com" && r.URL.Path == "/login/oauth/access_token":
			return jsonResponse(200, map[string]any{"access_token": "token"}), nil
		case r.URL.Host == "api.github.com" && r.URL.Path == "/user":
			return jsonResponse(200, map[string]any{"id": 42}), nil
		case r.URL.Host == "api.github.com" && r.URL.Path == "/user/installations":
			return jsonResponse(200, map[string]any{"installations": []map[string]any{{"id": 456}}}), nil
		case r.URL.Host == "api.github.com" && r.URL.Path == "/user/installations/456/repositories":
			return jsonResponse(200, map[string]any{"repositories": []map[string]any{{"id": 123, "full_name": "acme/repo"}}}), nil
		default:
			return jsonResponse(404, map[string]any{"message": "not found"}), nil
		}
	})}

	login := httptest.NewRequest(http.MethodGet, "/auth/github", nil)
	loginW := httptest.NewRecorder()
	s.Handler().ServeHTTP(loginW, login)
	if loginW.Code != http.StatusFound {
		t.Fatalf("login = %d", loginW.Code)
	}
	var stateCookie *http.Cookie
	for _, cookie := range loginW.Result().Cookies() {
		if cookie.Name == oauthStateCookieName {
			stateCookie = cookie
		}
	}
	if stateCookie == nil || !stateCookie.Secure || !stateCookie.HttpOnly || stateCookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("OAuth state cookie = %+v", stateCookie)
	}
	callback := httptest.NewRequest(http.MethodGet, "/auth/github/callback?state="+stateCookie.Value+"&code=code", nil)
	callback.AddCookie(stateCookie)
	callbackW := httptest.NewRecorder()
	s.Handler().ServeHTTP(callbackW, callback)
	if callbackW.Code != http.StatusFound {
		t.Fatalf("callback = %d: %s", callbackW.Code, callbackW.Body.String())
	}
	var relayCookie *http.Cookie
	for _, cookie := range callbackW.Result().Cookies() {
		if cookie.Name == relayCookieName && cookie.Value != "" {
			relayCookie = cookie
		}
	}
	if relayCookie == nil || !relayCookie.Secure || !relayCookie.HttpOnly || relayCookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("relay cookie = %+v", relayCookie)
	}
	session, ok := s.store.Session(SecretHash(relayCookie.Value), time.Now().UTC())
	if !ok || session.GitHubUserID != 42 || len(session.Repositories) != 1 ||
		session.Repositories[0].RepositoryID != 123 || session.Repositories[0].InstallationID != 456 {
		t.Fatalf("relay session = %+v, ok=%v", session, ok)
	}
}

func TestRelayArchitectureDoesNotImportLocalExecution(t *testing.T) {
	_, current, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(current), "..", "..", ".."))
	targets := []string{
		filepath.Join(root, "internal", "server", "relay"),
		filepath.Join(root, "cmd", "bonsai-relay"),
		filepath.Join(root, "cmd", "bonsai-server"),
	}
	banned := []string{
		"github.com/Tiago-0liveira/bonsai/internal/daemon",
		"github.com/Tiago-0liveira/bonsai/internal/git/local",
		"github.com/Tiago-0liveira/bonsai/internal/core/exec",
		"github.com/Tiago-0liveira/bonsai/internal/server/git",
		"os/exec",
	}
	for _, dir := range targets {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
				continue
			}
			path := filepath.Join(dir, entry.Name())
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
			if err != nil {
				t.Fatal(err)
			}
			for _, imp := range file.Imports {
				value, _ := strconv.Unquote(imp.Path.Value)
				for _, prefix := range banned {
					if value == prefix || strings.HasPrefix(value, prefix+"/") {
						t.Fatalf("%s imports banned relay dependency %s", path, value)
					}
				}
			}
		}
	}
}

