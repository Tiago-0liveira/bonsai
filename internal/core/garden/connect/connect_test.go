package connect

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
	gitstore "github.com/Tiago-0liveira/bonsai/internal/storage/git"
)

func TestBuildPlanZeroImportAndConflict(t *testing.T) {
	desired := Desired{ImportCount: 0, Labels: []Label{{Name: "bonsai:managed", Description: "managed"}}}
	p, err := BuildPlan(Observed{Labels: []Label{{Name: "bonsai:managed", Description: "other", Color: "f00"}}}, Create, desired)
	if err != nil {
		t.Fatal(err)
	}
	if p.ImportCount != 0 || len(p.Steps) != 1 || p.Steps[0].Kind != ReuseLabel {
		t.Fatalf("unexpected plan: %#v", p)
	}
	if _, err = BuildPlan(Observed{}, Create, Desired{ImportCount: 1}); err == nil {
		t.Fatal("nonzero import count accepted")
	}
}

type remotesFake []Remote

func (r remotesFake) ListRemotes(context.Context) ([]Remote, error) { return r, nil }

type resolverFake map[string]Repository

func (r resolverFake) ResolveRepository(_ context.Context, remote Remote) (Repository, error) {
	x, ok := r[remote.URL]
	if !ok {
		return Repository{}, ErrNoGitHubRemote
	}
	return x, nil
}
func TestAmbiguousTargetRequiresChoiceAndReadOnlySetup(t *testing.T) {
	rs := remotesFake{{Name: "origin", URL: "one"}, {Name: "upstream", URL: "two"}}
	rr := resolverFake{"one": {ID: 1, FullName: "a/one"}, "two": {ID: 2, FullName: "b/two"}}
	_, _, e := ResolveTarget(context.Background(), rs, rr, nil)
	var a *AmbiguousError
	if !errors.As(e, &a) || len(a.Candidates) != 2 {
		t.Fatalf("ambiguity: %v", e)
	}
	if _, _, e = ResolveTarget(context.Background(), rs, rr, &Candidate{Remote: Remote{Name: "bad", URL: "bad"}, Repository: Repository{ID: 3}}); e == nil {
		t.Fatal("accepted noncandidate")
	}
	c := Connection{Access: AccessRead, ProjectReader: &testPorts{}}
	if _, e = c.Reader(); e != nil {
		t.Fatal(e)
	}
	if _, e = c.Setup(); !errors.Is(e, ErrReadOnly) {
		t.Fatalf("Setup: %v", e)
	}
	if _, e = (Connection{Access: AccessWrite}).Setup(); e != nil {
		t.Fatalf("Write setup must not require Project scope before planning: %v", e)
	}
	forkResolver := resolverFake{"fork": {ID: 10, FullName: "user/repo", Fork: true, ParentID: 11}}
	_, _, e = ResolveTarget(context.Background(), remotesFake{{Name: "origin", URL: "fork"}}, forkResolver, nil)
	if !errors.As(e, &a) || len(a.Candidates) != 2 || !a.Candidates[1].Upstream || a.Candidates[1].Repository.ID != 11 {
		t.Fatalf("fork/upstream choice missing: %#v %v", a, e)
	}
}

