// Package git wraps the Git CLI. It returns plain Go types and errors and is
// independent of any TUI concern.
package git

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// gitTimeout bounds every non-interactive git subprocess so a hung fetch or
// lock can't block a tea.Cmd goroutine forever. A var (not const) as a seam
// for future injection.
var gitTimeout = 120 * time.Second

// Worktree describes a single entry from `git worktree list`.
type Worktree struct {
	Path   string
	Branch string
	HEAD   string
	IsMain bool
	Bare   bool
}

// Metrics holds ahead/behind counts relative to an upstream.
type Metrics struct {
	Ahead  int
	Behind int
}

// run executes a git command in dir and returns trimmed stdout. Bounded by
// gitTimeout. Interactive commands (RebaseCmd/MergeCmd) are built separately
// and deliberately untimed.
func run(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

// RepoRoot returns the top-level directory of the git repo containing dir. Note
// that from inside a linked worktree this is that worktree, not the main repo —
// use MainRoot when you need the primary worktree.
func RepoRoot(dir string) (string, error) {
	return run(dir, "rev-parse", "--show-toplevel")
}

// MainRoot returns the main worktree's path regardless of which worktree dir is
// inside. `git worktree list` always reports the main worktree first.
func MainRoot(dir string) (string, error) {
	trees, err := ListWorktrees(dir)
	if err != nil {
		return "", err
	}
	if len(trees) == 0 {
		return "", fmt.Errorf("no worktrees found for %s", dir)
	}
	return trees[0].Path, nil
}

// ListWorktrees parses `git worktree list --porcelain` into structured data.
// The first entry is the main worktree.
func ListWorktrees(dir string) ([]Worktree, error) {
	out, err := run(dir, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}

	var (
		trees []Worktree
		cur   Worktree
		open  bool
	)
	flush := func() {
		if open {
			trees = append(trees, cur)
			cur = Worktree{}
			open = false
		}
	}

	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "worktree "):
			flush()
			cur.Path = strings.TrimPrefix(line, "worktree ")
			open = true
		case strings.HasPrefix(line, "HEAD "):
			cur.HEAD = strings.TrimPrefix(line, "HEAD ")
		case strings.HasPrefix(line, "branch "):
			ref := strings.TrimPrefix(line, "branch ")
			cur.Branch = strings.TrimPrefix(ref, "refs/heads/")
		case line == "bare":
			cur.Bare = true
		case line == "detached":
			cur.Branch = "(detached)"
		}
	}
	flush()
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if len(trees) > 0 {
		trees[0].IsMain = true
	}
	return trees, nil
}

// AheadBehind returns commit counts of branch relative to upstream using
// `git rev-list --left-right --count upstream...branch`. Behind is the count of
// commits on upstream not in branch; Ahead is the reverse.
func AheadBehind(dir, branch, upstream string) (Metrics, error) {
	spec := fmt.Sprintf("%s...%s", upstream, branch)
	out, err := run(dir, "rev-list", "--left-right", "--count", spec)
	if err != nil {
		return Metrics{}, err
	}
	fields := strings.Fields(out)
	if len(fields) != 2 {
		return Metrics{}, fmt.Errorf("unexpected rev-list output: %q", out)
	}
	behind, err := strconv.Atoi(fields[0])
	if err != nil {
		return Metrics{}, err
	}
	ahead, err := strconv.Atoi(fields[1])
	if err != nil {
		return Metrics{}, err
	}
	return Metrics{Ahead: ahead, Behind: behind}, nil
}

// ListBranches returns local branch names.
func ListBranches(dir string) ([]string, error) {
	out, err := run(dir, "for-each-ref", "--format=%(refname:short)", "refs/heads")
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	return strings.Split(out, "\n"), nil
}

// ErrWorktreeDirty is returned by RemoveWorktree when git refuses to remove a
// worktree that still has modified or untracked files.
var ErrWorktreeDirty = errors.New("worktree contains uncommitted changes")

// RemoveWorktree removes the worktree at path. It fails with ErrWorktreeDirty
// if the worktree has uncommitted changes, so stale dirty-state can never
// silently destroy work — use ForceRemoveWorktree for the explicit opt-in.
func RemoveWorktree(dir, path string) error {
	_, err := run(dir, "worktree", "remove", path)
	if err != nil && isDirtyRemoveErr(err) {
		return fmt.Errorf("%w: %s", ErrWorktreeDirty, path)
	}
	return err
}

// ForceRemoveWorktree removes the worktree at path even if it is dirty.
// Only use when the user explicitly opted into discarding uncommitted changes.
func ForceRemoveWorktree(dir, path string) error {
	_, err := run(dir, "worktree", "remove", "--force", path)
	return err
}

// isDirtyRemoveErr reports whether a worktree-remove failure is git refusing
// because of modified or untracked files.
func isDirtyRemoveErr(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "use --force") ||
		strings.Contains(msg, "contains modified or untracked files")
}

// DeleteBranch force-deletes a local branch.
func DeleteBranch(dir, branch string) error {
	_, err := run(dir, "branch", "-D", branch)
	return err
}

// WorktreePath returns the conventional filesystem path for a branch's worktree:
// a sibling of the main repo named "<repo>-<branch>" (slashes flattened).
func WorktreePath(repoDir, branch string) string {
	base := filepath.Base(repoDir)
	safe := strings.ReplaceAll(branch, "/", "-")
	return filepath.Join(filepath.Dir(repoDir), base+"-"+safe)
}

// AddWorktreeNewBranch creates a worktree at path checking out a newly created
// branch.
func AddWorktreeNewBranch(dir, path, branch string) error {
	_, err := run(dir, "worktree", "add", "-b", branch, path)
	return err
}

