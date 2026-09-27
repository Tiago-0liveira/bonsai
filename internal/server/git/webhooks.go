package git

import (
	"context"
	"encoding/json"
	bridge "github.com/Tiago-0liveira/bonsai/internal/daemon/gitbridge"
	"github.com/Tiago-0liveira/bonsai/internal/server/webhooks"
	store "github.com/Tiago-0liveira/bonsai/internal/storage/git"
	"strconv"
	"strings"
	"time"
)

func (s *Service) Webhook(ctx context.Context, d webhooks.Delivery) error {
	var raw struct {
		Action     string
		Repository struct {
			ID       int64
			FullName string `json:"full_name"`
		}
		Installation struct{ ID int64 }
		Number       int
		PullRequest  struct {
			Number int
			Merged bool
		} `json:"pull_request"`
		Issue      struct{ Number int } `json:"issue"`
		Ref, After string
		Deleted    bool
		CheckRun   struct {
			HeadSHA string `json:"head_sha"`
		} `json:"check_run"`
		CheckSuite struct {
			HeadSHA string `json:"head_sha"`
		} `json:"check_suite"`
		WorkflowRun struct {
			HeadBranch string `json:"head_branch"`
		} `json:"workflow_run"`
		RepositoriesRemoved []struct{ ID int64 } `json:"repositories_removed"`
		RepositoriesAdded   []struct{ ID int64 } `json:"repositories_added"`
	}
	if e := json.Unmarshal(d.Payload, &raw); e != nil {
		return e
	}
	if d.Event == "installation" || d.Event == "installation_repositories" {
		s.Auth.Invalidate()
		// Block removed/suspended installation access immediately, and purge stale
		// remote cache instead of continuing to serve data after access is revoked.

		affected := []string{}
		err := s.Store.Update(func(data store.Data) error {
			for id, r := range s.Repositories {
				if r.InstallationID != raw.Installation.ID {
					continue
				}
				revoked := raw.Action == "deleted" || raw.Action == "suspend"
				restored := d.Event == "installation" && (raw.Action == "created" || raw.Action == "unsuspend")
				for _, removed := range raw.RepositoriesRemoved {
					revoked = revoked || removed.ID == r.GitHubRepositoryID
				}
				for _, added := range raw.RepositoriesAdded {
					restored = restored || added.ID == r.GitHubRepositoryID
				}
				if !revoked && !restored {
					continue
				}
				affected = append(affected, id)
				if revoked {
					delete(data["remote_snapshots"], id)
					store.Put(data, "revoked_repositories", id, true)
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
			if err = s.publish(id, "github", "repository.updated", id, d.ID+":"+id, map[string]string{"access_action": raw.Action}); err != nil {
				return err
			}
		}
		return nil

	}
	var repo string
	for id, r := range s.Repositories {
		if r.GitHubRepositoryID == raw.Repository.ID && r.InstallationID == raw.Installation.ID {
			repo = id
			break
		}
	}
	if repo == "" {
		return nil
	}
	switch d.Event {
	case "push", "pull_request", "pull_request_review", "pull_request_review_comment", "issue_comment", "check_run", "check_suite", "workflow_run", "workflow_job", "release", "deployment", "deployment_status":
	default:
		return nil
	}
	// Webhooks invalidate; GitHub's current API state wins over out-of-order payloads.
	if e := s.Reconcile(ctx, repo); e != nil {
		return e
	}
	kind := "repository.updated"
	entity := repo
	var payload any = s.snapshot(repo).Remote
	switch d.Event {
	case "push":
		kind = "branch.updated"
		entity = strings.TrimPrefix(raw.Ref, "refs/heads/")
		snap := s.snapshot(repo)
		if snap.Remote != nil {
			payload = snap.Remote.Branches
		}
	case "pull_request", "pull_request_review", "pull_request_review_comment", "issue_comment":
		kind = "pull_request.updated"
		if d.Event == "pull_request_review" {
			kind = "pull_request.reviewed"
		}
		if raw.Action == "opened" {
			kind = "pull_request.created"
		}
		if raw.PullRequest.Merged {
			kind = "pull_request.merged"
		}
		n := raw.Number
		if n == 0 {
			n = raw.PullRequest.Number
		}
		if n == 0 {
			n = raw.Issue.Number
		}
		entity = strconv.Itoa(n)
	case "check_run", "check_suite":
		kind = "checks.updated"
		sha := raw.CheckRun.HeadSHA
		if sha == "" {
			sha = raw.CheckSuite.HeadSHA
		}
		entity = sha
		if sha != "" {
			checks, e := s.GitHub.Checks(ctx, s.Repositories[repo].FullName, sha)
			if e != nil {
				return e
			}
			payload = checks
		}
	case "workflow_run", "workflow_job":
		kind = "workflow.updated"
		runs, e := s.GitHub.WorkflowRuns(ctx, s.Repositories[repo].FullName, raw.WorkflowRun.HeadBranch)
		if e != nil {
			return e
		}
		payload = runs
	case "release":
		kind = "release.updated"
		payload = map[string]string{"action": raw.Action}
	case "deployment", "deployment_status":
		kind = "deployment.updated"
		payload = map[string]string{"action": raw.Action}
	}
	if e := s.publish(repo, "github", kind, entity, d.ID, payload); e != nil {
		return e
	}
	if d.Event == "push" {
		// Throttle fetch to one per repository per ten-second window. GitHub SHAs
		// are already published; only a subsequent daemon snapshot updates refs.
		s.mu.Lock()
		conn := s.devices[repo]
		s.mu.Unlock()
		if conn != nil {
			var last time.Time
			s.Store.View(func(data store.Data) error { last, _ = store.Get[time.Time](data, "last_fetch", repo); return nil })
			if time.Since(last) > 10*time.Second {
				result, e := s.execute(ctx, bridge.Command{ID: "webhook-fetch-" + d.ID, UserID: conn.device.UserID, RepositoryID: repo, Type: "git.fetch", Arguments: json.RawMessage(`{}`)})
				if e == nil && result.Error == nil {
					_ = s.Store.Update(func(data store.Data) error { return store.Put(data, "last_fetch", repo, time.Now()) })
				}
			}
		}
	}
	return nil
}
