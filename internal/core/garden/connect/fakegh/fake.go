// Package fakegh provides deterministic GitHub setup ports for connection tests.
package fakegh

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/connect"
)

// Fake implements remote resolution and setup observations with injected failures.
type Fake struct {
	Mu               sync.Mutex
	Remotes          []connect.Remote
	Repositories     map[string]connect.Repository
	Labels           []connect.Label
	Projects         []garden.Project
	ViewerLogin      string
	FailAt           int
	Calls            int
	Failure          error
	FailAfterCreate  bool
	PermissionDenied bool
	MissingScope     bool
	Offline          bool
	RateLimited      bool
}

// Resolver and remote listing adapt the fake's seeded remote candidates.
func (f *Fake) ListRemotes(context.Context) ([]connect.Remote, error) {
	return append([]connect.Remote(nil), f.Remotes...), nil
}
func (f *Fake) ResolveRepository(_ context.Context, r connect.Remote) (connect.Repository, error) {
	if f.Offline {
		return connect.Repository{}, connect.ErrOffline
	}
	if f.RateLimited {
		return connect.Repository{}, connect.ErrRateLimited
	}
	if f.PermissionDenied {
		return connect.Repository{}, connect.ErrPermissionDenied
	}
	v, ok := f.Repositories[r.URL]
	if !ok {
		return connect.Repository{}, connect.ErrNoGitHubRemote
	}
	return v, nil
}
func (f *Fake) tick() error {
	if f.Offline {
		return connect.ErrOffline
	}
	if f.RateLimited {
		return connect.ErrRateLimited
	}
	if f.PermissionDenied {
		return connect.ErrPermissionDenied
	}
	if f.MissingScope {
		return connect.ErrScopeMissing{Scope: "project"}
	}
	f.Calls++
	if f.FailAt > 0 && f.Calls == f.FailAt {
		if f.Failure != nil {
			return f.Failure
		}
		return errors.New("fake step failure")
	}
	return nil
}
func (f *Fake) ListLabels(context.Context, garden.RepoID) ([]connect.Label, error) {
	f.Mu.Lock()
	defer f.Mu.Unlock()
	return append([]connect.Label(nil), f.Labels...), nil
}
func (f *Fake) CreateLabel(_ context.Context, _ garden.RepoID, l connect.Label) error {
	f.Mu.Lock()
	defer f.Mu.Unlock()
	if e := f.tick(); e != nil {
		return e
	}
	for _, old := range f.Labels {
		if old.Name == l.Name {
			return nil
		}
	}
	f.Labels = append(f.Labels, l)
	return nil
}
func (f *Fake) ListProjects(context.Context, garden.RepoID) ([]garden.Project, error) {
	f.Mu.Lock()
	defer f.Mu.Unlock()
	out := append([]garden.Project(nil), f.Projects...)
	for i := range out {
		out[i].Fields = append([]garden.ProjectField(nil), out[i].Fields...)
	}
	return out, nil
}
func (f *Fake) EnsureProject(context.Context, garden.RepoID, string) (garden.Project, error) {
	return garden.Project{}, fmt.Errorf("use operation-aware project create")
}
func (f *Fake) EnsureProjectWithMarker(_ context.Context, _ garden.RepoID, name, marker string) (garden.Project, error) {
	f.Mu.Lock()
	defer f.Mu.Unlock()
	if e := f.tick(); e != nil {
		return garden.Project{}, e
	}
	for _, p := range f.Projects {
		if p.Description == marker {
			return p, nil
		}
	}
	p := garden.Project{ID: fmt.Sprintf("project-%d", len(f.Projects)+1), Name: name, Description: marker, CreatorLogin: f.ViewerLogin}
	f.Projects = append(f.Projects, p)
	if f.FailAfterCreate {
		return p, errors.New("injected lost local acknowledgement")
	}
	return p, nil
}
func (f *Fake) EnsureField(_ context.Context, projectID string, w garden.ProjectField) (garden.ProjectField, error) {
	f.Mu.Lock()
	defer f.Mu.Unlock()
	if e := f.tick(); e != nil {
		return garden.ProjectField{}, e
	}
	for i := range f.Projects {
		if f.Projects[i].ID == projectID {
			for _, field := range f.Projects[i].Fields {
				if field.Name == w.Name {
					return field, nil
				}
			}
			f.Projects[i].Fields = append(f.Projects[i].Fields, w)
			return w, nil
		}
	}
	return garden.ProjectField{}, connect.ErrProjectNotFound
}
