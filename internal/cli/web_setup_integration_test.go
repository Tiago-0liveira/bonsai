package cli

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
	websetupui "github.com/Tiago-0liveira/bonsai/internal/ui/websetup"
	setup "github.com/Tiago-0liveira/bonsai/internal/websetup"
)

// setupCLI is an in-process `bonsai web` that spawns the real binary for the
// daemon and the API, so the setup backend can be driven without a terminal.
func setupCLI(t *testing.T, e *webEnv) (*webCLI, *bytes.Buffer) {
	t.Helper()
	var out bytes.Buffer
	w, err := newWebCLI(nil, &out, &out)
	if err != nil {
		t.Fatal(err)
	}
	w.executable = e.bin
	w.openURL = func(string) error { return nil }
	return w, &out
}

func apiPID(t *testing.T, w *webCLI) int {
	t.Helper()
	group, err := w.group(false)
	if err != nil || group == nil {
		t.Fatalf("bonsai web is not running: %v", err)
	}
	for _, p := range group.Processes {
		if p.Name == "api" {
			return p.PID
		}
	}
	t.Fatal("no api process")
	return 0
}

// apply runs one edit-mode Apply the way the Review screen does.
func apply(t *testing.T, w *webCLI, edit func(*setup.Draft)) (setup.Plan, websetupui.Result, []websetupui.Step) {
	t.Helper()
	rootsPath, err := config.ProjectRootsPath()
	if err != nil {
		t.Fatal(err)
	}
	saved, exists, err := config.ReadWebConfig(w.configPath)
	if err != nil {
		t.Fatal(err)
	}
	roots, err := config.ReadProjectRoots(rootsPath)
	if err != nil {
		t.Fatal(err)
	}
	before := setup.NewDraft(saved, exists, roots.Roots, nil, "")
	after := before.Clone()
	edit(&after)
	plan := setup.BuildPlan(before, after, w.running(), false)
	var steps []websetupui.Step
	result := w.newSetupBackend(rootsPath, saved, exists, webStartOptions{noOpen: true}).Apply(context.Background(), plan, func(s websetupui.Step) {
		steps = append(steps, s)
	})
	return plan, result, steps
}

func TestWebSetupEditAppliesWithTargetedRestart(t *testing.T) {
	e := newWebEnv(t)
	first := freePort(t)
	out := e.mustRun(t, "web", "--no-open", "--port", strconv.Itoa(first))
	// Without a terminal the first run writes defaults and points at setup.
	if !strings.Contains(out, "Customize with: bonsai web setup") {
		t.Fatalf("non-interactive first run:\n%s", out)
	}
	w, _ := setupCLI(t, e)
	if saved, _, _ := config.ReadWebConfig(w.configPath); saved.SetupVersion != 0 {
		t.Fatalf("defaults must leave setup_version 0 so setup is offered later, got %d", saved.SetupVersion)
	}

	// Port and hosted app: the API restarts on the new port with the hosted
	// origin no longer allowed; a repository folder is added on the way.
	work := filepath.Join(os.Getenv("HOME"), "work")
	gitInit(t, filepath.Join(work, "alpha"))
	workCanonical, err := config.CanonicalDirectory(work)
	if err != nil {
		t.Fatal(err)
	}
	second := freePort(t)
	plan, result, steps := apply(t, w, func(d *setup.Draft) {
		d.Config.APIPort = second
		d.Config.Interfaces.Hosted = false
		d.AddFolder(workCanonical)
	})
	if !plan.RestartAPI || !result.OK {
		t.Fatalf("plan %+v result %+v steps %+v", plan, result, steps)
	}
	secondPort := strconv.Itoa(second)
	if !portListening(t, secondPort) || !waitUntil(5*time.Second, func() bool { return !portListening(t, strconv.Itoa(first)) }) {
		t.Fatalf("API did not move from %d to %d", first, second)
	}
	if got := sessionStatus(t, secondPort, "https://app.bonsai.dev"); got != http.StatusForbidden {
		t.Fatalf("hosted origin after turning it off: %d", got)
	}
	if got := sessionStatus(t, secondPort, "http://127.0.0.1:"+secondPort); got != http.StatusCreated {
		t.Fatalf("own origin: %d", got)
	}
	saved, _, _ := config.ReadWebConfig(w.configPath)
	if saved.APIPort != second || saved.Interfaces.Hosted || saved.SetupVersion != config.WebSetupVersion {
		t.Fatalf("saved = %+v", saved)
	}
	rootsPath, _ := config.ProjectRootsPath()
	roots, _ := config.ReadProjectRoots(rootsPath)
	if len(roots.Roots) != 1 || roots.Roots[0].Path != workCanonical {
		t.Fatalf("roots = %+v", roots.Roots)
	}
	selection, _ := config.ReadProjectSelection(config.ProjectSelectionPath(rootsPath))
	alpha, _ := config.CanonicalDirectory(filepath.Join(work, "alpha"))
	if len(selection.Selected) != 1 || selection.Selected[0] != config.ProjectID(alpha) {
		t.Fatalf("selection = %+v, want the repository found in the new folder", selection)
	}

	// Folders only: nothing restarts.
	pid := apiPID(t, w)
	more := filepath.Join(os.Getenv("HOME"), "more")
	gitInit(t, filepath.Join(more, "beta"))
	moreCanonical, _ := config.CanonicalDirectory(more)
	plan, result, _ = apply(t, w, func(d *setup.Draft) {
		d.AddFolder(moreCanonical)
		d.Toggle(0) // remove ~/work
	})
	if plan.RestartAPI || !result.OK || len(plan.RemoveRoots) != 1 {
		t.Fatalf("plan %+v result %+v", plan, result)
	}
	if got := apiPID(t, w); got != pid {
		t.Fatalf("a folder change restarted the API: pid %d → %d", pid, got)
	}
	if !strings.Contains(strings.Join(result.Notes, "\n"), "within 30 s") {
		t.Fatalf("notes = %v", result.Notes)
	}
	roots, _ = config.ReadProjectRoots(rootsPath)
	if len(roots.Roots) != 1 || roots.Roots[0].Path != moreCanonical {
		t.Fatalf("roots = %+v", roots.Roots)
	}
	e.mustRun(t, "web", "stop")
}

