package localapi

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/agentruntime"
	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/agentterminal"
	domain "github.com/Tiago-0liveira/bonsai/internal/git"
	"github.com/Tiago-0liveira/bonsai/internal/providers/antigravity"
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
	Model      string           `json:"model,omitempty"`
	Prompt     string           `json:"prompt,omitempty"`
	FullAccess *bool            `json:"full_access,omitempty"`
	WorktreeID string           `json:"worktree_id"`
	AccountID  agents.AccountID `json:"account_id"`
	Name       string           `json:"name,omitempty"`
	Cols       int              `json:"cols"`
	Rows       int              `json:"rows"`
}
type agentMutation struct {
	Fingerprint string                `json:"fingerprint"`
	Summary     agentterminal.Summary `json:"summary"`
}

func (s *Server) registerAgentRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/agents/providers", s.agentProviders)
	mux.HandleFunc("GET /api/agents/accounts", s.agentAccounts)
	mux.HandleFunc("GET /api/projects/{projectId}/agents", s.listAgents)
	mux.HandleFunc("POST /api/projects/{projectId}/agents", s.startAgent)
	mux.HandleFunc("DELETE /api/projects/{projectId}/agents/{sessionId}", s.stopAgent)
	mux.HandleFunc("GET /api/projects/{projectId}/agents/{sessionId}/terminal", s.agentTerminal)
}
func (s *Server) agentProviders(w http.ResponseWriter, r *http.Request) {
	reason := ""
	if s.agents == nil {
		reason = "Agent runtime unavailable"
	} else if !agentterminal.Supported {
		reason = "Interactive terminals are not supported on this platform"
	} else if _, err := exec.LookPath("agy"); err != nil {
		reason = "Install agy and restart Bonsai"
	}
	out := []map[string]any{}
	for _, p := range []struct{ id, label string }{{"antigravity", "Antigravity"}, {"claude", "Claude"}, {"codex", "Codex"}} {
		why := reason
		if p.id != "antigravity" {
			why = "Not available yet"
		}
		out = append(out, map[string]any{"id": p.id, "label": p.label, "available": why == "", "unavailable_reason": map[string]string{"message": why}})
	}
	writeJSON(w, 200, out)
}
func (s *Server) agentAccounts(w http.ResponseWriter, r *http.Request) {
	if s.agents == nil {
		writeAPIError(w, 503, "agents_unavailable", "Agent runtime unavailable")
		return
	}
	accounts, err := s.agents.runtime.Accounts.List()
	if err != nil {
		writeAPIError(w, 503, "profiles_unavailable", "Cannot read profiles")
		return
	}
	out := []map[string]any{}
	for _, a := range accounts {
		if a.Provider == "antigravity" {
			settings, _ := antigravity.ParseSettings(a)
			out = append(out, map[string]any{"id": a.ID, "name": a.Name, "provider": a.Provider, "full_access": settings.DangerouslySkipPermissions})
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
			writeAPIError(w, 404, "not_found", "Session not found")
			return
		}
		err = s.agents.manager.Stop(project, summary.ID)
		summary, _ = s.agents.manager.Get(project, summary.ID)
	} else {
		dir, pathErr := s.agentWorktree(r, body.WorktreeID)
		if pathErr != nil {
			writeAPIError(w, 409, "worktree_unavailable", pathErr.Error())
			return
		}
		summary, err = s.agents.manager.Start(project, body.WorktreeID, body.AccountID, body.Name, dir, body.Cols, body.Rows, agentterminal.StartOptions{Model: body.Model, Prompt: body.Prompt, FullAccess: body.FullAccess})
	}
	if err != nil {
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
func (s *Server) startAgent(w http.ResponseWriter, r *http.Request) {
	var body agentRequest
	if !decodeStrictJSON(w, r, &body) {
		return
	}
	if !agentterminal.Dimensions(body.Cols, body.Rows) || len(body.Name) > 128 || len(body.Model) > 128 || len(body.Prompt) > agentterminal.InputLimit {
		writeAPIError(w, 400, "invalid", "Invalid name or terminal dimensions")
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
