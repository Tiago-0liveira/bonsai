package claude

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// usageServer answers every request with status and body and records the last one.
type usageServer struct {
	*httptest.Server
	hits atomic.Int32
	last atomic.Pointer[http.Request]
}

func newUsageServer(t *testing.T, status int, body []byte) *usageServer {
	t.Helper()
	s := &usageServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.hits.Add(1)
		s.last.Store(r)
		w.WriteHeader(status)
		_, _ = w.Write(body)
	}))
	t.Cleanup(s.Close)
	return s
}

func writeCredentials(t *testing.T, e *testEnv, account agents.Account, token string, expires time.Time) string {
	t.Helper()
	path := filepath.Join(configDir(e.accounts, account), ".credentials.json")
	data := fmt.Sprintf(`{"claudeAiOauth":{"accessToken":%q,"refreshToken":"refresh-secret","expiresAt":%d}}`, token, expires.UnixMilli())
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func fraction(t *testing.T, limits []agents.UsageLimit, id string) float64 {
	t.Helper()
	for _, l := range limits {
		if l.ID == id {
			if l.RemainingFraction == nil {
				t.Fatalf("%s has no fraction", id)
			}
			return *l.RemainingFraction
		}
	}
	t.Fatalf("limit %s missing in %+v", id, limits)
	return 0
}

func TestUsageSendsHeadersAndReadsLoginToken(t *testing.T) {
	e := newTestEnv(t)
	account := e.addLogin("work")
	server := newUsageServer(t, 200, readFixture(t, "usage-response.json"))
	e.provider.usageEndpoint = server.URL
	creds := writeCredentials(t, e, account, "access-abc", time.Now().Add(time.Hour))
	before, _ := os.ReadFile(creds)

	snapshot, err := e.provider.Usage(t.Context(), account, agents.UsageOptions{})
	if err != nil {
		t.Fatal(err)
	}
	req := server.last.Load()
	if req.Method != http.MethodGet || req.Header.Get("Authorization") != "Bearer access-abc" || req.Header.Get("anthropic-beta") != "oauth-2025-04-20" {
		t.Fatalf("request = %s %v", req.Method, req.Header)
	}
	if snapshot.Provider != ProviderID || snapshot.AccountID != account.ID || snapshot.FetchedAt.IsZero() {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	if got := fraction(t, snapshot.Limits, "five_hour"); got < 0.899 || got > 0.901 {
		t.Errorf("five_hour = %v", got)
	}
	if got := fraction(t, snapshot.Limits, "seven_day"); got != 0.5 {
		t.Errorf("seven_day = %v", got)
	}
	// Bonsai never refreshes or rewrites the login.
	after, _ := os.ReadFile(creds)
	if string(before) != string(after) || server.hits.Load() != 1 {
		t.Fatalf("credentials changed or extra requests: hits=%d", server.hits.Load())
	}
}

func TestUsageTokenProfileIsUnsupportedWithoutRequest(t *testing.T) {
	e := newTestEnv(t)
	account := e.addToken("ci")
	server := newUsageServer(t, 200, readFixture(t, "usage-response.json"))
	e.provider.usageEndpoint = server.URL
	_, err := e.provider.Usage(t.Context(), account, agents.UsageOptions{})
	if !errors.Is(err, agents.ErrUsageUnsupported) || !strings.Contains(err.Error(), "long-lived tokens") {
		t.Fatalf("err = %v", err)
	}
	// Phase 0: setup tokens always get 403 (missing user:profile scope), so the
	// token is not sent at all.
	if server.hits.Load() != 0 {
		t.Fatal("long-lived token was sent to the usage endpoint")
	}
}

func TestUsageLoginRejectionIsSoft(t *testing.T) {
	e := newTestEnv(t)
	account := e.addLogin("work")
	writeCredentials(t, e, account, "access-abc", time.Now().Add(time.Hour))
	for status, want := range map[int]string{401: "login rejected", 403: "cannot read usage"} {
		server := newUsageServer(t, status, readFixture(t, "usage-response-setup-token.json"))
		e.provider.usageEndpoint = server.URL
		_, err := e.provider.Usage(t.Context(), account, agents.UsageOptions{Refresh: true})
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("status %d: err = %v", status, err)
		}
		if strings.Contains(err.Error(), "request_id") || strings.Contains(err.Error(), "scope") {
			t.Fatalf("response body leaked: %v", err)
		}
	}
}

func TestUsageFailuresAreRememberedUntilRefresh(t *testing.T) {
	e := newTestEnv(t)
	account := e.addLogin("work")
	writeCredentials(t, e, account, "access-abc", time.Now().Add(time.Hour))
	server := newUsageServer(t, 429, nil)
	e.provider.usageEndpoint = server.URL
	now := time.Now()
	e.provider.now = func() time.Time { return now }
	for i := 0; i < 3; i++ {
		if _, err := e.provider.Usage(t.Context(), account, agents.UsageOptions{}); err == nil || !strings.Contains(err.Error(), "429") {
			t.Fatalf("err = %v", err)
		}
	}
	if server.hits.Load() != 1 {
		t.Fatalf("hits = %d, want 1 (failure remembered)", server.hits.Load())
	}
	_, _ = e.provider.Usage(t.Context(), account, agents.UsageOptions{Refresh: true})
	now = now.Add(usageFailureTTL + time.Second)
	_, _ = e.provider.Usage(t.Context(), account, agents.UsageOptions{})
	if server.hits.Load() != 3 {
		t.Fatalf("hits = %d, want 3 after refresh and expiry", server.hits.Load())
	}
}

func TestUsageNeverFollowsRedirectsOrUsesClearText(t *testing.T) {
	e := newTestEnv(t)
	account := e.addLogin("work")
	writeCredentials(t, e, account, "access-abc", time.Now().Add(time.Hour))
	target := newUsageServer(t, 200, readFixture(t, "usage-response.json"))
	redirector := httptest.NewServer(http.RedirectHandler(target.URL, http.StatusFound))
	t.Cleanup(redirector.Close)
	e.provider.usageEndpoint = redirector.URL
	if _, err := e.provider.Usage(t.Context(), account, agents.UsageOptions{Refresh: true}); err == nil || !strings.Contains(err.Error(), "302") {
		t.Fatalf("err = %v", err)
	}
	if target.hits.Load() != 0 {
		t.Fatal("redirect was followed with the bearer token")
	}
	for endpoint, ok := range map[string]bool{
		"https://api.anthropic.com/api/oauth/usage": true, "http://127.0.0.1:1/x": true, "http://localhost/x": true, "http://[::1]/x": true,
		"http://api.anthropic.com/api/oauth/usage": false, "http://example.com/": false, "ftp://127.0.0.1/": false, "": false,
	} {
		if safeUsageEndpoint(endpoint) != ok {
			t.Errorf("safeUsageEndpoint(%q) = %v", endpoint, !ok)
		}
	}
	e.provider.usageEndpoint = "http://api.anthropic.com/api/oauth/usage"
	if _, err := e.provider.Usage(t.Context(), account, agents.UsageOptions{Refresh: true}); err == nil || !strings.Contains(err.Error(), "https") {
		t.Fatalf("clear text endpoint accepted: %v", err)
	}
}

func TestUsageExpiredLoginMakesNoRequest(t *testing.T) {
	e := newTestEnv(t)
	account := e.addLogin("work")
	server := newUsageServer(t, 200, readFixture(t, "usage-response.json"))
	e.provider.usageEndpoint = server.URL
	writeCredentials(t, e, account, "access-abc", time.Now().Add(-time.Minute))
	_, err := e.provider.Usage(t.Context(), account, agents.UsageOptions{})
	if err == nil || !strings.Contains(err.Error(), "until a session refreshes the login") || errors.Is(err, agents.ErrUsageUnsupported) {
		t.Fatalf("err = %v", err)
	}
	if server.hits.Load() != 0 {
		t.Fatal("expired token was sent")
	}
}

func TestUsageMissingCredentials(t *testing.T) {
	e := newTestEnv(t)
	account := e.addLogin("work")
	server := newUsageServer(t, 200, nil)
	e.provider.usageEndpoint = server.URL
	if err := os.Remove(filepath.Join(configDir(e.accounts, account), ".credentials.json")); err != nil {
		t.Fatal(err)
	}
	e.provider.usageOS = "darwin"
	if _, err := e.provider.Usage(t.Context(), account, agents.UsageOptions{}); !errors.Is(err, agents.ErrUsageUnsupported) || !strings.Contains(err.Error(), "keychain") {
		t.Fatalf("darwin err = %v", err)
	}
	e.provider.usageOS = "linux"
	if _, err := e.provider.Usage(t.Context(), account, agents.UsageOptions{}); err == nil || errors.Is(err, agents.ErrUsageUnsupported) || !strings.Contains(err.Error(), "not logged in") {
		t.Fatalf("linux err = %v", err)
	}
	if server.hits.Load() != 0 {
		t.Fatal("request without credentials")
	}
}

func TestUsageTimeoutRespectsContext(t *testing.T) {
	e := newTestEnv(t)
	account := e.addLogin("work")
	writeCredentials(t, e, account, "access-abc", time.Now().Add(time.Hour))
	release := make(chan struct{})
	slow := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-release }))
	t.Cleanup(func() { close(release); slow.Close() })
	e.provider.usageEndpoint = slow.URL
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := e.provider.Usage(ctx, account, agents.UsageOptions{})
	if err == nil || time.Since(start) > 3*time.Second {
		t.Fatalf("err = %v after %v", err, time.Since(start))
	}
}

