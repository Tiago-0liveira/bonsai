package localapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/agentruntime"
	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/agentterminal"
	domain "github.com/Tiago-0liveira/bonsai/internal/git"
	gitstore "github.com/Tiago-0liveira/bonsai/internal/storage/git"
)

type agentAPI struct {
	mu        sync.Mutex
	publishMu sync.Mutex
	runtime   *agentruntime.Runtime
	manager   *agentterminal.Manager
	registry  projectRegistry
}
type agentRequest struct {
	Model      string `json:"model,omitempty"`
	Prompt     string `json:"prompt,omitempty"`
	FullAccess *bool  `json:"full_access,omitempty"`
	// Claude launch options; providers reject the ones they do not support.
	PermissionMode string           `json:"permission_mode,omitempty"`
	Effort         string           `json:"effort,omitempty"`
	WorktreeID     string           `json:"worktree_id"`
	AccountID      agents.AccountID `json:"account_id"`
	Name           string           `json:"name,omitempty"`
	Cols           int              `json:"cols"`
	Rows           int              `json:"rows"`
}
type agentMutation struct {
	Fingerprint string                `json:"fingerprint"`
	Summary     agentterminal.Summary `json:"summary"`
}

func (s *Server) registerAgentRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/agents/providers", s.agentProviders)
	mux.HandleFunc("GET /api/agents/accounts", s.agentAccounts)
	mux.HandleFunc("GET /api/agents/providers/{provider}/models", s.agentModels)
	mux.HandleFunc("GET /api/projects/{projectId}/agents", s.listAgents)
	mux.HandleFunc("POST /api/projects/{projectId}/agents", s.startAgent)
	mux.HandleFunc("DELETE /api/projects/{projectId}/agents/{sessionId}", s.stopAgent)
	mux.HandleFunc("GET /api/projects/{projectId}/agents/{sessionId}/terminal", s.agentTerminal)
}

// knownAgentProviders keeps the UI's provider tabs stable: these are listed even
// when not registered in this build.
var knownAgentProviders = []struct {
	id    agents.ProviderID
	label string
}{{"antigravity", "Antigravity"}, {"claude", "Claude"}, {"codex", "Codex"}}

func (s *Server) agentRegistry() *agents.Registry {
	if s.agents == nil || s.agents.runtime == nil {
		return nil
	}
	return s.agents.runtime.Registry
}

