package webhooks

import (
	"bytes"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func newDevRelayForTest(t *testing.T) (*DevRelay, []byte) {
	t.Helper()
	secret := []byte("development-webhook-secret")
	path := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(path, []byte(hex.EncodeToString(secret)), 0o600); err != nil {
		t.Fatal(err)
	}
	relay, err := NewDevRelay(DevRelayConfig{
		Address:       "127.0.0.1:7002",
		BrowserOrigin: "http://127.0.0.1:7003",
		SecretFile:    path,
	})
	if err != nil {
		t.Fatal(err)
	}
	return relay, secret
}

func devWebhookRequest(t *testing.T, relay *DevRelay, secret []byte, delivery, event string, body []byte, signed bool) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:7002/github/webhook", bytes.NewReader(body))
	req.Host = "example-tunnel.invalid"
	req.Header.Set("X-GitHub-Delivery", delivery)
	req.Header.Set("X-GitHub-Event", event)
	if signed {
		req.Header.Set("X-Hub-Signature-256", Sign(secret, body))
	} else {
		req.Header.Set("X-Hub-Signature-256", "sha256=bad")
	}
	rec := httptest.NewRecorder()
	relay.Handler().ServeHTTP(rec, req)
	return rec
}

func TestDevRelayVerifiesNormalizesAndDedupes(t *testing.T) {
	relay, secret := newDevRelayForTest(t)
	body := []byte("{\"action\":\"synchronize\",\"number\":42,\"pull_request\":{\"number\":42},\"repository\":{\"id\":123},\"installation\":{\"id\":456}}")

	if got := devWebhookRequest(t, relay, secret, "delivery-1", "pull_request", body, false).Code; got != http.StatusUnauthorized {
		t.Fatalf("invalid HMAC status = %d", got)
	}
	if got := devWebhookRequest(t, relay, secret, "delivery-1", "pull_request", body, true).Code; got != http.StatusAccepted {
		t.Fatalf("valid webhook status = %d", got)
	}
	if relay.sequence != 1 || len(relay.events) != 1 {
		t.Fatalf("published events = %d sequence=%d", len(relay.events), relay.sequence)
	}
	event := relay.events[0].Event
	if event.Event != "pull_request" || event.Action != "synchronize" || event.RepositoryID != 123 {
		t.Fatalf("normalized event = %#v", event)
	}
	if got := devWebhookRequest(t, relay, secret, "delivery-1", "pull_request", body, true).Code; got != http.StatusOK {
		t.Fatalf("duplicate status = %d", got)
	}
	conflict := []byte("{\"action\":\"opened\",\"number\":42,\"pull_request\":{\"number\":42},\"repository\":{\"id\":123},\"installation\":{\"id\":456}}")
	if got := devWebhookRequest(t, relay, secret, "delivery-1", "pull_request", conflict, true).Code; got != http.StatusConflict {
		t.Fatalf("delivery conflict status = %d", got)
	}
	unknown := []byte("{\"action\":\"not-real\",\"repository\":{\"id\":123},\"installation\":{\"id\":456}}")
	if got := devWebhookRequest(t, relay, secret, "delivery-2", "pull_request", unknown, true).Code; got != http.StatusBadRequest {
		t.Fatalf("unknown action status = %d", got)
	}
}

func TestDevRelayBoundsAndBrowserSecurity(t *testing.T) {
	relay, secret := newDevRelayForTest(t)
	oversized := bytes.Repeat([]byte("x"), devRelayMaxBody+1)
	if got := devWebhookRequest(t, relay, secret, "too-large", "push", oversized, true).Code; got != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized status = %d", got)
	}

	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:7002/events", nil)
	req.Host = "127.0.0.1:7002"
	req.Header.Set("Origin", "http://evil.example")
	rec := httptest.NewRecorder()
	relay.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("wrong Origin status = %d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "http://127.0.0.1:7002/healthz", nil)
	req.Host = "localhost:7002"
	rec = httptest.NewRecorder()
	relay.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("wrong Host status = %d", rec.Code)
	}

	for _, address := range []string{"0.0.0.0:7002", "192.168.1.10:7002", "localhost:7002"} {
		if err := requireDevLoopback(address); err == nil {
			t.Fatalf("development relay accepted address %q", address)
		}
	}
	if err := requireDevBrowserOrigin("http://127.0.0.1:7003"); err != nil {
		t.Fatal(err)
	}
	if err := requireDevBrowserOrigin("https://127.0.0.1:7003"); err == nil {
		t.Fatal("development relay accepted HTTPS browser origin")
	}
}
