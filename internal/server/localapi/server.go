package localapi

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/Tiago-0liveira/bonsai/internal/agentruntime"
	"github.com/Tiago-0liveira/bonsai/internal/core/agentterminal"
	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
	"github.com/Tiago-0liveira/bonsai/internal/daemon/gitbridge"
	"github.com/Tiago-0liveira/bonsai/internal/git/github/ghcli"
	git "github.com/Tiago-0liveira/bonsai/internal/git/local"
	"github.com/Tiago-0liveira/bonsai/internal/server/webhooks"
	"github.com/Tiago-0liveira/bonsai/internal/server/webui"
	gitstore "github.com/Tiago-0liveira/bonsai/internal/storage/git"
	"github.com/Tiago-0liveira/bonsai/internal/version"
	"github.com/Tiago-0liveira/bonsai/web"
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
	// UI is the web UI bundle served at /app. Nil uses the bundle embedded in
	// the binary (`-tags embedui`); without one, /app is a placeholder page.
	UI fs.FS
	// WebhookAddress turns on the live-updates receiver: a second loopback
	// listener serving only POST /github/webhook, verified with
	// WebhookSecret. Only the user-level API (no RepoDir) runs it.
	WebhookAddress string
	WebhookSecret  []byte
	// WebConfigPath, WebStatePath and WebHome (web.json, web-state.json and
	// the bonsai web daemon home) let the receiver's live controller manage
	// the repository hooks. Empty paths leave the hooks alone.
	WebConfigPath string
	WebStatePath  string
	WebHome       string
}

type Server struct {
	agents        *agentAPI
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
	ui            http.Handler
	// live is the webhook receiver and liveHooks its hook controller; both
	// nil when live updates are off.
	live           *webhooks.LiveReceiver
	liveHooks      *liveController
	webhookAddress string
}

