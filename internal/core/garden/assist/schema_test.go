package assist

import (
	"encoding/json"
	"reflect"
	"slices"
	"testing"
)

func TestStrictSchemas(t *testing.T) {
	for _, tc := range []struct {
		name string
		s    schema
	}{{"one question", questionsSchema(1)}, {"default questions", questionsSchema(DefaultQuestions)}, {"maximum questions", questionsSchema(MaxQuestions)}, {"draft", draftSchema()}} {
		t.Run(tc.name, func(t *testing.T) {
			var walk func(schema)
			walk = func(s schema) {
				t.Helper()
				if s.Type == "object" {
					if s.AdditionalProperties == nil || *s.AdditionalProperties {
						t.Fatal("object allows additional properties")
					}
					keys := make([]string, 0, len(s.Properties))
					for key, child := range s.Properties {
						keys = append(keys, key)
						walk(child)
					}
					slices.Sort(keys)
					if !reflect.DeepEqual(keys, s.Required) {
						t.Fatalf("required keys %v differ from properties %v", s.Required, keys)
					}
				}
				if s.Items != nil {
					walk(*s.Items)
				}
			}
			walk(tc.s)
			var doc any
			if err := json.Unmarshal(tc.s.json(), &doc); err != nil {
				t.Fatal(err)
			}
			var check func(any)
			check = func(v any) {
				switch v := v.(type) {
				case map[string]any:
					for k, child := range v {
						if k == "$ref" || k == "oneOf" || k == "format" {
							t.Fatalf("unsupported schema keyword %s", k)
						}
						check(child)
					}
				case []any:
					for _, child := range v {
						check(child)
					}
				}
			}
			check(doc)
		})
	}
	for n := 1; n <= MaxQuestions; n++ {
		if got := questionsSchema(n).Properties["questions"].MaxItems; got != n {
			t.Fatalf("questions maxItems=%d, want %d", got, n)
		}
	}
}
