package local

import (
	"context"
	"crypto/sha256"
	"fmt"
	domain "github.com/Tiago-0liveira/bonsai/internal/git"
	"os"
	"path/filepath"
	"strings"
)

func (s *Service) CreateWorktree(ctx context.Context, req domain.CreateWorktreeRequest) (domain.Worktree, error) {
	r, e := s.repo(req.RepositoryID)
	if e != nil {
		return domain.Worktree{}, e
	}
	unlock, e := r.lock(ctx)
	if e != nil {
		return domain.Worktree{}, e
	}
	defer unlock()
	if r.WithWorktreeRoot != nil {
		var result domain.Worktree
		err := r.WithWorktreeRoot(ctx, func(root string) error { var err error; result, err = s.createWorktree(ctx, r, req, root); return err })
		return result, err
	}
	return s.createWorktree(ctx, r, req, r.WorktreeRoot)
}
func (s *Service) createWorktree(ctx context.Context, r *repository, req domain.CreateWorktreeRequest, root string) (domain.Worktree, error) {
	var e error
	if e = branch(ctx, r.Root, req.Branch); e != nil {
		return domain.Worktree{}, e
	}
	path := filepath.Join(root, fmt.Sprintf("%x", sha256.Sum256([]byte(req.Branch)))[:24])
	args := []string{"worktree", "add"}
	switch req.Mode {
	case "existing":
		if _, e = ref(ctx, r.Root, "refs/heads/"+req.Branch); e != nil {
			return domain.Worktree{}, e
		}
		trees, e := s.ListWorktrees(ctx, r.ID)
		if e != nil {
			return domain.Worktree{}, e
		}
		for _, t := range trees {
			if t.Branch == req.Branch {
				return domain.Worktree{}, domain.E("busy", "branch is already checked out")
			}
		}
		args = append(args, "--", path, req.Branch)
	case "new":
		base, e := ref(ctx, r.Root, req.Base)
		if e != nil {
			return domain.Worktree{}, e
		}
		args = append(args, "-b", req.Branch, "--", path, base)
	case "remote":
		if !strings.HasPrefix(req.Base, "origin/") {
			return domain.Worktree{}, domain.E("invalid", "remote worktrees require an origin branch")
		}
		if e = branch(ctx, r.Root, strings.TrimPrefix(req.Base, "origin/")); e != nil {
			return domain.Worktree{}, e
		}
		if _, e = run(ctx, r.Root, "fetch", "origin", "--prune"); e != nil {
			return domain.Worktree{}, e
		}
		if _, e = ref(ctx, r.Root, "refs/remotes/"+req.Base); e != nil {
			return domain.Worktree{}, e
		}
		args = append(args, "--track", "-b", req.Branch, "--", path, req.Base)
	default:
		return domain.Worktree{}, domain.ErrInvalid
	}
	if _, err := os.Lstat(path); err == nil {
		return domain.Worktree{}, domain.E("conflict", "worktree destination already exists")
	} else if !os.IsNotExist(err) {
		return domain.Worktree{}, err
	}
	if _, e = run(ctx, r.Root, args...); e != nil {
		return domain.Worktree{}, e
	}
	trees, e := s.ListWorktrees(ctx, r.ID)
	if e != nil {
		return domain.Worktree{}, e
	}
	for _, t := range trees {
		if t.ID == ID(r.ID, path) {
			st, err := status(ctx, path)
			t.Status = &st
			return t, err
		}
	}
	return domain.Worktree{}, domain.ErrNotFound
}
func (s *Service) RemoveWorktree(ctx context.Context, req domain.RemoveWorktreeRequest) error {
	r, path, e := s.target(ctx, req.WorktreeID)
	if e != nil {
		return e
	}
	unlock, e := r.lock(ctx)
	if e != nil {
		return e
	}
	defer unlock()
	if path == r.Root {
		return domain.E("forbidden", "cannot remove main worktree")
	}
	st, e := status(ctx, path)
	if e != nil {
		return e
	}
	if st.Dirty && !req.ConfirmDiscard {
		return domain.ErrDirty
	}
	if st.GitState != "normal" {
		return domain.ErrBusy
	}
	args := []string{"worktree", "remove"}
	if req.ConfirmDiscard {
		args = append(args, "--force")
	}
	args = append(args, "--", path)
	_, e = run(ctx, r.Root, args...)
	return e
}
