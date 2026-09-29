package localapi

import (
	"context"
	gitstore "github.com/Tiago-0liveira/bonsai/internal/storage/git"
	"net/http"
	"strings"

	githubdomain "github.com/Tiago-0liveira/bonsai/internal/git/github"
)

type ProjectInfo struct {
	ID            string `json:"id"`
	RootID        string `json:"root_id"`
	Name          string `json:"name"`
	Path          string `json:"path"`
	Available     bool   `json:"available"`
	Launch        bool   `json:"launch"`
	WorkspaceID   string `json:"workspace_id"`
	FullName      string `json:"full_name"`
	DefaultBranch string `json:"default_branch"`
}

type projectServices struct {
	state  *gitstore.Store
	info   ProjectInfo
	daemon daemonClient
	github githubdomain.GitHubService
}

type projectRegistry interface {
	Default() projectServices
	Lookup(string) (projectServices, bool)
	Worktree(context.Context, string) (projectServices, bool)
	Refresh(context.Context) (bool, error)
	List() []ProjectInfo
}

type scopedProjectRegistry struct {
	project projectServices
}

func (r *scopedProjectRegistry) Default() projectServices { return r.project }

func (r *scopedProjectRegistry) List() []ProjectInfo {
	return []ProjectInfo{r.project.info}
}

func (s *Server) registerProjectRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/projects", s.projects)
	mux.HandleFunc("GET /api/projects/{projectId}/git", s.projectSnapshot)
	mux.HandleFunc("POST /api/projects/{projectId}/refresh", s.projectRefresh)
	mux.HandleFunc("PATCH /api/worktrees/{id}/metadata", s.patchWorktreeMetadata)
}

func (s *Server) projects(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.registry.List())
}

func (s *Server) projectSnapshot(w http.ResponseWriter, r *http.Request) {
	if !s.requireLocalProject(w, r) {
		return
	}
	projectID := s.registry.Default().info.ID
	snapshot, ok := s.stateSync.Snapshot(projectID)
	if !ok {
		writeAPIError(w, http.StatusNotFound, "not_found", "project not found")
		return
	}
	writeJSON(w, http.StatusOK, snapshot)
}

func (s *Server) projectRefresh(w http.ResponseWriter, r *http.Request) {
	if !s.requireLocalProject(w, r) {
		return
	}
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" || len(key) > 128 {
		writeAPIError(w, http.StatusBadRequest, "invalid", "Idempotency-Key is required for refresh requests")
		return
	}
	var input struct {
		Scope string `json:"scope"`
	}
	if !decodeStrictJSON(w, r, &input) {
		return
	}
	var scope refreshScope
	switch input.Scope {
	case "local":
		scope = refreshLocal | refreshProcesses
	case "provider":
		scope = refreshProvider
	case "all", "":
		scope = refreshAll
	default:
		writeAPIError(w, http.StatusBadRequest, "invalid", "refresh scope must be local, provider, or all")
		return
	}
	projectID := s.registry.Default().info.ID
	s.stateSync.MarkStale(projectID, scope)
	s.stateSync.Queue(projectID, scope, scope&refreshProvider != 0)
	snapshot, ok := s.stateSync.CachedSnapshot(projectID)
	if !ok {
		writeAPIError(w, http.StatusNotFound, "not_found", "project not found")
		return
	}
	writeJSON(w, http.StatusAccepted, snapshot)
}

func (s *Server) patchWorktreeMetadata(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		writeAPIError(w, http.StatusBadRequest, "invalid", "worktree id is required")
		return
	}
	var patch struct {
		MergeTargetBranch *string `json:"merge_target_branch"`
		Tag               *string `json:"tag"`
		StackPreference   *string `json:"stack_preference"`
	}
	if !decodeStrictJSON(w, r, &patch) {
		return
	}
	if patch.MergeTargetBranch != nil {
		if len(*patch.MergeTargetBranch) > 255 {
			writeAPIError(w, http.StatusBadRequest, "invalid", "merge target branch is too long")
			return
		}
	}
	if patch.Tag != nil {
		if len(*patch.Tag) > 100 {
			writeAPIError(w, http.StatusBadRequest, "invalid", "tag is too long")
			return
		}
	}
	if patch.StackPreference != nil {
		if *patch.StackPreference != "auto" && *patch.StackPreference != "never" {
			writeAPIError(w, http.StatusBadRequest, "invalid", "stack preference must be auto or never")
			return
		}
	}
	var current worktreeMetadata
	if err := s.state.Update(func(data gitstore.Data) error {
		current, _ = gitstore.Get[worktreeMetadata](data, "worktree_metadata", id)
		current.WorktreeID = id
		current.RepositoryID = localRepositoryID
		if patch.MergeTargetBranch != nil {
			current.MergeTargetBranch = *patch.MergeTargetBranch
		}
		if patch.Tag != nil {
			current.Tag = *patch.Tag
		}
		if patch.StackPreference != nil {
			current.StackPreference = *patch.StackPreference
		}
		return gitstore.Put(data, "worktree_metadata", id, current)
	}); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "metadata_write_failed", err.Error())
		return
	}
	s.stateSync.MarkStale(s.registry.Default().info.ID, refreshLocal)
	current.RepositoryID = s.registry.Default().info.ID
	writeJSON(w, http.StatusOK, current)
}

func (s *Server) requireLocalProject(w http.ResponseWriter, r *http.Request) bool {
	if projectID := r.PathValue("projectId"); projectID != "" && projectID != s.registry.Default().info.ID {
		writeAPIError(w, http.StatusNotFound, "not_found", "project not found")
		return false
	}
	return true
}

func (r *scopedProjectRegistry) Lookup(id string) (projectServices, bool) {
	return r.project, id == r.project.info.ID
}
func (r *scopedProjectRegistry) Worktree(_ context.Context, id string) (projectServices, bool) {
	return r.project, id != ""
}
func (r *scopedProjectRegistry) Refresh(context.Context) (bool, error) { return false, nil }
