// Package playgroundbackend implements the TUI's playgroundapi contract: it
// resolves an agent session to a verified worktree of this repository and reads
// that worktree's changes, pull request and CI. Every read is bounded, takes a
// context, and never writes the repository or fetches, pulls, pushes or checks
// out. Writes are local session links, review records, and reviewer sessions
// authorized by explicit local confirmation or the persisted local policy.
//
// Git is reached through the shared bounded runner with fixed argument lists
// (never a user path or a shell); GitHub through the shared service. Failures
// are reduced to fixed playgroundapi codes: no Git or provider text, URL or
// token leaves this package.
package playgroundbackend

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/launch"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/review"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/sessionlink"
	coregit "github.com/Tiago-0liveira/bonsai/internal/core/git"
	"github.com/Tiago-0liveira/bonsai/internal/git/github"
	"github.com/Tiago-0liveira/bonsai/internal/git/github/ghcli"
	localgit "github.com/Tiago-0liveira/bonsai/internal/git/local"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/playgroundapi"
)

// ErrNoSessions means there is no agent runtime, so there are no sessions to read.
var ErrNoSessions = errors.New("playground needs the agent runtime")

// repositoryID is the repository ID worktree IDs are derived with, as in the
// web API and the daemon.
const repositoryID = "local"

// Sessions lists the holder records.
type Sessions interface {
	List(ctx context.Context) ([]agentsapi.Session, error)
}

// Attempts reads Garden launch attempts.
type Attempts interface {
	Get(ctx context.Context, id string) (launch.Attempt, error)
}

// GitHub is the read-only part of the GitHub service the Playground uses.
type GitHub interface {
	PullRequests(ctx context.Context, repo string, f github.PRFilter) ([]github.PullRequest, error)
	PullRequest(ctx context.Context, repo string, number int) (github.PullRequestDetail, error)
	Checks(ctx context.Context, repo, sha string) ([]github.Check, error)
}

// Garden reads the verified work collection.
type Garden interface {
	Work(ctx context.Context) (gardenapi.WorkView, error)
}

// Links is the manual link store.
type Links interface {
	List(ctx context.Context, local garden.LocalRepoID) ([]sessionlink.Link, error)
	Get(ctx context.Context, local garden.LocalRepoID, session string) (sessionlink.Link, bool, error)
	Put(ctx context.Context, l sessionlink.Link, replace bool) (sessionlink.Link, bool, error)
}

// Deps are the seams of the backend. New fills them from the real packages;
// tests fill them with fakes. Sessions is required, every other part optional:
// a nil part leaves only the call that needs it unavailable.
type Deps struct {
	Sessions Sessions
	Attempts Attempts
	GitHub   GitHub
	Garden   Garden
	Links    Links
	// Review and the fields after it serve the optional review calls
	// (playgroundapi.ReviewBackend). A nil part leaves only the calls that need
	// it unavailable.
	Review         ReviewStore
	LaunchAttempts AttemptLister
	Host           ReviewHost
	Accounts       ReviewAccounts
	// Prefs reads the local review policy from the user's state file, every time
	// it is called: a changed or disabled consent must take effect at once, and
	// nothing read from GitHub may ever supply it.
	Prefs func() (*config.GardenReviewPrefs, error)
	// ReportDir is where reviewer reports are written, outside every worktree.
	ReportDir review.ReportDir
	// Git runs one read-only Git command in dir. A command that exits non-zero
	// returns an *exitStatus; anything else is a failure to run.
	Git func(ctx context.Context, dir string, args ...string) (string, error)
	// Trees lists the worktrees of the repository.
	Trees func(ctx context.Context, dir string) ([]coregit.Worktree, error)
	// Remotes lists the fetch remotes.
	Remotes func(ctx context.Context) ([]coregit.Remote, error)
	Now     func() time.Time
}

// Backend is the playgroundapi.Backend for one local clone.
type Backend struct {
	deps    Deps
	dir     string
	local   garden.LocalRepoID
	project string

	genChanges, genDiff, genPR, genChecks, genLinks atomic.Uint64
	genPolicy, genReviews, genPreview, genAction    atomic.Uint64

	mu    sync.Mutex
	snaps map[string]snapshot
}

var (
	_ playgroundapi.Backend       = (*Backend)(nil)
	_ playgroundapi.ReviewBackend = (*Backend)(nil)
)

// Option customizes New.
type Option func(*options)

type options struct {
	agents *agentsapi.Backend
	shared *ghcli.Shared
	garden Garden
	links  Links
}

// WithAgents supplies the session source. Without it New fails with ErrNoSessions.
func WithAgents(api agentsapi.Backend) Option { return func(o *options) { o.agents = &api } }

// WithShared supplies the GitHub client state, so Garden and the Playground
// share one token cache and rate-limit view.
func WithShared(s *ghcli.Shared) Option { return func(o *options) { o.shared = s } }

// WithGarden supplies the verified work read manual links are checked against.
func WithGarden(g Garden) Option { return func(o *options) { o.garden = g } }

// WithLinks overrides the link store (tests).
func WithLinks(l Links) Option { return func(o *options) { o.links = l } }

