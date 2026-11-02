package gardenbackend

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	coregit "github.com/Tiago-0liveira/bonsai/internal/core/git"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
)

// maxHandoffFiles bounds the file list of one handoff view; the totals still cover every file.
const maxHandoffFiles = 50

// WorkIdentity is what a retained worktree holds right now.
type WorkIdentity struct {
	Head, Tree string
	// Dirty means uncommitted or untracked changes exist.
	Dirty     bool
	Files     []gardenapi.FileChange
	Totals    gardenapi.ChangeTotals
	Truncated bool
}

// HandoffWork reads the identity of a retained worktree. It must not write, fetch or run hooks.
type HandoffWork interface {
	Inspect(ctx context.Context, dir, base string) (WorkIdentity, error)
}

var shaPattern = regexp.MustCompile(`^[0-9a-f]{40}([0-9a-f]{24})?$`)

// gitWork is the production HandoffWork. Inherited repository locators and config injection are scrubbed so the
// directory given is the one read. Every call is a read: fsmonitor and external diff or textconv drivers
// are switched off because they are the config-controlled ways a read could run a command.
type gitWork struct{}

func gitRead(ctx context.Context, dir string, args ...string) (string, error) {
	pre := []string{"--no-optional-locks", "-c", "core.fsmonitor=false"}
	return coregit.RunIsolatedContext(ctx, dir, append(pre, args...)...)
}

func (gitWork) Inspect(ctx context.Context, dir, base string) (WorkIdentity, error) {
	var w WorkIdentity
	if !shaPattern.MatchString(base) {
		return w, fmt.Errorf("work identity: base is not a commit ID")
	}
	for _, f := range []struct {
		dst  *string
		spec string
	}{{&w.Head, "HEAD"}, {&w.Tree, "HEAD^{tree}"}} {
		out, err := gitRead(ctx, dir, "rev-parse", "--verify", f.spec)
		if err != nil {
			return WorkIdentity{}, fmt.Errorf("work identity: %w", err)
		}
		*f.dst = strings.TrimSpace(out)
		if !shaPattern.MatchString(*f.dst) {
			return WorkIdentity{}, fmt.Errorf("work identity: unexpected rev-parse output")
		}
	}
	status, err := gitRead(ctx, dir, "status", "--porcelain", "--untracked-files=all", "--ignore-submodules=none")
	if err != nil {
		return WorkIdentity{}, fmt.Errorf("work identity: %w", err)
	}
	w.Dirty = strings.TrimSpace(status) != ""
	stat, err := gitRead(ctx, dir, "diff", "--numstat", "--no-renames", "--no-ext-diff", "--no-textconv", "-z", base+"...HEAD")
	if err != nil {
		return WorkIdentity{}, fmt.Errorf("work identity: %w", err)
	}
	w.Files, w.Totals, w.Truncated = parseNumstat(stat)
	return w, nil
}

// parseNumstat reads `git diff --numstat -z --no-renames`: records of "add\tdel\tpath" separated by NUL.
func parseNumstat(out string) ([]gardenapi.FileChange, gardenapi.ChangeTotals, bool) {
	var files []gardenapi.FileChange
	var totals gardenapi.ChangeTotals
	truncated := false
	for _, rec := range strings.Split(out, "\x00") {
		fields := strings.SplitN(strings.TrimLeft(rec, "\n"), "\t", 3)
		if len(fields) != 3 {
			continue
		}
		f := gardenapi.FileChange{Path: displayPath(fields[2])}
		if fields[0] == "-" || fields[1] == "-" {
			f.Binary = true
		} else {
			f.Add, _ = strconv.Atoi(fields[0])
			f.Del, _ = strconv.Atoi(fields[1])
		}
		totals.Files++
		totals.Add += f.Add
		totals.Del += f.Del
		if len(files) < maxHandoffFiles {
			files = append(files, f)
		} else {
			truncated = true
		}
	}
	return files, totals, truncated
}

// displayPath removes control and bidirectional characters, so a file name cannot steer the terminal.
func displayPath(p string) string {
	p = strings.ToValidUTF8(p, "�")
	var b strings.Builder
	for _, r := range p {
		switch {
		case unicode.IsControl(r), unicode.Is(unicode.Cf, r):
			b.WriteRune('?')
		default:
			b.WriteRune(r)
		}
	}
	out := b.String()
	if utf8.RuneCountInString(out) > 300 {
		out = string([]rune(out)[:299]) + "…"
	}
	return out
}
