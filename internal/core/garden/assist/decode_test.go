package assist

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
)

func encoded(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func questionFixture() map[string]any {
	return map[string]any{
		"schema": questionsSchemaID, "kind": "bug", "title": "Restore export", "understanding": "Export fails.",
		"questions": []any{map[string]any{
			"text": "Which platforms are affected?", "why": "Determines compatibility.", "area": "constraint", "suggestions": []any{"Desktop"},
		}},
	}
}

func draftFixture() map[string]any {
	doc := map[string]any{
		"schema": draftSchemaID, "title": "Restore export", "type": "bug", "summary": "Export fails. Restore successful export.",
		"open_questions": []any{map[string]any{"text": "Which platforms are affected?", "question_id": ""}},
		"resolved":       []any{map[string]any{"existing_id": "e1", "by_question_id": "r1q1"}},
		"notes":          []any{"Review the scope."},
	}
	for _, section := range []string{"current_behavior", "desired_behavior", "requirements", "acceptance_criteria", "constraints"} {
		doc[section] = []any{map[string]any{"text": "Preserve the reported behavior.", "basis": "stated", "question_id": ""}}
	}
	return doc
}

func first(doc map[string]any, field string) map[string]any {
	return doc[field].([]any)[0].(map[string]any)
}

func TestDecodeEnvelopeRejections(t *testing.T) {
	for _, kind := range []string{"questions", "draft"} {
		t.Run(kind, func(t *testing.T) {
			fixture := questionFixture
			arrayField, enumField, nestedEnum := "questions", "kind", "area"
			if kind == "draft" {
				fixture, arrayField, enumField, nestedEnum = draftFixture, "requirements", "type", "basis"
			}
			decodeReply := func(raw []byte) error {
				if kind == "questions" {
					_, err := DecodeQuestions(raw, 1, DefaultQuestions, nil)
					return err
				}
				_, err := DecodeDraft(raw, Input{})
				return err
			}
			for _, tc := range []struct {
				name string
				raw  []byte
			}{
				{"empty", nil}, {"null", []byte("null")}, {"array", []byte("[]")}, {"string", []byte(`"text"`)},
				{"number", []byte("1")}, {"invalid syntax", []byte("{")},
				{"trailing value", append(encoded(t, fixture()), []byte(" {}")...)},
				{"trailing garbage", append(encoded(t, fixture()), '!')},
				{"duplicate root", bytes.Replace(encoded(t, fixture()), []byte(`"title":`), []byte(`"title":"duplicate","title":`), 1)},
				{"duplicate nested", bytes.Replace(encoded(t, fixture()), []byte(`"text":`), []byte(`"text":"duplicate","text":`), 1)},
				{"payload cap", bytes.Repeat([]byte(" "), MaxPayloadBytes+1)},
				{"invalid UTF-8", bytes.Replace(encoded(t, fixture()), []byte("Restore export"), []byte{0xff}, 1)},
			} {
				t.Run(tc.name, func(t *testing.T) {
					if err := decodeReply(tc.raw); !errors.Is(err, ErrMalformed) {
						t.Fatalf("got %v, want malformed", err)
					}
				})
			}
			for _, tc := range []struct {
				name string
				edit func(map[string]any)
			}{
				{"wrong schema", func(d map[string]any) { d["schema"] = inputSchemaID }},
				{"missing schema", func(d map[string]any) { delete(d, "schema") }},
				{"null schema", func(d map[string]any) { d["schema"] = nil }},
				{"unknown root field", func(d map[string]any) { d["extra"] = true }},
				{"case variant", func(d map[string]any) { d["Title"] = d["title"]; delete(d, "title") }},
				{"missing root field", func(d map[string]any) { delete(d, "title") }},
				{"null root field", func(d map[string]any) { d["title"] = nil }},
				{"root wrong type", func(d map[string]any) { d["title"] = 1 }},
				{"root invalid enum", func(d map[string]any) { d[enumField] = "unknown" }},
				{"enum whitespace", func(d map[string]any) { d[enumField] = " bug " }},
				{"null array", func(d map[string]any) { d[arrayField] = nil }},
				{"array wrong type", func(d map[string]any) { d[arrayField] = "none" }},
				{"null array item", func(d map[string]any) { d[arrayField] = []any{nil} }},
				{"unknown nested field", func(d map[string]any) { first(d, arrayField)["id"] = "model-id" }},
				{"missing nested field", func(d map[string]any) { delete(first(d, arrayField), "text") }},
				{"null nested field", func(d map[string]any) { first(d, arrayField)["text"] = nil }},
				{"nested wrong type", func(d map[string]any) { first(d, arrayField)["text"] = true }},
				{"nested invalid enum", func(d map[string]any) { first(d, arrayField)[nestedEnum] = "invalid" }},
				{"title too long", func(d map[string]any) { d["title"] = strings.Repeat("界", 81) }},
			} {
				t.Run(tc.name, func(t *testing.T) {
					doc := fixture()
					tc.edit(doc)
					if err := decodeReply(encoded(t, doc)); !errors.Is(err, ErrMalformed) {
						t.Fatalf("got %v, want malformed", err)
					}
				})
			}
		})
	}
}

func TestDecodeTextAndArrayBounds(t *testing.T) {
	for _, tc := range []struct {
		name  string
		draft bool
		edit  func(map[string]any)
	}{
		{"understanding", false, func(d map[string]any) { d["understanding"] = strings.Repeat("a", 401) }},
		{"question short", false, func(d map[string]any) { first(d, "questions")["text"] = "Why?" }},
		{"question long", false, func(d map[string]any) { first(d, "questions")["text"] = strings.Repeat("a", 200) + "?" }},
		{"why long", false, func(d map[string]any) { first(d, "questions")["why"] = strings.Repeat("a", 161) }},
		{"suggestion long", false, func(d map[string]any) { first(d, "questions")["suggestions"] = []any{strings.Repeat("a", 121)} }},
		{"suggestions count", false, func(d map[string]any) { first(d, "questions")["suggestions"] = []any{"a", "b", "c", "d"} }},
		{"suggestions null", false, func(d map[string]any) { first(d, "questions")["suggestions"] = nil }},
		{"suggestion type", false, func(d map[string]any) { first(d, "questions")["suggestions"] = []any{1} }},
		{"cleaned question short", false, func(d map[string]any) { first(d, "questions")["text"] = "\x1b[31mWhy?\x1b[0m" }},
		{"empty title", true, func(d map[string]any) { d["title"] = " \n " }},
		{"summary", true, func(d map[string]any) { d["summary"] = strings.Repeat("a", 1201) }},
		{"item short", true, func(d map[string]any) { first(d, "requirements")["text"] = "ab" }},
		{"item empty", true, func(d map[string]any) { first(d, "requirements")["text"] = "" }},
		{"item long", true, func(d map[string]any) { first(d, "requirements")["text"] = strings.Repeat("a", 401) }},
		{"item after markers", true, func(d map[string]any) { first(d, "requirements")["text"] = "- [x] ## ab" }},
		{"question ID", true, func(d map[string]any) { first(d, "requirements")["question_id"] = strings.Repeat("q", 17) }},
		{"items count", true, func(d map[string]any) { d["constraints"] = repeated(first(d, "constraints"), 21) }},
		{"open short", true, func(d map[string]any) { first(d, "open_questions")["text"] = "Why?" }},
		{"open empty", true, func(d map[string]any) { first(d, "open_questions")["text"] = "" }},
		{"open long", true, func(d map[string]any) { first(d, "open_questions")["text"] = strings.Repeat("a", 301) }},
		{"open ID", true, func(d map[string]any) { first(d, "open_questions")["question_id"] = strings.Repeat("q", 17) }},
		{"open count", true, func(d map[string]any) { d["open_questions"] = repeated(first(d, "open_questions"), 31) }},
		{"resolved count", true, func(d map[string]any) { d["resolved"] = repeated(first(d, "resolved"), 31) }},
		{"existing ID", true, func(d map[string]any) { first(d, "resolved")["existing_id"] = strings.Repeat("e", 17) }},
		{"resolution ID", true, func(d map[string]any) { first(d, "resolved")["by_question_id"] = strings.Repeat("r", 17) }},
		{"note long", true, func(d map[string]any) { d["notes"] = []any{strings.Repeat("a", 201)} }},
		{"notes count", true, func(d map[string]any) { d["notes"] = []any{"a", "b", "c", "d"} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := questionFixture()
			if tc.draft {
				doc = draftFixture()
			}
			tc.edit(doc)
			var err error
			if tc.draft {
				_, err = DecodeDraft(encoded(t, doc), Input{})
			} else {
				_, err = DecodeQuestions(encoded(t, doc), 1, DefaultQuestions, nil)
			}
			if !errors.Is(err, ErrMalformed) {
				t.Fatalf("got %v, want malformed", err)
			}
		})
	}
}

func repeated(v any, n int) []any {
	out := make([]any, n)
	for i := range out {
		out[i] = v
	}
	return out
}

func TestDecodeQuestionsRecovery(t *testing.T) {
	for _, tc := range []struct {
		name    string
		texts   []string
		asked   []Answer
		max     int
		want    int
		dropped int
	}{
		{"zero", nil, nil, 5, 0, 0},
		{"missing mark", []string{"Which platform"}, nil, 5, 0, 1},
		{"bundled", []string{"Which platform? Which version?"}, nil, 5, 0, 1},
		{"mark in middle", []string{"Which platform? Explain."}, nil, 5, 0, 1},
		{"punctuation only", []string{".....?"}, nil, 5, 0, 1},
		{"duplicate", []string{"Which platform?", "WHICH   PLATFORM ?"}, nil, 5, 1, 1},
		{"word boundaries", []string{"Should we review a part?", "Should we review apart?"}, nil, 5, 2, 0},
		{"meaningful symbols", []string{"Should we support C++?", "Should we support C?"}, nil, 5, 2, 0},
		{"earlier skipped", []string{"Which platform?"}, []Answer{{Question: "which platform", State: Skipped}}, 5, 0, 1},
		{"earlier unknown", []string{"Which platform?"}, []Answer{{Question: "Which platform?", State: Unknown}}, 5, 0, 1},
		{"truncate valid", []string{"Which platform?", "Which version?", "Which audience?"}, nil, 2, 2, 1},
		{"drop before truncate", []string{"Not a question.", "Which platform?", "Which version?"}, nil, 2, 2, 1},
		{"session remaining", []string{"Which platform?", "Which version?"}, sessionAnswers(29), 5, 1, 1},
		{"session full", []string{"Which platform?"}, sessionAnswers(30), 5, 0, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := questionFixture()
			questions := make([]any, 0, len(tc.texts))
			for _, text := range tc.texts {
				questions = append(questions, map[string]any{"text": text, "why": "\x1b[31m Scope\n impact\x1b[0m", "area": "scope", "suggestions": []any{"Desktop", " desktop! ", "\x1b[0m"}})
			}
			doc["questions"] = questions
			got, err := DecodeQuestions(encoded(t, doc), 2, tc.max, tc.asked)
			if err != nil {
				t.Fatal(err)
			}
			if got.Kind != "bug" || got.Title != "Restore export" || got.Understanding != "Export fails." || len(got.Questions) != tc.want || got.Dropped != tc.dropped {
				t.Fatalf("unexpected result: %+v", got)
			}
			for i, q := range got.Questions {
				if q.ID != fmt.Sprintf("r2q%d", i+1) || q.Round != 2 || q.Why != "Scope impact" || len(q.Suggestions) != 1 || q.Suggestions[0] != "Desktop" {
					t.Fatalf("unexpected question: %+v", q)
				}
			}
		})
	}
	for _, tc := range []struct{ round, max, asked int }{{0, 5, 0}, {-1, 5, 0}, {1, 0, 0}, {1, 11, 0}, {1, 5, 31}} {
		if _, err := DecodeQuestions(encoded(t, questionFixture()), tc.round, tc.max, sessionAnswers(tc.asked)); !errors.Is(err, ErrMalformed) {
			t.Fatalf("invalid decode arguments %+v: %v", tc, err)
		}
	}
}

