package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	coreexec "github.com/Tiago-0liveira/bonsai/internal/core/exec"
	"github.com/Tiago-0liveira/bonsai/internal/core/fs"
	"github.com/Tiago-0liveira/bonsai/internal/core/gh"
	"github.com/Tiago-0liveira/bonsai/internal/core/git"
	"github.com/Tiago-0liveira/bonsai/internal/core/pkgmgr"
)

// --- Messages ---

type worktreesMsg struct {
	trees []git.Worktree
	err   error
}

type metricsMsg struct {
	path    string
	metrics git.Metrics
	err     error
}

type logMsg struct {
	content string
	err     error
}

type fileIndexMsg struct {
	files []string
	err   error
}

type scriptsMsg struct {
	manager string            // detected package manager name, "" if none
	scripts []string          // selectable script/target names
	runCmd  map[string]string // script name -> full shell command
	err     error
}

// opDoneMsg reports completion of a git/fs/exec operation.
type opDoneMsg struct {
	label string
	err   error
}

// procTickMsg drives periodic refresh of the process-output viewport.
type procTickMsg struct{}

// prTickMsg drives periodic re-check of pull-request states so merges made
// outside bonsai show up without a restart or manual refresh.
type prTickMsg struct{}

// prsMsg carries the loaded pull-request list for the create-from-PR modal.
type prsMsg struct {
	prs []gh.PR
	err error
}

// prMapMsg carries pull requests (any state) used to decorate worktree rows
// with badges. Errors are handled silently (gh missing/unauthenticated simply
// means no badges).
type prMapMsg struct {
	prs []gh.PR
	err error
}

// prDetailMsg carries a fully-loaded PR for the PR detail tab.
type prDetailMsg struct {
	detail gh.PRDetail
	err    error
}

// checksMsg carries a worktree's PR CI checks + rollup for the row badge and the
// PR tab checks section.
type checksMsg struct {
	path   string
	number int
	checks []gh.Check
	rollup string
}

// dirtyMsg reports whether a worktree has uncommitted changes.
type dirtyMsg struct {
	path  string
	dirty bool
}

// activityMsg carries a worktree's HEAD commit time (for sort-by-activity).
type activityMsg struct {
	path string
	unix int64
}

// diffMsg carries the changed-file list for the Diff tab.
type diffMsg struct {
	path  string
	base  string
	files []git.DiffFile
	err   error
}

// fileDiffMsg carries the colored diff of one expanded file.
type fileDiffMsg struct {
	path    string // worktree path (to validate the diff still applies)
	file    string
	content string
	err     error
}

// prCreatedMsg reports the outcome of creating a pull request.
type prCreatedMsg struct {
	number int
	err    error
}

// --- Commands ---

// loadWorktrees lists worktrees from the main repo.
func loadWorktrees(repoDir string) tea.Cmd {
	return func() tea.Msg {
		trees, err := git.ListWorktrees(repoDir)
		return worktreesMsg{trees: trees, err: err}
	}
}

// loadMetrics computes ahead/behind for one worktree branch against upstream.
func loadMetrics(path, branch, upstream string) tea.Cmd {
	return func() tea.Msg {
		mtr, err := git.AheadBehind(path, branch, upstream)
		return metricsMsg{path: path, metrics: mtr, err: err}
	}
}

// loadLog fetches the commit graph for a worktree.
func loadLog(path string) tea.Cmd {
	return func() tea.Msg {
		out, err := git.Log(path)
		return logMsg{content: out, err: err}
	}
}

// indexFiles builds the fuzzy search index over the main repo.
func indexFiles(repoDir string) tea.Cmd {
	return func() tea.Msg {
		files, err := fs.Index(repoDir)
		return fileIndexMsg{files: files, err: err}
	}
}

// loadScripts detects a package manager and lists its scripts.
func loadScripts(path string) tea.Cmd {
	return func() tea.Msg {
		pm, err := pkgmgr.Detect(path)
		if err != nil {
			return scriptsMsg{err: err}
		}
		if pm == nil {
			return scriptsMsg{} // no manager: modal still offers the ad-hoc entry
		}
		names := pm.GetScripts()
		runCmd := make(map[string]string, len(names))
		for _, n := range names {
			runCmd[n] = pm.RunCommand(n)
		}
		return scriptsMsg{manager: pm.Name(), scripts: names, runCmd: runCmd}
	}
}

