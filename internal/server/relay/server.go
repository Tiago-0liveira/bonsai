package relay

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const (
	ProductionFrontendOrigin = "https://app.bonsai.dev"
	ProductionExternalURL = "https://api.bonsai.dev"
)

type Config struct {
	Address            string
	Database           string
	ExternalURL        string
	FrontendOrigin     string
	GitHubClientID     string
	GitHubClientSecret string
	WebhookSecret      string
	HTTPClient         *http.Client
}

type Server struct {
	store              *Store
	hub                *eventHub
	httpClient         *http.Client
	externalURL        string
	frontendOrigin     string
	githubClientID     string
	githubClientSecret string
	webhookSecret      []byte
	heartbeat          time.Duration
}

func NewServer(cfg Config) (*Server, error) {
	if cfg.Database == "" {
		return nil, errors.New("relay database path is required")
	}
	if cfg.ExternalURL == "" {
		cfg.ExternalURL = ProductionExternalURL
	}
	cfg.ExternalURL = strings.TrimRight(cfg.ExternalURL, "/")
	if !strings.HasPrefix(cfg.ExternalURL, "https://") {
		return nil, errors.New("relay external URL must use HTTPS")
	}
	if cfg.FrontendOrigin == "" {
		cfg.FrontendOrigin = ProductionFrontendOrigin
	}
	if cfg.FrontendOrigin != ProductionFrontendOrigin {
		return nil, fmt.Errorf("relay frontend origin must be exactly %s", ProductionFrontendOrigin)
	}
	if cfg.GitHubClientID == "" || cfg.GitHubClientSecret == "" || cfg.WebhookSecret == "" {
		return nil, errors.New("GitHub OAuth credentials and webhook secret are required")
	}
	st, err := OpenStore(cfg.Database)
	if err != nil {
		return nil, err
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &Server{
		store: st, hub: newEventHub(), httpClient: client,
		externalURL: cfg.ExternalURL, frontendOrigin: cfg.FrontendOrigin,
		githubClientID: cfg.GitHubClientID, githubClientSecret: cfg.GitHubClientSecret,
		webhookSecret: []byte(cfg.WebhookSecret), heartbeat: 20 * time.Second,
	}, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	mux.HandleFunc("GET /auth/github", s.authLogin)
	mux.HandleFunc("GET /auth/github/callback", s.authCallback)
	mux.HandleFunc("GET /auth/session", s.authSession)
	mux.HandleFunc("POST /auth/logout", s.authLogout)
	mux.HandleFunc("POST /webhooks/github", s.githubWebhook)
	mux.HandleFunc("GET /events", s.events)
	mux.HandleFunc("OPTIONS /events", s.preflight)
	mux.HandleFunc("OPTIONS /auth/session", s.preflight)
	mux.HandleFunc("OPTIONS /auth/logout", s.preflight)
	return s.securityHeaders(mux)
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if r.URL.Path != "/events" {
			w.Header().Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) preflight(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Origin") != s.frontendOrigin {
		http.Error(w, "invalid origin", http.StatusForbidden)
		return
	}
	requested := r.Header.Get("Access-Control-Request-Method")
	if requested != http.MethodGet && requested != http.MethodPost {
		http.Error(w, "invalid preflight", http.StatusForbidden)
		return
	}
	w.Header().Set("Access-Control-Allow-Origin", s.frontendOrigin)
	w.Header().Set("Access-Control-Allow-Credentials", "true")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	w.Header().Set("Access-Control-Max-Age", "600")
	w.Header().Add("Vary", "Origin")
	w.WriteHeader(http.StatusNoContent)
}

func Run(ctx context.Context, cfg Config) error {
	s, err := NewServer(cfg)
	if err != nil {
		return err
	}
	if cfg.Address == "" {
		cfg.Address = "127.0.0.1:8080"
	}
	httpServer := &http.Server{
		Addr: cfg.Address, Handler: s.Handler(),
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
			_ = httpServer.Shutdown(shutdown)
		case <-done:
		}
	}()
	err = httpServer.ListenAndServe()
	close(done)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
