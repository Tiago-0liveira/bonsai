package ui

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agentactivity"
	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/work"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
	"github.com/charmbracelet/x/ansi"
)

func TestGardenStagesToggleRemembersEachMode(t *testing.T) {
	m := gardenEnv(t, boardFake(t), config.Prefs{}, 80, 24)
	if m.garden.board.selection.Mode != work.ModePhases {
		t.Fatal("default is not phases")
	}
	m = pressAll(t, m, "down", "down", "down")
	m.garden.board.selection.Scroll[work.PlanningPhase] = 2
	phase := m.garden.board.selection.Clone()
	m = pressAll(t, m, "d")
	if m.garden.board.selection.ID != phase.ID || m.garden.board.selection.Mode != work.ModeDetailed {
		t.Fatal("toggle lost identity")
	}
	m = pressAll(t, m, "down", "down", "down")
	m.garden.board.selection.Scroll[work.PlanReview] = 4
	detailed := m.garden.board.selection.Clone()
	m = pressAll(t, m, "d")
	if m.garden.board.selection.ID != detailed.ID || m.garden.board.selection.Scroll[work.PlanningPhase] != phase.Scroll[work.PlanningPhase] {
		t.Fatal("phase scroll lost")
	}
	m = pressAll(t, m, "d")
	if !reflect.DeepEqual(m.garden.board.selection, detailed) {
		t.Fatalf("detailed memory lost: %+v / %+v", m.garden.board.selection, detailed)
	}
	m = pressAll(t, m, "v")
	before := m.garden.board.selection.Clone()
	m = pressAll(t, m, "d")
	if !reflect.DeepEqual(before, m.garden.board.selection) {
		t.Fatal("table consumed stages toggle")
	}
}

func TestGardenPhaseSelectionVisibleEverySizeAndMode(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {120, 35}, {160, 48}} {
		for _, mode := range []work.Mode{work.ModePhases, work.ModeDetailed} {
			t.Run(fmt.Sprintf("%v/%d", size, mode), func(t *testing.T) {
				m := gardenEnv(t, boardFake(t), config.Prefs{}, size[0], size[1])
				if mode == work.ModeDetailed {
					m = pressAll(t, m, "d")
				}
				lanes := m.garden.board.collection.Lanes(mode)
				for lane, records := range lanes {
					for row, r := range records {
						m.garden.board.selection.Focus(m.garden.board.collection, r.Item.Issue.ID)
						m.gardenBoardVisible()
						s := m.garden.board.selection
						n := min(gardenBoardColumns(size[0], mode), len(lanes))
						capacity := gardenBoardCapacity(m.gardenBoardRows())
						if int(s.Column) != lane || lane < s.Window || lane >= s.Window+n || row < s.Scroll[lane] || row >= s.Scroll[lane]+capacity {
							t.Fatalf("invisible %+v", s)
						}
						if m.gardenCrumbs()[2] != m.garden.board.collection.LaneTitles(mode)[lane] {
							t.Fatal("breadcrumb names a different lane")
						}
						out := m.gsBoard(size[0], m.gardenBoardRows())
						for _, line := range out {
							if ansi.StringWidth(line) > size[0] {
								t.Fatal("line overflows")
							}
						}
						if !strings.Contains(ansi.Strip(strings.Join(out, "\n")), fmt.Sprintf("#%d", r.Item.Issue.Number)) {
							t.Fatal("selected card was clipped")
						}
					}
					m = pressAll(t, m, "right")
				}
			})
		}
	}
}

func TestGardenEvidenceRefreshStaleScopeAndDeletion(t *testing.T) {
	m, rig := plannerEnv(t, 80, 24)
	m = pressAll(t, m, "enter")
	opened, focus := m.garden.board.opened, m.shellFocus
	m = withLocal(t, m, rig, &localCases[0].sum)
	r, _, _ := m.garden.board.collection.Find(1)
	if r.Column != work.Planning || m.garden.board.opened != opened || m.shellFocus != focus {
		t.Fatal("local refresh stole context or failed to reselect")
	}
	rig.garden.LocalWorkErr = errors.New("failed")
	m, cmd := m.gardenLoadLocal()
	m = plannerRun(t, m, cmd)
	r, _, _ = m.garden.board.collection.Find(1)
	if !r.Evidence.Stale || r.Evidence.Planner == nil {
		t.Fatal("failed read discarded evidence")
	}
	for _, kind := range []string{"request", "generation", "repo", "clone", "epoch"} {
		t.Run(kind, func(t *testing.T) {
			before := m.garden.board.collection
			stamp := m.garden.local.gate.Last()
			stamp.Generation++
			msg := gardenLocalMsg{seq: m.garden.local.seq, view: gardenapi.LocalWorkView{Stamp: stamp}}
			switch kind {
			case "request":
				msg.seq--
				msg.view.Stamp.Generation -= 2 // an older request carries an older stamp; the gate rejects it
			case "generation":
				msg.view.Stamp.Generation -= 2
			case "repo":
				msg.view.Stamp.GardenID++
			case "clone":
				msg.view.Stamp.LocalRepoID = "other"
			case "epoch":
				msg.view.Stamp.Epoch++
			}
			next, _ := m.onGardenLocal(msg)
			if !reflect.DeepEqual(before, next.garden.board.collection) {
				t.Fatal("late/foreign local reply applied")
			}
		})
	}
	rig.garden.LocalWorkErr = nil
	rig.garden.WorkView.Snapshot.Items = rig.garden.WorkView.Snapshot.Items[1:]
	m = pressAll(t, m, "R")
	if _, _, ok := m.garden.board.collection.Find(1); ok {
		t.Fatal("local evidence resurrected deleted Issue")
	}
	if m.garden.board.opened != 0 {
		t.Fatal("deleted Issue overlay survived")
	}
}

