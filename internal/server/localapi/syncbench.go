package localapi

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
	"github.com/Tiago-0liveira/bonsai/internal/core/trace"
	"github.com/Tiago-0liveira/bonsai/internal/daemon/gitbridge"
	"github.com/Tiago-0liveira/bonsai/internal/git/local"
	gitstore "github.com/Tiago-0liveira/bonsai/internal/storage/git"
)

// SyncBenchOptions configures SyncBench.
type SyncBenchOptions struct {
	// ProjectRootsPath overrides the default project-roots.json location.
	ProjectRootsPath string
	// Root benchmarks every repository under this directory using a throwaway
	// project-roots file, leaving the user's configuration untouched.
	Root string
	// Repo restricts the run to the project whose root path equals this value.
	Repo string
	// Projects limits the run to the first N available projects (0 = all).
	Projects int
	// Timeout bounds each pass (default 2 minutes).
	Timeout time.Duration
}

// SyncBenchRow is one project's timings and process spawns for one pass.
type SyncBenchRow struct {
	Project     string
	Name        string
	Worktrees   int
	Inventory   time.Duration // first local state published
	LocalReady  time.Duration // local freshness ready
	PRCatalog   time.Duration // provider settled and PR catalog complete (or provider unavailable)
	AllChecks   time.Duration // every worktree's CI freshness settled
	GitSpawns   int64         // process-wide delta over the whole pass (not per project)
	GHSpawns    int64
	Incomplete  bool // a milestone was not reached before Timeout
	ProviderErr string
}

// SyncBenchResult holds one pass over the selected projects.
type SyncBenchResult struct {
	Rows      []SyncBenchRow
	Total     SyncBenchRow // milestones are the max over projects; spawns are the pass totals
	GitSpawns int64
	GHSpawns  int64
}

// inProcessDaemon serves git commands from this process, so the spawn
// counters and spans cover work that production runs inside the per-repo
// daemon. It has no process supervision.
type inProcessDaemon struct{ executor *gitbridge.Executor }

func (d inProcessDaemon) Git(c gitbridge.Command) (*gitbridge.Result, error) {
	return d.GitContext(context.Background(), c)
}

func (d inProcessDaemon) GitContext(ctx context.Context, c gitbridge.Command) (*gitbridge.Result, error) {
	result := d.executor.Execute(ctx, c)
	return &result, nil
}

func (inProcessDaemon) List() ([]*procstore.Record, error) { return nil, nil }

func (inProcessDaemon) ListContext(context.Context) ([]*procstore.Record, error) { return nil, nil }

func (inProcessDaemon) Restart(int) (*procstore.Record, error) {
	return nil, fmt.Errorf("process control is unavailable in sync bench")
}

func (inProcessDaemon) Kill(int, bool, string) ([]int, error) {
	return nil, fmt.Errorf("process control is unavailable in sync bench")
}

func (inProcessDaemon) Logs(int, bool, int, string, bool, func(string) error) error {
	return fmt.Errorf("process control is unavailable in sync bench")
}

func newInProcessDaemon(rootsPath, main string) (inProcessDaemon, error) {
	svc, err := local.New([]local.Config{{
		ID:   localRepositoryID,
		Root: main,
		WithWorktreeRoot: func(ctx context.Context, create func(string) error) error {
			return config.WithBrowserWorktreeRoot(ctx, rootsPath, main, create)
		},
	}})
	if err != nil {
		return inProcessDaemon{}, err
	}
	// Reads never touch the journal, but the executor expects one.
	journal, err := gitstore.Open(filepath.Join(procstore.New(main).Dir(), "git-commands.json"))
	if err != nil {
		return inProcessDaemon{}, err
	}
	return inProcessDaemon{executor: &gitbridge.Executor{Local: svc, Journal: journal}}, nil
}

