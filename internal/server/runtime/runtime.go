package runtime

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
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

	"github.com/Tiago-0liveira/bonsai/internal/daemon/client"
	"github.com/Tiago-0liveira/bonsai/internal/daemon/gitbridge"
	domain "github.com/Tiago-0liveira/bonsai/internal/git"
)

const (
	ProductionBrowserOrigin = "https://app.bonsai.dev"
	capabilityTTL           = 15 * time.Minute
	maxRequestBody          = 1 << 20
	localRepositoryID       = "local"
	localBrowserUserID      = "local-browser"
)

type capability struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

type capabilityStore struct {
	mu   sync.RWMutex
	path string
	cap  capability
}

func loadCapability(path string) (*capabilityStore, error) {
	if path == "" {
		return nil, fmt.Errorf("capability file required")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cap capability
	if err := json.Unmarshal(data, &cap); err != nil || cap.Token == "" || cap.ExpiresAt.IsZero() {
		return nil, fmt.Errorf("invalid capability file")
	}
	return &capabilityStore{path: path, cap: cap}, nil
}

func (s *capabilityStore) current() capability {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cap
}

func (s *capabilityStore) valid(token string, now time.Time) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if token == "" || !now.Before(s.cap.ExpiresAt) || len(token) != len(s.cap.Token) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(token), []byte(s.cap.Token)) == 1
}

func (s *capabilityStore) refresh() (capability, error) {
	next, err := newCapability(time.Now())
	if err != nil {
		return capability{}, err
	}
	data, err := json.Marshal(next)
	if err != nil {
		return capability{}, err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return capability{}, err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		_ = os.Remove(tmp)
		return capability{}, err
	}
	s.mu.Lock()
	s.cap = next
	s.mu.Unlock()
	return next, nil
}

func newCapability(now time.Time) (capability, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return capability{}, err
	}
	return capability{
		Token:     hex.EncodeToString(raw),
		ExpiresAt: now.Add(capabilityTTL).UTC(),
	}, nil
}

type localAPI struct {
	daemon *client.Client
}

func (a *localAPI) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/session", a.session)
	mux.HandleFunc("POST /api/v1/session/refresh", a.sessionRefresh)

	mux.HandleFunc("GET /api/v1/repository", a.gitRead("git.repository.refresh", false))
	mux.HandleFunc("POST /api/v1/repository/fetch", a.gitMutation("git.fetch", false))
	mux.HandleFunc("GET /api/v1/branches", a.gitRead("git.branches", false))
	mux.HandleFunc("GET /api/v1/worktrees", a.gitRead("git.worktrees", false))
	mux.HandleFunc("POST /api/v1/worktrees", a.gitMutation("git.worktree.create", false))
	mux.HandleFunc("DELETE /api/v1/worktrees/{id}", a.gitMutation("git.worktree.remove", true))
	mux.HandleFunc("GET /api/v1/worktrees/{id}/status", a.gitRead("git.status", true))
	mux.HandleFunc("GET /api/v1/worktrees/{id}/files", a.gitRead("git.files", true))
	mux.HandleFunc("GET /api/v1/worktrees/{id}/files/{path...}", a.fileRead)
	mux.HandleFunc("GET /api/v1/worktrees/{id}/diff", a.diffRead)
	mux.HandleFunc("POST /api/v1/worktrees/{id}/{action}", a.worktreeMutation)
	mux.HandleFunc("POST /api/v1/worktrees/{id}/operations/{action}", a.operationMutation)

	mux.HandleFunc("GET /api/v1/processes", a.processes)
	mux.HandleFunc("GET /api/v1/processes/{id}/logs", a.processLogs)
	mux.HandleFunc("POST /api/v1/processes/{id}/restart", a.processRestart)
	mux.HandleFunc("DELETE /api/v1/processes/{id}", a.processStop)
	return mux
}

func (a *localAPI) session(w http.ResponseWriter, _ *http.Request) {
	// The middleware replaces this response with the current expiry.
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *localAPI) sessionRefresh(w http.ResponseWriter, r *http.Request) {
	store, ok := r.Context().Value(capabilityContextKey{}).(*capabilityStore)
	if !ok {
		writeAPIError(w, http.StatusInternalServerError, "internal", "capability state unavailable")
		return
	}
	cap, err := store.refresh()
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal", "failed to rotate capability")
		return
	}
	writeJSON(w, http.StatusOK, cap)
}

func (a *localAPI) gitRead(kind string, worktree bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		args := map[string]string{}
		for _, key := range []string{"mode", "base"} {
			if value := r.URL.Query().Get(key); value != "" {
				args[key] = value
			}
		}
		a.executeGit(w, r, kind, worktreeID(r, worktree), mustJSON(args), false)
	}
}

