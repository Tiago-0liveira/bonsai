package gitbridge

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	domain "github.com/Tiago-0liveira/bonsai/internal/git"
	store "github.com/Tiago-0liveira/bonsai/internal/storage/git"
	"io"
	"sync"
	"time"
)

type Executor struct {
	Local   domain.LocalGitService
	Journal *store.Store
	mu      sync.Mutex
}
type audit struct {
	Command Command `json:"command"`
	Hash    string  `json:"hash"`
	State   string  `json:"state"`
	Result  Result  `json:"result"`
}
type arguments struct {
	FastForwardOnly bool     `json:"fast_forward_only"`
	Mode            string   `json:"mode"`
	Branch          string   `json:"branch"`
	Base            string   `json:"base"`
	Target          string   `json:"target"`
	Path            string   `json:"path"`
	Paths           []string `json:"paths"`
	Message         string   `json:"message"`
	SetUpstream     bool     `json:"set_upstream"`
	ConfirmDiscard  bool     `json:"confirm_discard"`
	OperationID     string   `json:"operation_id"`
}

func failure(id string, e error) Result {
	var de *domain.Error
	if !errors.As(e, &de) {
		de = &domain.Error{Code: domain.Code(e), Message: e.Error()}
	}
	return Result{ID: id, Error: de}
}
func (x *Executor) Execute(ctx context.Context, c Command) Result {
	if c.ID == "" || c.UserID == "" || c.RepositoryID == "" || !Allowed(c.Type) || c.CreatedAt.IsZero() {
		return failure(c.ID, domain.ErrInvalid)
	}
	if len(c.Arguments) == 0 {
		c.Arguments = json.RawMessage(`{}`)
	}
	var a arguments
	dec := json.NewDecoder(bytes.NewReader(c.Arguments))
	dec.DisallowUnknownFields()
	if e := dec.Decode(&a); e != nil {
		return failure(c.ID, domain.ErrInvalid)
	}
	if dec.Decode(new(any)) != io.EOF {
		return failure(c.ID, domain.ErrInvalid)
	}
	var e error
	if !IsRead(c.Type) {
		x.mu.Lock()
		defer x.mu.Unlock()
	}
	b, _ := json.Marshal(c)
	hash := fmt.Sprintf("%x", sha256.Sum256(b))
	var old audit
	var exists bool
	if !IsRead(c.Type) {
		if x.Journal == nil {
			return failure(c.ID, domain.E("internal", "mutation journal unavailable"))
		}
		x.Journal.View(func(d store.Data) error { old, exists = store.Get[audit](d, "commands", c.ID); return nil })
		if exists {
			if old.Hash != hash {
				return failure(c.ID, domain.ErrInvalid)
			}
			if old.State == "done" {
				return old.Result
			}
			return failure(c.ID, domain.ErrUncertain)
		}
		if time.Since(c.CreatedAt) > 24*time.Hour || c.CreatedAt.After(time.Now().Add(time.Minute)) {
			return failure(c.ID, domain.E("expired", "command expired"))
		}
	}
	// Bind a worktree target to its repository for EVERY command, including reads.
	trees, e := x.Local.ListWorktrees(ctx, c.RepositoryID)
	if e != nil {
		return failure(c.ID, e)
	}
	if c.WorktreeID != "" {
		found := false
		for _, w := range trees {
			found = found || w.ID == c.WorktreeID
		}
		if !found {
			return failure(c.ID, domain.ErrForbidden)
		}
	}
	if !IsRead(c.Type) {
		if e = x.Journal.Update(func(d store.Data) error {
			return store.Put(d, "commands", c.ID, audit{Command: c, Hash: hash, State: "running"})
		}); e != nil {
			return failure(c.ID, e)
		}
	}
	var value any
	ctx = domain.WithOperationID(ctx, c.ID)
	switch c.Type {
	case "git.repository.sync":
		syncer, ok := x.Local.(interface {
			SyncRepository(context.Context, string, domain.PullPolicy) (domain.RepositorySync, error)
		})
		if !ok {
			e = domain.ErrInvalid
		} else {
			value, e = syncer.SyncRepository(ctx, c.RepositoryID, domain.PullPolicy{FastForwardOnly: a.FastForwardOnly})
		}
	case "git.repository.refresh":
		value, e = x.Local.Repository(ctx, c.RepositoryID)
	case "git.branches":
		value, e = x.Local.ListBranches(ctx, c.RepositoryID)
	case "git.worktrees":
		value, e = x.Local.ListWorktrees(ctx, c.RepositoryID)
	case "git.status":
		value, e = x.Local.Status(ctx, c.WorktreeID)
	case "git.files":
		value, e = x.Local.Files(ctx, c.WorktreeID)
	case "git.file.read":
		value, e = x.Local.ReadFile(ctx, c.WorktreeID, a.Path)
	case "git.diff.read":
		req := domain.DiffRequest{WorktreeID: c.WorktreeID, Mode: a.Mode, Base: a.Base}
		if a.Path != "" {
			value, e = x.Local.DiffFile(ctx, domain.FileDiffRequest{DiffRequest: req, Path: a.Path})
		} else {
			value, e = x.Local.Diff(ctx, req)
		}
	case "git.fetch":
		e = x.Local.Fetch(ctx, c.RepositoryID)
	case "git.worktree.create":
		value, e = x.Local.CreateWorktree(ctx, domain.CreateWorktreeRequest{RepositoryID: c.RepositoryID, Mode: a.Mode, Branch: a.Branch, Base: a.Base})
	case "git.worktree.remove":
		e = x.Local.RemoveWorktree(ctx, domain.RemoveWorktreeRequest{WorktreeID: c.WorktreeID, ConfirmDiscard: a.ConfirmDiscard})
	case "git.pull":
		if a.FastForwardOnly {
			puller, ok := x.Local.(interface {
				PullWithPolicy(context.Context, string, domain.PullPolicy) (domain.Operation, error)
			})
			if !ok {
				e = domain.ErrInvalid
			} else {
				value, e = puller.PullWithPolicy(ctx, c.WorktreeID, domain.PullPolicy{FastForwardOnly: true})
			}
		} else {
			value, e = x.Local.PullOperation(ctx, c.WorktreeID)
		}
	case "git.push":
		e = x.Local.Push(ctx, c.WorktreeID, a.SetUpstream)
	case "git.commit":
		value, e = x.Local.Commit(ctx, c.WorktreeID, a.Message)
	case "git.rebase":
		value, e = x.Local.Rebase(ctx, c.WorktreeID, a.Target)
	case "git.merge":
		value, e = x.Local.Merge(ctx, c.WorktreeID, a.Target)
	case "git.operation.continue":
		value, e = x.Local.Continue(ctx, c.WorktreeID, a.OperationID)
	case "git.operation.abort":
		value, e = x.Local.Abort(ctx, c.WorktreeID, a.OperationID)
	case "git.stage", "git.unstage":
		stager, ok := x.Local.(interface {
			Stage(context.Context, string, []string, bool) error
		})
		if !ok {
			e = domain.ErrInvalid
		} else {
			e = stager.Stage(ctx, c.WorktreeID, a.Paths, c.Type == "git.unstage")
		}
	}
	result := Result{ID: c.ID}
	if e != nil {
		result = failure(c.ID, e)
	} else {
		result.Payload, _ = json.Marshal(value)
	}
	if !IsRead(c.Type) {
		if err := x.Journal.Update(func(d store.Data) error {
			return store.Put(d, "commands", c.ID, audit{Command: c, Hash: hash, State: "done", Result: result})
		}); err != nil {
			return failure(c.ID, domain.ErrUncertain)
		}
	}
	return result
}
