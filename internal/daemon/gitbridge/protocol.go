// Package gitbridge is the allowlisted, outbound-only Git bridge. It never
// exposes the daemon's local process/shell protocol to the web server.
package gitbridge

import (
	"encoding/json"
	domain "github.com/Tiago-0liveira/bonsai/internal/git"
	"time"
)

type Command struct {
	ID           string          `json:"id"`
	UserID       string          `json:"user_id"`
	RepositoryID string          `json:"repository_id"`
	WorktreeID   string          `json:"worktree_id,omitempty"`
	Type         string          `json:"type"`
	Arguments    json.RawMessage `json:"arguments"`
	CreatedAt    time.Time       `json:"created_at"`
}
type Result struct {
	ID      string          `json:"id"`
	Payload json.RawMessage `json:"payload,omitempty"`
	Error   *domain.Error   `json:"error,omitempty"`
}
type Frame struct {
	Operation    *domain.Operation       `json:"operation,omitempty"`
	Type         string                  `json:"type"`
	Repositories []string                `json:"repositories,omitempty"`
	Command      *Command                `json:"command,omitempty"`
	Result       *Result                 `json:"result,omitempty"`
	RepositoryID string                  `json:"repository_id,omitempty"`
	Snapshot     *domain.RepositoryState `json:"snapshot,omitempty"`
	Cursor       uint64                  `json:"cursor,omitempty"`
}

// git.inventory is a strict subset of git.repository.refresh (worktrees,
// branches, remotes; no status), so it is a read with the same authority.
var reads = map[string]bool{"git.repository.refresh": true, "git.inventory": true, "git.branches": true, "git.worktrees": true, "git.status": true, "git.files": true, "git.file.read": true, "git.diff.read": true}

func IsRead(kind string) bool { return reads[kind] }
func Allowed(kind string) bool {
	if kind == "git.repository.sync" {
		return true
	}
	return IsRead(kind) || map[string]bool{"git.fetch": true, "git.worktree.create": true, "git.worktree.remove": true, "git.pull": true, "git.push": true, "git.commit": true, "git.stage": true, "git.unstage": true, "git.rebase": true, "git.merge": true, "git.operation.continue": true, "git.operation.abort": true}[kind]
}

func IsOperation(kind string) bool {
	return kind == "git.rebase" || kind == "git.merge" || kind == "git.pull" || kind == "git.operation.continue" || kind == "git.operation.abort"
}
