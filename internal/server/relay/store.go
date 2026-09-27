package relay

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/server/webhooks"
)

const (
	defaultMaxEvents     = 10000
	defaultMaxDeliveries = 50000
	eventRetention       = 7 * 24 * time.Hour
	deliveryRetention    = 7 * 24 * time.Hour
)

var (
	ErrDeliveryConflict  = errors.New("delivery id reused with different payload")
	ErrUnauthorizedScope = errors.New("repository or installation is not authorized")
)

type RepositoryGrant struct {
	RepositoryID   int64  `json:"repository_id"`
	InstallationID int64  `json:"installation_id"`
	FullName       string `json:"full_name,omitempty"`
}

type RelaySession struct {
	GitHubUserID int64             `json:"github_user_id"`
	Repositories []RepositoryGrant `json:"repositories"`
	CreatedAt    time.Time         `json:"created_at"`
	ExpiresAt    time.Time         `json:"expires_at"`
}

type DeliveryRecord struct {
	DeliveryID    string         `json:"delivery_id"`
	PayloadDigest string         `json:"payload_digest"`
	ReceivedAt    time.Time      `json:"received_at"`
	Event         webhooks.Event `json:"event"`
	Sequence      uint64         `json:"sequence"`
}

type RelayEvent struct {
	ID                  string    `json:"id"`
	Sequence            uint64    `json:"sequence"`
	Event               string    `json:"event"`
	Action              string    `json:"action,omitempty"`
	RepositoryID        int64     `json:"repository_id,omitempty"`
	InstallationID      int64     `json:"installation_id,omitempty"`
	Number              int       `json:"number,omitempty"`
	PullRequestNumber   int       `json:"pull_request,omitempty"`
	PullRequestMerged   bool      `json:"pull_request_merged,omitempty"`
	IssueNumber         int       `json:"issue,omitempty"`
	Ref                 string    `json:"ref,omitempty"`
	CheckHeadSHA        string    `json:"check_head_sha,omitempty"`
	WorkflowHeadBranch  string    `json:"workflow_head_branch,omitempty"`
	RepositoriesAdded   []int64   `json:"repositories_added,omitempty"`
	RepositoriesRemoved []int64   `json:"repositories_removed,omitempty"`
	ReceivedAt          time.Time `json:"received_at"`
}

type diskState struct {
	OAuthStates map[string]time.Time      `json:"oauth_states"`
	Sessions    map[string]RelaySession   `json:"relay_sessions"`
	Deliveries  map[string]DeliveryRecord `json:"webhook_deliveries"`
	Events      []RelayEvent              `json:"relay_events"`
	Sequence    uint64                    `json:"sequence"`
}

type Store struct {
	mu            sync.Mutex
	path          string
	state         diskState
	maxEvents     int
	maxDeliveries int
}

func OpenStore(path string) (*Store, error) {
	s := &Store{
		path: path,
		state: diskState{
			OAuthStates: map[string]time.Time{},
			Sessions:    map[string]RelaySession{},
			Deliveries:  map[string]DeliveryRecord{},
			Events:      []RelayEvent{},
		},
		maxEvents:     defaultMaxEvents,
		maxDeliveries: defaultMaxDeliveries,
	}
	b, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err == nil && len(b) != 0 {
		if err := json.Unmarshal(b, &s.state); err != nil {
			return nil, err
		}
	}
	s.ensureMaps()
	return s, nil
}

func SecretHash(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

func PayloadDigest(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func (s *Store) ensureMaps() {
	if s.state.OAuthStates == nil {
		s.state.OAuthStates = map[string]time.Time{}
	}
	if s.state.Sessions == nil {
		s.state.Sessions = map[string]RelaySession{}
	}
	if s.state.Deliveries == nil {
		s.state.Deliveries = map[string]DeliveryRecord{}
	}
	if s.state.Events == nil {
		s.state.Events = []RelayEvent{}
	}
}

func (s *Store) persistLocked() error {
	if s.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return err
	}
	b, err := json.Marshal(s.state)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(s.path), ".relay-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(0600); err == nil {
		_, err = f.Write(b)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.Rename(name, s.path); err != nil {
		return err
	}
	return nil
}

func (s *Store) purgeLocked(now time.Time) {
	for key, expiry := range s.state.OAuthStates {
		if !now.Before(expiry) {
			delete(s.state.OAuthStates, key)
		}
	}
	for key, session := range s.state.Sessions {
		if !now.Before(session.ExpiresAt) {
			delete(s.state.Sessions, key)
		}
	}
	eventCutoff := now.Add(-eventRetention)
	first := 0
	for first < len(s.state.Events) && s.state.Events[first].ReceivedAt.Before(eventCutoff) {
		first++
	}
	if first > 0 {
		s.state.Events = append([]RelayEvent(nil), s.state.Events[first:]...)
	}
	if len(s.state.Events) > s.maxEvents {
		s.state.Events = append([]RelayEvent(nil), s.state.Events[len(s.state.Events)-s.maxEvents:]...)
	}
	deliveryCutoff := now.Add(-deliveryRetention)
	for key, d := range s.state.Deliveries {
		if d.ReceivedAt.Before(deliveryCutoff) {
			delete(s.state.Deliveries, key)
		}
	}
	if len(s.state.Deliveries) > s.maxDeliveries {
		type item struct {
			key string
			at  time.Time
		}
		items := make([]item, 0, len(s.state.Deliveries))
		for key, d := range s.state.Deliveries {
			items = append(items, item{key: key, at: d.ReceivedAt})
		}
		for len(items) > s.maxDeliveries {
			oldest := 0
			for i := 1; i < len(items); i++ {
				if items[i].at.Before(items[oldest].at) {
					oldest = i
				}
			}
			delete(s.state.Deliveries, items[oldest].key)
			items = append(items[:oldest], items[oldest+1:]...)
		}
	}
}

func (s *Store) PutOAuthState(hash string, expiry time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.purgeLocked(time.Now().UTC())
	s.state.OAuthStates[hash] = expiry.UTC()
	return s.persistLocked()
}

func (s *Store) ConsumeOAuthState(hash string, now time.Time) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	expiry, ok := s.state.OAuthStates[hash]
	if ok {
		delete(s.state.OAuthStates, hash)
	}
	s.purgeLocked(now.UTC())
	if !ok || !now.Before(expiry) {
		if ok {
			return false, s.persistLocked()
		}
		return false, nil
	}
	return true, s.persistLocked()
}

