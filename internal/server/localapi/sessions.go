package localapi

import (
	"crypto/rand"
	"crypto/subtle"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"sync"
	"time"
)

const (
	sessionTTL = 15 * time.Minute
	maxSessions = 32
)

type session struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

type sessionEntry struct {
	digest    [32]byte
	expiresAt time.Time
	createdAt time.Time
}

type sessionStore struct {
	mu       sync.Mutex
	sessions []sessionEntry
	now      func() time.Time
}

func newSessionStore() *sessionStore {
	return &sessionStore{now: time.Now}
}

func (s *sessionStore) create() (session, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return session{}, fmt.Errorf("generate local session: %w", err)
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	now := s.now().UTC()
	entry := sessionEntry{
		digest: sha256.Sum256([]byte(token)),
		expiresAt: now.Add(sessionTTL),
		createdAt: now,
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.purgeExpiredLocked(now)
	if len(s.sessions) >= maxSessions {
		oldest := 0
		for i := 1; i < len(s.sessions); i++ {
			if s.sessions[i].createdAt.Before(s.sessions[oldest].createdAt) {
				oldest = i
			}
		}
		s.sessions = append(s.sessions[:oldest], s.sessions[oldest+1:]...)
	}
	s.sessions = append(s.sessions, entry)
	return session{Token: token, ExpiresAt: entry.expiresAt}, nil
}

func (s *sessionStore) valid(token string) bool {
	if token == "" {
		return false
	}
	digest := sha256.Sum256([]byte(token))
	now := s.now().UTC()

	s.mu.Lock()
	defer s.mu.Unlock()
	s.purgeExpiredLocked(now)
	for _, entry := range s.sessions {
		if subtle.ConstantTimeCompare(digest[:], entry.digest[:]) == 1 {
			return true
		}
	}
	return false
}

func (s *sessionStore) purgeExpiredLocked(now time.Time) {
	kept := s.sessions[:0]
	for _, entry := range s.sessions {
		if now.Before(entry.expiresAt) {
			kept = append(kept, entry)
		}
	}
	s.sessions = kept
}
