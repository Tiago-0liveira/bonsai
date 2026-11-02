package assist

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/approve"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/edit"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/spec"
)

func SanitizeGuidance(s string) string { return cappedText(s, MaxGuidanceRunes) }
func SanitizeFeedback(s string) string { return cappedText(s, MaxFeedbackRunes) }

func cappedText(s string, max int) string {
	r := []rune(cleanText(s))
	if len(r) > max {
		r = r[:max]
	}
	return strings.TrimSpace(string(r))
}

func cleanText(s string) string {
	runes := []rune(s)
	var b strings.Builder
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		// Consume entire terminal sequences so their parameters cannot become prose.
		if r == '\x1b' || r == '\u009b' || r == '\u009d' || r == '\u0090' || r == '\u009e' || r == '\u009f' || r == '\u0098' {
			kind := r
			if r == '\x1b' {
				i++
				if i == len(runes) {
					break
				}
				kind = runes[i]
			}
			switch kind {
			case '[', '\u009b':
				for i++; i < len(runes); i++ {
					if runes[i] >= 0x40 && runes[i] <= 0x7e {
						break
					}
				}
			case ']', 'P', 'X', '^', '_', '\u009d', '\u0090', '\u0098', '\u009e', '\u009f':
				for i++; i < len(runes); i++ {
					if runes[i] == '\a' || runes[i] == '\u009c' {
						break
					}
					if runes[i] == '\x1b' && i+1 < len(runes) && runes[i+1] == '\\' {
						i++
						break
					}
				}
			default:
				for i < len(runes) && runes[i] >= 0x20 && runes[i] <= 0x2f {
					i++
				}
			}
			continue
		}
		if unicode.IsSpace(r) {
			b.WriteByte(' ')
		} else if !unicode.IsControl(r) {
			b.WriteRune(r)
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

type existingQuestion struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

type inputAnswer struct {
	ID       string      `json:"id"`
	Question string      `json:"question"`
	Status   AnswerState `json:"status"`
	Choice   string      `json:"choice"`
	Text     string      `json:"text"`
}

type inputItem struct {
	Number int64           `json:"number"`
	Title  string          `json:"title"`
	Type   garden.ItemType `json:"type"`
}

type inputDocument struct {
	Schema                string             `json:"schema"`
	Step                  string             `json:"step"`
	Repository            string             `json:"repository"`
	Item                  *inputItem         `json:"item"`
	Description           string             `json:"description"`
	Draft                 string             `json:"draft"`
	ExistingOpenQuestions []existingQuestion `json:"existing_open_questions"`
	Answered              []inputAnswer      `json:"answered"`
	Feedback              string             `json:"feedback"`
	Preferences           string             `json:"preferences"`
	MaxQuestions          int                `json:"max_questions"`
}

func validType(t garden.ItemType) bool {
	return t == garden.TypeBug || t == garden.TypeFeature || t == garden.TypeChore || t == garden.TypeResearch
}

func validateInput(in Input) error {
	for _, s := range []string{in.Repository, in.Title, string(in.Type), in.Description, in.Draft, in.Feedback, in.Guidance} {
		if !utf8.ValidString(s) {
			return fmt.Errorf("%w: invalid UTF-8", ErrInvalidInput)
		}
	}
	if in.Number < 0 || (in.Type != "" && !validType(in.Type)) {
		return fmt.Errorf("%w: item identity", ErrInvalidInput)
	}
	if len(in.Description) > garden.MaxDescriptionBytes || len(in.Draft) > edit.MaxSpecBytes || len(in.Answers) > MaxSessionQuestions {
		return fmt.Errorf("%w: description, draft or session limit", ErrInvalidInput)
	}
	seen := map[string]bool{}
	for _, a := range in.Answers {
		for _, s := range []string{a.QuestionID, a.Question, string(a.State), a.Choice, a.Text} {
			if !utf8.ValidString(s) {
				return fmt.Errorf("%w: answer UTF-8", ErrInvalidInput)
			}
		}
		if len(a.Text) > MaxAnswerBytes || len(a.Choice) > MaxAnswerBytes || a.QuestionID == "" || utf8.RuneCountInString(a.QuestionID) > 16 || seen[a.QuestionID] {
			return fmt.Errorf("%w: answer size or identity", ErrInvalidInput)
		}
		seen[a.QuestionID] = true
		switch a.State {
		case Answered, Skipped, Unknown, Open:
		default:
			return fmt.Errorf("%w: answer state", ErrInvalidInput)
		}
	}
	return nil
}

func existingQuestions(draft string) []existingQuestion {
	questions := approve.OpenQuestions(spec.Parse(draft).OpenQuestions)
	out := make([]existingQuestion, 0, len(questions))
	for i, q := range questions {
		out = append(out, existingQuestion{ID: fmt.Sprintf("e%d", i+1), Text: q})
	}
	return out
}

func buildCall(in Input, step string) (Call, error) {
	if err := validateInput(in); err != nil {
		return Call{}, err
	}
	max := 0
	if step == "questions" {
		max = in.MaxQuestions
		if max == 0 {
			max = DefaultQuestions
		}
		if max < 1 || max > MaxQuestions || len(in.Answers) == MaxSessionQuestions {
			return Call{}, fmt.Errorf("%w: question limit", ErrInvalidInput)
		}
		if remaining := MaxSessionQuestions - len(in.Answers); max > remaining {
			max = remaining
		}
	}
	doc := inputDocument{
		Schema: inputSchemaID, Step: step, Repository: in.Repository,
		Description: in.Description, Draft: in.Draft,
		ExistingOpenQuestions: existingQuestions(in.Draft), Answered: make([]inputAnswer, 0, len(in.Answers)),
		Preferences: SanitizeGuidance(in.Guidance), MaxQuestions: max,
	}
	if in.Number != 0 || in.Title != "" || in.Type != "" {
		doc.Item = &inputItem{Number: in.Number, Title: in.Title, Type: in.Type}
	}
	for _, a := range in.Answers {
		doc.Answered = append(doc.Answered, inputAnswer{ID: a.QuestionID, Question: a.Question, Status: a.State, Choice: a.Choice, Text: a.Text})
	}
	call := Call{System: questionsSystem, Schema: questionsSchema(max).json(), Timeout: QuestionsTimeout, MaxOutput: MaxOutputBytes}
	if step == "draft" {
		doc.Feedback = SanitizeFeedback(in.Feedback)
		call.System, call.Schema, call.Timeout = draftSystem, draftSchema().json(), DraftTimeout
	}
	b, err := json.Marshal(doc)
	if err != nil {
		return Call{}, fmt.Errorf("%w: input JSON: %v", ErrInvalidInput, err)
	}
	call.Input = append([]byte("Input (JSON):\n"), b...)
	if len(call.Input) > MaxInputBytes {
		return Call{}, fmt.Errorf("%w: stdin limit", ErrInvalidInput)
	}
	return call, nil
}
