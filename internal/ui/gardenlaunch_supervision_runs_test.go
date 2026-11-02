package ui

import (
	tea "github.com/charmbracelet/bubbletea"

	"strings"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/launch"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/overlay"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
)

func TestGardenLaunchStopClosedRunOfRecoverableAttemptSettlesPolling(t *testing.T) {
	m, fb, _ := launchTestStop(t)
	// An unacknowledged start that was stopped is closed but recoverable, which is still an active state.
	fb.attempts[0].State, fb.attempts[0].Revision = launch.StateRecoverable, fb.attempts[0].Revision+1
	fb.attempts[0].Runs[0].State, fb.attempts[0].Runs[0].Acknowledged = "not-started", false
	m = launchTestPoll(t, m)
	l := m.garden.launch
	if l.stopAttempt != "" || l.stopPending || l.stopPolls != 1 {
		t.Fatalf("proven closure kept polling: attempt=%q pending=%v polls=%d", l.stopAttempt, l.stopPending, l.stopPolls)
	}
	out := view(m)
	mustContain(t, "recoverable", out, "recoverable", "Next: retry (r) once the holder is closed")
	mustNotContain(t, "recoverable", out, "closure not yet proven", "Stopping…")
}

func TestGardenLaunchMouseBackToOverviewArmsRefresh(t *testing.T) {
	m, _, _, _ := fakeLaunchUIHistory(t, 120, 35, false)
	m = plannerPress(t, m, "esc", "right")
	if m.garden.launch.overviewPending {
		t.Fatal("leaving the Overview tab kept its timer")
	}
	x0, y0, w, h := overlay.Center(m.width, m.height)
	_, hits := m.issueOverlayRows(w, h)
	for _, hit := range hits {
		if hit.tab == 0 {
			var cmd tea.Cmd
			m, cmd = send(t, m, click(x0+hit.x0, y0+hit.y))
			if cmd == nil || !m.garden.launch.overviewPending || m.garden.board.section != 0 {
				t.Fatal("clicking the Overview tab did not arm the overview refresh")
			}
			return
		}
	}
	t.Fatal("no Overview tab to click")
}

func TestGardenLaunchMirrorNamesOutstandingTargetStage(t *testing.T) {
	a := launch.Attempt{MirroredStage: garden.StageInProgress, DesiredStage: garden.StageReview, Steps: map[launch.Step]launch.StepRecord{launch.StepMirror: {State: launch.StepPending}}}
	if got := launchMirrorText(a); got != "stage update to Review: pending" {
		t.Fatalf("offline exit: %q", got)
	}
	a.Steps[launch.StepMirror] = launch.StepRecord{State: launch.StepDone}
	if got := launchMirrorText(a); got != "stage update to Review: pending" {
		t.Fatalf("earlier write must not count for the new target: %q", got)
	}
	a.MirroredStage = garden.StageReview
	if got := launchMirrorText(a); got != "stage update to Review: written" {
		t.Fatalf("written: %q", got)
	}
}

func TestGardenLaunchAttemptDetailIdentifiesSelectedOlderRun(t *testing.T) {
	for _, size := range [][2]int{{120, 35}, {80, 24}} {
		m, _, fb, a := fakeLaunchUIHistory(t, size[0], size[1], false)
		start := time.Now().Add(-time.Hour)
		end := start.Add(90 * time.Second)
		code := 0
		fb.attempts[0].Runs = []launch.Run{
			{Number: 1, SessionID: "sess_old", State: "exited", Acknowledged: true, StartedAt: &start, EndedAt: &end, ExitCode: &code},
			{Number: 2, SessionID: "sess_new", State: "running", Acknowledged: true},
		}
		fb.attempts[0].Revision = a.Revision + 1
		m = plannerPress(t, m, "R", "enter")
		m = plannerPress(t, m, "[")
		cur, _ := m.garden.launch.current()
		out := m.launchSupervision(cur, time.Now())
		mustContain(t, "older run", out, "Selected run 1 / sess_old / exited / started", "01:30 elapsed", "newest run (run 2) only")
		if id, _ := m.garden.launch.runSession(cur); id != "sess_old" || m.launchSessionSel(cur).ID != "sess_old" {
			t.Fatalf("Open session would attach %q, not the run shown", id)
		}
		mustContain(t, "view", view(m), "sess_old")
		mustFit(t, "attempt detail", m)
		m = plannerPress(t, m, "]")
		cur, _ = m.garden.launch.current()
		mustContain(t, "newest run", m.launchSupervision(cur, time.Now()), "Selected run 2 / sess_new / running")
	}
}

func TestGardenLaunchReadyImplementerIsLabeledSavedDefault(t *testing.T) {
	m, _, _ := fakeLaunchUIEnv(t, 120, 35)
	acc := m.agents.api.Accounts.(*fakeLaunchUIAccounts)
	// An effort-qualified model whose effort ResolveLaunch would drop: the saved value is not the effective one.
	acc.resolution = agents.LaunchResolution{Launch: agents.LaunchOptions{Model: "gemini-3.8-flash-high", Effort: "low"}}
	m = plannerPress(t, m, "enter")
	mustContain(t, "saved default", m.launchReadyOverview(), "· low (saved default)")
	m.garden.launchReady.cached = false
	mustNotContain(t, "uncached", m.launchReadyOverview(), "(saved default)")
}

func TestGardenLaunchRetainedIdleIsNotClaimedAsRunning(t *testing.T) {
	now := time.Now()
	obs := gardenapi.LaunchActivity{Activity: "idle", ObservedAt: now.Add(-5 * time.Second)}
	for _, tc := range []struct {
		name  string
		state launch.State
		stale bool
		want  string
		not   string
	}{
		{"live", launch.StateRunning, false, "process still running", ""},
		{"stale", launch.StateRunning, true, "Last observed: idle · 5s ago", "process still running"},
		{"recoverable", launch.StateRecoverable, false, "Last observed: idle · 5s ago", "process still running"},
		{"stopped", launch.StateStopped, false, "Stopped", "process still running"},
		{"stopped stale", launch.StateStopped, true, "Stopped", "Last observed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := Model{layoutKind: LayoutRedesign}
			a := launch.Attempt{ID: "a", State: tc.state}
			m.garden.launch.activity = map[string]gardenapi.LaunchActivity{a.ID: obs}
			m.garden.launch.activityStale = map[string]bool{a.ID: tc.stale}
			out := m.launchSupervision(a, now)
			if !strings.Contains(out, tc.want) || tc.not != "" && strings.Contains(out, tc.not) {
				t.Fatalf("want %q without %q:\n%s", tc.want, tc.not, out)
			}
		})
	}
}
