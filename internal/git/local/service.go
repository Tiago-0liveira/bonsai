// Package local implements Git on the daemon's machine. Callers use registered
// IDs, never filesystem paths. The legacy core runner and worktree parser remain
// shared with the CLI/TUI.
package local

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	core "github.com/Tiago-0liveira/bonsai/internal/core/git"
	"github.com/Tiago-0liveira/bonsai/internal/core/trace"
	domain "github.com/Tiago-0liveira/bonsai/internal/git"
)

type Config struct {
	ID, Root, WorktreeRoot string
	WithWorktreeRoot       func(context.Context, func(string) error) error
	BeforeRemove           func(context.Context, string) error
}
type repository struct {
	Config
	gate chan struct{}
	inv  inventoryCache
}

// inventoryCacheTTL bounds how long a worktree inventory read by Repository or
// ListWorktrees may answer ID lookups. Detail reads (status, files, diff) of
// several worktrees right after a refresh then share one `git worktree list`.
const inventoryCacheTTL = 2 * time.Second

type inventoryCache struct {
	mu    sync.Mutex
	gen   uint64
	at    time.Time
	trees []core.Worktree
}

// begin returns the generation a list is about to be read under. A list that
// finishes after invalidate was called must not be stored: it may predate the
// mutation that invalidated the cache.
func (c *inventoryCache) begin() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.gen
}

func (c *inventoryCache) store(gen uint64, trees []core.Worktree) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if gen != c.gen {
		return
	}
	c.at, c.trees = time.Now(), append([]core.Worktree(nil), trees...)
}

func (c *inventoryCache) load() ([]core.Worktree, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.trees == nil || time.Since(c.at) > inventoryCacheTTL {
		return nil, false
	}
	return c.trees, true
}

func (c *inventoryCache) invalidate() {
	c.mu.Lock()
	c.gen++
	c.at, c.trees = time.Time{}, nil
	c.mu.Unlock()
}

// statusPoolSize bounds concurrent worktree status reads in Repository.
var statusPoolSize = func() int { return min(runtime.GOMAXPROCS(0), 8) }

// statusGlobalSlots caps `git status` processes across every repository served
// by this process, so several projects refreshing at once do not multiply the
// per-repository pool.
var statusGlobalSlots = make(chan struct{}, 8)

// overviewStatus is a seam so tests can observe pool concurrency.
var overviewStatus = statusOverview

type Service struct {
	repos      map[string]*repository
	mu         sync.Mutex
	operations map[string]domain.Operation
}

var _ domain.LocalGitService = (*Service)(nil)

