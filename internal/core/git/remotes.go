package git

import (
	"context"
	"fmt"
	"strings"
)

// Remote is one configured fetch remote. The URL is returned as configured and
// may carry credentials: callers sanitize it before it leaves their package.
type Remote struct {
	Name string
	URL  string
}

// RemoteLister lists a repository's remotes. Run is the seam for tests; nil
// uses the shared bounded Git runner.
type RemoteLister struct {
	Dir string
	Run func(ctx context.Context, dir string, args ...string) (string, error)
}

// ListRemotes returns every remote once, in Git's order, using its fetch URL.
// Git applies url.<base>.insteadOf rewrites, so the URL is what a fetch would use.
func (l RemoteLister) ListRemotes(ctx context.Context) ([]Remote, error) {
	run := l.Run
	if run == nil {
		run = RunContext
	}
	out, err := run(ctx, l.Dir, "remote", "-v")
	if err != nil {
		return nil, fmt.Errorf("list git remotes: %w", err)
	}
	return parseRemotes(out), nil
}

// parseRemotes reads `git remote -v` lines of the form "name<TAB>url (fetch)".
// Push lines and anything else are skipped, so a push-only URL never becomes a target.
func parseRemotes(out string) []Remote {
	var remotes []Remote
	seen := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		name, rest, ok := strings.Cut(strings.TrimRight(line, "\r"), "\t")
		url, found := strings.CutSuffix(rest, " (fetch)")
		if !ok || !found || name == "" || url == "" || seen[name] {
			continue
		}
		seen[name] = true
		remotes = append(remotes, Remote{Name: name, URL: url})
	}
	return remotes
}
