package localapi

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/agentsetup"
	"github.com/Tiago-0liveira/bonsai/internal/core/agentterminal"
)

// Bounds on setup request bodies.
const (
	maxSetupSecret   = 8 << 10
	maxSetupAuthMode = 32
	maxSetupName     = 64
	// maxSetupKeys bounds the in-memory idempotency map.
	maxSetupKeys = 256
)

// setupTerminalSupported is false on platforms without PTYs; tests flip it.
var setupTerminalSupported = agentterminal.Supported

// agentSetupRequest creates a profile. Secret is a pasted API key or token; it
// is turned into a reader at once and never stored, logged or fingerprinted.
type agentSetupRequest struct {
	Provider agents.ProviderID `json:"provider"`
	Name     string            `json:"name"`
	AuthMode string            `json:"auth_mode,omitempty"`
	Seed     *bool             `json:"seed,omitempty"`
	Secret   string            `json:"secret,omitempty"`
}

// agentLoginRequest signs an existing profile in again.
type agentLoginRequest struct {
	AuthMode string `json:"auth_mode,omitempty"`
	Secret   string `json:"secret,omitempty"`
}

type agentSecretRequest struct {
	Secret string `json:"secret"`
}

// setupKeys maps Idempotency-Key to the job it started. Fingerprints hash the
// request without its secret.
type setupKeys struct {
	mu   sync.Mutex
	keys map[string]setupKey
}

type setupKey struct {
	fingerprint string
	job         string
}

func (s *Server) registerAgentSetupRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/agents/setup", s.startAgentSetup)
	mux.HandleFunc("POST /api/agents/accounts/{id}/login", s.startAgentLogin)
	mux.HandleFunc("GET /api/agents/setup/{job}", s.agentSetupJob)
	mux.HandleFunc("POST /api/agents/setup/{job}/secret", s.answerAgentSetup)
	mux.HandleFunc("DELETE /api/agents/setup/{job}", s.cancelAgentSetup)
	mux.HandleFunc("GET /api/agents/setup/{job}/terminal", s.agentSetupTerminal)
}

func isSetupTerminalRoute(path string) bool {
	p := strings.Split(path, "/")
	return len(p) == 6 && p[0] == "" && p[1] == "api" && p[2] == "agents" && p[3] == "setup" && p[4] != "" && p[5] == "terminal"
}

// setupJobs returns the job manager, or writes why there is none.
func (s *Server) setupJobs(w http.ResponseWriter) (*agentsetup.Jobs, bool) {
	if !setupTerminalSupported {
		writeAPIError(w, 501, "terminal_unsupported", "Signing in from the browser needs a terminal, which this platform does not support. Use the bonsai agent CLI.")
		return nil, false
	}
	if s.agents == nil || s.agents.setup == nil || s.agents.runtime == nil || s.agents.runtime.Registry == nil {
		writeAPIError(w, 503, "agents_unavailable", "Agent runtime unavailable")
		return nil, false
	}
	return s.agents.setup, true
}

// setupProvider resolves a registered provider that is installed on this host.
func (s *Server) setupProvider(w http.ResponseWriter, r *http.Request, id agents.ProviderID) (agents.Provider, bool) {
	provider, err := s.agents.runtime.Registry.Get(id)
	if err != nil {
		writeAPIError(w, 404, "not_found", "Unknown provider")
		return nil, false
	}
	if describer, ok := provider.(agents.Describer); ok {
		if availability := describer.Availability(r.Context()); !availability.Available {
			message := availability.Reason
			if message == "" {
				message = "Provider is not available on this host"
			}
			writeAPIError(w, 409, "provider_unavailable", message)
			return nil, false
		}
	}
	return provider, true
}

