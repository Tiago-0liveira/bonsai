package localapi

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
	"github.com/Tiago-0liveira/bonsai/internal/daemon/gitbridge"
	gitstore "github.com/Tiago-0liveira/bonsai/internal/storage/git"
	"github.com/Tiago-0liveira/bonsai/internal/version"
)

const (
	localRepositoryID  = "local"
	localBrowserUserID = "local-browser"
)

type daemonClient interface {
	Git(gitbridge.Command) (*gitbridge.Result, error)
	List() ([]*procstore.Record, error)
	Restart(int) (*procstore.Record, error)
	Kill(int, bool, string) ([]int, error)
	Logs(int, bool, int, string, bool, func(string) error) error
}

type Config struct {
	RepoDir       string
	Address       string
	BrowserOrigin string
	Development   bool
}

type Server struct {
	repoDir       string
	expectedHost  string
	browserOrigin string
	development   bool
	registry      projectRegistry
	sessions      *sessionStore
	state         *gitstore.Store
	eventHub      *eventHub
	sequence      atomic.Uint64
}

func New(cfg Config) (*Server, error) {
	if cfg.RepoDir == "" {
		return nil, fmt.Errorf("repository root required")
	}
	if err := requireLoopback(cfg.Address); err != nil {
		return nil, err
	}
	if err := validateBrowserOrigin(cfg.BrowserOrigin, cfg.Development); err != nil {
		return nil, err
	}
	state, err := gitstore.Open(filepath.Join(procstore.New(cfg.RepoDir).Dir(), "local-api-state.json"))
	if err != nil {
		return nil, fmt.Errorf("open local API state: %w", err)
	}
	return &Server{
		repoDir:       cfg.RepoDir,
		expectedHost:  cfg.Address,
		browserOrigin: cfg.BrowserOrigin,
		development:   cfg.Development,
		registry:      newStaticProjectRegistry(cfg.RepoDir),
		sessions:      newSessionStore(),
		state:         state,
		eventHub:      newEventHub(),
	}, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.health)
	mux.HandleFunc("GET /version", s.version)
	mux.HandleFunc("POST /api/session", s.createSession)
	s.registerProjectRoutes(mux)
	s.registerGitRoutes(mux)
	s.registerProcessRoutes(mux)
	s.registerGitHubRoutes(mux)
	mux.HandleFunc("GET /events", s.events)
	return s.securityMiddleware(mux)
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) version(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"version": version.String()})
}

func (s *Server) createSession(w http.ResponseWriter, _ *http.Request) {
	session, err := s.sessions.create()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "session_unavailable", "failed to create local session")
		return
	}
	writeJSON(w, http.StatusCreated, session)
}

func Run(cfg Config) error {
	s, err := New(cfg)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	server := &http.Server{
		Addr:              cfg.Address,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       90 * time.Second,
		MaxHeaderBytes:    32 << 10,
	}
	go shutdownWithContext(ctx, server)
	log.Printf("Bonsai local API listening at http://%s for %s", cfg.Address, cfg.BrowserOrigin)
	err = server.ListenAndServe()
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

func shutdownWithContext(ctx context.Context, server *http.Server) {
	<-ctx.Done()
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdown)
}

func writeAPIError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]string{"code": code, "message": message},
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