func TestDecodeEnums(t *testing.T) {
	for _, kind := range []string{"bug", "feature", "chore", "research", "unclear"} {
		t.Run(kind, func(t *testing.T) {
			doc := questionFixture()
			doc["kind"] = kind
			if _, err := DecodeQuestions(encoded(t, doc), 1, 5, nil); err != nil {
				t.Fatal(err)
			}
			if kind != "unclear" {
				draft := draftFixture()
				draft["type"] = kind
				got, err := DecodeDraft(encoded(t, draft), Input{})
				if err != nil || got.Type != garden.ItemType(kind) {
					t.Fatalf("type: %s, %v", got.Type, err)
				}
			}
		})
	}
	for _, area := range []string{"scope", "behavior", "acceptance", "constraint", "context"} {
		doc := questionFixture()
		first(doc, "questions")["area"] = area
		if _, err := DecodeQuestions(encoded(t, doc), 1, 5, nil); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDecodeValidBoundaries(t *testing.T) {
	questions := questionFixture()
	questions["title"], questions["understanding"] = strings.Repeat("界", 80), strings.Repeat("界", 400)
	q := first(questions, "questions")
	q["text"], q["why"] = strings.Repeat("界", 199)+"?", strings.Repeat("界", 160)
	q["suggestions"] = []any{strings.Repeat("界", 120), strings.Repeat("語", 120), strings.Repeat("文", 120)}
	if got, err := DecodeQuestions(encoded(t, questions), 1, 1, nil); err != nil || len(got.Questions) != 1 {
		t.Fatalf("valid question limits: %+v, %v", got, err)
	}
	for _, maximum := range []bool{false, true} {
		doc := minimalDraft()
		doc["title"] = "A"
		text, openText := "abc", "What?"
		if maximum {
			doc["title"], doc["summary"] = strings.Repeat("界", 80), strings.Repeat("界", 1200)
			text, openText = strings.Repeat("界", 400), strings.Repeat("界", 300)
			doc["notes"] = []any{strings.Repeat("界", 200), strings.Repeat("界", 200), strings.Repeat("界", 200)}
		} else {
			doc["summary"] = ""
		}
		doc["requirements"] = repeated(item(text, "stated", ""), 20)
		doc["open_questions"] = repeated(openQuestion(openText, ""), 30)
		doc["resolved"] = repeated(map[string]any{"existing_id": "e1", "by_question_id": "r1q1"}, 30)
		if _, err := DecodeDraft(encoded(t, doc), Input{}); err != nil {
			t.Fatalf("valid draft limits (maximum=%v): %v", maximum, err)
		}
	}
	for _, draft := range []bool{false, true} {
		doc := questionFixture()
		if draft {
			doc = minimalDraft()
		}
		raw := encoded(t, doc)
		raw = append(raw, bytes.Repeat([]byte(" "), MaxPayloadBytes-len(raw))...)
		var err error
		if draft {
			_, err = DecodeDraft(raw, Input{})
		} else {
			_, err = DecodeQuestions(raw, 1, 5, nil)
		}
		if err != nil {
			t.Fatalf("exact payload bound (draft=%v): %v", draft, err)
		}
	}
}

func TestDecodeDraftRejectsInvalidInput(t *testing.T) {
	for _, in := range []Input{
		{Description: strings.Repeat("a", garden.MaxDescriptionBytes+1)},
		{Guidance: "\xff"},
		{Repository: strings.Repeat("a", MaxInputBytes)},
	} {
		if _, err := DecodeDraft(encoded(t, minimalDraft()), in); !errors.Is(err, ErrInvalidInput) || !errors.Is(err, ErrMalformed) {
			t.Fatalf("got %v, want invalid input and malformed", err)
		}
	}
}