// SyncBench runs one cold sync pass for the configured projects, in process,
// and reports when each milestone was reached. Provider and projection state
// are in memory only today, so every run starts cold.
func SyncBench(ctx context.Context, opts SyncBenchOptions) (SyncBenchResult, error) {
	s, ids, err := newBenchSync(ctx, opts)
	if err != nil {
		return SyncBenchResult{}, err
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer func() {
		// Stop queued retries and re-queues from outliving the bench.
		cancel()
		s.mu.Lock()
		s.closed = true
		s.mu.Unlock()
	}()
	s.mu.Lock()
	s.runCtx = runCtx
	s.mu.Unlock()
	return benchPass(runCtx, s, ids, opts)
}

func newBenchSync(ctx context.Context, opts SyncBenchOptions) (*stateSync, []string, error) {
	rootsPath := opts.ProjectRootsPath
	if opts.Root != "" {
		dir, err := os.MkdirTemp("", "bonsai-sync-bench-")
		if err != nil {
			return nil, nil, err
		}
		// Everything read afterwards comes from git and gh, not these files.
		defer os.RemoveAll(dir)
		rootsPath = filepath.Join(dir, "project-roots.json")
		if _, err := config.UpdateProjectRoots(rootsPath, "bench", 0, opts.Root, ""); err != nil {
			return nil, nil, err
		}
		scan := newProjectRegistry(rootsPath, "")
		if _, err := scan.Refresh(ctx); err != nil {
			return nil, nil, err
		}
		var selected []string
		for _, candidate := range scan.Candidates() {
			selected = append(selected, candidate.ID)
		}
		if err := scan.UpdateSelection(scan.SelectionRevision(), selected); err != nil {
			return nil, nil, err
		}
	}
	if rootsPath == "" {
		var err error
		if rootsPath, err = config.ProjectRootsPath(); err != nil {
			return nil, nil, err
		}
	}
	registry := newProjectRegistry(rootsPath, "")
	if _, err := registry.Refresh(ctx); err != nil {
		return nil, nil, err
	}
	wantRepo := opts.Repo
	if wantRepo != "" {
		if canonical, err := config.CanonicalDirectory(wantRepo); err == nil {
			wantRepo = canonical
		}
	}
	var ids []string
	registry.mu.Lock()
	for id, p := range registry.entries {
		if p.info.Available && (wantRepo == "" || filepath.Clean(p.info.Path) == wantRepo) {
			ids = append(ids, id)
		}
	}
	// Ordered by project ID (a hash), not by name.
	sort.Strings(ids)
	if opts.Projects > 0 && len(ids) > opts.Projects {
		ids = ids[:opts.Projects]
	}
	keep := make(map[string]bool, len(ids))
	for _, id := range ids {
		keep[id] = true
	}
	// Drop everything not benchmarked so no production daemon client is left
	// reachable from the registry.
	for id := range registry.entries {
		if !keep[id] {
			delete(registry.entries, id)
		}
	}
	for _, id := range ids {
		p := registry.entries[id]
		d, err := newInProcessDaemon(rootsPath, p.info.Path)
		if err != nil {
			registry.mu.Unlock()
			return nil, nil, fmt.Errorf("%s: %w", p.info.Path, err)
		}
		p.daemon = d
		registry.entries[id] = p
	}
	registry.mu.Unlock()
	if len(ids) == 0 {
		if opts.Repo != "" {
			return nil, nil, fmt.Errorf("no available project matches --repo %s (it must be a repository's main worktree selected under the roots)", opts.Repo)
		}
		return nil, nil, fmt.Errorf("no available projects found in %s", rootsPath)
	}
	s := newStateSync(registry, newEventHub())
	s.ReconcileCatalog()
	return s, ids, nil
}

func benchPass(ctx context.Context, s *stateSync, ids []string, opts SyncBenchOptions) (SyncBenchResult, error) {
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	deadline := time.Now().Add(timeout)
	g0, h0 := trace.Counts()
	start := time.Now()
	for _, id := range ids {
		s.Queue(id, refreshAll, false)
	}
	rows := make([]SyncBenchRow, len(ids))
	for i, id := range ids {
		rows[i].Project = id
	}
	for {
		allDone := true
		for i, id := range ids {
			row := &rows[i]
			view, ok := s.benchView(id)
			if !ok {
				allDone = false
				continue
			}
			elapsed := time.Since(start)
			row.Name = view.name
			if view.hasLocal {
				row.Worktrees = view.worktrees
				if row.Inventory == 0 {
					row.Inventory = elapsed
				}
			}
			if row.LocalReady == 0 && view.localState == "ready" {
				row.LocalReady = elapsed
			}
			if row.PRCatalog == 0 && providerSettled(view.providerState) && view.catalogDone {
				row.PRCatalog = elapsed
				row.ProviderErr = view.providerErr
			}
			if row.PRCatalog != 0 && row.AllChecks == 0 && !view.checksLoading {
				row.AllChecks = elapsed
			}
			if row.Inventory == 0 || row.LocalReady == 0 || row.PRCatalog == 0 || row.AllChecks == 0 {
				allDone = false
			}
		}
		if allDone {
			break
		}
		if time.Now().After(deadline) {
			for i := range rows {
				rows[i].Incomplete = rows[i].Inventory == 0 || rows[i].LocalReady == 0 || rows[i].PRCatalog == 0 || rows[i].AllChecks == 0
			}
			break
		}
		select {
		case <-ctx.Done():
			return SyncBenchResult{}, ctx.Err()
		case <-time.After(5 * time.Millisecond):
		}
	}
	g1, h1 := trace.Counts()
	result := SyncBenchResult{Rows: rows, GitSpawns: g1 - g0, GHSpawns: h1 - h0}
	result.Total = SyncBenchRow{Project: "total", Name: "all projects", GitSpawns: result.GitSpawns, GHSpawns: result.GHSpawns}
	for _, row := range rows {
		result.Total.Worktrees += row.Worktrees
		result.Total.Inventory = max(result.Total.Inventory, row.Inventory)
		result.Total.LocalReady = max(result.Total.LocalReady, row.LocalReady)
		result.Total.PRCatalog = max(result.Total.PRCatalog, row.PRCatalog)
		result.Total.AllChecks = max(result.Total.AllChecks, row.AllChecks)
		result.Total.Incomplete = result.Total.Incomplete || row.Incomplete
	}
	return result, nil
}

func providerSettled(state string) bool { return state != "" && state != "loading" }

// benchView is the few fields the bench polls. Reading them under the sync
// lock without cloning the snapshot keeps the 5 ms poll from competing with the
// commits it is timing.
type benchView struct {
	name          string
	hasLocal      bool
	worktrees     int
	localState    string
	providerState string
	providerErr   string
	catalogDone   bool
	checksLoading bool
}

func (s *stateSync) benchView(id string) (benchView, bool) {
	project, ok := s.registry.Lookup(id)
	if !ok {
		return benchView{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	snap := s.ensureLocked(project.info).snapshot
	v := benchView{
		name:          snap.Repository.Name,
		hasLocal:      snap.Local != nil,
		localState:    snap.Freshness["local"].State,
		providerState: snap.Freshness["provider"].State,
		catalogDone:   snap.Remote == nil || snap.Remote.PRCatalogComplete,
	}
	if snap.Local != nil {
		v.worktrees = len(snap.Local.Worktrees)
	}
	if err := snap.Freshness["provider"].Error; err != nil {
		v.providerErr = err.Code
	}
	for _, state := range snap.WorktreeState {
		if state.CI.Freshness.State == "loading" {
			v.checksLoading = true
			break
		}
	}
	return v, true
}
