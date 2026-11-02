package worktreelist

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/Tiago-0liveira/bonsai/internal/core/gh"
	"github.com/Tiago-0liveira/bonsai/internal/core/git"
	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
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
			name:    "open PR shows state word",
			item:    Item{WT: git.Worktree{Branch: "feat-x"}, PR: 12, PRState: gh.StateOpen},
			contain: []string{"#12", "open"},
		},
		{
			name:    "draft PR shows draft",
			item:    Item{WT: git.Worktree{Branch: "feat-x"}, PR: 12, PRState: gh.StateOpen, PRDraft: true},
			contain: []string{"#12", "draft"},
		},
		{
			name:    "approved PR shows state word",
			item:    Item{WT: git.Worktree{Branch: "feat-x"}, PR: 12, PRState: gh.StateOpen, PRReview: gh.ReviewApproved},
			contain: []string{"#12", "approved"},
		},
		{
			name:    "changes-requested PR shows changes",
			item:    Item{WT: git.Worktree{Branch: "feat-x"}, PR: 12, PRState: gh.StateOpen, PRReview: gh.ReviewChangesRequested},
			contain: []string{"#12", "changes"},
		},
		{
			name:    "draft wins over approval",
			item:    Item{WT: git.Worktree{Branch: "feat-x"}, PR: 12, PRState: gh.StateOpen, PRDraft: true, PRReview: gh.ReviewApproved},
			contain: []string{"draft"},
		},
		{
			name:    "merged PR shows merged",
			item:    Item{WT: git.Worktree{Branch: "feat-x"}, PR: 12, PRState: gh.StateMerged},
			contain: []string{"#12", "merged"},
		},
		{
			name:    "closed PR shows closed",
			item:    Item{WT: git.Worktree{Branch: "feat-x"}, PR: 12, PRState: gh.StateClosed},
			contain: []string{"#12", "closed"},
		},
		{
			name:     "compact mode keeps state readable",
			mode:     "compact",
			item:     Item{WT: git.Worktree{Branch: "feat-x"}, PR: 12, PRState: gh.StateMerged},
			contain:  []string{"#12", "merged"},
			notThere: []string{"●", "◐"},
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

func TestRowColumnsAndHomePaths(t *testing.T) {
	m := New()
	m.SetSize(100, 20)
	m.SetPathHome("/home/test")
	m.SetItems([]Item{{WT: git.Worktree{Path: "/home/test/one", Branch: "short"}, PR: 1, PRState: gh.StateOpen, Checks: "pass"}, {WT: git.Worktree{Path: "/home/test/two", Branch: "a-longer-branch"}, PR: 2, PRState: gh.StateOpen, Checks: "fail"}})
	out := m.View()
	if !strings.Contains(out, "/home/test/one") || !strings.Contains(out, "~/two") {
		t.Fatal(out)
	}
	columns := []int{}
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "#") {
			columns = append(columns, ansi.StringWidth(line[:strings.Index(line, "#")]))
		}
	}
	if len(columns) != 2 || columns[0] != columns[1] {
		t.Fatalf("PR columns %v\n%s", columns, out)
	}
	for _, tc := range []struct{ state, want string }{{"pass", "+"}, {"fail", "x"}, {"pending", "~"}} {
		if checkGlyph(tc.state, true) != tc.want {
			t.Fatal(tc.state)
		}
	}
}

func renderRow(items []Item, index, width int, ascii bool) string {
	m := New()
	m.SetSize(width, 20)
	m.SetItems(items)
	var out strings.Builder
	itemDelegate{ascii: ascii}.Render(&out, m.list, index, items[index])
	return strings.Split(out.String(), "\n")[0]
}

