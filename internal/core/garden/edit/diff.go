package edit

import (
	"strings"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden/spec"
)

// SectionDiff counts the lines one section gained and lost.
type SectionDiff struct {
	Section        string
	Added, Removed int
}

// maxDiffCells bounds the LCS table; past it the diff falls back to counting lines present on one side only.
const maxDiffCells = 4_000_000

// Diff lists the sections whose text differs, in section order, with line counts.
func Diff(oldSpec, newSpec spec.Spec) []SectionDiff {
	titles := oldSpec.Titles()
	seen := map[string]bool{}
	for _, t := range titles {
		seen[strings.ToLower(t)] = true
	}
	for _, t := range newSpec.Titles() {
		if !seen[strings.ToLower(t)] {
			titles = append(titles, t)
			seen[strings.ToLower(t)] = true
		}
	}
	var out []SectionDiff
	for _, t := range titles {
		a, b := sectionText(oldSpec, t), sectionText(newSpec, t)
		if a == b {
			continue
		}
		added, removed := lineDiff(splitLines(a), splitLines(b))
		out = append(out, SectionDiff{Section: t, Added: added, Removed: removed})
	}
	return out
}

func sectionText(s spec.Spec, title string) string {
	for i, t := range s.Titles() {
		if strings.EqualFold(t, title) {
			return s.Text(i)
		}
	}
	return ""
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// lineDiff returns the lines added and removed between a and b using a longest common subsequence.
func lineDiff(a, b []string) (added, removed int) {
	if len(a)*len(b) > maxDiffCells {
		count := map[string]int{}
		for _, l := range a {
			count[l]++
		}
		for _, l := range b {
			if count[l] > 0 {
				count[l]--
			} else {
				added++
			}
		}
		for _, n := range count {
			removed += n
		}
		return added, removed
	}
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for i := range a {
		for j := range b {
			switch {
			case a[i] == b[j]:
				cur[j+1] = prev[j] + 1
			case prev[j+1] >= cur[j]:
				cur[j+1] = prev[j+1]
			default:
				cur[j+1] = cur[j]
			}
		}
		prev, cur = cur, prev
	}
	common := prev[len(b)]
	return len(b) - common, len(a) - common
}
