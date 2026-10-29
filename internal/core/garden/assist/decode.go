package assist

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/edit"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/spec"
)

func malformed(reason string) error { return fmt.Errorf("%w: %s", ErrMalformed, reason) }

func decode(raw []byte, s schema, dst any, draft bool) error {
	if len(raw) > MaxPayloadBytes {
		return malformed("payload limit")
	}
	if !utf8.Valid(raw) {
		return malformed("invalid UTF-8")
	}
	if err := garden.RejectDuplicateJSONKeys(raw); err != nil {
		return malformed("invalid or duplicate-key JSON")
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return malformed("invalid JSON")
	}
	if err := validateValue(value, s, "$", draft); err != nil {
		return err
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(dst); err != nil {
		return malformed("invalid fields")
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return malformed("trailing data")
	}
	return nil
}

// Validate against the same shape we send to providers, including required fields
// and exact key spelling, which encoding/json alone does not enforce.
func validateValue(v any, s schema, path string, draft bool) error {
	switch s.Type {
	case "object":
		obj, ok := v.(map[string]any)
		if !ok {
			return malformed(path + " must be an object")
		}
		if len(obj) != len(s.Properties) {
			return malformed(path + " has missing or unknown fields")
		}
		for k, child := range s.Properties {
			value, ok := obj[k]
			if !ok {
				return malformed(path + " missing " + k)
			}
			if err := validateValue(value, child, path+"."+k, draft); err != nil {
				return err
			}
		}
	case "array":
		arr, ok := v.([]any)
		if !ok {
			return malformed(path + " must be an array")
		}
		// Question overflow is explicitly recoverable: retain the first N valid,
		// distinct questions, while still validating every supplied entry.
		if len(arr) > s.MaxItems && path != "$.questions" {
			return malformed(path + " exceeds item limit")
		}
		for i, value := range arr {
			if err := validateValue(value, *s.Items, fmt.Sprintf("%s[%d]", path, i), draft); err != nil {
				return err
			}
		}
	case "string":
		str, ok := v.(string)
		if !ok {
			return malformed(path + " must be a string")
		}
		if len(s.Enum) > 0 {
			for _, allowed := range s.Enum {
				if str == allowed {
					return nil
				}
			}
			return malformed(path + " has invalid enum")
		}
		str = cleanText(str)
		if draft && (path == "$.summary" || strings.HasSuffix(path, ".text")) {
			cleaned := cleanItem(str)
			if (cleaned == "" && str != "") || (strings.HasPrefix(path, "$.open_questions[") && placeholder(cleaned)) {
				return nil // Unsafe items and resolved placeholders are dropped.
			}
			str = cleaned
		}
		n := utf8.RuneCountInString(str)
		if n < s.MinLength || (s.MaxLength > 0 && n > s.MaxLength) {
			return malformed(path + " exceeds text bounds")
		}
	}
	return nil
}

type questionReply struct {
	Schema        string `json:"schema"`
	Kind          string `json:"kind"`
	Title         string `json:"title"`
	Understanding string `json:"understanding"`
	Questions     []struct {
		Text        string   `json:"text"`
		Why         string   `json:"why"`
		Area        string   `json:"area"`
		Suggestions []string `json:"suggestions"`
	} `json:"questions"`
}

func normalized(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(cleanText(s)) {
		if !unicode.IsPunct(r) {
			b.WriteRune(r)
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func DecodeQuestions(raw []byte, round, max int, asked []Answer) (QuestionSet, error) {
	if round < 1 || max < 1 || max > MaxQuestions || len(asked) > MaxSessionQuestions || len(fmt.Sprintf("r%dq%d", round, max)) > 16 {
		return QuestionSet{}, malformed("round or question limit")
	}
	var reply questionReply
	if err := decode(raw, questionsSchema(max), &reply, false); err != nil {
		return QuestionSet{}, err
	}
	if remaining := MaxSessionQuestions - len(asked); max > remaining {
		max = remaining
	}
	out := QuestionSet{Kind: reply.Kind, Title: cleanText(reply.Title), Understanding: cleanText(reply.Understanding)}
	seen := map[string]bool{}
	for _, a := range asked {
		seen[normalized(a.Question)] = true
	}
	for _, q := range reply.Questions {
		t := cleanText(q.Text)
		key := normalized(t)
		if !strings.HasSuffix(t, "?") || strings.Count(t, "?") != 1 || key == "" || seen[key] || len(out.Questions) == max {
			out.Dropped++
			continue
		}
		seen[key] = true
		question := Question{ID: fmt.Sprintf("r%dq%d", round, len(out.Questions)+1), Round: round, Text: t, Why: cleanText(q.Why), Area: q.Area}
		suggestions := map[string]bool{}
		for _, suggestion := range q.Suggestions {
			suggestion = cleanText(suggestion)
			key := normalized(suggestion)
			if key != "" && !suggestions[key] {
				suggestions[key] = true
				question.Suggestions = append(question.Suggestions, suggestion)
			}
		}
		out.Questions = append(out.Questions, question)
	}
	return out, nil
}

type draftReply struct {
	Schema             string          `json:"schema"`
	Title              string          `json:"title"`
	Type               garden.ItemType `json:"type"`
	Summary            string          `json:"summary"`
	CurrentBehavior    []Item          `json:"current_behavior"`
	DesiredBehavior    []Item          `json:"desired_behavior"`
	Requirements       []Item          `json:"requirements"`
	AcceptanceCriteria []Item          `json:"acceptance_criteria"`
	Constraints        []Item          `json:"constraints"`
	OpenQuestions      []struct {
		Text       string `json:"text"`
		QuestionID string `json:"question_id"`
	} `json:"open_questions"`
	Resolved []struct {
		ExistingID   string `json:"existing_id"`
		ByQuestionID string `json:"by_question_id"`
	} `json:"resolved"`
	Notes []string `json:"notes"`
}

func DecodeDraft(raw []byte, in Input) (Proposal, error) {
	// Reuse call validation, including the serialized stdin bound, even if callers
	// pass a reply without first constructing a Call.
	if _, err := DraftCall(in); err != nil {
		return Proposal{}, fmt.Errorf("%w: %w", ErrMalformed, err)
	}
	var reply draftReply
	if err := decode(raw, draftSchema(), &reply, true); err != nil {
		return Proposal{}, err
	}
	answers := map[string]Answer{}
	latestAnswers := map[string]string{}
	for _, a := range in.Answers {
		answers[a.QuestionID] = a
		if key := normalized(cleanItem(a.Question)); key != "" {
			latestAnswers[key] = a.QuestionID
		}
	}
	answered := func(id string) bool {
		a, ok := answers[id]
		return ok && a.State == Answered && (cleanText(a.Choice) != "" || cleanText(a.Text) != "")
	}
	p := Proposal{Title: cleanText(reply.Title), Type: reply.Type, Summary: cleanItem(reply.Summary), extra: spec.Parse(in.Draft).Extra}
	sections := []struct {
		src []Item
		dst *[]Item
	}{
		{reply.CurrentBehavior, &p.CurrentBehavior}, {reply.DesiredBehavior, &p.DesiredBehavior},
		{reply.Requirements, &p.Requirements}, {reply.AcceptanceCriteria, &p.AcceptanceCriteria}, {reply.Constraints, &p.Constraints},
	}
	for _, section := range sections {
		seen := map[string]bool{}
		for _, item := range section.src {
			item.Text = cleanItem(item.Text)
			key := normalized(item.Text)
			if item.Text == "" || seen[key] {
				p.Dropped++
				continue
			}
			seen[key] = true
			if item.Basis == "answered" && !answered(item.QuestionID) {
				item.Basis = "suggested"
			}
			if item.Basis != "answered" {
				item.QuestionID = ""
			}
			*section.dst = append(*section.dst, item)
		}
	}
	resolved := map[string]bool{}
	for _, r := range reply.Resolved {
		if answered(r.ByQuestionID) {
			resolved[r.ExistingID] = true
		}
	}
	p.retainExisting(in.Draft, resolved)
	seen := map[string]bool{}
	for _, line := range p.Open {
		seen[normalized(cleanItem(line))] = true
	}
	added := 0
	for _, q := range reply.OpenQuestions {
		t := cleanItem(q.Text)
		if t == "" || placeholder(t) {
			p.Dropped++
			continue
		}
		if _, ok := answers[q.QuestionID]; !ok {
			q.QuestionID = ""
		}
		// An unanswered question is carried below in its own words, so a model line
		// citing it cannot bury it under different text.
		if q.QuestionID != "" && !answered(q.QuestionID) {
			continue
		}
		key := normalized(t)
		if seen[key] || added == MaxNewOpenQuestions {
			p.Dropped++
			continue
		}
		seen[key] = true
		added++
		p.Open = append(p.Open, t)
	}
	for _, a := range in.Answers {
		// Local open-question rounds repeat the draft's lines under new IDs. Once
		// answered, an earlier skip must not recreate the resolved blocker on every
		// refinement. Existing lines still require the resolved mapping above.
		if answered(a.QuestionID) || answered(latestAnswers[normalized(cleanItem(a.Question))]) {
			continue
		}
		t := cleanItem(a.Question)
		// Even hostile or placeholder question text must leave a visible blocker.
		if t == "" || placeholder(t) {
			t = "Clarify unanswered question " + a.QuestionID + "."
		}
		// An `o` round asks the draft's own open lines back, so the blocker may already be
		// kept by retainExisting; a second copy would only repeat it.
		if seen[normalized(t)] {
			continue
		}
		seen[normalized(t)] = true
		p.Open = append(p.Open, t)
		p.Carried++
	}
	for _, note := range reply.Notes {
		if note = cleanItem(note); note != "" {
			p.Notes = append(p.Notes, note)
		} else {
			p.Dropped++
		}
	}
	if len(spec.Render(p.Spec(p.suggestions()))) > edit.MaxSpecBytes {
		return Proposal{}, fmt.Errorf("%w: %w: rendered specification limit", ErrMalformed, ErrTooLarge)
	}
	return p, nil
}