func (s *Server) startAgentSetup(w http.ResponseWriter, r *http.Request) {
	jobs, ok := s.setupJobs(w)
	if !ok {
		return
	}
	var body agentSetupRequest
	if !decodeStrictJSON(w, r, &body) {
		return
	}
	secret := takeSecret(&body.Secret)
	if len(body.Name) > maxSetupName || agents.ValidateAccountName(body.Name) != nil {
		writeProfileError(w, agents.ErrInvalidAccountName)
		return
	}
	if len(body.AuthMode) > maxSetupAuthMode || len(secret) > maxSetupSecret || body.Provider == "" || len(body.Provider) > 64 {
		writeAPIError(w, 400, "invalid", "Invalid provider, sign-in method or secret")
		return
	}
	key, ok := setupIdempotencyKey(w, r)
	if !ok {
		return
	}
	fingerprint := setupFingerprint(r, body)
	s.agents.setupKeys.mu.Lock()
	defer s.agents.setupKeys.mu.Unlock()
	if s.replaySetupKey(w, r, jobs, key, fingerprint) {
		return
	}
	if _, ok := s.setupProvider(w, r, body.Provider); !ok {
		return
	}
	if _, err := s.agents.runtime.Accounts.GetByName(body.Provider, body.Name); err == nil {
		writeProfileError(w, agents.ErrAccountExists)
		return
	} else if !errors.Is(err, agents.ErrAccountNotFound) {
		writeProfileError(w, err)
		return
	}
	options := agents.SetupOptions{AuthMode: body.AuthMode, Seed: body.Seed}
	if secret != "" {
		options.Secret = strings.NewReader(secret)
	}
	job, err := jobs.Start(agentsetup.Request{Kind: agentsetup.KindCreate, Provider: body.Provider, Name: body.Name, Options: options})
	s.finishSetupStart(w, r, jobs, key, fingerprint, job, err)
}

func (s *Server) startAgentLogin(w http.ResponseWriter, r *http.Request) {
	jobs, ok := s.setupJobs(w)
	if !ok {
		return
	}
	var body agentLoginRequest
	if !decodeStrictJSON(w, r, &body) {
		return
	}
	secret := takeSecret(&body.Secret)
	if len(body.AuthMode) > maxSetupAuthMode || len(secret) > maxSetupSecret {
		writeAPIError(w, 400, "invalid", "Invalid sign-in method or secret")
		return
	}
	key, ok := setupIdempotencyKey(w, r)
	if !ok {
		return
	}
	fingerprint := setupFingerprint(r, body)
	s.agents.setupKeys.mu.Lock()
	defer s.agents.setupKeys.mu.Unlock()
	if s.replaySetupKey(w, r, jobs, key, fingerprint) {
		return
	}
	_, account, ok := s.agentProfile(w, r)
	if !ok {
		return
	}
	provider, ok := s.setupProvider(w, r, account.Provider)
	if !ok {
		return
	}
	if _, ok := provider.(agents.AccountAuthenticator); !ok {
		writeAPIError(w, 409, "reauth_unsupported", "This provider cannot sign a profile in again; remove and add it instead")
		return
	}
	options := agents.SetupOptions{AuthMode: body.AuthMode}
	if secret != "" {
		options.Secret = strings.NewReader(secret)
	}
	job, err := jobs.Start(agentsetup.Request{Kind: agentsetup.KindLogin, Provider: account.Provider, Name: account.Name, AccountID: account.ID, Options: options})
	s.finishSetupStart(w, r, jobs, key, fingerprint, job, err)
}

// takeSecret moves a request's secret out of its struct, so the struct can be
// fingerprinted and dropped without it.
func takeSecret(field *string) string {
	secret := *field
	*field = ""
	return secret
}

func setupIdempotencyKey(w http.ResponseWriter, r *http.Request) (string, bool) {
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" || len(key) > 128 {
		writeAPIError(w, 400, "invalid", "Idempotency-Key is required")
		return "", false
	}
	return key, true
}

// setupFingerprint hashes the request; body must already be without its secret.
func setupFingerprint(r *http.Request, body any) string {
	encoded, _ := json.Marshal(body)
	sum := sha256.Sum256(encoded)
	return r.Method + ":" + r.URL.Path + ":" + hex.EncodeToString(sum[:])
}

// replaySetupKey answers a retried request key: the same job for the same
// request, 409 for a different one. Callers hold setupKeys.mu.
func (s *Server) replaySetupKey(w http.ResponseWriter, r *http.Request, jobs *agentsetup.Jobs, key, fingerprint string) bool {
	old, exists := s.agents.setupKeys.keys[key]
	if !exists {
		return false
	}
	if old.fingerprint != fingerprint {
		writeAPIError(w, 409, "mutation_conflict", "Request key was reused with a different request")
		return true
	}
	job, ok := jobs.Get(old.job)
	if !ok {
		writeAPIError(w, 404, "not_found", "Setup job is no longer retained; start again with a new request key")
		return true
	}
	writeJSON(w, 200, s.setupJobItem(r, job))
	return true
}

