package localapi

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
	"github.com/Tiago-0liveira/bonsai/internal/daemon/gitbridge"
	git "github.com/Tiago-0liveira/bonsai/internal/git/local"
	gitstore "github.com/Tiago-0liveira/bonsai/internal/storage/git"
	"github.com/Tiago-0liveira/bonsai/internal/version"
)

const (
	localRepositoryID  = "local"
	localBrowserUserID = "local-browser"
	localAPIVersion    = 3
)

type daemonClient interface {
	Git(gitbridge.Command) (*gitbridge.Result, error)
	List() ([]*procstore.Record, error)
	Restart(int) (*procstore.Record, error)
	Kill(int, bool, string) ([]int, error)
	Logs(int, bool, int, string, bool, func(string) error) error
}

type Config struct {
	RepoDir          string
	Address          string
	BrowserOrigin    string
	SecurityMode     BrowserSecurityMode
	ProjectRootsPath string
}

type Server struct {
	repoDir       string
	expectedHost  string
	browserOrigin string
	securityMode  BrowserSecurityMode
	registry      projectRegistry
	sessions      *sessionStore
	state         *gitstore.Store
	eventHub      *eventHub
	stateSync     *stateSync
	rootsPath     string
}

func New(cfg Config) (*Server, error) {
	if cfg.RepoDir == "" {
		return nil, fmt.Errorf("repository root required")
	}
	// Daemon runtime paths are keyed by the main worktree root. A local API may
	// be launched from a linked worktree, so canonicalize before constructing
	// procstore/client state; otherwise it waits on a socket the real daemon
	// never owns.
	if root, err := git.MainRoot(cfg.RepoDir); err == nil {
		cfg.RepoDir = root
	}
	if canonical, err := config.CanonicalDirectory(cfg.RepoDir); err == nil {
		cfg.RepoDir = canonical
	}
	if err := requireLoopback(cfg.Address); err != nil {
		return nil, err
	}
	if cfg.SecurityMode == "" {
		cfg.SecurityMode = BrowserSecurityProduction
	}
	if err := validateBrowserOrigin(cfg.BrowserOrigin, cfg.SecurityMode); err != nil {
		return nil, err
	}
	var err error
	rootsPath := cfg.ProjectRootsPath
	if rootsPath == "" {
		rootsPath, err = config.ProjectRootsPath()
		if err != nil {
			return nil, err
		}
	}
	registry := newProjectRegistry(rootsPath, cfg.RepoDir)
	if _, err := registry.Refresh(context.Background()); err != nil {
		return nil, err
	}
	events := newEventHub()
	s := &Server{
		repoDir:       cfg.RepoDir,
		expectedHost:  cfg.Address,
		browserOrigin: cfg.BrowserOrigin,
		securityMode:  cfg.SecurityMode,
		registry:      registry,
		rootsPath:     rootsPath,
		sessions:      newSessionStore(),
		eventHub:      events,
	}
	s.stateSync = newStateSync(registry, events)
	s.stateSync.ReconcileCatalog()
	return s, nil
}

func (s *Server) routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.health)
	mux.HandleFunc("GET /version", s.version)
	mux.HandleFunc("POST /api/session", s.createSession)
	s.registerSettingsRoutes(mux)
	s.registerProjectRoutes(mux)
	s.registerGitRoutes(mux)
	s.registerProcessRoutes(mux)
	s.registerGitHubRoutes(mux)
	mux.HandleFunc("GET /events", s.events)
	return mux
}

// Resolve ownership once per request, then bind all repository handlers to that
// entry. The shared server, sessions, event hub and catalog are never mutated.
func (s *Server) Handler() http.Handler {
	routes := s.routes()
	return s.securityMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(path) < 2 || path[0] != "api" || path[1] == "settings" || path[1] == "session" || (path[1] == "projects" && len(path) == 2) {
			routes.ServeHTTP(w, r)
			return
		}
		scopedRoute := path[1] == "projects" || path[1] == "worktrees" || path[1] == "repository" || path[1] == "branches" || path[1] == "github" || path[1] == "processes"
		if !scopedRoute {
			routes.ServeHTTP(w, r)
			return
		}
		var project projectServices
		var ok bool
		switch {
		case path[1] == "projects" && len(path) > 2:
			project, ok = s.registry.Lookup(path[2])
		case path[1] == "worktrees" && len(path) > 2:
			project, ok = s.registry.Worktree(r.Context(), path[2])
			// Completed removal remains replayable after the worktree leaves the
			// inventory. The durable request journal supplies repository ownership.
			if !ok && r.Method == http.MethodDelete {
				key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
				for _, info := range s.registry.List() {
					candidate, exists := s.registry.Lookup(info.ID)
					if !exists || candidate.state == nil {
						continue
					}
					_ = candidate.state.View(func(data gitstore.Data) error {
						if old, exists := gitstore.Get[apiGitMutation](data, "api_git_mutations", key); exists && old.Command.Type == "git.worktree.remove" && old.Command.WorktreeID == path[2] {
							project, ok = candidate, true
						}
						return nil
					})
					if ok {
						break
					}
				}
			}
		default:
			project = s.registry.Default()
			ok = project.daemon != nil
		}
		if !ok {
			writeAPIError(w, http.StatusNotFound, "project_unavailable", "Project or worktree is not in the configured roots. Add its directory in Settings.")
			return
		}
		allowStaleSnapshot := r.Method == http.MethodGet && path[1] == "projects" && len(path) == 4 && path[3] == "git"
		if !project.info.Available && !allowStaleSnapshot {
			writeAPIError(w, http.StatusServiceUnavailable, "project_unavailable", "Project directory is unavailable")
			return
		}
		scoped := *s
		scoped.registry = &scopedProjectRegistry{project: project}
		scoped.repoDir = project.info.Path
		scoped.state = project.state
		scoped.routes().ServeHTTP(w, r)
	}))
}
func (s *Server) Reconcile(ctx context.Context) error {
	changed, err := s.registry.Refresh(ctx)
	s.stateSync.ReconcileCatalog()
	if changed || err != nil {
		s.eventHub.publish(localEvent{Type: "catalog", Epoch: s.stateSync.epoch, Projects: s.registry.List()})
	}
	return err
}
func (s *Server) reconcilePeriodically(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.Reconcile(ctx); err != nil {
				log.Printf("Project discovery: %v", err)
			}
		}
	}
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) version(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"version": version.String(), "api_version": localAPIVersion})
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
	go s.reconcilePeriodically(ctx)
	go s.stateSync.Run(ctx)

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
