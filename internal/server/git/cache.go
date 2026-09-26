package git

import (
	"context"
	"encoding/json"
	"time"
)

type cachedRead struct {
	Payload json.RawMessage
	Expires time.Time
}

// Cached reads are already repository-authorized and use installation identity.
// Serialize misses so simultaneous browser renders do not multiply API calls.
func (s *Service) cached(ctx context.Context, key string, read func() (any, error)) (any, error) {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	if v, ok := s.readCache[key]; ok && time.Now().Before(v.Expires) {
		return v.Payload, nil
	}
	v, e := read()
	if e != nil {
		return nil, e
	}
	raw, e := json.Marshal(v)
	if e != nil {
		return nil, e
	}
	if s.readCache == nil {
		s.readCache = map[string]cachedRead{}
	}
	s.readCache[key] = cachedRead{Payload: raw, Expires: time.Now().Add(30 * time.Second)}
	return raw, nil
}
