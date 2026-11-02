package playgroundbackend

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/launch"
	domain "github.com/Tiago-0liveira/bonsai/internal/git"
	"github.com/Tiago-0liveira/bonsai/internal/ui/playgroundapi"
)

func wantCode(t *testing.T, err error, code playgroundapi.Code) {
	t.Helper()
	var e *playgroundapi.Error
	if !errors.As(err, &e) || e.Code != code {
		t.Fatalf("err = %v, want code %s", err, code)
	}
}

func fileByPath(v playgroundapi.ChangesView, p string) (playgroundapi.FileChange, bool) {
	for _, f := range v.Files {
		if f.Path == p {
			return f, true
		}
	}
	return playgroundapi.FileChange{}, false
}

func TestScope(t *testing.T) {
	r := newRig(t, "main")
	wt := r.worktree("feature")
	r.add("in-tree", wt)
	r.add("in-main", r.main)
	outside, _ := filepath.EvalSymlinks(t.TempDir())
	r.sessions.list = append(r.sessions.list, session("outside", outside, "other-project"))
	// Same project ID but a directory that exists and is not a worktree.
	r.sessions.list = append(r.sessions.list, session("same-project-elsewhere", outside, r.b.project))
	// A branch name alone never matches.
	r.sessions.list = append(r.sessions.list, session("by-branch", filepath.Join(outside, "feature"), "other-project"))
	ctx := context.Background()

	for _, id := range []string{"in-tree", "in-main"} {
		if _, err := r.b.Changes(ctx, playgroundapi.ChangesRequest{SessionID: id}); err != nil {
			t.Errorf("%s: %v", id, err)
		}
	}
	for _, id := range []string{"outside", "same-project-elsewhere", "by-branch"} {
		_, err := r.b.Changes(ctx, playgroundapi.ChangesRequest{SessionID: id})
		wantCode(t, err, playgroundapi.CodeNotInRepository)
	}
	_, err := r.b.Changes(ctx, playgroundapi.ChangesRequest{SessionID: "ghost"})
	wantCode(t, err, playgroundapi.CodeSessionNotFound)
	_, err = r.b.Changes(ctx, playgroundapi.ChangesRequest{})
	wantCode(t, err, playgroundapi.CodeInvalidInput)

	r.sessions.err = errors.New("holder down")
	_, err = r.b.Changes(ctx, playgroundapi.ChangesRequest{SessionID: "in-tree"})
	wantCode(t, err, playgroundapi.CodeUnavailable)
}

func TestScopeByWorktreeID(t *testing.T) {
	r := newRig(t, "main")
	wt := r.worktree("feature")
	s := session("by-id", filepath.Join(t.TempDir(), "elsewhere"), "other-project")
	s.WorktreeID = worktreeID(wt)
	r.sessions.list = append(r.sessions.list, s)
	v, err := r.b.Changes(context.Background(), playgroundapi.ChangesRequest{SessionID: "by-id"})
	if err != nil || v.Branch != "feature" {
		t.Fatalf("view = %+v err=%v", v, err)
	}
}

func TestWorktreeMissingIsVerified(t *testing.T) {
	r := newRig(t, "main")
	wt := r.worktree("feature")
	r.add("gone-listed", wt)
	if err := os.RemoveAll(wt); err != nil { // git still lists it (prunable)
		t.Fatal(err)
	}
	v, err := r.b.Changes(context.Background(), playgroundapi.ChangesRequest{SessionID: "gone-listed"})
	if err != nil || v.State != playgroundapi.WorktreeMissing {
		t.Fatalf("listed but gone: %+v err=%v", v, err)
	}
	sh(t, r.main, "worktree", "prune")
	v, err = r.b.Changes(context.Background(), playgroundapi.ChangesRequest{SessionID: "gone-listed"})
	if err != nil || v.State != playgroundapi.WorktreeMissing {
		t.Fatalf("pruned: %+v err=%v", v, err)
	}
	// A failed worktree listing is not "missing": nothing can be said.
	r.b.deps.Trees = func(context.Context, string) ([]coregitWorktree, error) { return nil, errors.New("boom") }
	_, err = r.b.Changes(context.Background(), playgroundapi.ChangesRequest{SessionID: "gone-listed"})
	wantCode(t, err, playgroundapi.CodeUnavailable)
}

