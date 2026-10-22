package assist

import (
	"encoding/json"
	"slices"
)

const (
	inputSchemaID     = "bonsai.garden.assist.input/v1"
	questionsSchemaID = "bonsai.garden.assist.questions/v1"
	draftSchemaID     = "bonsai.garden.assist.draft/v1"
)

type schema struct {
	Type                 string            `json:"type"`
	Properties           map[string]schema `json:"properties,omitempty"`
	Required             []string          `json:"required,omitempty"`
	AdditionalProperties *bool             `json:"additionalProperties,omitempty"`
	Enum                 []string          `json:"enum,omitempty"`
	MinLength            int               `json:"minLength,omitempty"`
	MaxLength            int               `json:"maxLength,omitempty"`
	Items                *schema           `json:"items,omitempty"`
	MaxItems             int               `json:"maxItems,omitempty"`
}

func object(properties map[string]schema) schema {
	keys := make([]string, 0, len(properties))
	for k := range properties {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return schema{Type: "object", Properties: properties, Required: keys, AdditionalProperties: new(bool)}
}

func text(min, max int) schema          { return schema{Type: "string", MinLength: min, MaxLength: max} }
func enum(values ...string) schema      { return schema{Type: "string", Enum: values} }
func array(item schema, max int) schema { return schema{Type: "array", Items: &item, MaxItems: max} }

func questionsSchema(max int) schema {
	return object(map[string]schema{
		"schema":        enum(questionsSchemaID),
		"kind":          enum("bug", "feature", "chore", "research", "unclear"),
		"title":         text(0, 80),
		"understanding": text(0, 400),
		"questions": array(object(map[string]schema{
			"text":        text(5, 200),
			"why":         text(0, 160),
			"area":        enum("scope", "behavior", "acceptance", "constraint", "context"),
			"suggestions": array(text(0, 120), 3),
		}), max),
	})
}

func draftSchema() schema {
	item := array(object(map[string]schema{
		"text": text(3, 400), "basis": enum("stated", "answered", "suggested"), "question_id": text(0, 16),
	}), 20)
	return object(map[string]schema{
		"schema": enum(draftSchemaID), "title": text(1, 80),
		"type": enum("bug", "feature", "chore", "research"), "summary": text(0, 1200),
		"current_behavior": item, "desired_behavior": item, "requirements": item,
		"acceptance_criteria": item, "constraints": item,
		"open_questions": array(object(map[string]schema{
			"text": text(5, 300), "question_id": text(0, 16),
		}), 30),
		"resolved": array(object(map[string]schema{
			"existing_id": text(0, 16), "by_question_id": text(0, 16),
		}), 30),
		"notes": array(text(0, 200), 3),
	})
}

func (s schema) json() json.RawMessage {
	// The schema consists only of Go strings, numbers and booleans.
	b, _ := json.Marshal(s)
	return b
}
