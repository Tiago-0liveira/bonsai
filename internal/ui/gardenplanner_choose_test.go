package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestPlannerAccountChoiceLayouts(t *testing.T) {
	for _, tc := range []struct {
		w, h int
		wide bool
	}{{120, 35, true}, {165, 45, true}, {80, 24, false}} {
		m, _ := plannerEnv(t, tc.w, tc.h)
		m = requestPlan(t, openPlanner(t, m))
		rows := m.gardenBoardRows()
		lines := m.gsPlannerChoose(tc.w, rows)
		if len(lines) != rows {
			t.Fatalf("%dx%d: %d lines, want %d", tc.w, tc.h, len(lines), rows)
		}
		var plain []string
		for _, l := range lines {
			if w := ansi.StringWidth(l); w != tc.w {
				t.Errorf("%dx%d: line is %d wide: %q", tc.w, tc.h, w, ansi.Strip(l))
			}
			plain = append(plain, strings.TrimRight(ansi.Strip(l), " "))
		}
		out := strings.Join(plain, "\n")
		t.Logf("%dx%d\n%s", tc.w, tc.h, out)
		want := []string{"Name", "Model", "Effort", "Accept read-only and request plan", "Read-only", "tool allowlist"}
		if tc.wide {
			want = append(want, "#101 ", "READ-ONLY / LOCAL MACHINE", "No worktree is created")
			if strings.Contains(out, "Session options") {
				t.Errorf("%dx%d keeps the options divider", tc.w, tc.h)
			}
		}
		for _, s := range want {
			if !strings.Contains(out, s) {
				t.Errorf("%dx%d lacks %q", tc.w, tc.h, s)
			}
		}
		if title := boardTitle(m.garden.planner.preview.Title); tc.wide && !strings.Contains(out, strings.ToUpper(title[:24])) {
			t.Errorf("%dx%d: issue title is not upper-cased", tc.w, tc.h)
		}
	}
}

func TestPlannerAccountChoiceKeepsButtonsInAShortPane(t *testing.T) {
	m, _ := plannerEnv(t, 120, 35)
	m = requestPlan(t, openPlanner(t, m))
	for rows := 6; rows <= 20; rows++ {
		lines := m.gsPlannerChoose(120, rows)
		out := ansi.Strip(strings.Join(lines, "\n"))
		if len(lines) != rows || !strings.Contains(out, "Accept read-only and request plan") {
			t.Errorf("%d rows: %d lines, buttons present %v\n%s", rows, len(lines), strings.Contains(out, "Accept read-only"), out)
		}
	}
}

func TestPlannerAccountChoiceShowsWhyAnAccountCannotPlan(t *testing.T) {
	m, _ := plannerEnv(t, 120, 35)
	m = requestPlan(t, openPlanner(t, m))
	for i := 0; i < 3; i++ {
		if cur, _ := m.garden.planner.dlg.Current(); cur.Provider == "antigravity" {
			break
		}
		m = plannerPress(t, m, "down")
	}
	out := ansi.Strip(strings.Join(m.gsPlannerChoose(120, m.gardenBoardRows()), "\n"))
	for _, s := range []string{"Antigravity cannot plan: agy has no tool allowlist", "Cannot plan"} {
		if !strings.Contains(out, s) {
			t.Errorf("lacks %q\n%s", s, out)
		}
	}
}
