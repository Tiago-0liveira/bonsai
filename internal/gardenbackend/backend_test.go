package gardenbackend

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/connect"
	gardensync "github.com/Tiago-0liveira/bonsai/internal/core/garden/sync"
	"github.com/Tiago-0liveira/bonsai/internal/git/github/ghcli"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
)

var ctx = context.Background()

func TestSameIDRemotesMergeAsAliases(t *testing.T) {
	h := newHarness(t, connect.AccessWrite)
	v, err := h.b.Candidates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if v.Ambiguous || len(v.Candidates) != 1 {
		t.Fatalf("same-ID remotes must be one candidate: %+v", v)
	}
	c := v.Candidates[0]
	if c.RepoID != uint64(acmeID) || strings.Join(c.Aliases, ",") != "origin,upstream" || c.Access != gardenapi.AccessWrite {
		t.Fatalf("candidate = %+v", c)
	}
	st, err := h.b.Status(ctx)
	if err != nil || st.Ambiguous || st.Target.RepoID != uint64(acmeID) || strings.Join(st.Target.Aliases, ",") != "origin,upstream" {
		t.Fatalf("status target = %+v err %v", st.Target, err)
	}
	if st.State != gardenapi.StateNone || !st.Caps.Setup || !st.Caps.Skip || st.Identity.Login != viewerName {
		t.Fatalf("status = %+v", st)
	}
}

func TestDifferentIDRemotesAreAmbiguousUntilChosen(t *testing.T) {
	h := newHarness(t, connect.AccessWrite)
	h.gh.Remotes = append(h.gh.Remotes, connect.Remote{Name: "fork", URL: forkURL})
	h.gh.Repositories[forkURL] = connect.Repository{ID: forkID, FullName: "octo-dev/my-app", Fork: true, ParentID: acmeID, ParentFullName: "acme/my-app", Access: connect.AccessAdmin, ProjectScopeKnown: true, ProjectScope: true}

	v, err := h.b.Candidates(ctx)
	if err != nil || !v.Ambiguous || len(v.Candidates) != 2 {
		t.Fatalf("Candidates = %+v, %v", v, err)
	}
	byID := map[uint64]gardenapi.Target{}
	for _, c := range v.Candidates {
		byID[c.RepoID] = c
	}
	if byID[uint64(forkID)].Kind != gardenapi.KindFork || byID[uint64(forkID)].ParentFullName != "acme/my-app" || byID[uint64(forkID)].Access != gardenapi.AccessAdmin {
		t.Fatalf("fork candidate = %+v", byID[uint64(forkID)])
	}
	if a := strings.Join(byID[uint64(acmeID)].Aliases, ","); !strings.Contains(a, "origin") || !strings.Contains(a, "upstream") {
		t.Fatalf("acme aliases = %q", a)
	}
	st, _ := h.b.Status(ctx)
	if !st.Ambiguous || st.Caps.Setup || st.Target.RepoID != 0 {
		t.Fatalf("ambiguous status = %+v", st)
	}
	if _, err := h.b.PreviewPlan(ctx, gardenapi.PlanRequest{Mode: gardenapi.ModeCreate}); err == nil {
		t.Fatal("preview without a chosen target must fail on ambiguity")
	} else {
		mustErrCode(t, err, gardenapi.CodeTargetAmbiguous)
	}
	p := h.preview(h.b, gardenapi.PlanRequest{Mode: gardenapi.ModeCreate, Target: gardenapi.TargetRef{RepoID: uint64(acmeID)}})
	if p.Target.RepoID != uint64(acmeID) {
		t.Fatalf("chosen target = %+v", p.Target)
	}
	_, err = h.b.PreviewPlan(ctx, gardenapi.PlanRequest{Mode: gardenapi.ModeCreate, Target: gardenapi.TargetRef{RepoID: 999}})
	mustErrCode(t, err, gardenapi.CodeIdentityChanged)
}

