package gardenbackend

import (
	"context"
	"errors"
	"strings"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/connect"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
)

// planned is a plan with the connection it was built for.
type planned struct {
	target   connect.Target
	identity connect.GitHubIdentity
	conn     connect.Connection
	plan     connect.Plan
	project  *connect.ProjectRef
	owner    string
}

func connectMode(m gardenapi.Mode) (connect.Mode, error) {
	switch m {
	case gardenapi.ModeCreate:
		return connect.Create, nil
	case gardenapi.ModeConnectExisting:
		return connect.ConnectExisting, nil
	case gardenapi.ModeSkip:
		return connect.Skip, nil
	}
	return "", connect.ErrInvalidOperation
}

// ownerOf is the repository owner, the account Create makes the Project under.
func ownerOf(fullName string) string {
	owner, _, _ := strings.Cut(fullName, "/")
	return owner
}

// requireFresh refuses setup unless this clone has no association.
func (b *Backend) requireFresh(ctx context.Context) error {
	_, a, err := b.association(ctx)
	switch {
	case err != nil:
		return err
	case a == nil:
		return nil
	case a.Mode == connect.Skip:
		return ErrSkipped
	}
	return ErrAlreadySetUp
}

// buildPlan reads GitHub and builds the plan. It never writes.
func (b *Backend) buildPlan(ctx context.Context, req gardenapi.PlanRequest) (planned, error) {
	mode, err := connectMode(req.Mode)
	if err != nil {
		return planned{}, err
	}
	var p planned
	if p.target, _, err = b.resolve(ctx, req.Target); err != nil {
		return planned{}, err
	}
	if p.identity, err = b.identity(ctx); err != nil {
		return planned{}, err
	}
	p.conn = b.connection(p.target, p.identity)
	p.owner = ownerOf(p.target.FullName)
	// Owner is the repository owner so connect verifies a selected Project belongs to it.
	desired := connect.Desired{Labels: desiredLabels(), ProjectName: DefaultProjectName, Owner: p.owner, Fields: desiredFields()}
	if mode == connect.Skip {
		p.plan, err = connect.BuildPlan(connect.Observed{}, mode, connect.Desired{})
		return p, err
	}
	if _, err = p.conn.Setup(); err != nil {
		return planned{}, err
	}
	reader, err := p.conn.Reader()
	if err != nil {
		return planned{}, err
	}
	if b.deps.Ports.Labels == nil {
		return planned{}, errors.New("label reader required")
	}
	observed := connect.Observed{}
	if observed.Labels, err = b.deps.Ports.Labels.ListLabels(ctx, p.target.Repository.ID); err != nil {
		return planned{}, err
	}
	if mode == connect.Create && reader != nil {
		// Create has to see same-name Projects to flag them. Without the Project
		// scope the read is skipped: label steps still run and Apply re-reads
		// before it writes a Project.
		projects, perr := reader.ListProjects(ctx, p.target.Repository.ID)
		var scope connect.ErrScopeMissing
		switch {
		case perr == nil:
			observed.Projects = projects
		case !errors.As(perr, &scope):
			return planned{}, perr
		}
	}
	if mode == connect.ConnectExisting {
		if req.Project == nil {
			return planned{}, connect.ErrProjectNotFound
		}
		p.project = &connect.ProjectRef{ID: req.Project.ID, Number: req.Project.Number}
		desired.ExistingProject = p.project
	}
	p.plan, err = connect.BuildConnectionPlan(ctx, reader, p.target.Repository.ID, observed, mode, desired)
	return p, err
}

// PreviewPlan implements gardenapi.Backend.
func (b *Backend) PreviewPlan(ctx context.Context, req gardenapi.PlanRequest) (gardenapi.PlanView, error) {
	gen := b.gen.Add(1)
	if req.Mode != gardenapi.ModeSkip {
		if err := b.requireFresh(ctx); err != nil {
			return gardenapi.PlanView{}, apiError(err)
		}
	}
	p, err := b.buildPlan(ctx, req)
	if err != nil {
		return gardenapi.PlanView{}, apiError(err)
	}
	b.observe(p.target.GardenID)
	view := gardenapi.PlanView{
		Stamp:   b.stamp(gen, p.target.Repository.ID),
		Request: req,
		PlanID:  planID(p.target.GardenID, p.plan),
		Target:  targetView(p.target),
		Rows:    planRows(p.plan.Steps, p.owner),
		Summary: summarize(p.plan, p.owner, p.target.Repository.Access >= connect.AccessWrite),
	}
	if req.Mode != gardenapi.ModeSkip {
		view.Rows = append(view.Rows, browserLayoutRow)
	}
	return view, nil
}

