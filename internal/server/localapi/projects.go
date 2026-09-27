package localapi

import (
	"net/http"
	"strings"

	"github.com/Tiago-0liveira/bonsai/internal/daemon/client"
	githubdomain "github.com/Tiago-0liveira/bonsai/internal/git/github"
	"github.com/Tiago-0liveira/bonsai/internal/git/github/ghcli"
)

type ProjectInfo struct {
	ID string `json:"id"`
}

type projectServices struct {
	info   ProjectInfo
	daemon daemonClient
	github githubdomain.GitHubService
}

type projectRegistry interface {
	Default() projectServices
	List() []ProjectInfo
}

type staticProjectRegistry struct {
	project projectServices
}

func newStaticProjectRegistry(repoDir string) projectRegistry {
	return &staticProjectRegistry{project: projectServices{
		info:   ProjectInfo{ID: localRepositoryID},
		daemon: client.For(repoDir),
		github: ghcli.New(repoDir),
	}}
}

func (r *staticProjectRegistry) Default() projectServices { return r.project }

func (r *staticProjectRegistry) List() []ProjectInfo {
	return []ProjectInfo{r.project.info}
}

func (s *Server) registerProjectRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/projects", s.projects)
	mux.HandleFunc("GET /api/projects/{projectId}/git", s.projectSnapshot)
	mux.HandleFunc("PATCH /api/worktrees/{id}/metadata", s.patchWorktreeMetadata)
}

func (s *Server) projects(w http.ResponseWriter, r *http.Request) {
	local, err := s.readLocalRepository()
	if err != nil {
		writeAPIError(w, http.StatusServiceUnavailable, "daemon_unavailable", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, []browserRepository{s.projectDescriptor(r.Context(), local)})
}

func (s *Server) projectSnapshot(w http.ResponseWriter, r *http.Request) {
	if !s.requireLocalProject(w, r) {
		return
	}
	snapshot, err := s.browserSnapshot(r.Context())
	if err != nil {
		writeAPIError(w, http.StatusServiceUnavailable, "snapshot_unavailable", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, snapshot)
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
	current := s.metadataSnapshot()[id]
	current.WorktreeID = id
	current.RepositoryID = localRepositoryID
	if patch.MergeTargetBranch != nil {
		if len(*patch.MergeTargetBranch) > 255 {
			writeAPIError(w, http.StatusBadRequest, "invalid", "merge target branch is too long")
			return
		}
		current.MergeTargetBranch = *patch.MergeTargetBranch
	}
	if patch.Tag != nil {
		if len(*patch.Tag) > 100 {
			writeAPIError(w, http.StatusBadRequest, "invalid", "tag is too long")
			return
		}
		current.Tag = *patch.Tag
	}
	if patch.StackPreference != nil {
		if *patch.StackPreference != "auto" && *patch.StackPreference != "never" {
			writeAPIError(w, http.StatusBadRequest, "invalid", "stack preference must be auto or never")
			return
		}
		current.StackPreference = *patch.StackPreference
	}
	if err := s.putMetadata(current); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "metadata_write_failed", err.Error())
		return
	}
	s.publishProjectEvent(id)
	writeJSON(w, http.StatusOK, current)
}

func (s *Server) requireLocalProject(w http.ResponseWriter, r *http.Request) bool {
	if projectID := r.PathValue("projectId"); projectID != "" && projectID != localRepositoryID {
		writeAPIError(w, http.StatusNotFound, "not_found", "project not found")
		return false
	}
	return true
}
