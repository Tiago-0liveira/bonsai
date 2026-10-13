package claude

import (
	"context"
	"encoding/json"
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
		parsed, err := ParseUsage(readFixture(t, "usage-response.json"))
		limits, warnings := parsed.Limits, parsed.Warnings
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
		parsed, err := ParseUsage([]byte(`{"five_hour":{"utilization":25,"resets_at":"2026-10-09T19:00:00Z"},"seven_day":{"utilization":40.5,"resets_at":null},"seven_day_opus":{"utilization":100,"resets_at":"2026-10-11T09:00:00Z"},"seven_day_sonnet":null,"tangelo":{"utilization":3}}`))
		limits := parsed.Limits
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
		parsed, err := ParseUsage([]byte(`{"limits":[{"kind":"weekly_all","group":"weekly","percent":60,"resets_at":"2026-10-11T09:00:00Z"},{"kind":"weekly_model","group":"weekly","percent":10,"scope":"haiku"},{"kind":"session","percent":5},{"kind":"broken"}]}`))
		limits := parsed.Limits
		if err != nil || len(limits) != 3 {
			t.Fatalf("limits=%+v err=%v", limits, err)
		}
		if fraction(t, limits, "five_hour") != 0.95 || fraction(t, limits, "weekly_model_haiku") != 0.9 || limits[2].Window != "weekly" {
			t.Fatalf("limits = %+v", limits)
		}
	})
	t.Run("percent is clamped and flat duplicates do not repeat", func(t *testing.T) {
		parsed, _ := ParseUsage([]byte(`{"limits":[{"kind":"session","percent":250}],"five_hour":{"utilization":1}}`))
		limits := parsed.Limits
		if len(limits) != 1 || fraction(t, limits, "five_hour") != 0 {
			t.Fatalf("limits = %+v", limits)
		}
	})
	t.Run("unknown JSON is an empty snapshot with a warning", func(t *testing.T) {
		for _, body := range []string{`{}`, `{"hello":"world"}`, `{"limits":"nope","five_hour":7}`, `{"limits":[1,2,3]}`} {
			parsed, err := ParseUsage([]byte(body))
			if err != nil || len(parsed.Limits) != 0 || len(parsed.Credits) != 0 || len(parsed.Warnings) != 1 {
				t.Fatalf("%s: parsed=%+v err=%v", body, parsed, err)
			}
		}
	})
	t.Run("non-objects are errors", func(t *testing.T) {
		for _, body := range []string{``, `not json`, `[]`, `null`, `42`} {
			if _, err := ParseUsage([]byte(body)); err == nil {
				t.Fatalf("%q parsed", body)
			}
		}
	})
}

func TestParseUsageCredits(t *testing.T) {
	t.Run("recorded dollar pool, disabled spend and a window not started", func(t *testing.T) {
		parsed, err := ParseUsage(readFixture(t, "usage-response-credits.json"))
		if err != nil || len(parsed.Limits) != 2 || len(parsed.Warnings) != 0 {
			t.Fatalf("parsed=%+v err=%v", parsed, err)
		}
		if parsed.Limits[0].ID != "five_hour" || parsed.Limits[0].ResetsAt != nil || fraction(t, parsed.Limits, "five_hour") != 1 {
			t.Fatalf("five hour = %+v", parsed.Limits[0])
		}
		if len(parsed.Credits) != 1 {
			t.Fatalf("credits = %+v", parsed.Credits)
		}
		c := parsed.Credits[0]
		if c.ID != "iguana_necktie" || c.Label != "Usage credits" || c.Unit != agents.CreditUnitUSD || !c.Enabled {
			t.Fatalf("credit = %+v", c)
		}
		if *c.Limit != 100 || *c.Used != 1.90559 || *c.Balance != 98.09441 || c.ResetsAt.Format(time.RFC3339) != "2026-11-05T07:59:00Z" {
			t.Fatalf("credit amounts = %v %v %v %v", *c.Limit, *c.Used, *c.Balance, c.ResetsAt)
		}
	})
	// Synthetic: no capture has spend or extra_usage enabled yet.
	t.Run("enabled spend wins over extra_usage", func(t *testing.T) {
		parsed, err := ParseUsage([]byte(`{"five_hour":{"utilization":1},
			"extra_usage":{"is_enabled":true,"monthly_limit":5000,"used_credits":1234,"currency":"USD","decimal_places":2},
			"spend":{"used":{"amount_minor":1234,"currency":"USD","exponent":2},"limit":{"amount_minor":5000,"currency":"USD","exponent":2},"enabled":true}}`))
		if err != nil || len(parsed.Credits) != 1 {
			t.Fatalf("parsed=%+v err=%v", parsed, err)
		}
		c := parsed.Credits[0]
		if c.ID != "extra_usage" || !c.Enabled || c.Unit != "USD" || *c.Used != 12.34 || *c.Limit != 50 || c.Balance != nil {
			t.Fatalf("credit = %+v", c)
		}
	})
	t.Run("extra_usage alone, in minor units", func(t *testing.T) {
		parsed, _ := ParseUsage([]byte(`{"extra_usage":{"is_enabled":true,"monthly_limit":2000,"used_credits":150,"currency":"eur","decimal_places":2}}`))
		if len(parsed.Credits) != 1 || *parsed.Credits[0].Used != 1.5 || *parsed.Credits[0].Limit != 20 || parsed.Credits[0].Unit != "EUR" {
			t.Fatalf("credits = %+v", parsed.Credits)
		}
	})
	t.Run("extra_usage without decimal places keeps no amounts", func(t *testing.T) {
		parsed, _ := ParseUsage([]byte(`{"extra_usage":{"is_enabled":true,"monthly_limit":2000,"used_credits":150}}`))
		if len(parsed.Credits) != 1 || parsed.Credits[0].Used != nil || parsed.Credits[0].Limit != nil {
			t.Fatalf("credits = %+v", parsed.Credits)
		}
	})
	t.Run("disabled with no values emits nothing", func(t *testing.T) {
		parsed, _ := ParseUsage([]byte(`{"spend":{"used":{"amount_minor":0,"currency":"USD","exponent":2},"limit":null,"enabled":false},"tangelo":{"limit_dollars":null}}`))
		if len(parsed.Credits) != 0 {
			t.Fatalf("credits = %+v", parsed.Credits)
		}
	})
}

