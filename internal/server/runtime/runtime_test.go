package runtime

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestValidateBrowserOrigin(t *testing.T) {
	for _, origin := range []string{
		ProductionBrowserOrigin,
		"http://127.0.0.1:5173",
		"http://localhost:5173",
	} {
		if err := validateBrowserOrigin(origin); err != nil {
			t.Fatalf("validateBrowserOrigin(%q): %v", origin, err)
		}
	}
	for _, origin := range []string{
		"https://example.com",
		"http://192.168.1.10:5173",
		"file://local",
	} {
		if err := validateBrowserOrigin(origin); err == nil {
			t.Fatalf("validateBrowserOrigin(%q) unexpectedly succeeded", origin)
		}
	}
}

func TestRequireLoopback(t *testing.T) {
	if err := requireLoopback("127.0.0.1:7001"); err != nil {
		t.Fatal(err)
	}
	for _, address := range []string{"0.0.0.0:7001", "192.168.1.20:7001", "localhost:7001"} {
		if err := requireLoopback(address); err == nil {
			t.Fatalf("requireLoopback(%q) unexpectedly succeeded", address)
		}
	}
}

func TestBrowserSecurityRequiresExactHostOriginAndCapability(t *testing.T) {
	caps := &capabilityStore{cap: capability{
		Token:     "secret-token",
		ExpiresAt: time.Now().Add(time.Minute),
	}}
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	handler := browserSecurity("127.0.0.1:7001", ProductionBrowserOrigin, caps, next)

	request := func(host, origin, token string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:7001/api/v1/repository", nil)
		r.Host = host
		r.Header.Set("Origin", origin)
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}

	if got := request("127.0.0.1:7001", ProductionBrowserOrigin, "secret-token").Code; got != http.StatusNoContent {
		t.Fatalf("authorized status = %d, want %d", got, http.StatusNoContent)
	}
	if got := request("localhost:7001", ProductionBrowserOrigin, "secret-token").Code; got != http.StatusForbidden {
		t.Fatalf("host mismatch status = %d, want %d", got, http.StatusForbidden)
	}
	if got := request("127.0.0.1:7001", "https://evil.example", "secret-token").Code; got != http.StatusForbidden {
		t.Fatalf("origin mismatch status = %d, want %d", got, http.StatusForbidden)
	}
	if got := request("127.0.0.1:7001", ProductionBrowserOrigin, "").Code; got != http.StatusUnauthorized {
		t.Fatalf("missing token status = %d, want %d", got, http.StatusUnauthorized)
	}
	if got := request("127.0.0.1:7001", ProductionBrowserOrigin, "wrong").Code; got != http.StatusUnauthorized {
		t.Fatalf("wrong token status = %d, want %d", got, http.StatusUnauthorized)
	}
}

func TestBrowserSecurityAllowsPrivateNetworkPreflight(t *testing.T) {
	caps := &capabilityStore{cap: capability{
		Token:     "secret-token",
		ExpiresAt: time.Now().Add(time.Minute),
	}}
	handler := browserSecurity(
		"127.0.0.1:7001",
		ProductionBrowserOrigin,
		caps,
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			t.Fatal("preflight reached application handler")
		}),
	)
	r := httptest.NewRequest(http.MethodOptions, "http://127.0.0.1:7001/api/v1/repository", nil)
	r.Host = "127.0.0.1:7001"
	r.Header.Set("Origin", ProductionBrowserOrigin)
	r.Header.Set("Access-Control-Request-Method", "GET")
	r.Header.Set("Access-Control-Request-Private-Network", "true")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != http.StatusNoContent {
		t.Fatalf("preflight status = %d, want %d", w.Code, http.StatusNoContent)
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != ProductionBrowserOrigin {
		t.Fatalf("allow origin = %q", got)
	}
	if got := w.Header().Get("Access-Control-Allow-Private-Network"); got != "true" {
		t.Fatalf("private network header = %q", got)
	}
}
