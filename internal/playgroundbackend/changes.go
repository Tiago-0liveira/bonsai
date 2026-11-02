package playgroundbackend

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden/launch"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/playgroundapi"
)

const (
	changesTimeout = 25 * time.Second
	diffTimeout    = 15 * time.Second
	maxSnapshots   = 32
	// maxRawPatch bounds how much of a patch is parsed; the rest is dropped and
	// the view says it was truncated.
	maxRawPatch = 4 * playgroundapi.MaxDiffBytes
)

// snapshot is what FileDiff validates a request against: the list one Changes
// reply showed, and the commit and directory it was read at.
type snapshot struct {
	gen                          uint64
	baseRef, diffBase, head, dir string
	files                        map[string]rawFile
}

func (b *Backend) remember(session string, s snapshot) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if cur, ok := b.snaps[session]; ok && cur.gen > s.gen {
		return
	}
	if _, ok := b.snaps[session]; !ok && len(b.snaps) >= maxSnapshots {
		oldest, first := "", true
		for id, v := range b.snaps {
			if first || v.gen < b.snaps[oldest].gen {
				oldest, first = id, false
			}
		}
		delete(b.snaps, oldest)
	}
	b.snaps[session] = s
}

func (b *Backend) snapshot(session string) (snapshot, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	s, ok := b.snaps[session]
	return s, ok
}

// Changes implements playgroundapi.Backend.
func (b *Backend) Changes(ctx context.Context, req playgroundapi.ChangesRequest) (playgroundapi.ChangesView, error) {
	ctx, cancel := context.WithTimeout(ctx, changesTimeout)
	defer cancel()
	gen := b.genChanges.Add(1)
	sc, err := b.scope(ctx, req.SessionID)
	if err != nil {
		return playgroundapi.ChangesView{}, err
	}
	v := playgroundapi.ChangesView{Stamp: b.stamp(gen), SessionID: req.SessionID, State: playgroundapi.WorktreeOK, Base: playgroundapi.Base{Source: playgroundapi.BaseNone}}
	v.Task, v.LinkErr = b.task(ctx, sc.session)
	if sc.missing {
		v.State = playgroundapi.WorktreeMissing
		return v, nil
	}
	unavailable := func(msg string) (playgroundapi.ChangesView, error) {
		if e := ctxErr(ctx); e != nil && !errors.Is(e, context.DeadlineExceeded) {
			return playgroundapi.ChangesView{}, e
		}
		v.State = playgroundapi.WorktreeUnavailable
		v.Err = fail(playgroundapi.CodeUnavailable, msg)
		return v, nil
	}
	dir, branch := sc.tree.Path, sc.tree.Branch
	detached := branch == "" || branch == "(detached)"
	if detached {
		v.State = playgroundapi.WorktreeDetached
		branch = ""
	} else {
		v.Branch = clean(branch, 200)
		ok, rerr := b.refExists(ctx, dir, "refs/heads/"+branch)
		if rerr != nil {
			return unavailable("The branch could not be read.")
		}
		if !ok {
			v.State = playgroundapi.BranchMissing
			return v, nil
		}
	}
	head, err := b.headCommit(ctx, dir)
	if err != nil {
		return unavailable("The worktree's HEAD could not be read.")
	}
	v.Head = head

	base, berr := b.chooseBase(ctx, sc, dir, branch)
	if berr != nil {
		if e := ctxErr(ctx); e != nil && !errors.Is(e, context.DeadlineExceeded) {
			return playgroundapi.ChangesView{}, e
		}
		v.Base = playgroundapi.Base{Source: playgroundapi.BaseNone, Note: "The base could not be read."}
		v.FilesErr = fail(playgroundapi.CodeUnavailable, "The changed files could not be read.")
		v.CommitsErr = fail(playgroundapi.CodeUnavailable, "The commits could not be read.")
		return v, nil
	}
	v.Base = base
	diffBase := base.MergeBase
	if diffBase == "" {
		diffBase = head.SHA
	}

	files, totals, ferr := b.readFiles(ctx, dir, diffBase)
	if ferr != nil {
		if e := ctxErr(ctx); e != nil && !errors.Is(e, context.DeadlineExceeded) {
			return playgroundapi.ChangesView{}, e
		}
		v.FilesErr = fail(playgroundapi.CodeUnavailable, "The changed files could not be read.")
	} else {
		v.Totals = totals
		snap := snapshot{gen: gen, baseRef: base.Ref, diffBase: diffBase, head: head.SHA, dir: dir, files: make(map[string]rawFile, len(files))}
		for _, f := range files {
			snap.files[f.change.Path] = f
		}
		if len(files) > playgroundapi.MaxFiles {
			files, v.FilesTrunc = files[:playgroundapi.MaxFiles], true
		}
		for _, f := range files {
			v.Files = append(v.Files, f.change)
		}
		// The whole list is remembered, not just the shown part, so a file the
		// list cut off is still a changed file of this view.
		b.remember(req.SessionID, snap)
	}

	if base.MergeBase != "" {
		commits, total, cerr := b.readCommits(ctx, dir, base.MergeBase)
		if cerr != nil {
			if e := ctxErr(ctx); e != nil && !errors.Is(e, context.DeadlineExceeded) {
				return playgroundapi.ChangesView{}, e
			}
			v.CommitsErr = fail(playgroundapi.CodeUnavailable, "The commits could not be read.")
		} else {
			v.Commits, v.CommitsTotal = commits, total
			v.CommitsTrunc = total > len(commits)
		}
	}
	return v, nil
}

