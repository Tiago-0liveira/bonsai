package plan

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agentactivity"
	"github.com/Tiago-0liveira/bonsai/internal/core/config"
)

func TestStartPreparesPrivatePlanDirAndTellsThePlannerOnlyThatPath(t *testing.T) {
	f := newFixture(t)
	a := f.start()
	root, err := config.GardenPlansRoot()
	if err != nil {
		t.Fatal(err)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(a.PlanPath) || filepath.Base(a.PlanPath) != "plan.md" || !config.ContainsPath(root, a.PlanPath) {
		t.Fatalf("PlanPath = %q (root %q)", a.PlanPath, root)
	}
	dir := filepath.Dir(a.PlanPath)
	fi, err := os.Stat(dir)
	if err != nil || !fi.IsDir() {
		t.Fatalf("plan dir not created before the session: %v", err)
	}
	if runtime.GOOS != "windows" && fi.Mode().Perm() != 0o700 {
		t.Fatalf("plan dir mode = %v, want 0700", fi.Mode().Perm())
	}
	if !strings.HasSuffix(dir, filepath.Join("42", a.ID)) {
		t.Fatalf("dir %q is not <repo>/<issue>/<attempt>", dir)
	}
	l := f.agents.started[0].Launch
	if l.PlanFile != a.PlanPath || !l.ReadOnly {
		t.Fatalf("launch = %+v", l)
	}
	for _, want := range []string{a.PlanPath, "only file you can write", "then say in this conversation that it is written", "Keep discussing", "Build the thing."} {
		if !strings.Contains(l.Prompt, want) {
			t.Errorf("prompt lacks %q", want)
		}
	}
	for _, gone := range []string{"you cannot edit files", "give the plan as text in this conversation"} {
		if strings.Contains(l.Prompt, gone) {
			t.Errorf("prompt still claims %q", gone)
		}
	}
	got, err := f.store.Get(context.Background(), a.ID)
	if err != nil || got.PlanPath != a.PlanPath {
		t.Fatalf("stored PlanPath = %q, %v", got.PlanPath, err)
	}
}

func TestStartRefusesProviderThatCannotConfineThePlanFile(t *testing.T) {
	f := newFixture(t)
	acct := f.agents.accounts[claudeID]
	acct.ReadOnly.PlanFile = false
	f.agents.accounts[claudeID] = acct
	req := f.request()
	_, err := f.svc.Start(context.Background(), req)
	if !errors.Is(err, ErrReadOnlyUnsupported) || len(f.agents.started) != 0 {
		t.Fatalf("Start = %v, started %d", err, len(f.agents.started))
	}
	if list, _ := f.store.List(context.Background(), "local-1", testIssue); len(list) != 0 {
		t.Fatal("a refused start left a record")
	}
	root, _ := config.GardenPlansRoot()
	if entries, _ := filepath.Glob(filepath.Join(root, "*", "*", "*")); len(entries) != 0 {
		t.Fatalf("a refused start left plan directories: %v", entries)
	}
}

func TestPlanPathStoreRoundTripAndOldRecords(t *testing.T) {
	f := newFixture(t)
	a := f.start()
	raw, err := os.ReadFile(f.store.Path)
	if err != nil || !strings.Contains(string(raw), `"plan_path"`) {
		t.Fatalf("plan_path not persisted: %v", err)
	}
	// A record from before plan files existed has no plan_path and must still load and report no file.
	old := strings.Replace(string(raw), `"plan_path":"`+strings.ReplaceAll(a.PlanPath, `\`, `\\`)+`",`, "", 1)
	if old == string(raw) {
		t.Fatalf("could not strip plan_path from %s", raw)
	}
	if err := writeFile(f.store.Path, old); err != nil {
		t.Fatal(err)
	}
	list, err := f.store.List(context.Background(), "local-1", testIssue)
	if err != nil || len(list) != 1 || list[0].PlanPath != "" {
		t.Fatalf("old record = %+v, %v", list, err)
	}
	if info := f.svc.InspectPlanFile(list[0], nil); info.State != PlanFileNone {
		t.Fatalf("old record plan file = %+v", info)
	}
	bad := list[0]
	bad.PlanPath = "relative/plan.md"
	if bad.valid() {
		t.Fatal("relative PlanPath accepted")
	}
	bad.PlanPath = "/tmp/other.md"
	if bad.valid() {
		t.Fatal("PlanPath with another file name accepted")
	}
}

func (f *fixture) writePlan(a Attempt, content string, age time.Duration) {
	f.t.Helper()
	if err := os.WriteFile(a.PlanPath, []byte(content), 0o600); err != nil {
		f.t.Fatal(err)
	}
	mt := f.clock.Add(-age)
	if err := os.Chtimes(a.PlanPath, mt, mt); err != nil {
		f.t.Fatal(err)
	}
}

func working() *agentactivity.Activity { return &agentactivity.Activity{State: agentactivity.Working} }
func idle() *agentactivity.Activity    { return &agentactivity.Activity{State: agentactivity.Idle} }

func TestInspectPlanFileStates(t *testing.T) {
	f := newFixture(t)
	a := f.start()
	live := a
	ended := a
	ended.State = StateEnded

	if got := f.svc.InspectPlanFile(live, idle()).State; got != PlanFileNone {
		t.Fatalf("no file = %s", got)
	}
	f.writePlan(a, "", time.Hour)
	if got := f.svc.InspectPlanFile(live, idle()).State; got != PlanFileNone {
		t.Fatalf("empty file = %s", got)
	}
	f.writePlan(a, "# Plan\n", time.Hour)
	for _, tc := range []struct {
		name string
		a    Attempt
		act  *agentactivity.Activity
		want PlanFileState
	}{
		{"live and working", live, working(), PlanFileWriting},
		{"live and idle", live, idle(), PlanFileReady},
		{"live, activity unknown", live, nil, PlanFileReady},
		{"ended ignores stale working", ended, working(), PlanFileReady},
	} {
		if info := f.svc.InspectPlanFile(tc.a, tc.act); info.State != tc.want || info.Size != 7 {
			t.Errorf("%s: %+v, want %s", tc.name, info, tc.want)
		}
	}
	f.writePlan(a, "# Plan\n", time.Second)
	if got := f.svc.InspectPlanFile(ended, idle()).State; got != PlanFileWriting {
		t.Fatalf("file changed 1s ago = %s, want writing", got)
	}
}

func TestInspectPlanFileRefusesUnsafeFiles(t *testing.T) {
	f := newFixture(t)
	a := f.start()
	dir := filepath.Dir(a.PlanPath)

	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if err := os.Symlink(outside, a.PlanPath); err != nil {
			t.Fatal(err)
		}
		if info := f.svc.InspectPlanFile(a, idle()); info.State != PlanFileInvalid {
			t.Fatalf("symlinked file = %+v", info)
		}
		if _, _, err := f.svc.Plan(context.Background(), a.ID); !errors.Is(err, ErrPlanFileInvalid) {
			t.Fatalf("Plan through a symlink = %v", err)
		}
		_ = os.Remove(a.PlanPath)
	}

	if err := os.Mkdir(a.PlanPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if info := f.svc.InspectPlanFile(a, idle()); info.State != PlanFileInvalid {
		t.Fatalf("directory as file = %+v", info)
	}
	_ = os.Remove(a.PlanPath)

	f.writePlan(a, strings.Repeat("x", MaxPlanBytes+1), time.Hour)
	if info := f.svc.InspectPlanFile(a, idle()); info.State != PlanFileInvalid || !strings.Contains(info.Reason, "large") {
		t.Fatalf("oversize = %+v", info)
	}
	if _, _, err := f.svc.Plan(context.Background(), a.ID); !errors.Is(err, ErrPlanFileInvalid) {
		t.Fatalf("Plan oversize = %v", err)
	}
	f.writePlan(a, strings.Repeat("x", MaxPlanBytes), time.Hour)
	if info := f.svc.InspectPlanFile(a, idle()); info.State != PlanFileReady {
		t.Fatalf("file at the cap = %+v", info)
	}

	// A record that names any path but its own is never read.
	foreign := a
	foreign.PlanPath = outside
	if info := f.svc.InspectPlanFile(foreign, idle()); info.State != PlanFileInvalid {
		t.Fatalf("foreign path = %+v", info)
	}
	other := a
	other.PlanPath = filepath.Join(filepath.Dir(dir), "plan-9999", "plan.md")
	if info := f.svc.InspectPlanFile(other, idle()); info.State != PlanFileInvalid {
		t.Fatalf("another attempt's path = %+v", info)
	}
	if runtime.GOOS != "windows" {
		// The attempt directory swapped for a link elsewhere.
		moved := filepath.Join(t.TempDir(), "elsewhere")
		if err := os.Rename(dir, moved); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(moved, dir); err != nil {
			t.Fatal(err)
		}
		if info := f.svc.InspectPlanFile(a, idle()); info.State != PlanFileInvalid {
			t.Fatalf("symlinked directory = %+v", info)
		}
	}
}

func TestPlanReturnsTextOnlyWhenReady(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	a := f.start()
	if _, _, err := f.svc.Plan(ctx, a.ID); !errors.Is(err, ErrPlanNotReady) {
		t.Fatalf("no file = %v", err)
	}
	f.writePlan(a, "# Plan\nstep 1\n", time.Hour)
	f.agents.status[a.SessionID] = SessionStatus{State: "running", Activity: working()}
	if _, _, err := f.svc.Plan(ctx, a.ID); !errors.Is(err, ErrPlanNotReady) {
		t.Fatalf("writing = %v", err)
	}
	f.agents.status[a.SessionID] = SessionStatus{State: "running", Activity: idle()}
	text, info, err := f.svc.Plan(ctx, a.ID)
	if err != nil || text != "# Plan\nstep 1\n" || info.State != PlanFileReady {
		t.Fatalf("ready = %q %+v %v", text, info, err)
	}
	if _, _, err := f.svc.Plan(ctx, "plan-nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown attempt = %v", err)
	}
}

func TestObservationsCarryLiveActivityAndFileButStoreNeither(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	a := f.start()
	f.agents.status[a.SessionID] = SessionStatus{State: "running", Activity: working()}
	obs, err := f.svc.Observations(ctx, testIssue)
	if err != nil || len(obs) != 1 {
		t.Fatalf("obs = %+v, %v", obs, err)
	}
	if obs[0].Attempt.State != StateRunning || obs[0].Activity == nil || obs[0].Activity.State != agentactivity.Working {
		t.Fatalf("obs = %+v", obs[0])
	}
	raw, _ := os.ReadFile(f.store.Path)
	if strings.Contains(string(raw), "working") {
		t.Fatal("activity was persisted")
	}
	f.agents.status[a.SessionID] = SessionStatus{State: "exited", ExitCode: exit(0)}
	if obs, _ = f.svc.Observations(ctx, testIssue); obs[0].Attempt.State != StateEnded || obs[0].Activity != nil {
		t.Fatalf("ended obs = %+v", obs[0])
	}
}

func TestOverviewUsesOneSessionListAndReconcilesAll(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	a := f.start()
	f.agents.status[a.SessionID] = SessionStatus{State: "running", Activity: idle()}
	f.writePlan(a, "# Plan", time.Hour)
	obs, err := f.svc.Overview(ctx)
	if err != nil || len(obs) != 1 || obs[0].Attempt.State != StateRunning || obs[0].File.State != PlanFileReady {
		t.Fatalf("overview = %+v, %v", obs, err)
	}
	// A session missing from the list ends the attempt, like a gone record.
	delete(f.agents.status, a.SessionID)
	if obs, _ = f.svc.Overview(ctx); obs[0].Attempt.State != StateFailed {
		t.Fatalf("vanished session = %+v", obs[0].Attempt)
	}
	// An unreadable list leaves a live attempt as it was.
	g := newFixture(t)
	b := g.start()
	g.agents.listErr = errors.New("holder down")
	if obs, err = g.svc.Overview(ctx); err != nil || obs[0].Attempt.State != StateStarting || obs[0].Attempt.ID != b.ID {
		t.Fatalf("list failure = %+v, %v", obs, err)
	}
}

func TestMarkPublishedIsIdempotent(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t)
	a := f.start()
	if err := f.svc.MarkPublished(ctx, a.ID, "hash-a"); err != nil {
		t.Fatal(err)
	}
	first, _ := f.store.Get(ctx, a.ID)
	f.clock = f.clock.Add(time.Hour)
	if err := f.svc.MarkPublished(ctx, a.ID, "hash-a"); err != nil {
		t.Fatal(err)
	}
	second, _ := f.store.Get(ctx, a.ID)
	if first.PlanPublishedAt == nil || !second.PlanPublishedAt.Equal(*first.PlanPublishedAt) || !second.UpdatedAt.Equal(first.UpdatedAt) {
		t.Fatalf("record changed: %+v -> %+v", first, second)
	}
	if err := f.svc.MarkPublished(ctx, "plan-nope", "hash-a"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown = %v", err)
	}
}

func TestMarkPublishedHashSemantics(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name         string
		marks        []string
		wantRevision string
	}{
		{"stores the hash", []string{"hash-a"}, "hash-a"},
		{"never overwrites a different hash", []string{"hash-a", "hash-b"}, "hash-a"},
		{"fills an empty hash later", []string{"", "hash-b"}, "hash-b"},
		{"empty never clears", []string{"hash-a", ""}, "hash-a"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			a := f.start()
			for _, h := range tc.marks {
				if err := f.svc.MarkPublished(ctx, a.ID, h); err != nil {
					t.Fatal(err)
				}
			}
			got, _ := f.store.Get(ctx, a.ID)
			if got.PublishedRevision != tc.wantRevision || got.PlanPublishedAt == nil {
				t.Fatalf("record = %+v", got)
			}
		})
	}
}
