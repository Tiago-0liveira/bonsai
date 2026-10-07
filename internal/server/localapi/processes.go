package localapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/core/pkgmgr"
	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
	domain "github.com/Tiago-0liveira/bonsai/internal/git"
)

func (s *Server) registerProcessRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/projects/{projectId}/process-commands", s.processCommands)
	mux.HandleFunc("GET /api/projects/{projectId}/processes/{id}/terminal", s.processTerminal)
	mux.HandleFunc("POST /api/projects/{projectId}/processes", s.processStart)
	mux.HandleFunc("POST /api/projects/{projectId}/processes/{id}/remove", s.processRemove)
	mux.HandleFunc("POST /api/projects/{projectId}/process-preview", s.processPreview)
	mux.HandleFunc("GET /api/projects/{projectId}/processes", s.processes)
	mux.HandleFunc("GET /api/projects/{projectId}/processes/{id}/logs", s.processLogs)
	mux.HandleFunc("POST /api/projects/{projectId}/processes/{id}/restart", s.processRestart)
	mux.HandleFunc("DELETE /api/projects/{projectId}/processes/{id}", s.processStop)
	mux.HandleFunc("GET /api/processes", s.processes)
	mux.HandleFunc("GET /api/processes/{id}/logs", s.processLogs)
	mux.HandleFunc("POST /api/processes/{id}/restart", s.processRestart)
	mux.HandleFunc("DELETE /api/processes/{id}", s.processStop)
}

func (s *Server) processCatalog(r *http.Request, worktreeID string) (*pkgmgr.Project, domain.Worktree, error) {
	project := s.registry.Default()
	payload, err := s.stateSync.gitPayload(r.Context(), project, "git.worktrees", func(raw json.RawMessage) (any, error) {
		var trees []domain.Worktree
		err := json.Unmarshal(raw, &trees)
		return trees, err
	})
	if err != nil {
		return nil, domain.Worktree{}, fmt.Errorf("cannot verify worktree: %w", err)
	}
	for _, tree := range payload.([]domain.Worktree) {
		if tree.ID != worktreeID {
			continue
		}
		if tree.Missing || tree.Path == "" {
			return nil, tree, fmt.Errorf("worktree directory is unavailable")
		}
		cfg, err := config.Load(project.info.Path)
		if err != nil {
			return nil, tree, err
		}
		catalog, err := pkgmgr.Discover(tree.Path, pkgmgr.Options{SearchDepth: &cfg.PkgMgr.SearchDepth})
		return catalog, tree, err
	}
	return nil, domain.Worktree{}, fmt.Errorf("worktree no longer belongs to project")
}

func (s *Server) processCommands(w http.ResponseWriter, r *http.Request) {
	catalog, _, err := s.processCatalog(r, r.URL.Query().Get("worktree_id"))
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "process_discovery_failed", err.Error())
		return
	}
	cfg, err := config.Load(s.registry.Default().info.Path)
	if err != nil {
		writeAPIError(w, 400, "process_policy_failed", err.Error())
		return
	}
	defaults := map[string]procstore.Policy{}
	for _, command := range catalog.Commands {
		invocation, _ := pkgmgr.Resolve(command, nil)
		display := strings.Join(append([]string{invocation.Program}, invocation.Args...), " ")
		defaults[command.ID] = cfg.PolicyFor(command.Name, display)
	}
	writeJSON(w, http.StatusOK, struct {
		*pkgmgr.Project
		DefaultPolicies map[string]procstore.Policy `json:"default_policies"`
	}{catalog, defaults})
}

// Only catalog IDs and argument values come from the browser. Re-discover the
// command before launch so execution always uses Bonsai's current invocation.
func (s *Server) processStart(w http.ResponseWriter, r *http.Request) {
	var input struct {
		WorktreeID string                `json:"worktree_id"`
		CommandID  string                `json:"command_id"`
		Values     pkgmgr.ArgumentValues `json:"values,omitempty"`
		Policy     *procstore.Policy     `json:"policy,omitempty"`
	}
	if !decodeStrictJSON(w, r, &input) {
		return
	}
	if input.Policy != nil {
		if err := procstore.ValidatePolicy(*input.Policy); err != nil {
			writeAPIError(w, 400, "invalid_policy", err.Error())
			return
		}
	}
	catalog, tree, err := s.processCatalog(r, input.WorktreeID)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "process_discovery_failed", err.Error())
		return
	}
	for _, command := range catalog.Commands {
		if command.ID != input.CommandID {
			continue
		}
		invocation, err := pkgmgr.Resolve(command, input.Values)
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, "invalid_arguments", err.Error())
			return
		}
		launcher, ok := s.registry.Default().daemon.(interface {
			SpawnExec(string, string, string, string, string, []string, *procstore.Policy) (*procstore.Record, error)
		})
		if !ok {
			writeAPIError(w, http.StatusServiceUnavailable, "process_start_unavailable", "Process launcher unavailable")
			return
		}
		record, err := launcher.SpawnExec(tree.Path, tree.Branch, invocation.Dir, command.Name, invocation.Program, invocation.Args, input.Policy)
		if err != nil {
			writeAPIError(w, http.StatusBadRequest, "process_start_failed", err.Error())
			return
		}
		s.stateSync.Queue(s.registry.Default().info.ID, refreshProcesses, false)
		writeJSON(w, http.StatusCreated, processSummary(s.registry.Default().info.ID, record))
		return
	}
	writeAPIError(w, http.StatusConflict, "command_unavailable", "Command no longer exists. Reload the package list.")
}

