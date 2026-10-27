// Package gardenbackend implements the TUI's gardenapi contract on the real
// Garden packages: Git remotes, the GitHub adapter, the association store and
// the read model. It owns request generations and the association epoch, and
// reduces every failure to a safe gardenapi.Error.
package gardenbackend

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/browser"
	"github.com/Tiago-0liveira/bonsai/internal/core/clipboard"
	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/approve"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/capture"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/connect"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/edit"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/launch"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/ops"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/plan"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/planrev"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/publish"
	gardensync "github.com/Tiago-0liveira/bonsai/internal/core/garden/sync"
	coregit "github.com/Tiago-0liveira/bonsai/internal/core/git"
	"github.com/Tiago-0liveira/bonsai/internal/git/github/ghcli"
	ghgarden "github.com/Tiago-0liveira/bonsai/internal/git/github/ghcli/garden"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
)

// authCheckTimeout bounds the startup login check so a hung keyring cannot
// stall the TUI; a timeout leaves the backend usable.
const authCheckTimeout = 3 * time.Second

// Deps are the seams of the backend. New fills them from gh and Git; tests
// fill them with fakes.
type Deps struct {
	OpenURL  func(string) error
	CopyText func(string) error
	Issues   garden.IssueReader
	Remotes  connect.RemoteLister
	Resolver connect.RepositoryResolver
	// Ports are the label and Project reads and writes. ViewerLogin is set per call.
	Ports  connect.SetupPorts
	Viewer func(context.Context) (connect.GitHubIdentity, error)
	// InvalidateAuth forgets cached credentials; the next read uses gh's stored login.
	InvalidateAuth func()
	Store          *connect.Store
	PrivateDrafts  PrivateDraftStore
	// Creator and Ops enable capture: the only Issue-creating seam and the durable
	// record of each create. Both are optional; without them capture is unavailable.
	Creator garden.IssueCreator
	Ops     *ops.Store
	// Writer appends specification revisions (comments); it never edits an Issue.
	Writer garden.IssueWriter
	// Plans, Checkout and Agents enable the planner role (plan 08.2): the attempt history, the checkout a planner
	// would read and the agent runtime that hosts its read-only session. All three are optional; without them
	// Request AI plan is unavailable.
	Plans    *plan.Store
	Checkout plan.Inspector
	Agents   plan.Agents
	// Assist runs tool-free calls without a writer or a persistent session.
	Assist agentsapi.Assist
	// NewID returns a fresh random token for operation IDs and idempotency keys.
	NewID func() string
	// The Launch* seams enable the local implementation launch (plan 09). All are optional: without the
	// store, accounts, worktrees, holder and sessions the launch is unavailable and Garden is not. The clock
	// and ID source default to the real ones; PRs only enables publishing a Draft pull request.
	LaunchStore     launch.Store
	LaunchAccounts  launch.Accounts
	LaunchWorktrees launch.Worktrees
	LaunchHolder    launch.Holder
	LaunchSessions  launch.Sessions
	LaunchPRs       launch.PullRequests
	LaunchClock     launch.Clock
	LaunchIDs       launch.IDs
}

// Backend is the gardenapi.Backend for one local clone.
type Backend struct {
	deps  Deps
	dir   string // the clone a planner session runs in
	local garden.LocalRepoID
	gen   atomic.Uint64

	mu             sync.Mutex
	reads          *gardensync.Store // created once a Garden is known
	viewer         connect.GitHubIdentity
	haveViewer     bool
	authGeneration uint64
	captures       *capture.Service // built on first use
	edits          *edit.Service    // built on first use
	approvals      *approve.Service // built on first use
	publishes      *publish.Service // built on first use
	planner        *plan.Service    // built on first use
	reviews        *planrev.Service // built on first use
	assistFlights  map[string]struct{}
	launches       *launch.Service // built on first use
}

var _ gardenapi.Backend = (*Backend)(nil)

// Option customizes New.
type Option func(*options)

type options struct {
	shared *ghcli.Shared
	store  *connect.Store
	agents *agentsapi.Backend
	drafts PrivateDraftStore
}

// WithShared supplies the GitHub client state (tests inject a token runner).
func WithShared(s *ghcli.Shared) Option { return func(o *options) { o.shared = s } }

// WithStore overrides the association store location.
func WithStore(s *connect.Store) Option { return func(o *options) { o.store = s } }

