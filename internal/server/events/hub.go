package events

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	store "github.com/Tiago-0liveira/bonsai/internal/storage/git"
	"net/http"
	"sort"
	"strconv"
	"sync"
	"time"
)

type Event struct {
	ID          string          `json:"id"`
	Sequence    uint64          `json:"sequence"`
	WorkspaceID string          `json:"workspace_id"`
	ProjectID   string          `json:"project_id"`
	Source      string          `json:"source"`
	Type        string          `json:"type"`
	EntityID    string          `json:"entity_id"`
	Revision    uint64          `json:"revision"`
	Payload     json.RawMessage `json:"payload"`
	CreatedAt   time.Time       `json:"created_at"`
}
type Hub struct {
	Store  *store.Store
	mu     sync.Mutex
	notify chan struct{}
}

func New(s *store.Store) *Hub { return &Hub{Store: s, notify: make(chan struct{})} }

// Publish IDs are idempotency keys shared across webhook/daemon retries.
func (h *Hub) Publish(e Event) (Event, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if e.ID == "" {
		e.ID = rand.Text()
	}
	err := h.Store.Update(func(d store.Data) error {
		if old, ok := store.Get[Event](d, "event_ids", e.ID); ok {
			e = old
			return nil
		}
		seq, _ := store.Get[uint64](d, "meta", "sequence")
		e.Sequence = seq + 1
		e.Revision = e.Sequence
		e.CreatedAt = time.Now().UTC()
		if err := store.Put(d, "meta", "sequence", e.Sequence); err != nil {
			return err
		}
		store.Put(d, "events", strconv.FormatUint(e.Sequence, 10), e)
		store.Put(d, "event_ids", e.ID, e)
		if e.Sequence > 10000 {
			key := strconv.FormatUint(e.Sequence-10000, 10)
			old, _ := store.Get[Event](d, "events", key)
			delete(d["events"], key)
			delete(d["event_ids"], old.ID)
		}
		return nil
	})
	if err == nil {
		close(h.notify)
		h.notify = make(chan struct{})
	}
	return e, err
}
func (h *Hub) Cursor() uint64 {
	var n uint64
	h.Store.View(func(d store.Data) error { n, _ = store.Get[uint64](d, "meta", "sequence"); return nil })
	return n
}
func (h *Hub) Replay(after uint64, allowed func(Event) bool) ([]Event, bool, error) {
	result := []Event{}
	reset := false
	e := h.Store.View(func(d store.Data) error {
		seq, _ := store.Get[uint64](d, "meta", "sequence")
		reset = after > seq || (seq > 10000 && after < seq-10000)
		for k, v := range d["events"] {
			n, _ := strconv.ParseUint(k, 10, 64)
			if n <= after {
				continue
			}
			var e Event
			if json.Unmarshal(v, &e) != nil {
				continue
			}
			if allowed(e) {
				result = append(result, e)
			}
		}
		return nil
	})
	sort.Slice(result, func(i, j int) bool { return result[i].Sequence < result[j].Sequence })
	return result, reset, e
}
func (h *Hub) Wait(ctx context.Context) {
	h.mu.Lock()
	ch := h.notify
	h.mu.Unlock()
	select {
	case <-ctx.Done():
	case <-ch:
	case <-time.After(15 * time.Second):
	}
}

// Stream serves browser-only SSE. The separate daemon bridge is bidirectional WS.
// Authorization must be checked by the caller and again for each event.
func (h *Hub) Stream(w http.ResponseWriter, r *http.Request, allowed func(Event) bool) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "stream unsupported", 500)
		return
	}
	after, _ := strconv.ParseUint(r.URL.Query().Get("after"), 10, 64)
	if v := r.Header.Get("Last-Event-ID"); v != "" {
		after, _ = strconv.ParseUint(v, 10, 64)
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	for r.Context().Err() == nil {
		h.mu.Lock()
		changed := h.notify
		h.mu.Unlock()
		items, reset, e := h.Replay(after, allowed)
		if e != nil {
			return
		}
		if reset {
			fmt.Fprint(w, "event: reset\ndata: {}\n\n")
			flusher.Flush()
			return
		}
		for _, v := range items {
			b, _ := json.Marshal(v)
			if _, e = fmt.Fprintf(w, "id: %d\nevent: git\ndata: %s\n\n", v.Sequence, b); e != nil {
				return
			}
			after = v.Sequence
		}
		fmt.Fprint(w, ": heartbeat\n\n")
		flusher.Flush()
		select {
		case <-r.Context().Done():
			return
		case <-changed:
		case <-time.After(15 * time.Second):
		}
	}
}
