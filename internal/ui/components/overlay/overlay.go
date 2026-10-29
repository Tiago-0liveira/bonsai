// Package overlay composes bounded terminal frames without splitting cells.
package overlay

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

const reset = "\x1b[0m"

// Frame gives content explicit bounds, including otherwise empty padding.
type Frame struct {
	Content       string
	Width, Height int
}

// cut pads partial graphemes rather than moving the cells after them. Replaying
// SGR before the cut restores even compound and selective resets without having
// to maintain a second terminal style interpreter.
func cut(line string, left, width int, base string) string {
	if width <= 0 {
		return ""
	}
	var out strings.Builder
	out.WriteString(reset + base)
	pos, used := 0, 0
	var state byte
	for len(line) > 0 {
		seq, cells, n, next := ansi.DecodeSequence(line, state, nil)
		if cells > 0 {
			seq, cells = ansi.FirstGraphemeCluster(line, ansi.GraphemeWidth)
			n = len(seq)
		}
		line, state = line[n:], next
		if isSGR(seq) {
			if pos < left+width {
				out.WriteString(seq)
				out.WriteString(base)
			}
			continue
		}
		if cells == 0 {
			continue
		}
		end := pos + cells
		visible := max(0, min(end, left+width)-max(pos, left))
		if visible > 0 {
			if pos >= left && end <= left+width {
				out.WriteString(seq)
			} else {
				out.WriteString(strings.Repeat(" ", visible))
			}
			used += visible
		}
		pos = end
	}
	out.WriteString(reset + base)
	out.WriteString(strings.Repeat(" ", width-used))
	out.WriteString(reset)
	return out.String()
}

func isSGR(seq string) bool {
	return strings.HasPrefix(seq, "\x1b[") && strings.HasSuffix(seq, "m")
}

func rows(f Frame, base string) []string {
	lines := strings.Split(f.Content, "\n")
	out := make([]string, max(f.Height, 0))
	var history strings.Builder
	for y := range out {
		line := ""
		if y < len(lines) {
			line = lines[y]
		}
		out[y] = cut(history.String()+line, 0, max(f.Width, 0), base)
		// SGR can span input lines even though each output row is isolated.
		var state byte
		for len(line) > 0 {
			seq, _, n, next := ansi.DecodeSequence(line, state, nil)
			line, state = line[n:], next
			if isSGR(seq) {
				if seq == reset || seq == "\x1b[m" {
					history.Reset()
				} else {
					history.WriteString(seq)
				}
			}
		}
	}
	return out
}

// Compose overlays panel at cell coordinates, clipping to the parent's bounds.
// background is reapplied after every panel SGR so resets cannot reveal the
// parent through text, borders or padding.
func Compose(parent, panel Frame, x, y int, background ansi.Style) string {
	parent.Width, parent.Height = max(parent.Width, 0), max(parent.Height, 0)
	back := rows(parent, "")
	front := rows(panel, "")
	left, right := max(x, 0), min(x+max(panel.Width, 0), parent.Width)
	for row := max(y, 0); row < min(y+len(front), len(back)) && left < right; row++ {
		back[row] = cut(back[row], 0, left, "") +
			cut(front[row-y], left-x, right-left, background.String()) +
			cut(back[row], right, parent.Width-right, "")
	}
	return strings.Join(back, "\n")
}

// Center reserves a narrow rim for the parent at compact sizes, and more room
// around a bounded panel on larger terminals. All geometry is in whole cells.
func Center(width, height int) (x, y, w, h int) {
	width, height = max(width, 0), max(height, 0)
	w, h = min(104, max(width-16, 0)), min(26, max(height-10, 0))
	if width <= 80 {
		w = max(width-2, 0)
	}
	if height <= 24 {
		h = max(height-4, 0)
	}
	if width < 4 {
		w = width
	}
	if height < 7 {
		h = height
	}
	return (width - w) / 2, (height - h) / 2, w, h
}
