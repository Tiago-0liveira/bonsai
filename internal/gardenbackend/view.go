package gardenbackend

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/connect"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
)

const needsScopeNote = "needs project scope"

// labelFamily splits a canonical label into its family and short name.
func labelFamily(name string) (family, short string) {
	switch {
	case name == garden.ManagedLabel:
		return "managed", name
	case strings.HasPrefix(name, garden.TypePrefix):
		return "type", strings.TrimPrefix(name, garden.TypePrefix)
	case strings.HasPrefix(name, garden.StagePrefix):
		return "stage", strings.TrimPrefix(name, garden.StagePrefix)
	}
	return "other", name
}

// planRows turns plan steps into display rows. Created labels of one family
// share a row (the contract's type and stage sets are 4 and 6 labels long); every
// other step keeps its own row, so a reuse or conflict is never hidden in a group.
func planRows(steps []connect.Step, owner string) []gardenapi.PlanRow {
	creates := map[string][]string{}
	for _, s := range steps {
		if s.Kind == connect.CreateLabel {
			if fam, short := labelFamily(s.Label.Name); fam == "type" || fam == "stage" {
				creates[fam] = append(creates[fam], short)
			}
		}
	}
	var rows []gardenapi.PlanRow
	emitted := map[string]bool{}
	for _, s := range steps {
		if s.Kind == connect.CreateLabel {
			if fam, _ := labelFamily(s.Label.Name); fam == "type" || fam == "stage" {
				if emitted[fam] {
					continue
				}
				emitted[fam] = true
				rows = append(rows, labelGroupRow(fam, creates[fam]))
				continue
			}
		}
		rows = append(rows, stepRow(s, owner))
	}
	return rows
}

func labelGroupRow(family string, shorts []string) gardenapi.PlanRow {
	prefix := garden.TypePrefix
	if family == "stage" {
		prefix = garden.StagePrefix
	}
	if len(shorts) == 1 {
		return gardenapi.PlanRow{Change: gardenapi.ChangeCreate, Kind: gardenapi.RowLabel, Target: prefix + shorts[0], Note: family + " label", Count: 1}
	}
	list := strings.Join(shorts, ", ")
	if len(shorts) > 3 {
		list = shorts[0] + " … " + shorts[len(shorts)-1]
	}
	return gardenapi.PlanRow{Change: gardenapi.ChangeCreate, Kind: gardenapi.RowLabels, Target: prefix + list, Note: fmt.Sprintf("%d %s labels", len(shorts), family), Count: len(shorts)}
}

// stepRow is the row of exactly one plan step.
func stepRow(s connect.Step, owner string) gardenapi.PlanRow {
	switch s.Kind {
	case connect.CreateLabel:
		note := "label"
		if s.Label.Name == garden.ManagedLabel {
			note = "managed marker"
		}
		return gardenapi.PlanRow{Change: gardenapi.ChangeCreate, Kind: gardenapi.RowLabel, Target: s.Label.Name, Note: note, Count: 1}
	case connect.ReuseLabel:
		return gardenapi.PlanRow{Change: gardenapi.ChangeReuse, Kind: gardenapi.RowLabel, Target: s.Label.Name, Note: "existing, kept as-is", Count: 1}
	case connect.ConflictLabel:
		return gardenapi.PlanRow{Change: gardenapi.ChangeManual, Kind: gardenapi.RowLabel, Target: s.Label.Name, Note: "conflicts with the canonical labels", Count: 1, Conflict: true}
	case connect.ConflictProject:
		return gardenapi.PlanRow{Change: gardenapi.ChangeManual, Kind: gardenapi.RowProject, Target: joinOwner(owner, s.ProjectName), Note: "same name, unverified", Count: 1, Conflict: true}
	case connect.CreateProject:
		note := needsScopeNote
		if owner != "" {
			note = "owned by " + owner + ", " + needsScopeNote
		}
		return gardenapi.PlanRow{Change: gardenapi.ChangeCreate, Kind: gardenapi.RowProject, Target: joinOwner(owner, s.ProjectName), Note: note, Count: 1}
	case connect.EnsureProjectField:
		return gardenapi.PlanRow{Change: gardenapi.ChangeAdd, Kind: gardenapi.RowField, Target: fieldTarget(s.Field), Note: needsScopeNote, Count: 1}
	}
	return gardenapi.PlanRow{Change: gardenapi.ChangeManual, Kind: gardenapi.RowView, Target: "Manual step", Note: "finish in the browser", Count: 1}
}

func joinOwner(owner, name string) string {
	if owner == "" {
		return name
	}
	return owner + " / " + name
}

func fieldTarget(f garden.ProjectField) string {
	f = garden.CanonicalSetupField(f)
	if len(f.Values) == 0 {
		return f.Name + "  (text)"
	}
	return fmt.Sprintf("%s  (single select, %d options)", f.Name, len(f.Values))
}

// browserLayoutRow is guidance the Project API cannot do: a board layout is a
// view, which GitHub only lets users arrange in the browser.
var browserLayoutRow = gardenapi.PlanRow{Change: gardenapi.ChangeManual, Kind: gardenapi.RowView, Target: "Board layout for the Project", Note: "finish in the browser", Count: 1, Advisory: true}

