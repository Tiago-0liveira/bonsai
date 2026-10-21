package assist

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden/approve"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/edit"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/spec"
)

func minimalDraft() map[string]any {
	doc := draftFixture()
	for _, key := range []string{"current_behavior", "desired_behavior", "requirements", "acceptance_criteria", "constraints", "open_questions", "resolved", "notes"} {
		doc[key] = []any{}
	}
	return doc
}

func item(text, basis, id string) map[string]any {
	return map[string]any{"text": text, "basis": basis, "question_id": id}
}

func openQuestion(text, id string) map[string]any {
	return map[string]any{"text": text, "question_id": id}
}

func TestAnsweredBasis(t *testing.T) {
	for _, tc := range []struct {
		name, basis, id, wantBasis, wantID string
		answers                            []Answer
	}{
		{"valid", "answered", "r1q1", "answered", "r1q1", []Answer{{QuestionID: "r1q1", State: Answered, Text: "Desktop"}}},
		{"unknown ID", "answered", "r9q1", "suggested", "", nil},
		{"empty ID", "answered", "", "suggested", "", nil},
		{"skipped", "answered", "r1q1", "suggested", "", []Answer{{QuestionID: "r1q1", State: Skipped}}},
		{"unknown", "answered", "r1q1", "suggested", "", []Answer{{QuestionID: "r1q1", State: Unknown}}},
		{"open", "answered", "r1q1", "suggested", "", []Answer{{QuestionID: "r1q1", State: Open}}},
		{"blank answer", "answered", "r1q1", "suggested", "", []Answer{{QuestionID: "r1q1", State: Answered, Text: " \n "}}},
		{"stated clears ID", "stated", "r1q1", "stated", "", nil},
		{"suggested clears ID", "suggested", "r1q1", "suggested", "", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := minimalDraft()
			doc["requirements"] = []any{item("Support desktop export.", tc.basis, tc.id)}
			got, err := DecodeDraft(encoded(t, doc), Input{Answers: tc.answers})
			if err != nil {
				t.Fatal(err)
			}
			req := got.Requirements[0]
			if req.Basis != tc.wantBasis || req.QuestionID != tc.wantID {
				t.Fatalf("unexpected basis: %+v", req)
			}
			s := got.Spec(nil)
			if tc.wantBasis == "suggested" {
				if s.Requirements != "" || strings.Contains(s.OpenQuestions, "Support desktop export.") {
					t.Fatalf("unconfirmed suggestion rendered: %+v", s)
				}
				if c := got.Spec(map[Item]bool{req: true}); c.Requirements != "- Support desktop export." {
					t.Fatalf("confirmed suggestion missing: %+v", c)
				}
			} else if s.Requirements != "- Support desktop export." {
				t.Fatalf("confirmed requirement missing: %+v", s)
			}
		})
	}
}

func TestUnansweredQuestionsStayOpen(t *testing.T) {
	for _, state := range []AnswerState{Skipped, Unknown, Open, Answered} {
		for _, tc := range []struct {
			name, text, id string
			carried        int
		}{
			{"omitted", "", "", 1},
			// A citation never replaces the carried question: the person's own words stay the blocker.
			{"valid citation", "Which platforms should export support?", "r1q1", 1},
			{"unknown citation", "Which platforms should export support?", "r9q9", 1},
			{"placeholder citation", "none", "r1q1", 1},
			{"unsafe citation", "<!-- resolved -->", "r1q1", 1},
		} {
			t.Run(string(state)+"/"+tc.name, func(t *testing.T) {
				doc := minimalDraft()
				if tc.text != "" {
					doc["open_questions"] = []any{openQuestion(tc.text, tc.id)}
				}
				// An answered entry with empty text is still no decision.
				in := Input{Answers: []Answer{{QuestionID: "r1q1", Question: "Which platforms are affected?", State: state}}}
				got, err := DecodeDraft(encoded(t, doc), in)
				if err != nil {
					t.Fatal(err)
				}
				open := approve.OpenQuestions(got.Spec(nil).OpenQuestions)
				if got.Carried != tc.carried || len(open) == 0 {
					t.Fatalf("unanswered question lost: %+v; gate questions=%v", got, open)
				}
				if tc.carried == 1 && open[len(open)-1] != in.Answers[0].Question {
					t.Fatalf("carried question missing: %v", open)
				}
			})
		}
	}
	for _, question := range []string{"[x] Which platforms are affected?", "none", "<!-- dropped -->", ""} {
		t.Run("hostile carried "+question, func(t *testing.T) {
			got, err := DecodeDraft(encoded(t, minimalDraft()), Input{Answers: []Answer{{QuestionID: "r1q1", Question: question, State: Skipped}}})
			if err != nil || got.Carried != 1 || len(approve.OpenQuestions(got.Spec(nil).OpenQuestions)) != 1 {
				t.Fatalf("carried blocker lost: %+v, %v", got, err)
			}
		})
	}
}

