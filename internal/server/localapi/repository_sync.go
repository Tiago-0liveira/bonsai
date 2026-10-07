package localapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/daemon/gitbridge"
	domain "github.com/Tiago-0liveira/bonsai/internal/git"
	gitstore "github.com/Tiago-0liveira/bonsai/internal/storage/git"
)

const repositorySyncInterval = 5 * time.Minute

type syncRequest struct {
	Initial bool `json:"initial"`
}

func (s *Server) projectSync(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if key == "" || len(key) > 128 {
		writeAPIError(w, 400, "invalid", "Idempotency-Key is required for sync requests")
		return
	}
	var input syncRequest
	if !decodeStrictJSON(w, r, &input) {
		return
	}
	replayed := false
	err := s.state.Update(func(data gitstore.Data) error {
		if old, ok := gitstore.Get[syncRequest](data, "sync_requests", key); ok {
			if old != input {
				return domain.ErrInvalid
			}
			replayed = true
			return nil
		}
		return gitstore.Put(data, "sync_requests", key, input)
	})
	if err != nil {
		writeDomainError(w, &domain.Error{Code: domain.Code(err), Message: err.Error()})
		return
	}
	if !replayed {
		s.stateSync.queueRepositorySync(s.registry.Default().info.ID, key, input.Initial, true)
	}
	writeJSON(w, http.StatusAccepted, map[string]bool{"accepted": true})
}

func (s *stateSync) syncActiveProjects() {
	s.mu.Lock()
	now := s.now()
	ids := []string{}
	for id, j := range s.jobs {
		if now.Before(j.activeUntil) {
			ids = append(ids, id)
		}
	}
	s.mu.Unlock()
	for _, id := range ids {
		s.queueRepositorySync(id, randomID(), true, false)
	}
}

func (s *stateSync) queueRepositorySync(projectID, commandID string, initial, activate bool) {
	project, ok := s.registry.Lookup(projectID)
	if !ok || !project.info.Available {
		return
	}
	s.mu.Lock()
	j := s.jobLocked(projectID)
	now := s.now()
	if activate {
		j.activeUntil = now.Add(repositorySyncInterval + time.Minute)
	}
	if s.closed || j.syncRunning || now.Before(j.syncRetryAt) || (initial && !j.lastSyncAttempt.IsZero() && now.Sub(j.lastSyncAttempt) < repositorySyncInterval) {
		s.mu.Unlock()
		return
	}
	j.syncRunning, j.lastSyncAttempt = true, now
	s.mu.Unlock()
	go func() {
		defer func() {
			s.mu.Lock()
			if j := s.jobs[projectID]; j != nil {
				j.syncRunning = false
			}
			s.mu.Unlock()
		}()
		// Publish local inventory before the mutation starts, independently of
		// GitHub authentication and latency.
		s.refreshLocal(projectID)
		s.commitProject(project, "sync", func(snapshot *browserSnapshot) {
			snapshot.Sync.Fetch.State, snapshot.Sync.Fetch.Error = "running", ""
			snapshot.Sync.Pull.State, snapshot.Sync.Pull.Error = "running", ""
		})
		ctx, cancel := s.readContext(90 * time.Second)
		defer cancel()
		var err error
		select {
		case <-ctx.Done():
			err = ctx.Err()
		case s.gitSyncSem <- struct{}{}:
			defer func() { <-s.gitSyncSem }()
		}
		command := gitbridge.Command{ID: "sync-" + commandID, UserID: localBrowserUserID, RepositoryID: localRepositoryID, Type: "git.repository.sync", Arguments: mustJSON(domain.PullPolicy{FastForwardOnly: true}), CreatedAt: now}
		var result *gitbridge.Result
		if err == nil {
			if daemon, ok := project.daemon.(contextDaemonClient); ok {
				result, err = daemon.GitContext(ctx, command)
			} else {
				result, err = project.daemon.Git(command)
			}
		}
		outcome := domain.RepositorySync{}
		if err == nil && result != nil && result.Error != nil {
			err = result.Error
		}
		if err == nil && result != nil {
			err = json.Unmarshal(result.Payload, &outcome)
		}
		if err != nil {
			outcome.Fetch = domain.SyncOutcome{State: "error", Error: err.Error()}
			outcome.Pull = domain.SyncOutcome{State: "skipped", Reason: "fetch_failed"}
		}
		s.commitProject(project, "sync", func(snapshot *browserSnapshot) {
			if outcome.Fetch.CompletedAt == nil {
				outcome.Fetch.CompletedAt = snapshot.Sync.Fetch.CompletedAt
			}
			if outcome.Pull.CompletedAt == nil {
				outcome.Pull.CompletedAt = snapshot.Sync.Pull.CompletedAt
			}
			snapshot.Sync = outcome
		})
		s.refreshLocal(projectID)
		s.queueProvider(projectID, !initial)
		s.mu.Lock()
		j := s.jobs[projectID]
		if j == nil {
			s.mu.Unlock()
			return
		}
		if outcome.Fetch.State == "error" {
			if j.syncBackoff == 0 {
				j.syncBackoff = 30 * time.Second
			} else {
				j.syncBackoff *= 2
			}
			if j.syncBackoff > repositorySyncInterval {
				j.syncBackoff = repositorySyncInterval
			}
			j.syncRetryAt = s.now().Add(j.syncBackoff)
		} else {
			j.syncRetryAt = time.Time{}
			j.syncBackoff = 0
		}
		s.mu.Unlock()
	}()
}