func (s *Server) processes(w http.ResponseWriter, _ *http.Request) {
	records, err := s.registry.Default().daemon.List()
	if err != nil {
		writeAPIError(w, http.StatusServiceUnavailable, "daemon_unavailable", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, records)
}

func (s *Server) processRestart(w http.ResponseWriter, r *http.Request) {
	id, ok := processID(w, r)
	if !ok {
		return
	}
	record, err := s.registry.Default().daemon.Restart(id)
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "process_restart_failed", err.Error())
		return
	}
	s.stateSync.Queue(s.registry.Default().info.ID, refreshProcesses, false)
	writeJSON(w, http.StatusOK, processSummary(s.registry.Default().info.ID, record))
}

func (s *Server) processStop(w http.ResponseWriter, r *http.Request) {
	id, ok := processID(w, r)
	if !ok {
		return
	}
	killed, err := s.registry.Default().daemon.Kill(id, false, "")
	if err != nil {
		writeAPIError(w, http.StatusBadRequest, "process_stop_failed", err.Error())
		return
	}
	s.stateSync.Queue(s.registry.Default().info.ID, refreshProcesses, false)
	writeJSON(w, http.StatusOK, map[string]any{"killed": killed})
}

func (s *Server) processLogs(w http.ResponseWriter, r *http.Request) {
	id, ok := processID(w, r)
	if !ok {
		return
	}
	lines := 200
	if value := r.URL.Query().Get("n"); value != "" {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 0 || parsed > 5000 {
			writeAPIError(w, http.StatusBadRequest, "invalid", "n must be between 0 and 5000")
			return
		}
		lines = parsed
	}
	var out strings.Builder
	if err := s.registry.Default().daemon.Logs(id, false, lines, "", false, func(chunk string) error {
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

func (s *Server) processPreview(w http.ResponseWriter, r *http.Request) {
	var input struct {
		WorktreeID string                `json:"worktree_id"`
		CommandID  string                `json:"command_id"`
		Values     pkgmgr.ArgumentValues `json:"values"`
	}
	if !decodeStrictJSON(w, r, &input) {
		return
	}
	catalog, _, err := s.processCatalog(r, input.WorktreeID)
	if err != nil {
		writeAPIError(w, 400, "process_discovery_failed", err.Error())
		return
	}
	for _, command := range catalog.Commands {
		if command.ID != input.CommandID {
			continue
		}
		invocation, err := pkgmgr.Resolve(command, input.Values)
		if err != nil {
			writeAPIError(w, 400, "invalid_arguments", err.Error())
			return
		}
		cfg, err := config.Load(s.registry.Default().info.Path)
		if err != nil {
			writeAPIError(w, 400, "process_policy_failed", err.Error())
			return
		}
		policy := cfg.PolicyFor(command.Name, strings.Join(append([]string{invocation.Program}, invocation.Args...), " "))
		writeJSON(w, 200, map[string]any{"program": invocation.Program, "args": invocation.Args, "dir": invocation.Dir, "default_policy": policy})
		return
	}
	writeAPIError(w, 409, "command_unavailable", "Command no longer exists. Reload the package list.")
}

func (s *Server) processRemove(w http.ResponseWriter, r *http.Request) {
	id, ok := processID(w, r)
	if !ok {
		return
	}
	var input struct {
		StopFirst bool `json:"stop_first"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeAPIError(w, 400, "invalid", "Expected stop_first JSON option")
		return
	}
	project := s.registry.Default()
	daemon, ok := project.daemon.(interface {
		RemoveExecution(int, bool) (procstore.ProcessVisibility, error)
	})
	if !ok {
		writeAPIError(w, 503, "daemon_unavailable", "Daemon lacks deletion support")
		return
	}
	s.stateSync.processAuthorityMu.Lock()
	visibility, err := daemon.RemoveExecution(id, input.StopFirst)
	if err == nil {
		s.stateSync.commitProject(project, "processes", func(snapshot *browserSnapshot) {
			processes := make([]browserProcessSummary, 0, len(snapshot.Processes))
			for _, process := range snapshot.Processes {
				if process.DaemonID != id {
					processes = append(processes, process)
				}
			}
			snapshot.Processes = processes
			snapshot.ProcessVisibility = visibility
		})
	}
	s.stateSync.processAuthorityMu.Unlock()
	s.stateSync.Queue(project.info.ID, refreshProcesses, false)
	if err != nil {
		writeAPIError(w, 400, "process_remove_failed", err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"id": project.info.ID + ":" + strconv.Itoa(id), "process_visibility": visibility})
}
