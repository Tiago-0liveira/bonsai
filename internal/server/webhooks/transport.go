package webhooks

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"sync"
	"time"

	store "github.com/Tiago-0liveira/bonsai/internal/storage/git"
)

const (
	maxIngressBody  = 2 << 20
	maxInternalBody = 64 << 10
)

type RepositoryIdentity struct {
	RepositoryID   int64
	InstallationID int64
}

// Ingress is the intentionally small internet-facing webhook boundary.
type Ingress struct {
	Secret       []byte
	Repositories []RepositoryIdentity
	Forward      func(context.Context, Event) error

	mu    sync.Mutex
	seen  map[string]string
	order []string
}

func NewIngress(secret []byte, repositories []RepositoryIdentity, forward func(context.Context, Event) error) *Ingress {
	return &Ingress{
		Secret:       append([]byte(nil), secret...),
		Repositories: append([]RepositoryIdentity(nil), repositories...),
		Forward:      forward,
		seen:         map[string]string{},
	}
}

func (h *Ingress) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxIngressBody)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "payload too large", http.StatusRequestEntityTooLarge)
		return
	}
	// Signature validation always precedes JSON parsing.
	if !Verify(h.Secret, body, r.Header.Get("X-Hub-Signature-256")) {
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}
	deliveryID := r.Header.Get("X-GitHub-Delivery")
	eventName := r.Header.Get("X-GitHub-Event")
	if deliveryID == "" || len(deliveryID) > 128 || eventName == "" || len(eventName) > 100 {
		http.Error(w, "invalid delivery", http.StatusBadRequest)
		return
	}
	event, err := Normalize(deliveryID, eventName, body)
	if err != nil {
		http.Error(w, "unsupported or invalid webhook", http.StatusBadRequest)
		return
	}
	if !h.allowedRepository(event) {
		http.Error(w, "repository or installation not allowed", http.StatusForbidden)
		return
	}

	digestBytes := sha256.Sum256(body)
	digest := hex.EncodeToString(digestBytes[:])
	h.mu.Lock()
	prior, duplicate := h.seen[deliveryID]
	h.mu.Unlock()
	if duplicate {
		if prior != digest {
			http.Error(w, "delivery id reused with different payload", http.StatusConflict)
			return
		}
		w.WriteHeader(http.StatusOK)
		return
	}
	if h.Forward == nil {
		http.Error(w, "webhook transport unavailable", http.StatusServiceUnavailable)
		return
	}
	if err := h.Forward(r.Context(), event); err != nil {
		http.Error(w, "webhook transport unavailable", http.StatusServiceUnavailable)
		return
	}

	h.mu.Lock()
	h.seen[deliveryID] = digest
	h.order = append(h.order, deliveryID)
	if len(h.order) > 4096 {
		old := h.order[0]
		h.order = h.order[1:]
		delete(h.seen, old)
	}
	h.mu.Unlock()
	w.WriteHeader(http.StatusAccepted)
}

func (h *Ingress) allowedRepository(event Event) bool {
	for _, repo := range h.Repositories {
		if event.Event == "installation" || event.Event == "installation_repositories" {
			if event.InstallationID == repo.InstallationID && event.InstallationID != 0 {
				return true
			}
			continue
		}
		if event.RepositoryID == repo.RepositoryID && event.InstallationID == repo.InstallationID &&
			event.RepositoryID != 0 && event.InstallationID != 0 {
			return true
		}
	}
	return false
}

type InternalDelivery struct {
	Event       Event      `json:"event"`
	Digest      string     `json:"digest"`
	ReceivedAt  time.Time  `json:"received_at"`
	ProcessedAt *time.Time `json:"processed_at,omitempty"`
	Status      string     `json:"status"`
	Attempts    int        `json:"attempts"`
	NextAttempt time.Time  `json:"next_attempt,omitempty"`
	Error       string     `json:"error,omitempty"`
}

// InternalQueue authenticates webhook->API messages and durably deduplicates
// normalized events before handing them to Bonsai business logic.
type InternalQueue struct {
	Secret  []byte
	Store   *store.Store
	Process func(context.Context, Event) error
	wake    chan struct{}
}

func NewInternalQueue(secret []byte, st *store.Store, process func(context.Context, Event) error) *InternalQueue {
	return &InternalQueue{
		Secret:  append([]byte(nil), secret...),
		Store:   st,
		Process: process,
		wake:    make(chan struct{}, 1),
	}
}