func TestCreatePreviewImportsNothingAndUsesCanonicalLabels(t *testing.T) {
	h := newHarness(t, connect.AccessWrite)
	h.gh.Labels = append(h.gh.Labels, connect.Label{Name: "bonsai:type/bug", Color: "1d76db", Description: "Garden item type"})
	p := h.preview(h.b, gardenapi.PlanRequest{Mode: gardenapi.ModeCreate})

	if p.Summary.ImportCount != 0 || !p.Summary.CanApply || p.Summary.CreateLabels != 10 || p.Summary.ReuseLabels != 1 ||
		!p.Summary.CreateProject || p.Summary.ProjectName != DefaultProjectName || p.Summary.AddFields != 2 || p.Summary.ProjectOwner != "acme" {
		t.Fatalf("summary = %+v", p.Summary)
	}
	var targets []string
	for _, r := range p.Rows {
		targets = append(targets, string(r.Change)+" "+string(r.Kind)+" "+r.Target)
	}
	want := []string{
		"create label bonsai:managed",
		"create labels bonsai:type/feature, chore, research",
		"reuse label bonsai:type/bug",
		"create labels bonsai:stage/draft … done",
		"create project acme / Bonsai Garden",
		"add field Stage  (single select, 6 options)",
		"add field Bonsai Type  (single select, 4 options)",
		"manual view Board layout for the Project",
	}
	if strings.Join(targets, "|") != strings.Join(want, "|") {
		t.Fatalf("rows:\n%s\nwant:\n%s", strings.Join(targets, "\n"), strings.Join(want, "\n"))
	}
	if !p.Rows[len(p.Rows)-1].Advisory {
		t.Fatal("browser layout row must be advisory")
	}
	// The planned labels are exactly the contract's canonical set, nothing more.
	got := map[string]bool{}
	for _, l := range desiredLabels() {
		got[l.Name] = true
	}
	for _, name := range []string{"bonsai:managed", "bonsai:type/feature", "bonsai:type/bug", "bonsai:type/chore", "bonsai:type/research",
		"bonsai:stage/draft", "bonsai:stage/ready", "bonsai:stage/in-progress", "bonsai:stage/review", "bonsai:stage/blocked", "bonsai:stage/done"} {
		delete(got, name)
	}
	if len(got) != 0 || len(desiredLabels()) != 11 {
		t.Fatalf("non-canonical labels planned: %v", got)
	}
	if h.calls() != 0 || h.labelCount() != 1 || h.projectCount() != 0 {
		t.Fatal("preview wrote to GitHub")
	}
	if _, a, _ := h.b.association(ctx); a != nil {
		t.Fatal("preview recorded an association")
	}
}

func TestSameNameProjectIsFlaggedAndNeverAdoptedByCreate(t *testing.T) {
	h := newHarness(t, connect.AccessWrite)
	h.gh.Projects = []garden.Project{
		{ID: "PVT_4", Number: "4", Name: "Roadmap", Owner: "acme", Fields: []garden.ProjectField{{Name: "Stage"}}},
		{ID: "PVT_7", Number: "7", Name: "Bonsai Garden", Owner: "acme", Fields: []garden.ProjectField{{Name: "Stage"}, {Name: TypeField}}},
	}
	pv, err := h.b.Projects(ctx, gardenapi.TargetRef{})
	if err != nil || len(pv.Projects) != 2 || pv.Owner != "acme" {
		t.Fatalf("Projects = %+v, %v", pv, err)
	}
	roadmap, same := pv.Projects[0], pv.Projects[1]
	if roadmap.Conflict || roadmap.Stage != gardenapi.FieldPresent || roadmap.Type != gardenapi.FieldMissing || roadmap.Compatible() {
		t.Fatalf("roadmap = %+v", roadmap)
	}
	if !same.Conflict || !same.Compatible() {
		t.Fatalf("same-name project = %+v", same)
	}

	create := h.preview(h.b, gardenapi.PlanRequest{Mode: gardenapi.ModeCreate})
	if create.Summary.CanApply || create.Summary.Conflicts != 1 || create.Summary.CreateProject {
		t.Fatalf("create summary = %+v", create.Summary)
	}
	var conflictRow *gardenapi.PlanRow
	for i := range create.Rows {
		if create.Rows[i].Conflict {
			conflictRow = &create.Rows[i]
		}
	}
	if conflictRow == nil || conflictRow.Kind != gardenapi.RowProject || conflictRow.Change != gardenapi.ChangeManual {
		t.Fatalf("no project conflict row in %+v", create.Rows)
	}
	_, err = h.b.Apply(ctx, create)
	mustErrCode(t, err, gardenapi.CodeProjectConflict)
	if h.calls() != 0 || h.labelCount() != 0 || h.projectCount() != 2 {
		t.Fatal("conflicting plan wrote to GitHub")
	}
	if _, a, _ := h.b.association(ctx); a != nil {
		t.Fatal("refused apply left an association")
	}

	connectPlan := h.preview(h.b, gardenapi.PlanRequest{Mode: gardenapi.ModeConnectExisting, Project: &gardenapi.ProjectRef{ID: "PVT_4", Number: "4"}})
	if !connectPlan.Summary.CanApply || connectPlan.Summary.CreateProject || connectPlan.Summary.AddFields != 1 || connectPlan.Summary.ImportCount != 0 {
		t.Fatalf("connect summary = %+v", connectPlan.Summary)
	}
	_, err = h.b.PreviewPlan(ctx, gardenapi.PlanRequest{Mode: gardenapi.ModeConnectExisting, Project: &gardenapi.ProjectRef{ID: "PVT_missing"}})
	mustErrCode(t, err, gardenapi.CodeProjectNotFound)
	_, err = h.b.PreviewPlan(ctx, gardenapi.PlanRequest{Mode: gardenapi.ModeConnectExisting})
	mustErrCode(t, err, gardenapi.CodeProjectNotFound)

	// Connect existing applies against the chosen Project and records it.
	op, err := h.b.Apply(ctx, connectPlan)
	if err != nil || op.Err != nil || op.State != gardenapi.StateConnected {
		t.Fatalf("Apply = %+v, %v", op, err)
	}
	if _, a, _ := h.b.association(ctx); a == nil || a.Project.ID != "PVT_4" || a.Mode != connect.ConnectExisting || a.ActiveOperationID != "" {
		t.Fatalf("association = %+v", a)
	}
	if h.projectCount() != 2 {
		t.Fatal("connect existing created a Project")
	}
}

