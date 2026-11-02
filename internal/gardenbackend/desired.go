package gardenbackend

import (
	"strings"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/connect"
)

// DefaultProjectName is the Project Create makes and the name a same-name
// conflict is judged against.
const DefaultProjectName = "Bonsai Garden"

// Project field names the Garden reads and writes.
const (
	StageField = "Stage"
	TypeField  = garden.ProjectTypeField
)

var (
	allTypes  = []garden.ItemType{garden.TypeFeature, garden.TypeBug, garden.TypeChore, garden.TypeResearch}
	allStages = []garden.Stage{garden.StageDraft, garden.StageReady, garden.StageInProgress, garden.StageReview, garden.StageBlocked, garden.StageDone}
)

// desiredLabels is the canonical set of the garden contract and nothing else.
func desiredLabels() []connect.Label {
	labels := []connect.Label{{Name: garden.ManagedLabel, Color: "0e8a16", Description: "Managed by Bonsai Garden"}}
	for _, t := range allTypes {
		labels = append(labels, connect.Label{Name: garden.FormatTypeLabel(t), Color: "1d76db", Description: "Garden item type"})
	}
	for _, s := range allStages {
		labels = append(labels, connect.Label{Name: garden.FormatStageLabel(s), Color: "fbca04", Description: "Garden item stage"})
	}
	return labels
}

// desiredFields is Stage and Bonsai Type as single selects; no other field is created.
func desiredFields() []garden.ProjectField {
	stages := make([]string, len(allStages))
	for i, s := range allStages {
		stages[i] = string(s)
	}
	types := make([]string, len(allTypes))
	for i, t := range allTypes {
		types[i] = string(t)
	}
	return []garden.ProjectField{{Name: StageField, Values: stages}, {Name: TypeField, Values: types}}
}

// hasProjectField reports a field of that name; values are never compared,
// matching how the planner reuses fields.
func hasProjectField(p garden.Project, name string) bool {
	for _, f := range p.Fields {
		if strings.EqualFold(f.Name, name) {
			return true
		}
	}
	return false
}