func (s *Server) agentProviders(w http.ResponseWriter, r *http.Request) {
	registry := s.agentRegistry()
	describe := func(id agents.ProviderID, label string) map[string]any {
		var provider agents.Provider
		if registry != nil {
			provider, _ = registry.Get(id)
		}
		if provider != nil {
			if describer, ok := provider.(agents.Describer); ok && describer.Label() != "" {
				label = describer.Label()
			}
		}
		if label == "" {
			label = string(id)
		}
		var availability agents.Availability
		switch {
		case s.agents == nil:
			availability.Reason = "Agent runtime unavailable"
		case provider == nil:
			availability.Reason = "Not available yet"
		case !agentterminal.Supported:
			availability.Reason = "Interactive terminals are not supported on this platform"
		case !provider.Capabilities().Interactive:
			availability.Reason = "Interactive terminals are not supported by this provider"
		default:
			availability = agents.Availability{Available: true}
			if describer, ok := provider.(agents.Describer); ok {
				availability = describer.Availability(r.Context())
			}
		}
		reason := ""
		if !availability.Available {
			reason = availability.Reason
			if reason == "" {
				reason = "Not available"
			}
		}
		out := map[string]any{"id": id, "label": label, "available": availability.Available, "unavailable_reason": map[string]string{"message": reason}}
		if availability.Version != "" {
			out["version"] = availability.Version
		}
		return out
	}
	out := []map[string]any{}
	known := map[agents.ProviderID]bool{}
	for _, p := range knownAgentProviders {
		known[p.id] = true
		out = append(out, describe(p.id, p.label))
	}
	if registry != nil {
		for _, p := range registry.List() {
			if !known[p.ID()] {
				out = append(out, describe(p.ID(), ""))
			}
		}
	}
	writeJSON(w, 200, out)
}
func (s *Server) agentAccounts(w http.ResponseWriter, r *http.Request) {
	registry := s.agentRegistry()
	if registry == nil {
		writeAPIError(w, 503, "agents_unavailable", "Agent runtime unavailable")
		return
	}
	accounts, err := s.agents.runtime.Accounts.List()
	if err != nil {
		writeAPIError(w, 503, "profiles_unavailable", "Cannot read profiles")
		return
	}
	// A slow provider (Claude runs `auth status`) must not serialize the list.
	visible := accounts[:0:0]
	for _, a := range accounts {
		// Profiles of providers this build cannot launch stay hidden.
		if _, err := registry.Get(a.Provider); err == nil {
			visible = append(visible, a)
		}
	}
	infos := make([]agents.AccountInfo, len(visible))
	var wg sync.WaitGroup
	for i, a := range visible {
		wg.Add(1)
		go func() {
			defer wg.Done()
			infos[i] = registry.DescribeAccount(r.Context(), a)
		}()
	}
	wg.Wait()
	out := []map[string]any{}
	for i, a := range visible {
		info := infos[i]
		item := map[string]any{}
		for k, v := range info.Options {
			item[k] = v
		}
		item["id"], item["name"], item["provider"] = a.ID, a.Name, a.Provider
		if info.AuthMode != "" {
			item["auth_mode"] = info.AuthMode
		}
		if info.Identity != "" {
			item["identity"] = info.Identity
		}
		if len(info.Warnings) > 0 {
			item["warnings"] = info.Warnings
		}
		out = append(out, item)
	}
	writeJSON(w, 200, out)
}

