package git

import (
	"context"
	"encoding/json"
	"fmt"
	domain "github.com/Tiago-0liveira/bonsai/internal/git"
	gh "github.com/Tiago-0liveira/bonsai/internal/git/github"
	"github.com/Tiago-0liveira/bonsai/internal/server/events"
	"github.com/Tiago-0liveira/bonsai/internal/server/githubapp"
	store "github.com/Tiago-0liveira/bonsai/internal/storage/git"
	"sync"
	"time"
)

type Service struct {
	connections  sync.WaitGroup
	readCache    map[string]cachedRead
	Store        *store.Store
	Events       *events.Hub
	Auth         *githubapp.Manager
	GitHub       gh.GitHubService
	Repositories map[string]Repository
	Origin       string
	mu           sync.Mutex
	devices      map[string]*connection
	cacheMu      sync.Mutex
}
type Snapshot struct {
	Repository Repository              `json:"repository"`
	Local      *domain.RepositoryState `json:"local"`
	Remote     *RemoteSnapshot         `json:"remote"`
	Online     bool                    `json:"online"`
	Sequence   uint64                  `json:"sequence"`
	Metadata   map[string]Metadata     `json:"metadata"`
}
type RemoteSnapshot struct {
	Repository   gh.RemoteRepository `json:"repository"`
	Branches     []gh.RemoteBranch   `json:"branches"`
	PullRequests []gh.PullRequest    `json:"pull_requests"`
	UpdatedAt    time.Time           `json:"updated_at"`
}
type Metadata struct {
	WorktreeID        string `json:"worktree_id"`
	RepositoryID      string `json:"repository_id"`
	MergeTargetBranch string `json:"merge_target_branch"`
	StackPreference   string `json:"stack_preference"`
}