func TestExistingQuestionsAndResolution(t *testing.T) {
	const existing = "- First uncertainty?\n* [ ] Second uncertainty?\n- [x] A prior decision.\nnone\n\n+ Third uncertainty?"
	in := Input{Draft: spec.Render(spec.Spec{OpenQuestions: existing}), Answers: []Answer{
		{QuestionID: "r1q1", Question: "Which outcome?", State: Answered, Text: "A successful export."},
		{QuestionID: "r1q2", Question: "Which platform?", State: Unknown},
	}}
	for _, tc := range []struct {
		name, existingID, byID, want string
	}{
		{"omitted", "", "", existing},
		{"answered resolves", "e1", "r1q1", "* [ ] Second uncertainty?\n- [x] A prior decision.\nnone\n\n+ Third uncertainty?"},
		{"unanswered cannot resolve", "e2", "r1q2", existing},
		{"unknown answer ID", "e1", "r9q9", existing},
		{"unknown existing ID", "e99", "r1q1", existing},
		{"checked lines have no ID", "e3", "r1q1", "- First uncertainty?\n* [ ] Second uncertainty?\n- [x] A prior decision.\nnone\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := minimalDraft()
			if tc.existingID != "" {
				doc["resolved"] = []any{map[string]any{"existing_id": tc.existingID, "by_question_id": tc.byID}}
			}
			got, err := DecodeDraft(encoded(t, doc), in)
			if err != nil {
				t.Fatal(err)
			}
			if got.Spec(nil).OpenQuestions != tc.want+"\n- Which platform?" {
				t.Fatalf("existing lines changed:\ngot %q\nwant prefix %q", got.Spec(nil).OpenQuestions, tc.want)
			}
		})
	}
}

func TestRenderingOrderAndRoundTrip(t *testing.T) {
	const extraText = "Keep  spacing.\n\n```md\n## Literal heading\n- [x] Authored content\n```\n<!-- authored note -->"
	draft := spec.Render(spec.Spec{
		Summary: "The working summary.", OpenQuestions: "- Existing uncertainty?\n- [x] Settled by the person.",
		Extra: []spec.Section{{Title: "User details", Text: extraText}, {Title: "References", Text: "A reference."}},
	})
	doc := minimalDraft()
	for i, section := range []string{"current_behavior", "desired_behavior", "requirements", "acceptance_criteria", "constraints"} {
		doc[section] = []any{
			item("Keep this stated fact.", "stated", "ignored"),
			item("Propose a check for section "+section+".", "suggested", ""),
		}
		if i == 2 {
			doc[section] = append(doc[section].([]any), item("Use the confirmed answer.", "answered", "r1q1"))
		}
	}
	doc["open_questions"] = []any{openQuestion("A new uncertainty?", "")}
	in := Input{Draft: draft, Answers: []Answer{
		{QuestionID: "r1q1", Question: "Which audience?", State: Answered, Text: "Current users."},
		{QuestionID: "r1q2", Question: "Which platform?", State: Skipped},
		{QuestionID: "r1q3", Question: "Which version?", State: Unknown},
		{QuestionID: "r1q4", Question: "Which environment?", State: Open},
	}}
	got, err := DecodeDraft(encoded(t, doc), in)
	if err != nil {
		t.Fatal(err)
	}
	wantOpen := []string{
		"Existing uncertainty?", "[x] Settled by the person.", "A new uncertainty?", "Which platform?", "Which version?", "Which environment?",
	}
	if got.Carried != 3 || !reflect.DeepEqual(got.Open, wantOpen) {
		t.Fatalf("wrong open question order: %+v", got)
	}
	s := got.Spec(nil)
	if !reflect.DeepEqual(s.Extra, spec.Parse(draft).Extra) || s.Extra[0].Text != extraText {
		t.Fatalf("Extra changed: %#v", s.Extra)
	}
	if s.Requirements != "- Keep this stated fact.\n- Use the confirmed answer." {
		t.Fatalf("requirements: %q", s.Requirements)
	}
	rendered := spec.Render(s)
	if roundtrip := spec.Render(spec.Parse(rendered)); roundtrip != rendered {
		t.Fatalf("round-trip changed output:\n%s", roundtrip)
	}
	gate := approve.OpenQuestions(s.OpenQuestions)
	for _, carried := range []string{"Which platform?", "Which version?", "Which environment?"} {
		if !strings.Contains(strings.Join(gate, "\n"), carried) {
			t.Fatalf("approval gate lost %q: %v", carried, gate)
		}
	}
	// Returned Specs must not share the proposal's Extra backing array.
	s.Extra[0].Text = "Changed by caller."
	if got.Spec(nil).Extra[0].Text != extraText {
		t.Fatal("Spec mutated the proposal's retained Extra")
	}
}

