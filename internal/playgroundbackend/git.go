package playgroundbackend

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"

	coregit "github.com/Tiago-0liveira/bonsai/internal/core/git"
	"github.com/Tiago-0liveira/bonsai/internal/ui/playgroundapi"
)

// exitStatus is a Git command that ran and exited non-zero. Several reads use
// it as an answer ("no upstream", "ref does not exist"), so it is told apart
// from a command that could not run or was canceled.
type exitStatus struct{ code int }

func (e *exitStatus) Error() string { return "git exited with status " + strconv.Itoa(e.code) }

// runGit is the production runner: the shared bounded Git runner with
// --no-optional-locks first, so a status read never rewrites the index.
func runGit(ctx context.Context, dir string, args ...string) (string, error) {
	out, err := coregit.RunContext(ctx, dir, append([]string{"--no-optional-locks"}, args...)...)
	var ee *exec.ExitError
	if err != nil && errors.As(err, &ee) {
		return "", &exitStatus{code: ee.ExitCode()}
	}
	return out, err
}

func isExit(err error, code int) bool {
	var e *exitStatus
	return errors.As(err, &e) && e.code == code
}

func isExitAny(err error) bool {
	var e *exitStatus
	return errors.As(err, &e)
}

var (
	shaRE  = regexp.MustCompile(`^[0-9a-f]{40}([0-9a-f]{24})?$`)
	anyRE  = regexp.MustCompile(`^[0-9a-fA-F]{7,64}$`)
	hunkRE = regexp.MustCompile(`^@@ -(\d+)(?:,\d+)? \+(\d+)`)
)

// safeRef refuses a ref that Git could read as an option.
func safeRef(ref string) bool {
	return ref != "" && !strings.HasPrefix(ref, "-") && !strings.ContainsAny(ref, "\x00\n\r ")
}

// clean makes provider-controlled text safe to print in a terminal: escape
// sequences, control characters (tabs become spaces), bidirectional overrides
// and invalid UTF-8 are removed. It truncates to limit runes (0 means none).
func clean(s string, limit int) string {
	s = strings.ToValidUTF8(ansi.Strip(s), "\uFFFD")
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\t':
			b.WriteString("    ")
		case unicode.IsControl(r), r >= 0x202A && r <= 0x202E, r >= 0x2066 && r <= 0x2069, r == 0x200E, r == 0x200F, r == 0x061C:
		default:
			b.WriteRune(r)
		}
	}
	out := b.String()
	if limit > 0 && utf8.RuneCountInString(out) > limit {
		out = string([]rune(out)[:limit-1]) + "…"
	}
	return out
}

func (b *Backend) git(ctx context.Context, dir string, args ...string) (string, error) {
	return b.deps.Git(ctx, dir, args...)
}

func (b *Backend) trim(ctx context.Context, dir string, args ...string) (string, error) {
	out, err := b.git(ctx, dir, args...)
	return strings.TrimSpace(out), err
}

// refExists reports whether a full ref name exists. Exit status 1 is the
// verified answer "no"; any other failure is an error, never "missing".
func (b *Backend) refExists(ctx context.Context, dir, ref string) (bool, error) {
	_, err := b.git(ctx, dir, "show-ref", "--verify", "--quiet", ref)
	switch {
	case err == nil:
		return true, nil
	case isExit(err, 1):
		return false, nil
	}
	return false, err
}

const commitFormat = "--format=%H%x1f%an%x1f%aI%x1f%s"

func parseCommit(rec string) (playgroundapi.Commit, bool) {
	f := strings.SplitN(strings.Trim(rec, "\n\x1e"), "\x1f", 4)
	if len(f) != 4 || !shaRE.MatchString(f[0]) {
		return playgroundapi.Commit{}, false
	}
	c := playgroundapi.Commit{SHA: f[0], Short: f[0][:7], Author: clean(f[1], 80), Subject: clean(f[3], 200)}
	if t, err := time.Parse(time.RFC3339, f[2]); err == nil {
		c.When = t.UTC()
	}
	return c, true
}

func (b *Backend) headCommit(ctx context.Context, dir string) (playgroundapi.Commit, error) {
	out, err := b.git(ctx, dir, "log", "-1", commitFormat, "HEAD")
	if err != nil {
		return playgroundapi.Commit{}, err
	}
	c, ok := parseCommit(out)
	if !ok {
		return playgroundapi.Commit{}, errors.New("unreadable HEAD commit")
	}
	return c, nil
}