// chooseBase picks, in order: the Garden attempt's recorded base, the branch's
// tracked upstream, the repository default branch, none. Each step is a
// verified read; a step that fails to read fails the choice instead of falling
// through, because a guess would label unrelated commits as this branch's work.
func (b *Backend) chooseBase(ctx context.Context, sc scoped, dir, branch string) (playgroundapi.Base, error) {
	note := ""
	if sc.session.Task != nil && sc.session.Task.AttemptID != "" {
		if a, ok := b.attempt(ctx, sc.session); ok {
			if base, ok, err := b.attemptBase(ctx, dir, a); err != nil {
				return playgroundapi.Base{}, err
			} else if ok {
				return base, nil
			}
			note = "The task attempt's base is not in this repository; using the branch's own base."
		} else {
			note = "The task attempt could not be read; using the branch's own base."
		}
	}
	if ref, ok, err := b.upstreamBase(ctx, dir, branch); err != nil {
		return playgroundapi.Base{}, err
	} else if ok {
		if mb, ok, err := b.mergeBase(ctx, dir, ref); err != nil {
			return playgroundapi.Base{}, err
		} else if ok {
			return playgroundapi.Base{Source: playgroundapi.BaseUpstream, Ref: clean(ref, 200), MergeBase: mb, Note: note}, nil
		}
	}
	if ref, ok, err := b.defaultBase(ctx, dir); err != nil {
		return playgroundapi.Base{}, err
	} else if ok {
		if mb, ok, err := b.mergeBase(ctx, dir, ref); err != nil {
			return playgroundapi.Base{}, err
		} else if ok {
			return playgroundapi.Base{Source: playgroundapi.BaseDefault, Ref: clean(ref, 200), MergeBase: mb, Note: note}, nil
		}
	}
	if note == "" {
		note = "No base could be established: no upstream and no default branch."
	}
	return playgroundapi.Base{Source: playgroundapi.BaseNone, Note: note}, nil
}

// attemptBase measures from the base commit the attempt recorded, so the view
// stays stable when the base branch later moves.
func (b *Backend) attemptBase(ctx context.Context, dir string, a launch.Attempt) (playgroundapi.Base, bool, error) {
	p := a.Proposal
	if !shaRE.MatchString(p.BaseSHA) {
		return playgroundapi.Base{}, false, nil
	}
	mb, ok, err := b.mergeBase(ctx, dir, p.BaseSHA)
	if err != nil || !ok {
		return playgroundapi.Base{}, false, err
	}
	return playgroundapi.Base{Source: playgroundapi.BaseAttempt, Ref: clean(p.BaseRef, 200), MergeBase: mb}, true, nil
}

// task reports how the session relates to a Garden task. A read failure of the
// link store is reported beside the result and never fails the caller.
func (b *Backend) task(ctx context.Context, s agentsapi.Session) (playgroundapi.TaskLink, *playgroundapi.Error) {
	if s.Task != nil {
		t := playgroundapi.TaskLink{Source: playgroundapi.TaskLaunch, IssueID: s.Task.IssueID, AttemptID: s.Task.AttemptID}
		if a, ok := b.attempt(ctx, s); ok {
			t.IssueNumber = a.Number
		}
		return t, nil
	}
	none := playgroundapi.TaskLink{Source: playgroundapi.TaskNone}
	if b.deps.Links == nil {
		return none, nil
	}
	l, found, err := b.deps.Links.Get(ctx, b.local, s.ID)
	if err != nil {
		return none, fail(playgroundapi.CodeStore, "Task links could not be read.")
	}
	if !found {
		return none, nil
	}
	return playgroundapi.TaskLink{Source: playgroundapi.TaskManual, IssueNumber: l.IssueNumber, IssueID: uint64(l.IssueID), LinkedAt: l.LinkedAt}, nil
}