func TestBranchMissingDetachedAndUnavailable(t *testing.T) {
	r := newRig(t, "main")
	ctx := context.Background()

	wt := r.worktree("feature")
	r.add("s", wt)
	sh(t, r.main, "update-ref", "-d", "refs/heads/feature")
	v, err := r.b.Changes(ctx, playgroundapi.ChangesRequest{SessionID: "s"})
	if err != nil || v.State != playgroundapi.BranchMissing || v.Branch != "feature" {
		t.Fatalf("branch deleted: %+v err=%v", v, err)
	}

	det := r.worktree("other")
	sh(t, det, "checkout", "--detach")
	r.add("d", det)
	v, err = r.b.Changes(ctx, playgroundapi.ChangesRequest{SessionID: "d"})
	if err != nil || v.State != playgroundapi.WorktreeDetached || v.Branch != "" || v.Head.SHA == "" {
		t.Fatalf("detached: %+v err=%v", v, err)
	}
	if v.Base.Source != playgroundapi.BaseDefault || v.Base.Ref != "main" {
		t.Fatalf("detached still reads a base: %+v", v.Base)
	}

	// A read that fails is "unavailable", never "missing".
	real := r.b.deps.Git
	r.b.deps.Git = func(ctx context.Context, dir string, args ...string) (string, error) {
		if len(args) > 0 && args[0] == "show-ref" {
			return "", errors.New("index lock")
		}
		return real(ctx, dir, args...)
	}
	r.add("u", r.worktree("third"))
	v, err = r.b.Changes(ctx, playgroundapi.ChangesRequest{SessionID: "u"})
	if err != nil || v.State != playgroundapi.WorktreeUnavailable || v.Err == nil || v.Err.Code != playgroundapi.CodeUnavailable {
		t.Fatalf("unavailable: %+v err=%v", v, err)
	}
	if strings.Contains(v.Err.Message, "index lock") {
		t.Fatal("raw Git text leaked")
	}
}

