package localapi

import (
	"errors"
	"net/http"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

// Bounds on a profile update body; real profiles have a handful of fields.
const (
	maxProfileDefaults     = 32
	maxProfileFieldID      = 64
	maxProfileDefaultValue = 256
)

type agentProfilePatch struct {
	Name *string `json:"name,omitempty"`
	// Defaults maps profile field IDs to values; null returns a field to the
	// provider default.
	Defaults map[string]*string `json:"defaults,omitempty"`
}

// agentProfile resolves the {id} path value to a profile of a registered
// provider, or writes the error.
func (s *Server) agentProfile(w http.ResponseWriter, r *http.Request) (*agents.Registry, agents.Account, bool) {
	registry := s.agentRegistry()
	if registry == nil {
		writeAPIError(w, 503, "agents_unavailable", "Agent runtime unavailable")
		return nil, agents.Account{}, false
	}
	account, err := s.agents.runtime.Accounts.Get(agents.AccountID(r.PathValue("id")))
	if err == nil {
		_, err = registry.Get(account.Provider)
	}
	if err != nil {
		writeProfileError(w, err)
		return nil, agents.Account{}, false
	}
	return registry, account, true
}

// agentAccount describes one profile, re-running the provider's describe.
func (s *Server) agentAccount(w http.ResponseWriter, r *http.Request) {
	registry, account, ok := s.agentProfile(w, r)
	if !ok {
		return
	}
	writeJSON(w, 200, accountItem(registry, account, registry.DescribeAccount(r.Context(), account)))
}

// updateAgentAccount renames a profile and/or changes its launch defaults in
// one write, and answers with the re-described profile.
func (s *Server) updateAgentAccount(w http.ResponseWriter, r *http.Request) {
	var body agentProfilePatch
	if !decodeStrictJSON(w, r, &body) {
		return
	}
	if body.Name == nil && len(body.Defaults) == 0 {
		writeAPIError(w, 400, "invalid", "Nothing to change: send name or defaults")
		return
	}
	if body.Name != nil && (len(*body.Name) > 64 || agents.ValidateAccountName(*body.Name) != nil) {
		writeProfileError(w, agents.ErrInvalidAccountName)
		return
	}
	if len(body.Defaults) > maxProfileDefaults {
		writeAPIError(w, 400, "invalid", "Too many profile defaults")
		return
	}
	for id, value := range body.Defaults {
		if id == "" || len(id) > maxProfileFieldID || (value != nil && len(*value) > maxProfileDefaultValue) {
			writeAPIError(w, 400, "invalid", "Invalid profile default")
			return
		}
	}
	registry, account, ok := s.agentProfile(w, r)
	if !ok {
		return
	}
	service := s.agents.runtime.AccountService
	if service == nil {
		writeAPIError(w, 503, "agents_unavailable", "Agent runtime unavailable")
		return
	}
	update := agents.ProfileUpdate{Defaults: body.Defaults}
	if body.Name != nil {
		update.Name = *body.Name
	}
	updated, err := service.UpdateProfile(r.Context(), account.ID, update)
	if err != nil {
		writeProfileError(w, err)
		return
	}
	s.publishAgentsChanged()
	writeJSON(w, 200, accountItem(registry, updated, registry.DescribeAccount(r.Context(), updated)))
}

// removeAgentAccount deletes a profile unless one of its sessions is active in
// any project or a sign-in for it runs. Holding the agents lock keeps API
// starts out until it is done, and the setup key lock keeps sign-ins out.
func (s *Server) removeAgentAccount(w http.ResponseWriter, r *http.Request) {
	if r.ContentLength != 0 {
		writeAPIError(w, 400, "invalid", "Remove does not accept a request body")
		return
	}
	_, account, ok := s.agentProfile(w, r)
	if !ok {
		return
	}
	service := s.agents.runtime.AccountService
	if service == nil {
		writeAPIError(w, 503, "agents_unavailable", "Agent runtime unavailable")
		return
	}
	s.agents.mu.Lock()
	s.agents.setupKeys.mu.Lock()
	if s.signingIn(account) {
		s.agents.setupKeys.mu.Unlock()
		s.agents.mu.Unlock()
		writeAPIError(w, 409, "setup_busy", "A sign-in for this profile is running; finish or cancel it first")
		return
	}
	warnings, err := service.Remove(r.Context(), account.ID, agents.RemoveOptions{InUse: s.agents.manager.ActiveSessions})
	s.agents.setupKeys.mu.Unlock()
	s.agents.mu.Unlock()
	if err != nil {
		writeProfileError(w, err)
		return
	}
	if warnings == nil {
		warnings = []string{}
	}
	s.publishAgentsChanged()
	writeJSON(w, 200, map[string]any{"warnings": warnings})
}

// signingIn reports whether a sign-in-again job for account runs. Its login
// writes into the profile directory, so removing the profile would orphan
// credentials there. (A profile being created is stored only once it is done.)
func (s *Server) signingIn(account agents.Account) bool {
	if s.agents.setup == nil {
		return false
	}
	for _, job := range s.agents.setup.List() {
		if job.Active() && job.AccountID == account.ID {
			return true
		}
	}
	return false
}

// writeProfileError maps account errors to API errors. Only a provider's
// validation message is passed through; anything else gets a generic message.
func writeProfileError(w http.ResponseWriter, err error) {
	var inUse *agents.AccountInUseError
	switch {
	case errors.As(err, &inUse):
		writeJSON(w, 409, map[string]any{"error": map[string]any{
			"code":        "profile_in_use",
			"message":     "Stop the profile's running sessions before removing it",
			"session_ids": inUse.SessionIDs,
		}})
	case errors.Is(err, agents.ErrAccountNotFound), errors.Is(err, agents.ErrProviderNotFound):
		writeAPIError(w, 404, "not_found", "Profile not found")
	case errors.Is(err, agents.ErrInvalidAccountName):
		writeAPIError(w, 400, "invalid_name", "Names start with a letter or digit, then use letters, digits, '.', '_' or '-' (at most 64)")
	case errors.Is(err, agents.ErrAccountExists):
		writeAPIError(w, 409, "name_taken", "Another profile of this provider already has this name")
	case errors.Is(err, agents.ErrInvalidProfile):
		writeAPIError(w, 400, "invalid_profile", err.Error())
	default:
		writeAPIError(w, 503, "profiles_unavailable", "Cannot update profiles")
	}
}
