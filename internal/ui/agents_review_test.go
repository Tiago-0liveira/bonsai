package ui

import (
	"strings"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/agentterminal"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
	"github.com/charmbracelet/x/ansi"
)

func TestLaunchAccountPointerUnique(t *testing.T) {
	for _, size := range launchSizes {
		for _, ascii := range []bool{false, true} {
			e := accountsEnv(t)
			e.launchAt(t, size[0], size[1])
			d := e.dialog()
			d.ascii = ascii
			view := ansi.Strip(strings.Join(d.groupedTable(size[0], size[1]), "\n"))
			mark := "▸"
			if ascii {
				mark = ">"
			}
			if got := strings.Count(view, mark); got != 1 {
				t.Fatalf("%v ASCII=%v: got %d account pointers\n%s", size, ascii, got, view)
			}
			if got := strings.Count(view, "suggested"); d.suggested != "" && got != 1 {
				t.Fatalf("got %d suggested tags", got)
			}
		}
	}
}

func TestHeaderLifecycleMark(t *testing.T) {
	for _, tc := range []struct{ state, mark string }{{"failed", "✗"}, {"exited", "✓"}, {"running", "●"}} {
		t.Run(tc.state, func(t *testing.T) {
			key, label := headerStatus(agentsapi.Session{Summary: agentterminal.Summary{State: tc.state}})
			if got := ansi.Strip(stateDot(key, label)); !strings.HasPrefix(got, tc.mark+" ") {
				t.Fatal(got)
			}
			m := Model{}
			if got := asciiRunes.Replace(stateDot(key, label)); strings.Contains(got, tc.mark) {
				t.Fatal(m.chrome(got))
			}
		})
	}
}

func TestEndedSummaryOmitsZeroCounts(t *testing.T) {
	for _, state := range []string{"failed", "exited"} {
		t.Run(state, func(t *testing.T) {
			e := newAgentsEnv(t)
			e.m.agents.sessions = []agentsapi.Session{{Summary: agentterminal.Summary{State: state}}}
			e.m.agents.byTree = map[string][]int{"/tree": {0}}
			got := e.m.endedSessionsSummary("/tree")
			if !strings.HasPrefix(got, "1 "+state+" · ") || strings.Contains(got, "0 ") {
				t.Fatal(got)
			}
		})
	}
}
