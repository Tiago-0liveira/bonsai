package ghcli

import (
	"net/http"
	"strconv"
	"sync"
	"time"
)

// Rate is the last primary (core) rate-limit window GitHub reported for a host.
type Rate struct {
	Limit     int
	Remaining int
	Reset     time.Time
}

// Low reports whether less than 10% of the window is left.
func (r Rate) Low() bool { return r.Limit > 0 && r.Remaining*10 < r.Limit }

// Exhausted reports whether no request is left before Reset.
func (r Rate) Exhausted(now time.Time) bool {
	return r.Limit > 0 && r.Remaining <= 0 && now.Before(r.Reset)
}

type rateTracker struct {
	mu    sync.Mutex
	hosts map[string]Rate
}

func newRateTracker() *rateTracker { return &rateTracker{hosts: map[string]Rate{}} }

func (t *rateTracker) observe(host string, h http.Header) {
	// GraphQL and search have their own windows; polling uses core REST.
	if resource := h.Get("X-RateLimit-Resource"); resource != "" && resource != "core" {
		return
	}
	limit, e1 := strconv.Atoi(h.Get("X-RateLimit-Limit"))
	remaining, e2 := strconv.Atoi(h.Get("X-RateLimit-Remaining"))
	reset, e3 := strconv.ParseInt(h.Get("X-RateLimit-Reset"), 10, 64)
	if e1 != nil || e2 != nil || e3 != nil || limit <= 0 {
		return
	}
	t.mu.Lock()
	t.hosts[host] = Rate{Limit: limit, Remaining: remaining, Reset: time.Unix(reset, 0).UTC()}
	t.mu.Unlock()
}

func (t *rateTracker) get(host string) (Rate, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	rate, ok := t.hosts[host]
	return rate, ok
}
