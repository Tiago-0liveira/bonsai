// Package git defines Bonsai's transport-independent Git domain.
package git

import "time"

type RepositoryState struct {
	// AffectedWorktrees is an in-process watcher hint, never canonical wire state.
	AffectedWorktrees []string   `json:"-"`
	ID                string     `json:"id"`
	DefaultBranch     string     `json:"default_branch"`
	Branches          []Branch   `json:"branches"`
	Worktrees         []Worktree `json:"worktrees"`
}
type Branch struct {
	Name              string `json:"name"`
	LocalHeadSHA      string `json:"local_head_sha,omitempty"`
	LocalRemoteRefSHA string `json:"local_remote_ref_sha,omitempty"`
	RemoteHeadSHA     string `json:"remote_head_sha,omitempty"`
	Upstream          string `json:"upstream,omitempty"`
	Remote            bool   `json:"remote"`
}
type Worktree struct {
	ID           string             `json:"id"`
	RepositoryID string             `json:"repository_id"`
	Branch       string             `json:"branch"`
	HeadSHA      string             `json:"local_head_sha"`
	Main         bool               `json:"main"`
	Status       *WorkingTreeStatus `json:"status,omitempty"`
}
type WorkingTreeStatus struct {
	ContentVersion    string       `json:"content_version"`
	Branch            string       `json:"branch"`
	HeadSHA           string       `json:"local_head_sha"`
	Upstream          string       `json:"upstream"`
	LocalRemoteRefSHA string       `json:"local_remote_ref_sha"`
	Ahead             int          `json:"ahead"`
	Behind            int          `json:"behind"`
	Staged            int          `json:"staged"`
	Modified          int          `json:"modified"`
	Untracked         int          `json:"untracked"`
	Conflicted        []string     `json:"conflicted"`
	Files             []FileStatus `json:"files"`
	LastCommit        *Commit      `json:"last_commit,omitempty"`
	StashCount        int          `json:"stash_count"`
	Dirty             bool         `json:"dirty"`
	GitState          string       `json:"git_state"`
}
type FileStatus struct {
	Path     string `json:"path"`
	Index    string `json:"index"`
	Worktree string `json:"worktree"`
}
type FileEntry struct {
	Path   string `json:"path"`
	Status string `json:"status"`
}
type FileContent struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	Binary  bool   `json:"binary"`
}
type Commit struct {
	SHA     string    `json:"sha"`
	Subject string    `json:"subject"`
	Author  string    `json:"author"`
	When    time.Time `json:"when"`
}
type CreateWorktreeRequest struct {
	RepositoryID string `json:"repository_id"`
	Mode         string `json:"mode"` // existing | remote | new
	Branch       string `json:"branch"`
	Base         string `json:"base"`
}
type RemoveWorktreeRequest struct {
	WorktreeID     string `json:"worktree_id"`
	ConfirmDiscard bool   `json:"confirm_discard"`
}
type DiffRequest struct {
	WorktreeID string `json:"worktree_id"`
	Mode       string `json:"mode"`
	Base       string `json:"base"`
}
type FileDiffRequest struct {
	DiffRequest
	Path string `json:"path"`
}
type Diff struct {
	Patch string     `json:"patch"`
	Files []FileDiff `json:"files"`
}
type FileDiff struct {
	Path      string `json:"path"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
	Patch     string `json:"patch,omitempty"`
}
type Operation struct {
	ID              string    `json:"id"`
	WorktreeID      string    `json:"worktree_id"`
	Kind            string    `json:"kind"`
	State           string    `json:"state"`
	ConflictedPaths []string  `json:"conflicted_paths"`
	GitState        string    `json:"git_state"`
	CanContinue     bool      `json:"can_continue"`
	CanAbort        bool      `json:"can_abort"`
	Error           string    `json:"error,omitempty"`
	UpdatedAt       time.Time `json:"updated_at"`
}
