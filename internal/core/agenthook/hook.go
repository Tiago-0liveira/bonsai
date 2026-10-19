// Package agenthook is the hook side of the activity transport: the code behind
// `bonsai __agent-hook <provider> <event>`, which a provider CLI runs for each
// lifecycle hook. It translates the payload on stdin into signals and appends
// them to the session's spool, where the holder tails them. It is observe-only
// and fail-open: whatever goes wrong, it prints the provider's expected output
// and reports success, so a hook can never change or block the agent.
package agenthook

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agentactivity"
	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

const (
	// MaxPayloadBytes caps the stdin payload read; a bigger one yields no signals.
	MaxPayloadBytes = 1 << 20
	// MaxSpoolBytes is the size past which a spool is no longer appended to.
	MaxSpoolBytes = 4 << 20
	// RootFlag is the optional `--sessions-root=<abs dir>` argument after the
	// event. Providers whose CLI runs with a different HOME (Antigravity) embed
	// the holder's sessions root in the hook command line, because the hook
	// would otherwise derive another default root from that HOME and reject the
	// spool. It lives in a config file Bonsai wrote, not in the environment the
	// agent could redirect.
	RootFlag = "--sessions-root="
)

// Options are the seams of Run.
type Options struct {
	// Args are `<provider> <event>`.
	Args   []string
	Stdin  io.Reader
	Stdout io.Writer
	Getenv func(string) string
	// SessionsRoot is the directory that holds every session's runtime directory.
	// A RootFlag argument overrides it.
	SessionsRoot string
	// Lookup finds a provider's activity support by ID.
	Lookup func(provider string) (agents.ActivityReporter, bool)
	// Now defaults to time.Now.
	Now func() time.Time
}

// Run executes one hook and returns the process exit code, which is always 0.
func Run(o Options) int {
	start := time.Now
	if o.Now != nil {
		start = o.Now
	}
	began := start()
	// A bug here must never surface in the agent as a failing hook.
	defer func() { _ = recover() }()
	if len(o.Args) < 2 || o.Lookup == nil {
		return 0
	}
	reporter, ok := o.Lookup(o.Args[0])
	if !ok {
		return 0
	}
	event := o.Args[1]
	defer func() {
		if o.Stdout != nil {
			_, _ = o.Stdout.Write(reporter.HookOutput(event))
		}
	}()
	getenv := o.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	root := o.SessionsRoot
	if flagged := rootFromArgs(o.Args[2:]); flagged != "" {
		root = flagged
	}
	spool, ok := ResolveSpool(getenv(agents.ActivityEnv), root)
	if !ok || o.Stdin == nil {
		return 0
	}
	payload, err := io.ReadAll(io.LimitReader(o.Stdin, MaxPayloadBytes+1))
	if err != nil || len(payload) > MaxPayloadBytes {
		return 0
	}
	signals := reporter.TranslateHook(event, payload)
	for i := range signals {
		// The start time, not the append time: async hooks finish out of order.
		signals[i].At = began
		signals[i].Source = agentactivity.SourceHook
	}
	_ = agentactivity.AppendSignals(spool, signals)
	return 0
}

// rootFromArgs returns the RootFlag value in args, or "" when there is none or
// it is not an absolute path.
func rootFromArgs(args []string) string {
	for _, arg := range args {
		if root, ok := strings.CutPrefix(arg, RootFlag); ok && filepath.IsAbs(root) {
			return root
		}
	}
	return ""
}

// ResolveSpool returns the resolved path of the spool named by path when it is
// the activity file of a session under root: <root>/<id>/activity.jsonl, a
// regular file no larger than MaxSpoolBytes. The environment is not trusted:
// symlinks are resolved before the check, so a link cannot point the append
// elsewhere.
func ResolveSpool(path, root string) (string, bool) {
	if path == "" || root == "" || !filepath.IsAbs(path) {
		return "", false
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", false
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", false
	}
	if filepath.Base(resolved) != agents.ActivitySpoolName || filepath.Dir(filepath.Dir(resolved)) != filepath.Clean(resolvedRoot) {
		return "", false
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.Mode().IsRegular() || info.Size() > MaxSpoolBytes {
		return "", false
	}
	return resolved, true
}
