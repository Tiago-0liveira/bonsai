package webhooks

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	devRelayMaxBody      = 2 << 20
	devRelayMaxReplay    = 1024
	devRelaySubscriberQ  = 32
	devRelayHeartbeat    = 20 * time.Second
)

type DevRelayConfig struct {
	Address       string
	BrowserOrigin string
	SecretFile    string
}

type devRelayEvent struct {
	Sequence uint64 `json:"sequence"`
	Event
}

type devSubscriber struct {
	id uint64
	ch chan devRelayEvent
}

type DevRelay struct {
	expectedHost  string
	browserOrigin string
	secret        []byte

	mu       sync.Mutex
	seen     map[string]string
	events   []devRelayEvent
	sequence uint64
	nextSub  uint64
	subs     map[uint64]*devSubscriber
}

func NewDevRelay(cfg DevRelayConfig) (*DevRelay, error) {
	if err := requireDevLoopback(cfg.Address); err != nil {
		return nil, err
	}
	if err := requireDevBrowserOrigin(cfg.BrowserOrigin); err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(cfg.SecretFile)
	if err != nil {
		return nil, fmt.Errorf("read development webhook secret: %w", err)
	}
	secret, err := hex.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil || len(secret) == 0 {
		return nil, fmt.Errorf("invalid development webhook secret file")
	}
	return &DevRelay{
		expectedHost: cfg.Address,
		browserOrigin: cfg.BrowserOrigin,
		secret: append([]byte(nil), secret...),
		seen: map[string]string{},
		subs: map[uint64]*devSubscriber{},
	}, nil
}

func (s *DevRelay) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("POST /github/webhook", s.githubWebhook)
	mux.HandleFunc("GET /events", s.eventsStream)
	mux.HandleFunc("OPTIONS /events", s.eventsPreflight)
	return s.securityHeaders(mux)
}

func (s *DevRelay) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		// A developer tunnel is allowed to forward only webhook ingress. Every
		// browser-facing route remains exact-Host loopback-only.
		if r.URL.Path != "/github/webhook" && r.Host != s.expectedHost {
			http.Error(w, "invalid host", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *DevRelay) health(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, `{"ok":true}`)
}

func (s *DevRelay) githubWebhook(w http.ResponseWriter, r *http.Request) {
	if r.ContentLength > devRelayMaxBody {
		http.Error(w, "payload too large", http.StatusRequestEntityTooLarge)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, devRelayMaxBody)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			http.Error(w, "payload too large", http.StatusRequestEntityTooLarge)
		} else {
			http.Error(w, "invalid payload", http.StatusBadRequest)
		}
		return
	}
	signature := r.Header.Get("X-Hub-Signature-256")
	if len(signature) > 128 || !Verify(s.secret, body, signature) {
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
	sum := sha256.Sum256(body)
	digest := hex.EncodeToString(sum[:])

	s.mu.Lock()
	if prior, ok := s.seen[deliveryID]; ok {
		s.mu.Unlock()
		if prior != digest {
			http.Error(w, "delivery id reused with different payload", http.StatusConflict)
			return
		}
		w.WriteHeader(http.StatusOK)
		return
	}
	s.seen[deliveryID] = digest
	s.sequence++
	item := devRelayEvent{Sequence: s.sequence, Event: event}
	s.events = append(s.events, item)
	if len(s.events) > devRelayMaxReplay {
		s.events = append([]devRelayEvent(nil), s.events[len(s.events)-devRelayMaxReplay:]...)
	}
	var stale []uint64
	for id, sub := range s.subs {
		select {
		case sub.ch <- item:
		default:
			stale = append(stale, id)
		}
	}
	for _, id := range stale {
		if sub := s.subs[id]; sub != nil {
			delete(s.subs, id)
			close(sub.ch)
		}
	}
	s.mu.Unlock()
	w.WriteHeader(http.StatusAccepted)
}

func (s *DevRelay) eventsPreflight(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Origin") != s.browserOrigin ||
		r.Header.Get("Access-Control-Request-Method") != http.MethodGet {
		http.Error(w, "invalid preflight", http.StatusForbidden)
		return
	}
	s.setEventCORS(w)
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	w.Header().Set("Access-Control-Max-Age", "600")
	w.WriteHeader(http.StatusNoContent)
}

func (s *DevRelay) setEventCORS(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", s.browserOrigin)
	w.Header().Add("Vary", "Origin")
}

func (s *DevRelay) subscribe(after uint64) (*devSubscriber, []devRelayEvent, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if after > s.sequence {
		return nil, nil, false
	}
	if after != 0 && len(s.events) > 0 && after+1 < s.events[0].Sequence {
		return nil, nil, false
	}
	var replay []devRelayEvent
	for _, item := range s.events {
		if item.Sequence > after {
			replay = append(replay, item)
		}
	}
	s.nextSub++
	sub := &devSubscriber{id: s.nextSub, ch: make(chan devRelayEvent, devRelaySubscriberQ)}
	s.subs[sub.id] = sub
	return sub, replay, true
}

func (s *DevRelay) unsubscribe(sub *devSubscriber) {
	if sub == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if current := s.subs[sub.id]; current == sub {
		delete(s.subs, sub.id)
		close(sub.ch)
	}
}

func (s *DevRelay) eventsStream(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Origin") != s.browserOrigin {
		http.Error(w, "invalid origin", http.StatusForbidden)
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
	sub, replay, ok := s.subscribe(after)
	if !ok {
		s.setEventCORS(w)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: reset\ndata: {\"reason\":\"cursor_expired\"}\n\n")
		flusher.Flush()
		return
	}
	defer s.unsubscribe(sub)

	s.setEventCORS(w)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	write := func(item devRelayEvent) bool {
		body, err := json.Marshal(item.Event)
		if err != nil {
			return false
		}
		if _, err := fmt.Fprintf(w, "id: %d\ndata: %s\n\n", item.Sequence, body); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}
	for _, item := range replay {
		if !write(item) {
			return
		}
	}
	_, _ = io.WriteString(w, ": connected\n\n")
	flusher.Flush()

	ticker := time.NewTicker(devRelayHeartbeat)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case item, open := <-sub.ch:
			if !open || !write(item) {
				return
			}
		case <-ticker.C:
			_, _ = io.WriteString(w, ": heartbeat\n\n")
			flusher.Flush()
		}
	}
}

func RunDevRelay(cfg DevRelayConfig) error {
	relay, err := NewDevRelay(cfg)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	server := &http.Server{
		Addr: cfg.Address, Handler: relay.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout: 90 * time.Second,
		MaxHeaderBytes: 32 << 10,
	}
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_ = server.Shutdown(shutdown)
		case <-done:
		}
	}()
	err = server.ListenAndServe()
	close(done)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func requireDevLoopback(address string) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("invalid development relay address %q: %w", address, err)
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("development relay must bind to a loopback IP, got %q", host)
	}
	return nil
}

func requireDevBrowserOrigin(origin string) error {
	u, err := url.Parse(origin)
	if err != nil || u.Scheme != "http" || u.Host == "" || u.User != nil ||
		u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("development browser origin must be an explicit loopback HTTP origin")
	}
	host := u.Hostname()
	if host != "localhost" {
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			return fmt.Errorf("development browser origin must be loopback")
		}
	}
	if u.Port() == "" {
		return fmt.Errorf("development browser origin requires an explicit port")
	}
	return nil
}
