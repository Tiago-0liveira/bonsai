package localapi

import (
	"net/http"
	"net/url"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
)

// updatesSettingsResponse is GET /api/settings/updates: how GitHub data
// reaches this API, read-only (setup changes it). It carries no secret and no
// hook URL: of the public URL only its host.
type updatesSettingsResponse struct {
	Mode                    string                `json:"mode"`
	StandardIntervalSeconds int                   `json:"standard_interval_seconds"`
	Live                    *liveSettingsResponse `json:"live"`
}

type liveSettingsResponse struct {
	Tunnel      string `json:"tunnel"`
	PublicHost  string `json:"public_host,omitempty"`
	TunnelUp    bool   `json:"tunnel_up"`
	TunnelError string `json:"tunnel_error,omitempty"`
	// SafetyPollSeconds is the polling interval of a project live updates
	// cover; the others keep the standard interval.
	SafetyPollSeconds int                      `json:"safety_poll_seconds"`
	Repositories      []liveRepositorySettings `json:"repositories"`
}

type liveRepositorySettings struct {
	FullName string `json:"full_name"`
	// State is a hook state (live, waiting_for_ping, failing, needs_admin,
	// scope_missing), or pending before the first reconcile.
	State          string     `json:"state"`
	Healthy        bool       `json:"healthy"`
	LastError      string     `json:"last_error,omitempty"`
	LastPingAt     *time.Time `json:"last_ping_at,omitempty"`
	LastDeliveryAt *time.Time `json:"last_delivery_at,omitempty"`
	CheckedAt      *time.Time `json:"checked_at,omitempty"`
	// ProjectIDs are the projects whose remote is this repository.
	ProjectIDs []string `json:"project_ids"`
}

func (s *Server) updateSettings(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.updateSettingsValue())
}

func (s *Server) updateSettingsValue() updatesSettingsResponse {
	out := updatesSettingsResponse{Mode: config.WebUpdatesStandard, StandardIntervalSeconds: int(StandardUpdateInterval / time.Second)}
	if s.liveHooks == nil {
		return out
	}
	view := s.liveHooks.view()
	out.Mode = config.WebUpdatesLive
	live := &liveSettingsResponse{
		Tunnel:            view.Tunnel,
		TunnelUp:          view.TunnelUp,
		TunnelError:       view.TunnelError,
		SafetyPollSeconds: int(providerLivePollInterval / time.Second),
		Repositories:      []liveRepositorySettings{},
	}
	if u, err := url.Parse(view.PublicURL); err == nil && view.PublicURL != "" {
		live.PublicHost = u.Hostname()
	}
	for _, r := range view.Repositories {
		state := r.State
		if state == "" {
			state = "pending"
		}
		row := liveRepositorySettings{
			FullName:       r.FullName,
			State:          state,
			Healthy:        view.TunnelUp && r.State == config.WebLiveStateLive,
			LastError:      r.LastError,
			LastPingAt:     timePointer(r.LastPingAt),
			LastDeliveryAt: timePointer(r.LastDeliveryAt),
			CheckedAt:      timePointer(r.CheckedAt),
			ProjectIDs:     []string{},
		}
		row.ProjectIDs = append(row.ProjectIDs, s.stateSync.liveProjectsByName(r.FullName)...)
		live.Repositories = append(live.Repositories, row)
	}
	out.Live = live
	return out
}

func timePointer(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}
