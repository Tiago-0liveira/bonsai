package localapi

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agentterminal"
	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
	domain "github.com/Tiago-0liveira/bonsai/internal/git"
	githubdomain "github.com/Tiago-0liveira/bonsai/internal/git/github"
	gitstore "github.com/Tiago-0liveira/bonsai/internal/storage/git"
)

// Warm start keeps the last committed metadata of every project on disk, so a
// restarted API can bootstrap browsers with it (labeled stale) while it
// revalidates. Only an explicit metadata subset is stored: never processes,
// agents, credentials, PR descriptions, file lists or error messages.
const (
	warmStartSchema      = 1
	warmStartFile        = "snapshots.json"
	warmStartLockFile    = "snapshots.lock"
	warmStartMaxDocument = 16 << 20
	warmStartMaxProject  = 2 << 20
	warmStartMaxProjects = 256
	warmStartMaxAge      = 7 * 24 * time.Hour
	// The writer coalesces commits: it saves after warmStartIdle without one,
	// and at least every warmStartMaxDelay while they keep coming.
	warmStartIdle     = time.Second
	warmStartMaxDelay = 5 * time.Second
	// warmStartShutdownFlush bounds the final save on shutdown.
	warmStartShutdownFlush = 2 * time.Second
)

// warmDocument is the on-disk format. Projects stay raw until restore so each
// one is bounded and a bad record is skipped alone.
type warmDocument struct {
	Schema    int               `json:"schema"`
	Namespace string            `json:"namespace"`
	SavedAt   time.Time         `json:"saved_at"`
	Projects  []json.RawMessage `json:"projects"`
}

type warmProject struct {
	ID       string        `json:"id"`
	Path     string        `json:"path"`
	LastUsed time.Time     `json:"last_used"`
	Local    *warmLocal    `json:"local,omitempty"`
	Provider *warmProvider `json:"provider,omitempty"`
}

// warmLocal is the local Git inventory as of ValidatedAt: worktree, branch and
// remote identities, status counts, groups and worktree preferences.
type warmLocal struct {
	ValidatedAt time.Time                   `json:"validated_at"`
	State       domain.RepositoryState      `json:"state"`
	Metadata    map[string]worktreeMetadata `json:"metadata"`
}

// warmProvider is the provider metadata as of ValidatedAt. Worktree CI
// entries carry their own validation times.
type warmProvider struct {
	ValidatedAt     time.Time                     `json:"validated_at"`
	Repository      githubdomain.RemoteRepository `json:"repository"`
	Branches        []githubdomain.RemoteBranch   `json:"branches"`
	PullRequests    []githubdomain.PullRequest    `json:"pull_requests"`
	CatalogComplete bool                          `json:"catalog_complete"`
	Worktrees       map[string]warmWorktree       `json:"worktrees"`
}

type warmWorktree struct {
	PullRequest  *githubdomain.PullRequest `json:"pull_request,omitempty"`
	PRDiagnostic string                    `json:"pr_diagnostic,omitempty"`
	CI           warmCI                    `json:"ci"`
}

type warmCI struct {
	Status      string               `json:"status"`
	CheckedSHA  string               `json:"checked_sha,omitempty"`
	Checks      []githubdomain.Check `json:"checks"`
	ValidatedAt *time.Time           `json:"validated_at,omitempty"`
}

// warmStartNamespace separates caches by project-roots file, launch
// repository and browser security mode, so product, development and test
// servers never read each other's documents.
func warmStartNamespace(rootsPath, repoDir string, mode BrowserSecurityMode) string {
	sum := sha256.Sum256([]byte(rootsPath + "\x00" + repoDir + "\x00" + string(mode)))
	return hex.EncodeToString(sum[:8])
}

// warmExport is one projection copied out of stateSync for saving.
type warmExport struct {
	id       string
	path     string
	lastUsed time.Time
	snapshot browserSnapshot
}