func (s *Store) PutSession(hash string, session RelaySession) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.purgeLocked(time.Now().UTC())
	s.state.Sessions[hash] = session
	return s.persistLocked()
}

func (s *Store) Session(hash string, now time.Time) (RelaySession, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.state.Sessions[hash]
	if !ok || !now.Before(session.ExpiresAt) {
		return RelaySession{}, false
	}
	return session, true
}

func (s *Store) DeleteSession(hash string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.state.Sessions, hash)
	return s.persistLocked()
}

func sessionAllows(session RelaySession, event RelayEvent) bool {
	for _, grant := range session.Repositories {
		if event.RepositoryID != 0 {
			if grant.RepositoryID == event.RepositoryID && grant.InstallationID == event.InstallationID {
				return true
			}
			continue
		}
		if event.InstallationID != 0 && grant.InstallationID == event.InstallationID {
			return true
		}
	}
	return false
}

func eventFromWebhook(event webhooks.Event, sequence uint64, receivedAt time.Time) RelayEvent {
	return RelayEvent{
		ID: event.DeliveryID, Sequence: sequence, Event: event.Event, Action: event.Action,
		RepositoryID: event.RepositoryID, InstallationID: event.InstallationID,
		Number: event.Number, PullRequestNumber: event.PullRequestNumber,
		PullRequestMerged: event.PullRequestMerged, IssueNumber: event.IssueNumber,
		Ref: event.Ref, CheckHeadSHA: event.CheckHeadSHA,
		WorkflowHeadBranch:  event.WorkflowHeadBranch,
		RepositoriesAdded:   append([]int64(nil), event.RepositoriesAdded...),
		RepositoriesRemoved: append([]int64(nil), event.RepositoriesRemoved...),
		ReceivedAt:          receivedAt.UTC(),
	}
}

func (s *Store) RecordWebhook(deliveryID, digest string, event webhooks.Event, now time.Time) (RelayEvent, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now = now.UTC()
	s.purgeLocked(now)
	if prior, ok := s.state.Deliveries[deliveryID]; ok {
		if prior.PayloadDigest != digest {
			return RelayEvent{}, false, ErrDeliveryConflict
		}
		for _, item := range s.state.Events {
			if item.Sequence == prior.Sequence {
				return item, true, nil
			}
		}
		return eventFromWebhook(prior.Event, prior.Sequence, prior.ReceivedAt), true, nil
	}
	authorized := false
	candidate := eventFromWebhook(event, 0, now)
	for _, session := range s.state.Sessions {
		if now.Before(session.ExpiresAt) && sessionAllows(session, candidate) {
			authorized = true
			break
		}
	}
	if !authorized {
		return RelayEvent{}, false, ErrUnauthorizedScope
	}
	s.state.Sequence++
	out := eventFromWebhook(event, s.state.Sequence, now)
	s.state.Deliveries[deliveryID] = DeliveryRecord{
		DeliveryID: deliveryID, PayloadDigest: digest, ReceivedAt: now, Event: event, Sequence: out.Sequence,
	}
	s.state.Events = append(s.state.Events, out)
	s.purgeLocked(now)
	if err := s.persistLocked(); err != nil {
		return RelayEvent{}, false, err
	}
	return out, false, nil
}

func (s *Store) Replay(after uint64, session RelaySession) ([]RelayEvent, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if after > s.state.Sequence {
		return nil, true
	}
	if after != 0 && len(s.state.Events) > 0 && after+1 < s.state.Events[0].Sequence {
		return nil, true
	}
	out := make([]RelayEvent, 0)
	for _, event := range s.state.Events {
		if event.Sequence > after && sessionAllows(session, event) {
			out = append(out, event)
		}
	}
	return out, false
}

func (s *Store) Cursor() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state.Sequence
}

func (s *Store) EventCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.state.Events)
}
