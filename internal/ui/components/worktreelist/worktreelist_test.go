package worktreelist

import (
	tea "github.com/charmbracelet/bubbletea"
	"strings"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/gh"
	"github.com/Tiago-0liveira/bonsai/internal/core/git"
)

func TestPrBadge(t *testing.T) {
	tests := []struct {
		name     string
		mode     string
		item     Item
		contain  []string
		notThere []string
	}{
		{
			name:    "open PR shows half circle and label",
			item:    Item{WT: git.Worktree{Branch: "feat-x"}, PR: 12, PRState: gh.StateOpen},
			contain: []string{"#12", "◐", "open"},
		},
		{
			name:    "draft PR shows draft",
			item:    Item{WT: git.Worktree{Branch: "feat-x"}, PR: 12, PRState: gh.StateOpen, PRDraft: true},
			contain: []string{"#12", "◐", "draft"},
		},
		{
			name:    "approved PR shows full circle",
			item:    Item{WT: git.Worktree{Branch: "feat-x"}, PR: 12, PRState: gh.StateOpen, PRReview: gh.ReviewApproved},
			contain: []string{"#12", "●", "approved"},
		},
		{
			name:    "changes-requested PR shows changes",
			item:    Item{WT: git.Worktree{Branch: "feat-x"}, PR: 12, PRState: gh.StateOpen, PRReview: gh.ReviewChangesRequested},
			contain: []string{"#12", "◐", "changes"},
		},
		{
			name:    "draft wins over approval",
			item:    Item{WT: git.Worktree{Branch: "feat-x"}, PR: 12, PRState: gh.StateOpen, PRDraft: true, PRReview: gh.ReviewApproved},
			contain: []string{"draft"},
		},
		{
			name:    "merged PR shows merged",
			item:    Item{WT: git.Worktree{Branch: "feat-x"}, PR: 12, PRState: gh.StateMerged},
			contain: []string{"#12", "●", "merged"},
		},
		{
			name:    "closed PR shows closed",
			item:    Item{WT: git.Worktree{Branch: "feat-x"}, PR: 12, PRState: gh.StateClosed},
			contain: []string{"#12", "●", "closed"},
		},
		{
			name:     "compact mode shows glyph without label",
			mode:     "compact",
			item:     Item{WT: git.Worktree{Branch: "feat-x"}, PR: 12, PRState: gh.StateMerged},
			contain:  []string{"#12", "●"},
			notThere: []string{"merged"},
		},
		{
			name:     "off mode shows number only",
			mode:     "off",
			item:     Item{WT: git.Worktree{Branch: "feat-x"}, PR: 12, PRState: gh.StateMerged},
			contain:  []string{"#12"},
			notThere: []string{"merged", "●", "◐"},
		},
		{
			name:     "pr-N branch name without gh data shows number",
			item:     Item{WT: git.Worktree{Branch: "pr-7"}},
			contain:  []string{"#7"},
			notThere: []string{"merged", "●", "◐"},
		},
		{
			name: "no PR shows nothing",
			item: Item{WT: git.Worktree{Branch: "main"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			SetPRStatusMode(tt.mode)
			t.Cleanup(func() { SetPRStatusMode("full") })

			got := tt.item.prBadge()
			if len(tt.contain) == 0 {
				if got != "" {
					t.Fatalf("prBadge() = %q, want empty", got)
				}
				return
			}
			for _, c := range tt.contain {
				if !strings.Contains(got, c) {
					t.Errorf("prBadge() = %q, want it to contain %q", got, c)
				}
			}
			for _, c := range tt.notThere {
				if strings.Contains(got, c) {
					t.Errorf("prBadge() = %q, should not contain %q", got, c)
				}
			}
		})
	}
}

func TestSetPRStatusMode(t *testing.T) {
	for _, mode := range PRStatusModes {
		SetPRStatusMode(mode)
		if prStatusMode != mode {
			t.Errorf("SetPRStatusMode(%q) = %q", mode, prStatusMode)
		}
	}
	SetPRStatusMode("bogus")
	if prStatusMode != "full" {
		t.Errorf("unknown mode should fall back to full, got %q", prStatusMode)
	}
	SetPRStatusMode("full")
}

func TestRefreshPreservesFilterAndSelectedPath(t *testing.T) {
	m := New()
	m.SetSize(80, 20)
	items := []Item{{WT: git.Worktree{Path: "/main", Branch: "main"}}, {WT: git.Worktree{Path: "/feature-one", Branch: "feature-one"}}, {WT: git.Worktree{Path: "/feature-two", Branch: "feature-two"}}}
	m.SetItems(items)
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	var cmd tea.Cmd
	m, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("feature")})
	if cmd != nil {
		m, _ = m.Update(cmd())
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.SelectByPath("/feature-two") {
		t.Fatal("visible filtered item not found")
	}
	if selected, ok := m.Selected(); !ok || selected.WT.Path != "/feature-two" {
		t.Fatal("filtered index used underlying-list position")
	}
	items[2].Status.Modified = 3
	m.SetItems([]Item{items[2], items[0], items[1]})
	if selected, ok := m.Selected(); !ok || selected.WT.Path != "/feature-two" || selected.Status.Modified != 3 {
		t.Fatalf("filtered refresh lost selected identity/data: %+v", selected)
	}
	if m.SelectByPath("/main") {
		t.Fatal("hidden item escaped the active filter")
	}
}
