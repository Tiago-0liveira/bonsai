package server

import (
	"context"
	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
	"path/filepath"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/daemon/gitbridge"
	domain "github.com/Tiago-0liveira/bonsai/internal/git"
	"github.com/Tiago-0liveira/bonsai/internal/git/local"
	store "github.com/Tiago-0liveira/bonsai/internal/storage/git"
)

const localRepositoryID = "local"

// initGit owns the single local Git service used by TUI, CLI, and the browser
// API. Plan 1 deliberately does not read git-bridge.json: cloud configuration
// must never enable a command channel into this daemon.
func (s *Server) initGit() error {
	s.gitOnce.Do(func() {
		svc, err := local.New([]local.Config{{
			ID:   localRepositoryID,
			Root: s.root,
			BeforeRemove: func(_ context.Context, path string) error {
				records, err := s.store.ListRecords()
				if err != nil {
					return err
				}
				for _, record := range records {
					if procstore.IsActive(record.Status) && local.ID(localRepositoryID, record.Worktree) == local.ID(localRepositoryID, path) {
						return domain.E("processes_running", "Stop running processes before deleting this worktree")
					}
				}
				return nil
			},
			WithWorktreeRoot: func(ctx context.Context, create func(string) error) error {
				path, err := config.ProjectRootsPath()
				if err != nil {
					return err
				}
				return config.WithBrowserWorktreeRoot(ctx, path, s.root, create)
			},
		}})
		if err != nil {
			s.gitErr = err
			return
		}
		journal, err := store.Open(filepath.Join(s.store.Dir(), "git-commands.json"))
		if err != nil {
			s.gitErr = err
			return
		}
		s.gitExecutor = &gitbridge.Executor{Local: svc, Journal: journal}
	})
	return s.gitErr
}

func (s *Server) gitCommand(c gitbridge.Command) gitbridge.Result {
	if err := s.initGit(); err != nil {
		return gitbridge.Result{ID: c.ID, Error: &domain.Error{Code: "internal", Message: err.Error()}}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	return s.gitExecutor.Execute(ctx, c)
}
