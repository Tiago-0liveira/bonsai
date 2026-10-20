package connect_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/connect"
)

// ============================================================================
// Helper Constructors and Test Doubles
// ============================================================================

const fakeSecretToken = "ghp_FAKETOKEN1234567890abcdef"

func newTestStore(t *testing.T) (*connect.Store, string) {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "bonsai", "garden-associations.json")
	return &connect.Store{Path: p}, dir
}

type testRemoteLister []connect.Remote

func (r testRemoteLister) ListRemotes(context.Context) ([]connect.Remote, error) {
	return append([]connect.Remote(nil), r...), nil
}

type testRepoResolver struct {
	mu          sync.Mutex
	repos       map[string]connect.Repository
	byID        map[garden.RepoID]connect.Repository
	errByRemote map[string]error
	defaultErr  error
	calls       int
}

func newTestRepoResolver() *testRepoResolver {
	return &testRepoResolver{
		repos:       make(map[string]connect.Repository),
		byID:        make(map[garden.RepoID]connect.Repository),
		errByRemote: make(map[string]error),
	}
}

func (r *testRepoResolver) ResolveRepository(_ context.Context, rem connect.Remote) (connect.Repository, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	if err, ok := r.errByRemote[rem.URL]; ok {
		return connect.Repository{}, err
	}
	if r.defaultErr != nil {
		return connect.Repository{}, r.defaultErr
	}
	repo, ok := r.repos[rem.URL]
	if !ok {
		return connect.Repository{}, connect.ErrNoGitHubRemote
	}
	return repo, nil
}

func (r *testRepoResolver) ResolveRepositoryID(_ context.Context, id garden.RepoID, fullName string) (connect.Repository, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if repo, ok := r.byID[id]; ok {
		if !strings.EqualFold(repo.FullName, fullName) {
			return connect.Repository{}, connect.ErrRepositoryIdentityChanged
		}
		return repo, nil
	}
	return connect.Repository{}, connect.ErrNoGitHubRemote
}

type adversarialPorts struct {
	mu           sync.Mutex
	labels       []connect.Label
	projects     []garden.Project
	createCalls  int
	failAtStep   int
	stepFailure  error
	failKind     error
	labelWrites  int
	projWrites   int
	fieldWrites  int
	beforeHook   func(stepName string)
	missingScope bool
}

func newAdversarialPorts() *adversarialPorts {
	return &adversarialPorts{
		labels:   make([]connect.Label, 0),
		projects: make([]garden.Project, 0),
	}
}

func (p *adversarialPorts) ports() connect.SetupPorts {
	return connect.SetupPorts{
		Labels:            p,
		LabelWrites:       p,
		Projects:          p,
		ProjectWrites:     p,
		OperationProjects: p,
		ViewerLogin:       "viewer",
	}
}

func (p *adversarialPorts) tick(action string) error {
	p.createCalls++
	if p.beforeHook != nil {
		p.beforeHook(action)
	}
	if p.missingScope && (action == "project" || action == "field") {
		return connect.ErrScopeMissing{Scope: "project"}
	}
	if p.failKind != nil {
		return p.failKind
	}
	if p.failAtStep > 0 && p.createCalls == p.failAtStep {
		if p.stepFailure != nil {
			return p.stepFailure
		}
		return errors.New("injected step failure")
	}
	return nil
}

func (p *adversarialPorts) ListLabels(context.Context, garden.RepoID) ([]connect.Label, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]connect.Label(nil), p.labels...), nil
}

func (p *adversarialPorts) CreateLabel(_ context.Context, _ garden.RepoID, l connect.Label) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.tick("label"); err != nil {
		return err
	}
	p.labelWrites++
	for _, old := range p.labels {
		if strings.EqualFold(old.Name, l.Name) {
			return nil
		}
	}
	p.labels = append(p.labels, l)
	return nil
}

func (p *adversarialPorts) ListProjects(context.Context, garden.RepoID) ([]garden.Project, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := append([]garden.Project(nil), p.projects...)
	for i := range out {
		out[i].Fields = append([]garden.ProjectField(nil), out[i].Fields...)
	}
	return out, nil
}

func (p *adversarialPorts) EnsureProject(context.Context, garden.RepoID, string) (garden.Project, error) {
	return garden.Project{}, errors.New("EnsureProject without marker called unexpectedly")
}

func (p *adversarialPorts) EnsureProjectWithMarker(_ context.Context, _ garden.RepoID, name, marker string) (garden.Project, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.tick("project"); err != nil {
		return garden.Project{}, err
	}
	p.projWrites++
	for _, pr := range p.projects {
		if pr.Description == marker {
			return pr, nil
		}
	}
	pr := garden.Project{
		ID:           fmt.Sprintf("proj-%d", len(p.projects)+1),
		Name:         name,
		Description:  marker,
		CreatorLogin: "viewer",
	}
	p.projects = append(p.projects, pr)
	return pr, nil
}

func (p *adversarialPorts) EnsureField(_ context.Context, projectID string, f garden.ProjectField) (garden.ProjectField, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.tick("field"); err != nil {
		return garden.ProjectField{}, err
	}
	p.fieldWrites++
	for i := range p.projects {
		if p.projects[i].ID == projectID {
			for _, existing := range p.projects[i].Fields {
				if strings.EqualFold(existing.Name, f.Name) {
					return existing, nil
				}
			}
			p.projects[i].Fields = append(p.projects[i].Fields, f)
			return f, nil
		}
	}
	return garden.ProjectField{}, connect.ErrProjectNotFound
}

// ============================================================================
// 1. Target Resolution
// ============================================================================