func New(configs []Config) (*Service, error) {
	s := &Service{repos: map[string]*repository{}, operations: map[string]domain.Operation{}}
	for _, c := range configs {
		if c.ID == "" || s.repos[c.ID] != nil {
			return nil, domain.ErrInvalid
		}
		root, err := core.MainRoot(c.Root)
		if err != nil {
			return nil, err
		}
		c.Root, err = filepath.EvalSymlinks(root)
		if err != nil {
			return nil, err
		}
		if c.WithWorktreeRoot == nil {
			if c.WorktreeRoot == "" {
				return nil, fmt.Errorf("worktree root must be configured")
			}
			c.WorktreeRoot, err = filepath.Abs(c.WorktreeRoot)
			if err != nil {
				return nil, err
			}
			if err = os.MkdirAll(c.WorktreeRoot, 0700); err != nil {
				return nil, err
			}
			c.WorktreeRoot, err = filepath.EvalSymlinks(c.WorktreeRoot)
			if err != nil {
				return nil, err
			}
		}
		s.repos[c.ID] = &repository{Config: c, gate: make(chan struct{}, 1)}
	}
	return s, nil
}
func ID(repo, path string) string {
	return fmt.Sprintf("wt-%x", sha256.Sum256([]byte(repo+"\x00"+filepath.Clean(path))))[:35]
}
func (s *Service) repo(id string) (*repository, error) {
	r := s.repos[id]
	if r == nil {
		return nil, domain.ErrNotFound
	}
	return r, nil
}
func (r *repository) lock(ctx context.Context) (func(), error) {
	select {
	case r.gate <- struct{}{}:
		return func() { <-r.gate }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func run(ctx context.Context, dir string, args ...string) (string, error) {
	return core.RunContext(ctx, dir, args...)
}
func trimmed(ctx context.Context, dir string, args ...string) (string, error) {
	v, e := run(ctx, dir, args...)
	return strings.TrimSpace(v), e
}
func (s *Service) target(ctx context.Context, id string) (*repository, string, error) {
	for attempt := 0; ; attempt++ {
		r, path, fromCache, err := s.lookupTarget(ctx, id, attempt == 0)
		if err != nil {
			return nil, "", err
		}
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			if fromCache && attempt == 0 {
				// The cached inventory may predate a removal; ask Git so the
				// caller sees "not found" rather than a filesystem error.
				r.inv.invalidate()
				continue
			}
			return nil, "", err
		}
		return r, resolved, nil
	}
}

// registeredTarget looks up Git's inventory without requiring the working
// directory to exist. Removal must also support stale worktree registrations,
// so it never reads the cache.
func (s *Service) registeredTarget(ctx context.Context, id string) (*repository, string, error) {
	r, path, _, err := s.lookupTarget(ctx, id, false)
	return r, path, err
}

func (s *Service) lookupTarget(ctx context.Context, id string, useCache bool) (*repository, string, bool, error) {
	find := func(r *repository, trees []core.Worktree) (string, bool) {
		for _, t := range trees {
			if !t.Bare && ID(r.ID, t.Path) == id {
				return t.Path, true
			}
		}
		return "", false
	}
	if useCache {
		for _, r := range s.repos {
			if trees, ok := r.inv.load(); ok {
				if path, found := find(r, trees); found {
					return r, path, true, nil
				}
			}
		}
	}
	for _, r := range s.repos {
		gen := r.inv.begin()
		trees, err := core.ListWorktreesContext(ctx, r.Root)
		if err != nil {
			return nil, "", false, err
		}
		r.inv.store(gen, trees)
		if path, found := find(r, trees); found {
			return r, path, false, nil
		}
	}
	return nil, "", false, domain.ErrNotFound
}
func ref(ctx context.Context, dir, name string) (string, error) {
	if name == "" || strings.HasPrefix(name, "-") || strings.ContainsAny(name, "\x00\r\n") {
		return "", domain.ErrInvalid
	}
	sha, err := trimmed(ctx, dir, "rev-parse", "--verify", "--end-of-options", name+"^{commit}")
	if err != nil {
		return "", domain.E("not_found", "ref does not resolve to a commit")
	}
	return sha, nil
}
func branch(ctx context.Context, dir, name string) error {
	if name == "" || strings.HasPrefix(name, "-") || strings.HasPrefix(name, "refs/") {
		return domain.ErrInvalid
	}
	_, err := run(ctx, dir, "check-ref-format", "refs/heads/"+name)
	if err != nil {
		return domain.E("invalid", "invalid branch name")
	}
	return nil
}
func (s *Service) Repository(ctx context.Context, id string) (domain.RepositoryState, error) {
	r, e := s.repo(id)
	if e != nil {
		return domain.RepositoryState{}, e
	}
	unlock, e := r.lock(ctx)
	if e != nil {
		return domain.RepositoryState{}, e
	}
	defer unlock()

	endInventory := trace.Start("local.inventory", r.Root)
	refs, e := listRefs(ctx, r.Root)
	if e != nil {
		endInventory()
		return domain.RepositoryState{}, e
	}
	w, e := s.ListWorktrees(ctx, id)
	endInventory()
	if e != nil {
		return domain.RepositoryState{}, e
	}

	// Independent of the statuses, so it overlaps them.
	var remotes []domain.RemoteIdentity
	var side sync.WaitGroup
	side.Add(1)
	go func() {
		defer side.Done()
		remotes = remoteIdentities(ctx, r.Root)
	}()

	// Parallel inside the repository lock: nothing mutates while statuses run.
	// Results are written by index, and one failing worktree stays isolated.
	slots := make(chan struct{}, max(1, statusPoolSize()))
	var pool sync.WaitGroup
	for i := range w {
		if w[i].Missing {
			w[i].StatusError = &domain.StateError{Code: "worktree_missing", Message: "Worktree directory is missing; its Git registration remains"}
			continue
		}
		pool.Add(1)
		slots <- struct{}{}
		go func(tree *domain.Worktree) {
			defer pool.Done()
			defer func() { <-slots }()
			statusGlobalSlots <- struct{}{}
			defer func() { <-statusGlobalSlots }()
			endStatus := trace.Start("local.status", r.Root, trace.Attrs{Worktree: tree.Path})
			st, err := overviewStatus(ctx, tree.Path, refs)
			endStatus()
			if err != nil {
				tree.StatusError = &domain.StateError{Code: domain.Code(err), Message: err.Error()}
				if tree.StatusError.Code == "" {
					tree.StatusError.Code = "status_unavailable"
				}
				return
			}
			tree.Status = &st
		}(&w[i])
	}
	pool.Wait()
	side.Wait()

	// refs/remotes/origin/HEAD is a symbolic ref; for-each-ref already reported
	// its target, so no `symbolic-ref` process is needed.
	defaultBranch := strings.TrimPrefix(refs.originHead, "refs/remotes/origin/")
	if defaultBranch == "" {
		for _, tree := range w {
			if tree.Main && tree.Branch != "" && tree.Branch != "(detached)" {
				defaultBranch = tree.Branch
				break
			}
		}
	}
	state := domain.RepositoryState{
		ID:            id,
		DefaultBranch: defaultBranch,
		Branches:      refs.branches(),
		Worktrees:     w,
		Remotes:       remotes,
	}
	domain.ClassifyWorktrees(&state, nil)
	return state, nil
}

// Inventory is the cheap first read of a repository: worktrees, branches and
// remote identities, with no status. It takes no repository lock, like
// ListBranches and ListWorktrees, and lets a caller start provider work before
// the per-worktree statuses finish.
func (s *Service) Inventory(ctx context.Context, id string) (domain.RepositoryState, error) {
	r, e := s.repo(id)
	if e != nil {
		return domain.RepositoryState{}, e
	}
	var (
		refs     *refIndex
		trees    []domain.Worktree
		remotes  []domain.RemoteIdentity
		refsErr  error
		treesErr error
		wg       sync.WaitGroup
	)
	wg.Add(3)
	go func() { defer wg.Done(); refs, refsErr = listRefs(ctx, r.Root) }()
	go func() { defer wg.Done(); trees, treesErr = s.ListWorktrees(ctx, id) }()
	go func() { defer wg.Done(); remotes = remoteIdentities(ctx, r.Root) }()
	wg.Wait()
	if refsErr != nil {
		return domain.RepositoryState{}, refsErr
	}
	if treesErr != nil {
		return domain.RepositoryState{}, treesErr
	}
	return domain.RepositoryState{ID: id, Branches: refs.branches(), Worktrees: trees, Remotes: remotes}, nil
}

func (s *Service) ListBranches(ctx context.Context, id string) ([]domain.Branch, error) {
	r, e := s.repo(id)
	if e != nil {
		return nil, e
	}
	refs, e := listRefs(ctx, r.Root)
	if e != nil {
		return nil, e
	}
	return refs.branches(), nil
}
func (s *Service) ListWorktrees(ctx context.Context, id string) ([]domain.Worktree, error) {
	r, e := s.repo(id)
	if e != nil {
		return nil, e
	}
	gen := r.inv.begin()
	trees, e := core.ListWorktreesContext(ctx, r.Root)
	if e != nil {
		return nil, e
	}
	r.inv.store(gen, trees)
	result := []domain.Worktree{}
	for _, t := range trees {
		if t.Bare {
			continue
		}
		_, statErr := os.Lstat(t.Path)
		result = append(result, domain.Worktree{ID: ID(id, t.Path), RepositoryID: id, Path: t.Path, Branch: t.Branch, HeadSHA: t.HEAD, Main: t.IsMain, Missing: os.IsNotExist(statErr)})
	}
	return result, nil
}

// RemoteIdentities lists the remotes of the repository at dir with the
// GitHub repository each one names (FullName, empty for other hosts).
func RemoteIdentities(ctx context.Context, dir string) []domain.RemoteIdentity {
	return remoteIdentities(ctx, dir)
}

func remoteIdentities(ctx context.Context, dir string) []domain.RemoteIdentity {
	out, err := run(ctx, dir, "remote")
	if err != nil {
		return nil
	}
	identities := []domain.RemoteIdentity{}
	for _, name := range strings.Fields(out) {
		raw, err := trimmed(ctx, dir, "remote", "get-url", name)
		if err != nil || raw == "" {
			continue
		}
		identities = append(identities, sanitizeRemoteIdentity(name, raw))
	}
	return identities
}

func sanitizeRemoteIdentity(name, raw string) domain.RemoteIdentity {
	identity := domain.RemoteIdentity{Name: name}
	host, path := "", ""
	if strings.Contains(raw, "://") {
		u, err := url.Parse(raw)
		if err != nil {
			return identity
		}
		host = strings.ToLower(u.Hostname())
		path = u.Path
	} else {
		value := raw
		if at := strings.LastIndex(value, "@"); at >= 0 {
			value = value[at+1:]
		}
		if colon := strings.Index(value, ":"); colon > 0 {
			host = strings.ToLower(value[:colon])
			path = value[colon+1:]
		}
	}
	identity.Host = host
	parts := strings.Split(strings.Trim(strings.TrimSuffix(path, ".git"), "/"), "/")
	if len(parts) != 2 {
		return identity
	}
	identity.Owner, identity.Repository = parts[0], parts[1]
	if strings.EqualFold(host, "github.com") && identity.Owner != "" && identity.Repository != "" {
		identity.FullName = identity.Owner + "/" + identity.Repository
	}
	return identity
}
