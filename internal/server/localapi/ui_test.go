package localapi

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"testing/fstest"
	"time"
)

var uiTestBundle = fstest.MapFS{
	"index.html":       {Data: []byte(`<html><head><meta name="bonsai-relay-origin" content="__BONSAI_RELAY_ORIGIN__" /></head><body></body></html>`)},
	"assets/app-1.js":  {Data: []byte("console.log(1)")},
	"assets/app-1.css": {Data: []byte("body{}")},
}

func newUIServer(t *testing.T, browserOrigin string) *Server {
	t.Helper()
	s, err := New(Config{
		ProjectRootsPath: filepath.Join(t.TempDir(), "project-roots.json"),
		Address:          "127.0.0.1:7001",
		BrowserOrigin:    browserOrigin,
		UI:               uiTestBundle,
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func uiRequest(h http.Handler, method, path, host, origin string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://127.0.0.1:7001"+path, nil)
	r.Host = host
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestOwnOriginsAreAllowedOnBothLoopbackNames(t *testing.T) {
	h := newUIServer(t, ProductionBrowserOrigin).Handler()
	for _, host := range []string{"127.0.0.1:7001", "localhost:7001"} {
		for _, origin := range []string{"http://127.0.0.1:7001", "http://localhost:7001"} {
			w := uiRequest(h, http.MethodPost, "/api/session", host, origin)
			if w.Code != http.StatusCreated {
				t.Fatalf("Host %s Origin %s: session status %d %s", host, origin, w.Code, w.Body.String())
			}
			if got := w.Header().Get("Access-Control-Allow-Origin"); got != origin {
				t.Fatalf("Host %s Origin %s: allow origin %q", host, origin, got)
			}
		}
	}
}

func TestForeignOriginsAreRejected(t *testing.T) {
	h := newUIServer(t, ProductionBrowserOrigin).Handler()
	for _, origin := range []string{
		"https://evil.example", "http://evil.example", "null",
		"http://127.0.0.1:7002", "http://localhost:7002", "https://127.0.0.1:7001", "https://localhost:7001",
		"http://[::1]:7001", "http://127.0.0.2:7001", "http://localhost.evil.example:7001",
	} {
		if w := uiRequest(h, http.MethodPost, "/api/session", "127.0.0.1:7001", origin); w.Code != http.StatusForbidden {
			t.Fatalf("Origin %s: status %d", origin, w.Code)
		}
	}
	// No Origin header on an API route is still refused.
	if w := uiRequest(h, http.MethodPost, "/api/session", "127.0.0.1:7001", ""); w.Code != http.StatusForbidden {
		t.Fatalf("missing Origin: status %d", w.Code)
	}
	// Hosts other than the API's own two names are refused, even on the UI.
	for _, host := range []string{"evil.example:7001", "localhost:7002", "127.0.0.1", "localhost", "[::1]:7001"} {
		for _, path := range []string{"/api/session", "/app", "/assets/app-1.js", "/"} {
			method := http.MethodGet
			if path == "/api/session" {
				method = http.MethodPost
			}
			if w := uiRequest(h, method, path, host, "http://127.0.0.1:7001"); w.Code != http.StatusForbidden {
				t.Fatalf("Host %s %s: status %d", host, path, w.Code)
			}
		}
	}
}

func TestHostedOriginFollowsTheInterfaceSetting(t *testing.T) {
	enabled := newUIServer(t, ProductionBrowserOrigin).Handler()
	if w := uiRequest(enabled, http.MethodPost, "/api/session", "127.0.0.1:7001", ProductionBrowserOrigin); w.Code != http.StatusCreated {
		t.Fatalf("hosted origin with the interface enabled: status %d", w.Code)
	}
	disabled := newUIServer(t, "").Handler()
	if w := uiRequest(disabled, http.MethodPost, "/api/session", "127.0.0.1:7001", ProductionBrowserOrigin); w.Code != http.StatusForbidden {
		t.Fatalf("hosted origin with the interface disabled: status %d", w.Code)
	}
	if w := uiRequest(disabled, http.MethodPost, "/api/session", "127.0.0.1:7001", "http://127.0.0.1:7001"); w.Code != http.StatusCreated {
		t.Fatalf("own origin with the hosted interface disabled: status %d", w.Code)
	}
	// An empty Origin never matches the disabled (empty) hosted origin.
	if w := uiRequest(disabled, http.MethodPost, "/api/session", "127.0.0.1:7001", ""); w.Code != http.StatusForbidden {
		t.Fatalf("empty origin with the hosted interface disabled: status %d", w.Code)
	}
}

func TestOwnOriginPreflight(t *testing.T) {
	h := newUIServer(t, "").Handler()
	r := httptest.NewRequest(http.MethodOptions, "http://localhost:7001/api/projects", nil)
	r.Host = "localhost:7001"
	r.Header.Set("Origin", "http://localhost:7001")
	r.Header.Set("Access-Control-Request-Method", http.MethodPost)
	r.Header.Set("Access-Control-Request-Headers", "content-type, x-bonsai-session")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusNoContent || w.Header().Get("Access-Control-Allow-Origin") != "http://localhost:7001" {
		t.Fatalf("preflight = %d allow %q", w.Code, w.Header().Get("Access-Control-Allow-Origin"))
	}
}

func TestEmbeddedUIIsServedWithoutASession(t *testing.T) {
	h := newUIServer(t, "").Handler()

	root := uiRequest(h, http.MethodGet, "/", "127.0.0.1:7001", "")
	if root.Code != http.StatusFound || root.Header().Get("Location") != "/app/" {
		t.Fatalf("GET / = %d %q", root.Code, root.Header().Get("Location"))
	}
	for _, path := range []string{"/app", "/app/", "/app/github", "/app/settings"} {
		w := uiRequest(h, http.MethodGet, path, "localhost:7001", "")
		if w.Code != http.StatusOK {
			t.Fatalf("GET %s = %d %s", path, w.Code, w.Body.String())
		}
		body := w.Body.String()
		if !strings.Contains(body, `<meta name="bonsai-local-api-origin" content="http://localhost:7001" />`) ||
			!strings.Contains(body, `<meta name="bonsai-relay-origin" content="" />`) ||
			!strings.Contains(body, `<meta name="bonsai-entry" content="local" />`) {
			t.Fatalf("GET %s runtime config missing:\n%s", path, body)
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("GET %s cache control %q", path, w.Header().Get("Cache-Control"))
		}
		if csp := w.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "connect-src 'self';") || !strings.Contains(csp, "frame-ancestors 'none'") {
			t.Fatalf("GET %s CSP %q", path, csp)
		}
		if w.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Fatalf("GET %s must not send CORS headers", path)
		}
	}
	asset := uiRequest(h, http.MethodGet, "/assets/app-1.js", "127.0.0.1:7001", "")
	if asset.Code != http.StatusOK || asset.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" {
		t.Fatalf("asset = %d cache %q", asset.Code, asset.Header().Get("Cache-Control"))
	}
	if w := uiRequest(h, http.MethodGet, "/assets/missing.js", "127.0.0.1:7001", ""); w.Code != http.StatusNotFound {
		t.Fatalf("missing asset = %d", w.Code)
	}
	// A cross-site page that names the UI in Origin still only reads static files.
	if w := uiRequest(h, http.MethodGet, "/app", "127.0.0.1:7001", "https://evil.example"); w.Code != http.StatusOK || w.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("static with foreign Origin = %d allow %q", w.Code, w.Header().Get("Access-Control-Allow-Origin"))
	}
	// Writes to UI paths are not static: they need an allowed origin.
	if w := uiRequest(h, http.MethodPost, "/app", "127.0.0.1:7001", "https://evil.example"); w.Code != http.StatusForbidden {
		t.Fatalf("POST /app with foreign Origin = %d", w.Code)
	}
	// API routes are unchanged: still session-protected.
	if w := uiRequest(h, http.MethodGet, "/api/projects", "127.0.0.1:7001", "http://127.0.0.1:7001"); w.Code != http.StatusUnauthorized {
		t.Fatalf("API without session = %d", w.Code)
	}
}

