package worktreelist

import (
	"strings"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/gh"
	"github.com/Tiago-0liveira/bonsai/internal/core/git"
)

func TestPrBadge(t *testing.T) {
	tests := []struct {
		name     string
		item     Item
		contain  string
		notThere string
	}{
		{
			name:     "open PR shows number only",
			item:     Item{WT: git.Worktree{Branch: "feat-x"}, PR: 12, PRState: gh.StateOpen},
			contain:  "#12",
			notThere: "merged",
		},
		{
			name:    "merged PR shows merged",
			item:    Item{WT: git.Worktree{Branch: "feat-x"}, PR: 12, PRState: gh.StateMerged},
			contain: "#12 merged",
		},
		{
			name:    "closed PR shows closed",
			item:    Item{WT: git.Worktree{Branch: "feat-x"}, PR: 12, PRState: gh.StateClosed},
			contain: "#12 closed",
		},
		{
			name:     "pr-N branch name without gh data shows number",
			item:     Item{WT: git.Worktree{Branch: "pr-7"}},
			contain:  "#7",
			notThere: "merged",
		},
		{
			name: "no PR shows nothing",
			item: Item{WT: git.Worktree{Branch: "main"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.item.prBadge()
			if tt.contain == "" {
				if got != "" {
					t.Fatalf("prBadge() = %q, want empty", got)
				}
				return
			}
			if !strings.Contains(got, tt.contain) {
				t.Errorf("prBadge() = %q, want it to contain %q", got, tt.contain)
			}
			if tt.notThere != "" && strings.Contains(got, tt.notThere) {
				t.Errorf("prBadge() = %q, should not contain %q", got, tt.notThere)
			}
		})
	}
}
