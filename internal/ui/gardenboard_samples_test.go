package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agentactivity"
	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	gardensync "github.com/Tiago-0liveira/bonsai/internal/core/garden/sync"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/work"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
	"github.com/charmbracelet/x/ansi"
)

// gardenBoardSamples exercises real model reads and rendering with the fake API.
// It returns text so tests can keep artifacts inside their own temporary directories.
func gardenBoardSamples(t *testing.T, ascii bool) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, sz := range [][2]int{{80, 24}, {120, 35}, {160, 48}} {
		fb := boardFake(t)
		rev := scriptPlan(t, plannerRig{garden: fb}, longPlan("demo approved pair"))
		fb.AddPlanDecision(1, rev, garden.DecisionApproved, "Approve exact pair")
		var items []gardensync.Item
		titles := []string{"Improve process status colors", "Reduce test setup time", "Keep account sessions when switching", "Switch account after rate limit", "Fix usage refresh after reconnect", "Fix CI flakes in worktrees", "Remember worktree filters", "Recover expired provider access", "Revalidate changed specification"}
		for i, index := range []int{0, 1, 2, 6, 12, 18, 24, 30, 31} {
			it := fb.WorkView.Snapshot.Items[index]
			it.Issue.Title = titles[i]
			if i == 8 {
				it.Verification.SupersededApprovals = it.Verification.ValidApprovals
				it.Verification.ValidApprovals = nil
				it.Verification.AcceptedSpecHash = ""
			}
			items = append(items, it)
		}
		fb.WorkView.Snapshot.Items = items
		now := time.Date(2026, 1, 2, 15, 4, 0, 0, time.Local)
		fb.LocalWorkIssues = map[uint64]gardenapi.IssueLocalWork{
			2:  {Planner: &gardenapi.LocalRunSummary{State: "running", Live: true, Activity: &agentactivity.Activity{State: agentactivity.Working}, Updated: now}},
			13: {Implementation: &gardenapi.LocalRunSummary{State: "running", Live: true, Updated: now}},
			19: {Implementation: &gardenapi.LocalRunSummary{State: "awaiting-human-review", Updated: now}},
			31: {Implementation: &gardenapi.LocalRunSummary{State: "failed", Updated: now}},
		}
		prefs := config.Prefs{}
		if ascii {
			prefs.AgentGlyphs = "ascii"
		}
		m := gardenEnv(t, fb, prefs, sz[0], sz[1])
		m.garden.board.selection.Focus(m.garden.board.collection, 2)
		m.gardenBoardVisible()
		for _, mode := range []work.Mode{work.ModePhases, work.ModeDetailed} {
			if mode == work.ModeDetailed {
				m = pressAll(t, m, "d")
			}
			name := "phases"
			if mode == work.ModeDetailed {
				name = "detailed"
			}
			text := ansi.Strip(m.View())
			for _, line := range strings.Split(text, "\n") {
				if ansi.StringWidth(line) > sz[0] {
					t.Fatal("demo line exceeds terminal width")
				}
			}
			if ascii {
				for _, r := range text {
					if r > 127 {
						t.Fatalf("non ASCII board rune %q", r)
					}
				}
			}
			out[fmt.Sprintf("%s-%dx%d.txt", name, sz[0], sz[1])] = text + "\n"
		}
	}
	return out
}
func TestGardenBoardRenderSamples(t *testing.T) {
	for _, ascii := range []bool{false, true} {
		for name, text := range gardenBoardSamples(t, ascii) {
			if strings.TrimSpace(text) == "" || !strings.Contains(text, "View:") {
				t.Errorf("%s (ascii=%v): empty or missing View: header", name, ascii)
			}
		}
	}
}
