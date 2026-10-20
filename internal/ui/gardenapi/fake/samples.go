package fake

import "github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"

// Sample identifiers, matching the Garden setup mockups.
const (
	SampleRepoID   = 48120394
	SampleForkID   = 51993011
	SampleOpID     = "op-7f3a21c0"
	SampleLogin    = "octo-dev"
	SampleFullName = "acme/my-app"
)

// SampleTarget is the repository of the mockups, reached through two remotes.
func SampleTarget(access gardenapi.Access) gardenapi.Target {
	return gardenapi.Target{RepoID: SampleRepoID, FullName: SampleFullName, Aliases: []string{"origin", "upstream"}, Access: access, Kind: gardenapi.KindRepository}
}

// StatusFor returns a consistent status for state, as the real backend would
// report it for the sample repository (GS-01 and GS-06 to GS-08).
func StatusFor(state gardenapi.State) gardenapi.StatusView {
	v := gardenapi.StatusView{
		State: state, Sync: string(state),
		Target:       SampleTarget(gardenapi.AccessWrite),
		Identity:     gardenapi.Identity{Login: SampleLogin, ID: "1001"},
		Access:       gardenapi.AccessWrite,
		ProjectScope: gardenapi.PresencePresent,
	}
	v.Caps.Refresh = true
	switch state {
	case gardenapi.StateNone:
		v.Caps.Setup, v.Caps.Skip = true, true
	case gardenapi.StateSkipped:
		v.Caps.Unskip = true
	case gardenapi.StateReadOnly:
		v.Target.Access, v.Access = gardenapi.AccessRead, gardenapi.AccessRead
		v.RemoteGarden = gardenapi.PresencePresent
		v.IssueCount = 12
		v.Caps.Read = true
	case gardenapi.StateConnected:
		v.ProjectID = "PVT_sample"
		v.Caps.Read = true
	case gardenapi.StatePartialSetup:
		op := SampleOperation(gardenapi.StatePartialSetup)
		v.Operation = &op
		v.ProjectScope = gardenapi.PresenceAbsent
		v.Caps.Read, v.Caps.Resume, v.Caps.Skip = true, true, true
	case gardenapi.StateAwaitingManual:
		v.Caps.Read, v.Caps.Skip = true, true
	case gardenapi.StateOperationInProgress:
		op := SampleOperation(gardenapi.StateOperationInProgress)
		v.Operation = &op
		v.Caps.Read = true
	}
	return v
}

// SampleCandidates is GS-02. With ambiguous false only the source repository is returned.
func SampleCandidates(ambiguous bool) gardenapi.CandidatesView {
	v := gardenapi.CandidatesView{Candidates: []gardenapi.Target{SampleTarget(gardenapi.AccessWrite)}}
	if ambiguous {
		v.Ambiguous = true
		v.Candidates = append(v.Candidates, gardenapi.Target{
			RepoID: SampleForkID, FullName: "octo-dev/my-app", Aliases: []string{"fork"},
			Access: gardenapi.AccessAdmin, Kind: gardenapi.KindFork, ParentFullName: SampleFullName,
		})
	}
	return v
}

// SampleProjects is GS-03: one compatible Project, one same-name Project, one without Garden fields.
func SampleProjects() gardenapi.ProjectsView {
	return gardenapi.ProjectsView{Owner: "acme", Projects: []gardenapi.Project{
		{ID: "PVT_4", Number: "4", Title: "Roadmap", Stage: gardenapi.FieldPresent},
		{ID: "PVT_7", Number: "7", Title: "Bonsai Garden", Stage: gardenapi.FieldPresent, Type: gardenapi.FieldPresent, Conflict: true},
		{ID: "PVT_9", Number: "9", Title: "Q3 Launch"},
	}}
}

