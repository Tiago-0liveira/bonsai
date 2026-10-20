package ui

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
	gardenfake "github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi/fake"
	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
)

// gardenEnv is the redesign shell on Garden with the fake backend: the first
// status read has landed and the body has the keyboard.
func gardenEnv(t *testing.T, fb gardenapi.Backend, prefs config.Prefs, w, h int) Model {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Cleanup(func() {
		theme.Current = theme.Resolve("bonsai", nil)
		applyTheme()
	})
	st, err := config.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	st.Prefs = prefs
	opts := []Option{WithLayout(LayoutRedesign)}
	if fb != nil {
		opts = append(opts, WithGarden(fb))
	}
	m := sized(t, New(t.TempDir(), &config.Config{}, st, opts...), w, h)
	m, _ = drainAll(t, m, m.Init())
	if m.workspace != wsGarden {
		return m
	}
	return pressAll(t, m, "enter")
}

func gardenFake(state gardenapi.State) *gardenfake.Backend { return gardenfake.New(state) }

// flat collapses runs of whitespace, so a wrapped sentence reads as one.
func flat(s string) string { return strings.Join(strings.Fields(s), " ") }

func mustContain(t *testing.T, label, got string, wants ...string) {
	t.Helper()
	for _, w := range wants {
		if !strings.Contains(got, w) {
			t.Errorf("%s: missing %q in:\n%s", label, w, got)
		}
	}
}

func mustNotContain(t *testing.T, label, got string, bad ...string) {
	t.Helper()
	for _, b := range bad {
		if strings.Contains(got, b) {
			t.Errorf("%s: unexpected %q in:\n%s", label, b, got)
		}
	}
}

func TestGardenStateRoutesToScreen(t *testing.T) {
	scopeErr := &gardenapi.Error{Code: gardenapi.CodeScopeMissing, Message: "The GitHub token lacks the project scope.", Fix: "gh auth refresh -s project"}
	tests := []struct {
		name   string
		setup  func(*gardenfake.Backend)
		screen gardenScreen
		want   []string
		absent []string
	}{
		{"none is GS-01", nil, scrStart,
			[]string{"Garden › Setup › Create Garden", "Create Garden", "Connect existing Project", "Skip for this repository", "acme/my-app", "octo-dev", "0 imported", "Preview setup plan…", "not set up", "FOCUS: Setup / Create Garden"}, nil},
		{"ambiguous is GS-02", func(f *gardenfake.Backend) {
			f.StatusView.Ambiguous = true
			f.StatusView.Target = gardenapi.Target{}
			f.CandidatesView = gardenfake.SampleCandidates(true)
		}, scrTarget, []string{"Choose repository", "octo-dev/my-app", "48120394", "51993011", "Use acme/my-app ›"}, nil},
		{"partial is GS-06", func(f *gardenfake.Backend) { f.StatusView.Operation.Err = scopeErr }, scrIncomplete,
			[]string{"Incomplete setup", "SETUP INCOMPLETE", "scope-missing", "gh auth refresh -s project", "op-7f3a21c0", "Resume setup ›", "Skip Garden for now"}, nil},
		{"awaiting manual is GS-06", func(f *gardenfake.Backend) {
			op := gardenfake.SampleOperation(gardenapi.StatePartialSetup)
			op.Steps[2].Status, op.Steps[2].ErrorCode = gardenapi.StepDone, ""
			op.AwaitingManual = []string{"Project board layout"}
			f.StatusView.Operation = &op
			f.StatusView.State = gardenapi.StateAwaitingManual
			f.StatusView.Caps.Resume = false
		}, scrIncomplete, []string{"Manual step", "MANUAL STEP NEEDED", "Manual: Project board layout", "Skip Garden for now"}, []string{"Resume setup"}},
		{"in progress is GS-06 without actions", func(f *gardenfake.Backend) {}, scrIncomplete,
			[]string{"Setup running", "SETUP RUNNING", "running", "11/14"}, []string{"Resume setup", "Skip Garden for now"}},
		{"read-only is GS-07", nil, scrReadOnly,
			[]string{"Read-only access", "READ-ONLY CONNECTION", "set up by a collaborator", "12 issues", "Read access", "Open Board ›"},
			[]string{"Create Garden", "Preview setup plan", "Resume setup", "Skip"}},
		{"skipped is GS-08", nil, scrOff,
			[]string{"Garden › Off", "GARDEN IS OFF FOR THIS REPOSITORY", "Set up Garden…", "FOCUS: Garden / Off"}, nil},
		{"connected leaves Setup", nil, scrBoard,
			[]string{"Garden › Board", "GARDEN BOARD", "plan 05", "connected"}, []string{"Preview setup plan", "Create Garden"}},
	}
	states := []gardenapi.State{gardenapi.StateNone, gardenapi.StateNone, gardenapi.StatePartialSetup, gardenapi.StateAwaitingManual,
		gardenapi.StateOperationInProgress, gardenapi.StateReadOnly, gardenapi.StateSkipped, gardenapi.StateConnected}
	for i, tc := range tests {
		for _, size := range [][2]int{{80, 24}, {120, 35}} {
			fb := gardenFake(states[i])
			if tc.setup != nil {
				tc.setup(fb)
			}
			m := gardenEnv(t, fb, config.Prefs{}, size[0], size[1])
			label := fmt.Sprintf("%s %dx%d", tc.name, size[0], size[1])
			if got := m.gardenScreen(); got != tc.screen {
				t.Fatalf("%s: screen %d, want %d\n%s", label, got, tc.screen, view(m))
			}
			out := view(m)
			// Wide screens show everything the mockup does; at 80x24 the
			// lower-priority explanation may be dropped but the screen identity,
			// the state and the action stay.
			want := tc.want
			if size[0] < 100 {
				want = want[:min(len(want), 3)]
			}
			mustContain(t, label, out, want...)
			mustNotContain(t, label, out, tc.absent...)
			assertBounded(t, label, m.View(), size[0], size[1])
		}
	}
}