func TestBaseSelectionOrder(t *testing.T) {
	ctx := context.Background()

	t.Run("default branch", func(t *testing.T) {
		r := newRig(t, "main")
		wt := r.worktree("feature")
		write(t, filepath.Join(wt, "a.txt"), "a\n")
		r.commit(wt, "add a")
		r.add("s", wt)
		v, err := r.b.Changes(ctx, playgroundapi.ChangesRequest{SessionID: "s"})
		if err != nil || v.Base.Source != playgroundapi.BaseDefault || v.Base.Ref != "main" || v.Base.MergeBase != sh(t, r.main, "rev-parse", "main") {
			t.Fatalf("base = %+v err=%v", v.Base, err)
		}
		if v.CommitsTotal != 1 || len(v.Commits) != 1 || v.Commits[0].Subject != "add a" {
			t.Fatalf("commits = %+v", v.Commits)
		}
	})

	t.Run("upstream beats default", func(t *testing.T) {
		r := newRig(t, "main")
		sh(t, r.main, "branch", "develop")
		wt := r.worktree("feature")
		sh(t, wt, "branch", "--set-upstream-to=develop")
		write(t, filepath.Join(wt, "a.txt"), "a\n")
		r.commit(wt, "add a")
		r.add("s", wt)
		v, err := r.b.Changes(ctx, playgroundapi.ChangesRequest{SessionID: "s"})
		if err != nil || v.Base.Source != playgroundapi.BaseUpstream || v.Base.Ref != "develop" {
			t.Fatalf("base = %+v err=%v", v.Base, err)
		}
	})

	t.Run("own push target is not a base", func(t *testing.T) {
		r := newRig(t, "main")
		remote := t.TempDir()
		sh(t, remote, "init", "--bare", "-b", "main")
		sh(t, r.main, "remote", "add", "origin", remote)
		wt := r.worktree("feature")
		write(t, filepath.Join(wt, "a.txt"), "a\n")
		r.commit(wt, "add a")
		sh(t, wt, "push", "-u", "origin", "feature")
		r.add("s", wt)
		v, err := r.b.Changes(ctx, playgroundapi.ChangesRequest{SessionID: "s"})
		if err != nil || v.Base.Source != playgroundapi.BaseDefault || v.CommitsTotal != 1 {
			t.Fatalf("base = %+v commits=%d err=%v", v.Base, v.CommitsTotal, err)
		}
	})

	t.Run("attempt base wins and stays stable", func(t *testing.T) {
		r := newRig(t, "main")
		baseSHA := sh(t, r.main, "rev-parse", "HEAD")
		wt := r.worktree("feature")
		write(t, filepath.Join(wt, "a.txt"), "a\n")
		r.commit(wt, "add a")
		write(t, filepath.Join(r.main, "later.txt"), "later\n") // main moves on
		r.commit(r.main, "main moves")
		a := launch.Attempt{ID: "att-1", Local: r.b.local, Issue: 7, Number: 42}
		a.Proposal.BaseRef, a.Proposal.BaseSHA = "origin/main", baseSHA
		r.attempts.byID["att-1"] = a
		s := r.add("s", wt)
		r.sessions.list[0].Task = &agents.TaskRef{IssueID: 7, AttemptID: "att-1", Role: "implementation"}
		_ = s
		v, err := r.b.Changes(ctx, playgroundapi.ChangesRequest{SessionID: "s"})
		if err != nil || v.Base.Source != playgroundapi.BaseAttempt || v.Base.Ref != "origin/main" || v.Base.MergeBase != baseSHA {
			t.Fatalf("base = %+v err=%v", v.Base, err)
		}
		if v.Task.Source != playgroundapi.TaskLaunch || v.Task.IssueNumber != 42 || v.Task.AttemptID != "att-1" {
			t.Fatalf("task = %+v", v.Task)
		}
		if f, ok := fileByPath(v, "a.txt"); !ok || f.Status != playgroundapi.FileAdded {
			t.Fatalf("files = %+v", v.Files)
		}
		if _, ok := fileByPath(v, "later.txt"); ok {
			t.Fatal("changes made on the base branch after the attempt leaked in")
		}
	})

	t.Run("foreign or unreadable attempt falls back with a note", func(t *testing.T) {
		r := newRig(t, "main")
		wt := r.worktree("feature")
		a := launch.Attempt{ID: "att-1", Local: "some-other-clone", Issue: 7}
		a.Proposal.BaseSHA = strings.Repeat("a", 40)
		r.attempts.byID["att-1"] = a
		r.add("s", wt)
		r.sessions.list[0].Task = &agents.TaskRef{IssueID: 7, AttemptID: "att-1"}
		v, err := r.b.Changes(ctx, playgroundapi.ChangesRequest{SessionID: "s"})
		if err != nil || v.Base.Source != playgroundapi.BaseDefault || v.Base.Note == "" {
			t.Fatalf("base = %+v err=%v", v.Base, err)
		}
	})

	t.Run("none is reported, never guessed", func(t *testing.T) {
		r := newRig(t, "trunk")
		wt := r.worktree("feature")
		write(t, filepath.Join(wt, "a.txt"), "a\n")
		r.commit(wt, "add a")
		write(t, filepath.Join(wt, "dirty.txt"), "x\n")
		r.add("s", wt)
		v, err := r.b.Changes(ctx, playgroundapi.ChangesRequest{SessionID: "s"})
		if err != nil || v.Base.Source != playgroundapi.BaseNone || v.Base.Ref != "" || v.Base.MergeBase != "" || v.Base.Note == "" {
			t.Fatalf("base = %+v err=%v", v.Base, err)
		}
		if len(v.Commits) != 0 || v.CommitsErr != nil {
			t.Fatalf("no base means no commit list: %+v", v)
		}
		// Only uncommitted work is shown against HEAD.
		if _, ok := fileByPath(v, "dirty.txt"); !ok {
			t.Fatalf("uncommitted work missing: %+v", v.Files)
		}
		if _, ok := fileByPath(v, "a.txt"); ok {
			t.Fatal("committed file shown without a base")
		}
	})
}