// agentModels suggests models for the launch dialog. Providers that cannot list
// models return an empty list, and the dialog falls back to a plain input.
func (s *Server) agentModels(w http.ResponseWriter, r *http.Request) {
	registry := s.agentRegistry()
	if registry == nil {
		writeAPIError(w, 503, "agents_unavailable", "Agent runtime unavailable")
		return
	}
	provider, err := registry.Get(agents.ProviderID(r.PathValue("provider")))
	if err != nil {
		writeAPIError(w, 404, "not_found", "Unknown provider")
		return
	}
	out := []agents.ModelOption{}
	if lister, ok := provider.(agents.ModelLister); ok {
		var account agents.Account
		if id := r.URL.Query().Get("account_id"); id != "" {
			account, err = s.agents.runtime.Accounts.Get(agents.AccountID(id))
			if err != nil || account.Provider != provider.ID() {
				writeAPIError(w, 404, "not_found", "Profile not found")
				return
			}
		}
		if models, err := lister.Models(r.Context(), account); err == nil && models != nil {
			out = models
		}
	}
	writeJSON(w, 200, out)
}
func (s *Server) listAgents(w http.ResponseWriter, r *http.Request) {
	if s.agents == nil {
		writeJSON(w, 200, []agentterminal.Summary{})
		return
	}
	writeJSON(w, 200, s.agents.manager.List(r.PathValue("projectId")))
}
func (s *Server) agentWorktree(r *http.Request, id string) (string, error) {
	project := s.registry.Default()
	payload, err := s.stateSync.gitPayload(r.Context(), project, "git.worktrees", func(raw json.RawMessage) (any, error) {
		var trees []domain.Worktree
		err := json.Unmarshal(raw, &trees)
		return trees, err
	})
	if err != nil {
		return "", fmt.Errorf("cannot verify worktree")
	}
	for _, tree := range payload.([]domain.Worktree) {
		if tree.ID == id {
			return tree.Path, nil
		}
	}
	return "", fmt.Errorf("worktree no longer belongs to project")
}
func (s *Server) agentMutation(w http.ResponseWriter, r *http.Request, body agentRequest, stop bool) {
	if s.agents == nil || s.state == nil {
		writeAPIError(w, 503, "agents_unavailable", "Agent runtime unavailable")
		return
	}
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" || len(key) > 128 {
		writeAPIError(w, 400, "invalid", "Idempotency-Key is required")
		return
	}
	encoded, _ := json.Marshal(body)
	fingerprint := r.Method + ":" + r.URL.Path + ":" + string(encoded)
	s.agents.mu.Lock()
	defer s.agents.mu.Unlock()
	var old agentMutation
	exists := false
	err := s.state.Update(func(data gitstore.Data) error {
		old, exists = gitstore.Get[agentMutation](data, "api_agent_mutations", key)
		if exists {
			if old.Fingerprint != fingerprint {
				return fmt.Errorf("key reused with different request")
			}
			return nil
		}
		return gitstore.Put(data, "api_agent_mutations", key, agentMutation{Fingerprint: fingerprint})
	})
	if err != nil {
		writeAPIError(w, 409, "mutation_conflict", "Cannot reserve request key, or key was reused with a different request")
		return
	}
	project := r.PathValue("projectId")
	if s.agents.registry != nil {
		if current, ok := s.agents.registry.Lookup(project); !ok || !current.info.Available {
			s.releaseReservation(key)
			writeAPIError(w, 409, "project_unavailable", "Project is no longer authorized")
			return
		}
	}
	if exists {
		if current, ok := s.agents.manager.Get(project, old.Summary.ID); ok {
			writeJSON(w, 200, current)
		} else {
			old.Summary.State = "failed"
			old.Summary.Error = "Previous request was interrupted or its session is no longer retained. Start explicitly with a new request key."
			writeJSON(w, 200, old.Summary)
		}
		return
	}
	var summary agentterminal.Summary
	if stop {
		var ok bool
		summary, ok = s.agents.manager.Get(project, r.PathValue("sessionId"))
		if !ok {
			s.releaseReservation(key)
			writeAPIError(w, 404, "not_found", "Session not found")
			return
		}
		err = s.agents.manager.Stop(project, summary.ID)
		summary, _ = s.agents.manager.Get(project, summary.ID)
	} else {
		dir, pathErr := s.agentWorktree(r, body.WorktreeID)
		if pathErr != nil {
			s.releaseReservation(key)
			writeAPIError(w, 409, "worktree_unavailable", pathErr.Error())
			return
		}
		summary, err = s.agents.manager.Start(project, body.WorktreeID, body.AccountID, body.Name, dir, body.Cols, body.Rows, agentterminal.StartOptions{Model: body.Model, Prompt: body.Prompt, FullAccess: body.FullAccess, PermissionMode: body.PermissionMode, Effort: body.Effort})
	}
	switch {
	case err == nil:
	case errors.Is(err, agents.ErrInvalidLaunch):
		s.releaseReservation(key)
		writeAPIError(w, 400, "invalid_launch_options", err.Error())
		return
	case errors.Is(err, agents.ErrAccountBusy), errors.Is(err, agents.ErrProviderBusy):
		s.releaseReservation(key)
		writeAPIError(w, 409, "agent_busy", err.Error())
		return
	default:
		s.releaseReservation(key)
		writeAPIError(w, 400, "agent_start_failed", err.Error())
		return
	}
	err = s.state.Update(func(data gitstore.Data) error {
		if current, ok := s.agents.manager.Get(project, summary.ID); ok {
			summary = current
		}
		return gitstore.Put(data, "api_agent_mutations", key, agentMutation{Fingerprint: fingerprint, Summary: summary})
	})
	if err != nil {
		writeAPIError(w, 503, "journal_unavailable", "Request accepted but result could not be saved. Reconcile sessions before starting again.")
		return
	}
	writeJSON(w, http.StatusAccepted, summary)
}