func TestGardenReadsStatusOnEntryAndRefreshOnly(t *testing.T) {
	fb := gardenFake(gardenapi.StateNone)
	m := gardenEnv(t, fb, config.Prefs{}, 120, 35)
	if n := fb.Count("Status"); n != 1 {
		t.Fatalf("Status calls after opening Garden = %d, want 1", n)
	}
	for _, k := range []string{"down", "up", "down", "up", "esc", "enter"} {
		m = pressAll(t, m, k)
		for i := 0; i < 5; i++ {
			_ = m.View()
		}
	}
	if n := fb.Count("Status"); n != 1 {
		t.Errorf("keys or frames called Status: %d calls", n)
	}
	m = pressAll(t, m, "R")
	if n := fb.Count("Status"); n != 2 {
		t.Errorf("explicit refresh made %d Status calls, want 2", n)
	}
	// Leaving and re-entering the workspace is the other read.
	m, cmd := m.selectWorkspace(wsPlayground)
	m, _ = drainAll(t, m, cmd)
	m, cmd = m.selectWorkspace(wsGarden)
	_, _ = drainAll(t, m, cmd)
	if n := fb.Count("Status"); n != 3 {
		t.Errorf("re-entering Garden made %d Status calls, want 3", n)
	}
	if fb.Writes() != 0 {
		t.Errorf("reading wrote %d times", fb.Writes())
	}
}

func TestGardenCreateFlowEscRestoresSelectionAndNeverWrites(t *testing.T) {
	fb := gardenFake(gardenapi.StateNone)
	m := gardenEnv(t, fb, config.Prefs{}, 120, 35)
	m = pressAll(t, m, "enter") // Create → preview
	if m.gardenScreen() != scrPlan {
		t.Fatalf("screen = %d after Enter on Create\n%s", m.gardenScreen(), view(m))
	}
	out := view(m)
	mustContain(t, "plan", out, "Garden › Setup › Preview plan", "SETUP PLAN", "bonsai:managed", "◇ manual", "finish in the browser",
		"0 issues imported", "Review and apply…", "FOCUS: Setup / Preview plan", "Create · acme/my-app")
	if fb.Count("PreviewPlan") != 1 || fb.Writes() != 0 {
		t.Fatalf("calls %v", fb.Calls())
	}
	// GS-04 → GS-05: Cancel is focused.
	m = pressAll(t, m, "down", "down", "enter")
	if m.garden.planSel != 2 {
		t.Errorf("plan selection = %d", m.garden.planSel)
	}
	if m.gardenScreen() != scrConfirm || m.garden.confirm != 0 {
		t.Fatalf("confirm screen %d focus %d", m.gardenScreen(), m.garden.confirm)
	}
	mustContain(t, "confirm", flat(view(m)), "REVIEW THE CONSEQUENCE", "[Cancel]", "[Apply setup]", "Cancel has initial focus.",
		`Create 10 labels and the Project "Bonsai Garden" in acme, reuse 1 label, and add the Stage and Bonsai Type fields to that Project.`,
		"No Issue is imported, edited or relabeled.", "FOCUS: Setup / Confirm setup")
	// Enter on the focused Cancel, then Esc: both go back and write nothing.
	m = pressAll(t, m, "enter")
	if m.gardenScreen() != scrPlan || m.garden.planSel != 2 {
		t.Fatalf("Cancel went to screen %d sel %d", m.gardenScreen(), m.garden.planSel)
	}
	m = pressAll(t, m, "enter", "esc")
	if m.gardenScreen() != scrPlan {
		t.Fatalf("Esc on confirm went to screen %d", m.gardenScreen())
	}
	m = pressAll(t, m, "esc")
	if m.gardenScreen() != scrStart {
		t.Fatalf("Esc on plan went to screen %d", m.gardenScreen())
	}
	m = pressAll(t, m, "esc")
	if m.shellFocus != shellNav {
		t.Errorf("Esc on the first screen left focus %s", m.shellFocus)
	}
	if fb.Writes() != 0 {
		t.Errorf("Esc/Cancel wrote %d times: %v", fb.Writes(), fb.Calls())
	}
}

func TestGardenConnectExistingFlow(t *testing.T) {
	fb := gardenFake(gardenapi.StateNone)
	m := gardenEnv(t, fb, config.Prefs{}, 120, 35)
	m = pressAll(t, m, "down", "enter") // Connect existing → Projects
	if m.gardenScreen() != scrProjects {
		t.Fatalf("screen %d\n%s", m.gardenScreen(), view(m))
	}
	mustContain(t, "projects", view(m), "Garden › Setup › Connect existing Project", "Projects owned by acme", "Roadmap", "Stage ✓  Bonsai Type +", "compatible",
		"! same name, unverified", "no Garden fields", "A same-name Project is never adopted automatically.", "Preview setup plan…")
	if fb.Count("PreviewPlan") != 0 {
		t.Fatal("selecting a Project in the list previewed early")
	}
	m = pressAll(t, m, "down", "enter") // the same-name Project is an explicit choice
	if m.gardenScreen() != scrPlan {
		t.Fatalf("screen %d", m.gardenScreen())
	}
	req := m.garden.plan.Request
	if req.Mode != gardenapi.ModeConnectExisting || req.Project == nil || req.Project.ID != "PVT_7" || req.Target.RepoID != gardenfake.SampleRepoID {
		t.Errorf("request = %+v project=%+v", req, req.Project)
	}
	m = pressAll(t, m, "esc")
	if m.gardenScreen() != scrProjects || m.garden.projSel != 1 {
		t.Fatalf("Esc from the plan: screen %d, project selection %d", m.gardenScreen(), m.garden.projSel)
	}
	m = pressAll(t, m, "esc")
	if m.gardenScreen() != scrStart || m.gardenRail() != 1 {
		t.Fatalf("Esc from projects: screen %d, rail %d", m.gardenScreen(), m.gardenRail())
	}
	if fb.Writes() != 0 {
		t.Errorf("wrote %d times", fb.Writes())
	}
}

func TestGardenApplySuccessConnects(t *testing.T) {
	fb := gardenFake(gardenapi.StateNone)
	m := gardenEnv(t, fb, config.Prefs{}, 120, 35)
	m = pressAll(t, m, "enter", "enter", "right", "enter")
	if fb.Count("Apply") != 1 || fb.Writes() != 1 {
		t.Fatalf("calls %v", fb.Calls())
	}
	if m.gardenScreen() != scrBoard || m.garden.status.State != gardenapi.StateConnected {
		t.Fatalf("after Apply: screen %d state %s\n%s", m.gardenScreen(), m.garden.status.State, view(m))
	}
	mustContain(t, "connected", view(m), "Garden › Board", "plan 05", "connected")
	if m.garden.step != stepStart || len(m.garden.plan.Rows) != 0 {
		t.Error("the finished flow was not reset")
	}
}

