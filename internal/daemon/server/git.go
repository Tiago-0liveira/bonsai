package server

import (
	"context"
	"github.com/Tiago-0liveira/bonsai/internal/core/agenthost"
	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
	"path/filepath"
	"strings"
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
		// The command journal opens first: isolation shares it as ownership
		// evidence, so a worktree created before a restart is still provably ours.
		journal, err := store.Open(filepath.Join(s.store.Dir(), "git-commands.json"))
		if err != nil {
			s.gitErr = err
			return
		}
		svc, err := local.New([]local.Config{{
			ID:   localRepositoryID,
			Root: s.root,
			BeforeRemove: func(_ context.Context, path string) error {
				if busy, err := agentHolderAt(path); err != nil || busy {
					return domain.E("processes_running", "Stop the agent sessions running in this worktree before deleting it")
				}
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
			IsolationJournal: journal,
		}})
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

// agentHolderAt reports whether any agent holder, started by the web, the CLI or
// the TUI, is active or still unsettled in dir or below it. Unreadable records
// are an error, never "none": removal refuses when it cannot tell.
func agentHolderAt(dir string) (bool, error) {
	paths, err := agenthost.DefaultPaths()
	if err != nil {
		return false, err
	}
	return agentHolderIn(paths, dir)
}

func agentHolderIn(paths agenthost.Paths, dir string) (bool, error) {
	records, err := agenthost.List(paths)
	if err != nil {
		return false, err
	}
	root := canonical(dir)
	for _, r := range records {
		if !r.Summary.Active() {
			continue
		}
		work := canonical(r.WorkDir)
		if work == root || strings.HasPrefix(work, root+string(filepath.Separator)) {
			return true, nil
		}
	}
	return false, nil
}

func canonical(dir string) string {
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		dir = resolved
	}
	return filepath.Clean(dir)
}