func TestPRBlockPlacementAndAlignment(t *testing.T) {
	for _, tc := range []struct {
		mode, block string
	}{
		{"full", "#114 merged ✓"},
		{"compact", "#114 merged ✓"},
		{"color", "#114 ✓"},
		{"off", "#114"},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			SetPRStatusMode(tc.mode)
			t.Cleanup(func() { SetPRStatusMode("full") })
			items := []Item{
				{WT: git.Worktree{Branch: "first"}, PR: 1, PRState: gh.StateOpen},
				{WT: git.Worktree{Branch: "second"}},
				{WT: git.Worktree{Branch: "third"}, PR: 114, PRState: gh.StateMerged, Checks: "pass"},
			}
			wantColumn := 2
			if tc.mode != "off" {
				wantColumn += ansi.StringWidth(tc.block) + 1
			}
			for i, item := range items {
				row := ansi.Strip(renderRow(items, i, 80, false))
				branchIndex := strings.Index(row, item.Title())
				if branchIndex < 0 {
					t.Fatalf("missing branch %q: %q", item.Title(), row)
				}
				if got := ansi.StringWidth(row[:branchIndex]); got != wantColumn {
					t.Errorf("branch column = %d, want %d: %q", got, wantColumn, row)
				}
				if item.PR > 0 {
					pr, branch := strings.Index(row, "#"), strings.Index(row, item.Title())
					if (pr < branch) != (tc.mode != "off") {
						t.Errorf("wrong PR placement: %q", row)
					}
				}
			}
			if row := ansi.Strip(renderRow(items, 2, 80, false)); !strings.Contains(row, tc.block) {
				t.Errorf("missing block %q: %q", tc.block, row)
			}
			noPR := []Item{{WT: git.Worktree{Branch: "plain"}}}
			if row := ansi.Strip(renderRow(noPR, 0, 80, false)); !strings.HasPrefix(row, "› plain") {
				t.Errorf("unexpected PR padding without PRs: %q", row)
			}
		})
	}
}

func TestColorModeBranchStyles(t *testing.T) {
	profile := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(profile); SetPRStatusMode("full") })
	SetPRStatusMode("color")
	for _, tc := range []struct {
		name, state, review string
		draft               bool
		pr                  int
		color               lipgloss.Color
	}{
		{"open", gh.StateOpen, "", false, 12, theme.Current.Success},
		{"draft", gh.StateOpen, "", true, 12, theme.Current.Dim},
		{"merged", gh.StateMerged, "", false, 12, theme.Current.PhaseReview},
		{"closed", gh.StateClosed, "", false, 12, theme.Current.Danger},
		{"changes", gh.StateOpen, gh.ReviewChangesRequested, false, 12, theme.Current.Success},
		{"no PR", "", "", false, 0, theme.Current.Text},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, selected := range []bool{false, true} {
				items := []Item{{WT: git.Worktree{Branch: "placeholder"}}, {
					WT: git.Worktree{Branch: "feature"}, PR: tc.pr, PRState: tc.state, PRDraft: tc.draft, PRReview: tc.review, Checks: "pass",
				}}
				index := 1
				if selected {
					items[0], items[1] = items[1], items[0]
					index = 0
				}
				row := renderRow(items, index, 60, false)
				color := tc.color
				if tc.pr == 0 && selected {
					color = theme.Current.Accent
				}
				want := lipgloss.NewStyle().Foreground(color).Bold(selected).Render("feature")
				if !strings.Contains(row, want) {
					t.Errorf("missing branch style %q in %q", want, row)
				}
				plain := ansi.Strip(row)
				if tc.pr > 0 && !strings.Contains(plain, "#12 ✓ feature") {
					t.Errorf("color block: %q", plain)
				}
				for _, word := range []string{"open", "draft", "merged", "closed", "changes"} {
					if strings.Contains(plain, word) {
						t.Errorf("unexpected state word %q: %q", word, plain)
					}
				}
				if selected && !strings.HasPrefix(plain, "› ") {
					t.Errorf("missing cursor: %q", plain)
				}
			}
		})
	}
}

func TestPRBlockNarrowAndASCII(t *testing.T) {
	for _, mode := range []string{"full", "compact", "color"} {
		for _, ascii := range []bool{false, true} {
			for _, check := range []struct{ state, unicode, ascii string }{{"pass", "✓", "+"}, {"fail", "✗", "x"}, {"pending", "◌", "~"}} {
				t.Run(fmt.Sprintf("%s/ascii=%t/%s", mode, ascii, check.state), func(t *testing.T) {
					SetPRStatusMode(mode)
					t.Cleanup(func() { SetPRStatusMode("full") })
					items := []Item{{WT: git.Worktree{Branch: "feature/a-very-long-branch-name"}, PR: 114, PRState: gh.StateOpen, Checks: check.state}}
					glyph, cursor := check.unicode, "› "
					if ascii {
						glyph, cursor = check.ascii, "> "
					}
					block := "#114 "
					if mode != "color" {
						block += "open "
					}
					block += glyph
					for _, width := range []int{ansi.StringWidth(block) + 4, 22, 30} {
						row := ansi.Strip(renderRow(items, 0, width, ascii))
						if !strings.HasPrefix(row, cursor+block+" ") || !strings.HasSuffix(row, "…") || ansi.StringWidth(row) > width {
							t.Errorf("width %d lost PR block or truncation: %q", width, row)
						}
					}
				})
			}
		}
	}
}
