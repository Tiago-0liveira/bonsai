package git

import "context"

type LocalGitService interface {
	Repository(context.Context, string) (RepositoryState, error)
	ListBranches(context.Context, string) ([]Branch, error)
	ListWorktrees(context.Context, string) ([]Worktree, error)
	CreateWorktree(context.Context, CreateWorktreeRequest) (Worktree, error)
	RemoveWorktree(context.Context, RemoveWorktreeRequest) error
	Status(context.Context, string) (WorkingTreeStatus, error)
	Files(context.Context, string) ([]FileEntry, error)
	ReadFile(context.Context, string, string) (FileContent, error)
	Diff(context.Context, DiffRequest) (Diff, error)
	DiffFile(context.Context, FileDiffRequest) (FileDiff, error)
	Fetch(context.Context, string) error
	Pull(context.Context, string) error
	Push(context.Context, string, bool) error
	Commit(context.Context, string, string) (Commit, error)
	Rebase(context.Context, string, string) (Operation, error)
	Merge(context.Context, string, string) (Operation, error)
	// PullOperation and recovery retain conflict details for web callers.
	PullOperation(context.Context, string) (Operation, error)
	Continue(context.Context, string, string) (Operation, error)
	Abort(context.Context, string, string) (Operation, error)
}

// Operation IDs are assigned by the command boundary so queued, running and
// terminal events refer to the same operation across transports.
type operationIDKey struct{}

func WithOperationID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, operationIDKey{}, id)
}
func OperationID(ctx context.Context) string {
	id, _ := ctx.Value(operationIDKey{}).(string)
	return id
}
