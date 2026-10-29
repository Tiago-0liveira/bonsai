package assist

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden/approve"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/spec"
)

var leadingMarker = regexp.MustCompile(`^(?:[-*+]\s+|[0-9]+[.)]\s+|\[[ xX]\]\s*|#+\s*)`)

func cleanItem(s string) string {
	s = cleanText(s)
	if strings.Contains(s, "<!--") || strings.Contains(s, "-->") {
		return ""
	}
	for {
		next := strings.TrimSpace(leadingMarker.ReplaceAllString(s, ""))
		if next == s {
			return s
		}
		s = next
	}
}

func placeholder(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "none", "none.", "n/a", "-":
		return true
	}
	return false
}

func (p *Proposal) retainExisting(draft string, resolved map[string]bool) {
	text := spec.Parse(draft).OpenQuestions
	if text == "" {
		return
	}
	n := 0
	for _, line := range strings.Split(text, "\n") {
		if len(approve.OpenQuestions(line)) > 0 {
			n++
			if resolved[fmt.Sprintf("e%d", n)] {
				continue
			}
		}
		p.existingLines = append(p.existingLines, line)
		p.Open = append(p.Open, strings.TrimPrefix(line, "- "))
	}
	p.existingCount = len(p.Open)
}

// suggestions is every proposed item nobody stated or answered.
func (p Proposal) suggestions() map[Item]bool {
	out := map[Item]bool{}
	for _, section := range [][]Item{p.CurrentBehavior, p.DesiredBehavior, p.Requirements, p.AcceptanceCriteria, p.Constraints} {
		for _, item := range section {
			if item.Basis == "suggested" {
				out[item] = true
			}
		}
	}
	return out
}

// Spec renders the proposal. A suggestion appears only when confirmed: an
// unconfirmed one is neither saved nor an open question, so it cannot block approval.
func (p Proposal) Spec(confirmed map[Item]bool) spec.Spec {
	s := spec.Spec{Summary: p.Summary, Extra: append([]spec.Section(nil), p.extra...)}
	for i, section := range [][]Item{p.CurrentBehavior, p.DesiredBehavior, p.Requirements, p.AcceptanceCriteria, p.Constraints} {
		var lines []string
		for _, item := range section {
			if item.Basis == "stated" || item.Basis == "answered" || confirmed[item] {
				lines = append(lines, "- "+item.Text)
			}
		}
		*s.Field(i + 1) = strings.Join(lines, "\n")
	}
	lines := append([]string(nil), p.existingLines...)
	for _, q := range p.Open[p.existingCount:] {
		lines = append(lines, "- "+q)
	}
	s.OpenQuestions = strings.Join(lines, "\n")
	return s
}
