package ui

import (
	"fmt"
	git "github.com/Tiago-0liveira/bonsai/internal/git/local"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/worktreelist"
	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
	"strings"
	"testing"
)

func TestDiffTree(t *testing.T) {
	tests := []struct {
		name      string
		files     []git.DiffFile
		collapsed map[string]bool
		paths     string
	}{
		{"root first and sorted", []git.DiffFile{{Path: "z/b"}, {Path: "b"}, {Path: "a"}, {Path: "z/a"}}, nil, "a,b,z/,z/a,z/b"},
		{"compress chain", []git.DiffFile{{Path: "docs/design/garden/a"}}, nil, "docs/design/garden/,docs/design/garden/a"},
		{"binary default", []git.DiffFile{{Path: "png/a", Add: -1}, {Path: "png/b", Add: -1}}, nil, "png/"},
		{"binary expandable", []git.DiffFile{{Path: "png/a", Add: -1}}, map[string]bool{"png/": false}, "png/,png/a"},
		{"explicit collapse", []git.DiffFile{{Path: "src/a"}}, map[string]bool{"src/": true}, "src/"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rows := buildDiffRows(tt.files, tt.collapsed)
			paths := []string{}
			for _, r := range rows {
				paths = append(paths, r.path)
			}
			if got := strings.Join(paths, ","); got != tt.paths {
				t.Fatalf("got %s want %s", got, tt.paths)
			}
		})
	}
}
func TestDiffRowWidth(t *testing.T) {
	for _, ascii := range []bool{false, true} {
		for _, width := range []int{1, 8, 24, 40, 60, 90, 120} {
			for _, file := range []git.DiffFile{{Path: "日本語/very-long-name.go", Add: 1200, Del: 30}, {Path: "snapshot.png", Add: -1, Del: -1}} {
				r := diffRow{path: file.Path, label: file.Path, file: file}
				out := renderDiffRow(r, true, width, 2000, 6, 4, ascii)
				if lipgloss.Width(out) > width {
					t.Fatalf("width %d: %q", width, out)
				}
				plain := ansi.Strip(out)
				if ascii && strings.ContainsAny(plain, "█░▰▱─▸") {
					t.Fatalf("non ASCII: %q", plain)
				}
				if width == 90 && file.Add >= 0 && !strings.Contains(plain, "+1,200") {
					t.Fatalf("lost counts: %q", plain)
				}
			}
		}
	}
}
func TestDiffToggleParent(t *testing.T) {
	m := Model{diffFiles: []git.DiffFile{{Path: "docs/a"}, {Path: "docs/b"}}, diffCursor: 2}
	m.toggleDiffDir("docs/")
	if m.diffCursor != 0 || len(m.diffRows()) != 1 {
		t.Fatal("collapse did not retain visible cursor")
	}
	m.toggleDiffDir("docs/")
	if len(m.diffRows()) != 3 {
		t.Fatal("expand failed")
	}
}

func TestDiffLargeListNavigation(t *testing.T) {
	m := renderModel()
	m.diffCollapsed = map[string]bool{"docs/": false}
	for i := 0; i < 988; i++ {
		m.diffFiles = append(m.diffFiles, git.DiffFile{Path: fmt.Sprintf("docs/file%04d.go", i), Add: i})
	}
	for i := 0; i < 988; i++ {
		_, next, _ := m.diffTabKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
		m = next.(Model)
	}
	if m.diffCursor != 988 {
		t.Fatalf("cursor %d", m.diffCursor)
	}
	_, next, _ := m.diffTabKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	m = next.(Model)
	if m.diffCursor != 0 || len(m.diffRows()) != 1 {
		t.Fatal("parent collapse failed")
	}
}

func TestDiffBulkFolders(t *testing.T) {
	for _, tc := range []struct {
		name                    string
		count                   int
		mixed, binary, override bool
		label                   string
		closed                  bool
	}{
		{name: "text bulk", count: 10, label: "bulk/ (10 .txt files)", closed: true},
		{name: "mixed bulk", count: 10, mixed: true, label: "bulk/ (10 files)", closed: true},
		{name: "nine files", count: 9, label: "bulk/ (9 files)"},
		{name: "binary", count: 2, binary: true, label: "bulk/ (2 binary files)", closed: true},
		{name: "user override", count: 10, override: true, label: "bulk/ (10 .txt files)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files := make([]git.DiffFile, tc.count)
			for i := range files {
				ext := "txt"
				if tc.mixed && i == 0 {
					ext = "go"
				}
				files[i] = git.DiffFile{Path: fmt.Sprintf("bulk/%02d.%s", i, ext), Add: 2, Del: 1}
				if tc.binary {
					files[i].Add = -1
					files[i].Del = -1
				}
			}
			overrides := map[string]bool{}
			if tc.override {
				overrides["bulk/"] = false
			}
			rows := buildDiffRows(files, overrides)
			r := rows[0]
			if r.label != tc.label || r.collapsed != tc.closed {
				t.Fatalf("row: %+v", r)
			}
			if tc.closed {
				if len(rows) != 1 || !r.summary {
					t.Fatalf("summary: %+v", rows)
				}
				if tc.binary {
					if !strings.Contains(ansi.Strip(renderDiffRow(r, false, 90, 30, 3, 3, false)), "[bin]") {
						t.Fatal("missing binary marker")
					}
				} else if r.file.Add != tc.count*2 || r.file.Del != tc.count {
					t.Fatalf("totals: %+v", r.file)
				}
			}
			if tc.override && len(rows) != tc.count+1 {
				t.Fatal("override did not expand")
			}
		})
	}
}