func TestTargetResolution_Adversarial(t *testing.T) {
	t.Parallel()

	t.Run("ZeroRemotes", func(t *testing.T) {
		t.Parallel()
		remotes := testRemoteLister{}
		resolver := newTestRepoResolver()
		_, _, err := connect.ResolveTarget(context.Background(), remotes, resolver, nil)
		if !errors.Is(err, connect.ErrNoGitHubRemote) {
			t.Fatalf("expected ErrNoGitHubRemote for zero remotes, got %v", err)
		}
	})

	t.Run("NonGitHubRemotes_NoTokenLeak", func(t *testing.T) {
		t.Parallel()
		nonGitHubURLs := []struct {
			name string
			url  string
		}{
			{"gitlab", "https://oauth2:" + fakeSecretToken + "@gitlab.com/group/repo.git"},
			{"bitbucket", "git@bitbucket.org:owner/repo.git"},
			{"file_proto", "file:///local/path/repo"},
			{"ssh_scp", "git@custom-git.internal:team/project.git"},
			{"weird_port", "https://gitlab.example.com:8443/custom/repo.git"},
			{"trailing_git", "https://source.notgithub.com/repo.git"},
			{"uppercase_host", "HTTPS://GITLAB.COM/USER/REPO.GIT"},
			{"userinfo_token", "https://" + fakeSecretToken + "@notgithub.org/secret/repo"},
		}

		remotes := make(testRemoteLister, len(nonGitHubURLs))
		resolver := newTestRepoResolver()
		for i, u := range nonGitHubURLs {
			remotes[i] = connect.Remote{Name: u.name, URL: u.url}
			resolver.errByRemote[u.url] = fmt.Errorf("transport failure containing %s", fakeSecretToken)
		}

		_, skipped, err := connect.ResolveTarget(context.Background(), remotes, resolver, nil)
		if err == nil {
			t.Fatal("expected error resolving non-GitHub remotes, got nil")
		}

		// Security invariant: secret token must NEVER leak in error, UserMessage, or skipped strings.
		errMsg := err.Error()
		userMsg := connect.UserMessage(err)
		if strings.Contains(errMsg, fakeSecretToken) {
			t.Fatalf("error string leaked secret token: %s", errMsg)
		}
		if strings.Contains(userMsg, fakeSecretToken) {
			t.Fatalf("UserMessage leaked secret token: %s", userMsg)
		}
		for _, s := range skipped {
			if strings.Contains(s, fakeSecretToken) {
				t.Fatalf("skipped entry leaked secret token: %s", s)
			}
		}
	})

	t.Run("TwoRemotes_SameRepo_NotAmbiguous", func(t *testing.T) {
		t.Parallel()
		remotes := testRemoteLister{
			{Name: "origin", URL: "https://github.com/org/repo.git"},
			{Name: "backup", URL: "git@github.com:org/repo.git"},
		}
		resolver := newTestRepoResolver()
		sameRepo := connect.Repository{ID: 42, FullName: "org/repo"}
		resolver.repos["https://github.com/org/repo.git"] = sameRepo
		resolver.repos["git@github.com:org/repo.git"] = sameRepo

		// CONTRACT: "Garden identity is constructed only from the numeric repository ID."
		// Two local remotes pointing to the exact same numeric GitHub repo ID have the same GardenID
		// and must not be rejected as ambiguous.
		target, _, err := connect.ResolveTarget(context.Background(), remotes, resolver, nil)
		if err != nil {
			t.Errorf("CONTRACT: \"Garden identity is constructed only from the numeric repository ID.\" - two remotes pointing to same repo ID should not be ambiguous: %v", err)
		} else if target.GardenID.Repo != 42 {
			t.Fatalf("expected GardenID 42, got %v", target.GardenID)
		}
		if len(target.RemoteAliases) != 2 || target.RemoteAliases[0] != "origin" || target.RemoteAliases[1] != "backup" {
			t.Fatalf("same-repository remote aliases were lost: %#v", target.RemoteAliases)
		}
		if target.RemoteName != "origin" {
			t.Fatalf("unexpected selected canonical remote: %q", target.RemoteName)
		}
		// Candidate aliases retain all equivalent remote names for the chooser.
		_, _, err = connect.ResolveTarget(context.Background(), remotes, resolver, &connect.Candidate{Repository: sameRepo})
		if err != nil {
			t.Fatalf("choosing by numeric ID failed: %v", err)
		}
	})

	t.Run("OriginUpstreamForkPair_Ambiguous", func(t *testing.T) {
		t.Parallel()
		remotes := testRemoteLister{
			{Name: "origin", URL: "https://github.com/user/fork.git"},
		}
		resolver := newTestRepoResolver()
		forkRepo := connect.Repository{ID: 10, FullName: "user/fork", Fork: true, ParentID: 20, ParentFullName: "upstream/canonical"}
		resolver.repos["https://github.com/user/fork.git"] = forkRepo
		resolver.byID[20] = connect.Repository{ID: 20, FullName: "upstream/canonical"}

		_, _, err := connect.ResolveTarget(context.Background(), remotes, resolver, nil)
		var amb *connect.AmbiguousError
		if !errors.As(err, &amb) {
			t.Fatalf("expected AmbiguousError for fork/upstream pair, got %v", err)
		}
		if len(amb.Candidates) != 2 {
			t.Fatalf("expected 2 candidates, got %d", len(amb.Candidates))
		}
	})

	t.Run("CandidateChoice_Validation", func(t *testing.T) {
		t.Parallel()
		remotes := testRemoteLister{
			{Name: "origin", URL: "https://github.com/user/fork.git"},
		}
		resolver := newTestRepoResolver()
		forkRepo := connect.Repository{ID: 10, FullName: "user/fork", Fork: true, ParentID: 20, ParentFullName: "upstream/canonical"}
		resolver.repos["https://github.com/user/fork.git"] = forkRepo
		resolver.byID[20] = connect.Repository{ID: 20, FullName: "upstream/canonical"}

		// Non-existent candidate must be rejected.
		bogus := &connect.Candidate{
			Remote:     connect.Remote{Name: "nonexistent", URL: "none"},
			Repository: connect.Repository{ID: 999},
		}
		if _, _, err := connect.ResolveTarget(context.Background(), remotes, resolver, bogus); err == nil {
			t.Fatal("expected rejection of non-candidate choice, got nil")
		}

		// Valid choice by fork candidate.
		chosenFork := &connect.Candidate{
			Remote:     connect.Remote{Name: "origin", URL: "https://github.com/user/fork.git"},
			Repository: forkRepo,
			Upstream:   false,
		}
		target, _, err := connect.ResolveTarget(context.Background(), remotes, resolver, chosenFork)
		if err != nil || target.GardenID.Repo != 10 {
			t.Fatalf("expected fork target ID 10, got %v, err: %v", target.GardenID, err)
		}

		// Valid choice by upstream candidate.
		chosenUpstream := &connect.Candidate{
			Remote:     connect.Remote{Name: "origin", URL: "https://github.com/user/fork.git"},
			Repository: connect.Repository{ID: 20, FullName: "upstream/canonical"},
			Upstream:   true,
		}
		targetUpstream, _, err := connect.ResolveTarget(context.Background(), remotes, resolver, chosenUpstream)
		if err != nil || targetUpstream.GardenID.Repo != 20 {
			t.Fatalf("expected upstream target ID 20, got %v, err: %v", targetUpstream.GardenID, err)
		}
		byExactName := &connect.Candidate{Repository: connect.Repository{FullName: "UPSTREAM/CANONICAL"}}
		if _, _, err := connect.ResolveTarget(context.Background(), remotes, resolver, byExactName); err != nil {
			t.Fatalf("case-insensitive exact full-name choice failed: %v", err)
		}
		partialName := &connect.Candidate{Repository: connect.Repository{FullName: "upstream"}}
		if _, _, err := connect.ResolveTarget(context.Background(), remotes, resolver, partialName); err == nil {
			t.Fatal("partial repository name was accepted")
		}

		// CONTRACT: "Multiple GitHub remotes or fork/upstream choices return candidates that the caller must select."
		// Choosing candidate by ID only or by Name only.
		partialByName := &connect.Candidate{Remote: connect.Remote{Name: "origin"}}
		if _, _, err := connect.ResolveTarget(context.Background(), remotes, resolver, partialByName); err != nil {
			t.Logf("Candidate selection by name only failed: %v", err)
		}
	})

	t.Run("GardenID_DerivedFromNumericRepoID", func(t *testing.T) {
		t.Parallel()
		resolver := newTestRepoResolver()

		// Renamed repo: same ID 100, new name.
		repoInitial := connect.Repository{ID: 100, FullName: "alice/project"}
		repoRenamed := connect.Repository{ID: 100, FullName: "bob/transferred-project"}
		resolver.repos["https://github.com/alice/project.git"] = repoInitial
		resolver.repos["https://github.com/bob/transferred-project.git"] = repoRenamed

		target1, _, err1 := connect.ResolveTarget(context.Background(), testRemoteLister{{Name: "origin", URL: "https://github.com/alice/project.git"}}, resolver, nil)
		target2, _, err2 := connect.ResolveTarget(context.Background(), testRemoteLister{{Name: "origin", URL: "https://github.com/bob/transferred-project.git"}}, resolver, nil)
		if err1 != nil || err2 != nil {
			t.Fatalf("unexpected error resolving: %v, %v", err1, err2)
		}

		// CONTRACT: "Garden identity is constructed only from the numeric repository ID."
		// CONTRACT: "GardenID derives only from the remote repository ID."
		if target1.GardenID != target2.GardenID {
			t.Fatalf("expected same GardenID for renamed repo (ID 100), got %v vs %v", target1.GardenID, target2.GardenID)
		}

		// Different repo reusing old name: ID 200.
		repoReusedName := connect.Repository{ID: 200, FullName: "alice/project"}
		resolver.repos["https://github.com/alice/project-reused.git"] = repoReusedName
		target3, _, err3 := connect.ResolveTarget(context.Background(), testRemoteLister{{Name: "origin", URL: "https://github.com/alice/project-reused.git"}}, resolver, nil)
		if err3 != nil {
			t.Fatalf("unexpected error: %v", err3)
		}
		if target3.GardenID == target1.GardenID {
			t.Fatalf("expected different GardenID for repo with ID 200, but got same as ID 100: %v", target3.GardenID)
		}
	})
}

// ============================================================================
// 2. Access
// ============================================================================