// mergeBase returns the merge base of commit-ish a and HEAD. ok is false when
// the two share no history (a verified answer); an unknown object also exits
// non-zero and is treated the same, since the base is then not usable.
func (b *Backend) mergeBase(ctx context.Context, dir, a string) (sha string, ok bool, err error) {
	if !safeRef(a) {
		return "", false, nil
	}
	out, err := b.trim(ctx, dir, "merge-base", a, "HEAD")
	switch {
	case err == nil && shaRE.MatchString(out):
		return out, true, nil
	case err == nil:
		return "", false, errors.New("unreadable merge base")
	case isExitAny(err):
		return "", false, nil
	}
	return "", false, err
}

// upstreamBase returns the branch's tracked upstream when it is a real base: an
// upstream with the branch's own name is its push target and says what is
// unpushed, not what the branch is based on.
func (b *Backend) upstreamBase(ctx context.Context, dir, branch string) (string, bool, error) {
	if !safeRef(branch) {
		return "", false, nil
	}
	out, err := b.trim(ctx, dir, "rev-parse", "--abbrev-ref", "--symbolic-full-name", branch+"@{upstream}")
	switch {
	case err == nil:
	case isExitAny(err):
		return "", false, nil
	default:
		return "", false, err
	}
	if !safeRef(out) {
		return "", false, nil
	}
	if _, rest, found := strings.Cut(out, "/"); found && rest == branch {
		return "", false, nil
	}
	return out, true, nil
}

// defaultBase finds the repository default branch: origin's HEAD when set,
// else a local main or master. It never guesses further.
func (b *Backend) defaultBase(ctx context.Context, dir string) (string, bool, error) {
	out, err := b.trim(ctx, dir, "symbolic-ref", "--quiet", "--short", "refs/remotes/origin/HEAD")
	switch {
	case err == nil && safeRef(out):
		if ok, rerr := b.refExists(ctx, dir, "refs/remotes/"+out); rerr != nil {
			return "", false, rerr
		} else if ok {
			return out, true, nil
		}
	case err != nil && !isExitAny(err):
		return "", false, err
	}
	for _, name := range []string{"main", "master"} {
		ok, err := b.refExists(ctx, dir, "refs/heads/"+name)
		if err != nil {
			return "", false, err
		}
		if ok {
			return name, true, nil
		}
	}
	return "", false, nil
}

// ---- file list ----

type numstat struct {
	path, old string
	add, del  int
	binary    bool
}

// parseNumstat reads `diff --numstat -z -M`: "A\tD\tpath\0" or, for a rename,
// "A\tD\t\0old\0new\0".
func parseNumstat(out string) ([]numstat, error) {
	toks := strings.Split(out, "\x00")
	var res []numstat
	for i := 0; i < len(toks); i++ {
		t := strings.TrimLeft(toks[i], "\n")
		if t == "" {
			continue
		}
		f := strings.SplitN(t, "\t", 3)
		if len(f) != 3 {
			return nil, errors.New("unreadable numstat")
		}
		n := numstat{path: f[2]}
		if f[0] == "-" || f[1] == "-" {
			n.binary = true
		} else {
			var err1, err2 error
			n.add, err1 = strconv.Atoi(f[0])
			n.del, err2 = strconv.Atoi(f[1])
			if err1 != nil || err2 != nil {
				return nil, errors.New("unreadable numstat counts")
			}
		}
		if f[2] == "" {
			if i+2 >= len(toks) {
				return nil, errors.New("truncated numstat rename")
			}
			n.old, n.path = toks[i+1], toks[i+2]
			i += 2
			if n.old == "" || n.path == "" {
				return nil, errors.New("truncated numstat rename")
			}
		}
		res = append(res, n)
	}
	return res, nil
}