func SignInternal(secret []byte, timestamp, delivery string, body []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(timestamp))
	mac.Write([]byte("\n"))
	mac.Write([]byte(delivery))
	mac.Write([]byte("\n"))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func VerifyInternal(secret []byte, timestamp, delivery string, body []byte, signature string) bool {
	if len(secret) == 0 || !stringsHasPrefix(signature, "sha256=") {
		return false
	}
	given, err := hex.DecodeString(signature[len("sha256="):])
	if err != nil {
		return false
	}
	expectedHex := SignInternal(secret, timestamp, delivery, body)
	expected, _ := hex.DecodeString(expectedHex[len("sha256="):])
	return hmac.Equal(given, expected)
}

func (q *InternalQueue) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxInternalBody)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "payload too large", http.StatusRequestEntityTooLarge)
		return
	}
	timestamp := r.Header.Get("X-Bonsai-Timestamp")
	delivery := r.Header.Get("X-Bonsai-Delivery")
	signature := r.Header.Get("X-Bonsai-Signature")
	unix, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil || time.Since(time.Unix(unix, 0)) > 5*time.Minute || time.Until(time.Unix(unix, 0)) > 5*time.Minute {
		http.Error(w, "stale internal request", http.StatusUnauthorized)
		return
	}
	if delivery == "" || len(delivery) > 128 || !VerifyInternal(q.Secret, timestamp, delivery, body, signature) {
		http.Error(w, "invalid internal signature", http.StatusUnauthorized)
		return
	}
	var event Event
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&event); err != nil || event.DeliveryID != delivery || !AllowedEventAction(event.Event, event.Action) {
		http.Error(w, "invalid internal event", http.StatusBadRequest)
		return
	}
	if decoder.Decode(new(any)) != io.EOF {
		http.Error(w, "invalid internal event", http.StatusBadRequest)
		return
	}
	digestBytes := sha256.Sum256(body)
	digest := hex.EncodeToString(digestBytes[:])
	duplicate := false
	conflict := false
	err = q.Store.Update(func(data store.Data) error {
		if prior, ok := store.Get[InternalDelivery](data, "internal_github_events", delivery); ok {
			if prior.Digest != digest {
				conflict = true
			} else {
				duplicate = true
			}
			return nil
		}
		return store.Put(data, "internal_github_events", delivery, InternalDelivery{
			Event: event, Digest: digest, ReceivedAt: time.Now().UTC(), Status: "pending",
		})
	})
	if err != nil {
		http.Error(w, "internal queue unavailable", http.StatusServiceUnavailable)
		return
	}
	if conflict {
		http.Error(w, "delivery id reused with different event", http.StatusConflict)
		return
	}
	if !duplicate {
		select {
		case q.wake <- struct{}{}:
		default:
		}
	}
	w.WriteHeader(http.StatusAccepted)
}

func (q *InternalQueue) Run(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		var pending []InternalDelivery
		_ = q.Store.View(func(data store.Data) error {
			for key := range data["internal_github_events"] {
				item, ok := store.Get[InternalDelivery](data, "internal_github_events", key)
				if ok && item.Status != "processed" && !item.NextAttempt.After(time.Now()) {
					pending = append(pending, item)
				}
			}
			return nil
		})
		sort.Slice(pending, func(i, j int) bool { return pending[i].ReceivedAt.Before(pending[j].ReceivedAt) })
		for _, item := range pending {
			if ctx.Err() != nil {
				return
			}
			item.Attempts++
			work, cancel := context.WithTimeout(ctx, 2*time.Minute)
			err := q.Process(work, item.Event)
			cancel()
			if err != nil {
				item.Status = "failed"
				item.Error = err.Error()
				item.NextAttempt = time.Now().Add(time.Second * time.Duration(1<<min(item.Attempts, 10)))
			} else {
				now := time.Now().UTC()
				item.Status = "processed"
				item.ProcessedAt = &now
				item.Error = ""
			}
			_ = q.Store.Update(func(data store.Data) error {
				return store.Put(data, "internal_github_events", item.Event.DeliveryID, item)
			})
		}
		select {
		case <-ctx.Done():
			return
		case <-q.wake:
		case <-ticker.C:
		}
	}
}

func stringsHasPrefix(value, prefix string) bool {
	return len(value) >= len(prefix) && value[:len(prefix)] == prefix
}

func ForwardEvent(ctx context.Context, client *http.Client, url string, secret []byte, event Event) error {
	body, err := json.Marshal(event)
	if err != nil {
		return err
	}
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Bonsai-Timestamp", timestamp)
	req.Header.Set("X-Bonsai-Delivery", event.DeliveryID)
	req.Header.Set("X-Bonsai-Signature", SignInternal(secret, timestamp, event.DeliveryID, body))
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, resp.Body)
		return fmt.Errorf("internal API returned %s", resp.Status)
	}
	return nil
}