// finishSetupStart records the key of a started job and answers 202, or maps
// the start error. Callers hold setupKeys.mu.
func (s *Server) finishSetupStart(w http.ResponseWriter, req *http.Request, jobs *agentsetup.Jobs, key, fingerprint string, job agentsetup.Job, err error) {
	switch {
	case err == nil:
	case errors.Is(err, agentsetup.ErrBusy):
		writeAPIError(w, 409, "setup_busy", "A sign-in for this profile is already running")
		return
	case errors.Is(err, agentsetup.ErrLimit):
		writeAPIError(w, 409, "setup_limit", "Two sign-ins are already running; finish or cancel one first")
		return
	default:
		writeAPIError(w, 503, "setup_unavailable", "Cannot start the sign-in right now")
		return
	}
	keys := s.agents.setupKeys.keys
	if keys == nil {
		keys = map[string]setupKey{}
		s.agents.setupKeys.keys = keys
	}
	for k, v := range keys {
		if _, ok := jobs.Get(v.job); !ok || len(keys) >= maxSetupKeys {
			delete(keys, k)
		}
	}
	keys[key] = setupKey{fingerprint: fingerprint, job: job.ID}
	writeJSON(w, http.StatusAccepted, s.setupJobItem(req, job))
}

// setupJobItem is a job as the API shows it; a succeeded job carries the
// described profile.
func (s *Server) setupJobItem(r *http.Request, job agentsetup.Job) map[string]any {
	encoded, _ := json.Marshal(job)
	item := map[string]any{}
	_ = json.Unmarshal(encoded, &item)
	if job.State != agentsetup.StateSucceeded || job.AccountID == "" {
		return item
	}
	registry := s.agentRegistry()
	if registry == nil {
		return item
	}
	account, err := s.agents.runtime.Accounts.Get(job.AccountID)
	if err != nil {
		return item
	}
	item["account"] = accountItem(registry, account, registry.DescribeAccount(r.Context(), account))
	return item
}

func (s *Server) agentSetupJob(w http.ResponseWriter, r *http.Request) {
	jobs, ok := s.setupJobs(w)
	if !ok {
		return
	}
	job, ok := jobs.Get(r.PathValue("job"))
	if !ok {
		writeAPIError(w, 404, "not_found", "Setup job not found")
		return
	}
	writeJSON(w, 200, s.setupJobItem(r, job))
}

func (s *Server) answerAgentSetup(w http.ResponseWriter, r *http.Request) {
	jobs, ok := s.setupJobs(w)
	if !ok {
		return
	}
	var body agentSecretRequest
	if !decodeStrictJSON(w, r, &body) {
		return
	}
	secret := takeSecret(&body.Secret)
	if len(secret) > maxSetupSecret {
		writeAPIError(w, 400, "invalid", "Secret is too long")
		return
	}
	switch err := jobs.Answer(r.PathValue("job"), secret); {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, agentsetup.ErrNotFound):
		writeAPIError(w, 404, "not_found", "Setup job not found")
	default:
		writeAPIError(w, 409, "no_prompt", "The sign-in is not waiting for a secret")
	}
}

func (s *Server) cancelAgentSetup(w http.ResponseWriter, r *http.Request) {
	if r.ContentLength != 0 {
		writeAPIError(w, 400, "invalid", "Cancel does not accept a request body")
		return
	}
	jobs, ok := s.setupJobs(w)
	if !ok {
		return
	}
	job, err := jobs.Cancel(r.PathValue("job"))
	if err != nil {
		writeAPIError(w, 404, "not_found", "Setup job not found")
		return
	}
	writeJSON(w, 200, s.setupJobItem(r, job))
}

func (s *Server) agentSetupTerminal(w http.ResponseWriter, r *http.Request) {
	jobs, ok := s.setupJobs(w)
	if !ok {
		return
	}
	id := r.PathValue("job")
	serveTerminal(s, w, r, func() (terminalEndpoint[agentsetup.Job], bool) {
		if _, ok := jobs.Get(id); !ok {
			return terminalEndpoint[agentsetup.Job]{}, false
		}
		return terminalEndpoint[agentsetup.Job]{
			attach: func(cursor uint64) (uint64, bool, []agentsetup.Frame, <-chan agentsetup.Frame, error) {
				return jobs.Attach(id, cursor)
			},
			detach: func(subscriber uint64) { jobs.Detach(id, subscriber) },
			input:  func(subscriber uint64, data []byte) error { return jobs.Input(id, subscriber, data) },
			resize: func(subscriber uint64, cols, rows int) error { return jobs.Resize(id, subscriber, cols, rows) },
		}, true
	})
}