func (a *localAPI) gitMutation(kind string, worktree bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		args, err := requestArguments(r)
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid", err.Error())
			return
		}
		a.executeGit(w, r, kind, worktreeID(r, worktree), args, true)
	}
}

func (a *localAPI) fileRead(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.PathValue("path"), "/")
	if path == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid", "file path is required")
		return
	}
	a.executeGit(w, r, "git.file.read", r.PathValue("id"), mustJSON(map[string]string{"path": path}), false)
}

func (a *localAPI) diffRead(w http.ResponseWriter, r *http.Request) {
	args := map[string]string{}
	for _, key := range []string{"mode", "base", "path"} {
		if value := r.URL.Query().Get(key); value != "" {
			args[key] = value
		}
	}
	a.executeGit(w, r, "git.diff.read", r.PathValue("id"), mustJSON(args), false)
}

func (a *localAPI) worktreeMutation(w http.ResponseWriter, r *http.Request) {
	action := r.PathValue("action")
	kind := map[string]string{
		"pull": "git.pull", "push": "git.push", "commit": "git.commit",
		"rebase": "git.rebase", "merge": "git.merge", "stage": "git.stage",
		"unstage": "git.unstage",
	}[action]
	if kind == "" {
		http.NotFound(w, r)
		return
	}
	args, err := requestArguments(r)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid", err.Error())
		return
	}
	a.executeGit(w, r, kind, r.PathValue("id"), args, true)
}

func (a *localAPI) operationMutation(w http.ResponseWriter, r *http.Request) {
	action := r.PathValue("action")
	if action != "continue" && action != "abort" {
		http.NotFound(w, r)
		return
	}
	args, err := requestArguments(r)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid", err.Error())
		return
	}
	a.executeGit(w, r, "git.operation."+action, r.PathValue("id"), args, true)
}

