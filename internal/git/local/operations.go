package local

import (
	"context"
	"crypto/rand"
	"errors"
	domain "github.com/Tiago-0liveira/bonsai/internal/git"
	"strings"
	"time"
)

func (s *Service) Fetch(ctx context.Context, id string) error {
	r, e := s.repo(id)
	if e != nil {
		return e
	}
	u, e := r.lock(ctx)
	if e != nil {
		return e
	}
	defer u()
	_, e = run(ctx, r.Root, "fetch", "--all", "--prune")
	return e
}
func (s *Service) Push(ctx context.Context, id string, upstream bool) error {
	r, dir, e := s.target(ctx, id)
	if e != nil {
		return e
	}
	u, e := r.lock(ctx)
	if e != nil {
		return e
	}
	defer u()
	args := []string{"push"}
	if upstream {
		args = append(args, "--set-upstream", "origin", "HEAD")
	}
	_, e = run(ctx, dir, args...)
	return e
}

// Commit commits the existing index; staging is an explicit separate command.
func (s *Service) Commit(ctx context.Context, id, message string) (domain.Commit, error) {
	if strings.TrimSpace(message) == "" || len(message) > 65536 {
		return domain.Commit{}, domain.ErrInvalid
	}
	r, dir, e := s.target(ctx, id)
	if e != nil {
		return domain.Commit{}, e
	}
	u, e := r.lock(ctx)
	if e != nil {
		return domain.Commit{}, e
	}
	defer u()
	st, e := status(ctx, dir)
	if e != nil {
		return domain.Commit{}, e
	}
	if st.GitState != "normal" {
		return domain.Commit{}, domain.ErrBusy
	}
	if _, e = run(ctx, dir, "commit", "-m", message); e != nil {
		return domain.Commit{}, e
	}
	return lastCommit(ctx, dir)
}
func (s *Service) Stage(ctx context.Context, id string, paths []string, unstage bool) error {
	if len(paths) == 0 {
		return domain.ErrInvalid
	}
	for _, p := range paths {
		if e := validPath(p); e != nil {
			return e
		}
	}
	r, dir, e := s.target(ctx, id)
	if e != nil {
		return e
	}
	u, e := r.lock(ctx)
	if e != nil {
		return e
	}
	defer u()
	args := []string{"add", "--"}
	if unstage {
		args = []string{"restore", "--staged", "--"}
	}
	for _, p := range paths {
		args = append(args, ":(literal)"+p)
	}
	_, e = run(ctx, dir, args...)
	return e
}
func (s *Service) Pull(ctx context.Context, id string) error {
	op, e := s.PullOperation(ctx, id)
	if e != nil {
		return e
	}
	if op.State == "conflict" {
		return domain.ErrConflict
	}
	if op.State != "completed" {
		return domain.E(op.State, op.Error)
	}
	return nil
}
func (s *Service) PullOperation(ctx context.Context, id string) (domain.Operation, error) {
	return s.operate(ctx, id, "pull", "", "")
}
func (s *Service) PullWithPolicy(ctx context.Context, id string, policy domain.PullPolicy) (domain.Operation, error) {
	return s.operatePolicy(ctx, id, "pull", "", "", policy)
}
func (s *Service) Rebase(ctx context.Context, id, target string) (domain.Operation, error) {
	return s.operate(ctx, id, "rebase", target, "")
}
func (s *Service) Merge(ctx context.Context, id, target string) (domain.Operation, error) {
	return s.operate(ctx, id, "merge", target, "")
}
func (s *Service) Continue(ctx context.Context, id, opID string) (domain.Operation, error) {
	return s.operate(ctx, id, "", opID, "continue")
}
func (s *Service) Abort(ctx context.Context, id, opID string) (domain.Operation, error) {
	return s.operate(ctx, id, "", opID, "abort")
}
func (s *Service) operate(ctx context.Context, id, kind, target, action string) (domain.Operation, error) {
	return s.operatePolicy(ctx, id, kind, target, action, domain.PullPolicy{})
}
func (s *Service) operatePolicy(ctx context.Context, id, kind, target, action string, policy domain.PullPolicy) (domain.Operation, error) {
	r, dir, e := s.target(ctx, id)
	if e != nil {
		return domain.Operation{}, e
	}
	u, e := r.lock(ctx)
	if e != nil {
		return domain.Operation{}, e
	}
	defer u()
	st, e := status(ctx, dir)
	if e != nil {
		return domain.Operation{}, e
	}
	opID := domain.OperationID(ctx)
	if opID == "" {
		opID = rand.Text()
	}
	op := domain.Operation{ID: opID, WorktreeID: id, Kind: kind, State: "running", UpdatedAt: time.Now().UTC()}
	var args []string
	if action != "" {
		s.mu.Lock()
		old, ok := s.operations[target]
		s.mu.Unlock()
		// The deterministic recovery ID also permits recovering after daemon restart.
		if !ok && target == "recover" && (st.GitState == "merge" || st.GitState == "rebase") {
			old = domain.Operation{ID: target, WorktreeID: id, Kind: st.GitState}
			ok = true
		}
		if !ok || old.WorktreeID != id || !(st.GitState == "merge" || st.GitState == "rebase") {
			return domain.Operation{}, domain.ErrNotFound
		}
		op = old
		op.State = "running"
		args = []string{st.GitState, "--" + action}
	} else {
		if st.GitState != "normal" {
			return domain.Operation{}, domain.ErrBusy
		}
		if st.Dirty {
			return domain.Operation{}, domain.ErrDirty
		}
		switch kind {
		case "pull":
			args = []string{"pull", "--no-rebase", "--no-edit"}
			if policy.FastForwardOnly {
				args = []string{"pull", "--ff-only", "--no-rebase", "--no-autostash", "--no-edit"}
			}
		case "rebase", "merge":
			sha, e := ref(ctx, dir, target)
			if e != nil {
				return domain.Operation{}, e
			}
			args = []string{kind}
			if kind == "merge" {
				args = append(args, "--no-edit")
			}
			args = append(args, sha)
		default:
			return domain.Operation{}, domain.ErrInvalid
		}
	}
	_, execErr := run(ctx, dir, args...)
	// Cancellation must still report any recovery state left by Git.
	inspect, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	after, e := status(inspect, dir)
	if e != nil {
		return op, e
	}
	op.ConflictedPaths = after.Conflicted
	op.GitState = after.GitState
	op.CanAbort = after.GitState == "merge" || after.GitState == "rebase"
	op.CanContinue = op.CanAbort && len(after.Conflicted) == 0
	op.State = "completed"
	op.Error = ""
	if execErr != nil {
		op.State = "failed"
		op.Error = execErr.Error()
		if errors.Is(execErr, context.Canceled) || errors.Is(execErr, context.DeadlineExceeded) {
			op.State = "cancelled"
		}
		if op.CanAbort || len(after.Conflicted) > 0 {
			op.State = "conflict"
		}
	}
	if action == "abort" && execErr == nil {
		op.State = "cancelled"
	}
	op.UpdatedAt = time.Now().UTC()
	s.mu.Lock()
	s.operations[op.ID] = op
	s.mu.Unlock()
	return op, nil
}
