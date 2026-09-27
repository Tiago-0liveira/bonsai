package relay

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"
)

const (
	relayCookieName       = "bonsai_relay_session"
	maxSessionSubscribers = 8
	subscriberQueueSize   = 16
)

type subscriber struct {
	id      uint64
	session string
	allows  func(RelayEvent) bool
	ch      chan RelayEvent
	closed  bool
}

type eventHub struct {
	mu         sync.Mutex
	nextID     uint64
	subs       map[uint64]*subscriber
	perSession map[string]int
}

func newEventHub() *eventHub {
	return &eventHub{subs: map[uint64]*subscriber{}, perSession: map[string]int{}}
}

func (h *eventHub) subscribe(sessionKey string, allows func(RelayEvent) bool) (*subscriber, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.perSession[sessionKey] >= maxSessionSubscribers {
		return nil, false
	}
	h.nextID++
	sub := &subscriber{id: h.nextID, session: sessionKey, allows: allows, ch: make(chan RelayEvent, subscriberQueueSize)}
	h.subs[sub.id] = sub
	h.perSession[sessionKey]++
	return sub, true
}

func (h *eventHub) removeLocked(sub *subscriber) {
	if sub == nil || sub.closed {
		return
	}
	sub.closed = true
	delete(h.subs, sub.id)
	if h.perSession[sub.session] <= 1 {
		delete(h.perSession, sub.session)
	} else {
		h.perSession[sub.session]--
	}
	close(sub.ch)
}

func (h *eventHub) unsubscribe(sub *subscriber) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.removeLocked(sub)
}

func (h *eventHub) closeSession(sessionKey string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, sub := range h.subs {
		if sub.session == sessionKey {
			h.removeLocked(sub)
		}
	}
}

func (h *eventHub) publish(event RelayEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, sub := range h.subs {
		if !sub.allows(event) {
			continue
		}
		select {
		case sub.ch <- event:
		default:
			h.removeLocked(sub)
		}
	}
}

func (s *Server) authenticatedSession(r *http.Request) (string, RelaySession, bool) {
	cookie, err := r.Cookie(relayCookieName)
	if err != nil || cookie.Value == "" {
		return "", RelaySession{}, false
	}
	key := SecretHash(cookie.Value)
	session, ok := s.store.Session(key, time.Now().UTC())
	return key, session, ok
}

func writeSSE(w http.ResponseWriter, event RelayEvent) error {
	body, err := json.Marshal(event)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "id: %d\ndata: %s\n\n", event.Sequence, body)
	return err
}

func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if origin := r.Header.Get("Origin"); origin != "" && origin != s.frontendOrigin {
		http.Error(w, "invalid origin", http.StatusForbidden)
		return
	}
	sessionKey, session, ok := s.authenticatedSession(r)
	if !ok {
		http.Error(w, "authentication required", http.StatusUnauthorized)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	after := uint64(0)
	if raw := r.Header.Get("Last-Event-ID"); raw != "" {
		var err error
		after, err = strconv.ParseUint(raw, 10, 64)
		if err != nil {
			http.Error(w, "invalid event cursor", http.StatusBadRequest)
			return
		}
	}
	allows := func(event RelayEvent) bool { return sessionAllows(session, event) }
	sub, ok := s.hub.subscribe(sessionKey, allows)
	if !ok {
		http.Error(w, "too many event streams", http.StatusTooManyRequests)
		return
	}
	defer s.hub.unsubscribe(sub)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("Access-Control-Allow-Origin", s.frontendOrigin)
	w.Header().Set("Access-Control-Allow-Credentials", "true")
	w.Header().Add("Vary", "Origin")

	items, reset := s.store.Replay(after, session)
	if reset {
		_, _ = fmt.Fprint(w, "event: reset\ndata: {\"reason\":\"cursor_expired\"}\n\n")
		flusher.Flush()
		return
	}
	for _, event := range items {
		if err := writeSSE(w, event); err != nil {
			return
		}
		after = event.Sequence
	}
	_, _ = fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()

	ticker := time.NewTicker(s.heartbeat)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case event, open := <-sub.ch:
			if !open {
				return
			}
			if event.Sequence <= after {
				continue
			}
			if _, current, valid := s.authenticatedSession(r); !valid || current.GitHubUserID != session.GitHubUserID {
				return
			}
			if err := writeSSE(w, event); err != nil {
				return
			}
			after = event.Sequence
			flusher.Flush()
		case <-ticker.C:
			if _, current, valid := s.authenticatedSession(r); !valid || current.GitHubUserID != session.GitHubUserID {
				return
			}
			_, _ = fmt.Fprint(w, ": heartbeat\n\n")
			flusher.Flush()
		}
	}
}