func TestResolveTargetNeverRetainsRemoteCredentials(t *testing.T) {
	urls := []string{"https://ghp_FAKETOKEN123@github.com/o/r.git", "https://user:ghp_FAKETOKEN123@github.com/o/r.git?token=ghp_FAKETOKEN123#secret", "git:ghp_FAKETOKEN123@github.com:o/r.git"}
	for _, raw := range urls {
		t.Run(raw[:min(len(raw), 12)], func(t *testing.T) {
			r := &resolverCapture{}
			target, _, err := ResolveTarget(context.Background(), remotesFake{{Name: "origin", URL: raw}}, r, nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, out := range []string{fmt.Sprintf("%v", target), fmt.Sprintf("%+v", target), fmt.Sprintf("%#v", target), fmt.Sprintf("%v", r.seen), fmt.Sprintf("%+v", r.seen), fmt.Sprintf("%#v", r.seen), fmt.Sprintf("%v", err), fmt.Sprintf("%+v", err), fmt.Sprintf("%#v", err)} {
				if strings.Contains(out, "ghp_FAKETOKEN123") {
					t.Fatalf("leaked: %s", out)
				}
			}
		})
	}
}

type resolverCapture struct{ seen Remote }

func (r *resolverCapture) ResolveRepository(_ context.Context, remote Remote) (Repository, error) {
	r.seen = remote
	return Repository{ID: 42, FullName: "o/r"}, nil
}

type resolverError struct{}

func (resolverError) ResolveRepository(context.Context, Remote) (Repository, error) {
	return Repository{}, errors.New("resolver ghp_FAKETOKEN123")
}

type resolverAmbiguous struct{}

func (resolverAmbiguous) ResolveRepository(_ context.Context, remote Remote) (Repository, error) {
	if remote.Name == "one" {
		return Repository{ID: 1, FullName: "o/one"}, nil
	}
	return Repository{ID: 2, FullName: "o/two"}, nil
}
func TestRemoteCredentialsAbsentFromAmbiguityAndResolutionErrors(t *testing.T) {
	secretURL := "https://user:ghp_FAKETOKEN123@github.com/o/r.git?access_token=ghp_FAKETOKEN123#secret"
	_, _, err := ResolveTarget(context.Background(), remotesFake{{Name: "origin", URL: secretURL}}, resolverError{}, nil)
	for _, out := range []string{fmt.Sprintf("%v", err), fmt.Sprintf("%+v", err), fmt.Sprintf("%#v", err)} {
		if strings.Contains(out, "ghp_FAKETOKEN123") {
			t.Fatalf("error leaked: %s", out)
		}
	}
	_, _, err = ResolveTarget(context.Background(), remotesFake{{Name: "one", URL: secretURL}, {Name: "two", URL: secretURL}}, resolverAmbiguous{}, nil)
	for _, out := range []string{fmt.Sprintf("%v", err), fmt.Sprintf("%+v", err), fmt.Sprintf("%#v", err)} {
		if strings.Contains(out, "ghp_FAKETOKEN123") {
			t.Fatalf("ambiguity leaked: %s", out)
		}
	}
}

type testPorts struct {
	mu            sync.Mutex
	labels        []Label
	projects      []garden.Project
	calls, failAt int
	fail          error
	block         chan struct{}
	raceLabel     *Label
	raceProject   bool
}

func (f *testPorts) ListLabels(context.Context, garden.RepoID) ([]Label, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Label(nil), f.labels...), nil
}
func (f *testPorts) CreateLabel(_ context.Context, _ garden.RepoID, l Label) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if e := f.hit(); e != nil {
		return e
	}
	if f.raceLabel != nil {
		f.labels = append(f.labels, *f.raceLabel)
		return nil
	}
	for _, x := range f.labels {
		if x.Name == l.Name {
			return nil
		}
	}
	f.labels = append(f.labels, l)
	return nil
}
func (f *testPorts) ListProjects(context.Context, garden.RepoID) ([]garden.Project, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]garden.Project(nil), f.projects...), nil
}
func (f *testPorts) EnsureProject(context.Context, garden.RepoID, string) (garden.Project, error) {
	return garden.Project{}, errors.New("unexpected non-marker project create")
}
func (f *testPorts) EnsureProjectWithMarker(_ context.Context, _ garden.RepoID, name, marker string) (garden.Project, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if e := f.hit(); e != nil {
		return garden.Project{}, e
	}
	if f.raceProject {
		p := garden.Project{ID: "raced", Name: name, Description: "other writer", CreatorLogin: "other"}
		f.projects = append(f.projects, p)
		return p, nil
	}
	for _, p := range f.projects {
		if p.Description == marker {
			return p, nil
		}
	}
	p := garden.Project{ID: "p1", Name: name, Description: marker, CreatorLogin: "viewer"}
	f.projects = append(f.projects, p)
	return p, nil
}
func (f *testPorts) EnsureField(_ context.Context, id string, w garden.ProjectField) (garden.ProjectField, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if e := f.hit(); e != nil {
		return garden.ProjectField{}, e
	}
	for i := range f.projects {
		if f.projects[i].ID == id {
			for _, x := range f.projects[i].Fields {
				if x.Name == w.Name {
					return x, nil
				}
			}
			f.projects[i].Fields = append(f.projects[i].Fields, w)
			return w, nil
		}
	}
	return garden.ProjectField{}, ErrProjectNotFound
}
func (f *testPorts) hit() error {
	f.calls++
	if f.failAt == f.calls {
		return f.fail
	}
	return nil
}
func (f *testPorts) ports() SetupPorts {
	return SetupPorts{Labels: f, LabelWrites: f, Projects: f, ProjectWrites: f, OperationProjects: f, ViewerLogin: "viewer"}
}