func TestParseUsageResetGrants(t *testing.T) {
	parsed, err := ParseUsage(readFixture(t, "usage-response-resets.json"))
	if err != nil || len(parsed.ResetCredits) != 1 {
		t.Fatalf("parsed=%+v err=%v", parsed, err)
	}
	r := parsed.ResetCredits[0]
	if r.Title != "Claude Opus 5.5 launch: one usage-limit reset for Pro and Max" || r.Status != "available" || r.Count != 1 ||
		r.GrantedAt.Format(time.RFC3339) != "2026-09-22T16:00:00Z" || r.ExpiresAt.Format(time.RFC3339) != "2026-10-22T16:00:00Z" {
		t.Fatalf("reset = %+v", r)
	}
	if strings.Join(r.Clears, ",") != "five_hour,seven_day,seven_day_overage_included" {
		t.Fatalf("clears = %v", r.Clears)
	}
	if data, _ := json.Marshal(parsed.ResetCredits); strings.Contains(string(data), "opus55-launch-promax") {
		t.Fatalf("grant id leaked: %s", data)
	}
	for body, want := range map[string]string{
		`{"cedar_ember":{"eligible":true,"grants":[{"label":"x","resets_left":0}]}}`:                    "used",
		`{"cedar_ember":{"eligible":false,"grants":[{"label":"x","resets_left":2,"usable_now":true}]}}`: "unavailable",
		`{"cedar_ember":{"eligible":true,"grants":[{"label":"x","resets_left":2,"paused":true}]}}`:      "paused",
		`{"cedar_ember":{"eligible":true,"grants":[{"label":"x","resets_left":2,"usable_now":false}]}}`: "not_usable_yet",
	} {
		parsed, _ := ParseUsage([]byte(body))
		if len(parsed.ResetCredits) != 1 || parsed.ResetCredits[0].Status != want {
			t.Errorf("%s = %+v", body, parsed.ResetCredits)
		}
	}
	for _, body := range []string{`{"cedar_ember":null}`, `{"cedar_ember":"x"}`, `{"cedar_ember":{"grants":[{"label":"no count"}]}}`} {
		if parsed, _ := ParseUsage([]byte(body)); len(parsed.ResetCredits) != 0 {
			t.Errorf("%s = %+v", body, parsed.ResetCredits)
		}
	}
}

func TestUsageAsksForResetGrantsAsClaudeCode(t *testing.T) {
	e := newTestEnv(t)
	account := e.addLogin("work")
	writeCredentials(t, e, account, "access-abc", time.Now().Add(time.Hour))
	server := newUsageServer(t, 200, readFixture(t, "usage-response-resets.json"))
	e.provider.usageEndpoint = server.URL + "/api/oauth/usage"
	snapshot, err := e.provider.Usage(t.Context(), account, agents.UsageOptions{})
	if err != nil || len(snapshot.ResetCredits) != 1 {
		t.Fatalf("snapshot=%+v err=%v", snapshot, err)
	}
	req := server.last.Load()
	if req.URL.Query().Get("cedar_ember") != "1" || req.Header.Get("x-app") != "cli" || !strings.HasPrefix(req.Header.Get("User-Agent"), "claude-cli/") {
		t.Fatalf("request = %s %v", req.URL, req.Header)
	}
}

func TestUsageReportsPlanAndCredits(t *testing.T) {
	e := newTestEnv(t)
	account := e.addLogin("work")
	path := writeCredentials(t, e, account, "access-abc", time.Now().Add(time.Hour))
	data := fmt.Sprintf(`{"claudeAiOauth":{"accessToken":"access-abc","expiresAt":%d,"subscriptionType":"max","rateLimitTier":"default_claude_max_20x"}}`, time.Now().Add(time.Hour).UnixMilli())
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	e.provider.usageEndpoint = newUsageServer(t, 200, readFixture(t, "usage-response-credits.json")).URL
	snapshot, err := e.provider.Usage(t.Context(), account, agents.UsageOptions{})
	if err != nil || snapshot.Plan != "Max 20x" || len(snapshot.Credits) != 1 {
		t.Fatalf("snapshot=%+v err=%v", snapshot, err)
	}
}

func TestPlanLabel(t *testing.T) {
	for _, tc := range []struct{ sub, tier, want string }{
		{"pro", "default_claude_ai", "Pro"},
		{"max", "default_claude_max_5x", "Max 5x"},
		{"max", "default_claude_max_20x", "Max 20x"},
		{"max", "", "Max"},
		{"team", "", "Team"},
		{"enterprise", "", "Enterprise"},
		{"", "x", ""},
		{"ultra\x00", "", "ultra"},
	} {
		if got := planLabel(tc.sub, tc.tier); got != tc.want {
			t.Errorf("planLabel(%q, %q) = %q, want %q", tc.sub, tc.tier, got, tc.want)
		}
	}
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