func summarize(plan connect.Plan, owner string, canWrite bool) gardenapi.PlanSummary {
	sum := gardenapi.PlanSummary{ImportCount: plan.ImportCount, ProjectOwner: owner}
	for _, s := range plan.Steps {
		switch s.Kind {
		case connect.CreateLabel:
			sum.CreateLabels++
		case connect.ReuseLabel:
			sum.ReuseLabels++
		case connect.CreateProject:
			sum.CreateProject, sum.ProjectName = true, s.ProjectName
			sum.NeedsProjectScope = true
		case connect.EnsureProjectField:
			sum.AddFields++
			sum.NeedsProjectScope = true
		case connect.ConflictLabel, connect.ConflictProject:
			sum.Conflicts++
		}
	}
	sum.CanApply = canWrite && plan.Mode != connect.Skip && len(plan.Steps) > 0 && sum.Conflicts == 0
	return sum
}

// planID fingerprints the target, mode and every step, so Apply can prove it
// runs exactly what the user saw.
func planID(id garden.GardenID, plan connect.Plan) string {
	raw, _ := json.Marshal(plan)
	sum := sha256.Sum256(append([]byte(id.String()+"\x00"), raw...))
	return hex.EncodeToString(sum[:16])
}

// operationSteps groups an operation's steps for display and reports each
// group's status. Older records used "remote-failure" for missing scopes; those
// are refined using the live token scope for compatibility.
func operationSteps(op connect.Operation, owner string, scopeMissing bool) []gardenapi.OperationStep {
	createDone := true
	for i, s := range op.Plan.Steps {
		if s.Kind == connect.CreateProject && i < len(op.Steps) && op.Steps[i].Status != connect.Done {
			createDone = false
		}
	}
	var out []gardenapi.OperationStep
	creating := -1 // index in out of the open "Create N labels" group
	for i, s := range op.Plan.Steps {
		if i >= len(op.Steps) {
			break
		}
		status, code := stepStatus(op.Steps[i], s, scopeMissing, createDone)
		if s.Kind == connect.CreateLabel {
			if creating >= 0 {
				g := &out[creating]
				g.Row.Count++
				g.Title = fmt.Sprintf("Create %d labels", g.Row.Count)
				g.Row.Kind = gardenapi.RowLabels
				g.Status, g.ErrorCode = mergeStatus(g.Status, g.ErrorCode, status, code)
				continue
			}
			creating = len(out)
		} else {
			creating = -1
		}
		out = append(out, gardenapi.OperationStep{Title: stepTitle(s, owner), Row: stepRow(s, owner), Status: status, ErrorCode: code})
	}
	return out
}

func stepStatus(st connect.OperationStep, s connect.Step, scopeMissing, createDone bool) (gardenapi.StepStatus, string) {
	switch st.Status {
	case connect.Done:
		return gardenapi.StepDone, ""
	case connect.Skipped:
		return gardenapi.StepSkipped, ""
	case connect.Failed:
		projectStep := s.Kind == connect.CreateProject || s.Kind == connect.EnsureProjectField
		// A field of a Project that was never created is waiting, not broken.
		if s.Kind == connect.EnsureProjectField && s.ProjectID == "" && !createDone {
			return gardenapi.StepPending, ""
		}
		if projectStep && scopeMissing && st.ErrorCode == gardenapi.StepRemoteFailure {
			return gardenapi.StepFailed, gardenapi.StepScopeMissing
		}
		return gardenapi.StepFailed, safeStepCode(st.ErrorCode)
	}
	return gardenapi.StepPending, ""
}

func safeStepCode(code string) string {
	switch code {
	case gardenapi.StepAuthRequired, gardenapi.StepRateLimited, gardenapi.StepOffline, gardenapi.StepLabelConflict, gardenapi.StepProjectConflict, gardenapi.StepScopeMissing, gardenapi.StepGraphQL, gardenapi.StepValidation:
		return code
	}
	return gardenapi.StepRemoteFailure
}

// mergeStatus folds one more step into a group: failed beats pending beats done.
func mergeStatus(cur gardenapi.StepStatus, curCode string, next gardenapi.StepStatus, nextCode string) (gardenapi.StepStatus, string) {
	rank := func(s gardenapi.StepStatus) int {
		switch s {
		case gardenapi.StepFailed:
			return 3
		case gardenapi.StepPending:
			return 2
		}
		return 1
	}
	if rank(next) > rank(cur) {
		return next, nextCode
	}
	return cur, curCode
}

func stepTitle(s connect.Step, owner string) string {
	switch s.Kind {
	case connect.CreateLabel:
		return "Create label " + s.Label.Name
	case connect.ReuseLabel:
		return "Reuse label " + s.Label.Name
	case connect.ConflictLabel:
		return "Label conflict " + s.Label.Name
	case connect.ConflictProject:
		return fmt.Sprintf("Project %q needs your decision", s.ProjectName)
	case connect.CreateProject:
		return fmt.Sprintf("Create Project %q", s.ProjectName)
	case connect.EnsureProjectField:
		return "Add field " + garden.CanonicalSetupField(s.Field).Name
	}
	return "Manual: " + s.Instructions
}