func TestSkipUnskipRoundTripBumpsEpoch(t *testing.T) {
	h := newHarness(t, connect.AccessWrite)
	var gate gardenapi.Gate
	first, err := h.b.Status(ctx)
	if err != nil || !gate.Accept(first.Stamp) || first.Stamp.Epoch == 0 {
		t.Fatalf("first = %+v, %v", first.Stamp, err)
	}
	skipped, err := h.b.Skip(ctx, gardenapi.TargetRef{})
	if err != nil {
		t.Fatal(err)
	}
	if skipped.State != gardenapi.StateSkipped || !skipped.Caps.Unskip || skipped.Caps.Setup || skipped.Stamp.Epoch <= first.Stamp.Epoch || !gate.Accept(skipped.Stamp) {
		t.Fatalf("skipped = %+v", skipped)
	}
	if _, a, _ := h.b.association(ctx); a == nil || a.Mode != connect.Skip || a.GardenID.Repo != acmeID {
		t.Fatalf("stored association = %+v", a)
	}
	_, err = h.b.PreviewPlan(ctx, gardenapi.PlanRequest{Mode: gardenapi.ModeCreate})
	mustErrCode(t, err, gardenapi.CodeSkipped)

	again, err := h.b.Skip(ctx, gardenapi.TargetRef{})
	if err != nil || again.State != gardenapi.StateSkipped || again.Stamp.Epoch != skipped.Stamp.Epoch {
		t.Fatalf("second skip must be a no-op: %+v, %v", again.Stamp, err)
	}

	back, err := h.b.Unskip(ctx)
	if err != nil || back.State != gardenapi.StateNone || !back.Caps.Setup || back.Stamp.Epoch <= skipped.Stamp.Epoch || !gate.Accept(back.Stamp) {
		t.Fatalf("unskip = %+v, %v", back, err)
	}
	if _, a, _ := h.b.association(ctx); a != nil {
		t.Fatalf("unskip left %+v", a)
	}
	if h.calls() != 0 {
		t.Fatal("skip/unskip wrote to GitHub")
	}
}

func TestPartialApplyOnMissingScopeThenResume(t *testing.T) {
	h := newHarness(t, connect.AccessWrite)
	repo := acmeRepo(connect.AccessWrite)
	repo.ProjectScope = false // known and absent
	h.gh.Repositories[acmeURL], h.gh.Repositories[acmeSSH] = repo, repo
	h.seedCanonicalLabels() // label steps run without the scope; only Project steps need it
	h.gh.MissingScope = true

	plan := h.preview(h.b, gardenapi.PlanRequest{Mode: gardenapi.ModeCreate})
	op, err := h.b.Apply(ctx, plan)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if op.Err == nil || op.Err.Code != gardenapi.CodeScopeMissing || op.Err.Fix != "gh auth refresh -s project" {
		t.Fatalf("operation error = %+v", op.Err)
	}
	if op.State != gardenapi.StatePartialSetup || op.Done != 11 || op.Total != 14 || op.ID == "" {
		t.Fatalf("operation = %+v", op)
	}
	var project, fields []gardenapi.OperationStep
	for _, s := range op.Steps {
		switch s.Row.Kind {
		case gardenapi.RowProject:
			project = append(project, s)
		case gardenapi.RowField:
			fields = append(fields, s)
		}
	}
	if len(project) != 1 || project[0].Status != gardenapi.StepFailed || project[0].ErrorCode != gardenapi.StepScopeMissing {
		t.Fatalf("project step = %+v", project)
	}
	for _, f := range fields {
		if f.Status != gardenapi.StepPending || f.ErrorCode != "" {
			t.Fatalf("a field of a Project that was never created must wait: %+v", f)
		}
	}

	st, err := h.b.Status(ctx)
	if err != nil || st.State != gardenapi.StatePartialSetup || st.Operation == nil || st.Operation.ID != op.ID || !st.Caps.Resume || st.Caps.Setup {
		t.Fatalf("status = %+v, %v", st, err)
	}
	if st.ProjectScope != gardenapi.PresenceAbsent {
		t.Fatalf("scope = %v", st.ProjectScope)
	}
	// Status recovers the scope failure from the durable step record.
	for _, s := range st.Operation.Steps {
		if s.Row.Kind == gardenapi.RowProject && s.ErrorCode != gardenapi.StepScopeMissing {
			t.Fatalf("status project step code = %q", s.ErrorCode)
		}
	}
	if st.Sync != string(gardenapi.StatePartialSetup) {
		t.Fatalf("sync = %q", st.Sync)
	}
	_, err = h.b.PreviewPlan(ctx, gardenapi.PlanRequest{Mode: gardenapi.ModeCreate})
	mustErrCode(t, err, gardenapi.CodeConflict)

	// Resume must reread credentials before resolving or retrying any writes.
	rereads := 0
	h.b.deps.InvalidateAuth = func() {
		rereads++
		h.gh.MissingScope = false
		repo.ProjectScope = true
		h.gh.Repositories[acmeURL], h.gh.Repositories[acmeSSH] = repo, repo
	}
	res, err := h.b.Resume(ctx, op.ID)
	if err != nil || res.Err != nil || res.State != gardenapi.StateConnected || res.Done != res.Total {
		t.Fatalf("Resume = %+v, %v", res, err)
	}
	if h.projectCount() != 1 || len(h.gh.Projects[0].Fields) != 2 {
		t.Fatalf("projects = %+v", h.gh.Projects)
	}
	if rereads != 1 {
		t.Fatalf("Resume reread credentials %d times", rereads)
	}
	final, _ := h.b.Status(ctx)
	if final.State != gardenapi.StateConnected || final.Operation != nil || final.ProjectID == "" || !final.Caps.Read || final.Sync != "connected" {
		t.Fatalf("final status = %+v", final)
	}
	_, err = h.b.Resume(ctx, "op-unknown")
	mustErrCode(t, err, gardenapi.CodeOperationNotFound)
}