func TestAccess_Adversarial(t *testing.T) {
	t.Parallel()

	t.Run("Matrix_SetupAndReader", func(t *testing.T) {
		t.Parallel()
		ports := newAdversarialPorts()

		tests := []struct {
			access        connect.Access
			projectScope  bool
			wantReaderOK  bool
			wantReaderErr error
			wantSetupOK   bool
			wantSetupErr  error
		}{
			{connect.AccessNone, false, false, connect.ErrAuthRequired, false, connect.ErrReadOnly},
			{connect.AccessNone, true, false, connect.ErrAuthRequired, false, connect.ErrReadOnly},
			{connect.AccessRead, false, true, nil, false, connect.ErrReadOnly},
			{connect.AccessRead, true, true, nil, false, connect.ErrReadOnly},
			{connect.AccessWrite, false, true, nil, true, nil},
			{connect.AccessWrite, true, true, nil, true, nil},
			{connect.AccessAdmin, false, true, nil, true, nil},
			{connect.AccessAdmin, true, true, nil, true, nil},
		}

		for _, tc := range tests {
			name := fmt.Sprintf("access_%d_scope_%v", tc.access, tc.projectScope)
			t.Run(name, func(t *testing.T) {
				conn := connect.Connection{
					Access:        tc.access,
					ProjectScope:  tc.projectScope,
					ProjectReader: ports,
					ProjectWriter: ports,
				}

				r, err := conn.Reader()
				if tc.wantReaderOK {
					if err != nil || r == nil {
						t.Errorf("expected Reader ok, got err %v", err)
					}
				} else {
					if !errors.Is(err, tc.wantReaderErr) {
						t.Errorf("expected Reader err %v, got %v", tc.wantReaderErr, err)
					}
				}

				w, err := conn.Setup()
				if tc.wantSetupOK {
					if err != nil || w == nil {
						t.Errorf("expected Setup ok, got err %v", err)
					}
				} else {
					if !errors.Is(err, tc.wantSetupErr) && !errors.As(err, new(connect.ErrScopeMissing)) {
						t.Errorf("expected Setup err %v, got %v", tc.wantSetupErr, err)
					}
				}
			})
		}
	})

	t.Run("ReadOnly_MutatingEntryPoints_ZeroWrites", func(t *testing.T) {
		t.Parallel()
		store, _ := newTestStore(t)
		ports := newAdversarialPorts()
		plan, err := connect.BuildPlan(connect.Observed{}, connect.Create, connect.Desired{
			Labels:      []connect.Label{{Name: "bonsai:managed", Description: "managed"}},
			ProjectName: "TestProject",
		})
		if err != nil {
			t.Fatal(err)
		}

		// Apply with AccessRead must return ErrReadOnly and perform ZERO writes.
		_, err = connect.Apply(context.Background(), store, "local-1", 100, connect.AccessRead, "op-ro-1", plan, ports.ports())
		if !errors.Is(err, connect.ErrReadOnly) {
			t.Fatalf("expected ErrReadOnly from Apply with read-only access, got %v", err)
		}
		if ports.labelWrites != 0 || ports.projWrites != 0 || ports.fieldWrites != 0 {
			t.Fatalf("fake recorded mutating writes during read-only Apply: %d labels, %d projs, %d fields",
				ports.labelWrites, ports.projWrites, ports.fieldWrites)
		}

		// CONTRACT: "Read permits observation and never setup; setup requires Write/Admin and Project scope."
		// Stored operation with read-only association resumed must not execute setup mutations.
		assoc := connect.Association{
			LocalRepoID: "local-ro-repo",
			GardenID:    garden.GardenID{Repo: 100},
			Access:      connect.AccessWrite,
			Mode:        connect.Create,
		}
		if _, err := store.Put(context.Background(), 0, "key-ro", assoc); err != nil {
			t.Fatal(err)
		}

		// Try to run an apply under AccessWrite first to partially create an operation, then downgrade to read.
		portsPartial := newAdversarialPorts()
		portsPartial.failAtStep = 2 // fail on project create
		_, _ = connect.Apply(context.Background(), store, "local-ro-repo", 100, connect.AccessWrite, "op-ro-resume", plan, portsPartial.ports())

		// Downgrade the persisted association before Resume.
		rev, _, err := store.Snapshot(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		assoc.Access = connect.AccessRead
		if _, err := store.Put(context.Background(), rev, "key-ro-downgrade", assoc); err != nil {
			t.Fatal(err)
		}
		// Now resume when association is Read-only:
		// CONTRACT: "Read permits observation and never setup; setup requires Write/Admin and Project scope."
		portsResume := newAdversarialPorts()
		_, resumeErr := connect.Resume(context.Background(), store, "op-ro-resume", 100, portsResume.ports(), "local-ro-repo", connect.AccessRead)
		if !errors.Is(resumeErr, connect.ErrReadOnly) {
			t.Errorf("CONTRACT: \"Read permits observation and never setup; setup requires Write/Admin and Project scope.\" - Resume allowed mutation without checking read-only access: %v", resumeErr)
		}
		if portsResume.labelWrites != 0 || portsResume.projWrites != 0 {
			t.Errorf("fake recorded mutating writes during Resume when connection is read-only: %d labels, %d projs",
				portsResume.labelWrites, portsResume.projWrites)
		}
	})

	t.Run("MissingScope_LabelsOnlyPlan_Ambiguity", func(t *testing.T) {
		t.Parallel()
		store, _ := newTestStore(t)
		ports := newAdversarialPorts()
		ports.missingScope = true

		// Plan with only labels (no Project steps).
		planLabelsOnly, err := connect.BuildPlan(connect.Observed{}, connect.Create, connect.Desired{
			Labels: []connect.Label{{Name: "bonsai:managed", Description: "managed"}},
		})
		if err != nil {
			t.Fatal(err)
		}

		// NOTE: Ambiguity in contract:
		// Contract states: "Read permits observation and never setup; setup requires Write/Admin and Project scope."
		// Does a labels-only setup require Project scope? Apply takes `Access` but does not take `ProjectScope`.
		// If ports.missingScope only applies to Project steps, a labels-only plan will succeed.
		res, err := connect.Apply(context.Background(), store, "local-labels-only", 100, connect.AccessWrite, "op-labels", planLabelsOnly, ports.ports())
		if err != nil {
			t.Logf("Labels-only plan without project scope returned error: %v", err)
		} else if len(res.Operation.Steps) != 1 || res.Operation.Steps[0].Status != connect.Done {
			t.Fatalf("unexpected operation result: %#v", res)
		}
	})

	t.Run("NoTokenLeakInErrorsAndUserMessage", func(t *testing.T) {
		t.Parallel()
		dirtyErrors := []error{
			fmt.Errorf("adapter failed with token %s", fakeSecretToken),
			fmt.Errorf("HTTP 401 Authorization: Bearer %s", fakeSecretToken),
			fmt.Errorf("{\"error\":\"invalid_client\",\"body\":\"%s\"}", fakeSecretToken),
		}

		for _, rawErr := range dirtyErrors {
			userMsg := connect.UserMessage(rawErr)
			if strings.Contains(userMsg, fakeSecretToken) {
				t.Fatalf("UserMessage leaked token: %s", userMsg)
			}
			if strings.Contains(userMsg, "Bearer") {
				t.Fatalf("UserMessage leaked Bearer header: %s", userMsg)
			}
		}
	})
}

// ============================================================================
// 3. Plan Invariants
// ============================================================================

func TestPlanInvariants_Adversarial(t *testing.T) {
	t.Parallel()

	observedStates := []struct {
		name     string
		observed connect.Observed
	}{
		{
			name:     "empty",
			observed: connect.Observed{},
		},
		{
			name: "compatible_labels",
			observed: connect.Observed{
				Labels: []connect.Label{
					{Name: "bonsai:managed", Description: "managed"},
					{Name: "bonsai:type/feature", Description: "feature"},
					{Name: "bonsai:stage/ready", Description: "ready"},
				},
			},
		},
		{
			name: "differing_color_and_description",
			observed: connect.Observed{
				Labels: []connect.Label{
					{Name: "bonsai:managed", Color: "ff0000", Description: "custom user description"},
				},
			},
		},
		{
			name: "conflicting_extra_stage_type_labels",
			observed: connect.Observed{
				Labels: []connect.Label{
					{Name: "bonsai:type/alien-type", Description: "alien"},
					{Name: "bonsai:stage/unknown-stage", Description: "unknown"},
				},
			},
		},
		{
			name: "existing_project",
			observed: connect.Observed{
				Projects: []garden.Project{
					{ID: "p-existing", Name: "Garden", Number: "1", Owner: "alice"},
				},
			},
		},
		{
			name: "project_with_extra_unrelated_fields",
			observed: connect.Observed{
				Projects: []garden.Project{
					{
						ID:     "p-existing",
						Name:   "Garden",
						Number: "1",
						Owner:  "alice",
						Fields: []garden.ProjectField{
							{Name: "Priority"},
							{Name: "CustomSprintField"},
							{Name: "RandomUnrelatedField"},
						},
					},
				},
			},
		},
		{
			name: "label_names_case_whitespace_unicode_confusables",
			observed: connect.Observed{
				Labels: []connect.Label{
					{Name: "BONSAI:MANAGED", Description: "uppercase"},
					{Name: " bonsai:type/feature ", Description: "whitespace"},
					{Name: "bonsаi:stage/ready", Description: "cyrillic a"}, // \u0430
				},
			},
		},
	}

	modes := []connect.Mode{connect.Create, connect.ConnectExisting, connect.Skip}

	for _, obs := range observedStates {
		for _, m := range modes {
			t.Run(fmt.Sprintf("%s_%s", obs.name, m), func(t *testing.T) {
				t.Parallel()
				desired := connect.Desired{
					Labels: []connect.Label{
						{Name: "bonsai:managed", Description: "managed", Color: "00ff00"},
						{Name: "bonsai:type/feature", Description: "feature"},
						{Name: "bonsai:stage/ready", Description: "ready"},
					},
					ProjectName: "Garden",
					Owner:       "alice",
					Fields:      []garden.ProjectField{{Name: "Status"}},
					ImportCount: 0,
				}
				if m == connect.ConnectExisting {
					desired.ExistingProject = &connect.ProjectRef{ID: "p-existing", Number: "1", Owner: "alice"}
				}

				plan, err := connect.BuildPlan(obs.observed, m, desired)
				if m == connect.ConnectExisting && obs.name != "existing_project" && obs.name != "project_with_extra_unrelated_fields" {
					// ErrProjectNotFound expected when project does not exist
					if !errors.Is(err, connect.ErrProjectNotFound) {
						t.Fatalf("expected ErrProjectNotFound, got %v", err)
					}
					return
				}
				if err != nil {
					t.Fatalf("unexpected BuildPlan error: %v", err)
				}

				// Plan invariants from contract:
				if err := plan.Validate(); err != nil {
					t.Fatalf("Plan.Validate() failed: %v", err)
				}
				if plan.ImportCount != 0 {
					t.Fatalf("expected ImportCount == 0, got %d", plan.ImportCount)
				}

				for _, step := range plan.Steps {
					// No forbidden step kinds:
					if step.Kind == "rename-label" || step.Kind == "delete-label" ||
						step.Kind == "edit-issue" || step.Kind == "enroll-issues" {
						t.Fatalf("forbidden step kind in plan: %s", step.Kind)
					}

					// Differing color/description must be reused as-is:
					if step.Kind == connect.ReuseLabel && step.Label.Name == "bonsai:managed" && obs.name == "differing_color_and_description" {
						if step.Label.Color != "ff0000" || step.Label.Description != "custom user description" {
							t.Fatalf("existing label visual metadata altered in step: %#v", step.Label)
						}
					}
				}
			})
		}
	}

	t.Run("ImportCount_Tampering", func(t *testing.T) {
		t.Parallel()
		store, _ := newTestStore(t)
		ports := newAdversarialPorts()

		tamperedCounts := []int{1, -1, math.MaxInt, math.MinInt, 999}
		for _, count := range tamperedCounts {
			// Tampering rejected by BuildPlan:
			desired := connect.Desired{ImportCount: count}
			if _, err := connect.BuildPlan(connect.Observed{}, connect.Create, desired); err == nil {
				t.Fatalf("BuildPlan accepted tampered import count %d", count)
			}

			// Tampering rejected by Validate:
			tamperedPlan := connect.Plan{
				Mode:        connect.Create,
				ImportCount: count,
			}
			if err := tamperedPlan.Validate(); err == nil {
				t.Fatalf("Validate accepted tampered import count %d", count)
			}

			// Tampering rejected by Apply:
			// CONTRACT: "Plans assert ImportCount == 0; they cannot enroll Issues, rename/delete labels, or edit Issue bodies."
			if _, err := connect.Apply(context.Background(), store, "local-tamp", 100, connect.AccessWrite, "op-tamp", tamperedPlan, ports.ports()); err == nil {
				t.Fatalf("Apply accepted tampered plan with import count %d", count)
			}
		}
	})

	t.Run("MaliciousPlan_RejectedByApply", func(t *testing.T) {
		t.Parallel()
		store, _ := newTestStore(t)
		ports := newAdversarialPorts()

		// 1. Disallowed step kind
		disallowedPlan := connect.Plan{
			Mode:        connect.Create,
			ImportCount: 0,
			Steps: []connect.Step{
				{Kind: connect.StepKind("delete-all-labels")},
			},
		}
		if _, err := connect.Apply(context.Background(), store, "local-mal-1", 100, connect.AccessWrite, "op-mal-1", disallowedPlan, ports.ports()); err == nil {
			t.Fatal("Apply accepted plan with disallowed step kind")
		}

		// 2. Duplicate steps in hand-built plan
		// CONTRACT: "Validate rejects any plan capable of destructive label or backlog changes."
		dupPlan := connect.Plan{
			Mode:        connect.Create,
			ImportCount: 0,
			Steps: []connect.Step{
				{Kind: connect.CreateLabel, Label: connect.Label{Name: "bonsai:managed"}},
				{Kind: connect.CreateLabel, Label: connect.Label{Name: "bonsai:managed"}},
			},
		}
		if _, err := connect.Apply(context.Background(), store, "local-mal-2", 100, connect.AccessWrite, "op-mal-2", dupPlan, ports.ports()); err == nil {
			t.Errorf("CONTRACT: \"Validate rejects any plan capable of destructive label or backlog changes.\" - Apply accepted plan with duplicate step actions")
		}

		// 3. Step referring to foreign repository
		foreignPlan := connect.Plan{
			Mode:        connect.Create,
			ImportCount: 0,
			Steps: []connect.Step{
				{Kind: connect.EnsureProjectField, ProjectName: "other-repo-proj", Field: garden.ProjectField{Name: "Status"}},
			},
		}
		foreignPlan.Steps[0].RepositoryID = 999
		if _, err := connect.Apply(context.Background(), store, "local-mal-3", 100, connect.AccessWrite, "op-mal-3", foreignPlan, ports.ports()); err == nil {
			t.Fatal("Apply accepted step referencing a different repository")
		}
	})

	t.Run("ProjectScopeIsPerStep", func(t *testing.T) {
		t.Parallel()
		store, _ := newTestStore(t)
		ports := newAdversarialPorts()
		ports.missingScope = true
		plan, err := connect.BuildPlan(connect.Observed{}, connect.Create, connect.Desired{Labels: []connect.Label{{Name: "bonsai:managed"}}, ProjectName: "scope-project", Fields: []garden.ProjectField{{Name: "Status"}}})
		if err != nil {
			t.Fatal(err)
		}
		res, err := connect.Apply(context.Background(), store, "scope-local", 100, connect.AccessWrite, "scope-op", plan, ports.ports())
		var scope connect.ErrScopeMissing
		if !errors.As(err, &scope) {
			t.Fatalf("expected Project scope error, got %v", err)
		}
		if ports.labelWrites != 1 || len(res.Operation.Steps) != 3 || res.Operation.Steps[0].Status != connect.Done || res.Operation.Steps[1].Status != connect.Failed || res.Operation.Steps[2].Status != connect.Failed {
			t.Fatalf("scope failure did not preserve other step progress: %#v writes=%d", res.Operation.Steps, ports.labelWrites)
		}
	})
}

// ============================================================================
// 4. Apply / Resume
// ============================================================================

func TestApplyResume_Adversarial(t *testing.T) {
	t.Parallel()

	plan, err := connect.BuildPlan(connect.Observed{}, connect.Create, connect.Desired{
		Labels:      []connect.Label{{Name: "bonsai:managed", Description: "managed"}},
		ProjectName: "AdversarialProject",
		Fields:      []garden.ProjectField{{Name: "Status"}},
		ImportCount: 0,
	})
	if err != nil {
		t.Fatal(err)
	}

	failureKinds := []struct {
		name     string
		err      error
		wantCode string
	}{
		{"permission", connect.ErrPermissionDenied, "remote-failure"},
		{"scope", connect.ErrScopeMissing{Scope: "project"}, "scope-missing"},
		{"graphql", garden.ErrGraphQL, "github-graphql"},
		{"validation", connect.ErrGitHubValidation, "github-validation"},
		{"offline", connect.ErrOffline, "offline"},
		{"rate_limited", connect.ErrRateLimited, "rate-limited"},
		{"generic", errors.New("network socket hangup"), "remote-failure"},
	}

	for _, fk := range failureKinds {
		for stepIdx := 1; stepIdx <= 3; stepIdx++ {
			t.Run(fmt.Sprintf("%s_step_%d", fk.name, stepIdx), func(t *testing.T) {
				t.Parallel()
				store, _ := newTestStore(t)
				ports := newAdversarialPorts()
				ports.failAtStep = stepIdx
				ports.stepFailure = fk.err

				opID := fmt.Sprintf("op-fail-%s-%d", fk.name, stepIdx)
				res, err := connect.Apply(context.Background(), store, "local-fail", 77, connect.AccessWrite, opID, plan, ports.ports())
				if err == nil {
					t.Fatalf("expected injected %s failure at step %d, got nil", fk.name, stepIdx)
				}

				// Check stored operation recorded the failure and error code:
				failedStep := res.Operation.Steps[stepIdx-1]
				if failedStep.Status != connect.Failed {
					t.Fatalf("expected step %d status Failed, got %s", stepIdx-1, failedStep.Status)
				}
				if failedStep.ErrorCode != fk.wantCode {
					t.Fatalf("expected ErrorCode %s, got %s", fk.wantCode, failedStep.ErrorCode)
				}

				// Resume after fixing the failure:
				ports.failAtStep = 0
				ports.stepFailure = nil
				resumeRes, err := connect.Resume(context.Background(), store, opID, 77, ports.ports(), "local-fail", connect.AccessWrite)
				if err != nil {
					t.Fatalf("Resume failed after cleared %s failure: %v", fk.name, err)
				}

				// Remote state: exactly 1 label, 1 project, 1 field:
				if len(ports.labels) != 1 {
					t.Fatalf("expected 1 label, got %d", len(ports.labels))
				}
				if len(ports.projects) != 1 {
					t.Fatalf("expected 1 project, got %d", len(ports.projects))
				}
				if len(ports.projects[0].Fields) != 1 {
					t.Fatalf("expected 1 project field, got %d", len(ports.projects[0].Fields))
				}

				// All steps in operation are Done:
				for i, s := range resumeRes.Operation.Steps {
					if s.Status != connect.Done {
						t.Fatalf("expected step %d Done after resume, got %s", i, s.Status)
					}
				}
			})
		}
	}

	t.Run("CtxCancelledAtEveryStep_ResumeWorks", func(t *testing.T) {
		t.Parallel()
		for stepIdx := 1; stepIdx <= 3; stepIdx++ {
			store, _ := newTestStore(t)
			ports := newAdversarialPorts()

			ctx, cancel := context.WithCancel(context.Background())
			stepCalls := 0
			ports.beforeHook = func(_ string) {
				stepCalls++
				if stepCalls == stepIdx {
					cancel()
				}
			}

			opID := fmt.Sprintf("op-cancel-%d", stepIdx)
			_, err := connect.Apply(ctx, store, "local-cancel", 88, connect.AccessWrite, opID, plan, ports.ports())
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("expected context.Canceled at step %d, got %v", stepIdx, err)
			}

			// Resume with active context must reconcile remaining work:
			ports.beforeHook = nil
			resumeRes, err := connect.Resume(context.Background(), store, opID, 88, ports.ports(), "local-cancel", connect.AccessWrite)
			if err != nil {
				t.Fatalf("Resume failed after cancellation at step %d: %v", stepIdx, err)
			}
			for i, s := range resumeRes.Operation.Steps {
				if s.Status != connect.Done {
					t.Fatalf("expected step %d Done after resume, got %s", i, s.Status)
				}
			}
		}
	})

	t.Run("ResumeRecreatesDeletedRemoteObject", func(t *testing.T) {
		t.Parallel()
		store, _ := newTestStore(t)
		ports := newAdversarialPorts()

		opID := "op-reobserve"
		_, err := connect.Apply(context.Background(), store, "local-reobserve", 99, connect.AccessWrite, opID, plan, ports.ports())
		if err != nil {
			t.Fatalf("initial Apply failed: %v", err)
		}

		// Delete the created label remotely to simulate external deletion:
		ports.mu.Lock()
		ports.labels = nil
		ports.mu.Unlock()

		// Resume must re-observe remote state and re-create the deleted label:
		// CONTRACT: "Resume loads the operation by ID and re-observes remote labels, fields, and Projects; create operations mark Projects with their operation ID so a remote success followed by local persistence failure can be adopted."
		_, err = connect.Resume(context.Background(), store, opID, 99, ports.ports(), "local-reobserve", connect.AccessWrite)
		if err != nil {
			t.Fatalf("Resume failed: %v", err)
		}
		if len(ports.labels) != 1 {
			t.Errorf("CONTRACT: \"Resume loads the operation by ID and re-observes remote labels, fields, and Projects\" - deleted label was not recreated by Resume: %#v", ports.labels)
		}
	})

	t.Run("Resume_UnknownForeignOpID", func(t *testing.T) {
		t.Parallel()
		store, _ := newTestStore(t)
		ports := newAdversarialPorts()

		if _, err := connect.Resume(context.Background(), store, "foreign-nonexistent-op", 99, ports.ports(), "local-unknown", connect.AccessWrite); !errors.Is(err, connect.ErrOperationNotFound) {
			t.Fatalf("expected typed not-found error resuming unknown operation, got %v", err)
		}
	})

	t.Run("Resume_LiveReadOnlyBeforeWrites", func(t *testing.T) {
		t.Parallel()
		store, _ := newTestStore(t)
		initial := newAdversarialPorts()
		initial.failAtStep = 1
		labelPlan, err := connect.BuildPlan(connect.Observed{}, connect.Create, connect.Desired{Labels: []connect.Label{{Name: "bonsai:managed"}}})
		if err != nil {
			t.Fatal(err)
		}
		_, _ = connect.Apply(context.Background(), store, "live-read-local", 99, connect.AccessWrite, "live-read-op", labelPlan, initial.ports())
		resumePorts := newAdversarialPorts()
		if _, err := connect.Resume(context.Background(), store, "live-read-op", 99, resumePorts.ports(), "live-read-local", connect.AccessRead); !errors.Is(err, connect.ErrReadOnly) {
			t.Fatalf("expected live read-only denial, got %v", err)
		}
		if resumePorts.labelWrites != 0 || resumePorts.projWrites != 0 || resumePorts.fieldWrites != 0 {
			t.Fatal("read-only Resume performed a remote write")
		}
	})

	t.Run("Resume_OperationBelongingToAnotherLocalRepoID", func(t *testing.T) {
		t.Parallel()
		store, _ := newTestStore(t)
		portsA := newAdversarialPorts()

		// Apply under local-repo-A for remote repo 100
		opID := "op-repo-a"
		_, err := connect.Apply(context.Background(), store, "local-repo-A", 100, connect.AccessWrite, opID, plan, portsA.ports())
		if err != nil {
			t.Fatal(err)
		}

		// CONTRACT: "Associations and operations are local-repository scoped, revisioned JSON under Bonsai config, written under the cross-process lock and atomic replacement pattern."
		// CONTRACT: "LocalRepoID is the separate path-derived config.ProjectID; clones and worktrees share a GardenID while retaining independent local execution identity and locks."
		// Resuming operation "op-repo-a" under a different remote repo (200) without matching local repo must fail:
		portsB := newAdversarialPorts()
		_, resumeErr := connect.Resume(context.Background(), store, opID, 100, portsB.ports(), garden.LocalRepoID("local-repo-B"), connect.AccessWrite)
		if !errors.Is(resumeErr, connect.ErrOperationForeign) {
			t.Errorf("CONTRACT: \"Associations and operations are local-repository scoped\" - Resume allowed operation from local-repo-A on foreign remote repo 200")
		}
		_, repoErr := connect.Resume(context.Background(), store, opID, 200, portsB.ports(), "local-repo-A", connect.AccessWrite)
		if !errors.Is(repoErr, connect.ErrOperationForeign) {
			t.Errorf("Resume accepted foreign repository: %v", repoErr)
		}
	})

	t.Run("ConcurrentApply_SameLocalRepoID_OneWins", func(t *testing.T) {
		t.Parallel()
		store, _ := newTestStore(t)
		ports := newAdversarialPorts()

		var startWg sync.WaitGroup
		startWg.Add(1)
		var doneWg sync.WaitGroup

		errs := make(chan error, 2)
		for i := 0; i < 2; i++ {
			doneWg.Add(1)
			go func(id int) {
				defer doneWg.Done()
				startWg.Wait()
				_, e := connect.Apply(context.Background(), store, "local-same", 55, connect.AccessWrite, fmt.Sprintf("op-same-%d", id), plan, ports.ports())
				errs <- e
			}(i)
		}

		startWg.Done()
		doneWg.Wait()
		close(errs)

		success := 0
		inProgress := 0
		for e := range errs {
			if e == nil {
				success++
			} else if errors.Is(e, connect.ErrOperationInProgress) {
				inProgress++
			} else {
				t.Fatalf("unexpected error from concurrent Apply: %v", e)
			}
		}

		if success != 1 || inProgress != 1 {
			t.Fatalf("expected exactly 1 success and 1 ErrOperationInProgress, got %d success, %d in-progress", success, inProgress)
		}
		if len(ports.labels) != 1 || len(ports.projects) != 1 {
			t.Fatalf("concurrent apply created duplicate remote objects: %#v", ports)
		}
	})

	t.Run("ConcurrentApply_DifferentLocalRepos_BothSucceed", func(t *testing.T) {
		t.Parallel()
		store, _ := newTestStore(t)

		// CONTRACT: "Two clones resolve the same Garden without sharing local execution locks."
		keyA := connect.ExecutionKey("clone-A")
		keyB := connect.ExecutionKey("clone-B")
		if keyA == keyB {
			t.Fatalf("execution keys must differ between clones: %s", keyA)
		}

		portsA := newAdversarialPorts()
		portsB := newAdversarialPorts()

		var startWg sync.WaitGroup
		startWg.Add(1)
		var doneWg sync.WaitGroup
		errs := make(chan error, 2)

		doneWg.Add(2)
		go func() {
			defer doneWg.Done()
			startWg.Wait()
			_, e := connect.Apply(context.Background(), store, "clone-A", 42, connect.AccessWrite, "op-clone-a", plan, portsA.ports())
			errs <- e
		}()
		go func() {
			defer doneWg.Done()
			startWg.Wait()
			_, e := connect.Apply(context.Background(), store, "clone-B", 42, connect.AccessWrite, "op-clone-b", plan, portsB.ports())
			errs <- e
		}()

		startWg.Done()
		doneWg.Wait()
		close(errs)

		for e := range errs {
			if e != nil {
				t.Fatalf("concurrent clone Apply failed: %v", e)
			}
		}
	})

	t.Run("ConflictLabel_ResultReportsConflict_OthersApplied", func(t *testing.T) {
		t.Parallel()
		store, _ := newTestStore(t)
		ports := newAdversarialPorts()
		ports.labels = []connect.Label{
			{Name: "bonsai:stage/legacy-stage", Description: "legacy"},
		}

		conflictPlan, err := connect.BuildPlan(connect.Observed{Labels: ports.labels}, connect.Create, connect.Desired{
			Labels:      []connect.Label{{Name: "bonsai:managed", Description: "managed"}},
			ProjectName: "ConflictProject",
			ImportCount: 0,
		})
		if err != nil {
			t.Fatal(err)
		}

		res, err := connect.Apply(context.Background(), store, "local-conflict", 123, connect.AccessWrite, "op-conflict", conflictPlan, ports.ports())
		if err != nil {
			t.Fatalf("Apply with conflict step failed: %v", err)
		}

		// Conflict step must be Failed with "label-conflict", and valid steps must be Done:
		hasConflict := false
		hasManaged := false
		for _, s := range res.Operation.Steps {
			if s.Kind == connect.ConflictLabel {
				hasConflict = true
				if s.Status != connect.Failed || s.ErrorCode != "label-conflict" {
					t.Fatalf("expected ConflictLabel step failed with label-conflict, got status %s, code %s", s.Status, s.ErrorCode)
				}
			}
			if s.Kind == connect.CreateLabel && s.Status == connect.Done {
				hasManaged = true
			}
		}
		if !hasConflict || !hasManaged {
			t.Fatalf("expected both conflict report and managed label creation: %#v", res.Operation.Steps)
		}
		// Existing conflict label must NOT be overwritten:
		if ports.labels[0].Name != "bonsai:stage/legacy-stage" {
			t.Fatalf("existing conflicting label was modified: %#v", ports.labels)
		}
	})

	t.Run("ManualStep_AwaitingManual_NeverMarkedDone", func(t *testing.T) {
		t.Parallel()
		store, _ := newTestStore(t)
		ports := newAdversarialPorts()

		// CONTRACT: "Unsupported browser-only finishing is represented by an explicit manual step."
		manualPlan := connect.Plan{
			Mode:        connect.Create,
			ImportCount: 0,
			Steps: []connect.Step{
				{Kind: connect.CreateLabel, Label: connect.Label{Name: "bonsai:managed"}},
				{Kind: connect.ManualStep, Instructions: "Configure custom board layout in GitHub browser UI."},
			},
		}

		res, err := connect.Apply(context.Background(), store, "local-manual", 124, connect.AccessWrite, "op-manual", manualPlan, ports.ports())
		if err != nil {
			t.Fatalf("Apply with manual step failed: %v", err)
		}

		if len(res.AwaitingManual) != 1 || res.AwaitingManual[0] != "Configure custom board layout in GitHub browser UI." {
			t.Fatalf("expected AwaitingManual instruction, got %#v", res.AwaitingManual)
		}

		// ManualStep must have status Skipped and NOT Done:
		for _, s := range res.Operation.Steps {
			if s.Kind == connect.ManualStep && s.Status == connect.Done {
				t.Fatalf("ManualStep must never be marked Done by Apply: %#v", s)
			}
		}

		// Stored operation must not be marked finished:
		op, err := store.Operation(context.Background(), "op-manual")
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range op.Steps {
			if s.Kind == connect.ManualStep && s.Status == connect.Done {
				t.Fatalf("stored ManualStep marked Done: %#v", s)
			}
		}
	})
}

