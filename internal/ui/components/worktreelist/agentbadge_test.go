package worktreelist

import (
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/core/git"
	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
)

func TestAgentBadgePlacement(t *testing.T) {
	const path = "/w/feat"
	one := theme.AgentBadge("claude", 1, false)
	two := one + " " + theme.AgentBadge("codex", 1, false)
	counted := theme.AgentBadge("claude", 2, false)
	failed := one + " !"
	ascii := theme.AgentBadge("claude", 1, true) + " " + theme.AgentBadge("codex", 1, true)

	tests := []struct {
		name   string
		badges string
		want   string // expected plain text on the badge, "" = none
	}{
		{"no agents", "", ""},
		{"one provider", one, "✹"},
		{"two providers", two, "✹ ❃"},
		{"count two", counted, "✹2"},
		{"failed marker", failed, "✹ !"},
		{"ascii", ascii, "C X"},
	}
	for _, tc := range tests {
		for _, width := range []int{30, 40, 80} {
			t.Run(tc.name+"/"+strconv.Itoa(width), func(t *testing.T) {
				m := New()
				m.SetSize(width, 10)
				m.SetItems([]Item{{
					WT:      git.Worktree{Path: path, Branch: "feature/a-rather-long-branch-name"},
					Running: 1,
				}})
				m.SetAgentBadges(func(p string) string {
					if p == path {
						return tc.badges
					}
					return ""
				})
				for _, line := range strings.Split(m.View(), "\n") {
					if w := ansi.StringWidth(line); w > width {
						t.Errorf("line width %d > %d: %q", w, width, line)
					}
				}
				row := strings.Split(ansi.Strip(m.View()), "\n")
				var first string
				for _, l := range row {
					if strings.Contains(l, "feature/") {
						first = strings.TrimRight(l, " ")
					}
				}
				if first == "" {
					t.Fatalf("branch row missing:\n%s", m.View())
				}
				if tc.want == "" {
					if strings.ContainsAny(first, "✹❃!") {
						t.Errorf("unexpected badge: %q", first)
					}
					return
				}
				if !strings.HasSuffix(first, tc.want) {
					t.Errorf("row %q should end with badge %q", first, tc.want)
				}
			})
		}
	}
}

func TestAgentBadgeFollowsOtherBadgesWhenRoomy(t *testing.T) {
	m := New()
	m.SetSize(80, 10)
	m.SetItems([]Item{{WT: git.Worktree{Path: "/w/x", Branch: "x"}, Running: 1}})
	m.SetAgentBadges(func(string) string { return "✹2" })
	for _, l := range strings.Split(ansi.Strip(m.View()), "\n") {
		if strings.Contains(l, "x") && strings.Contains(l, "✹") {
			if got := strings.TrimSpace(l); !strings.HasSuffix(got, "x ▶1 ✹2") {
				t.Errorf("agent count should follow the branch badges with one space: %q", got)
			}
			return
		}
	}
	t.Fatal("badge row not found")
}

func TestAgentBadgeNeverTruncatedAndDroppedWhenTooWide(t *testing.T) {
	m := New()
	m.SetSize(5, 10)
	m.SetItems([]Item{{WT: git.Worktree{Path: "/w/x", Branch: "some-branch"}}})
	m.SetAgentBadges(func(string) string { return "✹ ❃ ✦ !" })
	out := ansi.Strip(m.View())
	if strings.Contains(out, "✹") {
		t.Errorf("badges wider than the pane must be dropped:\n%s", out)
	}
}