// Apply implements gardenapi.Backend.
func (b *Backend) Apply(ctx context.Context, plan gardenapi.PlanView) (gardenapi.OperationView, error) {
	gen := b.gen.Add(1)
	fail := func(err error) (gardenapi.OperationView, error) { return gardenapi.OperationView{}, apiError(err) }
	if plan.Request.Mode == gardenapi.ModeSkip {
		return fail(connect.ErrInvalidOperation)
	}
	if err := b.requireFresh(ctx); err != nil {
		return fail(err)
	}
	p, err := b.buildPlan(ctx, plan.Request)
	if err != nil {
		return fail(err)
	}
	if planID(p.target.GardenID, p.plan) != plan.PlanID {
		return fail(ErrStalePlan)
	}
	if !summarize(p.plan, p.owner, true).CanApply {
		return fail(ErrPlanConflict)
	}
	if connect.OperationActive(b.deps.Store, b.local) {
		return fail(connect.ErrOperationInProgress)
	}
	opID := "op-" + b.deps.NewID()
	assoc := connect.Association{
		LocalRepoID: b.local, GardenID: p.target.GardenID, RemoteFullName: p.target.FullName,
		Mode: p.plan.Mode, Access: p.conn.Access, Identity: p.identity,
	}
	if p.project != nil {
		assoc.Project = *p.project
	}
	// connect records progress on an existing association, so it is written first
	// and removed again if the operation never started.
	if err := b.putAssociation(ctx, assoc, mustBeAbsent); err != nil {
		return fail(err)
	}
	b.reassociate(p.target.GardenID)
	_, runErr := connect.Apply(ctx, b.deps.Store, b.local, p.target.Repository.ID, p.conn.Access, opID, p.plan, b.setupPorts(p.identity))
	return b.finish(ctx, gen, opID, p.target, p.owner, runErr, true)
}

// Resume implements gardenapi.Backend.
func (b *Backend) Resume(ctx context.Context, operationID string) (gardenapi.OperationView, error) {
	gen := b.gen.Add(1)
	fail := func(err error) (gardenapi.OperationView, error) { return gardenapi.OperationView{}, apiError(err) }
	_, a, err := b.association(ctx)
	switch {
	case err != nil:
		return fail(err)
	case a == nil || a.Mode == connect.Skip || a.ActiveOperationID != operationID:
		return fail(connect.ErrOperationNotFound)
	}
	b.invalidateAuth()
	target, _, err := b.resolve(ctx, gardenapi.TargetRef{RepoID: uint64(a.GardenID.Repo)})
	if err != nil {
		return fail(err)
	}
	id, err := b.identity(ctx)
	if err != nil {
		return fail(err)
	}
	if connect.OperationActive(b.deps.Store, b.local) {
		return fail(connect.ErrOperationInProgress)
	}
	_, runErr := connect.Resume(ctx, b.deps.Store, operationID, target.Repository.ID, b.setupPorts(id), b.local, target.Repository.Access)
	return b.finish(ctx, gen, operationID, target, ownerOf(target.FullName), runErr, false)
}

// finish reports an operation that connect ran, from the durable record rather
// than from the call's partial result.
func (b *Backend) finish(ctx context.Context, gen uint64, opID string, target connect.Target, owner string, runErr error, created bool) (gardenapi.OperationView, error) {
	// A cancelled caller must still be able to record the outcome.
	rec := context.WithoutCancel(ctx)
	op, opErr := b.deps.Store.Operation(rec, opID)
	if opErr != nil {
		// The operation never started. Do not remove the association when another
		// run owns this clone: it belongs to that run.
		if created && !errors.Is(runErr, connect.ErrOperationInProgress) {
			_ = b.removeAssociation(rec, func(cur *connect.Association) error {
				if cur == nil || cur.ActiveOperationID != "" {
					return ErrAlreadySetUp
				}
				return nil
			})
			b.reassociate(target.GardenID)
		}
		if runErr == nil {
			runErr = opErr
		}
		return gardenapi.OperationView{}, apiError(runErr)
	}
	_, a, err := b.association(rec)
	if err != nil {
		return gardenapi.OperationView{}, apiError(err)
	}
	complete := true
	for _, st := range op.Steps {
		if st.Status != connect.Done {
			complete = false
		}
	}
	if complete && a != nil && a.Project.ID == "" {
		// Create learns its Project only now; remember it for the Board.
		for _, st := range op.Steps {
			if st.Kind == connect.CreateProject && st.ProjectID != "" {
				a.Project = connect.ProjectRef{ID: st.ProjectID}
				if perr := b.putAssociation(rec, *a, mustExist); perr != nil {
					return gardenapi.OperationView{}, apiError(perr)
				}
			}
		}
	}
	d, err := b.derive(rec, a, &live{access: target.Repository.Access})
	if err != nil {
		return gardenapi.OperationView{}, apiError(err)
	}
	var scope connect.ErrScopeMissing
	scopeMissing := errors.As(runErr, &scope)
	return b.operationView(b.stamp(gen, target.Repository.ID), op, d, owner, scopeMissing, runErr), nil
}

