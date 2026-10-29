package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/launch"
	gardenfake "github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi/fake"
)

// handoffLogScreens logs the shell at the two reference sizes and leaves the model at the wide one.
func handoffLogScreens(t *testing.T, step string, m Model) Model {
	t.Helper()
	for _, sz := range [][2]int{{120, 35}, {80, 24}, {120, 35}} {
		m, _ = send(t, m, tea.WindowSizeMsg{Width: sz[0], Height: sz[1]})
		mustFit(t, step, m)
		if sz[0] == 80 || !strings.Contains(step, "\x00") {
			t.Logf("%s %dx%d:\n%s", strings.TrimSuffix(step, "\x00"), sz[0], sz[1], view(m))
		}
		step += "\x00" // the second wide pass only restores the size
	}
	return m
}

// TestGardenHandoffFakeEndToEnd drives the real TUI through review, requested changes, rework, a second review,
// acceptance and a merged pull request against the scripted handoff. Nothing merges or cleans: the fake has no
// such call, and the launch fake counts the ones it has.
func TestGardenHandoffFakeEndToEnd(t *testing.T) {
	m, hb, fb, a := handoffUIEnv(t, 120, 35, gardenfake.HandoffNoReviewNoChecks, true, "")
	hb.Acknowledgments = []string{"no-ai-review", "checks-missing-or-failed"}
	mustContain(t, "human review", view(m), "YOUR REVIEW", "No AI review", "No checks recorded", "Accept")
	m = handoffLogScreens(t, "1 human review", m)

	// Request changes with text: a preview, then its own confirmation.
	hb.View = handoffRetarget(gardenfake.SampleHandoff(gardenfake.HandoffChangesRequested), a.ID)
	m = plannerPress(t, m, "c")
	m = typed(t, m, "Rename the counter and cover the overflow case.")
	m = plannerPress(t, m, "ctrl+s")
	mustContain(t, "request changes", view(m), "REQUEST CHANGES", "Rename the counter", "Rework does not start automatically.")
	m = handoffLogScreens(t, "2 request changes confirmation", m)
	m = plannerPress(t, m, "tab", "enter")
	if len(hb.Decided) != 1 || hb.Decided[0].Decision != garden.HandoffChangesRequested || hb.Decided[0].Text == "" {
		t.Fatalf("decided = %+v", hb.Decided)
	}
	mustContain(t, "changes requested", view(m), "changes requested", "Rework")
	if len(hb.Reworked) != 0 {
		t.Fatal("requesting changes started the rework")
	}

	// Rework is a separate confirmed action; the fake returns the running attempt.
	running := a
	running.State, running.Revision = launch.StateRunning, a.Revision+1
	hb.ReworkResult.Attempt = running
	fb.attempts[0] = running
	m = plannerPress(t, m, "m")
	mustContain(t, "rework", view(m), "START REWORK", "Rename the counter and cover the overflow case.")
	m = handoffLogScreens(t, "3 rework confirmation", m)
	m = plannerPress(t, m, "tab", "enter")
	if len(hb.Reworked) != 1 {
		t.Fatalf("reworked = %d", len(hb.Reworked))
	}
	mustContain(t, "supervising the rework", view(m), "IMPLEMENTING THE APPROVED PLAN")
	m = handoffLogScreens(t, "4 rework running", m)

	// The attempt comes back to human review (no AI review, no checks again).
	back := a
	back.State, back.Revision = launch.StateAwaitingReview, a.Revision+2
	fb.attempts[0] = back
	hb.View = handoffRetarget(gardenfake.SampleHandoff(gardenfake.HandoffNoReviewNoChecks), a.ID)
	m, cmd := send(t, m, gardenLaunchOverviewTickMsg{gen: m.garden.launch.overviewGen, issue: 1})
	m = plannerRun(t, m, cmd)
	mustContain(t, "second review", view(m), "YOUR REVIEW", "No checks recorded")
	m = handoffLogScreens(t, "5 back in human review", m)

	// Accept with every acknowledgment; the record is written once.
	hb.View = handoffRetarget(gardenfake.SampleHandoff(gardenfake.HandoffAcceptedAwaitingMerge), a.ID)
	m = plannerPress(t, m, "a")
	mustContain(t, "accept", view(m), "ACCEPT THIS WORK", "[ ] 1", "[ ] 2", "Merging remains a separate action.")
	m = handoffLogScreens(t, "6 accept confirmation", m)
	m = plannerPress(t, m, "1", "2", "tab", "tab", "tab", "enter")
	if len(hb.Decided) != 2 || hb.Decided[1].Decision != garden.HandoffAccepted || len(hb.Decided[1].Acknowledgments) != 2 {
		t.Fatalf("decided = %+v", hb.Decided)
	}
	mustContain(t, "accepted", view(m), "Your decision: accepted", "Waiting for the pull request to be merged")
	if strings.Contains(view(m), "COMPLETED") {
		t.Fatal("accepted work shown as done before the merge")
	}
	m = handoffLogScreens(t, "7 accepted, awaiting merge", m)

	// GitHub reports the squash merge: the next read says done.
	hb.View = handoffRetarget(gardenfake.SampleHandoff(gardenfake.HandoffDone), a.ID)
	m, cmd = send(t, m, gardenHandoffTickMsg{gen: m.garden.handoff.tickGen, issue: 1})
	m = plannerRun(t, m, cmd)
	mustContain(t, "done", view(m), "COMPLETED", "✓ Done", "merged into base", "commit 5555555", "The worktree is retained until you choose to clean it up.")
	// Controls render only Allowed actions, so a refused one is never a button, whatever the sample says.
	for _, c := range []struct {
		allowed bool
		label   string
	}{{hb.View.Actions.Accept.Allowed, "Accept…"}, {hb.View.Actions.RequestChanges.Allowed, "Request changes…"},
		{hb.View.Actions.Revoke.Allowed, "Revoke acceptance…"}, {hb.View.Actions.Rework.Allowed, "Start rework…"}} {
		if !c.allowed && strings.Contains(view(m), c.label) {
			t.Fatalf("done work draws a refused %q button", c.label)
		}
	}
	m = handoffLogScreens(t, "8 done", m)

	for _, c := range hb.Calls() {
		switch c {
		case "Handoff", "PreviewHandoffDecision", "DecideHandoff", "PreviewRework", "StartRework":
		default:
			t.Fatalf("unexpected call %s in %v", c, hb.Calls())
		}
	}
	if fb.cleanups != 0 || fb.cleanupPreviews != 0 || fb.publishes != 0 || fb.stops != 0 {
		t.Fatalf("merge or cleanup side effects: %v", fb.calls)
	}
}