func TestFullCreateGroupsLabelsAndConnects(t *testing.T) {
	h := newHarness(t, connect.AccessWrite)
	plan := h.preview(h.b, gardenapi.PlanRequest{Mode: gardenapi.ModeCreate})
	op, err := h.b.Apply(ctx, plan)
	if err != nil || op.Err != nil || op.State != gardenapi.StateConnected || op.Done != 14 || op.Total != 14 {
		t.Fatalf("Apply = %+v, %v", op, err)
	}
	if op.Steps[0].Title != "Create 11 labels" || op.Steps[0].Row.Count != 11 || op.Steps[0].Status != gardenapi.StepDone {
		t.Fatalf("first step = %+v", op.Steps[0])
	}
	if h.labelCount() != 11 || h.projectCount() != 1 {
		t.Fatalf("labels %d projects %d", h.labelCount(), h.projectCount())
	}
	if _, a, _ := h.b.association(ctx); a == nil || a.Project.ID == "" || a.Identity.Login != viewerName || a.Mode != connect.Create {
		t.Fatalf("association = %+v", a)
	}
	_, err = h.b.Apply(ctx, plan)
	mustErrCode(t, err, gardenapi.CodeConflict)
}

func TestStatusRereadsCredentialsAndIdentity(t *testing.T) {
	h := newHarness(t, connect.AccessWrite)
	repo := acmeRepo(connect.AccessWrite)
	repo.ProjectScope = false
	h.gh.Repositories[acmeURL], h.gh.Repositories[acmeSSH] = repo, repo
	first, err := h.b.Status(ctx)
	if err != nil || first.ProjectScope != gardenapi.PresenceAbsent || first.Identity.Login != viewerName {
		t.Fatalf("initial status = %+v, %v", first, err)
	}
	h.b.deps.InvalidateAuth = func() {
		repo.ProjectScope = true
		h.gh.Repositories[acmeURL], h.gh.Repositories[acmeSSH] = repo, repo
		h.gh.ViewerLogin = "new-viewer"
	}
	second, err := h.b.Status(ctx)
	if err != nil || second.ProjectScope != gardenapi.PresencePresent || second.Identity.Login != "new-viewer" {
		t.Fatalf("refreshed status = %+v, %v", second, err)
	}
}

func TestIdentityBeforeCredentialChangeCannotReplaceNewIdentity(t *testing.T) {
	h := newHarness(t, connect.AccessWrite)
	started, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	h.b.deps.Viewer = func(context.Context) (connect.GitHubIdentity, error) {
		if calls.Add(1) == 1 {
			close(started)
			<-release
			return connect.GitHubIdentity{ID: "1", Login: "old-viewer"}, nil
		}
		return connect.GitHubIdentity{ID: "2", Login: "new-viewer"}, nil
	}
	go func() {
		defer close(done)
		_, _ = h.b.identity(ctx)
	}()
	<-started
	h.b.invalidateAuth()
	id, err := h.b.identity(ctx)
	close(release)
	<-done
	if err != nil || id.Login != "new-viewer" {
		t.Fatalf("new identity = %+v, %v", id, err)
	}
	if id, err = h.b.identity(ctx); err != nil || id.Login != "new-viewer" || calls.Load() != 2 {
		t.Fatalf("cached identity after late response = %+v, %v", id, err)
	}
}

