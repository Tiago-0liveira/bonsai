package localapi

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
)

type rootSettingsResponse struct {
	Version     int                  `json:"version"`
	Revision    uint64               `json:"revision"`
	Roots       []config.ProjectRoot `json:"roots"`
	Diagnostics []RootDiagnostic     `json:"diagnostics"`
	Suggestions []string             `json:"suggestions"`
}

func (s *Server) registerSettingsRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/settings/project-roots", s.rootSettings)
	mux.HandleFunc("POST /api/settings/project-roots", s.changeRootSettings)
	mux.HandleFunc("DELETE /api/settings/project-roots/{rootId}", s.changeRootSettings)
}
func (s *Server) rootSettingsValue(cfg config.ProjectRoots) rootSettingsResponse {
	out := rootSettingsResponse{Version: cfg.Version, Revision: cfg.Revision, Roots: cfg.Roots, Diagnostics: []RootDiagnostic{}, Suggestions: []string{}}
	scans := map[string]RootDiagnostic{}
	if registry, ok := s.registry.(*discoveredProjectRegistry); ok {
		for _, d := range registry.Diagnostics() {
			scans[d.RootID] = d
		}
	}
	for _, root := range cfg.Roots {
		d, ok := scans[root.ID]
		if !ok {
			d = RootDiagnostic{RootID: root.ID, Available: true, Messages: []string{}}
		}
		if _, err := config.CanonicalDirectory(root.Path); err != nil {
			d.Available = false
			d.Messages = []string{err.Error()}
		}
		out.Diagnostics = append(out.Diagnostics, d)
	}
	home, _ := os.UserHomeDir()
	paths := []string{s.repoDir, filepath.Dir(s.repoDir)}
	if home != "" {
		for _, name := range []string{"projects", "dev", "code"} {
			paths = append(paths, filepath.Join(home, name))
		}
	}
	seen := map[string]bool{}
	for _, path := range paths {
		canonical, err := config.CanonicalDirectory(path)
		if err == nil && !seen[canonical] {
			seen[canonical] = true
			out.Suggestions = append(out.Suggestions, canonical)
		}
	}
	return out
}
func (s *Server) rootSettings(w http.ResponseWriter, r *http.Request) {
	cfg, err := config.ReadProjectRoots(s.rootsPath)
	if err != nil {
		writeAPIError(w, 503, "settings_unavailable", err.Error())
		return
	}
	writeJSON(w, 200, s.rootSettingsValue(cfg))
}
func (s *Server) changeRootSettings(w http.ResponseWriter, r *http.Request) {
	var path string
	var revision *uint64
	if r.Method == http.MethodPost {
		var body struct {
			Path     string  `json:"path"`
			Revision *uint64 `json:"revision"`
		}
		if !decodeStrictJSON(w, r, &body) {
			return
		}
		path = body.Path
		revision = body.Revision
	} else {
		var body struct {
			Revision *uint64 `json:"revision"`
		}
		if !decodeStrictJSON(w, r, &body) {
			return
		}
		revision = body.Revision
	}
	if revision == nil {
		writeAPIError(w, 400, "invalid", "revision is required")
		return
	}
	cfg, err := config.UpdateProjectRoots(s.rootsPath, r.Header.Get("Idempotency-Key"), *revision, path, r.PathValue("rootId"))
	if err != nil {
		status := 400
		if errors.Is(err, config.ErrRootRevision) || errors.Is(err, config.ErrRootIdempotency) {
			status = 409
		}
		writeAPIError(w, status, "settings_update_failed", err.Error())
		return
	}
	// Reconciliation failure must not turn a committed settings transaction into
	// an ambiguous save failure; diagnostics and the next periodic scan recover.
	if err := s.Reconcile(r.Context()); err != nil {
		s.eventHub.publish(localEvent{Type: "catalog"})
	}
	writeJSON(w, 200, s.rootSettingsValue(cfg))
}
