package localapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	coregit "github.com/Tiago-0liveira/bonsai/internal/core/git"
	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
	"github.com/Tiago-0liveira/bonsai/internal/daemon/client"
	"github.com/Tiago-0liveira/bonsai/internal/git/github/ghcli"
	"github.com/Tiago-0liveira/bonsai/internal/git/local"
	gitstore "github.com/Tiago-0liveira/bonsai/internal/storage/git"
)

const discoveryDepth = config.ProjectDiscoveryDepth
const discoveryLimit = 10000
const discoveryRefreshTimeout = time.Minute

type RootDiagnostic struct {
	RootID    string   `json:"root_id"`
	Available bool     `json:"available"`
	Truncated bool     `json:"truncated"`
	Messages  []string `json:"messages"`
}

type ProjectCandidate struct {
	ID        string `json:"id"`
	RootID    string `json:"root_id"`
	Name      string `json:"name"`
	Path      string `json:"path"`
	Selected  bool   `json:"selected"`
	Available bool   `json:"available"`
}

type projectSelectionConfig struct {
	Version  int      `json:"version"`
	Revision uint64   `json:"revision"`
	Selected []string `json:"selected"`
}

var errProjectSelectionRevision = errors.New("project selection revision changed")

type discoveredRepository struct {
	main      string
	paths     []string
	worktrees []string
}
type rootScan struct {
	diagnostic RootDiagnostic
	repos      []discoveredRepository
	complete   bool
}

func scanRoot(ctx context.Context, root config.ProjectRoot) rootScan {
	out := rootScan{diagnostic: RootDiagnostic{RootID: root.ID, Available: true, Messages: []string{}}, complete: true}
	canonical, err := config.CanonicalDirectory(root.Path)
	if err != nil || canonical != root.Path {
		out.complete = false
		out.diagnostic.Available = false
		if err != nil {
			out.diagnostic.Messages = append(out.diagnostic.Messages, err.Error())
		} else {
			out.diagnostic.Messages = append(out.diagnostic.Messages, "Folder now resolves to a different location; remove and add it again.")
		}
		return out
	}
	count := 0
	var walk func(string, int)
	walk = func(path string, depth int) {
		if ctx.Err() != nil {
			out.complete = false
			return
		}
		count++
		if count > discoveryLimit {
			out.diagnostic.Truncated = true
			out.complete = false
			return
		}
		dir, err := os.Open(path)
		var entries []os.DirEntry
		if err == nil {
			entries, err = dir.ReadDir(discoveryLimit - count + 1)
			dir.Close()
			if errors.Is(err, io.EOF) {
				err = nil
			}
		}
		if len(entries) > discoveryLimit-count {
			entries = entries[:discoveryLimit-count]
			out.diagnostic.Truncated = true
			out.complete = false
		}
		count += len(entries)
		sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
		if err != nil {
			out.complete = false
			out.diagnostic.Messages = append(out.diagnostic.Messages, fmt.Sprintf("%s: %v", path, err))
			if depth == 0 {
				out.diagnostic.Available = false
			}
			return
		}
		repo, bare := false, false
		for _, e := range entries {
			if e.Name() == ".git" {
				repo = true
			}
			if e.Name() == "HEAD" {
				if info, err := os.Stat(filepath.Join(path, "objects")); err == nil && info.IsDir() {
					bare = true
				}
			}
		}
		if bare && !repo {
			out.diagnostic.Messages = append(out.diagnostic.Messages, "Bare repository skipped: "+path)
			return
		}
		if repo {
			trees, err := coregit.ListWorktreesContext(ctx, path)
			if err != nil || len(trees) == 0 {
				out.complete = false
				out.diagnostic.Messages = append(out.diagnostic.Messages, "Cannot inspect repository: "+path)
				return
			}
			if trees[0].Bare {
				out.diagnostic.Messages = append(out.diagnostic.Messages, "Bare repository skipped: "+path)
				return
			}
			main, err := filepath.EvalSymlinks(trees[0].Path)
			if err != nil {
				out.complete = false
				out.diagnostic.Messages = append(out.diagnostic.Messages, "Main repository unavailable: "+trees[0].Path)
				return
			}
			found := discoveredRepository{main: main, paths: []string{path}}
			for _, tree := range trees {
				if !tree.Bare {
					found.worktrees = append(found.worktrees, tree.Path)
				}
			}
			out.repos = append(out.repos, found)
			return
		}
		for _, e := range entries {
			if !e.IsDir() || e.Type()&os.ModeSymlink != 0 || config.SkipProjectDirectory(e.Name()) {
				continue
			}
			if depth >= discoveryDepth {
				out.diagnostic.Truncated = true
				out.complete = false
				continue
			}
			if count >= discoveryLimit {
				out.diagnostic.Truncated = true
				out.complete = false
				break
			}
			walk(filepath.Join(path, e.Name()), depth+1)
		}
	}
	walk(root.Path, 0)
	if out.diagnostic.Truncated {
		out.diagnostic.Messages = append(out.diagnostic.Messages, "Scan limit reached; select a deeper directory as another root.")
	}
	return out
}

