package gardenbackend

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/connect"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/connect/fakegh"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
)

const (
	acmeURL    = "https://github.com/acme/my-app.git"
	acmeSSH    = "git@github.com:acme/my-app.git"
	forkURL    = "https://github.com/octo-dev/my-app.git"
	acmeID     = garden.RepoID(48120394)
	forkID     = garden.RepoID(51993011)
	viewerName = "octo-dev"
)

func acmeRepo(access connect.Access) connect.Repository {
	return connect.Repository{ID: acmeID, FullName: "acme/my-app", Access: access, ProjectScope: true, ProjectScopeKnown: true}
}

// harness wires a Backend to the shared GitHub fake and a temp association store.
type harness struct {
	t     *testing.T
	gh    *fakegh.Fake
	store *connect.Store
	b     *Backend
	ids   *atomic.Uint64
}

// newHarness builds a clone whose remotes origin and upstream both resolve to acme/my-app.
func newHarness(t *testing.T, access connect.Access) *harness {
	t.Helper()
	gh := &fakegh.Fake{
		ViewerLogin: viewerName,
		Remotes:     []connect.Remote{{Name: "origin", URL: acmeURL}, {Name: "upstream", URL: acmeSSH}},
		Repositories: map[string]connect.Repository{
			acmeURL: acmeRepo(access),
			acmeSSH: acmeRepo(access),
		},
	}
	store := &connect.Store{Path: filepath.Join(t.TempDir(), "garden-associations.json")}
	h := &harness{t: t, gh: gh, store: store, ids: new(atomic.Uint64)}
	h.b = h.clone(t.TempDir())
	return h
}

// clone adds another local checkout of the same Garden, sharing the store and GitHub.
func (h *harness) clone(dir string) *Backend {
	h.t.Helper()
	b, err := newBackend(dir, Deps{
		Remotes: h.gh, Resolver: h.gh,
		Ports: connect.SetupPorts{Labels: h.gh, LabelWrites: h.gh, Projects: h.gh, ProjectWrites: h.gh, OperationProjects: h.gh},
		Viewer: func(context.Context) (connect.GitHubIdentity, error) {
			return connect.GitHubIdentity{ID: "1001", Login: h.gh.ViewerLogin}, nil
		},
		Store: h.store,
		NewID: func() string { return fmt.Sprintf("t%d", h.ids.Add(1)) },
	})
	if err != nil {
		h.t.Fatal(err)
	}
	return b
}

// seedCanonicalLabels makes every canonical label exist exactly as Bonsai would create it.
func (h *harness) seedCanonicalLabels() {
	h.gh.Mu.Lock()
	defer h.gh.Mu.Unlock()
	h.gh.Labels = append(h.gh.Labels, desiredLabels()...)
}

func mustErrCode(t *testing.T, err error, want gardenapi.Code) *gardenapi.Error {
	t.Helper()
	var ae *gardenapi.Error
	if err == nil || !errors.As(err, &ae) || ae.Code != want {
		t.Fatalf("err = %v, want code %q", err, want)
	}
	return ae
}

func (h *harness) preview(b *Backend, req gardenapi.PlanRequest) gardenapi.PlanView {
	h.t.Helper()
	p, err := b.PreviewPlan(context.Background(), req)
	if err != nil {
		h.t.Fatalf("PreviewPlan: %v", err)
	}
	return p
}

func (h *harness) calls() int {
	h.gh.Mu.Lock()
	defer h.gh.Mu.Unlock()
	return h.gh.Calls
}

func (h *harness) labelCount() int {
	h.gh.Mu.Lock()
	defer h.gh.Mu.Unlock()
	return len(h.gh.Labels)
}

func (h *harness) projectCount() int {
	h.gh.Mu.Lock()
	defer h.gh.Mu.Unlock()
	return len(h.gh.Projects)
}
