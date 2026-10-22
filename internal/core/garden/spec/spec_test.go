package spec

import (
	"reflect"
	"strings"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
)

func full() Spec {
	return Spec{
		Summary: "Resume after a disconnect.", CurrentBehavior: "Sessions end.\nWith no record.", DesiredBehavior: "They continue.",
		Requirements: "- keep state\n- retry", AcceptanceCriteria: "1. works\n2. tested", Constraints: "No new daemon.", OpenQuestions: "Timeout?",
		Extra: []Section{{Title: "Rollout", Text: "Behind a flag."}, {Title: "Risks", Text: "Few."}},
	}
}

func TestRenderParseRoundTrip(t *testing.T) {
	cases := map[string]Spec{
		"full":       full(),
		"empty":      {},
		"summary":    {Summary: "Only a summary."},
		"sparse":     {Requirements: "r", Constraints: "c"},
		"extra only": {Extra: []Section{{Title: "Notes", Text: "n"}}},
		"unicode":    {Summary: "Keep 界 and é safe.\n\n- ünï", Extra: []Section{{Title: "Äpfel", Text: "x"}}},
		"fenced":     {Requirements: "```md\n## Not a heading\n```\nafter"},
		"tilde":      {Summary: "~~~\n## Nope\n~~~"},
		"blank gaps": {Summary: "a\n\n\nb"},
	}
	for name, in := range cases {
		out := Render(in)
		got := Parse(out)
		want := in.Normalize()
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: Parse(Render(s)) = %#v, want %#v", name, got, want)
		}
		if Render(got) != out {
			t.Errorf("%s: Render is not stable", name)
		}
		if garden.NormalizeContent(out) != out {
			t.Errorf("%s: output is not normalized", name)
		}
	}
}

func TestRenderIsDeterministicAndNormalizesInput(t *testing.T) {
	a := Spec{Summary: "x  \r\n\r\n", Requirements: "\n\ny\t"}
	b := Spec{Summary: "x", Requirements: "y"}
	if Render(a) != Render(b) || garden.ContentHash(Render(a)) != garden.ContentHash(Render(b)) {
		t.Fatalf("a = %q, b = %q", Render(a), Render(b))
	}
	if got := Render(b); got != "## Summary\nx\n\n## Requirements\ny" {
		t.Fatalf("got %q", got)
	}
}

func TestParseNeverDropsText(t *testing.T) {
	in := "Free text before.\n\n## summary\nSecond summary part.\n\n## Requirements\nr1\n\n## Requirements\nr2\n\n## Roadmap\nlater\n\n## roadmap\nmore\n\n### Not level two\nkept"
	s := Parse(in)
	if s.Summary != "Free text before.\n\nSecond summary part." {
		t.Errorf("summary = %q", s.Summary)
	}
	if s.Requirements != "r1\n\nr2" {
		t.Errorf("requirements = %q", s.Requirements)
	}
	if len(s.Extra) != 1 || s.Extra[0].Title != "Roadmap" || s.Extra[0].Text != "later\n\nmore\n\n### Not level two\nkept" {
		t.Errorf("extra = %#v", s.Extra)
	}
	for _, want := range []string{"Free text before.", "Second summary part.", "r1", "r2", "later", "more", "### Not level two", "kept"} {
		if !strings.Contains(Render(s), want) {
			t.Errorf("rendering lost %q:\n%s", want, Render(s))
		}
	}
}

func TestParseHeadingMatchingIsCaseInsensitiveAndKeepsExtraTitles(t *testing.T) {
	s := Parse("## ACCEPTANCE CRITERIA\nok\n\n## My Own Heading\nx")
	if s.AcceptanceCriteria != "ok" || len(s.Extra) != 1 || s.Extra[0].Title != "My Own Heading" {
		t.Fatalf("%#v", s)
	}
}

func TestTitlesAndText(t *testing.T) {
	s := full()
	titles := s.Titles()
	if len(titles) != FixedCount+2 || titles[0] != "Summary" || titles[FixedCount] != "Rollout" {
		t.Fatalf("titles = %v", titles)
	}
	if s.Text(3) != "- keep state\n- retry" || s.Text(FixedCount+1) != "Few." {
		t.Fatal("Text returned the wrong section")
	}
}