type discoveredProjectRegistry struct {
	scan              func(context.Context, config.ProjectRoot) rootScan
	mu                sync.RWMutex
	refreshMu         sync.Mutex
	path, launch      string
	entries           map[string]projectServices
	owners            map[string]string
	diagnostics       []RootDiagnostic
	candidates        []ProjectCandidate
	revision          uint64
	selectionRevision uint64
}

func newProjectRegistry(path, launch string) *discoveredProjectRegistry {
	return &discoveredProjectRegistry{scan: scanRoot, path: path, launch: launch, entries: map[string]projectServices{}, owners: map[string]string{}}
}
func (r *discoveredProjectRegistry) Default() projectServices {
	p, _ := r.Lookup(config.ProjectID(r.launch))
	return p
}
func (r *discoveredProjectRegistry) Lookup(id string) (projectServices, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.entries[id]
	return p, ok
}
func (r *discoveredProjectRegistry) Worktree(ctx context.Context, id string) (projectServices, bool) {
	// Git remains authoritative, including worktrees created outside the browser.
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	r.mu.RLock()
	hint := r.owners[id]
	entries := make([]projectServices, 0, len(r.entries))
	for _, p := range r.entries {
		if p.info.ID == hint {
			entries = append([]projectServices{p}, entries...)
		} else {
			entries = append(entries, p)
		}
	}
	r.mu.RUnlock()
	for _, p := range entries {
		if !p.info.Available {
			continue
		}
		trees, err := coregit.ListWorktreesContext(ctx, p.info.Path)
		if err != nil {
			continue
		}
		for _, t := range trees {
			if !t.Bare && local.ID(localRepositoryID, t.Path) == id {
				return p, true
			}
		}
	}
	return projectServices{}, false
}
func (r *discoveredProjectRegistry) List() []ProjectInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := []ProjectInfo{}
	for _, p := range r.entries {
		out = append(out, p.info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}
func (r *discoveredProjectRegistry) Diagnostics() []RootDiagnostic {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]RootDiagnostic{}, r.diagnostics...)
}
func (r *discoveredProjectRegistry) Candidates() []ProjectCandidate {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]ProjectCandidate{}, r.candidates...)
}
func (r *discoveredProjectRegistry) SelectionRevision() uint64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.selectionRevision
}
func (r *discoveredProjectRegistry) selectionPath() string {
	return r.path + ".repositories.json"
}
func readProjectSelection(path string) (projectSelectionConfig, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return projectSelectionConfig{Version: 1, Selected: []string{}}, nil
	}
	if err != nil {
		return projectSelectionConfig{}, err
	}
	var cfg projectSelectionConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return projectSelectionConfig{}, err
	}
	if cfg.Version == 0 {
		cfg.Version = 1
	}
	if cfg.Version != 1 {
		return projectSelectionConfig{}, fmt.Errorf("unsupported project selection version %d", cfg.Version)
	}
	if cfg.Selected == nil {
		cfg.Selected = []string{}
	}
	return cfg, nil
}
func writeProjectSelection(path string, cfg projectSelectionConfig) error {
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0600)
}
func (r *discoveredProjectRegistry) UpdateSelection(expected uint64, ids []string) error {
	r.refreshMu.Lock()
	defer r.refreshMu.Unlock()

	r.mu.RLock()
	if expected != r.selectionRevision {
		r.mu.RUnlock()
		return errProjectSelectionRevision
	}
	allowed := make(map[string]bool, len(r.candidates))
	for _, candidate := range r.candidates {
		allowed[candidate.ID] = true
	}
	r.mu.RUnlock()

	selected := make([]string, 0, len(ids))
	seen := map[string]bool{}
	for _, id := range ids {
		if !allowed[id] {
			return fmt.Errorf("unknown discovered repository %q", id)
		}
		if !seen[id] {
			seen[id] = true
			selected = append(selected, id)
		}
	}
	sort.Strings(selected)
	return writeProjectSelection(r.selectionPath(), projectSelectionConfig{
		Version:  1,
		Revision: expected + 1,
		Selected: selected,
	})
}
func (r *discoveredProjectRegistry) Refresh(ctx context.Context) (bool, error) {
	r.refreshMu.Lock()
	defer r.refreshMu.Unlock()
	cfg, err := config.ReadProjectRoots(r.path)
	if err != nil {
		return false, err
	}
	selection, err := readProjectSelection(r.selectionPath())
	if err != nil {
		return false, err
	}
	selected := make(map[string]bool, len(selection.Selected))
	for _, id := range selection.Selected {
		selected[id] = true
	}
	ctx, cancel := context.WithTimeout(ctx, discoveryRefreshTimeout)
	defer cancel()
	scans := make([]rootScan, len(cfg.Roots))
	jobs := make(chan int)
	var wg sync.WaitGroup
	for n := 0; n < 4; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				scans[i] = r.scan(ctx, cfg.Roots[i])
			}
		}()
	}
	for i := range cfg.Roots {
		select {
		case jobs <- i:
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return false, ctx.Err()
		}
	}
	close(jobs)
	wg.Wait()
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	found := map[string]discoveredRepository{}
	for _, scan := range scans {
		for _, repo := range scan.repos {
			old := found[repo.main]
			repo.paths = append(repo.paths, old.paths...)
			found[repo.main] = repo
		}
	}
	changed := false
	err = config.WithProjectRootsContext(ctx, r.path, func(current config.ProjectRoots) error {
		if current.Revision != cfg.Revision {
			return fmt.Errorf("project roots changed during discovery; retry")
		}
		r.mu.Lock()
		defer r.mu.Unlock()
		next := map[string]projectServices{}
		owners := map[string]string{}
		diagnostics := []RootDiagnostic{}
		candidates := []ProjectCandidate{}
		for _, scan := range scans {
			diagnostics = append(diagnostics, scan.diagnostic)
		}
		for main, repo := range found {
			root, ok := config.OwningRoot(cfg.Roots, main, repo.paths...)
			if !ok {
				continue
			}
			id := config.ProjectID(main)
			candidates = append(candidates, ProjectCandidate{
				ID:        id,
				RootID:    root.ID,
				Name:      filepath.Base(main),
				Path:      main,
				Selected:  selected[id],
				Available: true,
			})
			if !selected[id] {
				continue
			}
			p, exists := r.entries[id]
			if !exists || p.state == nil {
				state, e := gitstore.Open(filepath.Join(procstore.New(main).Dir(), "local-api-state.json"))
				if e != nil {
					for i := range diagnostics {
						if diagnostics[i].RootID == root.ID {
							diagnostics[i].Messages = append(diagnostics[i].Messages, fmt.Sprintf("Metadata unavailable for %s: %v", main, e))
						}
					}
				}
				p = projectServices{daemon: client.For(main), github: ghcli.New(main), state: state}
			}
			p.info = ProjectInfo{ID: id, RootID: root.ID, Name: filepath.Base(main), Path: main, Available: p.state != nil, Launch: main == r.launch, WorkspaceID: "local", FullName: filepath.Base(main)}
			next[id] = p
			for _, path := range repo.worktrees {
				owners[local.ID(localRepositoryID, path)] = id
			}
		}
		for id, p := range r.entries {
			if _, ok := next[id]; ok || !selected[id] {
				continue
			}
			for _, scan := range scans {
				if scan.diagnostic.RootID == p.info.RootID && !scan.complete {
					p.info.Available = false
					next[id] = p
					break
				}
			}
		}
		sort.Slice(candidates, func(i, j int) bool { return candidates[i].Path < candidates[j].Path })
		before, _ := json.Marshal(r.listUnlocked())
		after := []ProjectInfo{}
		for _, p := range next {
			after = append(after, p.info)
		}
		sort.Slice(after, func(i, j int) bool { return after[i].ID < after[j].ID })
		afterBytes, _ := json.Marshal(after)
		oldDiagnostics, _ := json.Marshal(r.diagnostics)
		newDiagnostics, _ := json.Marshal(diagnostics)
		changed = string(before) != string(afterBytes) || r.revision != cfg.Revision || r.selectionRevision != selection.Revision || string(oldDiagnostics) != string(newDiagnostics)
		r.entries = next
		r.owners = owners
		r.diagnostics = diagnostics
		r.candidates = candidates
		r.revision = cfg.Revision
		r.selectionRevision = selection.Revision
		return nil
	})
	return changed, err
}
func (r *discoveredProjectRegistry) listUnlocked() []ProjectInfo {
	out := []ProjectInfo{}
	for _, p := range r.entries {
		out = append(out, p.info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