// warmProjectFrom keeps the persisted subset of snapshot. It returns false
// when nothing in it was ever validated.
func warmProjectFrom(e warmExport) (warmProject, bool) {
	out := warmProject{ID: e.id, Path: e.path, LastUsed: e.lastUsed}
	snapshot := e.snapshot
	if local := snapshot.Freshness["local"]; snapshot.Local != nil && local.UpdatedAt != nil {
		out.Local = &warmLocal{ValidatedAt: *local.UpdatedAt, State: warmLocalState(*snapshot.Local), Metadata: map[string]worktreeMetadata{}}
		for id, value := range snapshot.Metadata {
			out.Local.Metadata[id] = value
		}
	}
	if out.Local == nil {
		return out, false
	}
	if provider := snapshot.Freshness["provider"]; snapshot.Remote != nil && provider.UpdatedAt != nil {
		remote := snapshot.Remote
		out.Provider = &warmProvider{
			ValidatedAt:     *provider.UpdatedAt,
			Repository:      remote.Repository,
			Branches:        append([]githubdomain.RemoteBranch{}, remote.Branches...),
			PullRequests:    make([]githubdomain.PullRequest, 0, len(remote.PullRequests)),
			CatalogComplete: remote.PRCatalogComplete,
			Worktrees:       map[string]warmWorktree{},
		}
		for _, pull := range remote.PullRequests {
			out.Provider.PullRequests = append(out.Provider.PullRequests, warmPull(pull))
		}
		for id, state := range snapshot.WorktreeState {
			worktree := warmWorktree{PRDiagnostic: state.PRDiagnostic, CI: warmCI{Status: state.CI.Status, CheckedSHA: state.CI.CheckedSHA, Checks: append([]githubdomain.Check{}, state.CI.Checks...)}}
			if state.PullRequest != nil {
				pull := warmPull(*state.PullRequest)
				worktree.PullRequest = &pull
			}
			if state.CI.Freshness.UpdatedAt != nil {
				validated := *state.CI.Freshness.UpdatedAt
				worktree.CI.ValidatedAt = &validated
			} else {
				// Never validated: the badge has nothing worth restoring.
				worktree.CI = warmCI{Status: "unknown", Checks: []githubdomain.Check{}}
			}
			out.Provider.Worktrees[id] = worktree
		}
	}
	return out, true
}

// warmPull drops the PR description; detail views read it live.
func warmPull(pull githubdomain.PullRequest) githubdomain.PullRequest {
	pull.Body = ""
	return pull
}

// warmLocalState keeps identities, counts and groups. Changed-file lists and
// status error messages are left out.
func warmLocalState(in domain.RepositoryState) domain.RepositoryState {
	out := in
	out.AffectedWorktrees = nil
	out.Branches = append([]domain.Branch{}, in.Branches...)
	out.Remotes = append([]domain.RemoteIdentity{}, in.Remotes...)
	out.Groups = append([]domain.WorktreeGroup{}, in.Groups...)
	out.Worktrees = make([]domain.Worktree, 0, len(in.Worktrees))
	for _, worktree := range in.Worktrees {
		if worktree.Status != nil {
			status := *worktree.Status
			status.Files = []domain.FileStatus{}
			status.Conflicted = []string{}
			worktree.Status = &status
		}
		if worktree.StatusError != nil {
			worktree.StatusError = &domain.StateError{Code: worktree.StatusError.Code}
		}
		out.Worktrees = append(out.Worktrees, worktree)
	}
	return out
}

// encodeWarmDocument serializes exports within the size and count limits.
// The priority project is kept first, then the most recently used; a project
// over the per-project limit is skipped whole. It returns the document and
// how many projects were skipped.
func encodeWarmDocument(namespace string, exports []warmExport, priority string, now time.Time) ([]byte, int, error) {
	sort.SliceStable(exports, func(i, j int) bool {
		if (exports[i].id == priority) != (exports[j].id == priority) {
			return exports[i].id == priority
		}
		if !exports[i].lastUsed.Equal(exports[j].lastUsed) {
			return exports[i].lastUsed.After(exports[j].lastUsed)
		}
		return exports[i].id < exports[j].id
	})
	doc := warmDocument{Schema: warmStartSchema, Namespace: namespace, SavedAt: now, Projects: []json.RawMessage{}}
	// Room for the envelope; projects fill the rest.
	budget := warmStartMaxDocument - 4096
	skipped := 0
	for _, export := range exports {
		project, ok := warmProjectFrom(export)
		if !ok {
			continue
		}
		raw, err := json.Marshal(project)
		if err != nil {
			return nil, 0, err
		}
		if len(raw) > warmStartMaxProject || len(raw)+1 > budget || len(doc.Projects) >= warmStartMaxProjects {
			skipped++
			continue
		}
		budget -= len(raw) + 1
		doc.Projects = append(doc.Projects, raw)
	}
	data, err := json.Marshal(doc)
	return data, skipped, err
}

