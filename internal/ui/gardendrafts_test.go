package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	gardensync "github.com/Tiago-0liveira/bonsai/internal/core/garden/sync"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
	gardenfake "github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi/fake"
)

var draftSizes = [][2]int{{80, 24}, {120, 35}, {160, 48}}

// draftsFake is a connected Garden holding one approved task and the three sample drafts.
func draftsFake(t *testing.T) *gardenfake.Backend {
	t.Helper()
	fb := gardenFake(gardenapi.StateConnected)
	fb.WorkView.Snapshot.Items = append([]gardensync.Item{boardItem(t, 1, garden.StageReady, false)}, gardenfake.SampleDraftItems()...)
	return fb
}

// draftsEnv opens the Drafts submenu with Tab, as a user does.
func draftsEnv(t *testing.T, fb *gardenfake.Backend, w, h int) Model {
	t.Helper()
	m := gardenEnv(t, fb, config.Prefs{}, w, h)
	m = pressAll(t, m, "tab")
	if m.gardenScreen() != scrDrafts {
		t.Fatalf("Tab did not open Drafts: screen %d\n%s", m.gardenScreen(), view(m))
	}
	return m
}

func mustFit(t *testing.T, label string, m Model) {
	t.Helper()
	lines := strings.Split(m.View(), "\n")
	if len(lines) != m.height {
		t.Errorf("%s: %d lines, want %d", label, len(lines), m.height)
	}
	for i, l := range lines {
		if w := ansi.StringWidth(l); w > m.width {
			t.Errorf("%s: line %d is %d wide, max %d: %q", label, i, w, m.width, ansi.Strip(l))
		}
	}
}

func typed(t *testing.T, m Model, s string) Model {
	t.Helper()
	for _, r := range s {
		m, _ = send(t, m, runeKey(string(r)))
	}
	return m
}

func TestDraftsInboxShowsEveryTypeSeparatelyFromTheBoard(t *testing.T) {
	for _, sz := range draftSizes {
		m := draftsEnv(t, draftsFake(t), sz[0], sz[1])
		out := view(m)
		mustContain(t, "drafts", out, "DRAFT", "#142", "#143", "#144", "Feature", "Bug", "Chore", "! attention", "Drafts 3")
		mustNotContain(t, "drafts", out, "Task 1")
		mustFit(t, "drafts", m)
		if sz[0] >= 120 {
			mustContain(t, "inspector", out, "NEXT HUMAN GATE", "Validate specification", "plan 08")
		}
		// Board keeps excluding drafts, and the Drafts entry stays amber-separate.
		b := pressAll(t, m, "v")
		if b.gardenScreen() != scrBoard {
			t.Fatalf("v did not return to the board: %d", b.gardenScreen())
		}
		mustNotContain(t, "board", view(b), "Per-repository agent budget", "Crash when the config")
		mustContain(t, "board", view(b), "Task 1")
	}
}

func TestDraftsEscReturnsToBoardAndTabCycles(t *testing.T) {
	m := draftsEnv(t, draftsFake(t), 120, 35)
	m = pressAll(t, m, "esc")
	if m.gardenScreen() != scrBoard {
		t.Fatalf("Esc: screen %d", m.gardenScreen())
	}
	m = pressAll(t, m, "tab")
	if m.gardenScreen() != scrDrafts {
		t.Fatalf("Tab: screen %d", m.gardenScreen())
	}
	m = pressAll(t, m, "tab")
	if m.gardenScreen() != scrBoard {
		t.Fatalf("Tab back: screen %d", m.gardenScreen())
	}
}

func TestDraftsSelectionFollowsIssueIDAcrossRefresh(t *testing.T) {
	fb := draftsFake(t)
	m := draftsEnv(t, fb, 120, 35)
	m = pressAll(t, m, "down")
	if m.garden.drafts.selected != 143 {
		t.Fatalf("selected %d", m.garden.drafts.selected)
	}
	// A new draft sorts before the selection; the selection stays on #143.
	fb.WorkView.Snapshot.Items = append(fb.WorkView.Snapshot.Items, gardenfake.SampleDraft(100, 100, "Earlier idea", garden.TypeChore))
	m = pressAll(t, m, "R")
	if m.garden.drafts.selected != 143 || m.garden.drafts.rowIndex() != 2 {
		t.Fatalf("selection moved: %d at row %d", m.garden.drafts.selected, m.garden.drafts.rowIndex())
	}
	// The selected draft is accepted elsewhere and leaves the inbox.
	var kept []gardensync.Item
	for _, it := range fb.WorkView.Snapshot.Items {
		if it.Issue.ID != 143 {
			kept = append(kept, it)
		}
	}
	fb.WorkView.Snapshot.Items = kept
	m = pressAll(t, m, "R")
	if m.garden.drafts.selected != 144 {
		t.Fatalf("selected %d after removal", m.garden.drafts.selected)
	}
	mustContain(t, "notice", view(m), "Selected draft left the inbox")
}