func TestApplyFailureAtEveryStepResumesWithoutDuplicates(t *testing.T) {
	for n := 1; n <= 3; n++ {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			f := &testPorts{failAt: n, fail: errors.New("temporary")}
			s := &Store{Path: t.TempDir() + "/store.json"}
			p, e := BuildPlan(Observed{}, Create, Desired{Labels: []Label{{Name: "bonsai:managed", Description: "managed"}}, ProjectName: "Garden", Fields: []garden.ProjectField{{Name: "Status"}}})
			if e != nil {
				t.Fatal(e)
			}
			res, e := Apply(context.Background(), s, "local", 7, AccessWrite, "op", p, f.ports())
			if e == nil {
				t.Fatal("injected failure ignored")
			}
			f.failAt = 0
			res, e = Resume(context.Background(), s, "op", 7, f.ports(), "local", AccessWrite)
			if e != nil {
				t.Fatal(e)
			}
			if len(f.labels) != 1 || len(f.projects) != 1 || len(f.projects[0].Fields) != 1 {
				t.Fatalf("duplicates or missing work: %#v", f)
			}
			if len(res.Operation.Steps) != 3 {
				t.Fatal(res.Operation)
			}
		})
	}
}

func TestRemoteCreateThenLocalWriteFailureAdoptsMarker(t *testing.T) {
	f := &testPorts{}
	s := &Store{Path: t.TempDir() + "/store.json"}
	writes := 0
	s.writeJSON = func(path string, b []byte) error {
		writes++
		if writes == 2 {
			return errors.New("local write failed")
		}
		return gitstore.WriteJSON(path, b)
	}
	p, _ := BuildPlan(Observed{}, Create, Desired{ProjectName: "Garden"})
	if _, e := Apply(context.Background(), s, "local", 9, AccessWrite, "op", p, f.ports()); e == nil {
		t.Fatal("expected local write failure")
	}
	s.writeJSON = nil
	res, e := Resume(context.Background(), s, "op", 9, f.ports(), "local", AccessWrite)
	if e != nil {
		t.Fatal(e)
	}
	if len(f.projects) != 1 || res.Operation.Steps[0].Status != Done {
		t.Fatalf("failed to adopt: %#v %#v", f.projects, res)
	}
}

func TestCanceledApplyPersistsResumableOperation(t *testing.T) {
	f := &testPorts{}
	s := &Store{Path: t.TempDir() + "/store.json"}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p, _ := BuildPlan(Observed{}, Create, Desired{Labels: []Label{{Name: "bonsai:managed"}}})
	_, e := Apply(ctx, s, "local", 3, AccessWrite, "cancel-op", p, f.ports())
	if e == nil {
		t.Fatal("expected cancellation")
	}
	if _, e = s.Operation(context.Background(), "cancel-op"); e != nil {
		t.Fatal(e)
	}
	_, e = Resume(context.Background(), s, "cancel-op", 3, f.ports(), "local", AccessWrite)
	if e != nil {
		t.Fatal(e)
	}
}

