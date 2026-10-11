package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/daemon/client"
	websetupui "github.com/Tiago-0liveira/bonsai/internal/ui/websetup"
	setup "github.com/Tiago-0liveira/bonsai/internal/websetup"
)

// offlineSetup is a setup backend over temporary settings files with no web
// daemon running, for the steps that need neither.
func offlineSetup(t *testing.T) (*setupBackend, string) {
	t.Helper()
	dir := t.TempDir()
	var out bytes.Buffer
	w := &webCLI{out: &out, errOut: &out, home: filepath.Join(dir, "home"), configPath: filepath.Join(dir, "web.json"), client: client.ForUserHome(filepath.Join(dir, "home"))}
	rootsPath := filepath.Join(dir, "project-roots.json")
	return w.newSetupBackend(rootsPath, config.DefaultWebConfig(), false, webStartOptions{noOpen: true}), rootsPath
}

func mkRepo(t *testing.T, path string) string {
	t.Helper()
	gitInit(t, path)
	canonical, err := config.CanonicalDirectory(path)
	if err != nil {
		t.Fatal(err)
	}
	return canonical
}

func TestSetupProjectFailureIsReportedAfterTheScreenCloses(t *testing.T) {
	b, _ := offlineSetup(t)
	plan := setup.Plan{Config: config.DefaultWebConfig(), AddRoots: []string{filepath.Join(t.TempDir(), "missing")}}
	plan.Config.SetupVersion = config.WebSetupVersion
	var steps []websetupui.Step
	result := b.Apply(context.Background(), plan, func(s websetupui.Step) { steps = append(steps, s) })
	if result.OK || steps[len(steps)-1].ID != "projects" || steps[len(steps)-1].State != websetupui.StepFailed {
		t.Fatalf("result %+v steps %+v", result, steps)
	}
	failure := b.lastFailure()
	if failure == nil || !strings.Contains(failure.detail, "Update project folders") || failure.fix == "" {
		t.Fatalf("failure = %+v; runSetup must not report \"Settings saved\" after this", failure)
	}

	// A later successful Apply clears it.
	plan.AddRoots = nil
	if result := b.Apply(context.Background(), plan, func(websetupui.Step) {}); !result.OK || b.lastFailure() != nil {
		t.Fatalf("result %+v failure %+v", result, b.lastFailure())
	}
}

func TestSetupProjectChangesAreSafeToRetry(t *testing.T) {
	b, rootsPath := offlineSetup(t)
	base := t.TempDir()
	code := mkRepo(t, filepath.Join(base, "code", "app"))
	codeRoot := filepath.Dir(code)
	if _, err := config.UpdateProjectRoots(rootsPath, "seed", 0, codeRoot, ""); err != nil {
		t.Fatal(err)
	}
	work := filepath.Dir(mkRepo(t, filepath.Join(base, "work", "alpha")))
	plan := setup.Plan{
		AddRoots:    []string{work},
		RemoveRoots: []config.ProjectRoot{{ID: config.PathID("root", codeRoot), Path: codeRoot}},
	}
	for attempt := 1; attempt <= 2; attempt++ {
		shown, err := b.applyProjects(context.Background(), plan)
		if err != nil {
			t.Fatalf("attempt %d: %v", attempt, err)
		}
		if attempt == 1 && shown != 1 {
			t.Fatalf("selected %d repos", shown)
		}
	}
	roots, _ := config.ReadProjectRoots(rootsPath)
	if len(roots.Roots) != 1 || roots.Roots[0].Path != work {
		t.Fatalf("roots = %+v", roots.Roots)
	}
}

func TestProjectsSummaryMatchesTheAPIRules(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs symlinks")
	}
	dir := t.TempDir()
	rootsPath := filepath.Join(dir, "project-roots.json")
	good := filepath.Dir(mkRepo(t, filepath.Join(dir, "good", "a")))
	mkRepo(t, filepath.Join(dir, "good", "b"))
	// A root recorded through a symlink that now points elsewhere: the API
	// reports it unavailable, so doctor must too.
	real := filepath.Dir(mkRepo(t, filepath.Join(dir, "elsewhere", "c")))
	link := filepath.Join(dir, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if _, err := config.UpdateProjectRoots(rootsPath, "k1", 0, good, ""); err != nil {
		t.Fatal(err)
	}
	cfg, _ := config.ReadProjectRoots(rootsPath)
	cfg.Roots = append(cfg.Roots, config.ProjectRoot{ID: config.PathID("root", link), Path: link})
	cfg.Requests = nil
	if err := config.WithProjectRoots(rootsPath, func(config.ProjectRoots) error { return writeRootsForTest(rootsPath, cfg) }); err != nil {
		t.Fatal(err)
	}
	summary, err := projectsSummary(context.Background(), rootsPath)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Folders != 2 || summary.Found != 2 || summary.Shown != 0 || len(summary.Unavailable) != 1 || !strings.HasSuffix(summary.Unavailable[0], "link") {
		t.Fatalf("summary = %+v", summary)
	}
}

func writeRootsForTest(path string, cfg config.ProjectRoots) error {
	raw, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o600)
}