// warmStore reads and replaces one namespace's document.
type warmStore struct {
	dir       string
	namespace string
}

func newWarmStore(base, namespace string) *warmStore {
	return &warmStore{dir: filepath.Join(base, namespace), namespace: namespace}
}

func (w *warmStore) path() string { return filepath.Join(w.dir, warmStartFile) }

// Reasons a document is not used. Missing is the normal first start.
const (
	warmReasonMissing     = "missing"
	warmReasonUnreadable  = "unreadable"
	warmReasonOversized   = "oversized"
	warmReasonCorrupt     = "corrupt"
	warmReasonUnsupported = "unsupported_version"
	warmReasonNamespace   = "namespace_mismatch"
)

// load returns the stored document, or the reason it cannot be used. It never
// reads more than the document limit.
func (w *warmStore) load() (*warmDocument, string) {
	file, err := os.Open(w.path())
	if errors.Is(err, fs.ErrNotExist) {
		return nil, warmReasonMissing
	}
	if err != nil {
		return nil, warmReasonUnreadable
	}
	defer file.Close()
	if info, err := file.Stat(); err != nil || !info.Mode().IsRegular() {
		return nil, warmReasonUnreadable
	} else if info.Size() > warmStartMaxDocument {
		return nil, warmReasonOversized
	}
	data, err := io.ReadAll(io.LimitReader(file, warmStartMaxDocument+1))
	if err != nil {
		return nil, warmReasonUnreadable
	}
	if len(data) > warmStartMaxDocument {
		return nil, warmReasonOversized
	}
	var head struct {
		Schema    int    `json:"schema"`
		Namespace string `json:"namespace"`
	}
	if err := json.Unmarshal(data, &head); err != nil {
		return nil, warmReasonCorrupt
	}
	if head.Schema != warmStartSchema {
		return nil, warmReasonUnsupported
	}
	if head.Namespace != w.namespace {
		return nil, warmReasonNamespace
	}
	var doc warmDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, warmReasonCorrupt
	}
	return &doc, ""
}

// save atomically replaces the document. Another process saving the same
// namespace holds the lock; this save is then skipped, not queued.
func (w *warmStore) save(data []byte) error {
	if err := os.MkdirAll(w.dir, 0o700); err != nil {
		return err
	}
	lock, err := procstore.TryLock(filepath.Join(w.dir, warmStartLockFile))
	if err != nil {
		return err
	}
	defer lock.Unlock()
	return gitstore.WriteJSON(w.path(), data)
}

// warmWriter saves coalesced exports in the background.
type warmWriter struct {
	store  *warmStore
	export func() ([]warmExport, string)
	now    func() time.Time
	idle   time.Duration
	limit  time.Duration

	signal chan struct{}
	stop   chan struct{}
	done   chan struct{}
	once   sync.Once

	// lastSkipped and saves report the latest outcome to tests and
	// measurements.
	mu          sync.Mutex
	lastSkipped int
	lastBytes   int
	saves       int
}

func newWarmWriter(store *warmStore, export func() ([]warmExport, string), now func() time.Time) *warmWriter {
	w := &warmWriter{
		store:  store,
		export: export,
		now:    now,
		idle:   warmStartIdle,
		limit:  warmStartMaxDelay,
		signal: make(chan struct{}, 1),
		stop:   make(chan struct{}),
		done:   make(chan struct{}),
	}
	go w.run()
	return w
}

// notify marks the projections changed. It never blocks a commit.
func (w *warmWriter) notify() {
	if w == nil {
		return
	}
	select {
	case w.signal <- struct{}{}:
	default:
	}
}

func (w *warmWriter) run() {
	defer close(w.done)
	timer := time.NewTimer(time.Hour)
	timer.Stop()
	dirty := false
	var first time.Time
	for {
		select {
		case <-w.stop:
			select {
			case <-w.signal:
				dirty = true
			default:
			}
			if dirty {
				w.flush()
			}
			return
		case <-w.signal:
			now := time.Now()
			if !dirty {
				dirty, first = true, now
			}
			wait := w.idle
			if remaining := first.Add(w.limit).Sub(now); remaining < wait {
				wait = remaining
			}
			timer.Stop()
			timer.Reset(max(wait, 0))
		case <-timer.C:
			dirty = false
			w.flush()
		}
	}
}