func TestWebSetupFirstRunStartsAndReportsFailures(t *testing.T) {
	e := newWebEnv(t)
	w, _ := setupCLI(t, e)
	port := freePort(t)
	cfg := config.DefaultWebConfig()
	cfg.APIPort = port
	before := setup.NewDraft(cfg, false, nil, nil, "")
	plan := setup.BuildPlan(before, before.Clone(), nil, true)

	// A foreign program on the port: the start step fails with one fix and
	// nothing keeps running.
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		t.Fatal(err)
	}
	rootsPath, _ := config.ProjectRootsPath()
	backend := w.newSetupBackend(rootsPath, cfg, false, webStartOptions{noOpen: true})
	var steps []websetupui.Step
	result := backend.Apply(context.Background(), plan, func(s websetupui.Step) { steps = append(steps, s) })
	ln.Close()
	last := steps[len(steps)-1]
	if result.OK || last.State != websetupui.StepFailed || !strings.Contains(last.Detail, "is used by another program") || !strings.Contains(last.Fix, "bonsai web --port") {
		t.Fatalf("result %+v last step %+v", result, last)
	}
	if backend.lastFailure() == nil || !backend.wasSaved() {
		t.Fatal("the failure is kept for the summary printed after the TUI closes")
	}

	// Port free again: the first run starts bonsai web.
	saved, _, _ := config.ReadWebConfig(w.configPath)
	backend = w.newSetupBackend(rootsPath, saved, true, webStartOptions{noOpen: true})
	result = backend.Apply(context.Background(), plan, func(websetupui.Step) {})
	if !result.OK || result.URL != "http://127.0.0.1:"+strconv.Itoa(port)+"/app" || backend.lastGroup() == nil {
		t.Fatalf("result %+v", result)
	}
	if got := sessionStatus(t, strconv.Itoa(port), "https://app.bonsai.dev"); got != http.StatusForbidden {
		t.Fatalf("a brand-new user gets this computer only; hosted origin answered %d", got)
	}
	e.mustRun(t, "web", "stop")
}

func TestWebDoctorExitCodes(t *testing.T) {
	e := newWebEnv(t)
	path, err := config.WebConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	port := freePort(t)
	if _, err := config.UpdateWebConfig(path, ^uint64(0), func(c *config.WebConfig) error {
		c.APIPort = port
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	out, _, code := e.run(t, "web", "doctor")
	if code != 0 || !strings.Contains(out, "bonsai web doctor") || !strings.Contains(out, "port "+strconv.Itoa(port)+"  ") {
		t.Fatalf("doctor with a free port: code %d\n%s", code, out)
	}

	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	next := port + 10
	if next > 65535 {
		next = port - 10
	}
	out, _, code = e.run(t, "web", "doctor")
	if code != 1 || !strings.Contains(out, "is used by another program") || !strings.Contains(out, "fix  bonsai web --port "+strconv.Itoa(next)) {
		t.Fatalf("doctor with the port taken: code %d\n%s", code, out)
	}
	if _, err := os.Stat(procstore.New(e.home).PidPath()); err == nil {
		t.Fatal("doctor started the web daemon")
	}
}
