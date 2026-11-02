package git

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// Checkout is what a reader of the working tree would see: the commit it is on and whether anything
// uncommitted sits on top. Paths and contents are not kept, only a count and a digest of the status entries.
type Checkout struct {
	Commit string
	// Branch is empty on a detached HEAD.
	Branch string
	// DirtyFiles counts changed, staged and untracked entries; zero means a clean tree.
	DirtyFiles int
	// DirtyDigest is the SHA-256 of the status entries (empty when clean). It changes when the set of
	// changed paths or their states does, which is what a caller compares between a preview and a start.
	DirtyDigest string
}

// Dirty reports uncommitted changes.
func (c Checkout) Dirty() bool { return c.DirtyFiles > 0 }

// CheckoutInspector reads a Checkout. Run is the seam for tests; nil uses the shared bounded Git runner.
type CheckoutInspector struct {
	Dir string
	Run func(ctx context.Context, dir string, args ...string) (string, error)
}

// Inspect reads HEAD, the branch and the working tree status. It writes nothing.
func (i CheckoutInspector) Inspect(ctx context.Context) (Checkout, error) {
	run := i.Run
	if run == nil {
		run = RunContext
	}
	head, err := run(ctx, i.Dir, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return Checkout{}, fmt.Errorf("read HEAD: %w", err)
	}
	c := Checkout{Commit: strings.TrimSpace(head)}
	// Detached HEAD makes symbolic-ref fail; that is an answer, not an error.
	if branch, err := run(ctx, i.Dir, "symbolic-ref", "--short", "-q", "HEAD"); err == nil {
		c.Branch = strings.TrimSpace(branch)
	}
	status, err := run(ctx, i.Dir, "status", "--porcelain=v1", "-z", "--untracked-files=normal")
	if err != nil {
		return Checkout{}, fmt.Errorf("read working tree status: %w", err)
	}
	c.DirtyFiles = countStatusEntries(status)
	if c.DirtyFiles > 0 {
		sum := sha256.Sum256([]byte(status))
		c.DirtyDigest = hex.EncodeToString(sum[:])
	}
	return c, nil
}

// countStatusEntries counts the entries of `git status --porcelain=v1 -z`. A rename or copy carries its
// source path as one more NUL-separated field, which is not another change.
func countStatusEntries(out string) int {
	fields := strings.Split(out, "\x00")
	n := 0
	for i := 0; i < len(fields); i++ {
		f := fields[i]
		if len(f) < 4 {
			continue
		}
		n++
		if f[0] == 'R' || f[0] == 'C' || f[1] == 'R' || f[1] == 'C' {
			i++
		}
	}
	return n
}
