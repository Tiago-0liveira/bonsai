package localapi

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/daemon/gitbridge"
	domain "github.com/Tiago-0liveira/bonsai/internal/git"
	githubdomain "github.com/Tiago-0liveira/bonsai/internal/git/github"
	"github.com/Tiago-0liveira/bonsai/internal/git/github/ghcli"
	gitstore "github.com/Tiago-0liveira/bonsai/internal/storage/git"
)

type browserRepository = ProjectInfo

type browserRemoteSnapshot struct {
	Repository   githubdomain.RemoteRepository `json:"repository"`
	Branches     []githubdomain.RemoteBranch   `json:"branches"`
	PullRequests []githubdomain.PullRequest    `json:"pull_requests"`
	UpdatedAt    time.Time                     `json:"updated_at"`
}

type worktreeMetadata struct {
	WorktreeID        string `json:"worktree_id"`
	RepositoryID      string `json:"repository_id"`
	MergeTargetBranch string `json:"merge_target_branch"`
	Tag               string `json:"tag"`
	StackPreference   string `json:"stack_preference"`
}

type browserSnapshot struct {
	Repository browserRepository           `json:"repository"`
	Local      *domain.RepositoryState     `json:"local,omitempty"`
	Remote     *browserRemoteSnapshot      `json:"remote,omitempty"`
	Online     bool                        `json:"online"`
	Sequence   uint64                      `json:"sequence"`
	Metadata   map[string]worktreeMetadata `json:"metadata"`
}

func (s *Server) readLocalRepository() (domain.RepositoryState, error) {
	result, err := s.registry.Default().daemon.Git(gitbridge.Command{
		ID:           randomID(),
		UserID:       localBrowserUserID,
		RepositoryID: localRepositoryID,
		Type:         "git.repository.refresh",
		CreatedAt:    time.Now().UTC(),
	})
	if err != nil {
		return domain.RepositoryState{}, err
	}
	if result.Error != nil {
		return domain.RepositoryState{}, result.Error
	}
	var state domain.RepositoryState
	if err := json.Unmarshal(result.Payload, &state); err != nil {
		return domain.RepositoryState{}, fmt.Errorf("decode local repository state: %w", err)
	}
	state.ID = s.registry.Default().info.ID
	for i := range state.Worktrees {
		state.Worktrees[i].RepositoryID = state.ID
	}
	return state, nil
}

func (s *Server) projectDescriptor(ctx context.Context, local domain.RepositoryState) browserRepository {
	out := s.registry.Default().info
	out.DefaultBranch = local.DefaultBranch
	if discovered, err := ghcli.Discover(ctx, s.repoDir); err == nil {
		out.FullName = discovered.FullName
		if out.DefaultBranch == "" {
			out.DefaultBranch = discovered.DefaultBranch
		}
	}
	return out
}

func (s *Server) browserSnapshot(ctx context.Context) (browserSnapshot, error) {
	local, err := s.readLocalRepository()
	if err != nil {
		return browserSnapshot{}, err
	}
	metadata, err := s.metadataSnapshot()
	if err != nil {
		return browserSnapshot{}, fmt.Errorf("read project metadata: %w", err)
	}
	repository := s.projectDescriptor(ctx, local)
	snapshot := browserSnapshot{
		Repository: repository,
		Local:      &local,
		Online:     true,
		Sequence:   s.sequence.Load(),
		Metadata:   metadata,
	}
	if validRepository(repository.FullName) {
		remoteRepository, repoErr := s.registry.Default().github.Repository(ctx, repository.FullName)
		branches, branchErr := s.registry.Default().github.Branches(ctx, repository.FullName)
		pullRequests, prErr := s.registry.Default().github.PullRequests(ctx, repository.FullName, githubdomain.PRFilter{State: "all"})
		if repoErr == nil && branchErr == nil && prErr == nil {
			snapshot.Remote = &browserRemoteSnapshot{
				Repository:   remoteRepository,
				Branches:     branches,
				PullRequests: pullRequests,
				UpdatedAt:    time.Now().UTC(),
			}
		}
	}
	return snapshot, nil
}

func (s *Server) metadataSnapshot() (map[string]worktreeMetadata, error) {
	out := map[string]worktreeMetadata{}
	err := s.state.View(func(data gitstore.Data) error {
		for id := range data["worktree_metadata"] {
			if value, ok := gitstore.Get[worktreeMetadata](data, "worktree_metadata", id); ok && value.RepositoryID == localRepositoryID {
				value.RepositoryID = s.registry.Default().info.ID
				out[id] = value
			}
		}
		return nil
	})
	return out, err
}

func (s *Server) publishProjectEvent(entity string) {
	s.sequence.Add(1)
	s.eventHub.publish(localEvent{Type: "git", ProjectID: s.registry.Default().info.ID, EntityID: entity})
}
