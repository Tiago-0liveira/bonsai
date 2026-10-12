package localapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/core/editors"
	domain "github.com/Tiago-0liveira/bonsai/internal/git"
)

type editorSettingsResponse struct {
	Environment    editors.Environment `json:"environment"`
	Revision       uint64              `json:"revision"`
	Editors        []editors.Editor    `json:"editors"`
	Preferred      string              `json:"preferred"`
	SavedPreferred string              `json:"saved_preferred"`
}

func (s *Server) registerEditorRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/settings/editors", s.editorSettings)
	mux.HandleFunc("POST /api/settings/editors", s.changeEditorSettings)
	mux.HandleFunc("POST /api/worktrees/{id}/open-editor", s.openFileEditor)
}

func (s *Server) editorSettingsValue(cfg config.EditorSettings) editorSettingsResponse {
	out := editorSettingsResponse{Environment: s.editors.Environment, Revision: cfg.Revision, Editors: s.editors.Editors(), SavedPreferred: cfg.Environments[s.editors.Environment.ID]}
	for _, editor := range out.Editors {
		if editor.ID == out.SavedPreferred {
			out.Preferred = editor.ID
		}
	}
	return out
}

func (s *Server) editorSettings(w http.ResponseWriter, r *http.Request) {
	cfg, err := config.ReadEditorSettings(s.rootsPath + ".editors.json")
	if err != nil {
		writeAPIError(w, 503, "settings_unavailable", "Cannot read editor settings: "+err.Error())
		return
	}
	writeJSON(w, 200, s.editorSettingsValue(cfg))
}

func (s *Server) changeEditorSettings(w http.ResponseWriter, r *http.Request) {
	var body struct {
		EnvironmentID string  `json:"environment_id"`
		Editor        string  `json:"editor"`
		Revision      *uint64 `json:"revision"`
	}
	if !decodeStrictJSON(w, r, &body) {
		return
	}
	if body.EnvironmentID != s.editors.Environment.ID {
		writeAPIError(w, 409, "environment_changed", "The environment changed. Refresh editor settings.")
		return
	}
	if body.Revision == nil {
		writeAPIError(w, 400, "invalid", "revision is required")
		return
	}
	found := body.Editor == ""
	for _, editor := range s.editors.Editors() {
		found = found || editor.ID == body.Editor
	}
	if !found {
		writeAPIError(w, 400, "editor_unavailable", "This editor is not installed in "+s.editors.Environment.Label+". Refresh editor settings.")
		return
	}
	cfg, err := config.UpdateEditorSettings(s.rootsPath+".editors.json", body.EnvironmentID, body.Editor, *body.Revision)
	if err != nil {
		status := 503
		if errors.Is(err, config.ErrEditorRevision) {
			status = 409
		}
		writeAPIError(w, status, "settings_update_failed", err.Error())
		return
	}
	writeJSON(w, 200, s.editorSettingsValue(cfg))
}

func (s *Server) openFileEditor(w http.ResponseWriter, r *http.Request) {
	var body struct {
		EnvironmentID string `json:"environment_id"`
		Path          string `json:"path"`
	}
	if !decodeStrictJSON(w, r, &body) {
		return
	}
	if body.EnvironmentID != s.editors.Environment.ID {
		writeAPIError(w, 409, "environment_changed", "The environment changed. Refresh editor settings.")
		return
	}
	if !filepath.IsLocal(body.Path) || strings.ContainsRune(body.Path, 0) {
		writeAPIError(w, 400, "invalid", "A file path within the selected worktree is required")
		return
	}
	cfg, err := config.ReadEditorSettings(s.rootsPath + ".editors.json")
	if err != nil {
		writeAPIError(w, 503, "settings_unavailable", "Cannot read editor settings: "+err.Error())
		return
	}
	settings := s.editorSettingsValue(cfg)
	var selected *editors.Editor
	for i := range settings.Editors {
		if settings.Editors[i].ID == settings.Preferred {
			selected = &settings.Editors[i]
			break
		}
	}
	if selected == nil {
		writeAPIError(w, 409, "editor_unavailable", "Choose an installed editor for "+settings.Environment.Label+" in Settings.")
		return
	}
	// Resolve the worktree through repository authority, not a browser-supplied
	// absolute path. Handler has already checked ownership of this worktree ID.
	result, ok := s.runGit(w, r, "git.worktrees", "", json.RawMessage(`{}`), false)
	if !ok {
		return
	}
	var worktrees []domain.Worktree
	if err := json.Unmarshal(result.Payload, &worktrees); err != nil {
		writeAPIError(w, 503, "worktree_unavailable", "Cannot read worktree paths")
		return
	}
	root := ""
	for _, worktree := range worktrees {
		if worktree.ID == r.PathValue("id") && !worktree.Missing {
			root = worktree.Path
			break
		}
	}
	if root == "" {
		writeAPIError(w, 404, "worktree_unavailable", "The selected worktree is unavailable")
		return
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		writeAPIError(w, 404, "worktree_unavailable", err.Error())
		return
	}
	file, err := filepath.EvalSymlinks(filepath.Join(root, body.Path))
	if err != nil {
		writeAPIError(w, 404, "file_unavailable", err.Error())
		return
	}
	if !config.ContainsPath(root, file) {
		writeAPIError(w, 400, "invalid", "The file resolves outside the selected worktree")
		return
	}
	info, err := os.Stat(file)
	if err != nil || !info.Mode().IsRegular() {
		writeAPIError(w, 400, "file_unavailable", "Select an existing regular file")
		return
	}
	if err := s.editors.Launch(*selected, root, file); err != nil {
		writeAPIError(w, 502, "editor_launch_failed", err.Error())
		return
	}
	writeJSON(w, 202, map[string]string{"editor": selected.Name, "environment": settings.Environment.Label})
}
