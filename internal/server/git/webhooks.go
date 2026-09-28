package git

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	bridge "github.com/Tiago-0liveira/bonsai/internal/daemon/gitbridge"
	"github.com/Tiago-0liveira/bonsai/internal/server/webhooks"
	store "github.com/Tiago-0liveira/bonsai/internal/storage/git"
)

// Webhook preserves the combined-server adapter while moving all business logic
// onto the same normalized event type used by the isolated serve webhook.
func (s *Service) Webhook(ctx context.Context, d webhooks.Delivery) error {
	event, err := webhooks.Normalize(d.ID, d.Event, d.Payload)
	if errors.Is(err, webhooks.ErrUnsupported) {
		return nil
	}
	if err != nil {
		return err
	}
	return s.WebhookEvent(ctx, event)
}

// WebhookEvent maps a verified, normalized event to trusted Bonsai behavior.
// Executable commands are selected locally; the event cannot provide commands,
// paths, working directories, or shell arguments.
func (s *Service) WebhookEvent(ctx context.Context, event webhooks.Event) error {
	if event.Event == "installation" || event.Event == "installation_repositories" {
		s.Auth.Invalidate()
		affected := []string{}
		err := s.Store.Update(func(data store.Data) error {
			for id, r := range s.Repositories {
				if r.InstallationID != event.InstallationID {
					continue
				}
				revoked := event.Action == "deleted" || event.Action == "suspend"
				restored := event.Event == "installation" && (event.Action == "created" || event.Action == "unsuspend")
				for _, removed := range event.RepositoriesRemoved {
					revoked = revoked || removed == r.GitHubRepositoryID
				}
				for _, added := range event.RepositoriesAdded {
					restored = restored || added == r.GitHubRepositoryID
				}
				if !revoked && !restored {
					continue
				}
				affected = append(affected, id)
				if revoked {
					delete(data["remote_snapshots"], id)
					_ = store.Put(data, "revoked_repositories", id, true)
				} else {
					delete(data["revoked_repositories"], id)
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
		s.cacheMu.Lock()
		s.readCache = nil
		s.cacheMu.Unlock()
		for _, id := range affected {
			if err = s.publish(id, "github", "repository.updated", id, event.DeliveryID+":"+id, map[string]string{"access_action": event.Action}); err != nil {
				return err
			}
		}
		return nil
	}

	var repo string
	for id, r := range s.Repositories {
		if r.GitHubRepositoryID == event.RepositoryID && r.InstallationID == event.InstallationID {
			repo = id
			break
		}
	}
	if repo == "" {
		return nil
	}
	switch event.Event {
	case "push", "pull_request", "pull_request_review", "pull_request_review_comment", "issue_comment",
		"check_run", "check_suite", "workflow_run", "workflow_job", "release", "deployment", "deployment_status":
	default:
		return nil
	}

	// Webhooks invalidate; GitHub's current API state wins over out-of-order payloads.
	if err := s.Reconcile(ctx, repo); err != nil {
		return err
	}
	kind := "repository.updated"
	entity := repo
	var payload any = s.snapshot(repo).Remote
	switch event.Event {
	case "push":
		kind = "branch.updated"
		entity = event.Ref
		const prefix = "refs/heads/"
		if len(entity) >= len(prefix) && entity[:len(prefix)] == prefix {
			entity = entity[len(prefix):]
		}
		snap := s.snapshot(repo)
		if snap.Remote != nil {
			payload = snap.Remote.Branches
		}
	case "pull_request", "pull_request_review", "pull_request_review_comment", "issue_comment":
		kind = "pull_request.updated"
		if event.Event == "pull_request_review" {
			kind = "pull_request.reviewed"
		}
		if event.Action == "opened" {
			kind = "pull_request.created"
		}
		if event.PullRequestMerged {
			kind = "pull_request.merged"
		}
		number := event.Number
		if number == 0 {
			number = event.PullRequestNumber
		}
		if number == 0 {
			number = event.IssueNumber
		}
		entity = strconv.Itoa(number)
	case "check_run", "check_suite":
		kind = "checks.updated"
		entity = event.CheckHeadSHA
		if event.CheckHeadSHA != "" {
			checks, err := s.GitHub.Checks(ctx, s.Repositories[repo].FullName, event.CheckHeadSHA)
			if err != nil {
				return err
			}
			payload = checks
		}
	case "workflow_run", "workflow_job":
		kind = "workflow.updated"
		runs, err := s.GitHub.WorkflowRuns(ctx, s.Repositories[repo].FullName, event.WorkflowHeadBranch)
		if err != nil {
			return err
		}
		payload = runs
	case "release":
		kind = "release.updated"
		payload = map[string]string{"action": event.Action}
	case "deployment", "deployment_status":
		kind = "deployment.updated"
		payload = map[string]string{"action": event.Action}
	}
	if err := s.publish(repo, "github", kind, entity, event.DeliveryID, payload); err != nil {
		return err
	}
	if event.Event == "push" {
		s.mu.Lock()
		conn := s.devices[repo]
		s.mu.Unlock()
		if conn != nil {
			var last time.Time
			_ = s.Store.View(func(data store.Data) error {
				last, _ = store.Get[time.Time](data, "last_fetch", repo)
				return nil
			})
			if time.Since(last) > 10*time.Second {
				result, err := s.execute(ctx, bridge.Command{
					ID: "webhook-fetch-" + event.DeliveryID, UserID: conn.device.UserID,
					RepositoryID: repo, Type: "git.fetch", Arguments: json.RawMessage(`{}`),
				})
				if err == nil && result.Error == nil {
					_ = s.Store.Update(func(data store.Data) error {
						return store.Put(data, "last_fetch", repo, time.Now())
					})
				}
			}
		}
	}
	return nil
}
