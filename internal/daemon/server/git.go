package server

import (
	"context"
	"encoding/json"
	"fmt"
	core "github.com/Tiago-0liveira/bonsai/internal/core/git"
	"github.com/Tiago-0liveira/bonsai/internal/daemon/gitbridge"
	"github.com/Tiago-0liveira/bonsai/internal/daemon/watcher"
	domain "github.com/Tiago-0liveira/bonsai/internal/git"
	"github.com/Tiago-0liveira/bonsai/internal/git/local"
	store "github.com/Tiago-0liveira/bonsai/internal/storage/git"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type GitBridgeConfig struct {
	RepositoryID string `json:"repository_id"`
	URL          string `json:"url"`
	Credential   string `json:"credential"`
	WorktreeRoot string `json:"worktree_root"`
}

func (s *Server) initGit() error {
	s.gitOnce.Do(func() {
		cfg := GitBridgeConfig{RepositoryID: "local", WorktreeRoot: filepath.Join(s.store.Dir(), "worktrees")}
		path := filepath.Join(s.store.Dir(), "git-bridge.json")
		b, e := os.ReadFile(path)
		if e != nil && !os.IsNotExist(e) {
			s.gitErr = e
			return
		}
		if e == nil {
			if e = json.Unmarshal(b, &cfg); e != nil {
				s.gitErr = e
				return
			}
		}
		svc, e := local.New([]local.Config{{ID: cfg.RepositoryID, Root: s.root, WorktreeRoot: cfg.WorktreeRoot}})
		if e != nil {
			s.gitErr = e
			return
		}
		journal, e := store.Open(filepath.Join(s.store.Dir(), "git-commands.json"))
		if e != nil {
			s.gitErr = e
			return
		}
		s.gitExecutor = &gitbridge.Executor{Local: svc, Journal: journal}
		s.gitConfig = cfg
	})
	return s.gitErr
}
func (s *Server) runGitBridge(ctx context.Context) error {
	if e := s.initGit(); e != nil {
		return e
	}
	if s.gitConfig.URL == "" {
		return nil
	}
	snapshots := make(chan domain.RepositoryState, 16)
	roots := []string{s.root, s.gitConfig.WorktreeRoot}
	common, e := core.RunContext(ctx, s.root, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if e != nil {
		return e
	}
	roots = append(roots, strings.TrimSpace(common))
	trees, e := core.ListWorktreesContext(ctx, s.root)
	if e != nil {
		return e
	}
	for _, w := range trees {
		roots = append(roots, w.Path)
	}
	watch := watcher.Watcher{Local: s.gitExecutor.Local, RepositoryID: s.gitConfig.RepositoryID, Roots: roots, WorktreePaths: func(ctx context.Context) map[string]string {
		out := map[string]string{}
		trees, err := core.ListWorktreesContext(ctx, s.root)
		if err == nil {
			for _, tree := range trees {
				out[local.ID(s.gitConfig.RepositoryID, tree.Path)] = tree.Path
			}
		}
		return out
	}, DiscoverRoots: func(ctx context.Context) []string {
		out := []string{s.root, s.gitConfig.WorktreeRoot, strings.TrimSpace(common)}
		trees, err := core.ListWorktreesContext(ctx, s.root)
		if err == nil {
			for _, tree := range trees {
				out = append(out, tree.Path)
			}
		}
		return out
	}, Publish: func(ctx context.Context, snapshot domain.RepositoryState) error {
		select {
		case snapshots <- snapshot:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		default:
			return fmt.Errorf("bridge snapshot queue full")
		}
	}}
	go func() { _ = watch.Run(ctx) }()
	b := gitbridge.Bridge{URL: s.gitConfig.URL, Credential: s.gitConfig.Credential, RepositoryIDs: []string{s.gitConfig.RepositoryID}, Executor: s.gitExecutor, Snapshots: snapshots}
	return b.Run(ctx)
}
func (s *Server) gitCommand(c gitbridge.Command) gitbridge.Result {
	if e := s.initGit(); e != nil {
		return gitbridge.Result{ID: c.ID, Error: &domain.Error{Code: "internal", Message: e.Error()}}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	return s.gitExecutor.Execute(ctx, c)
}
