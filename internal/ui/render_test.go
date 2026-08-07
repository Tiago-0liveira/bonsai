package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/help"

	"github.com/Tiago-0liveira/bonsai/internal/core/gh"
	"github.com/Tiago-0liveira/bonsai/internal/core/git"
)

func renderModel() Model {
	m := testModel()
	m.width, m.height = 100, 40
	m.help = help.New()
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
