// Package spec is the structured form of a Garden specification: seven fixed
// sections plus any others the text already had. Render and Parse are inverses
// on normalized text, and Parse never drops text, so an edit made on GitHub or a
// phone survives a round trip through the editor.
package spec

import (
	"strings"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
)

// Section is a heading the fixed sections do not cover. Its title is kept as written.
type Section struct {
	Title string
	Text  string
}

// Spec is one specification. Every field is normalized text without its heading.
type Spec struct {
	Summary            string
	CurrentBehavior    string
	DesiredBehavior    string
	Requirements       string
	AcceptanceCriteria string
	Constraints        string
	OpenQuestions      string
	Extra              []Section
}

// Names are the fixed section headings in render order.
var Names = [...]string{"Summary", "Current behavior", "Desired behavior", "Requirements", "Acceptance criteria", "Constraints", "Open questions"}

// FixedCount is the number of fixed sections.
const FixedCount = len(Names)

// Field returns the text of fixed section i.
func (s *Spec) Field(i int) *string {
	switch i {
	case 0:
		return &s.Summary
	case 1:
		return &s.CurrentBehavior
	case 2:
		return &s.DesiredBehavior
	case 3:
		return &s.Requirements
	case 4:
		return &s.AcceptanceCriteria
	case 5:
		return &s.Constraints
	case 6:
		return &s.OpenQuestions
	}
	return nil
}

// Titles lists every section of s in order: the fixed ones (even when empty), then Extra.
func (s Spec) Titles() []string {
	out := append([]string(nil), Names[:]...)
	for _, e := range s.Extra {
		out = append(out, e.Title)
	}
	return out
}

// Text returns the text of the section at index i of Titles.
func (s Spec) Text(i int) string {
	if i < FixedCount {
		return *s.Field(i)
	}
	return s.Extra[i-FixedCount].Text
}

// Normalize applies content normalization to every field and drops empty Extra sections.
func (s Spec) Normalize() Spec {
	for i := 0; i < FixedCount; i++ {
		f := s.Field(i)
		*f = garden.NormalizeContent(*f)
	}
	var extra []Section
	for _, e := range s.Extra {
		e.Title, e.Text = strings.TrimSpace(e.Title), garden.NormalizeContent(e.Text)
		if e.Title != "" && e.Text != "" {
			extra = append(extra, e)
		}
	}
	s.Extra = extra
	return s
}

// Render writes the specification: every non-empty section under a "## " heading, fixed sections first,
// then Extra in order. Output is normalized, so its hash depends only on the Spec.
func Render(s Spec) string {
	s = s.Normalize()
	var b strings.Builder
	write := func(title, text string) {
		if text == "" {
			return
		}
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString("## " + title + "\n" + text)
	}
	for i := 0; i < FixedCount; i++ {
		write(Names[i], *s.Field(i))
	}
	for _, e := range s.Extra {
		write(e.Title, e.Text)
	}
	return garden.NormalizeContent(b.String())
}

// Parse reads a specification. Text before the first heading joins Summary, a heading that is not a fixed one
// becomes an Extra section, and repeated headings are joined, so no text is ever dropped. Headings inside fenced
// code blocks are text.
func Parse(text string) Spec {
	var s Spec
	type part struct {
		fixed int
		title string
		text  []string
	}
	cur := &part{fixed: 0}
	var parts []*part
	parts = append(parts, cur)
	fence := ""
	for _, line := range strings.Split(garden.NormalizeContent(text), "\n") {
		trimmed := strings.TrimLeft(line, " ")
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			marker := string(trimmed[0])
			run := len(trimmed) - len(strings.TrimLeft(trimmed, marker))
			if fence == "" {
				fence = trimmed[:run]
			} else if strings.HasPrefix(trimmed, fence) && strings.Trim(trimmed, marker+" ") == "" {
				fence = ""
			}
		} else if fence == "" && strings.HasPrefix(line, "## ") {
			title := strings.TrimSpace(strings.TrimPrefix(line, "## "))
			cur = &part{fixed: -1, title: title}
			for i, n := range Names {
				if strings.EqualFold(title, n) {
					cur.fixed = i
				}
			}
			parts = append(parts, cur)
			continue
		}
		cur.text = append(cur.text, line)
	}
	extra := map[string]int{}
	for _, p := range parts {
		body := garden.NormalizeContent(strings.Join(p.text, "\n"))
		if body == "" {
			continue
		}
		if p.fixed >= 0 {
			f := s.Field(p.fixed)
			if *f != "" {
				*f += "\n\n"
			}
			*f += body
			continue
		}
		key := strings.ToLower(p.title)
		if i, ok := extra[key]; ok {
			s.Extra[i].Text += "\n\n" + body
			continue
		}
		extra[key] = len(s.Extra)
		s.Extra = append(s.Extra, Section{Title: p.title, Text: body})
	}
	return s
}