func TestDraftsSearchAndTypeFilter(t *testing.T) {
	m := draftsEnv(t, draftsFake(t), 120, 35)
	m = pressAll(t, m, "/")
	if m.currentShellFocus() != shellInput {
		t.Fatal("search does not own the keyboard")
	}
	m = typed(t, m, "crash")
	mustContain(t, "search", view(m), "#143")
	mustNotContain(t, "search", view(m), "#142", "#144")
	m = pressAll(t, m, "esc")
	mustContain(t, "cancelled", view(m), "#142", "#143", "#144")
	m = pressAll(t, m, "t")
	mustContain(t, "type", view(m), "Type: Feature", "#142")
	mustNotContain(t, "type", view(m), "#143")
	m = pressAll(t, m, "x")
	mustContain(t, "cleared", view(m), "#143", "Type: Any")
}

func TestDraftDetailShowsBodySpecAndAttention(t *testing.T) {
	for _, sz := range draftSizes {
		m := draftsEnv(t, draftsFake(t), sz[0], sz[1])
		m = pressAll(t, m, "enter")
		out := view(m)
		mustContain(t, "detail", out, "Per-repository agent budget limits", "ISSUE BODY", "Teams want a ceiling", "CURRENT SPECIFICATION", "Lines 1")
		mustNotContain(t, "detail", out, "bonsai:garden-metadata")
		mustFit(t, "detail", m)
		m = pressAll(t, m, "end")
		mustContain(t, "end", view(m), "Validate idea: unavailable until plan 08")
		m = pressAll(t, m, "esc")
		if m.garden.drafts.stage != draftsList || m.garden.drafts.selected != 142 {
			t.Fatalf("Esc: stage %d selected %d", m.garden.drafts.stage, m.garden.drafts.selected)
		}
	}
	m := draftsEnv(t, draftsFake(t), 120, 35)
	m = pressAll(t, m, "down", "down", "enter")
	mustContain(t, "attention", view(m), "Attention: competing revision heads", "COMPETING SPECIFICATION HEAD")
}

func TestCaptureHappyPathCreatesOnceAndSelectsTheNewDraft(t *testing.T) {
	for _, sz := range draftSizes {
		fb := draftsFake(t)
		m := draftsEnv(t, fb, sz[0], sz[1])
		m = pressAll(t, m, "n")
		if m.currentShellFocus() != shellInput || m.garden.drafts.stage != draftsForm {
			t.Fatalf("n did not open the form: stage %d focus %v", m.garden.drafts.stage, m.currentShellFocus())
		}
		mustContain(t, "form", view(m), "A NEW IDEA", "Title", "Type", "Description", "Create GitHub Issue")
		mustFit(t, "form", m)
		m = typed(t, m, "Add dark mode")
		m = pressAll(t, m, "tab", "tab", "right", "tab") // description, type, Bug... then the button
		if m.garden.drafts.form.focus != fieldPreview || captureTypes[m.garden.drafts.form.typ] != garden.TypeBug {
			t.Fatalf("focus %v type %v", m.garden.drafts.form.focus, captureTypes[m.garden.drafts.form.typ])
		}
		m = pressAll(t, m, "enter")
		out := view(m)
		mustContain(t, "preview", out, "REVIEW BEFORE CREATING", "Add dark mode", "Bug", "acme/my-app", "at Draft", "bonsai:stage/draft", "Nothing is approved")
		mustFit(t, "preview", m)
		if fb.Writes() != 0 {
			t.Fatal("preview wrote")
		}
		m = pressAll(t, m, "right", "enter") // Back to edit has the first focus; move to Create
		if fb.Count("Capture") != 1 || fb.Writes() != 1 {
			t.Fatalf("Capture calls %d", fb.Count("Capture"))
		}
		if m.garden.drafts.stage != draftsList || m.garden.drafts.selected != 9001 || m.garden.drafts.formOpen {
			t.Fatalf("after create: stage %d selected %d formOpen %v", m.garden.drafts.stage, m.garden.drafts.selected, m.garden.drafts.formOpen)
		}
		mustContain(t, "created", view(m), "#150", "Add dark mode", "Created #150 as a Draft.")
		mustFit(t, "created", m)
		if m.currentShellFocus() != shellBody {
			t.Fatalf("focus %v", m.currentShellFocus())
		}
	}
}