// gitPull / gitPush / gitCommit wrap the corresponding core ops.
func gitPull(path string) tea.Cmd {
	return func() tea.Msg { return opDoneMsg{label: "pull", err: git.Pull(path)} }
}

func gitPush(path string) tea.Cmd {
	return func() tea.Msg { return opDoneMsg{label: "push", err: git.Push(path)} }
}

func gitCommit(path, msg string) tea.Cmd {
	return func() tea.Msg { return opDoneMsg{label: "commit", err: git.Commit(path, msg)} }
}

// pruneWorktree optionally merges a PR, runs delete hooks in the worktree,
// removes it, then deletes its branch. When merge is true and prNumber > 0 the
// merge happens first and aborts the prune on failure. When force is true the
// removal discards uncommitted changes; otherwise a dirty worktree aborts the
// prune with git.ErrWorktreeDirty.
func pruneWorktree(repoDir, path, branch, upstream string, deleteHooks []string, merge, force bool, prNumber int) tea.Cmd {
	return func() tea.Msg {
		if merge && prNumber > 0 {
			if err := gh.MergePR(repoDir, prNumber); err != nil {
				return opDoneMsg{label: "merge PR", err: err}
			}
		}
		vars := config.HookVars(repoDir, path, branch, upstream, prNumber)
		if err := coreexec.RunHooks(path, deleteHooks, vars); err != nil {
			return opDoneMsg{label: "delete hook", err: err}
		}
		var err error
		if force {
			err = git.ForceRemoveWorktree(repoDir, path)
		} else {
			err = git.RemoveWorktree(repoDir, path)
		}
		if err != nil {
			return opDoneMsg{label: "prune", err: err}
		}
		if branch != "" {
			if err := git.DeleteBranch(repoDir, branch); err != nil {
				return opDoneMsg{label: "prune", err: err}
			}
		}
		return opDoneMsg{label: "prune", err: nil}
	}
}

// copyFile copies a main-repo relative file into the target worktree and records
// the copy in state for future ranking.
func copyFile(repoDir, worktreePath, rel string, record func(string) error) tea.Cmd {
	return func() tea.Msg {
		src := filepath.Join(repoDir, rel)
		dst := filepath.Join(worktreePath, rel)
		if err := fs.Copy(src, dst); err != nil {
			return opDoneMsg{label: "copy " + rel, err: err}
		}
		if record != nil {
			_ = record(rel)
		}
		return opDoneMsg{label: "copy " + rel, err: nil}
	}
}

// loadPRs fetches open pull requests via the gh CLI for the create-from-PR modal.
func loadPRs(repoDir string) tea.Cmd {
	return func() tea.Msg {
		prs, err := gh.ListPRs(repoDir, "open")
		return prsMsg{prs: prs, err: err}
	}
}

// branchesKind distinguishes which flow a loaded branch list belongs to.
type branchesKind int

const (
	branchesForRebase branchesKind = iota
	branchesForCreateExisting
)

// branchesMsg carries a loaded branch list for a select modal.
type branchesMsg struct {
	kind     branchesKind
	branches []string
	err      error
}

// loadBranches fetches local branch names for a select modal so the UI event
// loop is never blocked by the git call.
func loadBranches(kind branchesKind, dir string) tea.Cmd {
	return func() tea.Msg {
		branches, err := git.ListBranches(dir)
		return branchesMsg{kind: kind, branches: branches, err: err}
	}
}

// pruneCandidatesMsg carries the union of merged branch heads for the
// bulk-prune scan.
type pruneCandidatesMsg struct {
	merged map[string]bool
	err    error
}

