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
	journal, err := gitstore.Open(procstore.New(main).Dir() + "/git-commands.json")
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
	return benchPass(ctx, s, ids, opts)
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
	var ids []string
	registry.mu.Lock()
	for id, p := range registry.entries {
		if !p.info.Available || (opts.Repo != "" && p.info.Path != opts.Repo) {
			continue
		}
		d, err := newInProcessDaemon(rootsPath, p.info.Path)
		if err != nil {
			registry.mu.Unlock()
			return nil, nil, fmt.Errorf("%s: %w", p.info.Path, err)
		}
		p.daemon = d
		registry.entries[id] = p
		ids = append(ids, id)
	}
	registry.mu.Unlock()
	sort.Strings(ids)
	if opts.Projects > 0 && len(ids) > opts.Projects {
		ids = ids[:opts.Projects]
	}
	if len(ids) == 0 {
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
			snap, ok := s.CachedSnapshot(id)
			if !ok {
				allDone = false
				continue
			}
			elapsed := time.Since(start)
			row.Name = snap.Repository.Name
			if snap.Local != nil {
				row.Worktrees = len(snap.Local.Worktrees)
				if row.Inventory == 0 {
					row.Inventory = elapsed
				}
			}
			if row.LocalReady == 0 && snap.Freshness["local"].State == "ready" {
				row.LocalReady = elapsed
			}
			provider := snap.Freshness["provider"]
			if row.PRCatalog == 0 && providerSettled(provider.State) && (snap.Remote == nil || snap.Remote.PRCatalogComplete) {
				row.PRCatalog = elapsed
				if provider.Error != nil {
					row.ProviderErr = provider.Error.Code
				}
			}
			if row.PRCatalog != 0 && row.AllChecks == 0 && checksSettled(snap) {
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

func checksSettled(snap browserSnapshot) bool {
	for _, state := range snap.WorktreeState {
		if state.CI.Freshness.State == "loading" {
			return false
		}
	}
	return true
}
