package garden

import "strings"

// ProjectTypeField avoids GitHub's reserved built-in issue Type field.
const ProjectTypeField = "Bonsai Type"

// CanonicalSetupField lets saved Garden plans using the old Type name resume
// without rewriting their approved plan or its hash. Other Type fields are
// left alone: only Garden's exact single-select options identify this alias.
func CanonicalSetupField(f ProjectField) ProjectField {
	if !strings.EqualFold(f.Name, "Type") || len(f.Values) != 4 {
		return f
	}
	values := map[string]bool{}
	for _, v := range f.Values {
		values[v] = true
	}
	if values[string(TypeFeature)] && values[string(TypeBug)] && values[string(TypeChore)] && values[string(TypeResearch)] {
		f.Name = ProjectTypeField
	}
	return f
}