// FileDiff implements playgroundapi.Backend.
func (b *Backend) FileDiff(ctx context.Context, req playgroundapi.FileDiffRequest) (playgroundapi.FileDiffView, error) {
	ctx, cancel := context.WithTimeout(ctx, diffTimeout)
	defer cancel()
	gen := b.genDiff.Add(1)
	if !validPath(req.Path) || len(req.Base) > 512 {
		return playgroundapi.FileDiffView{}, fail(playgroundapi.CodeInvalidInput, "That path cannot be shown.")
	}
	stale := fail(playgroundapi.CodeStale, "The changes moved on. Reload them and pick the file again.")
	snap, ok := b.snapshot(req.SessionID)
	if !ok || snap.gen != req.Generation || snap.baseRef != req.Base {
		return playgroundapi.FileDiffView{}, stale
	}
	f, ok := snap.files[req.Path]
	if !ok {
		return playgroundapi.FileDiffView{}, fail(playgroundapi.CodeFileNotChanged, "That file is not among the changed files.")
	}
	sc, err := b.scope(ctx, req.SessionID)
	if err != nil {
		return playgroundapi.FileDiffView{}, err
	}
	if sc.missing || sc.tree.Path != snap.dir {
		return playgroundapi.FileDiffView{}, stale
	}
	head, err := b.headCommit(ctx, snap.dir)
	if err != nil {
		if e := ctxErr(ctx); e != nil {
			return playgroundapi.FileDiffView{}, e
		}
		return playgroundapi.FileDiffView{}, fail(playgroundapi.CodeUnavailable, "The worktree's HEAD could not be read.")
	}
	if head.SHA != snap.head {
		return playgroundapi.FileDiffView{}, stale
	}
	v := playgroundapi.FileDiffView{Stamp: b.stamp(gen), SessionID: req.SessionID, Path: f.change.Path, OldPath: f.change.OldPath, Head: head}
	if f.change.Binary {
		v.Binary = true
		return v, nil
	}

	var patch string
	cut := false
	if f.untracked {
		patch, cut, v.Binary, err = readUntracked(snap.dir, f.raw)
	} else {
		args := []string{"diff", "-U3", "-M", "--no-ext-diff", "--no-textconv", "--no-color", snap.diffBase, "--", ":(literal)" + f.raw}
		if f.oldRaw != "" {
			args = append(args, ":(literal)"+f.oldRaw)
		}
		patch, err = b.git(ctx, snap.dir, args...)
		if len(patch) > maxRawPatch {
			cut = true
			patch = patch[:maxRawPatch]
			if i := strings.LastIndex(patch, "\n"); i >= 0 {
				patch = patch[:i+1]
			}
		}
	}
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return playgroundapi.FileDiffView{}, stale
		}
		if e := ctxErr(ctx); e != nil {
			return playgroundapi.FileDiffView{}, e
		}
		return playgroundapi.FileDiffView{}, fail(playgroundapi.CodeUnavailable, "The diff could not be read.")
	}
	if v.Binary {
		return v, nil
	}
	hunks, binary, reason, lines := parsePatch(patch)
	if cut && reason == playgroundapi.TruncNone {
		reason = playgroundapi.TruncBytes
	}
	v.Hunks, v.Binary, v.Lines = hunks, binary, lines
	v.Truncated, v.TruncReason = reason != playgroundapi.TruncNone, reason
	v.Empty = !binary && len(hunks) == 0 && !v.Truncated
	return v, nil
}

// readUntracked renders a new, unadded file as an all-added patch. Only a plain
// file inside the worktree is read: a symlink is never followed.
func readUntracked(dir, rel string) (patch string, cut, binary bool, err error) {
	path := filepath.Join(dir, filepath.FromSlash(rel))
	info, err := os.Lstat(path)
	if err != nil {
		return "", false, false, err
	}
	if !info.Mode().IsRegular() {
		return "", false, true, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return "", false, false, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, playgroundapi.MaxDiffBytes+1))
	if err != nil {
		return "", false, false, fmt.Errorf("read untracked file: %w", err)
	}
	if strings.IndexByte(string(data), 0) >= 0 {
		return "", false, true, nil
	}
	text := string(data)
	if len(data) > playgroundapi.MaxDiffBytes {
		cut = true
		text = text[:strings.LastIndex(text, "\n")+1]
	}
	rows := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	if text == "" {
		return "", cut, false, nil
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "@@ -0,0 +1,%d @@\n", len(rows))
	for _, r := range rows {
		sb.WriteString("+" + r + "\n")
	}
	return sb.String(), cut, false, nil
}

// validPath accepts only a relative slash path without parent segments.
func validPath(p string) bool {
	if p == "" || len(p) > playgroundapi.MaxPathBytes || strings.ContainsRune(p, 0) {
		return false
	}
	if filepath.IsAbs(p) || strings.HasPrefix(p, "/") || strings.HasPrefix(p, `\`) || filepath.VolumeName(p) != "" {
		return false
	}
	for _, seg := range strings.FieldsFunc(p, func(r rune) bool { return r == '/' || r == '\\' }) {
		if seg == ".." {
			return false
		}
	}
	return true
}
