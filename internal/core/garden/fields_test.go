package garden

import "testing"

func TestCanonicalSetupFieldOnlyAliasesLegacyGardenType(t *testing.T) {
	for _, tc := range []struct {
		field ProjectField
		want  string
	}{
		{ProjectField{Name: "Type", Values: []string{"feature", "bug", "chore", "research"}}, ProjectTypeField},
		{ProjectField{Name: "type", Values: []string{"research", "chore", "bug", "feature"}}, ProjectTypeField},
		{ProjectField{Name: "Type"}, "Type"},
		{ProjectField{Name: "Type", Values: []string{"feature", "bug", "chore", "other"}}, "Type"},
		{ProjectField{Name: "Type", Values: []string{"feature", "bug", "chore", "chore"}}, "Type"},
		{ProjectField{Name: "Other", Values: []string{"feature", "bug", "chore", "research"}}, "Other"},
	} {
		got := CanonicalSetupField(tc.field)
		if got.Name != tc.want || got.ID != tc.field.ID || len(got.Values) != len(tc.field.Values) {
			t.Errorf("canonical field for %+v = %+v, want name %q", tc.field, got, tc.want)
		}
	}
}