// Browsers send no Origin on same-origin GETs; the embedded UI's reads are
// recognised by Sec-Fetch-Site, which page scripts cannot set.
func TestSameOriginReadsWithoutOriginHeader(t *testing.T) {
	s := newUIServer(t, "")
	h := s.Handler()
	session, err := s.sessions.create()
	if err != nil {
		t.Fatal(err)
	}
	read := func(method, host, fetchSite string) int {
		r := httptest.NewRequest(method, "http://"+host+"/api/settings/project-roots", nil)
		r.Host = host
		r.Header.Set("X-Bonsai-Session", session.Token)
		if fetchSite != "" {
			r.Header.Set("Sec-Fetch-Site", fetchSite)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}
	for _, host := range []string{"127.0.0.1:7001", "localhost:7001"} {
		if got := read(http.MethodGet, host, "same-origin"); got != http.StatusOK {
			t.Fatalf("same-origin GET via %s = %d", host, got)
		}
	}
	for _, site := range []string{"", "cross-site", "same-site", "none"} {
		if got := read(http.MethodGet, "127.0.0.1:7001", site); got != http.StatusForbidden {
			t.Fatalf("GET without Origin, Sec-Fetch-Site %q = %d", site, got)
		}
	}
	if got := read(http.MethodGet, "evil.example:7001", "same-origin"); got != http.StatusForbidden {
		t.Fatalf("same-origin GET on a foreign Host = %d", got)
	}
	// Writes always carry Origin in browsers; fetch metadata alone is not enough.
	if got := read(http.MethodDelete, "127.0.0.1:7001", "same-origin"); got != http.StatusForbidden {
		t.Fatalf("same-origin DELETE without Origin = %d", got)
	}
	// Still session-protected.
	r := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:7001/api/settings/project-roots", nil)
	r.Header.Set("Sec-Fetch-Site", "same-origin")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("same-origin GET without a session = %d", w.Code)
	}
}

// TestEmbeddedUIBrowserFixture serves the e2e UI bundle through the real local
// API handler on 127.0.0.1:7011, for the Playwright "local-served" entry
// point. Most specs mock the API routes; the page, assets, headers and
// runtime config always come from this Go server, and one spec talks to its
// real API (no project roots configured).
func TestEmbeddedUIBrowserFixture(t *testing.T) {
	if os.Getenv("BONSAI_UI_BROWSER_FIXTURE") != "1" {
		t.Skip("browser fixture disabled")
	}
	dist, err := filepath.Abs(filepath.Join("..", "..", "..", "web", "dist-e2e"))
	if err != nil {
		t.Fatal(err)
	}
	// The e2e bundle is built by the first Playwright web server.
	deadline := time.Now().Add(5 * time.Minute)
	for {
		if _, err := os.Stat(filepath.Join(dist, "index.html")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s/index.html not built", dist)
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Setenv("HOME", t.TempDir())
	s, err := New(Config{
		ProjectRootsPath: filepath.Join(t.TempDir(), "project-roots.json"),
		Address:          "127.0.0.1:7011",
		UI:               os.DirFS(dist),
	})
	if err != nil {
		t.Fatal(err)
	}
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