func TestGraphQLFailureSurvivesRestartAndResumeKeepsCompletedSteps(t *testing.T) {
	h := newHarness(t, connect.AccessWrite)
	plan := h.preview(h.b, gardenapi.PlanRequest{Mode: gardenapi.ModeCreate})
	h.gh.FailAt = 13 // eleven labels and the Project succeed; Stage fails
	h.gh.Failure = fmt.Errorf("raw response ghp_SECRET https://private.test: %w", garden.ErrGraphQL)
	op, err := h.b.Apply(ctx, plan)
	if err != nil || op.Err == nil || op.Err.Code != gardenapi.CodeGraphQL || op.Done != 12 {
		t.Fatalf("Apply = %+v, %v", op, err)
	}
	if strings.Contains(op.Err.Message+op.Err.Next, "SECRET") || strings.Contains(op.Err.Message+op.Err.Next, "private.test") {
		t.Fatal("GraphQL error leaked the raw response")
	}
	// Rebuild the backend for this same clone to discard all in-memory errors.
	restarted, err := newBackend(t.TempDir(), h.b.deps)
	if err != nil {
		t.Fatal(err)
	}
	restarted.local = h.b.local
	st, err := restarted.Status(ctx)
	if err != nil || st.Operation == nil || st.Operation.Err == nil || st.Operation.Err.Code != gardenapi.CodeGraphQL {
		t.Fatalf("status after restart = %+v, %v", st, err)
	}
	found := false
	for _, step := range st.Operation.Steps {
		if step.Status == gardenapi.StepFailed {
			found = true
			if step.ErrorCode != gardenapi.StepGraphQL || !strings.Contains(step.Title, "Stage") {
				t.Fatalf("failed step = %+v", step)
			}
		}
	}
	if !found {
		t.Fatal("failed Stage step missing")
	}
	h.gh.FailAt, h.gh.Failure = 0, nil
	res, err := restarted.Resume(ctx, op.ID)
	if err != nil || res.Err != nil || res.State != gardenapi.StateConnected {
		t.Fatalf("Resume = %+v, %v", res, err)
	}
	if h.labelCount() != 11 || h.projectCount() != 1 || len(h.gh.Projects[0].Fields) != 2 || h.calls() != 15 {
		t.Fatalf("Resume repeated completed writes: labels %d projects %d calls %d", h.labelCount(), h.projectCount(), h.calls())
	}
}

func TestApplyRefusesPlanThatChangedSincePreview(t *testing.T) {
	h := newHarness(t, connect.AccessWrite)
	plan := h.preview(h.b, gardenapi.PlanRequest{Mode: gardenapi.ModeCreate})
	h.gh.Labels = append(h.gh.Labels, connect.Label{Name: "bonsai:managed", Color: "ededed"}) // appeared after the preview
	_, err := h.b.Apply(ctx, plan)
	mustErrCode(t, err, gardenapi.CodeStalePlan)
	if h.calls() != 0 || h.projectCount() != 0 {
		t.Fatal("stale plan wrote")
	}
	if _, a, _ := h.b.association(ctx); a != nil {
		t.Fatal("stale plan left an association")
	}
	forged := plan
	forged.PlanID = "forged"
	_, err = h.b.Apply(ctx, forged)
	mustErrCode(t, err, gardenapi.CodeStalePlan)
}

func TestReadOnlyAccessOffersNoSetup(t *testing.T) {
	h := newHarness(t, connect.AccessRead)
	h.gh.Labels = append(h.gh.Labels, connect.Label{Name: "bonsai:managed"})
	st, err := h.b.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.State != gardenapi.StateReadOnly || st.Access != gardenapi.AccessRead || !st.Caps.Read ||
		st.Caps.Setup || st.Caps.Resume || st.Caps.Skip || st.Caps.Unskip {
		t.Fatalf("read-only status = %+v", st)
	}
	if st.RemoteGarden != gardenapi.PresencePresent {
		t.Fatalf("remote garden = %v", st.RemoteGarden)
	}
	_, err = h.b.PreviewPlan(ctx, gardenapi.PlanRequest{Mode: gardenapi.ModeCreate})
	mustErrCode(t, err, gardenapi.CodeReadOnly)
	forged := gardenapi.PlanView{Request: gardenapi.PlanRequest{Mode: gardenapi.ModeCreate}, PlanID: "x"}
	_, err = h.b.Apply(ctx, forged)
	mustErrCode(t, err, gardenapi.CodeReadOnly)
	if _, err := h.b.Projects(ctx, gardenapi.TargetRef{}); err != nil {
		t.Fatalf("reading Projects needs only Read: %v", err)
	}
	if h.calls() != 0 {
		t.Fatal("read-only clone wrote")
	}
}