func New(st *store.Store, auth *githubapp.Manager, github gh.GitHubService, repos []Repository, origin string) (*Service, error) {
	s := &Service{Store: st, Events: events.New(st), Auth: auth, GitHub: github, Repositories: map[string]Repository{}, Origin: origin, devices: map[string]*connection{}}
	for _, r := range repos {
		if r.ID == "" || s.Repositories[r.ID].ID != "" || r.WorkspaceID == "" {
			return nil, domain.ErrInvalid
		}
		s.Repositories[r.ID] = r
	}
	return s, nil
}
func (s *Service) publish(repo, source, kind, entity, id string, payload any) error {
	r := s.Repositories[repo]
	if r.ID == "" {
		return domain.ErrNotFound
	}
	raw, e := json.Marshal(payload)
	if e != nil {
		return e
	}
	_, e = s.Events.Publish(events.Event{ID: id, WorkspaceID: r.WorkspaceID, ProjectID: repo, Source: source, Type: kind, EntityID: entity, Payload: raw})
	return e
}
func (s *Service) snapshot(repo string) Snapshot {
	out := Snapshot{Repository: s.Repositories[repo], Metadata: map[string]Metadata{}}
	// Capture cursor before reading state. Racing events are safely replayed.
	out.Sequence = s.Events.Cursor()
	s.Store.View(func(d store.Data) error {
		if v, ok := store.Get[domain.RepositoryState](d, "local_snapshots", repo); ok {
			out.Local = &v
		}
		if v, ok := store.Get[RemoteSnapshot](d, "remote_snapshots", repo); ok {
			out.Remote = &v
			out.Repository.DefaultBranch = v.Repository.DefaultBranch
		}
		for id := range d["worktree_metadata"] {
			v, ok := store.Get[Metadata](d, "worktree_metadata", id)
			if ok && v.RepositoryID == repo {
				out.Metadata[id] = v
			}
		}
		return nil
	})
	s.mu.Lock()
	out.Online = s.devices[repo] != nil
	s.mu.Unlock()
	return out
}
func (s *Service) worktreeRepo(id string) (string, error) {
	var repo string
	s.Store.View(func(d store.Data) error {
		for key := range d["local_snapshots"] {
			v, ok := store.Get[domain.RepositoryState](d, "local_snapshots", key)
			if ok {
				for _, w := range v.Worktrees {
					if w.ID == id {
						repo = key
					}
				}
			}
		}
		return nil
	})
	if repo == "" {
		return "", domain.ErrNotFound
	}
	return repo, nil
}
func (s *Service) Reconcile(ctx context.Context, repo string) error {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()
	s.readCache = nil
	r, ok := s.Repositories[repo]
	if !ok {
		return domain.ErrNotFound
	}
	if s.GitHub == nil {
		return nil
	}
	remote, e := s.GitHub.Repository(ctx, r.FullName)
	if e != nil {
		return e
	}
	if remote.ID != r.GitHubRepositoryID {
		return domain.ErrForbidden
	}
	branches, e := s.GitHub.Branches(ctx, r.FullName)
	if e != nil {
		return e
	}
	prs, e := s.GitHub.PullRequests(ctx, r.FullName, gh.PRFilter{State: "all"})
	if e != nil {
		return e
	}
	next := RemoteSnapshot{Repository: remote, Branches: branches, PullRequests: prs, UpdatedAt: time.Now().UTC()}
	changed := true
	e = s.Store.Update(func(d store.Data) error {
		old, ok := store.Get[RemoteSnapshot](d, "remote_snapshots", repo)
		a := old
		b := next
		a.UpdatedAt = time.Time{}
		b.UpdatedAt = time.Time{}
		ba, _ := json.Marshal(a)
		bb, _ := json.Marshal(b)
		changed = !ok || string(ba) != string(bb)
		return store.Put(d, "remote_snapshots", repo, next)
	})
	if e != nil {
		return e
	}
	if changed {
		return s.publish(repo, "github", "repository.updated", repo, "", next)
	}
	return nil
}
func (s *Service) Run(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		for id := range s.Repositories {
			if ctx.Err() != nil {
				return
			}
			c, cancel := context.WithTimeout(ctx, 2*time.Minute)
			_ = s.Reconcile(c, id)
			cancel()
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func (s *Service) setLocal(repo string, snapshot domain.RepositoryState) error {
	if snapshot.ID != repo {
		return domain.ErrForbidden
	}
	seen := map[string]bool{}
	for _, w := range snapshot.Worktrees {
		if w.RepositoryID != repo || w.ID == "" || seen[w.ID] {
			return domain.ErrInvalid
		}
		seen[w.ID] = true
	}
	var old domain.RepositoryState
	changed := false
	e := s.Store.Update(func(d store.Data) error {
		old, _ = store.Get[domain.RepositoryState](d, "local_snapshots", repo)
		a, _ := json.Marshal(old)
		b, _ := json.Marshal(snapshot)
		changed = string(a) != string(b)
		return store.Put(d, "local_snapshots", repo, snapshot)
	})
	if e != nil || !changed {
		return e
	}
	// Publish canonical snapshots and entity events. Local snapshots never change
	// the independently persisted GitHub remote branch SHAs.
	if e = s.publish(repo, "daemon", "repository.updated", repo, "", snapshot); e != nil {
		return e
	}
	before := map[string]domain.Worktree{}
	for _, w := range old.Worktrees {
		before[w.ID] = w
	}
	for _, w := range snapshot.Worktrees {
		if previous, ok := before[w.ID]; ok {
			a, _ := json.Marshal(previous)
			b, _ := json.Marshal(w)
			if string(a) == string(b) {
				delete(before, w.ID)
				continue
			}
		}
		kind := "worktree.updated"
		if _, ok := before[w.ID]; !ok {
			kind = "worktree.created"
		}
		if e = s.publish(repo, "daemon", kind, w.ID, "", w); e != nil {
			return e
		}
		if e = s.publish(repo, "daemon", "working_tree.updated", w.ID, "", w.Status); e != nil {
			return e
		}
		delete(before, w.ID)
	}
	for id := range before {
		if e = s.publish(repo, "daemon", "worktree.removed", id, "", map[string]string{"id": id}); e != nil {
			return e
		}
	}
	return nil
}
func (s *Service) String() string {
	return fmt.Sprintf("Git backend (%d repositories)", len(s.Repositories))
}
