package localapi

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

// usageRefreshInterval is how often one profile may bypass the usage cache.
const usageRefreshInterval = 30 * time.Second

// agentsChangedEvent tells pages showing profiles or sessions to revalidate.
const agentsChangedEvent = "agents_changed"

// usageThrottle remembers when each profile last bypassed the usage cache.
type usageThrottle struct {
	mu   sync.Mutex
	last map[agents.AccountID]time.Time
	now  func() time.Time
}

// allow reports whether account may refresh now, and records the refresh.
func (t *usageThrottle) allow(account agents.AccountID) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now()
	if t.now != nil {
		now = t.now()
	}
	if t.last == nil {
		t.last = map[agents.AccountID]time.Time{}
	}
	for id, at := range t.last {
		if now.Sub(at) >= usageRefreshInterval {
			delete(t.last, id)
		}
	}
	if _, recent := t.last[account]; recent {
		return false
	}
	t.last[account] = now
	return true
}

type agentUsageError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type agentUsageItem struct {
	AccountID agents.AccountID      `json:"account_id"`
	Provider  agents.ProviderID     `json:"provider"`
	Usage     *agents.UsageSnapshot `json:"usage,omitempty"`
	Headline  *agents.UsageHeadline `json:"headline,omitempty"`
	Error     *agentUsageError      `json:"error,omitempty"`
	// Throttled means a refresh was asked for too soon and the cache answered.
	Throttled bool `json:"throttled,omitempty"`
}

// usageError maps a usage failure to a code and a message that never carries a
// provider response or path. Unsupported reasons are provider-authored text.
func usageError(err error) *agentUsageError {
	switch {
	case errors.Is(err, agents.ErrUsageUnsupported):
		reason := strings.TrimPrefix(err.Error(), agents.ErrUsageUnsupported.Error()+": ")
		if reason == err.Error() || reason == "" {
			reason = "Usage is not supported for this profile"
		}
		return &agentUsageError{Code: "unsupported", Message: reason}
	case errors.Is(err, agents.ErrNotAuthenticated):
		return &agentUsageError{Code: "not_authenticated", Message: "Not signed in"}
	default:
		return &agentUsageError{Code: "unavailable", Message: "Usage is unavailable right now"}
	}
}

func usageItem(result agents.AccountUsageResult) agentUsageItem {
	item := agentUsageItem{AccountID: result.Account.ID, Provider: result.Account.Provider, Usage: result.Usage, Headline: result.Headline}
	if result.Error != nil {
		item.Usage, item.Headline, item.Error = nil, nil, usageError(result.Error)
	}
	return item
}

// agentUsage serves usage of every profile, or of one with ?account_id=. The
// usage service answers from its cache while it is fresh.
func (s *Server) agentUsage(w http.ResponseWriter, r *http.Request) {
	s.serveAgentUsage(w, r, agents.AccountID(r.URL.Query().Get("account_id")), false)
}

// agentUsageRefresh bypasses the cache at most once per usageRefreshInterval per
// profile; profiles asked too soon are answered from the cache with throttled.
func (s *Server) agentUsageRefresh(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AccountID agents.AccountID `json:"account_id,omitempty"`
	}
	if r.ContentLength != 0 && !decodeStrictJSON(w, r, &body) {
		return
	}
	s.serveAgentUsage(w, r, body.AccountID, true)
}

func (s *Server) serveAgentUsage(w http.ResponseWriter, r *http.Request, accountID agents.AccountID, refresh bool) {
	registry := s.agentRegistry()
	if registry == nil || s.agents.runtime.UsageService == nil {
		writeAPIError(w, 503, "agents_unavailable", "Agent runtime unavailable")
		return
	}
	service := s.agents.runtime.UsageService
	var throttled sync.Map
	options := func(account agents.Account) agents.UsageOptions {
		if !refresh {
			return agents.UsageOptions{}
		}
		if s.agents.usageThrottle.allow(account.ID) {
			return agents.UsageOptions{Refresh: true}
		}
		throttled.Store(account.ID, true)
		return agents.UsageOptions{}
	}
	mark := func(item agentUsageItem) agentUsageItem {
		_, item.Throttled = throttled.Load(item.AccountID)
		return item
	}

	if accountID != "" {
		account, err := s.agents.runtime.Accounts.Get(accountID)
		var provider agents.Provider
		if err == nil {
			provider, err = registry.Get(account.Provider)
		}
		if err != nil {
			writeAPIError(w, 404, "not_found", "Profile not found")
			return
		}
		result := agents.AccountUsageResult{Account: account}
		if !provider.Capabilities().Usage {
			result.Error = agents.ErrUsageUnsupported
		} else {
			snapshot, err := service.Account(r.Context(), account.ID, options(account))
			result.Error = err
			if err == nil {
				result.Usage, result.Headline = &snapshot, service.Headline(snapshot)
			}
		}
		writeJSON(w, 200, []agentUsageItem{mark(usageItem(result))})
		return
	}

	results := service.AllFunc(r.Context(), options)
	out := []agentUsageItem{}
	for _, result := range results {
		if result.Account.ID == "" {
			writeAPIError(w, 503, "profiles_unavailable", "Cannot read profiles")
			return
		}
		// Profiles of providers this build cannot run stay hidden, as in the list.
		if _, err := registry.Get(result.Account.Provider); err != nil {
			continue
		}
		out = append(out, mark(usageItem(result)))
	}
	writeJSON(w, 200, out)
}

// writeJSONWithETag answers with the body's hash as its ETag, or 304 when the
// browser already has that body.
func writeJSONWithETag(w http.ResponseWriter, r *http.Request, value any) {
	body, err := json.Marshal(value)
	if err != nil {
		writeAPIError(w, 500, "internal", "Cannot encode response")
		return
	}
	sum := sha256.Sum256(body)
	tag := `"` + hex.EncodeToString(sum[:16]) + `"`
	w.Header().Set("ETag", tag)
	for _, candidate := range strings.Split(r.Header.Get("If-None-Match"), ",") {
		if candidate = strings.TrimSpace(candidate); candidate == tag || candidate == "W/"+tag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(200)
	_, _ = w.Write(append(body, '\n'))
}

// publishAgentsChanged tells every page to revalidate profiles and sessions.
// It carries only a revision, never a project or payload.
func (s *Server) publishAgentsChanged() {
	if s.agents == nil || s.eventHub == nil {
		return
	}
	s.eventHub.publish(localEvent{Type: agentsChangedEvent, Revision: s.agents.revision.Add(1)})
}
