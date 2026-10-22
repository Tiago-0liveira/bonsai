package connect

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/procstore"
)

// LabelReader observes labels independently from mutation capability.
type LabelReader interface {
	ListLabels(context.Context, garden.RepoID) ([]Label, error)
}

// LabelWriter creates labels without permitting destructive edits.
type LabelWriter interface {
	CreateLabel(context.Context, garden.RepoID, Label) error
}

// OperationProjectWriter creates projects with a marker used to adopt remote success after a crash.
type OperationProjectWriter interface {
	EnsureProjectWithMarker(context.Context, garden.RepoID, string, string) (garden.Project, error)
}

// SetupPorts contains only the remote reads and idempotent setup writes used by Apply.
type SetupPorts struct {
	Labels            LabelReader
	LabelWrites       LabelWriter
	Projects          garden.ProjectReader
	ProjectWrites     garden.ProjectSetupWriter
	OperationProjects OperationProjectWriter
	// ViewerLogin is the authenticated GitHub login; GitHub exposes no stable creator ID on ProjectV2.
	ViewerLogin string
}

// ApplyResult reports durable progress and explicit browser work still required.
type ApplyResult struct {
	Operation      Operation
	AwaitingManual []string
	Conflicts      []Step
}

var activeLocalOperations sync.Map

// Apply records a resumable operation and applies allowed steps one at a time.
func Apply(ctx context.Context, store *Store, local garden.LocalRepoID, repo garden.RepoID, access Access, opID string, p Plan, ports SetupPorts) (ApplyResult, error) {
	if access < AccessWrite {
		return ApplyResult{}, ErrReadOnly
	}
	release, ok := reserveLocal(store, local)
	if !ok {
		return ApplyResult{}, ErrOperationInProgress
	}
	defer release()
	if err := p.Validate(); err != nil {
		return ApplyResult{}, err
	}
	if err := store.checkAssociationAccess(context.Background(), local, access); err != nil {
		return ApplyResult{}, err
	}
	return run(ctx, store, local, repo, opID, p, ports, false, access)
}

// Resume loads the operation and re-observes remote state before retrying incomplete steps.
func Resume(ctx context.Context, store *Store, opID string, repo garden.RepoID, ports SetupPorts, callerLocal garden.LocalRepoID, liveAccess Access) (ApplyResult, error) {
	op, e := store.Operation(ctx, opID)
	if e != nil {
		return ApplyResult{}, e
	}
	if op.ID != opID {
		return ApplyResult{}, ErrOperationNotFound
	}
	if op.RepositoryID == 0 {
		return ApplyResult{}, ErrInvalidOperation
	}
	if op.RepositoryID != repo {
		return ApplyResult{}, ErrOperationForeign
	}
	if callerLocal != op.LocalRepoID {
		return ApplyResult{}, ErrOperationForeign
	}
	if planHash(op.Plan) != op.PlanHash {
		return ApplyResult{}, ErrInvalidOperation
	}
	release, ok := reserveLocal(store, callerLocal)
	if !ok {
		return ApplyResult{}, ErrOperationInProgress
	}
	defer release()
	if err := op.Plan.Validate(); err != nil {
		return ApplyResult{}, err
	}
	if err := validateOperation(op); err != nil {
		return ApplyResult{}, err
	}
	access, e := store.operationAccess(ctx, op)
	if e != nil {
		return ApplyResult{}, e
	}
	if access < AccessWrite || liveAccess < AccessWrite || op.Plan.ImportCount != 0 {
		return ApplyResult{}, ErrReadOnly
	}
	return run(ctx, store, op.LocalRepoID, repo, op.ID, op.Plan, ports, true, access)
}

func reserveLocal(store *Store, local garden.LocalRepoID) (func(), bool) {
	key := store.Path + "\x00" + local.String()
	if _, loaded := activeLocalOperations.LoadOrStore(key, struct{}{}); loaded {
		return nil, false
	}
	return func() { activeLocalOperations.Delete(key) }, true
}

