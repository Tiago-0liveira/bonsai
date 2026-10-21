package ui

import (
	"reflect"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/launch"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
)

func TestLaunchForeignStartReplyIsNotMerged(t *testing.T) {
	m, _, _, a := fakeLaunchUIHistory(t, 120, 35, false)
	foreign := a
	foreign.ID, foreign.Local = "impl-foreign", "foreign-clone"
	st := m.garden.gate.Last()
	st.LocalRepoID = "foreign-clone"
	issue := uint64(m.garden.board.opened)
	m, _ = m.onGardenLaunchStart(gardenLaunchStartMsg{issue: issue, scope: scopeOfAttempt(a),
		view: gardenapi.LaunchAttemptView{Stamp: st, IssueID: issue, Attempt: foreign}})
	for _, got := range m.garden.launch.attempts {
		if got.ID == foreign.ID {
			t.Fatal("foreign clone accepted into current history")
		}
	}
}

func TestLaunchOlderReadNeverOverwritesNewerAttempt(t *testing.T) {
	m, _, _, a := fakeLaunchUIHistory(t, 120, 35, false)
	newer := a
	newer.Revision, newer.State = 9, launch.StateAwaitingReview
	m.garden.launch.replaceAttempts([]launch.Attempt{newer})
	st := m.garden.gate.Last()
	st.Generation = 1
	issue := uint64(m.garden.board.opened)
	m, _ = m.onGardenLaunchAttempts(gardenLaunchAttemptsMsg{seq: m.garden.launch.readSeq, issue: issue,
		view: gardenapi.LaunchAttemptsView{Stamp: st, IssueID: issue, Attempts: []launch.Attempt{a}}})
	if m.garden.launch.attempts[0].Revision != 9 {
		t.Fatal("older reply overwrote a newer attempt")
	}
}

func TestLaunchRejectedRefreshKeepsConfirmationOff(t *testing.T) {
	m, _, fb := fakeLaunchUIEnv(t, 120, 35)
	m = fakeLaunchUIReview(t, m)
	old := fb.lastPreview
	m, _ = send(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	if !m.garden.launch.stale {
		t.Fatal("resize did not mark the confirmation stale")
	}
	m, _ = m.onGardenLaunchPreview(gardenLaunchPreviewMsg{seq: m.garden.launch.previewSeq, issue: uint64(m.garden.board.opened), view: old})
	if m.launchConfirmable() {
		t.Fatal("a rejected refresh re-enabled the old confirmation")
	}
}

func TestLaunchResizeRoundTripNeverConfirmsOtherDimensions(t *testing.T) {
	m, _, _ := fakeLaunchUIEnv(t, 120, 35)
	m = fakeLaunchUIReview(t, m)
	m, first := send(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m, second := send(t, m, tea.WindowSizeMsg{Width: 120, Height: 35})
	m = plannerRun(t, m, first)
	m = plannerRun(t, m, second)
	cols, rows := m.launchDims()
	if p := m.garden.launch.preview; m.launchConfirmable() && (p.Preview.Request.Cols != cols || p.Preview.Request.Rows != rows) {
		t.Fatalf("confirmed %dx%d under an area of %dx%d", p.Preview.Request.Cols, p.Preview.Request.Rows, cols, rows)
	}
	if !m.launchConfirmable() {
		t.Fatal("the refreshed preview for the current area was not accepted")
	}
}

func TestLaunchSessionChangeReconcilesAttempts(t *testing.T) {
	m, _, _, _ := fakeLaunchUIHistory(t, 120, 35, false)
	seq := m.garden.launch.readSeq
	m, cmd := send(t, m, agentsChangedMsg{ch: make(chan struct{})})
	if m.garden.launch.readSeq == seq || cmd == nil {
		t.Fatal("a Sessions.Changes signal did not request one attempt reconciliation")
	}
}

func TestLaunchPaletteBoardReleasesTerminal(t *testing.T) {
	m, rig, fb, _ := fakeLaunchUIHistory(t, 120, 35, true)
	m, stream := fakeLaunchUIOpenTerminal(t, m, rig)
	m, cmd := m.goToPalette(paletteTarget{ws: wsGarden, gardenAction: "board"})
	m = plannerRun(t, m, cmd)
	if !stream.Closed() || fb.stops != 0 {
		t.Fatalf("palette navigation leaked the attachment or stopped the holder: closed=%v stops=%d", stream.Closed(), fb.stops)
	}
}

func TestLaunchQuitReleasesTerminalWithoutStopping(t *testing.T) {
	for _, k := range []string{"q", "ctrl+c"} {
		m, rig, fb, _ := fakeLaunchUIHistory(t, 120, 35, true)
		m, stream := fakeLaunchUIOpenTerminal(t, m, rig)
		m = plannerPress(t, m, "ctrl+]")
		var cmd tea.Cmd
		if k == "q" {
			m, cmd = send(t, m, keyFor("q"))
		} else {
			m, cmd = send(t, m, tea.KeyMsg{Type: tea.KeyCtrlC})
		}
		// tea.Sequence hides its commands; run them the way the runtime would.
		seq := reflect.ValueOf(cmd())
		if seq.Kind() != reflect.Slice {
			t.Fatalf("%s: quit returned %T, want a sequence", k, seq.Interface())
		}
		for i := 0; i < seq.Len(); i++ {
			if c, ok := seq.Index(i).Interface().(tea.Cmd); ok && c != nil {
				c()
			}
		}
		if !stream.Closed() || fb.stops != 0 {
			t.Fatalf("%s: closed=%v stops=%d", k, stream.Closed(), fb.stops)
		}
	}
}

func TestLaunchCodexFormHasDistinctSandboxAndApproval(t *testing.T) {
	preset := agents.LaunchField{ID: "permissions", Kind: agents.LaunchFieldSelect, Options: []agents.FieldOption{
		{Value: "auto", Set: map[string]string{"sandbox": "workspace-write", "approval": "on-request"}}}}
	var sandbox, approval, perms bool
	for _, f := range codexNativeFields([]agents.LaunchField{preset, {ID: "effort"}}) {
		sandbox, approval, perms = sandbox || f.ID == "sandbox", approval || f.ID == "approval", perms || f.ID == "permissions"
	}
	if !sandbox || !approval || perms {
		t.Fatalf("sandbox=%v approval=%v preset kept=%v", sandbox, approval, perms)
	}
}
