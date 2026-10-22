package ui

import (
	"context"
	"strings"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/agentactivity"
	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/agentterminal"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/launch"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
	agentfake "github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi/fake"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
	tea "github.com/charmbracelet/bubbletea"
)

func fakeLaunchUIHistory(t *testing.T, w, h int, session bool) (Model, plannerRig, *fakeLaunchUIBackend, launch.Attempt) {
	t.Helper()
	m, rig, fb := fakeLaunchUIEnv(t, w, h)
	p, err := fb.PreviewLaunch(context.Background(), gardenapi.LaunchPreviewRequest{IssueID: 1, AccountID: "c1", Branch: "bonsai/101-retained", BaseRef: "main", RemoteName: "origin", Name: "Implement #101", Cols: 100, Rows: 25})
	if err != nil {
		t.Fatal(err)
	}
	a := fakeLaunchUIAttempt(p.Preview)
	fb.attempts = []launch.Attempt{a}
	if session {
		rig.agents.Sessions.Add(agentsapi.Session{Summary: agentterminal.Summary{ID: a.SessionID, ProjectID: "project", WorktreeID: "wt-impl", Provider: a.Provider, AccountID: a.AccountID, Name: a.Name, State: "running", OperationID: a.Runs[0].OperationID, Task: &agents.TaskRef{RepositoryID: uint64(a.Repo), IssueID: uint64(a.Issue), AttemptID: a.ID, Role: "implementation"}}})
	}
	m = plannerPress(t, m, "enter", "i")
	return m, rig, fb, a
}
func fakeLaunchUIOpenTerminal(t *testing.T, m Model, rig plannerRig) (Model, *agentfake.Stream) {
	t.Helper()
	for i := 0; i < 3 && len(rig.agents.Sessions.Attaches()) == 0; i++ {
		m = plannerPress(t, m, "enter")
	}
	calls := rig.agents.Sessions.Attaches()
	if len(calls) == 0 {
		t.Fatalf("attempt could not open stored terminal:\n%s", view(m))
	}
	streams := rig.agents.Sessions.Streams(calls[len(calls)-1].ID)
	if len(streams) == 0 {
		t.Fatalf("terminal attach did not produce stream:\n%s", view(m))
	}
	return m, streams[len(streams)-1]
}
func TestGardenLaunchTerminalUsesStoredRunWithoutWorktreeSelection(t *testing.T) {
	for _, size := range [][2]int{{120, 35}, {80, 24}} {
		m, rig, fb, a := fakeLaunchUIHistory(t, size[0], size[1], true)
		m, st := fakeLaunchUIOpenTerminal(t, m, rig)
		calls := rig.agents.Sessions.Attaches()
		if calls[0].ID != a.Runs[0].SessionID || !calls[0].Opts.ReadOnly || calls[0].Opts.Take || st.Writer() {
			t.Fatalf("history attach must observe exact stored run: %+v", calls)
		}
		if len(fb.starts) != 0 || len(rig.agents.Sessions.Starts()) != 0 {
			t.Fatal("opening existing session substituted a new start")
		}
		m = plannerFrames(t, m, st, agentterminal.Frame{Type: "output", Data: []byte("implementation output"), Offset: 0}, status("running"))
		mustContain(t, "stored terminal", view(m), "implementation output")
		mustFit(t, "terminal", m)
	}
}
func TestGardenLaunchTerminalNavigationDetachesWithoutStopping(t *testing.T) {
	for _, mode := range []string{"back", "workspace"} {
		t.Run(mode, func(t *testing.T) {
			m, rig, fb, _ := fakeLaunchUIHistory(t, 120, 35, true)
			m, st := fakeLaunchUIOpenTerminal(t, m, rig)
			if mode == "back" {
				m = plannerPress(t, m, "esc")
			} else {
				var cmd tea.Cmd
				m, cmd = m.selectWorkspace(wsAccounts)
				m = plannerRun(t, m, cmd)
			}
			if !st.Closed() {
				t.Fatal("navigation leaked terminal stream")
			}
			if fb.stops != 0 || len(fb.starts) != 0 {
				t.Fatalf("navigation changed holder: stop=%d starts=%d", fb.stops, len(fb.starts))
			}
		})
	}
}
func TestGardenLaunchTerminalMissingSessionDoesNotStartReplacement(t *testing.T) {
	m, rig, fb, a := fakeLaunchUIHistory(t, 120, 35, false)
	m = plannerPress(t, m, "enter", "enter")
	if len(fb.starts) != 0 || len(rig.agents.Sessions.Starts()) != 0 {
		t.Fatal("missing session started a replacement")
	}
	out := strings.ToLower(view(m))
	if !strings.Contains(out, "unavailable") && !strings.Contains(out, "missing") && !strings.Contains(out, "ended") && !strings.Contains(out, "not found") {
		t.Fatalf("missing %s not explained:\n%s", a.SessionID, view(m))
	}
}
func TestGardenLaunchTerminalEndedAndIdleDoNotInventCompletionOrDone(t *testing.T) {
	m, rig, fb, a := fakeLaunchUIHistory(t, 120, 35, true)
	m, st := fakeLaunchUIOpenTerminal(t, m, rig)
	m = plannerFrames(t, m, st, status("running"))
	if fb.attempts[0].State != launch.StateRunning {
		t.Fatal("live holder changed attempt")
	}
	idle := agentterminal.Summary{ID: a.SessionID, State: "running", Activity: &agentactivity.Activity{State: agentactivity.Idle, Reason: agentactivity.ReasonTurnComplete}}
	m = plannerFrames(t, m, st, agentterminal.Frame{Type: "status", Status: &idle})
	if fb.attempts[0].State != launch.StateRunning || fb.publishes != 0 {
		t.Fatal("completed turn fabricated process completion or publication")
	}

	ended := agentterminal.Summary{ID: a.SessionID, State: "exited", ExitCode: fakeLaunchUIExitCode(0)}
	fb.attempts[0].State = launch.StateAwaitingReview
	fb.attempts[0].Runs[0].State = "exited"
	fb.attempts[0].Runs[0].ExitCode = ended.ExitCode
	m = plannerFrames(t, m, st, agentterminal.Frame{Type: "status", Status: &ended})
	if m.agents.view != nil && m.agents.view.control {
		t.Fatal("ended implementation retained terminal keyboard")
	}
	if len(fb.starts) != 0 || fb.publishes != 0 || fb.cleanups != 0 {
		t.Fatal("exit automatically launched/published/cleaned")
	}
	m = plannerPress(t, m, "esc")
	mustContain(t, "ended attempt", view(m), "awaiting-human-review")
	mustNotContain(t, "no fabricated review", view(m), "automatic merge complete", "verification passed")
}
func fakeLaunchUIExitCode(n int) *int { return &n }
func TestGardenLaunchTerminalReusesGenerationOffsetAndWriterArbitration(t *testing.T) {
	m, rig, _, a := fakeLaunchUIHistory(t, 120, 35, true)
	m, st := fakeLaunchUIOpenTerminal(t, m, rig)
	m = plannerFrames(t, m, st, agentterminal.Frame{Type: "output", Offset: 0, Data: []byte("retained output")}, status("running"))
	v := m.agents.view
	if v == nil || v.generation != st.Generation() || v.offset == 0 {
		t.Fatalf("shared replay state not retained: %+v", v)
	}
	savedOffset := v.offset
	opts := v.attachOptions()
	if opts.Generation != st.Generation() || opts.Offset != savedOffset {
		t.Fatalf("reattach lost replay identity: %+v", opts)
	}
	if len(st.Inputs()) != 0 {
		t.Fatal("observing attachment sent input")
	}
	m = plannerPress(t, m, "enter")
	m = plannerPump(t, m)
	if m.agents.view == nil || !m.agents.view.control {
		t.Fatal("explicit terminal focus did not offer control")
	}
	streams := rig.agents.Sessions.Streams(a.SessionID)
	current := streams[len(streams)-1]
	if !current.Writer() {
		t.Fatal("control mode did not arbitrate writer")
	}
	m = plannerFrames(t, m, current, status("running"))
	m = plannerPress(t, m, "x")
	m = plannerPump(t, m)
	if len(current.Inputs()) == 0 {
		t.Fatal("controlled terminal did not forward input")
	}
	if len(rig.agents.Sessions.Starts()) != 0 {
		t.Fatalf("take-control started another %s", a.SessionID)
	}
	var cmd tea.Cmd
	m, cmd = send(t, m, tea.WindowSizeMsg{Width: 90, Height: 28})
	m = plannerRun(t, m, cmd)
	mustFit(t, "resized implementation terminal", m)
}