// New builds the backend for the clone at repoDir. It returns an error, never
// panics, when gh has no usable login or the config directory is unavailable;
// the caller then runs without Garden. It reads the stored gh token only and
// never refreshes or changes the login.
func New(repoDir string, opts ...Option) (*Backend, error) {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	if o.shared == nil {
		o.shared = ghcli.NewShared()
	}
	ctx, cancel := context.WithTimeout(context.Background(), authCheckTimeout)
	defer cancel()
	switch st := o.shared.AuthStatus(ctx, ghcli.DefaultHost); st.State {
	case ghcli.AuthMissing, ghcli.AuthUnauthenticated:
		// Detail and Fix never contain the token.
		return nil, fmt.Errorf("%w: %s (run: %s)", ErrAuthUnavailable, st.Detail, st.Fix)
	}
	gh, err := ghgarden.FromService(o.shared.Service(repoDir))
	if err != nil {
		return nil, fmt.Errorf("garden github adapter: %w", err)
	}
	if o.store == nil {
		if o.store, err = connect.OpenStore(); err != nil {
			return nil, fmt.Errorf("garden store: %w", err)
		}
	}
	opStore, err := ops.Open()
	if err != nil {
		return nil, fmt.Errorf("garden operations: %w", err)
	}
	planStore, err := plan.Open()
	if err != nil {
		return nil, fmt.Errorf("garden planner attempts: %w", err)
	}
	deps := Deps{
		Issues:         gh.Reader(),
		Creator:        gh.Creator(),
		Writer:         gh.Writer(),
		Ops:            opStore,
		Remotes:        gitRemotes{coregit.RemoteLister{Dir: repoDir}},
		Resolver:       ghgarden.RepositoryResolver{Adapter: gh},
		Ports:          connect.SetupPorts{Labels: gh, LabelWrites: gh, Projects: gh, ProjectWrites: gh, OperationProjects: gh},
		Viewer:         gh.Viewer,
		InvalidateAuth: func() { o.shared.InvalidateAuth(ghcli.DefaultHost) },
		Store:          o.store,
		PrivateDrafts:  o.drafts,
		Plans:          planStore,
		Checkout:       checkoutInspector{coregit.CheckoutInspector{Dir: repoDir}},
	}
	if o.agents != nil {
		deps.Agents = plannerAgents{api: *o.agents, dir: repoDir}
		deps.Assist = o.agents.Assist
	}
	wireLaunch(&deps, repoDir, o.shared, o.agents)
	return newBackend(repoDir, deps)
}

// gitRemotes adapts the Git remote lister to the connect seam. core/git cannot
// import connect (connect imports config, which imports core/git).
type gitRemotes struct{ lister coregit.RemoteLister }

func (g gitRemotes) ListRemotes(ctx context.Context) ([]connect.Remote, error) {
	remotes, err := g.lister.ListRemotes(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]connect.Remote, len(remotes))
	for i, r := range remotes {
		out[i] = connect.Remote{Name: r.Name, URL: r.URL}
	}
	return out, nil
}

func newBackend(repoDir string, d Deps) (*Backend, error) {
	local, err := garden.NewLocalRepoID(config.ProjectID(repoDir))
	if err != nil {
		return nil, err
	}
	if d.Remotes == nil || d.Resolver == nil || d.Viewer == nil || d.Store == nil {
		return nil, errors.New("garden backend needs remotes, resolver, viewer and store")
	}
	if d.OpenURL == nil {
		d.OpenURL = browser.Open
	}
	if d.CopyText == nil {
		d.CopyText = clipboard.Copy
	}
	if d.NewID == nil {
		d.NewID = randomID
	}
	return &Backend{deps: d, dir: repoDir, local: local}, nil
}

func randomID() string {
	var b [8]byte
	// crypto/rand.Read cannot fail: the runtime aborts when the system has no entropy.
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// Local is the LocalRepoID this backend serves.
func (b *Backend) Local() garden.LocalRepoID { return b.local }

// Reads is the clone's read model store, for the Board plan. It is nil until a
// Garden target is known.
func (b *Backend) Reads() *gardensync.Store {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.reads
}

// reassociate bumps the epoch after the association changed, so replies and
// reads from before the change are dropped.
func (b *Backend) reassociate(id garden.GardenID) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.reads == nil {
		b.reads = gardensync.NewStore(id, b.local)
	}
	b.reads.Reassociate(id)
}

// observe makes sure a read model exists for a known Garden without changing it.
func (b *Backend) observe(id garden.GardenID) {
	if id.Repo == 0 {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.reads == nil {
		b.reads = gardensync.NewStore(id, b.local)
	}
}

func (b *Backend) stamp(gen uint64, repo garden.RepoID) gardenapi.Stamp {
	s := gardenapi.Stamp{LocalRepoID: b.local.String(), GardenID: uint64(repo), Generation: gen}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.reads != nil {
		s.Epoch = b.reads.Epoch()
	}
	return s
}

// invalidateAuth lets Refresh and Resume observe scope and account changes
// made outside Bonsai, without changing the user's GitHub login.
func (b *Backend) invalidateAuth() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.deps.InvalidateAuth != nil {
		b.deps.InvalidateAuth()
	}
	b.authGeneration++
	b.haveViewer = false
}