func TestRecordedAccessLossShowsReadOnly(t *testing.T) {
	h := newHarness(t, connect.AccessWrite)
	if _, err := h.b.Apply(ctx, h.preview(h.b, gardenapi.PlanRequest{Mode: gardenapi.ModeCreate})); err != nil {
		t.Fatal(err)
	}
	h.gh.Repositories[acmeURL] = acmeRepo(connect.AccessRead)
	h.gh.Repositories[acmeSSH] = acmeRepo(connect.AccessRead)
	st, _ := h.b.Status(ctx)
	if st.State != gardenapi.StateReadOnly || st.Caps.Setup || st.Caps.Resume {
		t.Fatalf("status = %+v", st)
	}
}

// blockingLabels holds the first label write until released.
type blockingLabels struct {
	inner   connect.LabelWriter
	once    sync.Once
	started chan struct{}
	release chan struct{}
}

func (w *blockingLabels) CreateLabel(c context.Context, r garden.RepoID, l connect.Label) error {
	w.once.Do(func() { close(w.started); <-w.release })
	return w.inner.CreateLabel(c, r, l)
}

func TestTwoClonesShareOneGardenWithoutSharingLocks(t *testing.T) {
	h := newHarness(t, connect.AccessWrite)
	a := h.b
	b := h.clone(t.TempDir())
	if a.Local() == b.Local() {
		t.Fatal("clones must have distinct LocalRepoIDs")
	}
	gate := &blockingLabels{inner: h.gh, started: make(chan struct{}), release: make(chan struct{})}
	a.deps.Ports.LabelWrites = gate

	planA := h.preview(a, gardenapi.PlanRequest{Mode: gardenapi.ModeCreate})
	doneA := make(chan gardenapi.OperationView, 1)
	go func() {
		op, err := a.Apply(ctx, planA)
		if err != nil {
			t.Errorf("clone A Apply: %v", err)
		}
		doneA <- op
	}()
	<-gate.started

	stA, err := a.Status(ctx)
	if err != nil || stA.State != gardenapi.StateOperationInProgress || stA.Caps.Setup || stA.Caps.Resume {
		t.Fatalf("clone A status while running = %+v, %v", stA, err)
	}
	stB, err := b.Status(ctx)
	if err != nil || stB.State != gardenapi.StateNone || !stB.Caps.Setup {
		t.Fatalf("clone B must not see A's lock: %+v, %v", stB, err)
	}
	opB, err := b.Apply(ctx, h.preview(b, gardenapi.PlanRequest{Mode: gardenapi.ModeCreate}))
	if err != nil || opB.Err != nil || opB.State != gardenapi.StateConnected {
		t.Fatalf("clone B must finish while A holds its own lock: %+v, %v", opB, err)
	}
	close(gate.release)
	opA := <-doneA
	// Both clones raced to create the same Project. A finds B's Project without its own
	// marker and reports a conflict instead of adopting it: locks are per clone, the
	// remote Project is shared.
	if opA.Err == nil || opA.Err.Code != gardenapi.CodeProjectConflict || opA.State != gardenapi.StatePartialSetup || opA.ID == opB.ID {
		t.Fatalf("clone A = %+v", opA)
	}
	_, all, err := h.store.Snapshot(ctx)
	if err != nil || len(all) != 2 {
		t.Fatalf("associations = %+v, %v", all, err)
	}
	for _, as := range all {
		if as.GardenID.Repo != acmeID {
			t.Fatalf("clones must share GardenID %d: %+v", acmeID, as)
		}
	}
	if h.projectCount() != 1 {
		t.Fatalf("projects = %d, want the one B created", h.projectCount())
	}
}

func TestStaleGenerationsAreDistinguishable(t *testing.T) {
	h := newHarness(t, connect.AccessWrite)
	other := h.clone(t.TempDir())
	var gate gardenapi.Gate
	old, _ := h.b.Status(ctx)
	newer, _ := h.b.Status(ctx)
	if old.Stamp.Generation >= newer.Stamp.Generation || old.Stamp.LocalRepoID != h.b.Local().String() || old.Stamp.GardenID != uint64(acmeID) {
		t.Fatalf("stamps %+v then %+v", old.Stamp, newer.Stamp)
	}
	if !gate.Accept(newer.Stamp) || gate.Accept(old.Stamp) {
		t.Fatal("the late reply must be dropped")
	}
	foreign, _ := other.Status(ctx)
	if foreign.Stamp.LocalRepoID == newer.Stamp.LocalRepoID || gate.Accept(foreign.Stamp) {
		t.Fatalf("another clone's reply must be dropped: %+v", foreign.Stamp)
	}
	cands, _ := h.b.Candidates(ctx)
	proj, _ := h.b.Projects(ctx, gardenapi.TargetRef{})
	plan := h.preview(h.b, gardenapi.PlanRequest{Mode: gardenapi.ModeCreate})
	if !(newer.Stamp.Generation < cands.Stamp.Generation && cands.Stamp.Generation < proj.Stamp.Generation && proj.Stamp.Generation < plan.Stamp.Generation) {
		t.Fatalf("generations must grow per request: %d %d %d %d", newer.Stamp.Generation, cands.Stamp.Generation, proj.Stamp.Generation, plan.Stamp.Generation)
	}
}