func TestCaptureFormKeepsTextThroughErrorsResizeAndWorkspaceSwitch(t *testing.T) {
	fb := draftsFake(t)
	m := draftsEnv(t, fb, 120, 35)
	m = pressAll(t, m, "n")
	m = typed(t, m, "q n r") // letters bound elsewhere must be text here
	m = pressAll(t, m, "tab")
	m = typed(t, m, "line one")
	m = pressAll(t, m, "enter")
	m = typed(t, m, "line two")
	if m.garden.drafts.form.title.Value() != "q n r" || m.garden.drafts.form.desc.Value() != "line one\nline two" {
		t.Fatalf("text = %q / %q", m.garden.drafts.form.title.Value(), m.garden.drafts.form.desc.Value())
	}
	// A failed preview keeps the form and says why.
	fb.PreviewCaptureErr = &gardenapi.Error{Code: gardenapi.CodeOffline, Message: "GitHub could not be reached."}
	m = pressAll(t, m, "tab", "tab", "enter")
	if m.garden.drafts.stage != draftsForm {
		t.Fatalf("stage %d", m.garden.drafts.stage)
	}
	mustContain(t, "error", view(m), "GitHub could not be reached.", "q n r", "line one", "line two")
	for _, sz := range draftSizes {
		m = sized(t, m, sz[0], sz[1])
		mustContain(t, "resized", view(m), "q n r", "line one")
		mustFit(t, "resized", m)
	}
	// Leaving the workspace and coming back resumes typing, not navigation.
	m, _ = m.selectWorkspace(wsSettings)
	m, cmd := m.selectWorkspace(wsGarden)
	m, _ = drainAll(t, m, cmd)
	m = pressAll(t, m, "enter")
	if m.garden.drafts.form.title.Value() != "q n r" || m.currentShellFocus() != shellInput {
		t.Fatalf("form lost: %q focus %v", m.garden.drafts.form.title.Value(), m.currentShellFocus())
	}
}

func TestCaptureEscapeAsksBeforeDiscardingText(t *testing.T) {
	fb := draftsFake(t)
	m := draftsEnv(t, fb, 120, 35)
	m = pressAll(t, m, "n", "esc")
	if m.garden.drafts.stage != draftsList || m.garden.drafts.formOpen {
		t.Fatal("clean form should close at once")
	}
	m = pressAll(t, m, "n")
	m = typed(t, m, "keep me")
	m = pressAll(t, m, "esc")
	mustContain(t, "discard", view(m), "Discard this idea?", "Keep editing")
	m = pressAll(t, m, "esc")
	if m.garden.drafts.form.title.Value() != "keep me" || m.garden.drafts.stage != draftsForm {
		t.Fatal("Esc on the question must keep editing")
	}
	m = pressAll(t, m, "esc", "right", "enter")
	if m.garden.drafts.formOpen || m.garden.drafts.stage != draftsList {
		t.Fatal("discard did not close")
	}
	if fb.Count("PreviewCapture") != 0 || fb.Writes() != 0 {
		t.Fatal("discard reached the backend")
	}
}

func TestCaptureNeedsATitleBeforeAnyCall(t *testing.T) {
	fb := draftsFake(t)
	m := draftsEnv(t, fb, 120, 35)
	m = pressAll(t, m, "n", "tab", "tab", "tab", "enter")
	mustContain(t, "title required", view(m), "Give the idea a title.")
	if fb.Count("PreviewCapture") != 0 {
		t.Fatal("empty title reached the backend")
	}
}

func TestCaptureUnknownOffersReconcileNeverRetry(t *testing.T) {
	fb := draftsFake(t)
	fb.CapturedView = &gardenapi.CaptureView{State: gardenapi.CaptureUnknown, Err: &gardenapi.Error{Code: gardenapi.CodeCaptureUnknown, Message: "The idea may already exist on GitHub.", Next: "Reconcile to check GitHub."}}
	m := draftsEnv(t, fb, 120, 35)
	m = pressAll(t, m, "n")
	m = typed(t, m, "Maybe created")
	m = pressAll(t, m, "tab", "tab", "tab", "enter", "right", "enter")
	out := view(m)
	mustContain(t, "unknown", out, "THE IDEA MAY ALREADY EXIST", "Reconcile", "will not create it again")
	mustNotContain(t, "unknown", out, "Retry")
	m = pressAll(t, m, "right", "enter") // Close
	if m.garden.drafts.stage != draftsList || !m.garden.drafts.formOpen {
		t.Fatalf("close: stage %d formOpen %v", m.garden.drafts.stage, m.garden.drafts.formOpen)
	}
	// n is refused while the outcome is unknown, so the same text cannot be created twice.
	m = pressAll(t, m, "n")
	if m.garden.drafts.stage != draftsList {
		t.Fatal("n opened a second capture over an unknown one")
	}
	mustContain(t, "blocked", view(m), "Reconcile it first")
	if fb.Count("Capture") != 1 {
		t.Fatalf("Capture called %d times", fb.Count("Capture"))
	}
}

