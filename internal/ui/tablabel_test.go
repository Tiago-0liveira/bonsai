package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestLegacyTabLabelsUseLiveKeys(t *testing.T) {
	tests := []struct {
		tab          rightTab
		action, name string
	}{
		{tabLog, "log_tab", "Git Log"}, {tabProcs, "processes", "Processes"},
		{tabDiff, "diff_tab", "Diff"}, {tabPR, "pr_tab", "PR"},
		{tabInspect, "inspect_tab", "Inspect"}, {tabChecks, "checks_tab", "Checks"},
		{tabAgents, "agents_tab", "Agents"},
	}
	for _, tt := range tests {
		t.Run(tt.action, func(t *testing.T) {
			m := Model{keys: newKeyMap(map[string]string{tt.action: "ctrl+g"})}
			if got := m.tabLabel(tt.tab, false); !strings.Contains(got, tt.name+" (ctrl+g)") {
				t.Fatal(got)
			}
			if got := m.tabLabel(tt.tab, true); !strings.Contains(got, "(ctrl+g)") {
				t.Fatal(got)
			}
		})
	}
	m := Model{keys: newKeyMap(map[string]string{"agents_tab": "ctrl+shift+alt+g"})}
	if got := m.tabLabel(tabAgents, true); lipgloss.Width(got) > 21 {
		t.Fatal(got)
	}
}