// parseNameStatus reads `diff --name-status -z -M` into new path -> status
// letter ("R" for renames).
func parseNameStatus(out string) (map[string]byte, error) {
	toks := strings.Split(out, "\x00")
	res := map[string]byte{}
	for i := 0; i < len(toks); i++ {
		t := strings.TrimLeft(toks[i], "\n")
		if t == "" {
			continue
		}
		switch t[0] {
		case 'R', 'C':
			if i+2 >= len(toks) {
				return nil, errors.New("truncated name-status rename")
			}
			res[toks[i+2]] = t[0]
			i += 2
		case 'A', 'M', 'D', 'T', 'U':
			if i+1 >= len(toks) {
				return nil, errors.New("truncated name-status")
			}
			res[toks[i+1]] = t[0]
			i++
		default:
			return nil, errors.New("unreadable name-status")
		}
	}
	return res, nil
}

type porcelain struct {
	untracked map[string]bool
	touched   map[string]bool // every path the index or working tree changes
}

// parsePorcelain reads `status --porcelain=v1 -z --no-renames`: "XY path\0".
func parsePorcelain(out string) (porcelain, error) {
	p := porcelain{untracked: map[string]bool{}, touched: map[string]bool{}}
	for _, t := range strings.Split(out, "\x00") {
		t = strings.TrimLeft(t, "\n")
		if t == "" {
			continue
		}
		if len(t) < 4 || t[2] != ' ' {
			return porcelain{}, errors.New("unreadable status")
		}
		path := t[3:]
		p.touched[path] = true
		if t[:2] == "??" {
			p.untracked[path] = true
		}
	}
	return p, nil
}

func hiddenPath(p string) bool {
	return p == ".bonsai" || strings.HasPrefix(p, ".bonsai/") || strings.Contains(p, "/.bonsai/") ||
		p == ".git" || strings.HasPrefix(p, ".git/")
}

// rawFile remembers the unmodified Git path behind a display-safe one.
type rawFile struct {
	raw, oldRaw string
	untracked   bool
	change      playgroundapi.FileChange
}

// readFiles lists every changed file against diffBase (a commit; HEAD when the
// base is none): committed and uncommitted changes together, with uncommitted
// files marked. Totals count every file even though the caller caps the list.
func (b *Backend) readFiles(ctx context.Context, dir, diffBase string) ([]rawFile, playgroundapi.Totals, error) {
	ns, err := b.git(ctx, dir, "diff", "--numstat", "-z", "-M", "--no-ext-diff", "--no-textconv", "--no-color", diffBase, "--")
	if err != nil {
		return nil, playgroundapi.Totals{}, err
	}
	nm, err := b.git(ctx, dir, "diff", "--name-status", "-z", "-M", "--no-ext-diff", "--no-textconv", "--no-color", diffBase, "--")
	if err != nil {
		return nil, playgroundapi.Totals{}, err
	}
	st, err := b.git(ctx, dir, "status", "--porcelain=v1", "-z", "--no-renames", "--untracked-files=all")
	if err != nil {
		return nil, playgroundapi.Totals{}, err
	}
	stats, err := parseNumstat(ns)
	if err != nil {
		return nil, playgroundapi.Totals{}, err
	}
	kinds, err := parseNameStatus(nm)
	if err != nil {
		return nil, playgroundapi.Totals{}, err
	}
	pf, err := parsePorcelain(st)
	if err != nil {
		return nil, playgroundapi.Totals{}, err
	}
	var files []rawFile
	seen := map[string]bool{}
	var tot playgroundapi.Totals
	add := func(rf rawFile) {
		rf.change.Path = clean(rf.raw, playgroundapi.MaxPathBytes)
		if rf.oldRaw != "" {
			rf.change.OldPath = clean(rf.oldRaw, playgroundapi.MaxPathBytes)
		}
		if rf.change.Path == "" || seen[rf.change.Path] {
			return
		}
		seen[rf.change.Path] = true
		tot.Files++
		if rf.change.Counted {
			tot.Additions += rf.change.Additions
			tot.Deletions += rf.change.Deletions
		}
		if rf.change.Binary || !rf.change.Counted {
			tot.Binary++
		}
		if rf.change.Uncommitted {
			tot.Uncommitted++
		}
		files = append(files, rf)
	}
	for _, n := range stats {
		if hiddenPath(n.path) {
			continue
		}
		c := playgroundapi.FileChange{Binary: n.binary, Counted: !n.binary, Additions: n.add, Deletions: n.del,
			Uncommitted: pf.touched[n.path] || (n.old != "" && pf.touched[n.old])}
		switch kinds[n.path] {
		case 'A':
			c.Status = playgroundapi.FileAdded
		case 'D':
			c.Status = playgroundapi.FileDeleted
		case 'R', 'C':
			c.Status = playgroundapi.FileRenamed
		default:
			c.Status = playgroundapi.FileModified
		}
		add(rawFile{raw: n.path, oldRaw: n.old, change: c})
	}
	for p := range pf.untracked {
		if hiddenPath(p) {
			continue
		}
		add(rawFile{raw: p, untracked: true, change: playgroundapi.FileChange{Status: playgroundapi.FileAdded, Uncommitted: true, Untracked: true}})
	}
	sortFiles(files)
	return files, tot, nil
}