// loadPruneCandidates unions merged PR heads (gh) with locally-merged branches
// (git) in a goroutine. err is set only when both sources fail.
func loadPruneCandidates(repoDir, base string) tea.Cmd {
	return func() tea.Msg {
		merged := map[string]bool{}
		mprs, ghErr := gh.ListMergedPRs(repoDir)
		if ghErr == nil {
			for _, pr := range mprs {
				merged[pr.Head] = true
			}
		}
		locals, gitErr := git.MergedBranches(repoDir, base)
		if gitErr == nil {
			for _, b := range locals {
				merged[b] = true
			}
		}
		if ghErr != nil && gitErr != nil {
			return pruneCandidatesMsg{err: ghErr}
		}
		return pruneCandidatesMsg{merged: merged}
	}
}

// fetchPRs fetches PRs in any state to decorate worktree rows (open, merged,
// and closed all get a badge). Runs quietly.
func fetchPRs(repoDir string) tea.Cmd {
	return func() tea.Msg {
		prs, err := gh.ListPRs(repoDir, "all")
		return prMapMsg{prs: prs, err: err}
	}
}

// loadPRDetail fetches the full detail of a single PR for the PR tab.
func loadPRDetail(repoDir string, number int) tea.Cmd {
	return func() tea.Msg {
		d, err := gh.ViewPR(repoDir, number)
		return prDetailMsg{detail: d, err: err}
	}
}

// loadChecks fetches a PR's CI checks + rollup. Runs quietly (errors → no badge).
func loadChecks(repoDir, path string, number int) tea.Cmd {
	return func() tea.Msg {
		checks, err := gh.Checks(repoDir, number)
		if err != nil {
			return checksMsg{path: path, number: number}
		}
		return checksMsg{path: path, number: number, checks: checks, rollup: gh.Rollup(checks)}
	}
}

// loadDirty reports whether a worktree has uncommitted changes.
func loadDirty(path string) tea.Cmd {
	return func() tea.Msg {
		d, _ := git.Dirty(path)
		return dirtyMsg{path: path, dirty: d}
	}
}

// loadActivity fetches a worktree's HEAD commit time for sort-by-activity.
func loadActivity(path string) tea.Cmd {
	return func() tea.Msg {
		u, _ := git.LastCommitUnix(path)
		return activityMsg{path: path, unix: u}
	}
}

// loadDiff fetches the changed-file list of a worktree branch against its base.
func loadDiff(path, base string) tea.Cmd {
	return func() tea.Msg {
		files, err := git.DiffStat(path, base)
		return diffMsg{path: path, base: base, files: files, err: err}
	}
}

// loadFileDiff fetches the colored diff of a single file (on expand).
func loadFileDiff(path, base, file string) tea.Cmd {
	return func() tea.Msg {
		out, err := git.FileDiff(path, base, file)
		return fileDiffMsg{path: path, file: file, content: out, err: err}
	}
}

// gitFetch runs `git fetch --all --prune` for a worktree.
func gitFetch(path string) tea.Cmd {
	return func() tea.Msg { return opDoneMsg{label: "fetch", err: git.Fetch(path)} }
}

// prReview submits a PR review (approve / request-changes / comment).
func prReview(repoDir string, number int, kind, body string) tea.Cmd {
	return func() tea.Msg {
		return opDoneMsg{label: "review", err: gh.ReviewPR(repoDir, number, kind, body)}
	}
}

// prMerge merges a PR with a strategy (merge / squash / rebase).
func prMerge(repoDir string, number int, strat string) tea.Cmd {
	return func() tea.Msg {
		return opDoneMsg{label: "merge PR", err: gh.MergePRStrategy(repoDir, number, strat)}
	}
}

// prClose / prReopen / prReady wrap the corresponding gh state ops.
func prClose(repoDir string, number int) tea.Cmd {
	return func() tea.Msg { return opDoneMsg{label: "close PR", err: gh.ClosePR(repoDir, number)} }
}

func prReopen(repoDir string, number int) tea.Cmd {
	return func() tea.Msg { return opDoneMsg{label: "reopen PR", err: gh.ReopenPR(repoDir, number)} }
}

func prReady(repoDir string, number int) tea.Cmd {
	return func() tea.Msg { return opDoneMsg{label: "ready PR", err: gh.ReadyPR(repoDir, number)} }
}

