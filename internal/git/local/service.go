// Package local implements Git on the daemon's machine. Callers use registered
// IDs, never filesystem paths. The legacy core runner and worktree parser remain
// shared with the CLI/TUI.
package local

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	core "github.com/Tiago-0liveira/bonsai/internal/core/git"
	domain "github.com/Tiago-0liveira/bonsai/internal/git"
)

type Config struct{ ID, Root, WorktreeRoot string }
type repository struct {
	Config
	gate chan struct{}
}
type Service struct {
	repos      map[string]*repository
	mu         sync.Mutex
	operations map[string]domain.Operation
}

var _ domain.LocalGitService = (*Service)(nil)

func New(configs []Config) (*Service, error) {
	s := &Service{repos: map[string]*repository{}, operations: map[string]domain.Operation{}}
	for _, c := range configs {
		if c.ID == "" || s.repos[c.ID] != nil {
			return nil, domain.ErrInvalid
		}
		root, err := core.MainRoot(c.Root)
		if err != nil {
			return nil, err
		}
		c.Root, err = filepath.EvalSymlinks(root)
		if err != nil {
			return nil, err
		}
		if c.WorktreeRoot == "" {
			return nil, fmt.Errorf("worktree root must be configured")
		}
		c.WorktreeRoot, err = filepath.Abs(c.WorktreeRoot)
		if err != nil {
			return nil, err
		}
		if err = os.MkdirAll(c.WorktreeRoot, 0700); err != nil {
			return nil, err
		}
		c.WorktreeRoot, err = filepath.EvalSymlinks(c.WorktreeRoot)
		if err != nil {
			return nil, err
		}
		s.repos[c.ID] = &repository{Config: c, gate: make(chan struct{}, 1)}
	}
	return s, nil
}
func ID(repo, path string) string {
	return fmt.Sprintf("wt-%x", sha256.Sum256([]byte(repo+"\x00"+filepath.Clean(path))))[:35]
}
func (s *Service) repo(id string) (*repository, error) {
	r := s.repos[id]
	if r == nil {
		return nil, domain.ErrNotFound
	}
	return r, nil
}
func (r *repository) lock(ctx context.Context) (func(), error) {
	select {
	case r.gate <- struct{}{}:
		return func() { <-r.gate }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func run(ctx context.Context, dir string, args ...string) (string, error) {
	return core.RunContext(ctx, dir, args...)
}
func trimmed(ctx context.Context, dir string, args ...string) (string, error) {
	v, e := run(ctx, dir, args...)
	return strings.TrimSpace(v), e
}
func (s *Service) target(ctx context.Context, id string) (*repository, string, error) {
	for _, r := range s.repos {
		trees, err := core.ListWorktreesContext(ctx, r.Root)
		if err != nil {
			return nil, "", err
		}
		for _, t := range trees {
			if !t.Bare && ID(r.ID, t.Path) == id {
				p, e := filepath.EvalSymlinks(t.Path)
				if e != nil {
					return nil, "", e
				}
				return r, p, nil
			}
		}
	}
	return nil, "", domain.ErrNotFound
}
func ref(ctx context.Context, dir, name string) (string, error) {
	if name == "" || strings.HasPrefix(name, "-") || strings.ContainsAny(name, "\x00\r\n") {
		return "", domain.ErrInvalid
	}
	sha, err := trimmed(ctx, dir, "rev-parse", "--verify", "--end-of-options", name+"^{commit}")
	if err != nil {
		return "", domain.E("not_found", "ref does not resolve to a commit")
	}
	return sha, nil
}
func branch(ctx context.Context, dir, name string) error {
	if name == "" || strings.HasPrefix(name, "-") || strings.HasPrefix(name, "refs/") {
		return domain.ErrInvalid
	}
	_, err := run(ctx, dir, "check-ref-format", "refs/heads/"+name)
	if err != nil {
		return domain.E("invalid", "invalid branch name")
	}
	return nil
}
func (s *Service) Repository(ctx context.Context, id string) (domain.RepositoryState, error) {
	r, e := s.repo(id)
	if e != nil {
		return domain.RepositoryState{}, e
	}
	unlock, e := r.lock(ctx)
	if e != nil {
		return domain.RepositoryState{}, e
	}
	defer unlock()

	b, e := s.ListBranches(ctx, id)
	if e != nil {
		return domain.RepositoryState{}, e
	}
	w, e := s.ListWorktrees(ctx, id)
	if e != nil {
		return domain.RepositoryState{}, e
	}

	paths, e := core.ListWorktreesContext(ctx, r.Root)
	if e != nil {
		return domain.RepositoryState{}, e
	}
	byID := map[string]string{}
	for _, tree := range paths {
		byID[ID(id, tree.Path)] = tree.Path
	}
	for i := range w {
		st, err := status(ctx, byID[w[i].ID])
		if err != nil {
			return domain.RepositoryState{}, err
		}
		w[i].Status = &st
	}

	def, _ := trimmed(ctx, r.Root, "symbolic-ref", "--quiet", "refs/remotes/origin/HEAD")
	return domain.RepositoryState{ID: id, DefaultBranch: strings.TrimPrefix(def, "refs/remotes/origin/"), Branches: b, Worktrees: w}, nil
}
func (s *Service) ListBranches(ctx context.Context, id string) ([]domain.Branch, error) {
	r, e := s.repo(id)
	if e != nil {
		return nil, e
	}
	out, e := run(ctx, r.Root, "for-each-ref", "--format=%(refname)%00%(objectname)%00%(upstream:short)%00%(symref)", "refs/heads/", "refs/remotes/")
	if e != nil {
		return nil, e
	}
	result := []domain.Branch{}
	for _, line := range strings.Split(strings.TrimSuffix(out, "\n"), "\n") {
		p := strings.Split(line, "\x00")
		if len(p) != 4 || p[3] != "" {
			continue
		}
		b := domain.Branch{Upstream: p[2]}
		b.Remote = strings.HasPrefix(p[0], "refs/remotes/")
		if b.Remote {
			b.Name = strings.TrimPrefix(p[0], "refs/remotes/")
			b.LocalRemoteRefSHA = p[1]
		} else {
			b.Name = strings.TrimPrefix(p[0], "refs/heads/")
			b.LocalHeadSHA = p[1]
		}
		result = append(result, b)
	}
	return result, nil
}
func (s *Service) ListWorktrees(ctx context.Context, id string) ([]domain.Worktree, error) {
	r, e := s.repo(id)
	if e != nil {
		return nil, e
	}
	trees, e := core.ListWorktreesContext(ctx, r.Root)
	if e != nil {
		return nil, e
	}
	result := []domain.Worktree{}
	for _, t := range trees {
		if t.Bare {
			continue
		}
		result = append(result, domain.Worktree{ID: ID(id, t.Path), RepositoryID: id, Branch: t.Branch, HeadSHA: t.HEAD, Main: t.IsMain})
	}
	return result, nil
}
