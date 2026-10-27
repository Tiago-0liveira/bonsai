package drafts

import "strings"

// Readiness says what a draft still lacks before it could become an Issue.
// It is pure: no store, clock or provider is involved.
type Readiness struct {
	// Missing names the required parts that are empty: Title, Problem, Outcome, Criteria.
	Missing []string
	// Unresolved counts open questions nobody has answered yet.
	Unresolved int
}

func (r Readiness) Ready() bool { return len(r.Missing) == 0 && r.Unresolved == 0 }

// Assess checks the required parts of d.
func Assess(d Draft) Readiness {
	var r Readiness
	if strings.TrimSpace(d.Title) == "" {
		r.Missing = append(r.Missing, "Title")
	}
	for _, req := range []struct{ name, text string }{
		{"Problem", d.Spec.CurrentBehavior},
		{"Outcome", d.Spec.DesiredBehavior},
		{"Criteria", d.Spec.AcceptanceCriteria},
	} {
		if strings.TrimSpace(req.text) == "" {
			r.Missing = append(r.Missing, req.name)
		}
	}
	for _, line := range strings.Split(d.Spec.OpenQuestions, "\n") {
		if openQuestion(line) {
			r.Unresolved++
		}
	}
	return r
}

// openQuestion reports whether a line of the Open questions section is a
// question still waiting for an answer.
func openQuestion(line string) bool {
	line = strings.TrimSpace(line)
	for {
		trimmed := stripMarker(line)
		if trimmed == line {
			break
		}
		line = trimmed
	}
	if line == "" || strings.EqualFold(line, "none") {
		return false
	}
	if strings.HasPrefix(line, "[x]") || strings.HasPrefix(line, "[X]") || strings.HasPrefix(line, "Resolved:") {
		return false
	}
	return true
}

func stripMarker(s string) string {
	for _, m := range []string{"-", "*", "•"} {
		if rest, ok := strings.CutPrefix(s, m); ok {
			return strings.TrimSpace(rest)
		}
	}
	// "1." and "12." style numbering.
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i > 0 && i < len(s) && s[i] == '.' {
		return strings.TrimSpace(s[i+1:])
	}
	return s
}
