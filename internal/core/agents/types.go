package agents

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

type ProviderID string
type AccountID string
type SessionID string

type Account struct {
	ID        AccountID       `json:"id"`
	Provider  ProviderID      `json:"provider"`
	Name      string          `json:"name"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
	Settings  json.RawMessage `json:"settings,omitempty"`
}

type Session struct {
	ID         SessionID
	Provider   ProviderID
	AccountID  AccountID
	WorkDir    string
	RuntimeDir string
	HomeDir    string
	CreatedAt  time.Time
}

func NewAccountID() (AccountID, error) {
	id, err := randomID("acct_")
	return AccountID(id), err
}

func NewSessionID() (SessionID, error) {
	id, err := randomID("sess_")
	return SessionID(id), err
}

func randomID(prefix string) (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate id: %w", err)
	}
	return prefix + hex.EncodeToString(b[:]), nil
}