// OperationActive reports whether a setup operation is running for local, here or in another Bonsai
// process, so status can tell "in progress" from "interrupted". The cross-process probe holds the lock
// for an instant; a start racing that instant fails with ErrOperationInProgress and can be retried.
func OperationActive(store *Store, local garden.LocalRepoID) bool {
	if _, busy := activeLocalOperations.Load(store.Path + "\x00" + local.String()); busy {
		return true
	}
	lock, err := procstore.TryLock(localOperationLockPath(store.Path, local))
	if err != nil {
		return errors.Is(err, procstore.ErrLocked)
	}
	lock.Unlock()
	return false
}

func validateOperation(op Operation) error {
	if len(op.Steps) != len(op.Plan.Steps) {
		return ErrInvalidOperation
	}
	seen := make(map[string]bool, len(op.Steps))
	for i, step := range op.Steps {
		if step.ID == "" || seen[step.ID] || step.Kind != op.Plan.Steps[i].Kind {
			return ErrInvalidOperation
		}
		seen[step.ID] = true
		switch step.Status {
		case Pending, Done, Failed, Skipped:
		default:
			return ErrInvalidOperation
		}
	}
	return nil
}
func run(ctx context.Context, store *Store, local garden.LocalRepoID, repo garden.RepoID, id string, p Plan, ports SetupPorts, resume bool, access Access) (ApplyResult, error) {
	if e := p.Validate(); e != nil {
		return ApplyResult{}, e
	}
	if ports.Labels == nil || ports.LabelWrites == nil {
		return ApplyResult{}, fmt.Errorf("label ports required")
	}
	if e := ensureStoreDir(store.Path); e != nil {
		return ApplyResult{}, e
	}
	lock, e := procstore.TryLock(localOperationLockPath(store.Path, local))
	if errors.Is(e, procstore.ErrLocked) {
		return ApplyResult{}, ErrOperationInProgress
	}
	if e != nil {
		return ApplyResult{}, e
	}
	defer lock.Unlock()
	op := Operation{ID: id, LocalRepoID: local, PlanHash: planHash(p), Plan: p, Access: access, RepositoryID: repo}
	if resume {
		op, e = store.Operation(ctx, id)
		if e != nil {
			return ApplyResult{}, e
		}
	} else {
		for i, s := range p.Steps {
			step := OperationStep{ID: fmt.Sprintf("%s-%d", id, i), Kind: s.Kind, Status: Pending}
			if s.Kind == CreateProject {
				var nonce [32]byte
				if _, e := rand.Read(nonce[:]); e != nil {
					return ApplyResult{}, e
				}
				step.Nonce = hex.EncodeToString(nonce[:])
			}
			op.Steps = append(op.Steps, step)
		}
	}
	var observedProjects []garden.Project
	if resume {
		labels, e := ports.Labels.ListLabels(ctx, repo)
		if e != nil {
			return ApplyResult{}, safeError(e)
		}
		projects := []garden.Project{}
		if ports.Projects != nil {
			projects, e = ports.Projects.ListProjects(ctx, repo)
			if e != nil {
				return ApplyResult{}, safeError(e)
			}
		}
		observedProjects = projects
		for i, s := range p.Steps {
			op.Steps[i].Status = Pending
			op.Steps[i].ErrorCode = ""
			switch s.Kind {
			case CreateLabel:
				for _, l := range labels {
					if strings.EqualFold(l.Name, s.Label.Name) && l.Color == s.Label.Color && l.Description == s.Label.Description {
						op.Steps[i].Status = Done
					}
				}
			case ReuseLabel:
				for _, l := range labels {
					if strings.EqualFold(l.Name, s.Label.Name) {
						op.Steps[i].Status = Done
						break
					}
				}
			case CreateProject:
				for _, pr := range projects {
					if op.Steps[i].Attempted && verifiedOwnedProject(pr, op, i, repo, ports.ViewerLogin) {
						op.Steps[i].Status = Done
					}
				}
			case EnsureProjectField:
				for _, pr := range projects {
					boundID := s.ProjectID
					if boundID == "" && opHasCreateAttempt(op) && verifiedOwnedProject(pr, op, createStepIndex(op), repo, ports.ViewerLogin) {
						boundID = pr.ID
					}
					if boundID != "" && pr.ID == boundID && hasField(pr.Fields, s.Field) {
						op.Steps[i].Status = Done
					}
				}
			}
		}
		if e := store.saveOperation(ctx, op); e != nil {
			return ApplyResult{}, e
		}
	} else if e := store.saveOperation(ctx, op); e != nil && ctx.Err() != nil {
		if saveErr := store.saveOperation(context.Background(), op); saveErr != nil {
			return ApplyResult{}, saveErr
		}
	} else if e != nil {
		return ApplyResult{}, e
	}
	result := ApplyResult{}
	var deferredProjectErr error
	projectID := ""
	if resume {
		for _, pr := range observedProjects {
			if opHasCreateAttempt(op) && verifiedOwnedProject(pr, op, createStepIndex(op), repo, ports.ViewerLogin) {
				projectID = pr.ID
				for i, st := range op.Steps {
					if st.Kind == CreateProject {
						op.Steps[i].ProjectID = pr.ID
					}
				}
			}
		}
	}
	for i, s := range p.Steps {
		if e := ctx.Err(); e != nil {
			_ = store.saveOperation(context.Background(), op)
			return result, e
		}
		if op.Steps[i].Status == Done {
			continue
		}
		switch s.Kind {
		case ConflictLabel:
			op.Steps[i].Status = Failed
			op.Steps[i].ErrorCode = "label-conflict"
		case ConflictProject:
			op.Steps[i].Status = Failed
			op.Steps[i].ErrorCode = "project-conflict"
			result.Conflicts = append(result.Conflicts, s)
			deferredProjectErr = ErrProjectConflict
		case ManualStep:
			op.Steps[i].Status = Skipped
			result.AwaitingManual = append(result.AwaitingManual, s.Instructions)
		case ReuseLabel:
			op.Steps[i].Status = Done
		case CreateLabel:
			labels, e := ports.Labels.ListLabels(ctx, repo)
			if e != nil {
				return result, safeError(e)
			}
			found := false
			for _, old := range labels {
				if strings.EqualFold(old.Name, s.Label.Name) {
					found = true
					if old.Color == s.Label.Color && old.Description == s.Label.Description {
						op.Steps[i].Status = Done
					} else {
						op.Steps[i].Status = Failed
						op.Steps[i].ErrorCode = "label-conflict"
						result.Conflicts = append(result.Conflicts, Step{Kind: ConflictLabel, Label: old, Instructions: "A same-name label appeared with different properties; resolve it explicitly."})
					}
					break
				}
			}
			if found {
				break
			}
			if e := ports.LabelWrites.CreateLabel(ctx, repo, s.Label); e != nil {
				return failed(ctx, store, op, i, e)
			}
			labels, e = ports.Labels.ListLabels(ctx, repo)
			if e != nil {
				return result, safeError(e)
			}
			compatible := false
			for _, created := range labels {
				if strings.EqualFold(created.Name, s.Label.Name) {
					compatible = created.Color == s.Label.Color && created.Description == s.Label.Description
					if !compatible {
						op.Steps[i].Status = Failed
						op.Steps[i].ErrorCode = "label-conflict"
						result.Conflicts = append(result.Conflicts, Step{Kind: ConflictLabel, Label: created, Instructions: "A same-name label appeared with different properties; resolve it explicitly."})
					}
					break
				}
			}
			if compatible {
				op.Steps[i].Status = Done
			}
		case CreateProject:
			if ports.OperationProjects == nil {
				return result, fmt.Errorf("operation-aware project port required")
			}
			projects, e := observeProjects(ctx, ports, repo)
			if e != nil {
				return result, safeError(e)
			}
			for _, pr := range projects {
				if strings.EqualFold(pr.Name, s.ProjectName) && !verifiedOwnedProject(pr, op, i, repo, ports.ViewerLogin) {
					conflict := Step{Kind: ConflictProject, ProjectName: s.ProjectName, ProjectID: pr.ID, Instructions: "A same-name Project appeared without verifiable ownership. Select it explicitly with ConnectExisting if it is yours."}
					op.Steps[i].Status = Failed
					op.Steps[i].ErrorCode = "project-conflict"
					result.Conflicts = append(result.Conflicts, conflict)
					if e := store.saveOperation(ctx, op); e != nil {
						return result, e
					}
					deferredProjectErr = ErrProjectConflict
					goto nextStep
				}
			}
			op.Steps[i].Attempted = true
			if e := store.saveOperation(ctx, op); e != nil {
				return result, e
			}
			created, e := ports.OperationProjects.EnsureProjectWithMarker(ctx, repo, s.ProjectName, projectMarker(id, repo, op.Steps[i].Nonce))
			if e != nil {
				var scope ErrScopeMissing
				if errors.As(e, &scope) {
					op.Steps[i].Status = Failed
					op.Steps[i].ErrorCode = errorCode(e)
					deferredProjectErr = safeError(e)
					if se := store.saveOperation(ctx, op); se != nil {
						return result, se
					}
					continue
				}
				return failed(ctx, store, op, i, e)
			}
			if !verifiedCreatedProject(created, op, i, repo, ports.ViewerLogin) {
				conflict := Step{Kind: ConflictProject, ProjectName: s.ProjectName, ProjectID: created.ID, Instructions: "A same-name Project could not be verified after create. Select it explicitly with ConnectExisting if it is yours."}
				op.Steps[i].Status = Failed
				op.Steps[i].ErrorCode = "project-conflict"
				result.Conflicts = append(result.Conflicts, conflict)
				deferredProjectErr = ErrProjectConflict
				break
			}
			projectID = created.ID
			op.Steps[i].ProjectID = created.ID
			op.Steps[i].Status = Done
		case EnsureProjectField:
			if ports.ProjectWrites == nil {
				return result, fmt.Errorf("project setup port required")
			}
			s.Field = garden.CanonicalSetupField(s.Field)
			fieldProject := s.ProjectID
			if fieldProject == "" {
				fieldProject = op.Steps[i].ProjectID
			}
			if fieldProject == "" {
				fieldProject = projectID
			}
			if fieldProject == "" {
				op.Steps[i].Status = Failed
				op.Steps[i].ErrorCode = "project-conflict"
				break
			}
			if projectID != "" {
				fieldProject = projectID
			}
			projects, e := observeProjects(ctx, ports, repo)
			if e != nil {
				return result, safeError(e)
			}
			matched := false
			for _, pr := range projects {
				if s.ProjectName != "" && strings.EqualFold(pr.Name, s.ProjectName) && createStepIndex(op) >= 0 && !verifiedOwnedProject(pr, op, createStepIndex(op), repo, ports.ViewerLogin) {
					op.Steps[i].Status = Failed
					op.Steps[i].ErrorCode = "project-conflict"
					conflict := Step{Kind: ConflictProject, ProjectName: s.ProjectName, ProjectID: pr.ID, Instructions: "A same-name Project could not be verified before adding a field. Select it explicitly with ConnectExisting if it is yours."}
					result.Conflicts = append(result.Conflicts, conflict)
					deferredProjectErr = ErrProjectConflict
					goto fieldConflict
				}
				if pr.ID == fieldProject {
					matched = true
					break
				}
			}
			if !matched {
				op.Steps[i].Status = Failed
				op.Steps[i].ErrorCode = "project-conflict"
				result.Conflicts = append(result.Conflicts, Step{Kind: ConflictProject, ProjectName: s.ProjectName, ProjectID: fieldProject, Instructions: "The Project could not be verified before adding a field. Select it explicitly with ConnectExisting if it is yours."})
				deferredProjectErr = ErrProjectConflict
				break
			}
			goto fieldVerified
		fieldConflict:
			break
		fieldVerified:
			if _, e := ports.ProjectWrites.EnsureField(ctx, fieldProject, s.Field); e != nil {
				var scope ErrScopeMissing
				if errors.As(e, &scope) {
					op.Steps[i].Status = Failed
					op.Steps[i].ErrorCode = errorCode(e)
					deferredProjectErr = safeError(e)
					if se := store.saveOperation(ctx, op); se != nil {
						return result, se
					}
					continue
				}
				return failed(ctx, store, op, i, e)
			}
			op.Steps[i].Status = Done
		}
		if e := store.saveOperation(ctx, op); e != nil {
			return result, e
		}
	nextStep:
	}
	result.Operation = op
	if e := store.finishOperation(ctx, op); e != nil {
		return result, e
	}
	if deferredProjectErr != nil {
		return result, deferredProjectErr
	}
	return result, nil
}