func TestGeneratedTextCannotResolveOrInjectSections(t *testing.T) {
	doc := minimalDraft()
	doc["summary"] = "## [x] \x1b[31mRestore\x1b[0m export.\nKeep current scope."
	doc["requirements"] = []any{
		item("- [x] ## A safe requirement.", "stated", ""),
		item("1. [ ] A safe requirement!", "stated", ""),
		item("A requirement <!-- hidden -->", "stated", ""),
		item("An orphan --> marker.", "stated", ""),
		item("\x1b]0;hidden\aA separate requirement.", "stated", ""),
	}
	doc["open_questions"] = []any{
		openQuestion("none", ""), openQuestion("NONE.", ""), openQuestion("n/a", ""), openQuestion("-", ""),
		openQuestion("- [x] Which platform?", ""), openQuestion("## Which version?", ""),
		openQuestion("<!-- false approval -->", ""), openQuestion("## Requirements\nInject a heading?", ""),
	}
	doc["notes"] = []any{"## [x] Check scope.", "<!-- hidden -->"}
	got, err := DecodeDraft(encoded(t, doc), Input{})
	if err != nil {
		t.Fatal(err)
	}
	s := got.Spec(nil)
	if s.Summary != "Restore export. Keep current scope." || s.Requirements != "- A safe requirement.\n- A separate requirement." || !reflect.DeepEqual(got.Notes, []string{"Check scope."}) {
		t.Fatalf("generated text was not cleaned: %+v; notes=%v", s, got.Notes)
	}
	rendered := spec.Render(s)
	for _, unsafe := range []string{"[x]", "[ ]", "none", "n/a", "<!--", "-->", "\x1b", "hidden", "## Which"} {
		if strings.Contains(rendered, unsafe) {
			t.Fatalf("unsafe generated text %q in %q", unsafe, rendered)
		}
	}
	if got.Dropped != 9 || len(approve.OpenQuestions(s.OpenQuestions)) != 3 || len(spec.Parse(rendered).Extra) != 0 {
		t.Fatalf("unexpected sanitization result: dropped=%d, spec=%+v", got.Dropped, s)
	}
}