func TestGardenApplyIgnoresDuplicateEnterWhileInFlight(t *testing.T) {
	fb := gardenFake(gardenapi.StateNone)
	release := make(chan struct{})
	fb.Hook = func(method string) {
		if method == "Apply" {
			<-release
		}
	}
	m := gardenEnv(t, fb, config.Prefs{}, 120, 35)
	m = pressAll(t, m, "enter", "enter", "right")
	m, cmd := send(t, m, keyOf("enter"))
	if cmd == nil || m.garden.busy != busyApply {
		t.Fatal("Apply did not start")
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	for _, k := range []string{"enter", "enter", "esc", "left", "right"} {
		var again tea.Cmd
		m, again = send(t, m, keyOf(k))
		if again != nil {
			t.Fatalf("key %s started more work while a write was in flight", k)
		}
	}
	mustContain(t, "in flight", view(m), "Applying setup…")
	close(release)
	msg := <-done
	m, next := send(t, m, msg)
	m, _ = drainAll(t, m, next)
	if fb.Count("Apply") != 1 {
		t.Errorf("Apply ran %d times", fb.Count("Apply"))
	}
	if m.gardenScreen() != scrBoard {
		t.Errorf("screen %d after Apply", m.gardenScreen())
	}
}

func TestGardenApplyPartialShowsResumeAndUserFix(t *testing.T) {
	fb := gardenFake(gardenapi.StateNone)
	op := gardenfake.SampleOperation(gardenapi.StatePartialSetup)
	op.Err = &gardenapi.Error{Code: gardenapi.CodeScopeMissing, Message: "The GitHub token lacks the project scope.",
		Next: "Grant the scope yourself, then Resume. Bonsai never refreshes or changes your login.", Fix: "gh auth refresh -s project"}
	fb.AppliedView = op
	m := gardenEnv(t, fb, config.Prefs{}, 120, 35)
	m = pressAll(t, m, "enter", "enter", "right", "enter")
	if m.gardenScreen() != scrIncomplete {
		t.Fatalf("screen %d\n%s", m.gardenScreen(), view(m))
	}
	out := view(m)
	mustContain(t, "partial", out, "SETUP INCOMPLETE", "Setup stopped at Create Project \"Bonsai Garden\". The GitHub token lacks the project scope.",
		"gh auth refresh -s project", "Grant the scope yourself, then Resume.", "Operation op-7f3a21c0", "scope-missing",
		"Resume setup ›", "Skip Garden for now")
	if fb.Count("Status") != 2 {
		t.Errorf("Status calls = %d: the outcome must be reconciled with one status read", fb.Count("Status"))
	}
}

func TestGardenResumeSkipUnskip(t *testing.T) {
	fb := gardenFake(gardenapi.StatePartialSetup)
	m := gardenEnv(t, fb, config.Prefs{}, 120, 35)
	m = pressAll(t, m, "enter")
	if fb.Count("Resume") != 1 || fb.Writes() != 1 {
		t.Fatalf("calls %v", fb.Calls())
	}
	if m.gardenScreen() != scrBoard {
		t.Fatalf("after Resume: screen %d\n%s", m.gardenScreen(), view(m))
	}

	fb = gardenFake(gardenapi.StatePartialSetup)
	m = gardenEnv(t, fb, config.Prefs{}, 120, 35)
	m = pressAll(t, m, "right", "enter") // Skip Garden for now
	if fb.Count("Skip") != 1 || fb.Count("Resume") != 0 || m.gardenScreen() != scrOff {
		t.Fatalf("calls %v screen %d", fb.Calls(), m.gardenScreen())
	}
	m = pressAll(t, m, "enter") // Set up Garden…
	if fb.Count("Unskip") != 1 || m.gardenScreen() != scrStart {
		t.Fatalf("calls %v screen %d\n%s", fb.Calls(), m.gardenScreen(), view(m))
	}

	// Skip from GS-01 is reversible and asks for no confirmation.
	fb = gardenFake(gardenapi.StateNone)
	m = gardenEnv(t, fb, config.Prefs{}, 120, 35)
	m = pressAll(t, m, "down", "down", "enter")
	if fb.Count("Skip") != 1 || m.gardenScreen() != scrOff {
		t.Fatalf("calls %v screen %d", fb.Calls(), m.gardenScreen())
	}
}

func TestGardenGraphQLFailureShowsRecoveryWithoutCommand(t *testing.T) {
	fb := gardenFake(gardenapi.StatePartialSetup)
	op := gardenfake.SampleOperation(gardenapi.StatePartialSetup)
	op.Steps = []gardenapi.OperationStep{{Title: "Add field Stage", Status: gardenapi.StepFailed, ErrorCode: gardenapi.StepGraphQL}}
	op.Err = &gardenapi.Error{Code: gardenapi.CodeGraphQL, Message: "GitHub rejected Bonsai's GraphQL request.", Next: "Update Bonsai, then resume setup. If it persists, report this error."}
	fb.StatusView.Operation = &op
	m := gardenEnv(t, fb, config.Prefs{}, 120, 35)
	mustContain(t, "GraphQL failure", flat(view(m)), "Add field Stage", "github-graphql", "GitHub rejected Bonsai's GraphQL request.", "Update Bonsai, then resume setup.")
}

func TestGardenStaleReplyIsDroppedAndDoesNotMoveFocus(t *testing.T) {
	fb := gardenFake(gardenapi.StateNone)
	release := make(chan struct{})
	fb.Hook = func(method string) {
		if method == "PreviewPlan" {
			<-release
		}
	}
	m := gardenEnv(t, fb, config.Prefs{}, 120, 35)
	m, cmd := send(t, m, keyOf("enter"))
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	m = pressAll(t, m, "esc") // abandons the read, stays on GS-01
	if m.garden.busy != busyNone || m.gardenScreen() != scrStart || m.shellFocus != shellBody {
		t.Fatalf("Esc while loading: busy %d screen %d focus %s", m.garden.busy, m.gardenScreen(), m.shellFocus)
	}
	close(release)
	select {
	case msg := <-done:
		var next tea.Cmd
		m, next = send(t, m, msg)
		if next != nil {
			t.Error("a dropped reply started more work")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("preview never returned")
	}
	if m.gardenScreen() != scrStart || len(m.garden.plan.Rows) != 0 {
		t.Errorf("late reply moved the screen to %d", m.gardenScreen())
	}
}

func TestGardenGateRejectsOtherCloneAndOlderGeneration(t *testing.T) {
	fb := gardenFake(gardenapi.StateNone)
	m := gardenEnv(t, fb, config.Prefs{}, 120, 35)
	last := m.garden.gate.Last()
	if last.LocalRepoID != gardenfake.Clone {
		t.Fatalf("gate did not record the first reply: %+v", last)
	}
	other := gardenfake.StatusFor(gardenapi.StateConnected)
	other.Stamp = gardenapi.Stamp{LocalRepoID: "another-clone", GardenID: 1, Epoch: last.Epoch, Generation: last.Generation + 10}
	older := gardenfake.StatusFor(gardenapi.StateConnected)
	older.Stamp = gardenapi.Stamp{LocalRepoID: gardenfake.Clone, GardenID: gardenfake.SampleRepoID, Epoch: last.Epoch, Generation: last.Generation}
	for name, v := range map[string]gardenapi.StatusView{"other clone": other, "older generation": older} {
		m, _ = send(t, m, gardenStatusMsg{seq: m.garden.seq, view: v})
		if m.garden.status.State != gardenapi.StateNone || m.gardenScreen() != scrStart {
			t.Errorf("%s landed: state %s", name, m.garden.status.State)
		}
		mustContain(t, name, view(m), "Ignored an out-of-date Garden reply.")
	}
}

func TestGardenReplyForAnotherRequestIsDropped(t *testing.T) {
	fb := gardenFake(gardenapi.StateNone)
	m := gardenEnv(t, fb, config.Prefs{}, 120, 35)
	v := gardenfake.StatusFor(gardenapi.StateConnected)
	v.Stamp = gardenapi.Stamp{LocalRepoID: gardenfake.Clone, Epoch: 9, Generation: 99}
	m, _ = send(t, m, gardenStatusMsg{seq: m.garden.seq + 1, view: v})
	if m.garden.status.State != gardenapi.StateNone {
		t.Error("a reply no request is waiting for was applied")
	}
}

func TestGardenStalePlanRePreviewsAndSaysSo(t *testing.T) {
	fb := gardenFake(gardenapi.StateNone)
	fb.ApplyErr = &gardenapi.Error{Code: gardenapi.CodeStalePlan, Message: "GitHub changed since the preview."}
	m := gardenEnv(t, fb, config.Prefs{}, 120, 35)
	m = pressAll(t, m, "enter", "enter", "right", "enter")
	if fb.Count("PreviewPlan") != 2 {
		t.Fatalf("calls %v: a stale plan must be previewed again", fb.Calls())
	}
	if m.gardenScreen() != scrPlan {
		t.Fatalf("screen %d: the user must review the new plan before applying", m.gardenScreen())
	}
	mustContain(t, "stale", view(m), "The plan changed on GitHub since you reviewed it. Review it again.")
	if fb.Count("Apply") != 1 {
		t.Errorf("Apply ran %d times", fb.Count("Apply"))
	}
}

func TestGardenErrorCodesRoute(t *testing.T) {
	type tc struct {
		name   string
		setup  func(*gardenfake.Backend)
		keys   []string
		screen gardenScreen
		want   []string
	}
	tests := []tc{
		{"skipped re-reads status", func(f *gardenfake.Backend) {
			f.PlanErr = &gardenapi.Error{Code: gardenapi.CodeSkipped, Message: "Garden is off for this repository."}
		}, []string{"enter"}, scrStart, []string{"Garden is off for this repository."}},
		{"read-only re-reads status", func(f *gardenfake.Backend) {
			f.PlanErr = &gardenapi.Error{Code: gardenapi.CodeReadOnly, Message: "You only have read access."}
		}, []string{"enter"}, scrStart, []string{"You only have read access."}},
		{"conflict re-reads status", func(f *gardenfake.Backend) {
			f.PlanErr = &gardenapi.Error{Code: gardenapi.CodeConflict, Message: "A Garden is already set up here."}
		}, []string{"enter"}, scrStart, []string{"A Garden is already set up here."}},
		{"operation in progress re-reads status", func(f *gardenfake.Backend) {
			f.PlanErr = &gardenapi.Error{Code: gardenapi.CodeOperationInProgress, Message: "Setup is already running."}
		}, []string{"enter"}, scrStart, []string{"Setup is already running."}},
		{"no remote shows the safe message", func(f *gardenfake.Backend) {
			f.PlanErr = &gardenapi.Error{Code: gardenapi.CodeNoGitHubRemote, Message: "No remote points at GitHub.", Next: "Add a GitHub remote."}
		}, []string{"enter"}, scrStart, []string{"! No remote points at GitHub.", "Add a GitHub remote."}},
		{"auth required shows the fix", func(f *gardenfake.Backend) {
			f.PlanErr = &gardenapi.Error{Code: gardenapi.CodeAuthRequired, Message: "GitHub sign-in is required.", Fix: "gh auth login"}
		}, []string{"enter"}, scrStart, []string{"! GitHub sign-in is required.", "Run this yourself:", "gh auth login"}},
		{"offline", func(f *gardenfake.Backend) {
			f.ProjectsErr = &gardenapi.Error{Code: gardenapi.CodeOffline, Message: "GitHub could not be reached."}
		}, []string{"down", "enter"}, scrStart, []string{"! GitHub could not be reached."}},
		{"rate limited", func(f *gardenfake.Backend) {
			f.PlanErr = &gardenapi.Error{Code: gardenapi.CodeRateLimited, Message: "GitHub is rate limiting requests.", Next: "Try again later."}
		}, []string{"enter"}, scrStart, []string{"! GitHub is rate limiting requests.", "Try again later."}},
		{"project conflict on preview", func(f *gardenfake.Backend) {
			f.PlanErr = &gardenapi.Error{Code: gardenapi.CodeProjectConflict, Message: "A Project with that name already exists.", Next: "Connect it explicitly."}
		}, []string{"enter"}, scrStart, []string{"! A Project with that name already exists.", "Connect it explicitly."}},
	}
	for _, c := range tests {
		fb := gardenFake(gardenapi.StateNone)
		c.setup(fb)
		m := gardenEnv(t, fb, config.Prefs{}, 120, 35)
		m = pressAll(t, m, c.keys...)
		if m.gardenScreen() != c.screen {
			t.Errorf("%s: screen %d\n%s", c.name, m.gardenScreen(), view(m))
		}
		mustContain(t, c.name, view(m), c.want...)
		if fb.Writes() != 0 {
			t.Errorf("%s: wrote %d times", c.name, fb.Writes())
		}
	}
}

func TestGardenSkippedCodeLandsOnGS08(t *testing.T) {
	fb := gardenFake(gardenapi.StateNone)
	fb.PlanErr = &gardenapi.Error{Code: gardenapi.CodeSkipped, Message: "Garden is off for this repository."}
	m := gardenEnv(t, fb, config.Prefs{}, 120, 35)
	fb.StatusView = gardenfake.StatusFor(gardenapi.StateSkipped) // the state behind the refusal
	m = pressAll(t, m, "enter")
	if m.gardenScreen() != scrOff {
		t.Fatalf("screen %d\n%s", m.gardenScreen(), view(m))
	}
	mustContain(t, "skipped", view(m), "Garden is off for this repository.", "Set up Garden…")
	m = pressAll(t, m, "enter")
	if fb.Count("Unskip") != 1 {
		t.Errorf("Unskip calls = %d", fb.Count("Unskip"))
	}
}

func TestGardenTargetAmbiguousFlows(t *testing.T) {
	// Status says ambiguous: GS-02 first, then GS-01 for the chosen repository.
	fb := gardenFake(gardenapi.StateNone)
	fb.StatusView.Ambiguous = true
	fb.StatusView.Target = gardenapi.Target{}
	fb.CandidatesView = gardenfake.SampleCandidates(true)
	m := gardenEnv(t, fb, config.Prefs{}, 120, 35)
	if m.gardenScreen() != scrTarget {
		t.Fatalf("screen %d\n%s", m.gardenScreen(), view(m))
	}
	m = pressAll(t, m, "down", "enter")
	if m.gardenScreen() != scrStart || m.garden.target.RepoID != gardenfake.SampleForkID {
		t.Fatalf("pick: screen %d target %+v", m.gardenScreen(), m.garden.target)
	}
	mustContain(t, "GS-01 after pick", view(m), "octo-dev/my-app")
	m = pressAll(t, m, "enter")
	if m.gardenScreen() != scrPlan {
		t.Fatalf("screen %d", m.gardenScreen())
	}
	if got := m.garden.plan.Request.Target.RepoID; got != gardenfake.SampleForkID {
		t.Errorf("preview used target %d, want the chosen fork", got)
	}

	// A call that fails with target-ambiguous leads to the choice and then continues.
	fb = gardenFake(gardenapi.StateNone)
	fb.PlanErr = &gardenapi.Error{Code: gardenapi.CodeTargetAmbiguous, Message: "Several repositories match."}
	fb.CandidatesView = gardenfake.SampleCandidates(true)
	m = gardenEnv(t, fb, config.Prefs{}, 120, 35)
	m = pressAll(t, m, "enter")
	if m.gardenScreen() != scrTarget || m.garden.pending != gardenapi.ModeCreate {
		t.Fatalf("screen %d pending %q", m.gardenScreen(), m.garden.pending)
	}
	fb.PlanErr = nil
	m = pressAll(t, m, "enter")
	if m.gardenScreen() != scrPlan || fb.Count("PreviewPlan") != 2 {
		t.Fatalf("screen %d calls %v", m.gardenScreen(), fb.Calls())
	}
	// Esc on a choice that interrupted a flow returns to GS-01.
	fb.PlanErr = &gardenapi.Error{Code: gardenapi.CodeTargetAmbiguous, Message: "Several repositories match."}
	m = pressAll(t, m, "esc", "enter", "esc")
	if m.gardenScreen() != scrStart {
		t.Errorf("Esc on GS-02 went to screen %d", m.gardenScreen())
	}
}

func TestGardenReadOnlyOffersNoSetupAction(t *testing.T) {
	fb := gardenFake(gardenapi.StateReadOnly)
	m := gardenEnv(t, fb, config.Prefs{}, 120, 35)
	out := view(m)
	mustNotContain(t, "read-only", out, "Preview setup plan", "Create Garden", "Connect existing", "Resume", "Skip", "Set up Garden")
	m = pressAll(t, m, "enter") // Open Board
	if m.gardenScreen() != scrBoard {
		t.Fatalf("screen %d", m.gardenScreen())
	}
	mustContain(t, "board placeholder", view(m), "plan 05", "GARDEN BOARD")
	m = pressAll(t, m, "esc")
	if m.gardenScreen() != scrReadOnly {
		t.Errorf("Esc from the Board went to screen %d", m.gardenScreen())
	}
	if fb.Writes() != 0 {
		t.Errorf("read-only wrote %d times", fb.Writes())
	}
	// Capabilities gate each action: a clone that may only skip shows no setup.
	fb = gardenFake(gardenapi.StateNone)
	fb.StatusView.Caps.Setup = false
	m = gardenEnv(t, fb, config.Prefs{}, 120, 35)
	out = view(m)
	mustContain(t, "skip only", out, "Skip for this repository")
	mustNotContain(t, "skip only", out, "Create Garden", "Connect existing Project")
}

func TestGardenPlanWithConflictCannotBeApplied(t *testing.T) {
	fb := gardenFake(gardenapi.StateNone)
	fb.PlanView.Rows[4].Conflict = true
	fb.PlanView.Summary.Conflicts = 1
	fb.PlanView.Summary.CanApply = false
	m := gardenEnv(t, fb, config.Prefs{}, 120, 35)
	m = pressAll(t, m, "enter", "enter")
	if m.gardenScreen() != scrPlan {
		t.Fatalf("Enter on a blocked plan moved to screen %d", m.gardenScreen())
	}
	mustContain(t, "blocked", view(m), "Review and apply…", "blocked by 1 conflict on GitHub", "! create")
	mustNotContain(t, "blocked hints", strings.Split(view(m), "\n")[len(strings.Split(view(m), "\n"))-2], "review and apply")
	if fb.Writes() != 0 {
		t.Error("a blocked plan wrote")
	}
}

func TestGardenFailureNeverShowsRawErrors(t *testing.T) {
	raw := "dial tcp: token ghp_SECRET123 refused at https://user:pw@github.com/x"
	fb := gardenFake(gardenapi.StateNone)
	fb.StatusErr = errors.New(raw)
	m := gardenEnv(t, fb, config.Prefs{}, 120, 35)
	out := view(m)
	mustContain(t, "status error", out, "CANNOT READ GARDEN", "Garden could not complete that request.", "Retry")
	mustNotContain(t, "status error", out, "SECRET", "ghp_", "dial tcp", "user:pw")

	fb = gardenFake(gardenapi.StateNone)
	fb.StatusErr = &gardenapi.Error{Code: gardenapi.CodeAuthRequired, Message: "GitHub sign-in is required.", Next: "Sign in, then retry.", Fix: "gh auth login"}
	m = gardenEnv(t, fb, config.Prefs{}, 80, 24)
	mustContain(t, "auth", view(m), "GitHub sign-in is required.", "Sign in, then retry.", "gh auth login")
	if m.gardenScreen() != scrError {
		t.Fatalf("screen %d", m.gardenScreen())
	}
	fb.StatusErr = nil
	m = pressAll(t, m, "enter") // Retry
	if m.gardenScreen() != scrStart {
		t.Errorf("Retry left screen %d\n%s", m.gardenScreen(), view(m))
	}
}

func TestGardenUnavailableWithoutBackend(t *testing.T) {
	m := gardenEnv(t, nil, config.Prefs{}, 80, 24)
	out := view(m)
	mustContain(t, "no backend", out, "Garden is unavailable", "○ unavailable")
	for _, k := range []string{"enter", "down", "R", "esc", "tab"} {
		m = pressAll(t, m, k)
	}
	_ = m.View()
	m, _ = m.selectWorkspace(wsSettings)
	m = openSettings(t, m, catGarden, 0, 3)
	mustContain(t, "settings", view(m), "unavailable: no Garden backend in this session")
}

func TestGardenSettingsShowsTheSameStatus(t *testing.T) {
	fb := gardenFake(gardenapi.StatePartialSetup)
	fb.StatusView.LastSync = time.Date(2026, 10, 20, 12, 30, 0, 0, time.UTC)
	m := gardenEnv(t, fb, config.Prefs{}, 120, 35)
	m = pressAll(t, m, "esc")
	m, cmd := m.goToPalette(paletteTarget{ws: wsSettings, category: catGarden, hasCategory: true})
	m, _ = drainAll(t, m, cmd)
	m = pressAll(t, m, "enter") // into the sections
	out := view(m)
	mustContain(t, "repository section", out, "GitHub repository", "acme/my-app · origin, upstream", "GitHub identity", "octo-dev · Write access",
		"Garden", "setup incomplete", "Open Garden setup ›")
	m = pressAll(t, m, "down", "down", "down")
	out = view(m)
	mustContain(t, "sync section", out, "Sync state", "partial-setup", "Operation", "11 of 14 steps · op-7f3a21c0", "Last sync")
	if fb.Count("Status") != 1 {
		t.Errorf("Settings made its own status call: %d Status calls", fb.Count("Status"))
	}
	if !strings.Contains(out, "20") {
		t.Error("last sync time missing")
	}
	// The link switches to the Garden workspace on the same state.
	m.set = settingsState{cat: catGarden, sec: 0, field: 6, level: levelField}
	if f, _ := m.curField(); f.id != "garden_link" {
		t.Fatalf("cursor on %q", f.id)
	}
	m, cmd = send(t, m, keyOf("enter"))
	m, _ = drainAll(t, m, cmd)
	if m.workspace != wsGarden || m.shellFocus != shellBody {
		t.Fatalf("link opened %s with focus %s", m.workspace.name(), m.shellFocus)
	}
	if m.gardenScreen() != scrIncomplete {
		t.Errorf("Garden screen %d", m.gardenScreen())
	}
}

func TestGardenSettingsNeverReadYetAndNeverSyncedRows(t *testing.T) {
	fb := gardenFake(gardenapi.StateNone)
	m := gardenEnv(t, fb, config.Prefs{StartWorkspace: "settings"}, 120, 35)
	if m.workspace != wsSettings {
		t.Fatalf("started on %s", m.workspace.name())
	}
	if fb.Count("Status") != 0 {
		t.Fatal("Settings called the backend before the Garden category was opened")
	}
	m = openSettings(t, m, catGarden, 0, 0)
	if fb.Count("Status") != 1 {
		t.Fatalf("opening the Garden category made %d Status calls, want 1", fb.Count("Status"))
	}
	m = pressAll(t, m, "tab", "down", "down", "down")
	mustContain(t, "sync", view(m), "never", "none")
}

func TestGardenConsequenceSentence(t *testing.T) {
	pl := gardenfake.SamplePlan()
	want := `Create 10 labels and the Project "Bonsai Garden" in acme, reuse 1 label, and add the Stage and Bonsai Type fields to that Project.`
	if got := gardenConsequence(pl); got != want {
		t.Errorf("sentence = %q", got)
	}
	pl.Summary = gardenapi.PlanSummary{CreateLabels: 1, ProjectName: "Roadmap", ProjectOwner: "acme", AddFields: 1, CanApply: true}
	pl.Rows = []gardenapi.PlanRow{{Change: gardenapi.ChangeAdd, Kind: gardenapi.RowField, Target: "Bonsai Type  (single select)"}}
	if got, want := gardenConsequence(pl), `Create 1 label, use the existing Project "Roadmap", and add the Bonsai Type field to that Project.`; got != want {
		t.Errorf("connect sentence = %q, want %q", got, want)
	}
	if got := gardenConsequence(gardenapi.PlanView{}); got != "Nothing to change." {
		t.Errorf("empty plan sentence = %q", got)
	}
}

// gardenScenes reaches each screen through the keyboard.
func gardenScenes(long string) []struct {
	name  string
	setup func(*gardenfake.Backend)
	state gardenapi.State
	keys  []string
} {
	longTarget := gardenapi.Target{RepoID: gardenfake.SampleRepoID, FullName: long + "/" + long, Aliases: []string{long, "upstream"}, Access: gardenapi.AccessWrite, Kind: gardenapi.KindRepository}
	ambiguous := func(f *gardenfake.Backend) {
		f.StatusView.Ambiguous = true
		f.StatusView.Target = gardenapi.Target{}
		f.CandidatesView = gardenfake.SampleCandidates(true)
		f.CandidatesView.Candidates[0] = longTarget
		f.CandidatesView.Candidates[1].FullName = long + "/" + long
		f.CandidatesView.Candidates[1].ParentFullName = long + "/" + long
		f.CandidatesView.Notes = []string{"mirror: not a GitHub repository"}
	}
	long2 := func(f *gardenfake.Backend) {
		f.StatusView.Target = longTarget
		f.StatusView.Identity.Login = long
		f.PlanView.Target = longTarget
		f.PlanView.Rows[1].Target = long + " " + long
		f.PlanView.Rows[1].Note = long
		f.PlanView.Summary.ProjectName = long
		f.PlanView.Summary.ProjectOwner = long
		f.ProjectsView.Owner = long
		f.ProjectsView.Projects[0].Title = long + long
		f.ProjectsErr = nil
		if f.StatusView.Operation != nil {
			f.StatusView.Operation.Steps[0].Title = long + long
			f.StatusView.Operation.Err = &gardenapi.Error{Code: gardenapi.CodeScopeMissing, Message: long + " " + long, Next: long + " " + long, Fix: long + long}
		}
	}
	scene := func(name string, state gardenapi.State, extra func(*gardenfake.Backend), keys ...string) struct {
		name  string
		setup func(*gardenfake.Backend)
		state gardenapi.State
		keys  []string
	} {
		return struct {
			name  string
			setup func(*gardenfake.Backend)
			state gardenapi.State
			keys  []string
		}{name, func(f *gardenfake.Backend) {
			long2(f)
			if extra != nil {
				extra(f)
			}
		}, state, keys}
	}
	return []struct {
		name  string
		setup func(*gardenfake.Backend)
		state gardenapi.State
		keys  []string
	}{
		scene("GS-01", gardenapi.StateNone, nil),
		scene("GS-01 connect selected", gardenapi.StateNone, nil, "down"),
		scene("GS-01 skip selected", gardenapi.StateNone, nil, "down", "down"),
		scene("GS-02", gardenapi.StateNone, ambiguous),
		scene("GS-03", gardenapi.StateNone, nil, "down", "enter"),
		scene("GS-03 empty", gardenapi.StateNone, func(f *gardenfake.Backend) { f.ProjectsView.Projects = nil }, "down", "enter"),
		scene("GS-04", gardenapi.StateNone, nil, "enter"),
		scene("GS-04 blocked", gardenapi.StateNone, func(f *gardenfake.Backend) {
			f.PlanView.Rows[1].Conflict = true
			f.PlanView.Summary.Conflicts = 1
			f.PlanView.Summary.CanApply = false
		}, "enter"),
		scene("GS-05", gardenapi.StateNone, nil, "enter", "enter"),
		scene("GS-05 apply focused", gardenapi.StateNone, nil, "enter", "enter", "right"),
		scene("GS-06 partial", gardenapi.StatePartialSetup, nil),
		scene("GS-06 awaiting", gardenapi.StateAwaitingManual, func(f *gardenfake.Backend) {
			op := gardenfake.SampleOperation(gardenapi.StatePartialSetup)
			op.AwaitingManual = []string{long + " " + long}
			f.StatusView.Operation = &op
		}),
		scene("GS-06 running", gardenapi.StateOperationInProgress, nil),
		scene("GS-07", gardenapi.StateReadOnly, nil),
		scene("GS-08", gardenapi.StateSkipped, nil),
		scene("board", gardenapi.StateConnected, nil),
		scene("error", gardenapi.StateNone, func(f *gardenfake.Backend) {
			f.StatusErr = &gardenapi.Error{Code: gardenapi.CodeAuthRequired, Message: long + " " + long, Next: long, Fix: long + long}
		}),
		scene("refresh error", gardenapi.StateNone, func(f *gardenfake.Backend) {
			f.StatusView.RefreshError = &gardenapi.Error{Code: gardenapi.CodeOffline, Message: long + " " + long}
			f.StatusView.RemoteChanged = true
		}),
	}
}

func TestGardenScreensFitEverySizeAndASCII(t *testing.T) {
	long := strings.Repeat("very-long-name-", 6)
	for _, ascii := range []bool{false, true} {
		for _, size := range [][2]int{{80, 24}, {120, 35}, {160, 48}, {64, 10}, {100, 12}, {99, 24}, {70, 14}} {
			for _, sc := range gardenScenes(long) {
				fb := gardenFake(sc.state)
				sc.setup(fb)
				prefs := config.Prefs{}
				if ascii {
					prefs.AgentGlyphs = "ascii"
				}
				m := gardenEnv(t, fb, prefs, size[0], size[1])
				m = pressAll(t, m, sc.keys...)
				label := fmt.Sprintf("%s ascii=%v %dx%d", sc.name, ascii, size[0], size[1])
				v := m.View()
				assertBounded(t, label, v, size[0], size[1])
				if got := len(strings.Split(v, "\n")); got != size[1] {
					t.Errorf("%s: %d lines, want exactly %d", label, got, size[1])
				}
				if ascii {
					for _, r := range view(m) {
						if r > 127 {
							t.Errorf("%s: non-ASCII %q in\n%s", label, string(r), view(m))
							break
						}
					}
				}
			}
		}
	}
}

func TestGardenScreensKeepTheirActionsAtEverySize(t *testing.T) {
	long := strings.Repeat("very-long-name-", 6)
	actions := map[string]string{
		"GS-01": "Preview setup plan…", "GS-03": "Preview setup plan…", "GS-04": "Review and apply…", "GS-05": "[Apply setup]",
		"GS-06 partial": "Resume setup ›", "GS-07": "Open Board ›", "GS-08": "Set up Garden…", "error": "Retry",
	}
	for _, size := range [][2]int{{80, 24}, {120, 35}, {160, 48}} {
		for _, sc := range gardenScenes(long) {
			want, ok := actions[sc.name]
			if !ok {
				continue
			}
			fb := gardenFake(sc.state)
			sc.setup(fb)
			m := gardenEnv(t, fb, config.Prefs{}, size[0], size[1])
			m = pressAll(t, m, sc.keys...)
			mustContain(t, fmt.Sprintf("%s %dx%d", sc.name, size[0], size[1]), view(m), want)
		}
	}
}

func TestGardenWideLayoutHasRailAndNarrowCollapsesIt(t *testing.T) {
	fb := gardenFake(gardenapi.StateNone)
	wide := view(gardenEnv(t, fb, config.Prefs{}, 120, 35))
	mustContain(t, "wide", wide, "SET UP", "CREATE GARDEN", "Project scope", "Existing issues")
	narrow := view(gardenEnv(t, gardenFake(gardenapi.StateNone), config.Prefs{}, 80, 24))
	mustNotContain(t, "narrow", narrow, "SET UP ", "Project scope  ")
	mustContain(t, "narrow", narrow, "new labels and a Project", "reuse a Project you own", "stay off; reversible", "project scope ✓")
	planWide := view(pressAll(t, gardenEnv(t, gardenFake(gardenapi.StateNone), config.Prefs{}, 120, 35), "enter"))
	mustContain(t, "plan wide", planWide, "KIND", "NOTE", "CHANGE", "TARGET")
	planNarrow := view(pressAll(t, gardenEnv(t, gardenFake(gardenapi.StateNone), config.Prefs{}, 80, 24), "enter"))
	mustContain(t, "plan narrow", planNarrow, "CHANGE", "TARGET")
	mustNotContain(t, "plan narrow", planNarrow, "KIND", "NOTE")
}

func TestGardenHeaderAndSubmenu(t *testing.T) {
	m := gardenEnv(t, gardenFake(gardenapi.StateNone), config.Prefs{}, 120, 35)
	lines := strings.Split(view(m), "\n")
	mustContain(t, "header", lines[0], "acme/my-app · ○ not set up")
	mustContain(t, "submenu", lines[4], "✚ Setup", "▦ Board", "▤ Table", "✎ Drafts")
	for _, state := range []gardenapi.State{gardenapi.StateSkipped, gardenapi.StateReadOnly, gardenapi.StateConnected, gardenapi.StatePartialSetup} {
		m := gardenEnv(t, gardenFake(state), config.Prefs{}, 120, 35)
		mustContain(t, string(state), strings.Split(view(m), "\n")[0], "acme/my-app · ", gardenStateLabel(state))
	}
}

func TestGardenTabTogglesBoardOnlyWhenReadable(t *testing.T) {
	m := gardenEnv(t, gardenFake(gardenapi.StateReadOnly), config.Prefs{}, 120, 35)
	m = pressAll(t, m, "tab")
	if m.gardenScreen() != scrBoard {
		t.Fatalf("Tab on read-only: screen %d", m.gardenScreen())
	}
	m = pressAll(t, m, "tab")
	if m.gardenScreen() != scrReadOnly {
		t.Fatalf("Tab again: screen %d", m.gardenScreen())
	}
	m = gardenEnv(t, gardenFake(gardenapi.StateNone), config.Prefs{}, 120, 35)
	m = pressAll(t, m, "tab")
	if m.gardenScreen() != scrStart {
		t.Errorf("Tab before a Garden exists changed the screen to %d", m.gardenScreen())
	}
	mustNotContain(t, "hints", view(m), "tab submenu")
}

func TestGardenFooterHintsTrackTheScreen(t *testing.T) {
	m := gardenEnv(t, gardenFake(gardenapi.StateNone), config.Prefs{}, 160, 48)
	hints := func(m Model) string { l := strings.Split(view(m), "\n"); return l[len(l)-2] }
	mustContain(t, "GS-01", hints(m), "↑/↓ choice", "enter preview plan", "esc parent", "R refresh")
	m = pressAll(t, m, "down")
	mustContain(t, "connect", hints(m), "enter choose Project")
	m = pressAll(t, m, "down")
	mustContain(t, "skip", hints(m), "enter skip")
	m = pressAll(t, m, "up", "up", "enter")
	mustContain(t, "plan", hints(m), "↑/↓ step", "enter review and apply", "esc back")
	m = pressAll(t, m, "enter")
	mustContain(t, "confirm", hints(m), "←/→ button", "esc cancel")
}

func TestGardenShowsLoadingLineWhileStatusIsInFlight(t *testing.T) {
	fb := gardenFake(gardenapi.StateNone)
	release := make(chan struct{})
	fb.Hook = func(method string) {
		if method == "Status" {
			<-release
		}
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	st, err := config.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	m := sized(t, New(t.TempDir(), &config.Config{}, st, WithLayout(LayoutRedesign), WithGarden(fb)), 80, 24)
	cmd := m.Init()
	if cmd == nil {
		t.Fatal("opening on Garden must start the status read")
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	m, _ = send(t, m, keyOf("enter"))
	mustContain(t, "loading", view(m), "Reading GitHub for this repository…", "reading")
	if m.gardenScreen() != scrLoading {
		t.Fatalf("screen %d", m.gardenScreen())
	}
	close(release)
	m, _ = send(t, m, <-done)
	if m.gardenScreen() != scrStart {
		t.Errorf("screen %d after the reply", m.gardenScreen())
	}
}

func TestGardenLeavingTheWorkspaceDropsAnInFlightRead(t *testing.T) {
	fb := gardenFake(gardenapi.StateNone)
	release := make(chan struct{})
	fb.Hook = func(method string) {
		if method == "PreviewPlan" {
			<-release
		}
	}
	m := gardenEnv(t, fb, config.Prefs{}, 120, 35)
	m, cmd := send(t, m, keyOf("enter"))
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	m, switchCmd := m.selectWorkspace(wsPlayground)
	m, _ = drainAll(t, m, switchCmd)
	close(release)
	m, _ = send(t, m, <-done)
	if m.garden.step != stepStart || len(m.garden.plan.Rows) != 0 {
		t.Errorf("a plan reply opened step %d after the user left", m.garden.step)
	}
}
