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

// prsMsg carries the loaded pull-request list for the create-from-PR modal.
type prsMsg struct {
	prs []gh.PR
	err error
}

// prMapMsg carries open PRs used to decorate worktree rows with badges. Errors
// are handled silently (gh missing/unauthenticated simply means no badges).
type prMapMsg struct {
	prs []gh.PR
	err error
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
// merge happens first and aborts the prune on failure.
func pruneWorktree(repoDir, path, branch, upstream string, deleteHooks []string, merge bool, prNumber int) tea.Cmd {
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
		if err := git.RemoveWorktree(repoDir, path); err != nil {
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
		prs, err := gh.ListPRs(repoDir)
		return prsMsg{prs: prs, err: err}
	}
}

// fetchPRs fetches open PRs to decorate worktree rows. Runs quietly.
func fetchPRs(repoDir string) tea.Cmd {
	return func() tea.Msg {
		prs, err := gh.ListPRs(repoDir)
		return prMapMsg{prs: prs, err: err}
	}
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
