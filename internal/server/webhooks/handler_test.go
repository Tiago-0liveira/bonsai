package webhooks

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	store "github.com/Tiago-0liveira/bonsai/internal/storage/git"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestSignatureDedupeAndRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db.json")
	db, _ := store.Open(path)
	secret := []byte("secret")
	body := `{"repository":{"id":1}}`
	var calls atomic.Int32
	processed := make(chan struct{}, 1)
	process := func(context.Context, Delivery) error { calls.Add(1); processed <- struct{}{}; return nil }
	h := New(secret, db, process)
	deliver := func(signature string) int {
		r := httptest.NewRequest("POST", "/webhooks/github", strings.NewReader(body))
		r.Header.Set("X-GitHub-Delivery", "delivery")
		r.Header.Set("X-GitHub-Event", "push")
		r.Header.Set("X-Hub-Signature-256", signature)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}
	if code := deliver("sha256=bad"); code != 401 {
		t.Fatal(code)
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(body))
	signature := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if code := deliver(signature); code != 202 {
		t.Fatal(code)
	}
	if calls.Load() != 0 {
		t.Fatal("processed in request path")
	}
	if code := deliver(signature); code != 200 {
		t.Fatal(code)
	}
	reopened, e := store.Open(path)
	if e != nil {
		t.Fatal(e)
	}
	h = New(secret, reopened, process)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go h.Run(ctx)
	select {
	case <-processed:
	case <-time.After(time.Second):
		t.Fatal("durable queue did not resume")
	}
	if calls.Load() != 1 {
		t.Fatal(calls.Load())
	}
}
