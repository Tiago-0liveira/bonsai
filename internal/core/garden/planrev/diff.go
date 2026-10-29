package planrev

import "strings"

// Op says what a diff line did.
type Op byte

const (
	OpSame Op = ' '
	OpAdd  Op = '+'
	OpDel  Op = '-'
)

// Line is one line of a revision comparison. Old and New are 1-based line numbers, 0 on the side that lacks it.
// Collapse marks a run of unchanged lines it dropped with Skipped.
type Line struct {
	Op       Op
	Text     string
	Old, New int
	Skipped  int
}

// maxCells bounds the LCS table; past it the comparison reports a whole replacement instead of guessing.
const maxCells = 4_000_000

// Diff compares two plan texts line by line. It is the same for the unified and the side-by-side view: the wide
// view only lays the same lines out in two columns.
func Diff(oldText, newText string) []Line {
	a, b := split(oldText), split(newText)
	if len(a)*len(b) > maxCells {
		out := make([]Line, 0, len(a)+len(b))
		for i, l := range a {
			out = append(out, Line{Op: OpDel, Text: l, Old: i + 1})
		}
		for j, l := range b {
			out = append(out, Line{Op: OpAdd, Text: l, New: j + 1})
		}
		return out
	}
	// lcs[i][j] is the longest common subsequence of a[i:] and b[j:].
	lcs := make([][]int32, len(a)+1)
	for i := range lcs {
		lcs[i] = make([]int32, len(b)+1)
	}
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	var out []Line
	i, j := 0, 0
	for i < len(a) || j < len(b) {
		switch {
		case i < len(a) && j < len(b) && a[i] == b[j]:
			out = append(out, Line{Op: OpSame, Text: a[i], Old: i + 1, New: j + 1})
			i++
			j++
		case i < len(a) && (j == len(b) || lcs[i+1][j] >= lcs[i][j+1]):
			// Removals come before the additions that replace them, as in every unified diff.
			out = append(out, Line{Op: OpDel, Text: a[i], Old: i + 1})
			i++
		default:
			out = append(out, Line{Op: OpAdd, Text: b[j], New: j + 1})
			j++
		}
	}
	return out
}

func split(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// Changed counts the added and removed lines.
func Changed(lines []Line) (added, removed int) {
	for _, l := range lines {
		switch l.Op {
		case OpAdd:
			added++
		case OpDel:
			removed++
		}
	}
	return added, removed
}

// Collapse keeps the changed lines with ctx lines of context around them, and drops the rest: a Line with Op
// Skipped > 0 stands for a run of that many unchanged lines. With nothing changed it returns nil.
func Collapse(lines []Line, ctx int) []Line {
	keep := make([]bool, len(lines))
	any := false
	for i, l := range lines {
		if l.Op == OpSame {
			continue
		}
		any = true
		for k := max(i-ctx, 0); k <= min(i+ctx, len(lines)-1); k++ {
			keep[k] = true
		}
	}
	if !any {
		return nil
	}
	var out []Line
	skipped := 0
	flush := func() {
		if skipped > 0 {
			out = append(out, Line{Op: OpSame, Skipped: skipped})
			skipped = 0
		}
	}
	for i, l := range lines {
		if keep[i] {
			flush()
			out = append(out, l)
			continue
		}
		skipped++
	}
	flush()
	return out
}