// flush saves the current projections. The export closure only clones them
// under the projection lock; encoding and file I/O happen here, unlocked.
func (w *warmWriter) flush() {
	exports, priority := w.export()
	data, skipped, err := encodeWarmDocument(w.store.namespace, exports, priority, w.now())
	if err == nil {
		err = w.store.save(data)
	}
	w.mu.Lock()
	w.lastSkipped, w.lastBytes = skipped, len(data)
	if err == nil {
		w.saves++
	}
	w.mu.Unlock()
	switch {
	case errors.Is(err, procstore.ErrLocked):
		log.Printf("Warm start: another Bonsai API is saving %s (locked); this save is skipped.", w.store.path())
	case err != nil:
		log.Printf("Warm start: could not save %s (write_failed); the previous copy is kept. Check that the directory is writable. %v", w.store.path(), err)
	}
}

// Close saves pending changes, waiting at most timeout.
func (w *warmWriter) Close(timeout time.Duration) {
	if w == nil {
		return
	}
	w.once.Do(func() { close(w.stop) })
	select {
	case <-w.done:
	case <-time.After(timeout):
	}
}

// loadWarmStart restores the saved projects, if the document is usable. A
// missing document is a normal cold start; any other problem is logged and
// the document is left alone until live data replaces it.
func (s *stateSync) loadWarmStart(store *warmStore) {
	doc, reason := store.load()
	switch reason {
	case "":
		s.restoreWarm(doc)
	case warmReasonMissing:
	default:
		log.Printf("Warm start: ignored %s (%s). Bonsai starts cold and replaces it once live data arrives; delete the file to reset it.", store.path(), reason)
	}
}

// restoreWarm installs the saved projects the current catalog still has at
// the same path. They enter this process's epoch at sequence 0, and every
// component with data is stale and marked as restored until revalidated.
// Removed projects never come back.
func (s *stateSync) restoreWarm(doc *warmDocument) int {
	now := s.now()
	restored := 0
	var newest time.Time
	priority := ""
	for _, raw := range doc.Projects {
		if len(raw) > warmStartMaxProject {
			continue
		}
		var record warmProject
		if err := json.Unmarshal(raw, &record); err != nil || record.Local == nil {
			continue
		}
		project, ok := s.registry.Lookup(record.ID)
		if !ok || !sameWorktreePath(project.info.Path, record.Path) || now.Sub(record.Local.ValidatedAt) > warmStartMaxAge {
			continue
		}
		snapshot := s.restoredSnapshot(project.info, record, now)
		s.mu.Lock()
		if s.projects[record.ID] != nil {
			s.mu.Unlock()
			continue
		}
		s.projects[record.ID] = &projectProjection{path: project.info.Path, snapshot: snapshot}
		s.lastUsed[record.ID] = record.LastUsed
		s.restored = append(s.restored, record.ID)
		s.mu.Unlock()
		if record.LastUsed.After(newest) {
			newest, priority = record.LastUsed, record.ID
		}
		s.seedProvider(snapshot)
		restored++
	}
	if priority != "" {
		s.mu.Lock()
		if s.priorityProject == "" {
			s.priorityProject = priority
		}
		s.mu.Unlock()
	}
	return restored
}