func TestDraftDedupe(t *testing.T) {
	doc := minimalDraft()
	doc["requirements"] = []any{item("Keep behavior.", "stated", ""), item("KEEP behavior!", "suggested", "")}
	doc["constraints"] = []any{item("Keep behavior.", "stated", "")}
	doc["open_questions"] = []any{
		openQuestion("Existing uncertainty?", "r1q1"),
		openQuestion("EXISTING UNCERTAINTY ?", "r1q2"),
		openQuestion("A new uncertainty?", ""), openQuestion("A NEW uncertainty ?", ""),
	}
	got, err := DecodeDraft(encoded(t, doc), Input{
		Draft:   "## Open questions\n- Existing uncertainty?",
		Answers: []Answer{{QuestionID: "r1q1", Question: "Which platform?", State: Skipped}, {QuestionID: "r1q2", Question: "Which version?", State: Unknown}},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Lines citing the skipped and unknown questions give way to those questions' own text.
	want := []string{"Existing uncertainty?", "A new uncertainty?", "Which platform?", "Which version?"}
	if got.Dropped != 2 || got.Carried != 2 || len(got.Requirements) != 1 || len(got.Constraints) != 1 || !reflect.DeepEqual(got.Open, want) {
		t.Fatalf("unexpected dedupe: %+v", got)
	}
}

func TestRenderedSizeCap(t *testing.T) {
	doc := minimalDraft()
	// Input fits the editor's byte cap, but retaining Extra and adding a summary
	// and an open question crosses it. No provider-size limit is involved.
	base, err := DecodeDraft(encoded(t, doc), Input{Draft: "## Details\nx"})
	if err != nil {
		t.Fatal(err)
	}
	overhead := len(spec.Render(base.Spec(nil))) - 1
	for _, tc := range []struct {
		name  string
		delta int
	}{{"at cap", 0}, {"over cap", 1}} {
		t.Run(tc.name, func(t *testing.T) {
			in := Input{Draft: "## Details\n" + strings.Repeat("a", edit.MaxSpecBytes-overhead+tc.delta)}
			got, err := DecodeDraft(encoded(t, doc), in)
			if tc.delta == 0 {
				if err != nil || len(spec.Render(got.Spec(nil))) != edit.MaxSpecBytes {
					t.Fatalf("exact size refused: %v", err)
				}
			} else if !errors.Is(err, ErrTooLarge) || !errors.Is(err, ErrMalformed) {
				t.Fatalf("got %v, want too large and malformed", err)
			}
		})
	}
}

func TestSkippedWithNotesKeepsOwnOpenLine(t *testing.T) {
	for _, state := range []AnswerState{Skipped, Unknown} {
		in := Input{Answers: []Answer{{QuestionID: "r1q1", Question: "Which platform?", State: state, Text: "maybe mac"}}}
		got, err := DecodeDraft(encoded(t, minimalDraft()), in)
		if err != nil {
			t.Fatal(err)
		}
		if open := approve.OpenQuestions(got.Spec(nil).OpenQuestions); got.Carried != 1 || !reflect.DeepEqual(open, []string{"Which platform?"}) {
			t.Fatalf("%s: carried=%d open=%v", state, got.Carried, open)
		}
	}
}

// An `o` round turns the draft's own open lines into questions; skipping one must not repeat it.
func TestSkippedOpenLineIsNotDuplicated(t *testing.T) {
	draft := spec.Render(spec.Spec{Summary: "S.", OpenQuestions: "- Which platform?"})
	in := Input{Draft: draft, Answers: []Answer{{QuestionID: "r2q1", Question: "Which platform?", State: Skipped}}}
	got, err := DecodeDraft(encoded(t, minimalDraft()), in)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"Which platform?"}; !reflect.DeepEqual(got.Open, want) || got.Carried != 0 {
		t.Fatalf("open=%v carried=%d", got.Open, got.Carried)
	}
}

func TestOpenRoundAnswerSettlesEarlierUnansweredQuestion(t *testing.T) {
	for _, state := range []AnswerState{Skipped, Unknown, Open} {
		t.Run(string(state), func(t *testing.T) {
			in := Input{
				Draft: spec.Render(spec.Spec{OpenQuestions: "- Which platform?"}),
				Answers: []Answer{
					{QuestionID: "r1q1", Question: "Which platform?", State: state},
					{QuestionID: "o2q1", Question: "Which platform?", State: Answered, Text: "Desktop"},
				},
			}
			doc := minimalDraft()
			// A later answer alone cannot silently remove an existing draft line.
			got, err := DecodeDraft(encoded(t, doc), in)
			if err != nil || len(approve.OpenQuestions(got.Spec(nil).OpenQuestions)) != 1 {
				t.Fatalf("unresolved existing question lost: %+v, %v", got, err)
			}
			doc["resolved"] = []any{map[string]any{"existing_id": "e1", "by_question_id": "o2q1"}}
			got, err = DecodeDraft(encoded(t, doc), in)
			if err != nil || got.Carried != 0 || len(approve.OpenQuestions(got.Spec(nil).OpenQuestions)) != 0 {
				t.Fatalf("answered open question came back: %+v, %v", got, err)
			}
			// Refining once more must not resurrect the historical skip either.
			in.Draft = spec.Render(got.Spec(nil))
			got, err = DecodeDraft(encoded(t, minimalDraft()), in)
			if err != nil || got.Carried != 0 || len(approve.OpenQuestions(got.Spec(nil).OpenQuestions)) != 0 {
				t.Fatalf("refinement resurrected the question: %+v, %v", got, err)
			}
		})
	}
}

func TestOpenRoundWithoutAnswerKeepsEarlierBlocker(t *testing.T) {
	for _, state := range []AnswerState{Skipped, Unknown, Open, Answered} {
		t.Run(string(state), func(t *testing.T) {
			in := Input{Answers: []Answer{
				{QuestionID: "r1q1", Question: "Which platform?", State: Skipped},
				{QuestionID: "o2q1", Question: "Which platform?", State: state},
			}}
			got, err := DecodeDraft(encoded(t, minimalDraft()), in)
			if err != nil || got.Carried != 1 || !reflect.DeepEqual(approve.OpenQuestions(got.Spec(nil).OpenQuestions), []string{"Which platform?"}) {
				t.Fatalf("unanswered open round lost its blocker: %+v, %v", got, err)
			}
		})
	}
}

func TestChoiceAloneIsAnswered(t *testing.T) {
	doc := minimalDraft()
	doc["requirements"] = []any{item("Support desktop export.", "answered", "r1q1")}
	in := Input{Answers: []Answer{{QuestionID: "r1q1", Question: "Which?", State: Answered, Choice: "Desktop"}}}
	got, err := DecodeDraft(encoded(t, doc), in)
	if err != nil || got.Requirements[0].Basis != "answered" || got.Carried != 0 {
		t.Fatalf("choice-only answer not honored: %+v, %v", got, err)
	}
}

func TestNewOpenQuestionCap(t *testing.T) {
	doc := minimalDraft()
	doc["open_questions"] = []any{openQuestion("New one?", ""), openQuestion("New two?", ""), openQuestion("New three?", ""), openQuestion("New four?", ""), openQuestion("New five?", "")}
	draft := spec.Render(spec.Spec{Summary: "S.", OpenQuestions: "- Existing one?\n- Existing two?\n- Existing three?\n- Existing four?"})
	in := Input{Draft: draft, Answers: []Answer{{QuestionID: "r1q1", Question: "Which platform?", State: Skipped}}}
	got, err := DecodeDraft(encoded(t, doc), in)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Existing one?", "Existing two?", "Existing three?", "Existing four?", "New one?", "New two?", "New three?", "Which platform?"}
	if !reflect.DeepEqual(got.Open, want) || got.Dropped != 2 || got.Carried != 1 {
		t.Fatalf("open=%v dropped=%d carried=%d", got.Open, got.Dropped, got.Carried)
	}
}

// Issue #115: every question answered, suggestions unconfirmed. Only real blockers remain.
func TestUnconfirmedSuggestionsDoNotBlock(t *testing.T) {
	doc := minimalDraft()
	doc["requirements"] = []any{item("Report export failures.", "answered", "r1q1"), item("Add a retry button.", "suggested", "")}
	doc["acceptance_criteria"] = []any{item("Retry works.", "suggested", "")}
	doc["open_questions"] = []any{openQuestion("What size limit applies?", "")}
	in := Input{Answers: []Answer{{QuestionID: "r1q1", Question: "What happens?", State: Answered, Choice: "An error appears", Text: "only over 50 MB"}}}
	got, err := DecodeDraft(encoded(t, doc), in)
	if err != nil {
		t.Fatal(err)
	}
	s := got.Spec(nil)
	if open := approve.OpenQuestions(s.OpenQuestions); !reflect.DeepEqual(open, []string{"What size limit applies?"}) {
		t.Fatalf("blockers = %v", open)
	}
	if s.Requirements != "- Report export failures." || s.AcceptanceCriteria != "" {
		t.Fatalf("unconfirmed rendered: %+v", s)
	}
	c := got.Spec(map[Item]bool{got.Requirements[1]: true})
	if c.Requirements != "- Report export failures.\n- Add a retry button." || c.AcceptanceCriteria != "" {
		t.Fatalf("confirmed not in section: %+v", c)
	}
	if open := approve.OpenQuestions(c.OpenQuestions); len(open) != 1 {
		t.Fatalf("confirming added blockers: %v", open)
	}
}