func findProjectName(p Plan) string {
	for _, s := range p.Steps {
		if s.Kind == CreateProject {
			return s.ProjectName
		}
	}
	return ""
}

func projectMarker(op string, repo garden.RepoID, nonce string) string {
	return fmt.Sprintf("bonsai-operation:%s:garden:%d:nonce:%s", op, repo, nonce)
}
func observeProjects(ctx context.Context, ports SetupPorts, repo garden.RepoID) ([]garden.Project, error) {
	if ports.Projects == nil {
		return nil, fmt.Errorf("project reader required")
	}
	return ports.Projects.ListProjects(ctx, repo)
}
func createStepIndex(op Operation) int {
	for i, s := range op.Steps {
		if s.Kind == CreateProject {
			return i
		}
	}
	return -1
}
func verifiedOwnedProject(pr garden.Project, op Operation, stepIndex int, repo garden.RepoID, viewerLogin string) bool {
	if stepIndex < 0 || stepIndex >= len(op.Steps) || viewerLogin == "" || op.Steps[stepIndex].Nonce == "" || !op.Steps[stepIndex].Attempted {
		return false
	}
	return strings.EqualFold(pr.CreatorLogin, viewerLogin) && pr.Description == projectMarker(op.ID, repo, op.Steps[stepIndex].Nonce)
}
func verifiedCreatedProject(pr garden.Project, op Operation, stepIndex int, repo garden.RepoID, viewerLogin string) bool {
	return verifiedOwnedProject(pr, op, stepIndex, repo, viewerLogin) && strings.EqualFold(pr.Name, op.Plan.Steps[stepIndex].ProjectName)
}
func opHasCreateAttempt(op Operation) bool {
	for _, s := range op.Steps {
		if s.Kind == CreateProject && s.Attempted {
			return true
		}
	}
	return false
}