func (a *localAPI) executeGit(w http.ResponseWriter, r *http.Request, kind, worktree string, args json.RawMessage, mutation bool) {
	id := randomID()
	if mutation {
		id = strings.TrimSpace(r.Header.Get("Idempotency-Key"))
		if id == "" || len(id) > 128 {
			writeAPIError(w, http.StatusBadRequest, "invalid", "Idempotency-Key is required for mutations")
			return
		}
	}
	result, err := a.daemon.Git(gitbridge.Command{
		ID:           id,
		UserID:       localBrowserUserID,
		RepositoryID: localRepositoryID,
		WorktreeID:   worktree,
		Type:         kind,
		Arguments:    args,
		CreatedAt:    time.Now().UTC(),
	})
	if err != nil {
		writeAPIError(w, http.StatusServiceUnavailable, "daemon_unavailable", err.Error())
		return
	}
	if result.Error != nil {
		writeDomainError(w, result.Error)
		return
	}
	if len(result.Payload) == 0 {
		writeJSON(w, http.StatusOK, nil)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(result.Payload)
}

func (a *localAPI) processes(w http.ResponseWriter, _ *http.Request) {
	records, err := a.daemon.List()
	if err != nil {
		writeAPIError(w, http.StatusServiceUnavailable, "daemon_unavailable", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, records)
}

func (a *localAPI) processRestart(w http.ResponseWriter, r *http.Request) {
	id, ok := processID(w, r)
	if !ok {
		return
	}
	record, err := a.daemon.Restart(id)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "process_restart_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, record)
}

func (a *localAPI) processStop(w http.ResponseWriter, r *http.Request) {
	id, ok := processID(w, r)
	if !ok {
		return
	}
	killed, err := a.daemon.Kill(id, false, "")
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "process_stop_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"killed": killed})
}

func (a *localAPI) processLogs(w http.ResponseWriter, r *http.Request) {
	id, ok := processID(w, r)
	if !ok {
		return
	}
	lines := 200
	if value := r.URL.Query().Get("n"); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil && parsed >= 0 && parsed <= 5000 {
			lines = parsed
		} else {
			writeAPIError(w, http.StatusBadRequest, "invalid", "n must be between 0 and 5000")
			return
		}
	}
	var out strings.Builder
	if err := a.daemon.Logs(id, false, lines, "", false, func(chunk string) error {
		_, err := out.WriteString(chunk)
		return err
	}); err != nil {
		writeAPIError(w, http.StatusBadRequest, "process_logs_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"content": out.String()})
}

func processID(w http.ResponseWriter, r *http.Request) (int, bool) {
	id, err := strconv.Atoi(r.PathValue("id"))
	if err != nil || id <= 0 {
		writeAPIError(w, http.StatusBadRequest, "invalid", "invalid process id")
		return 0, false
	}
	return id, true
}

func worktreeID(r *http.Request, enabled bool) string {
	if enabled {
		return r.PathValue("id")
	}
	return ""
}

func requestArguments(r *http.Request) (json.RawMessage, error) {
	if r.Body == nil || r.ContentLength == 0 {
		return json.RawMessage(`{}`), nil
	}
	reader := io.LimitReader(r.Body, maxRequestBody+1)
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	if len(data) > maxRequestBody {
		return nil, fmt.Errorf("request body exceeds %d bytes", maxRequestBody)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return json.RawMessage(`{}`), nil
	}
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, fmt.Errorf("invalid JSON")
	}
	return json.RawMessage(data), nil
}

func mustJSON(value any) json.RawMessage {
	data, _ := json.Marshal(value)
	return data
}

func randomID() string {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(raw)
}

type capabilityContextKey struct{}

func browserSecurity(expectedHost, browserOrigin string, caps *capabilityStore, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		if r.Host != expectedHost {
			writeAPIError(w, http.StatusForbidden, "invalid_host", "Host is not allowed")
			return
		}
		origin := r.Header.Get("Origin")
		if origin != browserOrigin {
			writeAPIError(w, http.StatusForbidden, "invalid_origin", "Origin is not allowed")
			return
		}
		w.Header().Set("Access-Control-Allow-Origin", browserOrigin)
		w.Header().Set("Vary", "Origin")
		if r.Method == http.MethodOptions {
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Idempotency-Key")
			w.Header().Set("Access-Control-Max-Age", "600")
			if strings.EqualFold(r.Header.Get("Access-Control-Request-Private-Network"), "true") {
				w.Header().Set("Access-Control-Allow-Private-Network", "true")
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		const prefix = "Bearer "
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, prefix) || !caps.valid(strings.TrimPrefix(auth, prefix), time.Now()) {
			writeAPIError(w, http.StatusUnauthorized, "invalid_capability", "valid local capability required")
			return
		}
		if r.URL.Path == "/api/v1/session" {
			writeJSON(w, http.StatusOK, map[string]any{"expires_at": caps.current().ExpiresAt})
			return
		}
		ctx := context.WithValue(r.Context(), capabilityContextKey{}, caps)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func validateBrowserOrigin(origin string) error {
	if origin == ProductionBrowserOrigin {
		return nil
	}
	u, err := url.Parse(origin)
	if err != nil || u.Scheme != "http" || u.Host == "" || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("browser origin must be %s or loopback HTTP", ProductionBrowserOrigin)
	}
	host := u.Hostname()
	if host == "localhost" {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("development browser origin must be loopback")
	}
	return nil
}

func requireLoopback(address string) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("invalid listen address %q: %w", address, err)
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("local API must bind to a loopback IP, got %q", host)
	}
	return nil
}

func writeDomainError(w http.ResponseWriter, e *domain.Error) {
	status := http.StatusBadRequest
	switch e.Code {
	case "not_found":
		status = http.StatusNotFound
	case "unauthorized":
		status = http.StatusUnauthorized
	case "forbidden":
		status = http.StatusForbidden
	case "conflict", "dirty_worktree", "busy", "outcome_unknown":
		status = http.StatusConflict
	case "daemon_offline":
		status = http.StatusServiceUnavailable
	case "internal":
		status = http.StatusInternalServerError
	}
	writeAPIError(w, status, e.Code, e.Message)
}

func writeAPIError(w http.ResponseWriter, status int, code, message string) {
	writeJSONStatus(w, status, map[string]any{
		"error": map[string]string{"code": code, "message": message},
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	writeJSONStatus(w, status, value)
}

func writeJSONStatus(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

// RunAPI starts the privileged local Bonsai API. It owns no GitHub credentials
// and never opens a repository directly; all operations go through the daemon.
func RunAPI(repoDir, address, browserOrigin, capabilityFile string) error {
	if repoDir == "" {
		return fmt.Errorf("repository root required")
	}
	if err := requireLoopback(address); err != nil {
		return err
	}
	if err := validateBrowserOrigin(browserOrigin); err != nil {
		return err
	}
	caps, err := loadCapability(capabilityFile)
	if err != nil {
		return err
	}
	api := &localAPI{daemon: client.For(repoDir)}
	handler := browserSecurity(address, browserOrigin, caps, api.routes())

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	server := &http.Server{
		Addr:              address,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       90 * time.Second,
		MaxHeaderBytes:    32 << 10,
	}
	go shutdownWithContext(ctx, server)
	log.Printf("Bonsai local API listening at http://%s for %s", address, browserOrigin)
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
