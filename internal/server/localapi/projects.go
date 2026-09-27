package localapi

import (
	"net/http"

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
	mux.HandleFunc("GET /api/projects", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, s.registry.List())
	})
}
