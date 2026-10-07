package localapi

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
	"github.com/Tiago-0liveira/bonsai/internal/daemon/gitbridge"
	domain "github.com/Tiago-0liveira/bonsai/internal/git"
	gitstore "github.com/Tiago-0liveira/bonsai/internal/storage/git"
)

const maxRequestBody = 1 << 20

func (s *Server) registerGitRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/repository", s.gitRead("git.repository.refresh", false))
	mux.HandleFunc("POST /api/repository/fetch", s.gitMutation("git.fetch", false))
	mux.HandleFunc("GET /api/branches", s.gitRead("git.branches", false))
	mux.HandleFunc("GET /api/worktrees", s.gitRead("git.worktrees", false))
	mux.HandleFunc("POST /api/worktrees", s.gitMutation("git.worktree.create", false))
	mux.HandleFunc("DELETE /api/worktrees/{id}", s.gitMutation("git.worktree.remove", true))
	mux.HandleFunc("GET /api/worktrees/{id}/status", s.gitRead("git.status", true))
	mux.HandleFunc("GET /api/worktrees/{id}/files", s.gitRead("git.files", true))
	mux.HandleFunc("GET /api/worktrees/{id}/files/{path...}", s.fileRead)
	mux.HandleFunc("GET /api/worktrees/{id}/diff", s.diffRead)
	mux.HandleFunc("POST /api/worktrees/{id}/{action}", s.worktreeMutation)
	mux.HandleFunc("POST /api/worktrees/{id}/operations/{action}", s.operationMutation)

	// Compatibility with the current Bonsai Web route shape while requests now
	// terminate directly at the loopback API instead of a cloud daemon bridge.
	mux.HandleFunc("POST /api/projects/{projectId}/fetch", s.gitMutation("git.fetch", false))
	mux.HandleFunc("GET /api/projects/{projectId}/branches", s.projectGitRead("git.branches"))
	mux.HandleFunc("GET /api/projects/{projectId}/worktrees", s.projectGitRead("git.worktrees"))
	mux.HandleFunc("POST /api/projects/{projectId}/worktrees", s.compatCreateWorktree)
}

func (s *Server) gitRead(kind string, worktree bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		args := map[string]string{}
		for _, key := range []string{"mode", "base"} {
			if value := r.URL.Query().Get(key); value != "" {
				args[key] = value
			}
		}
		s.executeGitRead(w, r, kind, worktreeID(r, worktree), mustJSON(args))
	}
}

func (s *Server) projectGitRead(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.requireLocalProject(w, r) {
			return
		}
		s.executeGitRead(w, r, kind, "", json.RawMessage(`{}`))
	}
}

func (s *Server) gitMutation(kind string, worktree bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		args, ok := requestArguments(w, r, allowedArguments(kind)...)
		if !ok {
			return
		}
		s.executeGitMutation(w, r, kind, worktreeID(r, worktree), args)
	}
}

func (s *Server) compatCreateWorktree(w http.ResponseWriter, r *http.Request) {
	if !s.requireLocalProject(w, r) {
		return
	}
	args, ok := requestArguments(w, r, allowedArguments("git.worktree.create")...)
	if !ok {
		return
	}
	s.executeGitMutation(w, r, "git.worktree.create", "", args)
}

func (s *Server) fileRead(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.PathValue("path"), "/")
	if path == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid", "file path is required")
		return
	}
	s.executeGitRead(w, r, "git.file.read", r.PathValue("id"), mustJSON(map[string]string{"path": path}))
}

func (s *Server) diffRead(w http.ResponseWriter, r *http.Request) {
	args := map[string]string{}
	for _, key := range []string{"mode", "base", "path"} {
		if value := r.URL.Query().Get(key); value != "" {
			args[key] = value
		}
	}
	s.executeGitRead(w, r, "git.diff.read", r.PathValue("id"), mustJSON(args))
}

func (s *Server) worktreeMutation(w http.ResponseWriter, r *http.Request) {
	action := r.PathValue("action")
	kind := map[string]string{
		"pull":    "git.pull",
		"push":    "git.push",
		"commit":  "git.commit",
		"rebase":  "git.rebase",
		"merge":   "git.merge",
		"stage":   "git.stage",
		"unstage": "git.unstage",
	}[action]
	if kind == "" {
		http.NotFound(w, r)
		return
	}
	args, ok := requestArguments(w, r, allowedArguments(kind)...)
	if !ok {
		return
	}
	s.executeGitMutation(w, r, kind, r.PathValue("id"), args)
}

