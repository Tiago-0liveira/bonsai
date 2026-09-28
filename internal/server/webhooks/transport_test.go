package webhooks

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	store "github.com/Tiago-0liveira/bonsai/internal/storage/git"
)

func githubSignature(secret []byte, body string) string {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(body))
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func TestIngressSecurityAndNormalization(t *testing.T) {
	secret := []byte("github-secret")
	body := `{"action":"synchronize","repository":{"id":123},"installation":{"id":456},"number":42,"pull_request":{"number":42}}`
	var calls atomic.Int32
	var forwarded Event
	ingress := NewIngress(secret, []RepositoryIdentity{{RepositoryID: 123, InstallationID: 456}}, func(_ context.Context, event Event) error {
		calls.Add(1)
		forwarded = event
		return nil
	})
	deliver := func(method, signature, eventName, delivery, payload string) int {
		req := httptest.NewRequest(method, "/github/webhook", strings.NewReader(payload))
		req.Header.Set("X-Hub-Signature-256", signature)
		req.Header.Set("X-GitHub-Event", eventName)
		req.Header.Set("X-GitHub-Delivery", delivery)
		w := httptest.NewRecorder()
		ingress.ServeHTTP(w, req)
		return w.Code
	}
	if got := deliver(http.MethodGet, "", "pull_request", "d1", body); got != http.StatusMethodNotAllowed {
		t.Fatalf("GET = %d", got)
	}
	if got := deliver(http.MethodPost, "sha256=bad", "pull_request", "d1", body); got != http.StatusUnauthorized {
		t.Fatalf("bad signature = %d", got)
	}
	sig := githubSignature(secret, body)
	if got := deliver(http.MethodPost, sig, "pull_request", "d1", body); got != http.StatusAccepted {
		t.Fatalf("valid = %d", got)
	}
	if forwarded.DeliveryID != "d1" || forwarded.PullRequestNumber != 42 || forwarded.RepositoryID != 123 {
		t.Fatalf("forwarded = %+v", forwarded)
	}
	if got := deliver(http.MethodPost, sig, "pull_request", "d1", body); got != http.StatusOK {
		t.Fatalf("duplicate = %d", got)
	}
	if calls.Load() != 1 {
		t.Fatalf("forward calls = %d", calls.Load())
	}

	unknownBody := `{"action":"synchronize","repository":{"id":999},"installation":{"id":456},"number":42}`
	if got := deliver(http.MethodPost, githubSignature(secret, unknownBody), "pull_request", "d2", unknownBody); got != http.StatusForbidden {
		t.Fatalf("unknown repository = %d", got)
	}
	if got := deliver(http.MethodPost, githubSignature(secret, body), "pull_request", "d3",
		strings.Replace(body, "synchronize", "totally_unknown", 1)); got == http.StatusAccepted {
		t.Fatal("unsupported action accepted")
	}
}

func TestInternalQueueAuthReplayAndDedupe(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "db.json"))
	if err != nil {
		t.Fatal(err)
	}
	secret := []byte("01234567890123456789012345678901")
	var processed atomic.Int32
	done := make(chan struct{}, 1)
	queue := NewInternalQueue(secret, db, func(_ context.Context, event Event) error {
		processed.Add(1)
		done <- struct{}{}
		return nil
	})
	event := Event{
		DeliveryID: "delivery", Event: "pull_request", Action: "synchronize",
		RepositoryID: 123, InstallationID: 456, PullRequestNumber: 42,
	}
	body, _ := json.Marshal(event)
	send := func(timestamp, signature string) int {
		req := httptest.NewRequest(http.MethodPost, "/internal/github-events", strings.NewReader(string(body)))
		req.Header.Set("X-Bonsai-Timestamp", timestamp)
		req.Header.Set("X-Bonsai-Delivery", event.DeliveryID)
		req.Header.Set("X-Bonsai-Signature", signature)
		w := httptest.NewRecorder()
		queue.ServeHTTP(w, req)
		return w.Code
	}
	now := time.Now().Unix()
	ts := fmt.Sprint(now)
	if got := send(ts, "sha256=bad"); got != http.StatusUnauthorized {
		t.Fatalf("bad internal signature = %d", got)
	}
	stale := fmt.Sprint(now - 600)
	if got := send(stale, SignInternal(secret, stale, event.DeliveryID, body)); got != http.StatusUnauthorized {
		t.Fatalf("stale = %d", got)
	}
	signature := SignInternal(secret, ts, event.DeliveryID, body)
	if got := send(ts, signature); got != http.StatusAccepted {
		t.Fatalf("accepted = %d", got)
	}
	if got := send(ts, signature); got != http.StatusAccepted {
		t.Fatalf("duplicate = %d", got)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go queue.Run(ctx)
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("normalized event was not processed")
	}
	time.Sleep(30 * time.Millisecond)
	if processed.Load() != 1 {
		t.Fatalf("processed = %d", processed.Load())
	}
}
