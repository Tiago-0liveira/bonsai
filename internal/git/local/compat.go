// Compatibility names preserve CLI/TUI behavior while the daemon and web use
// the context-aware domain interfaces. Implementation remains in core.
package local

import core "github.com/Tiago-0liveira/bonsai/internal/core/git"

type Worktree = core.Worktree
type Metrics = core.Metrics
type StatusSummary = core.StatusSummary
type HeadCommit = core.HeadCommit
type DiffFile = core.DiffFile

var RunContext = core.RunContext
var RepoRoot = core.RepoRoot
var MainRoot = core.MainRoot
var ListWorktrees = core.ListWorktrees
var ListWorktreesContext = core.ListWorktreesContext
var AheadBehind = core.AheadBehind
var ListBranches = core.ListBranches
var RemoveWorktree = core.RemoveWorktree
var ForceRemoveWorktree = core.ForceRemoveWorktree
var DeleteBranch = core.DeleteBranch
var WorktreePath = core.WorktreePath
var AddWorktreeNewBranch = core.AddWorktreeNewBranch
var AddWorktreeExisting = core.AddWorktreeExisting
var CreateWorktreeFromPR = core.CreateWorktreeFromPR
var Push = core.Push
var Pull = core.Pull
var Commit = core.Commit
var Log = core.Log
var Status = core.Status
var RebaseCmd = core.RebaseCmd
var MergeCmd = core.MergeCmd
var StatusSummaryOf = core.StatusSummaryOf
var LastCommit = core.LastCommit
var StashCount = core.StashCount
var LastCommitUnix = core.LastCommitUnix
var DiffBase = core.DiffBase
var DiffStat = core.DiffStat
var FileDiff = core.FileDiff
var Fetch = core.Fetch
var PushSetUpstream = core.PushSetUpstream
var MergedBranches = core.MergedBranches
var ErrWorktreeDirty = core.ErrWorktreeDirty