func TestFilesCommittedUncommittedUntrackedRenameBinary(t *testing.T) {
	r := newRig(t, "main")
	wt := r.worktree("feature")
	write(t, filepath.Join(wt, "committed.txt"), "a\nb\n")
	r.commit(wt, "committed")
	if err := os.Rename(filepath.Join(wt, "base.txt"), filepath.Join(wt, "renamed.txt")); err != nil {
		t.Fatal(err)
	}
	r.commit(wt, "rename")
	write(t, filepath.Join(wt, "committed.txt"), "a\nb\nc\n") // uncommitted edit of a committed file
	write(t, filepath.Join(wt, "new.txt"), "n1\nn2\n")        // untracked
	write(t, filepath.Join(wt, "pic.bin"), "\x00\x01\x02")
	sh(t, wt, "add", "pic.bin") // staged binary
	write(t, filepath.Join(wt, ".bonsai", "x"), "ignored by the view")
	r.add("s", wt)

	v, err := r.b.Changes(context.Background(), playgroundapi.ChangesRequest{SessionID: "s"})
	if err != nil {
		t.Fatal(err)
	}
	c, _ := fileByPath(v, "committed.txt")
	if c.Status != playgroundapi.FileAdded || c.Additions != 3 || !c.Uncommitted || !c.Counted {
		t.Errorf("committed.txt = %+v", c)
	}
	rn, _ := fileByPath(v, "renamed.txt")
	if rn.Status != playgroundapi.FileRenamed || rn.OldPath != "base.txt" || rn.Uncommitted {
		t.Errorf("renamed.txt = %+v", rn)
	}
	n, _ := fileByPath(v, "new.txt")
	if !n.Untracked || !n.Uncommitted || n.Counted || n.Status != playgroundapi.FileAdded {
		t.Errorf("new.txt = %+v", n)
	}
	p, _ := fileByPath(v, "pic.bin")
	if !p.Binary || p.Counted || !p.Uncommitted {
		t.Errorf("pic.bin = %+v", p)
	}
	for _, f := range v.Files {
		if strings.Contains(f.Path, ".bonsai") {
			t.Errorf("hidden path listed: %s", f.Path)
		}
	}
	if v.Totals.Files != 4 || v.Totals.Additions != 3 || v.Totals.Binary != 2 || v.Totals.Uncommitted != 3 {
		t.Errorf("totals = %+v", v.Totals)
	}
	if v.Head.SHA == "" || v.Head.Subject != "rename" {
		t.Errorf("head = %+v", v.Head)
	}
}

func TestFileListTruncationKeepsTotals(t *testing.T) {
	r := newRig(t, "main")
	wt := r.worktree("feature")
	for i := 0; i < playgroundapi.MaxFiles+5; i++ {
		write(t, filepath.Join(wt, "many", fmt.Sprintf("f%04d.txt", i)), "x\n")
	}
	r.add("s", wt)
	v, err := r.b.Changes(context.Background(), playgroundapi.ChangesRequest{SessionID: "s"})
	if err != nil {
		t.Fatal(err)
	}
	if !v.FilesTrunc || len(v.Files) != playgroundapi.MaxFiles || v.Totals.Files != playgroundapi.MaxFiles+5 {
		t.Fatalf("trunc=%v shown=%d totals=%+v", v.FilesTrunc, len(v.Files), v.Totals)
	}
	// A file the list cut off is still a changed file of this view.
	cut := fmt.Sprintf("many/f%04d.txt", playgroundapi.MaxFiles+4)
	d, err := r.b.FileDiff(context.Background(), playgroundapi.FileDiffRequest{SessionID: "s", Path: cut, Base: v.Base.Ref, Generation: v.Stamp.Generation})
	if err != nil || d.Path != cut {
		t.Fatalf("diff of cut-off file: %+v err=%v", d, err)
	}
}