func TestConcurrentStoreWritersAndOperationLock(t *testing.T) {
	s := &Store{Path: t.TempDir() + "/store.json"}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, e := s.Put(context.Background(), 0, fmt.Sprint("k", i), Association{LocalRepoID: garden.LocalRepoID(fmt.Sprint("local", i)), GardenID: garden.GardenID{Repo: 5}, Mode: Skip})
			errs <- e
		}(i)
	}
	wg.Wait()
	close(errs)
	success := 0
	for e := range errs {
		if e == nil {
			success++
		} else if !errors.Is(e, ErrRevision) {
			t.Fatal(e)
		}
	}
	if success != 1 {
		t.Fatalf("successful stale-writer count %d", success)
	}
	_ = os.MkdirAll(filepath.Dir(s.Path), 0700)
	lock, e := procstore.TryLock(localOperationLockPath(s.Path, "busy"))
	if e != nil {
		t.Fatal(e)
	}
	defer lock.Unlock()
	p := Plan{Mode: Skip}
	if _, e = Apply(context.Background(), s, "busy", 5, AccessWrite, "busy-op", p, SetupPorts{Labels: &testPorts{}, LabelWrites: &testPorts{}}); !errors.Is(e, ErrOperationInProgress) {
		t.Fatalf("concurrent apply: %v", e)
	}
}

func TestLabelColorAndDescriptionDifferencesReuseButExtraManagedSchemeConflicts(t *testing.T) {
	want := Label{Name: "bonsai:managed", Color: "00ff00", Description: "managed"}
	p, e := BuildPlan(Observed{Labels: []Label{{Name: "bonsai:managed", Color: "ff0000", Description: "older text"}, {Name: "bonsai:stage/legacy", Description: "old scheme"}}}, Create, Desired{Labels: []Label{want}})
	if e != nil {
		t.Fatal(e)
	}
	if p.Steps[0].Kind != ReuseLabel || p.Steps[0].Label.Color != "ff0000" || p.Steps[0].Instructions == "" {
		t.Fatalf("difference should be reused unchanged: %#v", p.Steps[0])
	}
	if p.Steps[1].Kind != ConflictLabel {
		t.Fatalf("second scheme not conflict: %#v", p.Steps)
	}
}

func TestConnectExistingValidatesOwnerAndOnlyAddsMissingFields(t *testing.T) {
	pr := garden.Project{ID: "node-1", Number: "12", Owner: "alice", Fields: []garden.ProjectField{{Name: "Status"}}}
	d := Desired{Owner: "alice", ExistingProject: &ProjectRef{Number: "12", Owner: "alice"}, Fields: []garden.ProjectField{{Name: "Status"}, {Name: "Priority"}}}
	p, e := BuildPlan(Observed{Projects: []garden.Project{pr}}, ConnectExisting, d)
	if e != nil {
		t.Fatal(e)
	}
	if len(p.Steps) != 1 || p.Steps[0].Kind != EnsureProjectField || p.Steps[0].ProjectName != "node-1" || p.Steps[0].Field.Name != "Priority" {
		t.Fatalf("unexpected steps: %#v", p.Steps)
	}
	d.Owner = "mallory"
	if _, e = BuildPlan(Observed{Projects: []garden.Project{pr}}, ConnectExisting, d); !errors.Is(e, ErrProjectOwner) {
		t.Fatalf("wrong owner: %v", e)
	}
	d.Owner = "alice"
	d.ExistingProject.Number = "404"
	if _, e = BuildPlan(Observed{Projects: []garden.Project{pr}}, ConnectExisting, d); !errors.Is(e, ErrProjectNotFound) {
		t.Fatalf("missing project: %v", e)
	}
}

