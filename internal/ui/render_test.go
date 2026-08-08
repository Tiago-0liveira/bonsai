package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/gh"
	"github.com/Tiago-0liveira/bonsai/internal/core/git"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/worktreelist"
)

func renderModel() Model {
	m := testModel()
	m.width, m.height = 100, 40
	m.keys = newKeyMap(nil)
	m.diffFileContent = map[string]string{}
	return m
}

func TestRenderPRDetail(t *testing.T) {
	m := renderModel()
	m.prExpandDesc = true
	m.prExpandCommits = true
	d := gh.PRDetail{
		Number: 12, Title: "Add login", State: "OPEN", IsDraft: true,
		Author: "alice", Base: "main", Head: "feat", Mergeable: "CONFLICTING",
		Labels: []string{"enh"}, Reviewers: []string{"carol"},
		Body: "line one\nline two", Additions: 40, Deletions: 3, ChangedFiles: 5,
		Commits:  []gh.Commit{{OID: "abcdef1234", Headline: "do it", Author: "alice"}},
		Reviews:  []gh.Review{{Author: "carol", State: "APPROVED", Body: "lgtm", SubmittedAt: "2026-01-03T00:00:00Z"}},
		Comments: []gh.TimelineItem{{Author: "dan", Body: "nit", CreatedAt: "2026-01-02T00:00:00Z"}},
	}
	out := m.renderPRDetail(d, []gh.Check{{Name: "build", Bucket: "pass"}})
	for _, want := range []string{"#12", "Add login", "OPEN", "conflicting", "Commits", "abcdef1", "Activity", "carol", "build"} {
		if !strings.Contains(out, want) {
			t.Errorf("renderPRDetail missing %q\n%s", want, out)
		}
	}
}

func TestRenderPRDetailCollapsedDescription(t *testing.T) {
	m := renderModel()
	m.prExpandDesc = false
	d := gh.PRDetail{Number: 1, Title: "t", State: "OPEN", Body: "secret detail\nmore"}
	out := m.renderPRDetail(d, nil)
	if strings.Contains(out, "more") {
		t.Errorf("collapsed description should hide later lines:\n%s", out)
	}
	if !strings.Contains(out, "expand") {
		t.Errorf("collapsed description should show expand hint:\n%s", out)
	}
}

func TestRenderDiff(t *testing.T) {
	m := renderModel()
	m.diffBase = "origin/main"
	m.diffFiles = []git.DiffFile{
		{Path: "a.go", Add: 10, Del: 2},
		{Path: "b.bin", Add: -1, Del: -1},
	}
	m.diffCursor = 0
	out := m.renderDiff()
	for _, want := range []string{"2 files changed", "a.go", "+10", "binary"} {
		if !strings.Contains(out, want) {
			t.Errorf("renderDiff missing %q\n%s", want, out)
		}
	}
}

func TestRenderDiffEmpty(t *testing.T) {
	m := renderModel()
	m.diffBase = "origin/main"
	out := m.renderDiff()
	if !strings.Contains(out, "no changes") {
		t.Errorf("empty diff should say no changes: %q", out)
	}
}

func TestRenderInspect(t *testing.T) {
	m := renderModel()
	m.list = worktreelist.New()
	m.list.SetItems([]worktreelist.Item{{WT: git.Worktree{Path: "/w/feat", Branch: "feat"}}})
	m.inspectPath = "/w/feat"
	m.inspect = inspectorData{
		status:   git.StatusSummary{Modified: 2, Untracked: 1},
		commit:   git.HeadCommit{Subject: "fix: thing", Author: "ana", When: time.Now().Add(-2 * time.Hour)},
		commitOK: true,
		diskKB:   42 * 1024,
		diskOK:   true,
		stashes:  2,
		base:     "origin/main",
		files:    []git.DiffFile{{Path: "a.go", Add: 3, Del: 1}},
	}
	out := m.renderInspect()
	for _, want := range []string{"feat", "2 modified", "1 untracked", "fix: thing", "ana", "2h ago", "42.0 MB", "2 stash", "+3", "main"} {
		if !strings.Contains(out, want) {
			t.Errorf("renderInspect missing %q\n%s", want, out)
		}
	}
}

func TestRenderChecks(t *testing.T) {
	m := renderModel()
	m.list = worktreelist.New()
	m.list.SetItems([]worktreelist.Item{{WT: git.Worktree{Path: "/w/feat", Branch: "feat"}}})
	m.prByBranch = map[string]gh.PR{}
	m.ciPath = "/w/feat"
	m.ciRuns = []gh.Run{
		{Name: "fix: thing", Workflow: "ci", Event: "push", Status: "completed", Conclusion: "success", CreatedAt: time.Now().Add(-time.Hour).Format(time.RFC3339)},
		{Name: "wip", Workflow: "ci", Event: "pull_request", Status: "in_progress"},
	}
	out := m.renderChecks()
	for _, want := range []string{"Runs · feat", "fix: thing", "wip", "1h ago"} {
		if !strings.Contains(out, want) {
			t.Errorf("renderChecks missing %q\n%s", want, out)
		}
	}

	m.ciRuns = nil
	m.ciErr = "boom"
	if out := m.renderChecks(); !strings.Contains(out, "gh unavailable") {
		t.Errorf("error state missing:\n%s", out)
	}
	m.ciErr = ""
	if out := m.renderChecks(); !strings.Contains(out, "no workflow runs") {
		t.Errorf("empty state missing:\n%s", out)
	}
}

func TestRenderInspectCleanNoCommit(t *testing.T) {
	m := renderModel()
	m.list = worktreelist.New()
	m.list.SetItems([]worktreelist.Item{{WT: git.Worktree{Path: "/w/main", Branch: "main", IsMain: true}}})
	m.inspectPath = "/w/main"
	m.inspect = inspectorData{}
	out := m.renderInspect()
	for _, want := range []string{"clean", "no commits yet"} {
		if !strings.Contains(out, want) {
			t.Errorf("renderInspect missing %q\n%s", want, out)
		}
	}
	if strings.Contains(out, "Diff vs") {
		t.Errorf("main worktree should not show a diff section:\n%s", out)
	}
}