func TestApplyThatNeverStartsRemovesItsAssociation(t *testing.T) {
	h := newHarness(t, connect.AccessWrite)
	plan := h.preview(h.b, gardenapi.PlanRequest{Mode: gardenapi.ModeCreate})
	h.b.deps.Ports.LabelWrites = nil // connect refuses to start without a label writer
	if _, err := h.b.Apply(ctx, plan); err == nil {
		t.Fatal("apply without a label writer must fail")
	}
	if _, a, _ := h.b.association(ctx); a != nil {
		t.Fatalf("association left behind: %+v", a)
	}
	st, _ := h.b.Status(ctx)
	if st.State != gardenapi.StateNone {
		t.Fatalf("state = %s", st.State)
	}
}

func TestOfflineStatusKeepsRecordedState(t *testing.T) {
	h := newHarness(t, connect.AccessWrite)
	if _, err := h.b.Apply(ctx, h.preview(h.b, gardenapi.PlanRequest{Mode: gardenapi.ModeCreate})); err != nil {
		t.Fatal(err)
	}
	h.gh.Offline = true
	st, err := h.b.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st.State != gardenapi.StateConnected || st.Target.FullName != "acme/my-app" || st.Access != gardenapi.AccessWrite ||
		st.Caps.Refresh || st.Caps.Setup || st.RefreshError == nil || st.RefreshError.Code != gardenapi.CodeOffline || st.Identity.Login != viewerName {
		t.Fatalf("offline status = %+v", st)
	}
}

func TestRemoteMovedToAnotherRepositoryIsFlagged(t *testing.T) {
	h := newHarness(t, connect.AccessWrite)
	if _, err := h.b.Apply(ctx, h.preview(h.b, gardenapi.PlanRequest{Mode: gardenapi.ModeCreate})); err != nil {
		t.Fatal(err)
	}
	other := connect.Repository{ID: forkID, FullName: "octo-dev/my-app", Access: connect.AccessAdmin}
	h.gh.Repositories[acmeURL], h.gh.Repositories[acmeSSH] = other, other
	st, _ := h.b.Status(ctx)
	if !st.RemoteChanged || st.Target.RepoID != uint64(acmeID) || st.State != gardenapi.StateConnected {
		t.Fatalf("status = %+v", st)
	}
}

func TestErrorsNeverCarrySecretsOrURLs(t *testing.T) {
	secret := "ghp_SECRET https://user:pw@github.com/acme/my-app.git"
	h := newHarness(t, connect.AccessWrite)
	h.b.deps.Resolver = leakyResolver{err: errors.New(secret)}
	st, err := h.b.Status(ctx)
	if err != nil || st.RefreshError == nil || st.RefreshError.Code != gardenapi.CodeFailed {
		t.Fatalf("status = %+v, %v", st.RefreshError, err)
	}
	_, cerr := h.b.Candidates(ctx)
	_, perr := h.b.PreviewPlan(ctx, gardenapi.PlanRequest{Mode: gardenapi.ModeCreate})
	for _, text := range []string{st.RefreshError.Message, st.RefreshError.Next, cerr.Error(), perr.Error()} {
		if strings.Contains(text, "ghp_") || strings.Contains(text, "pw@") || strings.Contains(text, "github.com") {
			t.Fatalf("leaked into %q", text)
		}
	}
	// A credentialed remote is sanitized before the resolver sees it.
	h.gh.Remotes = []connect.Remote{{Name: "origin", URL: "https://user:pw@github.com/acme/my-app.git?x=1#f"}}
	seen := &recordingResolver{inner: h.gh}
	h.b.deps.Resolver = seen
	if _, err := h.b.Candidates(ctx); err != nil {
		t.Fatal(err)
	}
	if len(seen.urls) != 1 || strings.Contains(seen.urls[0], "pw") || strings.Contains(seen.urls[0], "user") {
		t.Fatalf("resolver saw %v", seen.urls)
	}
}

type leakyResolver struct{ err error }

func (r leakyResolver) ResolveRepository(context.Context, connect.Remote) (connect.Repository, error) {
	return connect.Repository{}, r.err
}