func (s *Server) operationMutation(w http.ResponseWriter, r *http.Request) {
	action := r.PathValue("action")
	if action != "continue" && action != "abort" {
		http.NotFound(w, r)
		return
	}
	kind := "git.operation." + action
	args, ok := requestArguments(w, r, allowedArguments(kind)...)
	if !ok {
		return
	}
	s.executeGitMutation(w, r, kind, r.PathValue("id"), args)
}

func (s *Server) executeGitRead(w http.ResponseWriter, r *http.Request, kind, worktree string, args json.RawMessage) {
	result, ok := s.runGit(w, r, kind, worktree, args, false)
	if !ok {
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

func (s *Server) executeGitMutation(w http.ResponseWriter, r *http.Request, kind, worktree string, args json.RawMessage) {
	result, ok := s.runGit(w, r, kind, worktree, args, true)
	if !ok {
		return
	}
	projectID := s.registry.Default().info.ID
	metadataError := ""
	if kind == "git.worktree.remove" && s.state != nil {
		if err := s.state.Update(func(data gitstore.Data) error { delete(data["worktree_metadata"], worktree); return nil }); err != nil {
			metadataError = err.Error()
		}
	}
	s.stateSync.Queue(projectID, refreshAll, true)
	var value any
	if len(result.Payload) > 0 {
		value = json.RawMessage(result.Payload)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"command_id":     result.ID,
		"result":         value,
		"metadata_error": metadataError,
	})
}

func (s *Server) runGit(w http.ResponseWriter, r *http.Request, kind, worktree string, args json.RawMessage, mutation bool) (*gitbridge.Result, bool) {
	if kind == "git.worktree.remove" && s.agents != nil {
		s.agents.mu.Lock()
		defer s.agents.mu.Unlock()
	}
	id := randomID()
	if mutation {
		id = strings.TrimSpace(r.Header.Get("Idempotency-Key"))
		if id == "" || len(id) > 128 {
			writeAPIError(w, http.StatusBadRequest, "invalid", "Idempotency-Key is required for mutations")
			return nil, false
		}
	}
	command := gitbridge.Command{
		ID:           id,
		UserID:       localBrowserUserID,
		RepositoryID: localRepositoryID,
		WorktreeID:   worktree,
		Type:         kind,
		Arguments:    args,
		CreatedAt:    time.Now().UTC(),
	}
	if mutation && s.state != nil {
		var saved *gitbridge.Result
		err := s.state.Update(func(data gitstore.Data) error {
			if old, ok := gitstore.Get[apiGitMutation](data, "api_git_mutations", id); ok {
				if old.Command.Type != kind || old.Command.WorktreeID != worktree || string(old.Command.Arguments) != string(args) {
					return domain.ErrInvalid
				}
				command, saved = old.Command, old.Result
				return nil
			}
			return gitstore.Put(data, "api_git_mutations", id, apiGitMutation{Command: command})
		})
		if err != nil {
			writeDomainError(w, &domain.Error{Code: domain.Code(err), Message: err.Error()})
			return nil, false
		}
		if saved != nil {
			return saved, true
		}
	}
	if kind == "git.worktree.remove" {
		if s.hasLiveAgents(s.registry.Default().info.ID, worktree) {
			writeAPIError(w, 409, "agents_running", "Stop agents before deleting this worktree")
			return nil, false
		}
		project := s.registry.Default()
		payload, err := s.stateSync.gitPayload(r.Context(), project, "git.worktrees", func(raw json.RawMessage) (any, error) {
			var trees []domain.Worktree
			err := json.Unmarshal(raw, &trees)
			return trees, err
		})
		if err != nil {
			writeAPIError(w, 503, "status_unavailable", err.Error())
			return nil, false
		}
		records, err := project.daemon.List()
		if err != nil {
			writeAPIError(w, 503, "processes_unavailable", "Cannot verify running processes: "+err.Error())
			return nil, false
		}
		for _, tree := range payload.([]domain.Worktree) {
			if tree.ID != worktree {
				continue
			}
			for _, process := range records {
				if procstore.IsActive(process.Status) && sameWorktreePath(tree.Path, process.Worktree) {
					writeAPIError(w, 409, "processes_running", "Stop running processes before deleting this worktree")
					return nil, false
				}
			}
		}
	}
	result, err := s.registry.Default().daemon.Git(command)
	if err != nil {
		writeAPIError(w, http.StatusServiceUnavailable, "daemon_unavailable", err.Error())
		return nil, false
	}
	if result.Error != nil {
		writeDomainError(w, result.Error)
		return nil, false
	}
	if len(result.Payload) > 0 {
		payload, err := publicGitPayload(kind, s.registry.Default().info.ID, result.Payload)
		if err != nil {
			writeAPIError(w, http.StatusBadGateway, "invalid_daemon_response", err.Error())
			return nil, false
		}
		result.Payload = payload
	}
	if mutation && s.state != nil {
		if err := s.state.Update(func(data gitstore.Data) error {
			return gitstore.Put(data, "api_git_mutations", id, apiGitMutation{Command: command, Result: result})
		}); err != nil {
			writeAPIError(w, http.StatusConflict, "outcome_unknown", domain.ErrUncertain.Error())
			return nil, false
		}
	}
	return result, true
}

type apiGitMutation struct {
	Command gitbridge.Command `json:"command"`
	Result  *gitbridge.Result `json:"result,omitempty"`
}

func allowedArguments(kind string) []string {
	switch kind {
	case "git.fetch":
		return nil
	case "git.pull":
		return []string{"fast_forward_only"}
	case "git.worktree.create":
		return []string{"mode", "branch", "base"}
	case "git.worktree.remove":
		return []string{"confirm_discard"}
	case "git.push":
		return []string{"set_upstream"}
	case "git.commit":
		return []string{"message"}
	case "git.rebase", "git.merge":
		return []string{"target"}
	case "git.stage", "git.unstage":
		return []string{"paths"}
	case "git.operation.continue", "git.operation.abort":
		return []string{"operation_id"}
	default:
		return nil
	}
}

func requestArguments(w http.ResponseWriter, r *http.Request, allowed ...string) (json.RawMessage, bool) {
	if r.Body == nil {
		return json.RawMessage(`{}`), true
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
	dec := json.NewDecoder(r.Body)
	var values map[string]json.RawMessage
	if err := dec.Decode(&values); err != nil {
		if err == io.EOF {
			return json.RawMessage(`{}`), true
		}
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeAPIError(w, http.StatusRequestEntityTooLarge, "request_too_large", "request body is too large")
			return nil, false
		}
		writeAPIError(w, http.StatusBadRequest, "invalid_json", "request body must be one JSON object")
		return nil, false
	}
	if values == nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", "request body must be a JSON object")
		return nil, false
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", "request body must contain exactly one JSON value")
		return nil, false
	}

	allowedSet := make(map[string]struct{}, len(allowed))
	for _, key := range allowed {
		allowedSet[key] = struct{}{}
	}
	for key, raw := range values {
		if _, ok := allowedSet[key]; !ok {
			writeAPIError(w, http.StatusBadRequest, "unknown_field", fmt.Sprintf("unknown JSON field %q", key))
			return nil, false
		}
		if len(raw) > 64<<10 {
			writeAPIError(w, http.StatusBadRequest, "invalid", fmt.Sprintf("field %q is too large", key))
			return nil, false
		}
	}
	data, err := json.Marshal(values)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "invalid_json", "invalid request body")
		return nil, false
	}
	return data, true
}

