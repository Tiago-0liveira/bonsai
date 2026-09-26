package agents

import "time"

type sessionRecord struct {
	Version   int        `json:"version"`
	ID        SessionID  `json:"id"`
	Provider  ProviderID `json:"provider"`
	AccountID AccountID  `json:"account_id"`
	WorkDir   string     `json:"work_dir,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}