func (s *stateSync) restoredSnapshot(info ProjectInfo, record warmProject, now time.Time) browserSnapshot {
	disk := func(at time.Time) browserFreshness {
		return browserFreshness{State: "stale", UpdatedAt: &at, Source: freshnessSourceDisk}
	}
	local := record.Local.State
	local.ID = info.ID
	for i := range local.Worktrees {
		local.Worktrees[i].RepositoryID = info.ID
	}
	metadata := record.Local.Metadata
	if metadata == nil {
		metadata = map[string]worktreeMetadata{}
	}
	snapshot := browserSnapshot{
		Epoch:      s.epoch,
		Repository: descriptorFromLocal(info, &local),
		Online:     info.Available,
		Local:      &local,
		Metadata:   metadata,
		Processes:  []browserProcessSummary{},
		Agents:     []agentterminal.Summary{},
		Freshness: map[string]browserFreshness{
			"agents":    {State: "ready"},
			"local":     disk(record.Local.ValidatedAt),
			"processes": {State: "loading"},
			"provider":  {State: "loading"},
		},
		WorktreeState: map[string]browserWorktreeState{},
	}
	if provider := record.Provider; provider != nil && now.Sub(provider.ValidatedAt) <= warmStartMaxAge {
		snapshot.Remote = &browserRemoteSnapshot{
			Repository:        provider.Repository,
			Branches:          append([]githubdomain.RemoteBranch{}, provider.Branches...),
			PullRequests:      append([]githubdomain.PullRequest{}, provider.PullRequests...),
			UpdatedAt:         provider.ValidatedAt,
			PRCatalogComplete: provider.CatalogComplete,
		}
		snapshot.Freshness["provider"] = disk(provider.ValidatedAt)
		if provider.Repository.FullName != "" {
			snapshot.Repository.FullName = provider.Repository.FullName
		}
		present := map[string]bool{}
		for _, worktree := range local.Worktrees {
			present[worktree.ID] = true
		}
		for id, saved := range provider.Worktrees {
			if !present[id] {
				continue
			}
			state := browserWorktreeState{
				PullRequest:  saved.PullRequest,
				PRDiagnostic: saved.PRDiagnostic,
				CI:           browserCIState{Status: "unknown", Checks: []githubdomain.Check{}, Freshness: browserFreshness{State: "loading"}},
			}
			if validated := saved.CI.ValidatedAt; validated != nil && now.Sub(*validated) <= warmStartMaxAge {
				state.CI = browserCIState{Status: saved.CI.Status, CheckedSHA: saved.CI.CheckedSHA, Checks: append([]githubdomain.Check{}, saved.CI.Checks...), Freshness: disk(*validated)}
			}
			snapshot.WorktreeState[id] = state
		}
	}
	deriveBranchCandidates(&snapshot)
	return snapshot
}

// seedProvider puts a restored snapshot's provider data in the provider
// cache, expired, so revalidation keeps it on screen until live reads
// replace it instead of starting from empty.
func (s *stateSync) seedProvider(snapshot browserSnapshot) {
	if snapshot.Remote == nil || snapshot.Local == nil {
		return
	}
	identity, ok := preferredRemote(snapshot.Local.Remotes)
	if !ok || identity.FullName == "" {
		return
	}
	s.providers.seedRepository(identity.FullName, snapshot.Remote)
	for _, worktree := range snapshot.Local.Worktrees {
		state, ok := snapshot.WorktreeState[worktree.ID]
		if !ok || state.CI.Freshness.UpdatedAt == nil {
			continue
		}
		repository, sha := checkTargetFor(snapshot.Local, worktree, state.PullRequest, identity)
		if repository != "" && sha != "" && sha == state.CI.CheckedSHA {
			s.providers.seedChecks(repository, sha, state.CI.Checks, *state.CI.Freshness.UpdatedAt)
		}
	}
}

// revalidateRestored queues every refresh of the restored projects, the most
// recently used first. Nothing here fetches or pulls: these are the same
// reads a browser's first connection triggers.
func (s *stateSync) revalidateRestored() {
	s.mu.Lock()
	ids := s.restored
	s.restored = nil
	priority := s.priorityProject
	s.mu.Unlock()
	sort.SliceStable(ids, func(i, j int) bool { return ids[i] == priority && ids[j] != priority })
	for _, id := range ids {
		s.Queue(id, refreshAll, false)
	}
}

// dropRestoredProviderForOtherRemote forgets restored provider data once the
// live inventory shows the project now points at another repository.
func dropRestoredProviderForOtherRemote(snapshot *browserSnapshot) {
	if snapshot.Remote == nil || snapshot.Local == nil || snapshot.Freshness["provider"].Source != freshnessSourceDisk {
		return
	}
	identity, _ := preferredRemote(snapshot.Local.Remotes)
	if strings.EqualFold(identity.FullName, snapshot.Remote.Repository.FullName) {
		return
	}
	snapshot.Remote = nil
	snapshot.WorktreeState = map[string]browserWorktreeState{}
	snapshot.Freshness["provider"] = browserFreshness{State: "loading"}
}
