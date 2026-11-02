package planrev

import (
	"reflect"
	"strings"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
)

func TestParseKeepsEverythingAndRespectsFences(t *testing.T) {
	in := "Intro line.\n\n## Steps\n1. a\n```\n## Not a heading\n```\n\n## Risks\nNone\n"
	got := Parse(in)
	var titles []string
	for _, s := range got {
		titles = append(titles, s.Title)
	}
	if !reflect.DeepEqual(titles, []string{"Summary", "Steps", "Risks"}) {
		t.Fatalf("titles = %v", titles)
	}
	if !strings.Contains(got[1].Text, "## Not a heading") {
		t.Fatalf("fenced heading split the section: %q", got[1].Text)
	}
	if Render(got) != garden.NormalizeContent(Render(Parse(Render(got)))) {
		t.Fatal("Render and Parse are not inverses")
	}
}

func TestMissingIsCaseInsensitiveAndIgnoresEmptySections(t *testing.T) {
	got := Missing(Parse("## steps\nx\n\n## Tests\n\n## RISKS\ny"))
	want := []string{"Assumptions", "Touched areas", "Tests", "Acceptance mapping"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("missing = %v, want %v", got, want)
	}
}

func TestFindIsFocusedAndCaseInsensitive(t *testing.T) {
	secs := Parse(goodPlan)
	got := Find(secs, "COUNTER")
	if len(got) != 1 || secs[got[0].Section].Title != "Risks" || got[0].Line != 0 {
		t.Fatalf("matches = %+v", got)
	}
	if Find(secs, "  ") != nil || Find(secs, "zzz") != nil {
		t.Fatal("blank or absent queries matched")
	}
}

func TestDiff(t *testing.T) {
	cases := []struct {
		name, a, b string
		add, del   int
		render     string
	}{
		{"same", "a\nb", "a\nb", 0, 0, " a| b"},
		{"insert", "a\nc", "a\nb\nc", 1, 0, " a|+b| c"},
		{"delete", "a\nb\nc", "a\nc", 0, 1, " a|-b| c"},
		{"replace", "a\nb\nc", "a\nx\nc", 1, 1, " a|-b|+x| c"},
		{"from empty", "", "a\nb", 2, 0, "+a|+b"},
		{"to empty", "a", "", 0, 1, "-a"},
		{"both empty", "", "", 0, 0, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lines := Diff(tc.a, tc.b)
			var parts []string
			for _, l := range lines {
				parts = append(parts, string(l.Op)+l.Text)
			}
			add, del := Changed(lines)
			if add != tc.add || del != tc.del || strings.Join(parts, "|") != tc.render {
				t.Fatalf("diff = %s (+%d -%d)", strings.Join(parts, "|"), add, del)
			}
		})
	}
	// Line numbers follow each side.
	for _, l := range Diff("a\nb\nc", "a\nx\nc") {
		switch {
		case l.Text == "b" && (l.Old != 2 || l.New != 0):
			t.Fatalf("deleted line numbers = %+v", l)
		case l.Text == "x" && (l.Old != 0 || l.New != 2):
			t.Fatalf("added line numbers = %+v", l)
		case l.Text == "c" && (l.Old != 3 || l.New != 3):
			t.Fatalf("kept line numbers = %+v", l)
		}
	}
}

func TestDiffFallsBackToReplacementPastTheCap(t *testing.T) {
	a := strings.Repeat("x\n", 2100)
	b := strings.Repeat("y\n", 2100)
	add, del := Changed(Diff(a, b))
	if add != 2101 || del != 2101 {
		t.Fatalf("+%d -%d", add, del)
	}
}

func TestCollapseKeepsContextAndCountsTheRest(t *testing.T) {
	var a, b []string
	for i := 0; i < 20; i++ {
		a = append(a, "line")
		b = append(b, "line")
	}
	b[10] = "changed"
	got := Collapse(Diff(strings.Join(a, "\n"), strings.Join(b, "\n")), 2)
	var parts []string
	for _, l := range got {
		if l.Skipped > 0 {
			parts = append(parts, "~"+string(rune('0'+l.Skipped/10))+string(rune('0'+l.Skipped%10)))
			continue
		}
		parts = append(parts, string(l.Op))
	}
	if strings.Join(parts, "") != "~08  -+  ~07" {
		t.Fatalf("collapsed = %q", strings.Join(parts, ""))
	}
	if Collapse(Diff("a", "a"), 3) != nil {
		t.Fatal("an unchanged comparison kept lines")
	}
}