// releaseReservation drops a request key whose request failed before any session
// existed, so the same key can be retried. Reservations that produced a session
// are never released. Callers hold s.agents.mu.
func (s *Server) releaseReservation(key string) {
	if err := s.state.Update(func(data gitstore.Data) error {
		if record, ok := gitstore.Get[agentMutation](data, "api_agent_mutations", key); ok && record.Summary.ID == "" {
			delete(data["api_agent_mutations"], key)
		}
		return nil
	}); err != nil {
		log.Printf("Agent request key release failed")
	}
}
func (s *Server) startAgent(w http.ResponseWriter, r *http.Request) {
	var body agentRequest
	if !decodeStrictJSON(w, r, &body) {
		return
	}
	if !agentterminal.Dimensions(body.Cols, body.Rows) || len(body.Name) > 128 || len(body.Model) > 128 || len(body.Prompt) > agentterminal.InputLimit || len(body.PermissionMode) > 32 || len(body.Effort) > 32 {
		writeAPIError(w, 400, "invalid", "Invalid name, launch options or terminal dimensions")
		return
	}
	s.agentMutation(w, r, body, false)
}
func (s *Server) stopAgent(w http.ResponseWriter, r *http.Request) {
	if r.ContentLength != 0 {
		writeAPIError(w, 400, "invalid", "Stop does not accept a request body")
		return
	}
	s.agentMutation(w, r, agentRequest{}, true)
}
func (s *Server) publishAgents(_ string) {
	if s.agents == nil {
		return
	}
	s.agents.publishMu.Lock()
	defer s.agents.publishMu.Unlock()
	// Eviction can affect another project's retained history, so reconcile all
	// project summaries on lifecycle changes. Terminal bytes never take this path.
	for _, info := range s.registry.List() {
		summaries := s.agents.manager.List(info.ID)
		if project, ok := s.registry.Lookup(info.ID); ok && project.state != nil {
			byID := map[string]agentterminal.Summary{}
			for _, summary := range summaries {
				byID[summary.ID] = summary
			}
			if err := project.state.Update(func(data gitstore.Data) error {
				for key := range data["api_agent_mutations"] {
					record, ok := gitstore.Get[agentMutation](data, "api_agent_mutations", key)
					if !ok {
						continue
					}
					if summary, ok := byID[record.Summary.ID]; ok {
						record.Summary = summary
						if err := gitstore.Put(data, "api_agent_mutations", key, record); err != nil {
							return err
						}
					}
				}
				return nil
			}); err != nil {
				log.Printf("Agent lifecycle journal update failed")
			}
		}
		s.stateSync.commit(info.ID, "agents", func(snapshot *browserSnapshot) {
			snapshot.Agents = summaries
			now := time.Now().UTC()
			snapshot.Freshness["agents"] = browserFreshness{State: "ready", UpdatedAt: &now}
		})
	}
}

func (s *Server) hasLiveAgents(project, tree string) bool {
	if s.agents == nil {
		return false
	}
	for _, a := range s.agents.manager.List(project) {
		if a.Active() && (tree == "" || a.WorktreeID == tree) {
			return true
		}
	}
	return false
}

func (s *Server) recoverAgents() error {
	for _, info := range s.registry.List() {
		project, ok := s.registry.Lookup(info.ID)
		if !ok || project.state == nil {
			continue
		}
		if err := project.state.Update(func(data gitstore.Data) error {
			for key := range data["api_agent_mutations"] {
				record, ok := gitstore.Get[agentMutation](data, "api_agent_mutations", key)
				if !ok || record.Summary.ID == "" {
					continue
				}
				if record.Summary.Active() {
					now := time.Now().UTC()
					record.Summary.State = "failed"
					record.Summary.EndedAt = &now
					record.Summary.Error = "Agent interrupted by API restart; start explicitly to run again."
					if err := gitstore.Put(data, "api_agent_mutations", key, record); err != nil {
						return err
					}
				}
				s.agents.manager.Restore(record.Summary)
			}
			return nil
		}); err != nil {
			return err
		}
		s.publishAgents(info.ID)
	}
	return nil
}