// identity returns the authenticated account, cached until credentials are reread.
func (b *Backend) identity(ctx context.Context) (connect.GitHubIdentity, error) {
	b.mu.Lock()
	if b.haveViewer {
		id := b.viewer
		b.mu.Unlock()
		return id, nil
	}
	gen := b.authGeneration
	b.mu.Unlock()
	id, err := b.deps.Viewer(ctx)
	if err != nil {
		return connect.GitHubIdentity{}, err
	}
	b.mu.Lock()
	if b.authGeneration == gen {
		b.viewer, b.haveViewer = id, true
	}
	b.mu.Unlock()
	return id, nil
}

// resolve returns the target, or a *connect.AmbiguousError. A non-zero ref
// picks one candidate by numeric ID.
func (b *Backend) resolve(ctx context.Context, ref gardenapi.TargetRef) (connect.Target, []string, error) {
	var chosen *connect.Candidate
	if ref.RepoID != 0 {
		chosen = &connect.Candidate{Repository: connect.Repository{ID: garden.RepoID(ref.RepoID)}}
	}
	t, notes, err := connect.ResolveTarget(ctx, b.deps.Remotes, b.deps.Resolver, chosen)
	if err != nil && chosen != nil && !isKnownFailure(err) {
		// The only unclassified failure with a chosen ID is "not a candidate".
		return connect.Target{}, notes, ErrRemoteChanged
	}
	return t, notes, err
}

func isKnownFailure(err error) bool {
	var scope connect.ErrScopeMissing
	var amb *connect.AmbiguousError
	for _, known := range []error{connect.ErrAuthRequired, connect.ErrRateLimited, connect.ErrOffline, connect.ErrPermissionDenied,
		connect.ErrRepositoryIdentityChanged, connect.ErrGitHubValidation, connect.ErrNoGitHubRemote, connect.ErrTargetAmbiguous} {
		if errors.Is(err, known) {
			return true
		}
	}
	return errors.As(err, &scope) || errors.As(err, &amb)
}

// connection builds the capability set for a resolved target.
func (b *Backend) connection(t connect.Target, id connect.GitHubIdentity) connect.Connection {
	return connect.Connection{
		Target: t, Access: t.Repository.Access,
		ProjectScope: t.Repository.ProjectScope, ProjectScopeKnown: t.Repository.ProjectScopeKnown,
		Identity: id, ViewerLogin: id.Login,
		ProjectReader: b.deps.Ports.Projects, ProjectWriter: b.deps.Ports.ProjectWrites,
	}
}

func (b *Backend) setupPorts(id connect.GitHubIdentity) connect.SetupPorts {
	p := b.deps.Ports
	p.ViewerLogin = id.Login
	return p
}

// association returns the revision and this clone's association, nil if none.
func (b *Backend) association(ctx context.Context) (uint64, *connect.Association, error) {
	rev, all, err := b.deps.Store.Snapshot(ctx)
	if err != nil {
		return 0, nil, err
	}
	for i := range all {
		if all[i].LocalRepoID == b.local {
			return rev, &all[i], nil
		}
	}
	return rev, nil, nil
}

// putAssociation writes this clone's association. guard sees the association
// found on every attempt and may refuse: other clones and processes write the
// shared store constantly, and a retry after a revision change must not
// overwrite a decision that landed in between.
func (b *Backend) putAssociation(ctx context.Context, a connect.Association, guard func(cur *connect.Association) error) error {
	key := "gardenbackend-put-" + b.deps.NewID()
	for attempt := 0; ; attempt++ {
		rev, cur, err := b.association(ctx)
		if err != nil {
			return err
		}
		if err = guard(cur); err != nil {
			return err
		}
		_, err = b.deps.Store.Put(ctx, rev, key, a)
		if !errors.Is(err, connect.ErrRevision) || attempt >= 4 {
			return err
		}
	}
}

// removeAssociation deletes this clone's association under the same rules.
func (b *Backend) removeAssociation(ctx context.Context, guard func(cur *connect.Association) error) error {
	key := "gardenbackend-remove-" + b.deps.NewID()
	for attempt := 0; ; attempt++ {
		rev, cur, err := b.association(ctx)
		if err != nil {
			return err
		}
		if err = guard(cur); err != nil {
			return err
		}
		_, err = b.deps.Store.Remove(ctx, rev, key, b.local)
		if !errors.Is(err, connect.ErrRevision) || attempt >= 4 {
			return err
		}
	}
}

func accessOf(a connect.Access) gardenapi.Access {
	switch a {
	case connect.AccessRead:
		return gardenapi.AccessRead
	case connect.AccessWrite:
		return gardenapi.AccessWrite
	case connect.AccessAdmin:
		return gardenapi.AccessAdmin
	}
	return gardenapi.AccessNone
}

func presence(known, present bool) gardenapi.Presence {
	switch {
	case !known:
		return gardenapi.PresenceUnknown
	case present:
		return gardenapi.PresencePresent
	}
	return gardenapi.PresenceAbsent
}
