package worktreelist

import (
	"strings"
	"testing"

	gh "github.com/Tiago-0liveira/bonsai/internal/git/github/ghcli"
	git "github.com/Tiago-0liveira/bonsai/internal/git/local"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func TestRowAnatomy(t *testing.T) {
	for _, tc := range []struct {
		name            string
		item            Item
		selected, ascii bool
		want1, want2    string
	}{
		{name: "selected root", item: Item{WT: git.Worktree{Branch: "main", IsMain: true}, Base: "main", DefaultBase: "main"}, selected: true, want1: "┃ main", want2: "┃ ↳ main (root)"},
		{name: "open dirty", item: Item{WT: git.Worktree{Branch: "feat"}, PR: 114, PRState: gh.StateOpen, Checks: "fail", Status: git.StatusSummary{Modified: 2, Untracked: 1}}, want1: "feat [#114 ●open ✗]", want2: "±2 ?1"},
		{name: "merged", item: Item{WT: git.Worktree{Branch: "feat"}, PR: 118, PRState: gh.StateMerged}, want1: "[#118 ✓merged]", want2: "clean"},
		{name: "draft", item: Item{WT: git.Worktree{Branch: "feat"}, PR: 111, PRState: gh.StateOpen, PRDraft: true}, want1: "[#111 ○draft]", want2: "clean"},
		{name: "custom base", item: Item{WT: git.Worktree{Branch: "feat"}, Base: "develop", DefaultBase: "main", HasMetrics: true, Metrics: git.Metrics{Ahead: 12}}, want1: "──", want2: "↳ base: develop"},
		{name: "ascii", item: Item{WT: git.Worktree{Branch: "feat"}, Base: "main", DefaultBase: "main"}, selected: true, ascii: true, want1: "| feat", want2: "| -> main"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := itemDelegate{ascii: tc.ascii}
			first, second := d.renderRow(tc.item, 80, tc.selected)
			if !strings.Contains(ansi.Strip(first), tc.want1) || !strings.Contains(ansi.Strip(second), tc.want2) {
				t.Fatalf("%s\n%s", first, second)
			}
			for width := 0; width <= 120; width++ {
				item := tc.item
				item.WT.Branch = "feature/世界/very-long-branch-name"
				item.Running = 1
				a, b := d.renderRow(item, width, tc.selected)
				if lipgloss.Width(a) > width || lipgloss.Width(b) > width {
					t.Fatalf("overflow at %d: %q / %q", width, a, b)
				}
			}
		})
	}
}

func TestRowPreservesActivity(t *testing.T) {
	d := itemDelegate{badges: func(string) string { return "✹2 ❃1" }}
	a, b := d.renderRow(Item{WT: git.Worktree{Branch: strings.Repeat("branch", 20)}, Running: 1, HasMetrics: true, Metrics: git.Metrics{Ahead: 4, Behind: 15}}, 30, true)
	if !strings.Contains(a, "…") || !strings.HasSuffix(a, "▶1 ✹2 ❃1") || !strings.HasSuffix(b, "↑4 ↓15") {
		t.Fatalf("%s\n%s", a, b)
	}
}
