// Package webhooks verifies and durably queues GitHub deliveries before replying.
package webhooks

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	store "github.com/Tiago-0liveira/bonsai/internal/storage/git"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

type Delivery struct {
	ID          string          `json:"id"`
	Event       string          `json:"event"`
	Payload     json.RawMessage `json:"payload"`
	ReceivedAt  time.Time       `json:"received_at"`
	ProcessedAt *time.Time      `json:"processed_at,omitempty"`
	Status      string          `json:"status"`
	Attempts    int             `json:"attempts"`
	NextAttempt time.Time       `json:"next_attempt"`
	Error       string          `json:"error,omitempty"`
}
type Handler struct {
	Secret  []byte
	Store   *store.Store
	Process func(context.Context, Delivery) error
	wake    chan struct{}
}

func New(secret []byte, st *store.Store, process func(context.Context, Delivery) error) *Handler {
	return &Handler{Secret: secret, Store: st, Process: process, wake: make(chan struct{}, 1)}
}
func Sign(secret, body []byte) string {
	h := hmac.New(sha256.New, secret)
	h.Write(body)
	return "sha256=" + hex.EncodeToString(h.Sum(nil))
}

func Verify(secret, body []byte, signature string) bool {
	if len(secret) == 0 || !strings.HasPrefix(signature, "sha256=") {
		return false
	}
	given, e := hex.DecodeString(strings.TrimPrefix(signature, "sha256="))
	if e != nil {
		return false
	}
	expected, _ := hex.DecodeString(strings.TrimPrefix(Sign(secret, body), "sha256="))
	return hmac.Equal(given, expected)
}
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		w.WriteHeader(405)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 25<<20)
	body, e := io.ReadAll(r.Body)
	if e != nil {
		http.Error(w, "payload too large", 413)
		return
	}
	if !Verify(h.Secret, body, r.Header.Get("X-Hub-Signature-256")) {
		http.Error(w, "invalid signature", 401)
		return
	}
	id := r.Header.Get("X-GitHub-Delivery")
	event := r.Header.Get("X-GitHub-Event")
	if id == "" || len(id) > 128 || event == "" || len(event) > 100 || !json.Valid(body) {
		http.Error(w, "invalid delivery", 400)
		return
	}
	duplicate := false
	e = h.Store.Update(func(d store.Data) error {
		if _, ok := d["webhook_deliveries"][id]; ok {
			duplicate = true
			return nil
		}
		return store.Put(d, "webhook_deliveries", id, Delivery{ID: id, Event: event, Payload: body, ReceivedAt: time.Now().UTC(), Status: "pending"})
	})
	if e != nil {
		http.Error(w, "queue unavailable", 503)
		return
	}
	select {
	case h.wake <- struct{}{}:
	default:
	}
	if duplicate {
		w.WriteHeader(200)
	} else {
		w.WriteHeader(202)
	}
}

// Run retries failed and interrupted deliveries. A single worker preserves
// receipt order; handlers reconcile against GitHub so delayed hooks cannot
// roll remote state backward.
func (h *Handler) Run(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		var pending []Delivery
		h.Store.View(func(d store.Data) error {
			for key := range d["webhook_deliveries"] {
				v, ok := store.Get[Delivery](d, "webhook_deliveries", key)
				if ok && v.Status != "processed" && !v.NextAttempt.After(time.Now()) {
					pending = append(pending, v)
				}
			}
			return nil
		})
		sort.Slice(pending, func(i, j int) bool { return pending[i].ReceivedAt.Before(pending[j].ReceivedAt) })
		for _, v := range pending {
			if ctx.Err() != nil {
				return
			}
			v.Attempts++
			work, cancel := context.WithTimeout(ctx, 2*time.Minute)
			e := h.Process(work, v)
			cancel()
			if e != nil {
				v.Status = "failed"
				v.Error = e.Error()
				delay := time.Second * time.Duration(1<<min(v.Attempts, 10))
				v.NextAttempt = time.Now().Add(delay)
			} else {
				now := time.Now().UTC()
				v.Status = "processed"
				v.ProcessedAt = &now
				v.Payload = nil
				v.Error = ""
			}
			_ = h.Store.Update(func(d store.Data) error { return store.Put(d, "webhook_deliveries", v.ID, v) })
		}
		select {
		case <-ctx.Done():
			return
		case <-h.wake:
		case <-ticker.C:
		}
	}
}