// createPR pushes the branch (setting upstream if needed) then opens a PR.
func createPR(repoDir, path, title, body, base string, draft bool) tea.Cmd {
	return func() tea.Msg {
		if err := git.PushSetUpstream(path); err != nil {
			return prCreatedMsg{err: err}
		}
		n, err := gh.CreatePR(path, title, body, base, draft)
		return prCreatedMsg{number: n, err: err}
	}
}

// bulkPrune removes each merged worktree in sequence, aggregating the result.
// Targets whose delete hooks or removal fail (including dirty worktrees) are
// skipped, and the first error is reported alongside the success count.
func bulkPrune(repoDir string, targets []pruneTarget, deleteHooks []string) tea.Cmd {
	return func() tea.Msg {
		var firstErr error
		n := 0
		for _, t := range targets {
			vars := config.HookVars(repoDir, t.path, t.branch, t.upstream, t.prNumber)
			if err := coreexec.RunHooks(t.path, deleteHooks, vars); err != nil {
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			if err := git.RemoveWorktree(repoDir, t.path); err != nil {
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			if t.branch != "" {
				_ = git.DeleteBranch(repoDir, t.branch)
			}
			n++
		}
		return opDoneMsg{label: fmt.Sprintf("bulk prune (%d/%d)", n, len(targets)), err: firstErr}
	}
}

// pruneTarget is one worktree scheduled for bulk pruning.
type pruneTarget struct {
	path, branch, upstream string
	prNumber               int
}

// createWorktree builds a new worktree. mode selects the source:
//   - "new":      create branch `branch` and a worktree for it
//   - "existing": check out existing branch `branch` in a new worktree
//   - "pr":       fetch pull request `prNumber` into a worktree
//
// The path comes from cfg.WorktreePath (template + root); create hooks run in the
// new worktree on success and cfg.Upstream feeds the {base_branch} hook variable.
func createWorktree(repoDir, mode, branch string, prNumber int, cfg *config.Config) tea.Cmd {
	return func() tea.Msg {
		var (
			path string
			err  error
		)
		switch mode {
		case "new":
			path = cfg.WorktreePath(repoDir, branch)
			err = ensureParent(path)
			if err == nil {
				err = git.AddWorktreeNewBranch(repoDir, path, branch)
			}
		case "existing":
			path = cfg.WorktreePath(repoDir, branch)
			err = ensureParent(path)
			if err == nil {
				err = git.AddWorktreeExisting(repoDir, path, branch)
			}
		case "pr":
			path = cfg.WorktreePath(repoDir, fmt.Sprintf("pr-%d", prNumber))
			if err = ensureParent(path); err == nil {
				branch, err = git.CreateWorktreeFromPR(repoDir, path, prNumber)
			}
		default:
			return opDoneMsg{label: "create", err: fmt.Errorf("unknown create mode %q", mode)}
		}
		if err != nil {
			return opDoneMsg{label: "create", err: err}
		}
		vars := config.HookVars(repoDir, path, branch, cfg.Upstream, prNumber)
		if hookErr := coreexec.RunHooks(path, cfg.CreateHooks(), vars); hookErr != nil {
			return opDoneMsg{label: "create hook", err: hookErr}
		}
		return opDoneMsg{label: "create " + branch, err: nil}
	}
}

// ensureParent creates the parent directory of a worktree path so a custom
// worktree root/template can nest under not-yet-existing directories.
func ensureParent(path string) error {
	return os.MkdirAll(filepath.Dir(path), 0o755)
}

// commitPreviewMsg carries the git status shown in the commit modal.
type commitPreviewMsg struct {
	status string
	err    error
}

// loadCommitPreview fetches the short status for the commit modal preview.
func loadCommitPreview(path string) tea.Cmd {
	return func() tea.Msg {
		s, err := git.Status(path)
		return commitPreviewMsg{status: s, err: err}
	}
}

// tickProc schedules the next process-output refresh.
func tickProc() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(time.Time) tea.Msg { return procTickMsg{} })
}

// tickPRs schedules the next pull-request state re-check.
func tickPRs() tea.Cmd {
	return tea.Tick(30*time.Second, func(time.Time) tea.Msg { return prTickMsg{} })
}
