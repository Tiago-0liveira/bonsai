package localapi

import (
	"github.com/Tiago-0liveira/bonsai/internal/core/agentterminal"
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
	// ResetAt is when GitHub's rate-limit window resets (rate_limited only).
	ResetAt *time.Time `json:"reset_at,omitempty"`
}

type browserFreshness struct {
	State     string             `json:"state"`
	UpdatedAt *time.Time         `json:"updated_at,omitempty"`
	Error     *browserStateError `json:"error,omitempty"`
}

type browserRemoteSnapshot struct {
	Repository        githubdomain.RemoteRepository `json:"repository"`
	Branches          []githubdomain.RemoteBranch   `json:"branches"`
	PullRequests      []githubdomain.PullRequest    `json:"pull_requests"`
	UpdatedAt         time.Time                     `json:"updated_at"`
	PRCatalogComplete bool                          `json:"pr_catalog_complete"`
	PRCatalogLoading  bool                          `json:"pr_catalog_loading"`
}

type browserBranchCandidate struct {
	ID                string                     `json:"id"`
	Ref               string                     `json:"ref"`
	Name              string                     `json:"name"`
	Source            string                     `json:"source"` // local | remote | provider
	Remote            string                     `json:"remote,omitempty"`
	UpstreamRef       string                     `json:"upstream_ref,omitempty"`
	LocalBranch       string                     `json:"local_branch,omitempty"`
	HeadSHA           string                     `json:"head_sha,omitempty"`
	LastCommitAt      *time.Time                 `json:"last_commit_at,omitempty"`
	PullRequests      []githubdomain.PullRequest `json:"pull_requests"`
	WorktreeIDs       []string                   `json:"worktree_ids"`
	CreationMode      string                     `json:"creation_mode,omitempty"`
	SourceRef         string                     `json:"source_ref,omitempty"`
	UnavailableReason string                     `json:"unavailable_reason,omitempty"`
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
	CommandKey     string           `json:"command_key"`
	ExecutionOrder uint64           `json:"execution_order"`
	ID             string           `json:"id"`
	DaemonID       int              `json:"daemon_id"`
	ProjectID      string           `json:"project_id"`
	WorktreeID     string           `json:"worktree_id,omitempty"`
	Label          string           `json:"label"`
	Command        string           `json:"command"`
	Status         string           `json:"status"`
	PID            int              `json:"pid,omitempty"`
	ExpectedPort   int              `json:"expected_port,omitempty"`
	URL            string           `json:"url,omitempty"`
	StartedAt      time.Time        `json:"started_at,omitempty"`
	ExitCode       *int             `json:"exit_code,omitempty"`
	ExitError      string           `json:"exit_error,omitempty"`
	ServeGroup     string           `json:"serve_group,omitempty"`
	ServeName      string           `json:"serve_name,omitempty"`
	Policy         procstore.Policy `json:"policy"`
	Restarts       int              `json:"restarts"`
	RetryCount     int              `json:"retry_count"`
	Attempt        int              `json:"attempt"`
	Revision       uint64           `json:"revision"`
	RetryAt        *time.Time       `json:"retry_at,omitempty"`
}

type browserSnapshot struct {
	Agents            []agentterminal.Summary         `json:"agents"`
	BranchCandidates  []browserBranchCandidate        `json:"branch_candidates"`
	Sync              domain.RepositorySync           `json:"sync"`
	Epoch             string                          `json:"epoch"`
	Repository        browserRepository               `json:"repository"`
	Local             *domain.RepositoryState         `json:"local,omitempty"`
	Remote            *browserRemoteSnapshot          `json:"remote,omitempty"`
	Online            bool                            `json:"online"`
	Sequence          uint64                          `json:"sequence"`
	Metadata          map[string]worktreeMetadata     `json:"metadata"`
	ProcessVisibility procstore.ProcessVisibility     `json:"process_visibility"`
	Processes         []browserProcessSummary         `json:"processes"`
	Freshness         map[string]browserFreshness     `json:"freshness"`
	WorktreeState     map[string]browserWorktreeState `json:"worktree_state"`
}

type worktreeMetadata struct {
	WorktreeID        string `json:"worktree_id"`
	RepositoryID      string `json:"repository_id"`
	MergeTargetBranch string `json:"merge_target_branch"`
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
		CommandKey:     procstore.CommandIdentity(record),
		ExecutionOrder: procstore.ExecutionOrder(record),
		ID:             projectID + ":" + strconv.Itoa(record.ID),
		DaemonID:       record.ID,
		ProjectID:      projectID,
		WorktreeID:     worktreeID,
		Label:          record.Label,
		Command:        record.Command,
		Status:         record.Status,
		PID:            record.PID,
		ExpectedPort:   record.ExpectedPort,
		URL:            record.LastURL,
		StartedAt:      record.StartedAt,
		ExitCode:       record.ExitCode,
		ExitError:      record.ExitError,
		ServeGroup:     record.ServeGroup,
		ServeName:      record.ServeName,
		Policy:         record.Policy,
		Restarts:       record.Restarts,
		RetryCount:     record.RetryCount,
		Attempt:        record.Attempt,
		Revision:       record.Revision,
		RetryAt:        record.RetryAt,
	}
}

func publicWorktreeID(path string) string {
	if canonical, err := filepath.EvalSymlinks(path); err == nil {
		path = canonical
	}
	return gitlocal.ID(localRepositoryID, path)
}