// New builds the local API. RepoDir is optional: a repository-scoped API (the
// legacy per-repo serve) names its launch repository, which also backs the
// unscoped legacy routes. Without it (`bonsai web`), projects come only from
// the configured project roots and unscoped routes answer no_default_project.
func New(cfg Config) (*Server, error) {
	if cfg.RepoDir != "" {
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
	}
	if err := requireLoopback(cfg.Address); err != nil {
		return nil, err
	}
	if cfg.WebhookAddress != "" {
		if cfg.RepoDir != "" {
			return nil, fmt.Errorf("the webhook receiver runs only in the user-level API (bonsai web)")
		}
		if err := requireLoopback(cfg.WebhookAddress); err != nil {
			return nil, err
		}
		if cfg.WebhookAddress == cfg.Address {
			return nil, fmt.Errorf("the webhook receiver needs its own port")
		}
		if len(cfg.WebhookSecret) == 0 {
			return nil, fmt.Errorf("the webhook receiver needs a secret")
		}
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
	ui := cfg.UI
	if ui == nil {
		ui = web.Dist()
	}
	// The relay is frozen (decision D1): the embedded UI gets no relay origin.
	s.ui = webui.New(ui, webui.Options{})
	s.stateSync = newStateSync(registry, events)
	s.stateSync.ReconcileCatalog()
	if cfg.WebhookAddress != "" {
		s.webhookAddress = cfg.WebhookAddress
		// In-process GitHub only: no `gh api` fallback directory.
		s.liveHooks = newLiveController(cfg.WebConfigPath, cfg.WebStatePath, cfg.WebhookSecret, registry.github.Service(""), daemonPublicURL(cfg.WebHome), s.stateSync)
		s.stateSync.live = s.liveHooks
		s.live = webhooks.NewLiveReceiver(cfg.WebhookSecret, s.onLiveEvent)
	}
	runtime, err := agentruntime.New(nil, nil, nil)
	if err != nil {
		return nil, err
	}
	s.agents = &agentAPI{runtime: runtime, registry: s.registry}
	s.agents.manager = agentterminal.New(runtime.Accounts, runtime.Sessions, runtime.Registry, s.publishAgents)
	if err := s.recoverAgents(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Server) routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.health)
	mux.HandleFunc("GET /version", s.version)
	mux.HandleFunc("POST /api/session", s.createSession)
	s.registerAgentRoutes(mux)
	s.registerSettingsRoutes(mux)
	s.registerProjectRoutes(mux)
	s.registerGitRoutes(mux)
	s.registerProcessRoutes(mux)
	s.registerGitHubRoutes(mux)
	mux.HandleFunc("GET /events", s.events)
	ui := s.ui
	if ui == nil {
		ui = webui.New(nil, webui.Options{})
	}
	mux.Handle("GET /{$}", ui)
	mux.Handle("GET /app", ui)
	mux.Handle("GET /app/", ui)
	mux.Handle("GET /assets/", ui)
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
			if !s.hasDefaultProject() {
				writeAPIError(w, http.StatusNotFound, "no_default_project", "This local API serves several projects and has no default one. Use the /api/projects/{projectId}/... routes.")
				return
			}
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

// hasDefaultProject reports whether this API was launched for a repository.
func (s *Server) hasDefaultProject() bool { return s.repoDir != "" }

func (s *Server) Reconcile(ctx context.Context) error {
	if s.agents != nil {
		s.agents.manager.CheckDirectories()
	}
	previous := s.registry.List()
	changed, err := s.registry.Refresh(ctx)
	if s.agents != nil {
		for _, info := range previous {
			current, ok := s.registry.Lookup(info.ID)
			if !ok || !current.info.Available {
				for _, session := range s.agents.manager.List(info.ID) {
					if session.Active() {
						_ = s.agents.manager.Stop(info.ID, session.ID)
					}
				}
			}
		}
	}
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
	defer s.agents.manager.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go s.reconcilePeriodically(ctx)
	go s.stateSync.Run(ctx)
	if s.liveHooks != nil {
		go s.liveHooks.Run(ctx)
	}
	if registry, ok := s.registry.(*discoveredProjectRegistry); ok {
		// The browser connects after the API is up; have the GitHub token and
		// connection ready by then.
		go registry.github.Prewarm(ctx, ghcli.DefaultHost)
	}

	// Both listeners bind before either serves, so a taken webhook port fails
	// the start instead of leaving an API without its receiver.
	apiListener, err := net.Listen("tcp", cfg.Address)
	if err != nil {
		return err
	}
	receiverFailed := make(chan error, 1)
	if s.live != nil {
		webhookListener, err := net.Listen("tcp", s.webhookAddress)
		if err != nil {
			_ = apiListener.Close()
			return fmt.Errorf("webhook receiver: %w", err)
		}
		// The receiver is the one internet-facing surface (through the
		// tunnel): its own server and mux, never the API handler or its
		// Host/Origin checks.
		receiver := webhooks.NewLiveServer(s.webhookAddress, s.live.Handler())
		go shutdownWithContext(ctx, receiver)
		go func() {
			if err := receiver.Serve(webhookListener); err != http.ErrServerClosed {
				receiverFailed <- err
				stop()
			}
		}()
		log.Printf("Bonsai webhook receiver listening at http://%s%s (signed GitHub deliveries only)", s.webhookAddress, webhooks.LivePath)
	}

	server := &http.Server{
		Addr:              cfg.Address,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       90 * time.Second,
		MaxHeaderBytes:    32 << 10,
	}
	go shutdownWithContext(ctx, server)
	log.Printf("Bonsai local API listening at http://%s (UI at /app) for %s", cfg.Address, strings.Join(s.allowedOrigins(), ", "))
	err = server.Serve(apiListener)
	if err == http.ErrServerClosed {
		select {
		case err := <-receiverFailed:
			return fmt.Errorf("webhook receiver: %w", err)
		default:
			return nil
		}
	}
	return err
}

// onLiveEvent handles one verified delivery: refresh the matching projects,
// and record that the repository's hook works.
func (s *Server) onLiveEvent(event webhooks.LiveEvent) {
	s.stateSync.handleLiveEvent(event)
	if s.liveHooks != nil {
		s.liveHooks.observe(event)
	}
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
