package ui

import (
	"fmt"
	"path"
	"sort"
	"strings"

	git "github.com/Tiago-0liveira/bonsai/internal/git/local"
	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

type diffRow struct {
	path, label, parent, prefix string
	dir, collapsed              bool
	summary                     bool
	file                        git.DiffFile
}
type diffNode struct {
	path  string
	files []git.DiffFile
	dirs  map[string]*diffNode
}

func diffNumber(n int) string {
	s := fmt.Sprint(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}
func (m Model) diffRows() []diffRow { return buildDiffRows(m.diffFiles, m.diffCollapsed) }
func buildDiffRows(files []git.DiffFile, collapsed map[string]bool) []diffRow {
	root := &diffNode{dirs: map[string]*diffNode{}}
	for _, f := range files {
		parts := strings.Split(f.Path, "/")
		n := root
		for _, part := range parts[:len(parts)-1] {
			key := n.path + part + "/"
			if n.dirs[part] == nil {
				n.dirs[part] = &diffNode{path: key, dirs: map[string]*diffNode{}}
			}
			n = n.dirs[part]
		}
		n.files = append(n.files, f)
	}
	var rows []diffRow
	var stats func(*diffNode) (int, bool)
	stats = func(n *diffNode) (int, bool) {
		count, binary := len(n.files), true
		for _, f := range n.files {
			binary = binary && f.Add < 0
		}
		for _, d := range n.dirs {
			c, b := stats(d)
			count += c
			binary = binary && b
		}
		return count, binary
	}
	var walk func(*diffNode, string, string)
	walk = func(n *diffNode, parent, indent string) {
		sort.Slice(n.files, func(i, j int) bool { return n.files[i].Path < n.files[j].Path })
		names := make([]string, 0, len(n.dirs))
		for name := range n.dirs {
			names = append(names, name)
		}
		sort.Strings(names)
		total := len(n.files) + len(names)
		connector := func(i int) string {
			if n == root {
				return ""
			}
			if i == total-1 {
				return indent + "└─ "
			}
			return indent + "├─ "
		}
		for i, f := range n.files {
			label := f.Path
			if n != root {
				label = strings.TrimPrefix(f.Path, n.path)
			}
			rows = append(rows, diffRow{path: f.Path, label: label, parent: parent, prefix: connector(i), file: f})
		}
		for i, name := range names {
			d := n.dirs[name]
			for len(d.files) == 0 && len(d.dirs) == 1 {
				for _, child := range d.dirs {
					d = child
					break
				}
			}
			count, binary := stats(d)
			// Large leaf folders can dominate navigation; show their totals until expanded.
			bulk := len(d.dirs) == 0 && len(d.files) >= 10
			closed := binary || bulk
			if v, ok := collapsed[d.path]; ok {
				closed = v
			}
			label := strings.TrimPrefix(d.path, n.path)
			if binary {
				label += fmt.Sprintf(" (%s binary files)", diffNumber(count))
			} else if bulk {
				ext := strings.ToLower(path.Ext(d.files[0].Path))
				for _, f := range d.files[1:] {
					if strings.ToLower(path.Ext(f.Path)) != ext {
						ext = ""
						break
					}
				}
				if ext != "" {
					label += fmt.Sprintf(" (%s %s files)", diffNumber(count), ext)
				} else {
					label += fmt.Sprintf(" (%s files)", diffNumber(count))
				}
			} else if count > 1 {
				label += fmt.Sprintf(" (%s files)", diffNumber(count))
			}
			aggregate := git.DiffFile{}
			if binary {
				aggregate.Add, aggregate.Del = -1, -1
			} else if bulk {
				for _, f := range d.files {
					aggregate.Add += max(0, f.Add)
					aggregate.Del += max(0, f.Del)
				}
			}
			rows = append(rows, diffRow{path: d.path, label: label, parent: parent, prefix: connector(len(n.files) + i), dir: true, collapsed: closed, summary: closed && (binary || bulk), file: aggregate})
			if !closed {
				walk(d, d.path, indent+"  ")
			}
		}
	}
	walk(root, "", "")
	return rows
}
func (m *Model) toggleDiffDir(path string) {
	rows := m.diffRows()
	for _, r := range rows {
		if r.dir && r.path == path {
			if m.diffCollapsed == nil {
				m.diffCollapsed = map[string]bool{}
			}
			m.diffCollapsed[path] = !r.collapsed
			break
		}
	}
	for i, r := range m.diffRows() {
		if r.path == path {
			m.diffCursor = i
			break
		}
	}
}
func renderDiffRow(r diffRow, selected bool, width, peak, aw, dw int, ascii bool) string {
	// Style each segment so inner ANSI resets cannot leave holes in selection.
	rowStyle := func(st lipgloss.Style) lipgloss.Style {
		if selected {
			return st.Background(theme.Current.Raised)
		}
		return st
	}
	space := rowStyle(lipgloss.NewStyle()).Render
	meta := rowStyle(prMeta)
	cursor := "  "
	if selected {
		cursor = "▸ "
	}
	label := r.prefix + r.label
	if r.dir {
		mark := "▾ "
		if r.collapsed {
			mark = "▸ "
		}
		label = r.prefix + mark + r.label
	}
	if ascii {
		cursor = strings.ReplaceAll(cursor, "▸", ">")
		label = strings.NewReplacer("▾", "v", "▸", ">", "├─", "|-", "└─", "`-").Replace(label)
	}
	suffix := ""
	if !r.dir || r.summary {
		bar := meta.Render("────────")
		if ascii {
			bar = meta.Render("--------")
		}
		counts := meta.Render(fmt.Sprintf("%*s", aw+dw+1, "[bin]"))
		if r.file.Add >= 0 {
			fraction := float64(r.file.Add+r.file.Del) / float64(max(1, peak))
			if fraction > 0 {
				fraction = max(fraction, 1.0/8)
			}
			bar = theme.Bar(fraction, 8, ascii, meta, rowStyle(lipgloss.NewStyle().Foreground(theme.Current.Dim)))
			counts = rowStyle(diffAdd).Render(fmt.Sprintf("%*s", aw, "+"+diffNumber(r.file.Add))) + space(" ") + rowStyle(diffDel).Render(fmt.Sprintf("%*s", dw, "-"+diffNumber(r.file.Del)))
		}
		suffix = space("  ") + counts + space("  ") + bar
	}
	// Keep numeric columns until the path has exhausted its available cells.
	if lipgloss.Width(suffix)+3 > width {
		suffix = ""
	}
	pathWidth := max(0, width-2-lipgloss.Width(suffix))
	label = ansi.Truncate(label, pathWidth, "…")
	label += strings.Repeat(" ", max(0, pathWidth-lipgloss.Width(label)))
	if selected {
		label = rowStyle(diffCursorSt).Render(label)
	} else {
		label = diffPathSt.Render(label)
	}
	return ansi.Truncate(space(cursor)+label+suffix, max(0, width), "")
}