func TestGardenActivityTextAndExactStates(t *testing.T) {
	now := time.Date(2026, 1, 2, 15, 4, 0, 0, time.Local)
	for _, kind := range []string{"planner", "implementation"} {
		for _, tc := range []struct {
			state    string
			activity *agentactivity.Activity
			live     bool
			want     string
		}{
			{state: "running", activity: &agentactivity.Activity{State: agentactivity.Working}, live: true, want: "working"},
			{state: "running", activity: &agentactivity.Activity{State: agentactivity.Waiting}, live: true, want: "needs you"},
			{state: "running", activity: &agentactivity.Activity{State: agentactivity.Idle}, live: true, want: "idle"},
			{state: "stopped", want: "exited"}, {state: "failed", want: "failed"}, {state: "running", live: true, want: "Activity unknown"},
		} {
			t.Run(kind+"/"+tc.want, func(t *testing.T) {
				sum := &gardenapi.LocalRunSummary{State: tc.state, Live: tc.live, Activity: tc.activity, Updated: now}
				local := gardenapi.IssueLocalWork{Planner: sum}
				if kind == "implementation" {
					local = gardenapi.IssueLocalWork{Implementation: sum}
				}
				l := gardenLocalState{issues: map[uint64]gardenapi.IssueLocalWork{1: local}, stale: true}
				r := work.Record{Column: work.HumanReview, Evidence: l.evidence()[1]}
				want := tc.want
				if kind == "implementation" {
					// Exact stored state, never an invented exit.
					switch want {
					case "working":
						want = "running"
					case "exited":
						want = "stopped"
					case "Activity unknown":
						want = "Implementation running"
					}
				}
				text := boardActivity(r, false, true)
				if !strings.Contains(text, want) || !strings.Contains(text, "updated Jan 2 15:04") || !strings.Contains(text, "(stale)") {
					t.Fatal(text)
				}
				run := r.Evidence.Planner
				if kind == "implementation" {
					run = r.Evidence.Implementation
				}
				if run.State != tc.state {
					t.Fatal("stored state changed")
				}
			})
		}
	}
	for c := work.Column(0); c < work.ColumnCount; c++ {
		r := work.Record{Column: c}
		if !strings.Contains(boardState(r), c.String()) {
			t.Fatalf("lost exact state %s", c)
		}
	}
}

func TestGardenVanishedLaneDoesNotPanic(t *testing.T) {
	m := gardenEnv(t, boardFake(t), config.Prefs{}, 120, 35)
	n := len(m.garden.board.collection.Lanes(work.ModePhases))
	m = pressAll(t, m, "v")
	// Table never reconciled the board lane: simulate the Unplaced lane vanishing under the cursor.
	m.garden.board.selection.Column = work.Column(n)
	m.garden.board.query.Text = "no-such-issue-title"
	m.garden.board.reselect()
	_ = m.gardenCrumbs()
	_ = m.gardenBoardIdentity()
	m.garden.board.layout = layoutBoard
	m.garden.board.selection.Column = work.Column(n + 1)
	_ = m.gardenCrumbs()
	_ = m.gardenBoardIdentity()
}

func TestGardenSelectedCardMarker(t *testing.T) {
	prev := theme.Current
	t.Cleanup(func() { theme.Current = prev })
	for _, tc := range []struct {
		name  string
		ascii bool
		mono  bool
	}{{"unicode", false, false}, {"ascii", true, false}, {"mono", false, true}} {
		t.Run(tc.name, func(t *testing.T) {
			m := gardenEnv(t, boardFake(t), config.Prefs{}, 120, 35)
			if tc.ascii {
				m.work.AgentGlyphs = "ascii"
			}
			if tc.mono {
				theme.Current = theme.Resolve("mono", nil)
			}
			r := m.garden.board.collection.Records[0]
			if first := ansi.Strip(m.boardCard(30, r, true)[0]); !strings.HasPrefix(strings.TrimSpace(first), ">") {
				t.Fatalf("selected card has no marker: %q", first)
			}
			if first := ansi.Strip(m.boardCard(30, r, false)[0]); strings.HasPrefix(strings.TrimSpace(first), ">") {
				t.Fatalf("unselected card has marker: %q", first)
			}
		})
	}
}

func TestGardenBoardHeaderKeepsFilterAndView(t *testing.T) {
	m := gardenEnv(t, boardFake(t), config.Prefs{}, 120, 35)
	m = pressAll(t, m, "t")
	head := ansi.Strip(m.gsBoard(120, m.gardenBoardRows())[0])
	if !strings.Contains(head, "Type: ") || strings.Contains(head, "Type: Any") || !strings.Contains(head, "View: Phases") {
		t.Fatalf("header %q", head)
	}
	for _, line := range m.gsBoard(40, m.gardenBoardRows()) {
		if ansi.StringWidth(line) > 40 {
			t.Fatal("narrow header overflows")
		}
	}
}