func TestIndependentFailures(t *testing.T) {
	r := newRig(t, "main")
	wt := r.worktree("feature")
	write(t, filepath.Join(wt, "a.txt"), "a\n")
	r.commit(wt, "a")
	r.add("s", wt)
	ctx := context.Background()

	// The link store is corrupt: Changes still works and says so.
	if err := os.WriteFile(r.links.Path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	// GitHub is down: Changes still works.
	r.gh.prsErr, r.gh.checksErr = errors.New("connection refused"), errors.New("connection refused")
	v, err := r.b.Changes(ctx, playgroundapi.ChangesRequest{SessionID: "s"})
	if err != nil || v.State != playgroundapi.WorktreeOK || v.LinkErr == nil || v.LinkErr.Code != playgroundapi.CodeStore || len(v.Files) != 1 {
		t.Fatalf("changes = %+v err=%v", v, err)
	}
	pr, err := r.b.PullRequest(ctx, playgroundapi.PRRequest{SessionID: "s"})
	if err != nil || pr.State != playgroundapi.PRUnavailable {
		t.Fatalf("pr = %+v err=%v", pr, err)
	}
	ck, err := r.b.Checks(ctx, playgroundapi.ChecksRequest{SessionID: "s"})
	if err != nil || ck.State != playgroundapi.ChecksUnavailable {
		t.Fatalf("checks = %+v err=%v", ck, err)
	}

	// Git is down: the pull request and checks still answer.
	r.gh.prsErr, r.gh.checksErr = nil, nil
	real := r.b.deps.Git
	r.b.deps.Git = func(context.Context, string, ...string) (string, error) { return "", errors.New("git broke") }
	v, err = r.b.Changes(ctx, playgroundapi.ChangesRequest{SessionID: "s"})
	if err != nil || v.State != playgroundapi.WorktreeUnavailable {
		t.Fatalf("git down: %+v err=%v", v, err)
	}
	if pr, err = r.b.PullRequest(ctx, playgroundapi.PRRequest{SessionID: "s"}); err != nil || pr.State != playgroundapi.PRNone {
		t.Fatalf("pr with git down = %+v err=%v", pr, err)
	}

	// Only the file list fails: base and commits survive.
	r.b.deps.Git = func(ctx context.Context, dir string, args ...string) (string, error) {
		if len(args) > 0 && args[0] == "status" {
			return "", errors.New("status broke")
		}
		return real(ctx, dir, args...)
	}
	v, err = r.b.Changes(ctx, playgroundapi.ChangesRequest{SessionID: "s"})
	if err != nil || v.FilesErr == nil || v.CommitsErr != nil || len(v.Commits) != 1 || v.Base.Source != playgroundapi.BaseDefault || len(v.Files) != 0 {
		t.Fatalf("files down: %+v err=%v", v, err)
	}
}

func TestGenerationsAreMonotonicPerKind(t *testing.T) {
	r := newRig(t, "main")
	r.add("s", r.main)
	ctx := context.Background()
	a, _ := r.b.Changes(ctx, playgroundapi.ChangesRequest{SessionID: "s"})
	b, _ := r.b.Changes(ctx, playgroundapi.ChangesRequest{SessionID: "s"})
	p, _ := r.b.PullRequest(ctx, playgroundapi.PRRequest{SessionID: "s"})
	if b.Stamp.Generation != a.Stamp.Generation+1 || p.Stamp.Generation != 1 {
		t.Fatalf("generations %d %d pr=%d", a.Stamp.Generation, b.Stamp.Generation, p.Stamp.Generation)
	}
	if a.Stamp.Scope != r.b.local.String() || !a.Stamp.FetchedAt.Equal(r.tick) {
		t.Fatalf("stamp = %+v", a.Stamp)
	}
	var g playgroundapi.Gate
	if !g.Accept(b.Stamp) || g.Accept(a.Stamp) {
		t.Fatal("the gate must drop the older changes reply")
	}
}

func TestCanceledContextPassesThrough(t *testing.T) {
	r := newRig(t, "main")
	r.add("s", r.main)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r.sessions.err = context.Canceled
	if _, err := r.b.Changes(ctx, playgroundapi.ChangesRequest{SessionID: "s"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func TestGitErrorKinds(t *testing.T) {
	if !isExit(&exitStatus{code: 1}, 1) || isExit(errors.New("x"), 1) || !isExitAny(fmt.Errorf("w: %w", &exitStatus{code: 128})) {
		t.Fatal("exit status helpers")
	}
	if domain.Code(errors.New("x")) == "" {
		t.Fatal("unreachable")
	}
}

func TestNewNeedsSessions(t *testing.T) {
	gitEnv(t)
	if _, err := New(t.TempDir()); !errors.Is(err, ErrNoSessions) {
		t.Fatalf("New without agents = %v, want ErrNoSessions", err)
	}
}