func TestSameNameProjectIsConflictAndBlocksProjectWrites(t *testing.T) {
	p, err := BuildPlan(Observed{Projects: []garden.Project{{ID: "foreign", Name: "Garden", Owner: "alice"}}}, Create, Desired{ProjectName: "Garden", Fields: []garden.ProjectField{{Name: "Status"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Steps) != 1 || p.Steps[0].Kind != ConflictProject {
		t.Fatalf("plan adopted same-name project: %#v", p)
	}
	f := &testPorts{projects: []garden.Project{{ID: "foreign", Name: "Garden", Owner: "alice"}}}
	s := &Store{Path: t.TempDir() + "/store.json"}
	result, err := Apply(context.Background(), s, "local", 18, AccessWrite, "same-name", p, f.ports())
	if !errors.Is(err, ErrProjectConflict) || UserMessage(err) != "Select that Project explicitly with ConnectExisting if it is yours." || len(result.Conflicts) != 1 || len(f.projects[0].Fields) != 0 {
		t.Fatalf("same-name Project was adopted/written: result=%#v projects=%#v err=%v", result, f.projects, err)
	}
}

func TestResumeRejectsSpoofedProjectAdoption(t *testing.T) {
	for _, tc := range []struct {
		name            string
		creator, marker string
	}{
		{"foreign creator", "other", "keep"},
		{"wrong nonce", "viewer", "wrong"},
		{"unknown creator", "", "keep"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &testPorts{}
			s := &Store{Path: t.TempDir() + "/store.json"}
			p, _ := BuildPlan(Observed{}, Create, Desired{ProjectName: "Garden"})
			if _, err := Apply(context.Background(), s, "local", 9, AccessWrite, "spoof", p, f.ports()); err != nil {
				t.Fatal(err)
			}
			op, _ := s.Operation(context.Background(), "spoof")
			op.Steps[0].Status = Pending
			op.Steps[0].Attempted = true
			if err := s.saveOperation(context.Background(), op); err != nil {
				t.Fatal(err)
			}
			f.projects[0].CreatorLogin = tc.creator
			if tc.marker == "wrong" {
				f.projects[0].Description = "forged marker"
			}
			res, err := Resume(context.Background(), s, "spoof", 9, f.ports(), "local", AccessWrite)
			if !errors.Is(err, ErrProjectConflict) || UserMessage(err) != "Select that Project explicitly with ConnectExisting if it is yours." || len(res.Conflicts) != 1 || f.calls != 1 {
				t.Fatalf("spoof accepted or written: result=%#v calls=%d err=%v", res, f.calls, err)
			}
			if !strings.Contains(res.Conflicts[0].Instructions, "ConnectExisting") {
				t.Fatalf("missing explicit selection guidance: %#v", res.Conflicts[0])
			}
		})
	}
}

func TestResumeAdoptsProjectUsingCaseInsensitiveCreatorLoginAndNonce(t *testing.T) {
	f := &testPorts{}
	store := &Store{Path: t.TempDir() + "/store.json"}
	plan, _ := BuildPlan(Observed{}, Create, Desired{ProjectName: "Garden"})
	if _, err := Apply(context.Background(), store, "local", 44, AccessWrite, "login-adoption", plan, f.ports()); err != nil {
		t.Fatal(err)
	}
	op, _ := store.Operation(context.Background(), "login-adoption")
	op.Steps[0].Status = Pending
	op.Steps[0].Attempted = true
	if err := store.saveOperation(context.Background(), op); err != nil {
		t.Fatal(err)
	}
	f.projects[0].CreatorLogin = "VIEWER"
	result, err := Resume(context.Background(), store, "login-adoption", 44, f.ports(), "local", AccessWrite)
	if err != nil || len(result.Conflicts) != 0 || result.Operation.Steps[0].Status != Done {
		t.Fatalf("case-insensitive creator login not adopted: %#v %v", result, err)
	}
}

func TestPlanRejectsProjectConflictWithAnyProjectWrite(t *testing.T) {
	p := Plan{Mode: Create, Steps: []Step{{Kind: ConflictProject, ProjectName: "Garden"}, {Kind: CreateProject, ProjectName: "Garden"}}}
	if err := p.Validate(); err == nil {
		t.Fatal("contradictory Project plan validated")
	}
	ports := &testPorts{}
	if _, err := Apply(context.Background(), &Store{Path: t.TempDir() + "/s.json"}, "local", 9, AccessWrite, "contradiction", p, ports.ports()); err == nil || ports.calls != 0 {
		t.Fatalf("hand-built conflict plan performed work: calls=%d err=%v", ports.calls, err)
	}
}

func TestApplyRevalidatesProjectAndLabelBeforeWrites(t *testing.T) {
	p, _ := BuildPlan(Observed{}, Create, Desired{ProjectName: "Garden", Labels: []Label{{Name: "bonsai:managed", Color: "00ff00", Description: "managed"}}})
	f := &testPorts{projects: []garden.Project{{ID: "foreign", Name: "Garden"}}, labels: []Label{{Name: "bonsai:managed", Color: "ff0000", Description: "other"}}}
	res, err := Apply(context.Background(), &Store{Path: t.TempDir() + "/s.json"}, "local", 9, AccessWrite, "revalidate", p, f.ports())
	if !errors.Is(err, ErrProjectConflict) || len(res.Conflicts) != 2 || f.calls != 0 {
		t.Fatalf("revalidation failed: result=%#v calls=%d err=%v", res, f.calls, err)
	}
}

func TestApplyReusesCompatibleLabelAppearingAfterPlan(t *testing.T) {
	want := Label{Name: "bonsai:managed", Color: "00ff00", Description: "managed"}
	p, _ := BuildPlan(Observed{}, Create, Desired{Labels: []Label{want}})
	f := &testPorts{labels: []Label{want}}
	res, err := Apply(context.Background(), &Store{Path: t.TempDir() + "/s.json"}, "local", 9, AccessWrite, "late-compatible-label", p, f.ports())
	if err != nil || f.calls != 0 || res.Operation.Steps[0].Status != Done {
		t.Fatalf("compatible concurrent label was not reused: result=%#v calls=%d err=%v", res, f.calls, err)
	}
}

func TestApplySurfacesObjectsLostInObserveCreateRace(t *testing.T) {
	t.Run("label", func(t *testing.T) {
		want := Label{Name: "bonsai:managed", Color: "00ff00", Description: "managed"}
		f := &testPorts{raceLabel: &Label{Name: want.Name, Color: "ff0000", Description: "foreign"}}
		p, _ := BuildPlan(Observed{}, Create, Desired{Labels: []Label{want}})
		res, err := Apply(context.Background(), &Store{Path: t.TempDir() + "/s.json"}, "local", 9, AccessWrite, "label-race", p, f.ports())
		if err != nil || len(res.Conflicts) != 1 || res.Conflicts[0].Kind != ConflictLabel {
			t.Fatalf("lost label race not surfaced: %#v %v", res, err)
		}
	})
	t.Run("project", func(t *testing.T) {
		f := &testPorts{raceProject: true}
		p, _ := BuildPlan(Observed{}, Create, Desired{ProjectName: "Garden"})
		res, err := Apply(context.Background(), &Store{Path: t.TempDir() + "/s.json"}, "local", 9, AccessWrite, "project-race", p, f.ports())
		if !errors.Is(err, ErrProjectConflict) || len(res.Conflicts) != 1 || res.Conflicts[0].Kind != ConflictProject {
			t.Fatalf("lost Project race not surfaced: %#v %v", res, err)
		}
	})
}

func TestResumePersistsReconcileBeforeAnyRemoteWrite(t *testing.T) {
	f := &testPorts{}
	s := &Store{Path: t.TempDir() + "/store.json"}
	p, _ := BuildPlan(Observed{}, Create, Desired{Labels: []Label{{Name: "bonsai:managed", Color: "fff"}}})
	if _, err := Apply(context.Background(), s, "local", 17, AccessWrite, "resume-reconcile", p, f.ports()); err != nil {
		t.Fatal(err)
	}
	f.labels = nil
	base := s.writeJSON
	s.writeJSON = func(path string, b []byte) error { return errors.New("injected after reconcile") }
	if _, err := Resume(context.Background(), s, "resume-reconcile", 17, f.ports(), "local", AccessWrite); err == nil {
		t.Fatal("expected reconcile persistence failure")
	}
	if len(f.labels) != 0 {
		t.Fatalf("remote write happened before reconcile persistence: %#v", f.labels)
	}
	s.writeJSON = base
}

func TestStoreRevisionSanitizationAndCloneIdentity(t *testing.T) {
	s := &Store{Path: t.TempDir() + "/assoc.json"}
	ctx := context.Background()
	a := Association{LocalRepoID: garden.LocalRepoID("local-a"), GardenID: garden.GardenID{Repo: 44}, RemoteFullName: "org/repo", Mode: Skip, Identity: GitHubIdentity{ID: "7", Login: "user"}}
	rev, err := s.Put(ctx, 0, "req-a", a)
	if err != nil || rev != 1 {
		t.Fatalf("Put = %d, %v", rev, err)
	}
	if _, err = s.Put(ctx, 0, "req-b", a); !errors.Is(err, ErrRevision) {
		t.Fatalf("stale revision: %v", err)
	}
	a.LocalRepoID = "local-b"
	if _, err = s.Put(ctx, 1, "req-b", a); err != nil {
		t.Fatal(err)
	}
	if ExecutionKey("local-a") == ExecutionKey("local-b") {
		t.Fatal("clone execution keys collided")
	}
	b, err := os.ReadFile(s.Path)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"token-fake-secret", "header-fake-secret", "command-fake-secret"} {
		if strings.Contains(string(b), secret) {
			t.Fatalf("persisted secret %q", secret)
		}
	}
}

func TestPutAndRemoveIdempotencyReplayOriginalRevision(t *testing.T) {
	s := &Store{Path: t.TempDir() + "/s.json"}
	ctx := context.Background()
	a := Association{LocalRepoID: "local", GardenID: garden.GardenID{Repo: 7}, Mode: Skip}
	r1, err := s.Put(ctx, 0, "put", a)
	if err != nil {
		t.Fatal(err)
	}
	other := a
	other.LocalRepoID = "other"
	if _, err = s.Put(ctx, r1, "other-put", other); err != nil {
		t.Fatal(err)
	}
	if replay, err := s.Put(ctx, 0, "put", a); err != nil || replay != r1 {
		t.Fatalf("Put replay=%d err=%v, want %d", replay, err, r1)
	}
	if _, err = s.Put(ctx, r1, "put", other); !errors.Is(err, ErrIdempotency) {
		t.Fatalf("different payload: %v", err)
	}
	r3, err := s.Remove(ctx, 2, "remove", "local")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.Put(ctx, r3, "later", Association{LocalRepoID: "third", GardenID: garden.GardenID{Repo: 8}, Mode: Skip}); err != nil {
		t.Fatal(err)
	}
	if replay, err := s.Remove(ctx, 2, "remove", "local"); err != nil || replay != r3 {
		t.Fatalf("Remove replay=%d err=%v want %d", replay, err, r3)
	}
	if _, err = s.Remove(ctx, 3, "remove", "other"); !errors.Is(err, ErrIdempotency) {
		t.Fatalf("remove payload change: %v", err)
	}
}

func TestSkipAssociationIsReversibleAndErrorsDoNotLeakSecrets(t *testing.T) {
	s := &Store{Path: t.TempDir() + "/state.json"}
	a := Association{LocalRepoID: "local", GardenID: garden.GardenID{Repo: 2}, Mode: Skip}
	rev, err := s.Put(context.Background(), 0, "skip", a)
	if err != nil {
		t.Fatal(err)
	}
	_, items, err := s.Snapshot(context.Background())
	if err != nil || len(items) != 1 || items[0].Mode != Skip {
		t.Fatalf("Skip not persisted: %#v %v", items, err)
	}
	rev, err = s.Remove(context.Background(), rev, "undo-skip", a.LocalRepoID)
	if err != nil {
		t.Fatal(err)
	}
	_, items, err = s.Snapshot(context.Background())
	if err != nil || len(items) != 0 {
		t.Fatalf("Skip not reversible: %#v %v rev %d", items, err, rev)
	}
	secret := "ghp_fake_token_header_body"
	safe := safeError(errors.New(secret))
	if strings.Contains(safe.Error(), secret) {
		t.Fatalf("error leaked secret: %s", safe)
	}
}

func FuzzBuildPlan(f *testing.F) {
	f.Add("managed", "draft", uint8(0))
	f.Add("odd", "ready", uint8(1))
	f.Fuzz(func(t *testing.T, label, description string, modeByte uint8) {
		mode := Create
		if modeByte%2 == 1 {
			mode = ConnectExisting
		}
		p, err := BuildPlan(Observed{Labels: []Label{{Name: label, Description: description}}}, mode, Desired{Labels: []Label{{Name: "bonsai:managed", Description: "managed"}}, ImportCount: 0})
		if err != nil {
			return
		}
		if p.Validate() != nil || p.ImportCount != 0 {
			t.Fatalf("invalid plan: %#v", p)
		}
		for _, s := range p.Steps {
			if s.Kind == "rename-label" || s.Kind == "delete-label" || s.Kind == "enroll-issues" {
				t.Fatalf("forbidden step %q", s.Kind)
			}
		}
	})
}