// New builds the backend for the clone at repoDir. A part that cannot be built
// (launch attempts, the link store) is left out, never the whole backend.
func New(repoDir string, opts ...Option) (*Backend, error) {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	if o.agents == nil || o.agents.Sessions == nil {
		return nil, ErrNoSessions
	}
	d := Deps{
		Sessions: o.agents.Sessions,
		Garden:   o.garden,
		Git:      runGit,
		Trees:    coregit.ListWorktreesContext,
		Remotes:  coregit.RemoteLister{Dir: repoDir}.ListRemotes,
	}
	if store, err := launch.Open(); err == nil {
		d.Attempts = store
		d.LaunchAttempts = store
	}
	// Review is optional: when its store, the report directory or the session
	// host cannot be built, only the review calls are unavailable.
	if store, err := review.Open(); err == nil {
		d.Review = store
	}
	if dir, err := review.DefaultReportDir(); err == nil {
		d.ReportDir = dir
	}
	if o.agents.Accounts != nil {
		d.Accounts = o.agents.Accounts
	}
	if ops, ok := o.agents.Sessions.(agentsapi.OperationSessions); ok {
		d.Host = reviewHost{OperationSessions: ops, sessions: o.agents.Sessions}
	}
	d.Prefs = loadReviewPrefs
	if o.links != nil {
		d.Links = o.links
	} else if store, err := sessionlink.Open(); err == nil {
		d.Links = store
	}
	if o.shared != nil {
		d.GitHub = o.shared.Service(repoDir)
	} else {
		d.GitHub = ghcli.NewShared().Service(repoDir)
	}
	return newBackend(repoDir, d)
}

func newBackend(repoDir string, d Deps) (*Backend, error) {
	if d.Sessions == nil || d.Git == nil || d.Trees == nil {
		return nil, errors.New("playground backend needs sessions, git and worktrees")
	}
	if d.Now == nil {
		d.Now = func() time.Time { return time.Now().UTC() }
	}
	id := config.ProjectID(repoDir)
	local, err := garden.NewLocalRepoID(id)
	if err != nil {
		return nil, fmt.Errorf("playground repository identity: %w", err)
	}
	return &Backend{deps: d, dir: repoDir, local: local, project: id, snaps: map[string]snapshot{}}, nil
}

func (b *Backend) stamp(gen uint64) playgroundapi.Stamp {
	return playgroundapi.Stamp{Scope: b.local.String(), Generation: gen, FetchedAt: b.deps.Now()}
}

func fail(code playgroundapi.Code, msg string) *playgroundapi.Error {
	return &playgroundapi.Error{Code: code, Message: msg}
}

// ctxErr passes a canceled or expired caller context through untouched, so the
// TUI can tell "I gave up" from "the read failed".
func ctxErr(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

// resolve maps a directory to its real path, falling back to the cleaned path
// when it cannot be resolved (for example because it no longer exists).
func resolve(p string) string {
	p = filepath.Clean(p)
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return filepath.Clean(r)
	}
	return p
}

// scoped is a session proven to belong to this repository.
type scoped struct {
	session agentsapi.Session
	tree    coregit.Worktree // zero when missing
	// missing: the session belongs here but its worktree directory is verified gone.
	missing bool
}

// scope finds the session and proves it runs in a worktree of this repository,
// by the same rule as the Playground list: the session's resolved WorkDir or its
// WorktreeID equals a listed worktree, and a branch name alone never matches. A
// session recorded under this project whose directory no longer exists is
// reported as missing; anything else outside the repository is refused. The UI
// never supplies a path.
func (b *Backend) scope(ctx context.Context, id string) (scoped, error) {
	if id == "" || len(id) > playgroundapi.MaxSessionIDLn {
		return scoped{}, fail(playgroundapi.CodeInvalidInput, "A session is required.")
	}
	all, err := b.deps.Sessions.List(ctx)
	if err != nil {
		if e := ctxErr(ctx); e != nil {
			return scoped{}, e
		}
		return scoped{}, fail(playgroundapi.CodeUnavailable, "Sessions could not be read.")
	}
	var s *agentsapi.Session
	for i := range all {
		if all[i].ID == id {
			s = &all[i]
			break
		}
	}
	if s == nil {
		return scoped{}, fail(playgroundapi.CodeSessionNotFound, "That session no longer exists.")
	}
	trees, err := b.deps.Trees(ctx, b.dir)
	if err != nil {
		if e := ctxErr(ctx); e != nil {
			return scoped{}, e
		}
		return scoped{}, fail(playgroundapi.CodeUnavailable, "The repository's worktrees could not be read.")
	}
	dir := ""
	if s.WorkDir != "" {
		dir = resolve(s.WorkDir)
	}
	for _, t := range trees {
		if t.Bare {
			continue
		}
		if (dir != "" && dir == resolve(t.Path)) || (s.WorktreeID != "" && s.WorktreeID == localgit.ID(repositoryID, t.Path)) {
			if _, serr := os.Stat(t.Path); errors.Is(serr, os.ErrNotExist) {
				return scoped{session: *s, missing: true}, nil
			} else if serr != nil {
				return scoped{}, fail(playgroundapi.CodeUnavailable, "The worktree could not be read.")
			}
			return scoped{session: *s, tree: t}, nil
		}
	}
	if s.ProjectID == b.project && dir != "" {
		if _, serr := os.Lstat(dir); errors.Is(serr, os.ErrNotExist) {
			return scoped{session: *s, missing: true}, nil
		}
	}
	return scoped{}, fail(playgroundapi.CodeNotInRepository, "That session does not run in this repository.")
}

// attempt reads the launch attempt of a session's task, and only trusts it when
// it belongs to this clone and to the task's Issue.
func (b *Backend) attempt(ctx context.Context, s agentsapi.Session) (launch.Attempt, bool) {
	if s.Task == nil || s.Task.AttemptID == "" || b.deps.Attempts == nil {
		return launch.Attempt{}, false
	}
	a, err := b.deps.Attempts.Get(ctx, s.Task.AttemptID)
	if err != nil || a.ID != s.Task.AttemptID || a.Local != b.local || uint64(a.Issue) != s.Task.IssueID {
		return launch.Attempt{}, false
	}
	return a, true
}