func mustBeAbsent(cur *connect.Association) error {
	if cur == nil {
		return nil
	}
	if cur.Mode == connect.Skip {
		return ErrSkipped
	}
	return ErrAlreadySetUp
}

func mustExist(cur *connect.Association) error {
	if cur == nil {
		return connect.ErrRevision
	}
	return nil
}

func (b *Backend) operationView(stamp gardenapi.Stamp, op connect.Operation, d derived, owner string, scopeMissing bool, runErr error) gardenapi.OperationView {
	v := gardenapi.OperationView{
		Stamp: stamp, ID: op.ID, State: d.state, Total: len(op.Steps),
		Running: d.state == gardenapi.StateOperationInProgress,
		Steps:   operationSteps(op, owner, scopeMissing),
	}
	for _, st := range op.Steps {
		if st.Status == connect.Done {
			v.Done++
		}
	}
	if runErr == nil {
		for _, st := range v.Steps {
			if st.Status != gardenapi.StepFailed {
				continue
			}
			switch st.ErrorCode {
			case gardenapi.StepGraphQL:
				runErr = garden.ErrGraphQL
			case gardenapi.StepScopeMissing:
				runErr = connect.ErrScopeMissing{Scope: "project"}
			case gardenapi.StepValidation:
				runErr = connect.ErrGitHubValidation
			}
			break
		}
	}
	for _, s := range op.Plan.Steps {
		if s.Kind == connect.ManualStep {
			v.AwaitingManual = append(v.AwaitingManual, s.Instructions)
		}
	}
	if runErr != nil {
		var ae *gardenapi.Error
		if e := apiError(runErr); errors.As(e, &ae) {
			v.Err = ae
		}
	}
	return v
}

// Skip implements gardenapi.Backend.
func (b *Backend) Skip(ctx context.Context, ref gardenapi.TargetRef) (gardenapi.StatusView, error) {
	_, a, err := b.association(ctx)
	if err != nil {
		return gardenapi.StatusView{}, apiError(err)
	}
	if a != nil && a.Mode == connect.Skip {
		return b.Status(ctx)
	}
	if connect.OperationActive(b.deps.Store, b.local) {
		return gardenapi.StatusView{}, apiError(connect.ErrOperationInProgress)
	}
	if a != nil && a.ActiveOperationID == "" {
		return gardenapi.StatusView{}, apiError(ErrAlreadySetUp) // connected: Skip is for setup that is not done
	}
	skip := connect.Association{LocalRepoID: b.local, Mode: connect.Skip}
	if a != nil {
		// The interrupted operation's record stays in the store but is no longer linked.
		skip.GardenID, skip.RemoteFullName, skip.Access, skip.Identity = a.GardenID, a.RemoteFullName, a.Access, a.Identity
	} else {
		target, _, rerr := b.resolve(ctx, ref)
		if rerr != nil {
			return gardenapi.StatusView{}, apiError(rerr)
		}
		skip.GardenID, skip.RemoteFullName, skip.Access = target.GardenID, target.FullName, target.Repository.Access
	}
	if err := b.putAssociation(ctx, skip, func(cur *connect.Association) error {
		// Only the state this call decided on may be replaced.
		if (cur == nil) != (a == nil) || cur != nil && cur.ActiveOperationID != a.ActiveOperationID {
			return connect.ErrRevision
		}
		return nil
	}); err != nil {
		return gardenapi.StatusView{}, apiError(err)
	}
	b.reassociate(skip.GardenID)
	return b.Status(ctx)
}

// Unskip implements gardenapi.Backend.
func (b *Backend) Unskip(ctx context.Context) (gardenapi.StatusView, error) {
	_, a, err := b.association(ctx)
	if err != nil {
		return gardenapi.StatusView{}, apiError(err)
	}
	if a == nil || a.Mode != connect.Skip {
		return b.Status(ctx)
	}
	if err := b.removeAssociation(ctx, func(cur *connect.Association) error {
		if cur == nil || cur.Mode != connect.Skip {
			return connect.ErrRevision
		}
		return nil
	}); err != nil {
		return gardenapi.StatusView{}, apiError(err)
	}
	b.reassociate(a.GardenID)
	return b.Status(ctx)
}
