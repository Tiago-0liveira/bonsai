package assist

import (
	"strings"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
)

// SampleInputs is for inspecting prompt composition, never for provider calls.
var SampleInputs = []struct {
	Name string
	In   Input
}{
	{"bug", Input{Repository: "example/exporter", Title: "Export fails", Type: garden.TypeBug, Description: "When I export a large report, the app says it finished but the file is empty."}},
	{"feature", Input{Repository: "example/exporter", Title: "Remember export filters", Type: garden.TypeFeature, Description: "I rebuild the same filters each morning. Let me reuse a saved set."}},
	{"chore", Input{Repository: "example/exporter", Title: "Remove retired flag", Type: garden.TypeChore, Description: "The legacy export flag is no longer used. Remove it while keeping export behavior the same."}},
	{"research", Input{Repository: "example/exporter", Title: "Evaluate offline export", Type: garden.TypeResearch, Description: "We need evidence to decide whether offline export is practical. Compare the options within two days."}},
	{"ambiguous", Input{Repository: "example/exporter", Description: "The reports area feels awkward. It should work better."}},
	{"already complete", Input{Repository: "example/exporter", Number: 42, Title: "Show empty report status", Type: garden.TypeFeature, Description: "Show a clear status for empty reports.", Draft: "## Summary\nPeople viewing a report with no rows need a clear empty status.\n\n## Desired behavior\n- Show No results when the report contains zero rows.\n\n## Acceptance criteria\n- A zero-row report shows No results.\n- A report with rows displays them as before.\n\n## Constraints\n- Keep export behavior unchanged."}},
}

func TestSampleCalls(t *testing.T) {
	for _, sample := range SampleInputs {
		t.Run(sample.Name, func(t *testing.T) {
			for _, tc := range []struct {
				name, system string
				fn           func(Input) (Call, error)
			}{{"questions", questionsSystem, QuestionsCall}, {"draft", draftSystem, DraftCall}} {
				call, err := tc.fn(sample.In)
				if err != nil {
					t.Fatal(err)
				}
				if call.System != tc.system || !strings.HasPrefix(call.System, sharedPreamble+"\n\n") {
					t.Fatal("system instructions changed with sample input")
				}
				t.Logf("%s\nSystem:\n%s\n%s\nSchema:\n%s", tc.name, call.System, call.Input, call.Schema)
			}
		})
	}
}

func TestAdvisoryAreaIsOnlyConstraintAndContext(t *testing.T) {
	for area, want := range map[string]bool{"constraint": true, "context": true, "scope": false, "behavior": false, "acceptance": false, "": false, "other": false} {
		if got := AdvisoryArea(area); got != want {
			t.Errorf("AdvisoryArea(%q) = %v", area, got)
		}
	}
}