func sortFiles(f []rawFile) {
	sort.Slice(f, func(i, j int) bool { return f[i].change.Path < f[j].change.Path })
}

// readCommits lists the commits between base and HEAD, newest first, bounded.
func (b *Backend) readCommits(ctx context.Context, dir, base string) (commits []playgroundapi.Commit, total int, err error) {
	out, err := b.trim(ctx, dir, "rev-list", "--count", base+"..HEAD")
	if err != nil {
		return nil, 0, err
	}
	total, err = strconv.Atoi(out)
	if err != nil {
		return nil, 0, fmt.Errorf("unreadable commit count: %w", err)
	}
	log, err := b.git(ctx, dir, "log", "-n", strconv.Itoa(playgroundapi.MaxCommits), "--format=%H%x1f%an%x1f%aI%x1f%s%x1e", base+"..HEAD")
	if err != nil {
		return nil, 0, err
	}
	for _, rec := range strings.Split(log, "\x1e") {
		if strings.TrimSpace(rec) == "" {
			continue
		}
		c, ok := parseCommit(rec)
		if !ok {
			return nil, 0, errors.New("unreadable commit")
		}
		commits = append(commits, c)
	}
	return commits, total, nil
}

// ---- patch ----

// parsePatch splits one file's unified diff into hunks, keeping at most the
// byte and line caps. Everything before the first hunk header is dropped, so
// metadata (and any warning Git printed) never reaches the screen.
func parsePatch(patch string) (hunks []playgroundapi.Hunk, binary bool, reason playgroundapi.TruncReason, lines int) {
	var cur *playgroundapi.Hunk
	oldN, newN, size := 0, 0, 0
	rows := strings.Split(patch, "\n")
	if n := len(rows); n > 0 && rows[n-1] == "" {
		rows = rows[:n-1]
	}
	flush := func() {
		if cur != nil {
			hunks = append(hunks, *cur)
			cur = nil
		}
	}
	for _, row := range rows {
		if cur == nil && (strings.HasPrefix(row, "Binary files ") || row == "GIT binary patch") {
			return nil, true, playgroundapi.TruncNone, 0
		}
		if m := hunkRE.FindStringSubmatch(row); m != nil {
			flush()
			oldN, _ = strconv.Atoi(m[1])
			newN, _ = strconv.Atoi(m[2])
			header := clean(row, 200)
			size += len(header)
			cur = &playgroundapi.Hunk{Header: header}
			continue
		}
		if cur == nil {
			continue
		}
		if lines >= playgroundapi.MaxDiffLines {
			reason = playgroundapi.TruncLines
			break
		}
		l := playgroundapi.DiffLine{Kind: playgroundapi.DiffCtx}
		text := row
		switch {
		case strings.HasPrefix(row, "+"):
			l.Kind, text = playgroundapi.DiffAdd, row[1:]
			l.New, newN = newN, newN+1
		case strings.HasPrefix(row, "-"):
			l.Kind, text = playgroundapi.DiffDel, row[1:]
			l.Old, oldN = oldN, oldN+1
		case strings.HasPrefix(row, "\\"):
			// "\ No newline at end of file": context without line numbers.
		case strings.HasPrefix(row, " "):
			text = row[1:]
			l.Old, l.New = oldN, newN
			oldN, newN = oldN+1, newN+1
		case row == "":
			l.Old, l.New = oldN, newN
			oldN, newN = oldN+1, newN+1
		default:
			continue
		}
		l.Text = clean(text, 0)
		if size+len(l.Text) > playgroundapi.MaxDiffBytes {
			reason = playgroundapi.TruncBytes
			break
		}
		size += len(l.Text)
		cur.Lines = append(cur.Lines, l)
		lines++
	}
	flush()
	return hunks, false, reason, lines
}
