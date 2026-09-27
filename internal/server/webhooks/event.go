package webhooks

import (
	"encoding/json"
	"errors"
	"fmt"
)

// ErrUnsupported marks a valid GitHub payload whose event/action is outside the
// deliberately narrow Bonsai webhook contract.
var ErrUnsupported = errors.New("unsupported GitHub webhook event or action")

// Event is the normalized, non-executable message that crosses from the
// internet-facing webhook listener to the local Bonsai API.
type Event struct {
	DeliveryID          string  `json:"delivery_id"`
	Event               string  `json:"event"`
	Action              string  `json:"action,omitempty"`
	RepositoryID        int64   `json:"repository_id,omitempty"`
	InstallationID      int64   `json:"installation_id,omitempty"`
	Number              int     `json:"number,omitempty"`
	PullRequestNumber   int     `json:"pull_request,omitempty"`
	PullRequestMerged   bool    `json:"pull_request_merged,omitempty"`
	IssueNumber         int     `json:"issue,omitempty"`
	Ref                 string  `json:"ref,omitempty"`
	CheckHeadSHA        string  `json:"check_head_sha,omitempty"`
	WorkflowHeadBranch  string  `json:"workflow_head_branch,omitempty"`
	RepositoriesAdded   []int64 `json:"repositories_added,omitempty"`
	RepositoriesRemoved []int64 `json:"repositories_removed,omitempty"`
}

// Normalize parses only the fields Bonsai needs after GitHub HMAC verification.
// It never accepts command, path, shell, working-directory, or argument fields.
func Normalize(deliveryID, eventName string, payload []byte) (Event, error) {
	var raw struct {
		Action     string
		Repository struct {
			ID int64
		}
		Installation struct {
			ID int64
		}
		Number      int
		PullRequest struct {
			Number int
			Merged bool
		} `json:"pull_request"`
		Issue struct {
			Number int
		} `json:"issue"`
		Ref      string
		CheckRun struct {
			HeadSHA string `json:"head_sha"`
		} `json:"check_run"`
		CheckSuite struct {
			HeadSHA string `json:"head_sha"`
		} `json:"check_suite"`
		WorkflowRun struct {
			HeadBranch string `json:"head_branch"`
		} `json:"workflow_run"`
		WorkflowJob struct {
			HeadBranch string `json:"head_branch"`
		} `json:"workflow_job"`
		RepositoriesRemoved []struct {
			ID int64
		} `json:"repositories_removed"`
		RepositoriesAdded []struct {
			ID int64
		} `json:"repositories_added"`
	}
	if err := json.Unmarshal(payload, &raw); err != nil {
		return Event{}, err
	}
	if !AllowedEventAction(eventName, raw.Action) {
		return Event{}, fmt.Errorf("%w: %s/%s", ErrUnsupported, eventName, raw.Action)
	}
	out := Event{
		DeliveryID:        deliveryID,
		Event:             eventName,
		Action:            raw.Action,
		RepositoryID:      raw.Repository.ID,
		InstallationID:    raw.Installation.ID,
		Number:            raw.Number,
		PullRequestNumber: raw.PullRequest.Number,
		PullRequestMerged: raw.PullRequest.Merged,
		IssueNumber:       raw.Issue.Number,
		Ref:               raw.Ref,
	}
	if raw.CheckRun.HeadSHA != "" {
		out.CheckHeadSHA = raw.CheckRun.HeadSHA
	} else {
		out.CheckHeadSHA = raw.CheckSuite.HeadSHA
	}
	if raw.WorkflowRun.HeadBranch != "" {
		out.WorkflowHeadBranch = raw.WorkflowRun.HeadBranch
	} else {
		out.WorkflowHeadBranch = raw.WorkflowJob.HeadBranch
	}
	for _, r := range raw.RepositoriesAdded {
		out.RepositoriesAdded = append(out.RepositoriesAdded, r.ID)
	}
	for _, r := range raw.RepositoriesRemoved {
		out.RepositoriesRemoved = append(out.RepositoriesRemoved, r.ID)
	}
	return out, nil
}

func AllowedEventAction(eventName, action string) bool {
	allowed := map[string]map[string]bool{
		"push": {"": true},
		"pull_request": {
			"opened": true, "closed": true, "reopened": true, "synchronize": true,
			"edited": true, "ready_for_review": true, "converted_to_draft": true,
			"review_requested": true, "review_request_removed": true,
			"labeled": true, "unlabeled": true, "assigned": true, "unassigned": true,
			"locked": true, "unlocked": true, "auto_merge_enabled": true, "auto_merge_disabled": true,
		},
		"pull_request_review":         {"submitted": true, "edited": true, "dismissed": true},
		"pull_request_review_comment": {"created": true, "edited": true, "deleted": true},
		"issue_comment":               {"created": true, "edited": true, "deleted": true},
		"check_run":                   {"created": true, "rerequested": true, "completed": true, "requested_action": true},
		"check_suite":                 {"requested": true, "rerequested": true, "completed": true},
		"workflow_run":                {"requested": true, "in_progress": true, "completed": true},
		"workflow_job":                {"queued": true, "in_progress": true, "completed": true, "waiting": true},
		"release": {
			"published": true, "unpublished": true, "created": true, "edited": true,
			"deleted": true, "prereleased": true, "released": true,
		},
		"deployment":                {"created": true},
		"deployment_status":         {"created": true},
		"installation":              {"created": true, "deleted": true, "suspend": true, "unsuspend": true, "new_permissions_accepted": true},
		"installation_repositories": {"added": true, "removed": true},
	}
	actions := allowed[eventName]
	return actions != nil && actions[action]
}