func worktreeID(r *http.Request, enabled bool) string {
	if enabled {
		return r.PathValue("id")
	}
	return ""
}

func mustJSON(value any) json.RawMessage {
	data, _ := json.Marshal(value)
	return data
}

func randomID() string {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(raw)
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
	case "too_large":
		status = http.StatusRequestEntityTooLarge
	case "internal":
		status = http.StatusInternalServerError
	}
	writeAPIError(w, status, e.Code, e.Message)
}

// Only repository identities cross the public/daemon boundary. Operation IDs,
// worktree IDs and file contents must remain byte-for-byte untouched.
func publicGitPayload(kind, id string, raw json.RawMessage) (json.RawMessage, error) {
	switch kind {
	case "git.repository.refresh":
		var value domain.RepositoryState
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, err
		}
		value.ID = id
		for i := range value.Worktrees {
			value.Worktrees[i].RepositoryID = id
		}
		return json.Marshal(value)
	case "git.worktrees":
		var value []domain.Worktree
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, err
		}
		for i := range value {
			value[i].RepositoryID = id
		}
		return json.Marshal(value)
	case "git.worktree.create":
		var value domain.Worktree
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, err
		}
		value.RepositoryID = id
		return json.Marshal(value)
	default:
		return raw, nil
	}
}