// SamplePlan is the Create preview of GS-04.
func SamplePlan() gardenapi.PlanView {
	return gardenapi.PlanView{
		PlanID:  "plan-sample",
		Request: gardenapi.PlanRequest{Mode: gardenapi.ModeCreate},
		Target:  SampleTarget(gardenapi.AccessWrite),
		Rows: []gardenapi.PlanRow{
			{Change: gardenapi.ChangeCreate, Kind: gardenapi.RowLabel, Target: "bonsai:managed", Note: "managed marker", Count: 1},
			{Change: gardenapi.ChangeCreate, Kind: gardenapi.RowLabels, Target: "bonsai:type/feature, chore, research", Note: "3 type labels", Count: 3},
			{Change: gardenapi.ChangeReuse, Kind: gardenapi.RowLabel, Target: "bonsai:type/bug", Note: "existing, kept as-is", Count: 1},
			{Change: gardenapi.ChangeCreate, Kind: gardenapi.RowLabels, Target: "bonsai:stage/draft … done", Note: "6 stage labels", Count: 6},
			{Change: gardenapi.ChangeCreate, Kind: gardenapi.RowProject, Target: "acme / Bonsai Garden", Note: "owned by acme, needs project scope", Count: 1},
			{Change: gardenapi.ChangeAdd, Kind: gardenapi.RowField, Target: "Stage  (single select, 6 options)", Note: "needs project scope", Count: 1},
			{Change: gardenapi.ChangeAdd, Kind: gardenapi.RowField, Target: "Bonsai Type  (single select, 4 options)", Note: "needs project scope", Count: 1},
			{Change: gardenapi.ChangeManual, Kind: gardenapi.RowView, Target: "Board layout for the Project", Note: "finish in the browser", Count: 1, Advisory: true},
		},
		Summary: gardenapi.PlanSummary{
			CreateLabels: 10, ReuseLabels: 1, CreateProject: true, ProjectName: "Bonsai Garden", ProjectOwner: "acme",
			AddFields: 2, NeedsProjectScope: true, CanApply: true,
		},
	}
}

// SampleOperation is the operation behind GS-06 (state partial-setup, stopped on a missing scope)
// or, for any other state, a finished or running one.
func SampleOperation(state gardenapi.State) gardenapi.OperationView {
	labels := gardenapi.OperationStep{Title: "Create 10 labels", Row: gardenapi.PlanRow{Change: gardenapi.ChangeCreate, Kind: gardenapi.RowLabels, Count: 10}, Status: gardenapi.StepDone}
	reuse := gardenapi.OperationStep{Title: "Reuse label bonsai:type/bug", Row: gardenapi.PlanRow{Change: gardenapi.ChangeReuse, Kind: gardenapi.RowLabel, Count: 1}, Status: gardenapi.StepDone}
	project := gardenapi.OperationStep{Title: `Create Project "Bonsai Garden"`, Row: gardenapi.PlanRow{Change: gardenapi.ChangeCreate, Kind: gardenapi.RowProject, Count: 1}, Status: gardenapi.StepDone}
	stage := gardenapi.OperationStep{Title: "Add field Stage", Row: gardenapi.PlanRow{Change: gardenapi.ChangeAdd, Kind: gardenapi.RowField, Count: 1}, Status: gardenapi.StepDone}
	typ := gardenapi.OperationStep{Title: "Add field Bonsai Type", Row: gardenapi.PlanRow{Change: gardenapi.ChangeAdd, Kind: gardenapi.RowField, Count: 1}, Status: gardenapi.StepDone}
	v := gardenapi.OperationView{ID: SampleOpID, State: state, Done: 14, Total: 14, Steps: []gardenapi.OperationStep{labels, reuse, project, stage, typ}}
	switch state {
	case gardenapi.StatePartialSetup:
		v.Done = 11
		v.Steps[2].Status, v.Steps[2].ErrorCode = gardenapi.StepFailed, gardenapi.StepScopeMissing
		v.Steps[3].Status, v.Steps[4].Status = gardenapi.StepPending, gardenapi.StepPending
	case gardenapi.StateOperationInProgress:
		v.Running, v.Done = true, 11
		v.Steps[2].Status = gardenapi.StepPending
		v.Steps[3].Status, v.Steps[4].Status = gardenapi.StepPending, gardenapi.StepPending
	}
	return v
}