func TestUsageErrorsNeverContainTokenOrBody(t *testing.T) {
	e := newTestEnv(t)
	login := e.addLogin("work")
	token := e.addToken("ci")
	const secret = "sk-ant-oat01-LEAKLEAKLEAKLEAKLEAKLEAKLEAK"
	writeCredentials(t, e, login, secret, time.Now().Add(time.Hour))
	body := []byte(`{"error":"echo ` + secret + ` refresh-secret"}`)
	for _, status := range []int{200, 400, 401, 403, 429, 500, 502} {
		server := newUsageServer(t, status, body)
		e.provider.usageEndpoint = server.URL
		for _, account := range []agents.Account{login, token} {
			_, err := e.provider.Usage(t.Context(), account, agents.UsageOptions{Refresh: true})
			if err == nil {
				continue
			}
			for _, bad := range []string{secret, testToken, "refresh-secret", "echo"} {
				if strings.Contains(err.Error(), bad) {
					t.Fatalf("status %d: error contains %q: %v", status, bad, err)
				}
			}
		}
	}
	// A transport error that embeds the token is redacted too.
	err := redact(fmt.Errorf("Get https://x?t=%s: refused", secret), secret)
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("redact kept the secret: %v", err)
	}
}

func TestParseUsageShapes(t *testing.T) {
	t.Run("recorded response uses limits and ignores unknown buckets", func(t *testing.T) {
		limits, warnings, err := ParseUsage(readFixture(t, "usage-response.json"))
		if err != nil || len(warnings) != 0 || len(limits) != 2 {
			t.Fatalf("limits=%+v warnings=%v err=%v", limits, warnings, err)
		}
		if limits[0].ID != "five_hour" || limits[0].Window != "5h" || limits[1].ID != "seven_day" || limits[1].Window != "weekly" {
			t.Fatalf("limits = %+v", limits)
		}
		if limits[1].ResetsAt == nil || limits[1].ResetsAt.Format(time.RFC3339) != "2026-10-11T09:00:00Z" {
			t.Fatalf("reset = %v", limits[1].ResetsAt)
		}
	})
	t.Run("flat buckets with per-model weekly", func(t *testing.T) {
		limits, _, err := ParseUsage([]byte(`{"five_hour":{"utilization":25,"resets_at":"2026-10-09T19:00:00Z"},"seven_day":{"utilization":40.5,"resets_at":null},"seven_day_opus":{"utilization":100,"resets_at":"2026-10-11T09:00:00Z"},"seven_day_sonnet":null,"tangelo":{"utilization":3}}`))
		if err != nil || len(limits) != 3 {
			t.Fatalf("limits=%+v err=%v", limits, err)
		}
		if fraction(t, limits, "five_hour") != 0.75 || fraction(t, limits, "seven_day_opus") != 0 {
			t.Fatalf("limits = %+v", limits)
		}
		if limits[1].ResetsAt != nil || limits[2].ID != "seven_day_opus" || limits[2].Label != "Weekly Opus" {
			t.Fatalf("limits = %+v", limits)
		}
	})
	t.Run("limits array with scope", func(t *testing.T) {
		limits, _, err := ParseUsage([]byte(`{"limits":[{"kind":"weekly_all","group":"weekly","percent":60,"resets_at":"2026-10-11T09:00:00Z"},{"kind":"weekly_model","group":"weekly","percent":10,"scope":"haiku"},{"kind":"session","percent":5},{"kind":"broken"}]}`))
		if err != nil || len(limits) != 3 {
			t.Fatalf("limits=%+v err=%v", limits, err)
		}
		if fraction(t, limits, "five_hour") != 0.95 || fraction(t, limits, "weekly_model_haiku") != 0.9 || limits[2].Window != "weekly" {
			t.Fatalf("limits = %+v", limits)
		}
	})
	t.Run("percent is clamped and flat duplicates do not repeat", func(t *testing.T) {
		limits, _, _ := ParseUsage([]byte(`{"limits":[{"kind":"session","percent":250}],"five_hour":{"utilization":1}}`))
		if len(limits) != 1 || fraction(t, limits, "five_hour") != 0 {
			t.Fatalf("limits = %+v", limits)
		}
	})
	t.Run("unknown JSON is an empty snapshot with a warning", func(t *testing.T) {
		for _, body := range []string{`{}`, `{"hello":"world"}`, `{"limits":"nope","five_hour":7}`, `{"limits":[1,2,3]}`} {
			limits, warnings, err := ParseUsage([]byte(body))
			if err != nil || len(limits) != 0 || len(warnings) != 1 {
				t.Fatalf("%s: limits=%v warnings=%v err=%v", body, limits, warnings, err)
			}
		}
	})
	t.Run("non-objects are errors", func(t *testing.T) {
		for _, body := range []string{``, `not json`, `[]`, `null`, `42`} {
			if _, _, err := ParseUsage([]byte(body)); err == nil {
				t.Fatalf("%q parsed", body)
			}
		}
	})
}

func TestUsageUnknownResponseThroughProvider(t *testing.T) {
	e := newTestEnv(t)
	account := e.addLogin("work")
	writeCredentials(t, e, account, "access-abc", time.Now().Add(time.Hour))
	e.provider.usageEndpoint = newUsageServer(t, 200, []byte(`{"surprise":true}`)).URL
	snapshot, err := e.provider.Usage(t.Context(), account, agents.UsageOptions{})
	if err != nil || len(snapshot.Limits) != 0 || len(snapshot.Warnings) != 1 {
		t.Fatalf("snapshot=%+v err=%v", snapshot, err)
	}
}

func TestUsageCapabilityAndPolicy(t *testing.T) {
	e := newTestEnv(t)
	if !e.provider.Capabilities().Usage || e.provider.UsageTTL() != 5*time.Minute {
		t.Fatalf("usage=%v ttl=%v", e.provider.Capabilities().Usage, e.provider.UsageTTL())
	}
	var _ agents.UsagePolicy = e.provider
}