// AddWorktreeExisting creates a worktree at path checking out an existing branch.
func AddWorktreeExisting(dir, path, branch string) error {
	_, err := run(dir, "worktree", "add", path, branch)
	return err
}

// CreateWorktreeFromPR fetches a GitHub pull request's head into a local branch
// "pr-<number>" and checks it out in a new worktree at path. Returns the local
// branch name. Works for forks via the pull ref.
func CreateWorktreeFromPR(dir, path string, number int) (string, error) {
	branch := fmt.Sprintf("pr-%d", number)
	ref := fmt.Sprintf("pull/%d/head:%s", number, branch)
	if _, err := run(dir, "fetch", "origin", ref); err != nil {
		return "", err
	}
	if err := AddWorktreeExisting(dir, path, branch); err != nil {
		return "", err
	}
	return branch, nil
}

// Push pushes the current branch.
func Push(dir string) error {
	_, err := run(dir, "push")
	return err
}

// Pull pulls the current branch.
func Pull(dir string) error {
	_, err := run(dir, "pull")
	return err
}

// Commit stages all changes and commits with msg.
func Commit(dir, msg string) error {
	if _, err := run(dir, "add", "-A"); err != nil {
		return err
	}
	_, err := run(dir, "commit", "-m", msg)
	return err
}

// Log returns a compact, colorized commit graph for the right pane. Color is
// forced on (output isn't a TTY) so the viewport can render it.
func Log(dir string) (string, error) {
	return run(dir, "log", "--oneline", "--graph", "--decorate", "--color=always", "-n", "50")
}

// Status returns `git status --short` with color forced on, for the commit
// modal preview.
func Status(dir string) (string, error) {
	return run(dir, "-c", "color.status=always", "status", "--short")
}

// RebaseCmd builds an *exec.Cmd for `git rebase target` in dir, suitable for
// handing to tea.ExecProcess so the user can resolve conflicts interactively.
func RebaseCmd(dir, target string) *exec.Cmd {
	cmd := exec.Command("git", "rebase", target)
	cmd.Dir = dir
	return cmd
}

// MergeCmd builds an *exec.Cmd for `git merge target` in dir, for interactive
// conflict resolution via tea.ExecProcess.
func MergeCmd(dir, target string) *exec.Cmd {
	cmd := exec.Command("git", "merge", target)
	cmd.Dir = dir
	return cmd
}

// Dirty reports whether the worktree has uncommitted changes (staged, unstaged,
// or untracked) via `git status --porcelain`.
func Dirty(dir string) (bool, error) {
	out, err := run(dir, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

// LastCommitUnix returns the HEAD commit's author time as a unix timestamp.
func LastCommitUnix(dir string) (int64, error) {
	out, err := run(dir, "log", "-1", "--format=%ct")
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(strings.TrimSpace(out), 10, 64)
}

// DiffBase returns a colored stat summary plus the full diff of the worktree's
// HEAD against base, using the merge-base (base...HEAD) form so it shows only
// what the branch adds.
func DiffBase(dir, base string) (string, error) {
	spec := base + "...HEAD"
	stat, err := run(dir, "-c", "color.ui=always", "diff", "--stat", spec)
	if err != nil {
		return "", err
	}
	full, err := run(dir, "-c", "color.ui=always", "diff", spec)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(stat) == "" {
		return "(no changes vs " + base + ")", nil
	}
	return stat + "\n\n" + full, nil
}

// DiffFile is one changed file in a diff, with its added/deleted line counts.
// Binary files report Add == Del == -1.
type DiffFile struct {
	Path string
	Add  int
	Del  int
}

// DiffStat lists the files changed between base and HEAD (merge-base form) with
// per-file line counts, parsed from `git diff --numstat`.
func DiffStat(dir, base string) ([]DiffFile, error) {
	out, err := run(dir, "diff", "--numstat", base+"...HEAD")
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(out) == "" {
		return nil, nil
	}
	var files []DiffFile
	for _, line := range strings.Split(out, "\n") {
		fields := strings.SplitN(strings.TrimSpace(line), "\t", 3)
		if len(fields) != 3 {
			continue
		}
		f := DiffFile{Path: fields[2]}
		if fields[0] == "-" { // binary
			f.Add, f.Del = -1, -1
		} else {
			f.Add, _ = strconv.Atoi(fields[0])
			f.Del, _ = strconv.Atoi(fields[1])
		}
		files = append(files, f)
	}
	return files, nil
}

// FileDiff returns the colored diff of a single file between base and HEAD.
func FileDiff(dir, base, file string) (string, error) {
	out, err := run(dir, "-c", "color.ui=always", "diff", base+"...HEAD", "--", file)
	if err != nil {
		return "", err
	}
	return out, nil
}

// Fetch updates all remotes and prunes deleted remote branches.
func Fetch(dir string) error {
	_, err := run(dir, "fetch", "--all", "--prune")
	return err
}

// PushSetUpstream pushes the current branch, setting the upstream on first push
// (`git push -u origin HEAD`), so a freshly-created branch can open a PR.
func PushSetUpstream(dir string) error {
	_, err := run(dir, "push", "-u", "origin", "HEAD")
	return err
}

// MergedBranches returns local branches already merged into base (excluding the
// base branch itself and the current HEAD marker).
func MergedBranches(dir, base string) ([]string, error) {
	out, err := run(dir, "branch", "--merged", base, "--format=%(refname:short)")
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(out) == "" {
		return nil, nil
	}
	var branches []string
	for _, b := range strings.Split(out, "\n") {
		b = strings.TrimSpace(b)
		if b == "" || b == base {
			continue
		}
		branches = append(branches, b)
	}
	return branches, nil
}