func TestDiffResize(t *testing.T) {
	m := renderModel()
	m.list = worktreelist.New()
	m.rightTab = tabDiff
	m.layoutKind = LayoutRedesign
	files := []git.DiffFile{}
	for i := 0; i < 40; i++ {
		files = append(files, git.DiffFile{Path: fmt.Sprintf("file%02d.go", i), Add: 123, Del: 45})
	}
	m = m.onResize(tea.WindowSizeMsg{Width: 120, Height: 24})
	m.diffFiles = files
	m.diffCursor = 35
	m.refreshDiffPane()
	for _, width := range []int{60, 90, 120, 150} {
		m = m.onResize(tea.WindowSizeMsg{Width: width, Height: 24})
		inner := m.termInnerWidth()
		lines := strings.Split(m.renderDiff(), "\n")
		for _, line := range lines {
			if lipgloss.Width(line) > inner {
				t.Fatalf("width %d: %q", inner, line)
			}
		}
		if lipgloss.Width(lines[1]) != inner {
			t.Fatal("divider width")
		}
		visible := ansi.Strip(m.term.View())
		if !strings.Contains(visible, "file35.go") || !strings.Contains(visible, "▸") {
			t.Fatalf("cursor not visible: %s", visible)
		}
		// The viewport must contain freshly rendered lines, including the resized divider.
		m.term.GotoTop()
		if !strings.Contains(ansi.Strip(m.term.View()), strings.Repeat("━", inner)) {
			t.Fatal("stale viewport divider")
		}
	}
}

// TestDiffChurnBar covers rounding, minimum visibility, and binary placeholders.
func TestDiffChurnBar(t *testing.T) {
	for _, tc := range []struct {
		name           string
		add, del, peak int
		want, ascii    string
	}{
		{"empty", 0, 0, 100, "▱▱▱▱▱▱▱▱", "........"},
		{"minimum", 1, 0, 100, "▰▱▱▱▱▱▱▱", "#......."},
		{"half", 30, 20, 100, "▰▰▰▰▱▱▱▱", "####...."},
		{"full", 60, 40, 100, "▰▰▰▰▰▰▰▰", "########"},
		{"binary", -1, -1, 100, "────────", "--------"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, ascii := range []bool{false, true} {
				r := diffRow{label: "sample.go", file: git.DiffFile{Add: tc.add, Del: tc.del}}
				got := ansi.Strip(renderDiffRow(r, false, 90, tc.peak, 3, 3, ascii))
				want := tc.want
				if ascii {
					want = tc.ascii
				}
				if !strings.HasSuffix(got, want) || lipgloss.Width(got) != 90 {
					t.Fatalf("90-cell row: %q; want suffix %q", got, want)
				}
			}
		})
	}
}

func TestDiffSelectionHighlight(t *testing.T) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(old) })
	// Derive the SGR prefix from the theme rather than hard-coding its RGB value.
	sample := lipgloss.NewStyle().Background(theme.Current.Raised).Render("x")
	bg := strings.Split(sample, "x")[0]
	rows := []diffRow{
		{label: "file.go", file: git.DiffFile{Add: 20, Del: 5}},
		{label: "dir/", dir: true},
		{label: "bulk/", dir: true, summary: true, file: git.DiffFile{Add: 20, Del: 5}},
		{label: "binary/", dir: true, summary: true, file: git.DiffFile{Add: -1, Del: -1}},
	}
	for _, width := range []int{60, 90, 120} {
		for _, r := range rows {
			for _, selected := range []bool{false, true} {
				out := renderDiffRow(r, selected, width, 100, 3, 3, false)
				if strings.Contains(out, bg) != selected {
					t.Fatalf("selection %v: %q", selected, out)
				}
				if lipgloss.Width(out) != width {
					t.Fatalf("width %d: %q", width, out)
				}
				if selected {
					// Every independently styled span, including padding and bar cells, has a background.
					for _, span := range strings.Split(out, "\x1b[0m") {
						if ansi.Strip(span) != "" && !strings.Contains(span, "48;") {
							t.Fatalf("unhighlighted span: %q", span)
						}
					}
				}
			}
		}
	}
}

func TestDiffHighlightFollowsNavigation(t *testing.T) {
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(old) })
	m := renderModel()
	m.diffFiles = []git.DiffFile{{Path: "src/a.go", Add: 3}, {Path: "src/b.go", Del: 2}}
	for _, key := range []rune{'j', 'j', 'k', 'c', 'c'} {
		_, next, _ := m.diffTabKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{key}})
		m = next.(Model)
		for _, width := range []int{60, 90, 120} {
			m.width = width
			highlighted := 0
			for _, line := range strings.Split(m.renderDiff(), "\n") {
				if strings.Contains(line, "48;") {
					highlighted++
					if !strings.Contains(ansi.Strip(line), "▸") {
						t.Fatalf("highlight without cursor: %q", line)
					}
				}
			}
			if highlighted != 1 {
				t.Fatalf("key %c width %d: %d highlighted lines", key, width, highlighted)
			}
		}
	}
}