// ============================================================================
// 5. Store
// ============================================================================

func TestStore_Adversarial(t *testing.T) {
	t.Parallel()

	t.Run("FilePermissions_Private", func(t *testing.T) {
		t.Parallel()
		if runtime.GOOS == "windows" {
			t.Skip("skipping Unix permission check on Windows")
		}

		store, _ := newTestStore(t)
		assoc := connect.Association{
			LocalRepoID: "local-perm",
			GardenID:    garden.GardenID{Repo: 10},
			Mode:        connect.Skip,
		}
		if _, err := store.Put(context.Background(), 0, "key-perm", assoc); err != nil {
			t.Fatal(err)
		}

		info, err := os.Stat(store.Path)
		if err != nil {
			t.Fatal(err)
		}
		perm := info.Mode().Perm()
		if perm&0077 != 0 {
			t.Fatalf("store file permissions are not private: 0%o (expected 0600)", perm)
		}
	})

	t.Run("Atomic_KillSimulation_DirReadOnly", func(t *testing.T) {
		t.Parallel()
		if runtime.GOOS == "windows" {
			t.Skip("skipping directory permission check on Windows")
		}

		store, _ := newTestStore(t)
		assoc1 := connect.Association{
			LocalRepoID: "local-atomic",
			GardenID:    garden.GardenID{Repo: 20},
			Mode:        connect.Skip,
		}
		rev1, err := store.Put(context.Background(), 0, "key-atomic-1", assoc1)
		if err != nil {
			t.Fatal(err)
		}

		origBytes, err := os.ReadFile(store.Path)
		if err != nil {
			t.Fatal(err)
		}

		// Make directory read-only to simulate mid-update failure during atomic replacement
		storeDir := filepath.Dir(store.Path)
		if err := os.Chmod(storeDir, 0500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(storeDir, 0700) })

		assoc2 := connect.Association{
			LocalRepoID: "local-atomic",
			GardenID:    garden.GardenID{Repo: 20},
			Mode:        connect.Create,
		}
		_, err = store.Put(context.Background(), rev1, "key-atomic-2", assoc2)
		if err == nil {
			t.Fatal("expected Put to fail when directory is read-only")
		}

		// Restore write permission and check file integrity:
		_ = os.Chmod(storeDir, 0700)
		afterBytes, err := os.ReadFile(store.Path)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(origBytes, afterBytes) {
			t.Fatalf("kill-simulation: store file corrupted during write failure!\nBefore: %s\nAfter: %s", origBytes, afterBytes)
		}

		// Stored data must be intact:
		snapRev, items, err := store.Snapshot(context.Background())
		if err != nil || snapRev != rev1 || len(items) != 1 || items[0].Mode != connect.Skip {
			t.Fatalf("snapshot invalid after failed write: rev %d, items: %#v, err: %v", snapRev, items, err)
		}
	})

	t.Run("StaleRevision_Rejected", func(t *testing.T) {
		t.Parallel()
		store, _ := newTestStore(t)
		assoc := connect.Association{
			LocalRepoID: "local-rev",
			GardenID:    garden.GardenID{Repo: 30},
			Mode:        connect.Skip,
		}

		rev, err := store.Put(context.Background(), 0, "k1", assoc)
		if err != nil || rev != 1 {
			t.Fatalf("Put failed: rev %d, err %v", rev, err)
		}

		// Stale Put with rev 0 must fail:
		if _, err := store.Put(context.Background(), 0, "k2", assoc); !errors.Is(err, connect.ErrRevision) {
			t.Fatalf("expected ErrRevision for stale Put, got %v", err)
		}

		// Stale Remove with rev 0 must fail:
		if _, err := store.Remove(context.Background(), 0, "k3", assoc.LocalRepoID); !errors.Is(err, connect.ErrRevision) {
			t.Fatalf("expected ErrRevision for stale Remove, got %v", err)
		}
	})

	t.Run("Idempotency_KeyReplay", func(t *testing.T) {
		t.Parallel()
		store, _ := newTestStore(t)
		assoc := connect.Association{
			LocalRepoID: "local-idemp",
			GardenID:    garden.GardenID{Repo: 40},
			Mode:        connect.Skip,
		}

		rev1, err := store.Put(context.Background(), 0, "idemp-key-1", assoc)
		if err != nil {
			t.Fatal(err)
		}

		// Replay with exact same request returns original result without incrementing revision:
		rev2, err := store.Put(context.Background(), 999, "idemp-key-1", assoc)
		if err != nil {
			t.Fatalf("replayed Put failed: %v", err)
		}
		if rev2 != rev1 {
			t.Fatalf("replayed Put changed revision: %d != %d", rev2, rev1)
		}

		// Replay with SAME key but DIFFERENT association must return ErrIdempotency:
		differentAssoc := connect.Association{
			LocalRepoID: "local-idemp",
			GardenID:    garden.GardenID{Repo: 999},
			Mode:        connect.Create,
		}
		if _, err := store.Put(context.Background(), rev1, "idemp-key-1", differentAssoc); !errors.Is(err, connect.ErrIdempotency) {
			t.Fatalf("expected ErrIdempotency for different request with same key, got %v", err)
		}
	})

	t.Run("CorruptedTruncatedUnknownSchema_Untouched", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name      string
			content   string
			wantTyped error
		}{
			{"corrupted_json", "{not-valid-json", nil},
			{"truncated_json", "{\"version\": 1, \"associations\":", nil},
			{"unknown_schema_version", "{\"version\": 999, \"revision\": 1, \"associations\": {}, \"operations\": {}}", connect.ErrUnsupportedSchema},
			{"duplicate_keys", "{\"version\": 1, \"revision\": 1, \"revision\": 2, \"associations\": {}, \"operations\": {}}", connect.ErrInvalidOperation},
			{"huge_file", "{\"version\": 1, \"revision\": 1, \"associations\": {}, \"operations\": {}, \"padding\": \"" + strings.Repeat("A", 10*1024*1024) + "\"}", connect.ErrCorruptStore},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				store, _ := newTestStore(t)
				if err := os.MkdirAll(filepath.Dir(store.Path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(store.Path, []byte(tc.content), 0600); err != nil {
					t.Fatal(err)
				}

				_, _, err := store.Snapshot(context.Background())
				if err == nil {
					// CONTRACT: "JSON is bounded by MaxMetadataBytes, valid UTF-8, rejects duplicate and unknown fields, and requires schema_version: 1; unknown versions are unsupported."
					t.Errorf("CONTRACT: \"JSON is bounded by MaxMetadataBytes, valid UTF-8, rejects duplicate and unknown fields, and requires schema_version: 1; unknown versions are unsupported.\" - expected read error for %s, got nil", tc.name)
					return
				}

				// CONTRACT: "unknown future versions return an unsupported-schema problem."
				// CONTRACT: "JSON is bounded by MaxMetadataBytes, valid UTF-8, rejects duplicate and unknown fields, and requires schema_version: 1; unknown versions are unsupported."
				if tc.wantTyped != nil && !errors.Is(err, tc.wantTyped) {
					t.Errorf("CONTRACT: \"unknown future versions return an unsupported-schema problem.\" - expected typed error for %s, got %v", tc.name, err)
				}
				if tc.name == "huge_file" && !errors.Is(err, connect.ErrStoreTooLarge) {
					t.Errorf("expected size-limit cause, got %v", err)
				}

				// Assert original bytes are untouched (never "repaired" or overwritten by failed read):
				bytesOnDisk, readErr := os.ReadFile(store.Path)
				if readErr != nil {
					t.Fatal(readErr)
				}
				if string(bytesOnDisk) != tc.content {
					t.Fatalf("%s: store bytes were modified/repaired by decoder!\nOriginal: %s\nOn disk: %s", tc.name, tc.content, bytesOnDisk)
				}
			})
		}
	})

	t.Run("UnknownFields_Rejected", func(t *testing.T) {
		t.Parallel()
		store, _ := newTestStore(t)
		if err := os.MkdirAll(filepath.Dir(store.Path), 0700); err != nil {
			t.Fatal(err)
		}
		unknownPayload := `{"version": 1, "revision": 1, "associations": {}, "operations": {}, "malicious_extra_field": "injected"}`
		if err := os.WriteFile(store.Path, []byte(unknownPayload), 0600); err != nil {
			t.Fatal(err)
		}

		// CONTRACT: "Decoders reject unknown fields for known schemas; unknown future versions return an unsupported-schema problem."
		_, _, err := store.Snapshot(context.Background())
		if !errors.Is(err, connect.ErrCorruptStore) {
			t.Errorf("CONTRACT: \"Decoders reject unknown fields for known schemas; unknown future versions return an unsupported-schema problem.\" - expected ErrCorruptStore, got %v", err)
		}
	})

	t.Run("Resume_PlanHashMismatchDoesNotExecute", func(t *testing.T) {
		t.Parallel()
		store, _ := newTestStore(t)
		ports := newAdversarialPorts()
		plan, err := connect.BuildPlan(connect.Observed{}, connect.Create, connect.Desired{Labels: []connect.Label{{Name: "bonsai:managed"}}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := connect.Apply(context.Background(), store, "hash-local", 100, connect.AccessWrite, "hash-op", plan, ports.ports()); err != nil {
			t.Fatal(err)
		}
		encoded, err := os.ReadFile(store.Path)
		if err != nil {
			t.Fatal(err)
		}
		encoded = bytes.Replace(encoded, []byte("bonsai:managed"), []byte("bonsai:altered"), 1)
		if err := os.WriteFile(store.Path, encoded, 0600); err != nil {
			t.Fatal(err)
		}
		resumePorts := newAdversarialPorts()
		if _, err := connect.Resume(context.Background(), store, "hash-op", 100, resumePorts.ports(), "hash-local", connect.AccessWrite); !errors.Is(err, connect.ErrInvalidOperation) {
			t.Fatalf("expected plan-hash error, got %v", err)
		}
		if resumePorts.labelWrites != 0 || resumePorts.projWrites != 0 || resumePorts.fieldWrites != 0 {
			t.Fatal("mismatched plan executed a remote mutation")
		}
	})

	t.Run("NoTokenStringsPersisted", func(t *testing.T) {
		t.Parallel()
		store, _ := newTestStore(t)
		assoc := connect.Association{
			LocalRepoID:    "local-token-test",
			GardenID:       garden.GardenID{Repo: 50},
			RemoteFullName: "org/repo",
			Mode:           connect.Create,
			Identity: connect.GitHubIdentity{
				ID:    "user-123",
				Login: "testuser",
			},
		}

		if _, err := store.Put(context.Background(), 0, "key-token", assoc); err != nil {
			t.Fatal(err)
		}

		// Also run an Apply with dummy ports to write operations to store
		ports := newAdversarialPorts()
		plan, _ := connect.BuildPlan(connect.Observed{}, connect.Create, connect.Desired{
			Labels: []connect.Label{{Name: "bonsai:managed"}},
		})
		_, _ = connect.Apply(context.Background(), store, "local-token-test", 50, connect.AccessWrite, "op-token", plan, ports.ports())

		rawBytes, err := os.ReadFile(store.Path)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(rawBytes, []byte(fakeSecretToken)) {
			t.Fatalf("persisted store leaked secret token %s", fakeSecretToken)
		}
	})

	t.Run("TwoClones_ExecutionKey_IndependentRemoval", func(t *testing.T) {
		t.Parallel()
		store, _ := newTestStore(t)

		cloneA := garden.LocalRepoID("clone-repo-A")
		cloneB := garden.LocalRepoID("clone-repo-B")
		gardenID := garden.GardenID{Repo: 60}

		if connect.ExecutionKey(cloneA) == connect.ExecutionKey(cloneB) {
			t.Fatalf("clones must have different execution keys: %s", connect.ExecutionKey(cloneA))
		}

		rev1, err := store.Put(context.Background(), 0, "key-a", connect.Association{
			LocalRepoID: cloneA,
			GardenID:    gardenID,
			Mode:        connect.Create,
		})
		if err != nil {
			t.Fatal(err)
		}

		rev2, err := store.Put(context.Background(), rev1, "key-b", connect.Association{
			LocalRepoID: cloneB,
			GardenID:    gardenID,
			Mode:        connect.Create,
		})
		if err != nil {
			t.Fatal(err)
		}

		// Remove clone A association:
		rev3, err := store.Remove(context.Background(), rev2, "rm-a", cloneA)
		if err != nil {
			t.Fatal(err)
		}

		// Clone B must still be present and intact:
		snapRev, items, err := store.Snapshot(context.Background())
		if err != nil || snapRev != rev3 {
			t.Fatalf("snapshot failed: rev %d, err %v", snapRev, err)
		}
		if len(items) != 1 || items[0].LocalRepoID != cloneB {
			t.Fatalf("expected only clone B remaining, got %#v", items)
		}
	})

	t.Run("Skip_PersistedAndReversible", func(t *testing.T) {
		t.Parallel()
		store, _ := newTestStore(t)

		localID := garden.LocalRepoID("local-skip-test")
		gardenID := garden.GardenID{Repo: 70}

		// Persist Skip decision:
		rev1, err := store.Put(context.Background(), 0, "key-skip", connect.Association{
			LocalRepoID: localID,
			GardenID:    gardenID,
			Mode:        connect.Skip,
		})
		if err != nil {
			t.Fatal(err)
		}

		_, items, err := store.Snapshot(context.Background())
		if err != nil || len(items) != 1 || items[0].Mode != connect.Skip {
			t.Fatalf("expected persisted Skip association, got %#v", items)
		}

		// Reversible: transition Skip -> ConnectExisting
		rev2, err := store.Put(context.Background(), rev1, "key-connect", connect.Association{
			LocalRepoID: localID,
			GardenID:    gardenID,
			Mode:        connect.ConnectExisting,
		})
		if err != nil {
			t.Fatal(err)
		}

		_, itemsAfter, err := store.Snapshot(context.Background())
		if err != nil || len(itemsAfter) != 1 || itemsAfter[0].Mode != connect.ConnectExisting || rev2 <= rev1 {
			t.Fatalf("expected updated ConnectExisting association, got rev %d, items: %#v", rev2, itemsAfter)
		}
	})
}

func TestOpenStore_UsesUserConfigDir(t *testing.T) {
	// Not parallel because t.Setenv is used to test default config directory resolution
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("APPDATA", dir)

	s, err := connect.OpenStore()
	if err != nil {
		t.Fatalf("OpenStore failed: %v", err)
	}
	expected := filepath.Join(dir, "bonsai", "garden-associations.json")
	if s.Path != expected {
		t.Fatalf("expected OpenStore path %s, got %s", expected, s.Path)
	}
}

// ============================================================================
// 6. Fuzz Tests
// ============================================================================

func FuzzResolveTarget(f *testing.F) {
	// Seed corpus with real remote shapes
	f.Add("origin", "https://github.com/octocat/Hello-World.git", uint64(100), false)
	f.Add("upstream", "git@github.com:octocat/Hello-World.git", uint64(100), true)
	f.Add("gitlab", "https://gitlab.com/company/repo.git", uint64(0), false)
	f.Add("file", "file:///local/path", uint64(0), false)
	f.Add("ssh_custom", "ssh://git@github.com:2222/org/repo.git", uint64(200), false)
	f.Add("token", "https://user:"+fakeSecretToken+"@github.com/org/repo.git", uint64(300), false)
	f.Add("empty", "", uint64(0), false)
	f.Add("whitespace", "   ", uint64(0), false)
	f.Add("long_url", "https://github.com/"+strings.Repeat("a", 300)+"/"+strings.Repeat("b", 300), uint64(400), false)

	f.Fuzz(func(t *testing.T, remoteName, remoteURL string, repoID uint64, isFork bool) {
		remotes := testRemoteLister{
			{Name: remoteName, URL: remoteURL},
		}
		resolver := newTestRepoResolver()
		if repoID > 0 {
			resolver.repos[remoteURL] = connect.Repository{
				ID:       garden.RepoID(repoID),
				FullName: "org/repo",
				Fork:     isFork,
				ParentID: garden.RepoID(repoID + 1),
			}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		_, _, err := connect.ResolveTarget(ctx, remotes, resolver, nil)
		if err != nil {
			// Invariant: no panic and no token echo in errors or UserMessage
			if strings.Contains(err.Error(), fakeSecretToken) {
				t.Fatalf("FuzzResolveTarget error leaked secret token: %v", err)
			}
			msg := connect.UserMessage(err)
			if strings.Contains(msg, fakeSecretToken) {
				t.Fatalf("FuzzResolveTarget UserMessage leaked secret token: %s", msg)
			}
		}
	})
}

func FuzzStoreDecode(f *testing.F) {
	// Seed corpus with real and malformed JSON shapes
	f.Add([]byte(`{"version": 1, "revision": 1, "associations": {}, "operations": {}}`))
	f.Add([]byte(`{"version": 1, "revision": 2, "associations": {"repo-1": {"local_repo_id": "repo-1", "garden_id": {"Repo": 42}, "mode": "create"}}, "operations": {}}`))
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"version": 2}`))
	f.Add([]byte(`{"version": 1, "extra_field": "unexpected"}`))
	f.Add([]byte(`{"token": "` + fakeSecretToken + `"}`))
	f.Add([]byte(`invalid json`))
	f.Add([]byte(`{"version": 1, `))
	f.Add([]byte{0x00, 0xff, 0xfe, 0xfd})

	f.Fuzz(func(t *testing.T, data []byte) {
		tempDir := t.TempDir()
		storePath := filepath.Join(tempDir, "store.json")
		if err := os.WriteFile(storePath, data, 0600); err != nil {
			return
		}

		store := &connect.Store{Path: storePath}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()

		_, _, err := store.Snapshot(ctx)
		if err != nil {
			// Invariant: no panic, no token echo in error
			if strings.Contains(err.Error(), fakeSecretToken) {
				t.Fatalf("FuzzStoreDecode error leaked token: %v", err)
			}
			msg := connect.UserMessage(err)
			if strings.Contains(msg, fakeSecretToken) {
				t.Fatalf("FuzzStoreDecode UserMessage leaked token: %s", msg)
			}
		}

		_, _ = store.Operation(ctx, "any-op")
	})
}
