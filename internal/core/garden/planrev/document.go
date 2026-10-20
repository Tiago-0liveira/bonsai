// Package planrev is the plan-revision half of the Garden workflow (plan 08.3): it publishes a plan written in a
// read-only planner session as a content-addressed revision on the canonical Issue, reads every revision back
// with its integrity re-verified, and records the reviewer's decision about an exact revision. It starts no
// planner, agent, worktree or pull request, and approval ends at Ready.
package planrev

import (
	"fmt"
	"strings"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/plan"
)

// Titles of the sections Bonsai itself writes into every revision, from the attempt record. A published text
// cannot supply them: they say what was inspected and which specification the plan answers.
const (
	SectionSpec = "Source specification"
	SectionBase = "Inspected base"
)

// Required are the sections a plan must fill in before it is published.
var Required = []string{"Assumptions", "Touched areas", "Steps", "Tests", "Risks", "Acceptance mapping"}

// MaxBytes keeps a revision, with its envelope, under the adapter's comment limit.
const MaxBytes = 56 << 10

// Section is one headed part of a plan document.
type Section struct {
	Title string
	Text  string
}

// Parse splits a plan into its "## " sections in order. Text before the first heading becomes a Summary
// section, headings inside fenced code blocks are text, and no text is dropped.
func Parse(text string) []Section {
	var out []Section
	cur := Section{Title: "Summary"}
	flush := func() {
		cur.Text = garden.NormalizeContent(cur.Text)
		if cur.Text != "" || cur.Title != "Summary" {
			out = append(out, cur)
		}
	}
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
			flush()
			cur = Section{Title: strings.TrimSpace(strings.TrimPrefix(line, "## "))}
			continue
		}
		cur.Text += line + "\n"
	}
	flush()
	return out
}

// Render writes sections back as a normalized document.
func Render(sections []Section) string {
	var b strings.Builder
	for _, s := range sections {
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString("## " + s.Title + "\n" + garden.NormalizeContent(s.Text))
	}
	return garden.NormalizeContent(b.String())
}

// Compose builds the document that is published: Bonsai's two owned sections from the attempt, then the user's
// sections. A user section with an owned title is dropped, because only the attempt record can say what was inspected.
func Compose(text string, spec plan.SpecRef, base plan.Base) string {
	owned := []Section{
		{SectionSpec, fmt.Sprintf("Specification: s%d\nRevision: %s\nFingerprint: %s", spec.Ordinal, spec.RevisionHash, spec.Fingerprint)},
		{SectionBase, baseText(base)},
	}
	for _, s := range Parse(text) {
		if strings.EqualFold(s.Title, SectionSpec) || strings.EqualFold(s.Title, SectionBase) {
			continue
		}
		owned = append(owned, s)
	}
	return Render(owned)
}

func baseText(b plan.Base) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Commit: %s", b.Commit)
	if b.Branch != "" {
		fmt.Fprintf(&sb, "\nBranch: %s", b.Branch)
	}
	if b.Dirty {
		fmt.Fprintf(&sb, "\nUncommitted changes read: %d", b.DirtyFiles)
	}
	return sb.String()
}

// Missing lists the required sections that are absent or empty, in Required order.
func Missing(sections []Section) []string {
	var out []string
	for _, want := range Required {
		found := false
		for _, s := range sections {
			if strings.EqualFold(s.Title, want) && strings.TrimSpace(s.Text) != "" {
				found = true
			}
		}
		if !found {
			out = append(out, want)
		}
	}
	return out
}

// field reads "Key: value" from the text of an owned section.
func field(sections []Section, section, key string) string {
	for _, s := range sections {
		if !strings.EqualFold(s.Title, section) {
			continue
		}
		for _, line := range strings.Split(s.Text, "\n") {
			if k, v, ok := strings.Cut(line, ":"); ok && strings.EqualFold(strings.TrimSpace(k), key) {
				return strings.TrimSpace(v)
			}
		}
	}
	return ""
}

// Match is one search hit inside a document.
type Match struct {
	Section int // index into the searched sections
	Line    int // line within the section text, 0-based
}

// Find returns every line containing query, case-insensitively, in document order. A blank query matches nothing.
func Find(sections []Section, query string) []Match {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return nil
	}
	var out []Match
	for i, s := range sections {
		for j, line := range strings.Split(s.Text, "\n") {
			if strings.Contains(strings.ToLower(line), q) {
				out = append(out, Match{i, j})
			}
		}
	}
	return out
}