func TestPendingCaptureFromARestartCanBeReconciled(t *testing.T) {
	fb := draftsFake(t)
	fb.WorkView.PendingCaptures = []gardenapi.CaptureView{{OperationID: "cap-old", State: gardenapi.CaptureUnknown, Title: "Before the crash"}}
	m := draftsEnv(t, fb, 120, 35)
	mustContain(t, "banner", view(m), "1 capture may already exist on GitHub", "r reconcile")
	m = pressAll(t, m, "n")
	mustContain(t, "blocked", view(m), "Reconcile it first")
	m = pressAll(t, m, "r")
	if fb.Count("ReconcileCapture") != 1 || len(m.garden.drafts.pending) != 0 {
		t.Fatalf("reconcile calls %d pending %d", fb.Count("ReconcileCapture"), len(m.garden.drafts.pending))
	}
	mustContain(t, "found", view(m), "#150")
}

func TestCaptureFailureKeepsTextAndRetryReusesTheOperation(t *testing.T) {
	fb := draftsFake(t)
	fb.CapturedView = &gardenapi.CaptureView{State: gardenapi.CaptureFailed, Err: &gardenapi.Error{Code: gardenapi.CodeValidation, Message: "GitHub rejected the request."}}
	m := draftsEnv(t, fb, 120, 35)
	m = pressAll(t, m, "n")
	m = typed(t, m, "Will fail once")
	m = pressAll(t, m, "tab", "tab", "tab", "enter", "right", "enter")
	mustContain(t, "failed", view(m), "NOTHING WAS CREATED", "Retry", "GitHub rejected the request.")
	fb.CapturedView = nil
	m = pressAll(t, m, "enter") // Retry has the first focus: nothing exists on GitHub
	if len(fb.Captured) != 2 || fb.Captured[0].OperationID != fb.Captured[1].OperationID {
		t.Fatalf("retry changed the operation: %+v", fb.Captured)
	}
	if m.garden.drafts.selected != 9001 {
		t.Fatalf("selected %d", m.garden.drafts.selected)
	}
}

func TestCaptureWriteIsNeverCancelledAndIgnoresKeys(t *testing.T) {
	fb := draftsFake(t)
	m := draftsEnv(t, fb, 120, 35)
	m = pressAll(t, m, "n")
	m = typed(t, m, "Slow")
	m = pressAll(t, m, "tab", "tab", "tab", "enter")
	release := make(chan struct{})
	fb.Hook = func(method string) {
		if method == "Capture" {
			<-release
		}
	}
	m, cmd := send(t, m, keyOf("right"))
	if cmd != nil {
		t.Fatal("unexpected command")
	}
	m, cmd = send(t, m, keyOf("enter"))
	if cmd == nil || m.garden.busy != busyCapture {
		t.Fatalf("busy %v", m.garden.busy)
	}
	mustContain(t, "pending", view(m), "Creating the Issue")
	for _, k := range []string{"esc", "enter", "n", "tab"} {
		var c tea.Cmd
		m, c = send(t, m, keyOf(k))
		if c != nil || m.garden.busy != busyCapture || m.garden.drafts.stage != draftsPreview {
			t.Fatalf("%s was not swallowed", k)
		}
	}
	close(release)
	m, _ = drainAll(t, m, cmd)
	if fb.Count("Capture") != 1 || m.garden.drafts.stage != draftsList {
		t.Fatalf("Capture %d stage %d", fb.Count("Capture"), m.garden.drafts.stage)
	}
}

func TestReadOnlyAccessDisablesCaptureWithAReason(t *testing.T) {
	fb := draftsFake(t)
	fb.StatusView.Access = gardenapi.AccessRead
	fb.StatusView.Target.Access = gardenapi.AccessRead
	m := draftsEnv(t, fb, 120, 35)
	m = pressAll(t, m, "n")
	mustContain(t, "read-only", view(m), "needs Write access to acme/my-app")
	if m.garden.drafts.stage != draftsList || fb.Count("PreviewCapture") != 0 || fb.Writes() != 0 {
		t.Fatal("read-only user reached the capture flow")
	}
}

func TestDraftsEmptyAndUnknownStates(t *testing.T) {
	fb := gardenFake(gardenapi.StateConnected)
	m := draftsEnv(t, fb, 80, 24)
	mustContain(t, "empty", view(m), "no draft ideas yet", "press n")
	mustFit(t, "empty", m)
}