func localOperationLockPath(path string, id garden.LocalRepoID) string {
	sum := sha256.Sum256([]byte(id.String()))
	return path + ".operation." + hex.EncodeToString(sum[:]) + ".lock"
}

func projectsForResume(ports SetupPorts, ctx context.Context, repo garden.RepoID) []garden.Project {
	if !hasProjectsReader(ports) || ctx.Err() != nil {
		return nil
	}
	ps, _ := ports.Projects.ListProjects(ctx, repo)
	return ps
}
func hasProjectsReader(ports SetupPorts) bool { return ports.Projects != nil }
func failed(ctx context.Context, s *Store, op Operation, i int, e error) (ApplyResult, error) {
	op.Steps[i].Status = Failed
	op.Steps[i].ErrorCode = errorCode(e)
	_ = s.saveOperation(context.Background(), op)
	return ApplyResult{Operation: op}, safeError(e)
}
func errorCode(e error) string {
	var scope ErrScopeMissing
	switch {
	case errors.As(e, &scope):
		return "scope-missing"
	case errors.Is(e, garden.ErrGraphQL):
		return "github-graphql"
	case errors.Is(e, ErrGitHubValidation):
		return "github-validation"
	case errors.Is(e, ErrAuthRequired):
		return "auth-required"
	case errors.Is(e, ErrRateLimited):
		return "rate-limited"
	case errors.Is(e, ErrOffline):
		return "offline"
	default:
		return "remote-failure"
	}
}
