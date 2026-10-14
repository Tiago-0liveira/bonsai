package localapi

import (
	"errors"
	"net/http"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
)

// agentSettingsResponse is GET /api/settings/agents. ActiveSessions counts the
// running agent sessions in every project, so the page can say what a stop
// would end.
type agentSettingsResponse struct {
	Revision       uint64 `json:"revision"`
	KeepRunning    bool   `json:"keep_running"`
	ActiveSessions int    `json:"active_sessions"`
}

func (s *Server) registerAgentSettingsRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/settings/agents", s.agentSettings)
	mux.HandleFunc("POST /api/settings/agents", s.changeAgentSettings)
}

func (s *Server) agentSettingsValue(cfg config.WebConfig) agentSettingsResponse {
	out := agentSettingsResponse{Revision: cfg.Revision, KeepRunning: cfg.Agents.KeepRunning}
	if s.agents != nil {
		out.ActiveSessions = s.agents.manager.ActiveCount()
	}
	return out
}

func (s *Server) agentSettings(w http.ResponseWriter, _ *http.Request) {
	if s.webConfigPath == "" {
		writeAPIError(w, 503, "settings_unavailable", "Agent settings belong to bonsai web")
		return
	}
	cfg, _, err := config.ReadWebConfig(s.webConfigPath)
	if err != nil {
		writeAPIError(w, 503, "settings_unavailable", "Cannot read web settings: "+err.Error())
		return
	}
	writeJSON(w, 200, s.agentSettingsValue(cfg))
}

func (s *Server) changeAgentSettings(w http.ResponseWriter, r *http.Request) {
	var body struct {
		KeepRunning *bool   `json:"keep_running"`
		Revision    *uint64 `json:"revision"`
	}
	if !decodeStrictJSON(w, r, &body) {
		return
	}
	if body.Revision == nil || body.KeepRunning == nil {
		writeAPIError(w, 400, "invalid", "keep_running and revision are required")
		return
	}
	if s.webConfigPath == "" {
		writeAPIError(w, 503, "settings_unavailable", "Agent settings belong to bonsai web")
		return
	}
	cfg, err := config.UpdateWebConfig(s.webConfigPath, *body.Revision, func(cfg *config.WebConfig) error {
		cfg.Agents.KeepRunning = *body.KeepRunning
		return nil
	})
	if err != nil {
		status := 503
		if errors.Is(err, config.ErrWebConfigRevision) {
			status = 409
		}
		writeAPIError(w, status, "settings_update_failed", err.Error())
		return
	}
	writeJSON(w, 200, s.agentSettingsValue(cfg))
}
