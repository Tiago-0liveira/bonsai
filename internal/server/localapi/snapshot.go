package localapi

import (
	"path/filepath"
	"strconv"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
	domain "github.com/Tiago-0liveira/bonsai/internal/git"
	githubdomain "github.com/Tiago-0liveira/bonsai/internal/git/github"
	gitlocal "github.com/Tiago-0liveira/bonsai/internal/git/local"
	gitstore "github.com/Tiago-0liveira/bonsai/internal/storage/git"
)

type browserRepository = ProjectInfo

type browserStateError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type browserFreshness struct {
	State     string             `json:"state"`
	UpdatedAt *time.Time         `json:"updated_at,omitempty"`
	Error     *browserStateError `json:"error,omitempty"`
}

type browserRemoteSnapshot struct {
	Repository   githubdomain.RemoteRepository `json:"repository"`
	Branches     []githubdomain.RemoteBranch   `json:"branches"`
	PullRequests []githubdomain.PullRequest    `json:"pull_requests"`
	UpdatedAt    time.Time                     `json:"updated_at"`
}

type browserCIState struct {
	Status     string               `json:"status"`
	CheckedSHA string               `json:"checked_sha,omitempty"`
	Checks     []githubdomain.Check `json:"checks"`
	Freshness  browserFreshness     `json:"freshness"`
}

type browserWorktreeState struct {
	PullRequest  *githubdomain.PullRequest `json:"pull_request,omitempty"`
	PRDiagnostic string                    `json:"pr_diagnostic,omitempty"`
	CI           browserCIState            `json:"ci"`
}

type browserProcessSummary struct {
	ID           string    `json:"id"`
	DaemonID     int       `json:"daemon_id"`
	ProjectID    string    `json:"project_id"`
	WorktreeID   string    `json:"worktree_id,omitempty"`
	Label        string    `json:"label"`
	Command      string    `json:"command"`
	Status       string    `json:"status"`
	PID          int       `json:"pid,omitempty"`
	ExpectedPort int       `json:"expected_port,omitempty"`
	URL          string    `json:"url,omitempty"`
	StartedAt    time.Time `json:"started_at,omitempty"`
	ExitCode     *int      `json:"exit_code,omitempty"`
	ExitError    string    `json:"exit_error,omitempty"`
	ServeGroup   string    `json:"serve_group,omitempty"`
	ServeName    string    `json:"serve_name,omitempty"`
}

type browserSnapshot struct {
	Epoch         string                          `json:"epoch"`
	Repository    browserRepository               `json:"repository"`
	Local         *domain.RepositoryState         `json:"local,omitempty"`
	Remote        *browserRemoteSnapshot          `json:"remote,omitempty"`
	Online        bool                            `json:"online"`
	Sequence      uint64                          `json:"sequence"`
	Metadata      map[string]worktreeMetadata     `json:"metadata"`
	Processes     []browserProcessSummary         `json:"processes"`
	Freshness     map[string]browserFreshness     `json:"freshness"`
	WorktreeState map[string]browserWorktreeState `json:"worktree_state"`
}

type worktreeMetadata struct {
	WorktreeID        string `json:"worktree_id"`
	RepositoryID      string `json:"repository_id"`
	MergeTargetBranch string `json:"merge_target_branch"`
	Tag               string `json:"tag"`
	StackPreference   string `json:"stack_preference"`
}

func metadataSnapshotFor(project projectServices) (map[string]worktreeMetadata, error) {
	out := map[string]worktreeMetadata{}
	if project.state == nil {
		return out, nil
	}
	err := project.state.View(func(data gitstore.Data) error {
		for id := range data["worktree_metadata"] {
			if value, ok := gitstore.Get[worktreeMetadata](data, "worktree_metadata", id); ok && value.RepositoryID == localRepositoryID {
				value.RepositoryID = project.info.ID
				out[id] = value
			}
		}
		return nil
	})
	return out, err
}

func processSummary(projectID string, record *procstore.Record) browserProcessSummary {
	worktreeID := ""
	if record.Worktree != "" {
		worktreeID = publicWorktreeID(record.Worktree)
	}
	return browserProcessSummary{
		ID:           projectID + ":" + strconv.Itoa(record.ID),
		DaemonID:     record.ID,
		ProjectID:    projectID,
		WorktreeID:   worktreeID,
		Label:        record.Label,
		Command:      record.Command,
		Status:       record.Status,
		PID:          record.PID,
		ExpectedPort: record.ExpectedPort,
		URL:          record.LastURL,
		StartedAt:    record.StartedAt,
		ExitCode:     record.ExitCode,
		ExitError:    record.ExitError,
		ServeGroup:   record.ServeGroup,
		ServeName:    record.ServeName,
	}
}

func publicWorktreeID(path string) string {
	if canonical, err := filepath.EvalSymlinks(path); err == nil {
		path = canonical
	}
	return gitlocal.ID(localRepositoryID, path)
}