type recordingResolver struct {
	inner connect.RepositoryResolver
	urls  []string
}

func (r *recordingResolver) ResolveRepository(c context.Context, remote connect.Remote) (connect.Repository, error) {
	r.urls = append(r.urls, remote.URL)
	return r.inner.ResolveRepository(c, connect.Remote{Name: remote.Name, URL: acmeURL})
}

func TestConstructorFailsWithoutLoginInsteadOfPanicking(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("HOME", cfg)
	t.Setenv("XDG_CONFIG_HOME", cfg)
	cases := map[string]ghcli.TokenRunner{
		"gh missing": func(context.Context, string) ([]byte, []byte, error) { return nil, nil, exec.ErrNotFound },
		"logged out": func(context.Context, string) ([]byte, []byte, error) {
			return nil, []byte("You are not logged in to any GitHub hosts. Run gh auth login"), errors.New("exit status 1")
		},
	}
	for name, runner := range cases {
		shared := ghcli.NewShared()
		shared.SetTokenRunner(runner)
		b, err := New(t.TempDir(), WithShared(shared))
		if b != nil || !errors.Is(err, ErrAuthUnavailable) {
			t.Fatalf("%s: New = %v, %v", name, b, err)
		}
		mustErrCode(t, safeAPIError(err), gardenapi.CodeAuthRequired)
	}
}

func TestConstructorSucceedsWithStoredToken(t *testing.T) {
	cfg := t.TempDir()
	t.Setenv("HOME", cfg)
	t.Setenv("XDG_CONFIG_HOME", cfg)
	shared := ghcli.NewShared()
	shared.SetTokenRunner(func(context.Context, string) ([]byte, []byte, error) { return []byte("ghp_TOPSECRET\n"), nil, nil })
	b, err := New(t.TempDir(), WithShared(shared))
	if err != nil || b == nil {
		t.Fatalf("New = %v, %v", b, err)
	}
	if !strings.HasPrefix(b.deps.Store.Path, cfg) {
		t.Fatalf("store path %q is outside the temp config dir", b.deps.Store.Path)
	}
	if b.Local() == "" {
		t.Fatal("no LocalRepoID")
	}
	if b.deps.InvalidateAuth == nil {
		t.Fatal("production backend must support rereading stored credentials")
	}
}

func TestConstructorAcceptsOldGhThatCannotPrintTokens(t *testing.T) {
	shared := ghcli.NewShared()
	shared.SetTokenRunner(func(context.Context, string) ([]byte, []byte, error) {
		return nil, []byte("unknown command"), errors.New("exit 1")
	})
	if b, err := New(t.TempDir(), WithShared(shared), WithStore(&connect.Store{Path: t.TempDir() + "/g.json"})); err != nil || b == nil {
		t.Fatalf("gh without `auth token` falls back to gh api: %v, %v", b, err)
	}
}

func TestNewBackendRejectsMissingDeps(t *testing.T) {
	if _, err := newBackend(t.TempDir(), Deps{}); err == nil {
		t.Fatal("empty deps accepted")
	}
}

// The contract's states are the sync package's, not a second state machine.
func TestStatesMatchSyncAssociationStates(t *testing.T) {
	pairs := map[gardenapi.State]gardensync.AssociationState{
		gardenapi.StateNone: gardensync.AssociationNone, gardenapi.StateSkipped: gardensync.AssociationSkipped,
		gardenapi.StateReadOnly: gardensync.AssociationReadOnly, gardenapi.StateConnected: gardensync.AssociationConnected,
		gardenapi.StatePartialSetup: gardensync.AssociationPartialSetup, gardenapi.StateAwaitingManual: gardensync.AssociationAwaitingManual,
		gardenapi.StateOperationInProgress: gardensync.AssociationOperationInProgress,
	}
	for api, domain := range pairs {
		if string(api) != string(domain) {
			t.Errorf("%q != %q", api, domain)
		}
	}
}

func TestConnectExistingVerifiesRepositoryOwner(t *testing.T) {
	h := newHarness(t, connect.AccessWrite)
	h.gh.Projects = []garden.Project{
		{ID: "PVT_mine", Number: "1", Name: "Roadmap", Owner: "acme"},
		{ID: "PVT_viewer", Number: "2", Name: "Personal", Owner: viewerName},
	}
	req := func(id, n string) gardenapi.PlanRequest {
		return gardenapi.PlanRequest{Mode: gardenapi.ModeConnectExisting, Project: &gardenapi.ProjectRef{ID: id, Number: n}}
	}
	if _, err := h.b.PreviewPlan(ctx, req("PVT_mine", "1")); err != nil {
		t.Fatalf("repository owner Project refused: %v", err)
	}
	_, err := h.b.PreviewPlan(ctx, req("PVT_viewer", "2"))
	mustErrCode(t, err, gardenapi.CodeProjectOwner)
}
