package gardenbackend

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/connect"
	gardensync "github.com/Tiago-0liveira/bonsai/internal/core/garden/sync"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
)

// live is what GitHub said just now; nil in derive means it was unreachable.
type live struct {
	access connect.Access
}

// derived is the association state plus the operation it came from.
type derived struct {
	state gardenapi.State
	view  gardensync.AssociationView
	op    *connect.Operation
}

// derive maps the stored association, its operation and the live access to the
// sync package's association states. It adds no state of its own.
func (b *Backend) derive(ctx context.Context, a *connect.Association, lv *live) (derived, error) {
	set := func(s gardensync.AssociationState) derived {
		return derived{state: gardenapi.State(s), view: gardensync.AssociationView{State: s}}
	}
	liveReadOnly := lv != nil && lv.access < connect.AccessWrite
	switch {
	case a == nil && liveReadOnly:
		return set(gardensync.AssociationReadOnly), nil
	case a == nil:
		return set(gardensync.AssociationNone), nil
	case a.Mode == connect.Skip:
		return set(gardensync.AssociationSkipped), nil
	case a.Access < connect.AccessWrite || liveReadOnly:
		return set(gardensync.AssociationReadOnly), nil
	case a.ActiveOperationID == "":
		return set(gardensync.AssociationConnected), nil
	}
	op, err := b.deps.Store.Operation(ctx, a.ActiveOperationID)
	if errors.Is(err, connect.ErrOperationNotFound) {
		// The record vanished: nothing to resume, but setup is not complete either.
		d := set(gardensync.AssociationPartialSetup)
		d.view.OperationID = a.ActiveOperationID
		return d, nil
	}
	if err != nil {
		return derived{}, err
	}
	d := derived{op: &op, view: gardensync.AssociationView{OperationID: op.ID, OperationTotal: len(op.Steps)}}
	open := false
	for _, st := range op.Steps {
		switch st.Status {
		case connect.Done:
			d.view.OperationDone++
		case connect.Failed, connect.Pending:
			open = true
		}
	}
	switch {
	case connect.OperationActive(b.deps.Store, b.local):
		d.view.State = gardensync.AssociationOperationInProgress
	case open:
		d.view.State = gardensync.AssociationPartialSetup
	default:
		d.view.State = gardensync.AssociationAwaitingManual
	}
	d.state = gardenapi.State(d.view.State)
	return d, nil
}

// Status implements gardenapi.Backend.
func (b *Backend) Status(ctx context.Context) (gardenapi.StatusView, error) {
	gen := b.gen.Add(1)
	_, a, err := b.association(ctx)
	if err != nil {
		return gardenapi.StatusView{}, apiError(err)
	}
	b.invalidateAuth()
	var v gardenapi.StatusView
	target, _, rerr := b.resolve(ctx, gardenapi.TargetRef{})
	var amb *connect.AmbiguousError
	var lv *live
	switch {
	case rerr == nil:
		lv = &live{access: target.Repository.Access}
		if id, ierr := b.identity(ctx); ierr == nil {
			v.Identity = gardenapi.Identity{Login: id.Login, ID: id.ID}
		}
	case errors.As(rerr, &amb):
		v.Ambiguous = true
	default:
		if e := apiError(rerr); e != nil {
			var ae *gardenapi.Error
			if errors.As(e, &ae) {
				v.RefreshError = ae
			}
		}
	}
	if v.Identity.Login == "" && a != nil {
		v.Identity = gardenapi.Identity{Login: a.Identity.Login, ID: a.Identity.ID}
	}
	if a != nil {
		v.Ambiguous = false // a recorded Garden needs no choice
	}
	v.Caps.Refresh = lv != nil

	// The recorded Garden wins over a live target that moved.
	switch {
	case a != nil && (lv == nil || target.GardenID == a.GardenID || a.Mode == connect.Skip):
		v.Target = gardenapi.Target{RepoID: uint64(a.GardenID.Repo), FullName: a.RemoteFullName, Access: accessOf(a.Access), Kind: gardenapi.KindRepository}
		if lv != nil {
			v.Target = targetView(target)
		}
	case a != nil:
		v.RemoteChanged = true
		v.Target = gardenapi.Target{RepoID: uint64(a.GardenID.Repo), FullName: a.RemoteFullName, Access: accessOf(a.Access), Kind: gardenapi.KindRepository}
	case lv != nil:
		v.Target = targetView(target)
	}
	if v.RemoteChanged {
		lv = nil // the live repository is not the one this clone's association is about
	}
	if lv != nil {
		v.Access = accessOf(lv.access)
		v.ProjectScope = presence(target.Repository.ProjectScopeKnown, target.Repository.ProjectScope)
	} else if a != nil {
		v.Access = accessOf(a.Access)
	}
	if a != nil {
		v.ProjectID = a.Project.ID
	}

	d, err := b.derive(ctx, a, lv)
	if err != nil {
		return gardenapi.StatusView{}, apiError(err)
	}
	v.State = d.state

	repo := garden.GardenID{Repo: garden.RepoID(v.Target.RepoID)}
	b.observe(repo)
	v.Stamp = b.stamp(gen, repo.Repo)
	b.fillSync(&v, d, repo)

	if d.op != nil {
		scopeMissing := v.ProjectScope == gardenapi.PresenceAbsent
		ov := b.operationView(v.Stamp, *d.op, d, ownerOf(v.Target.FullName), scopeMissing, nil)
		v.Operation = &ov
	}
	if a == nil && d.state == gardenapi.StateReadOnly && lv != nil {
		v.RemoteGarden = b.remoteGarden(ctx, target.Repository.ID)
	}
	v.Caps = capabilities(v, lv != nil)
	return v, nil
}

// fillSync projects the status through the sync package. Before any read
// exists the snapshot is neutral, so "connected" does not read as "partial".
func (b *Backend) fillSync(v *gardenapi.StatusView, d derived, repo garden.GardenID) {
	snap := gardensync.Snapshot{}
	if r := b.Reads(); r != nil {
		snap = r.Read()
	}
	if snap.Generation == 0 {
		snap.Complete, snap.Source = true, gardensync.SourceLive
	}
	cs := gardensync.Status(d.view, snap)
	v.Sync, v.IssueCount, v.LastSync = cs.State, cs.IssueCount, cs.LastSync
	if snap.Generation > 0 {
		v.LastSync = snap.SuccessfulAt
	}
}

func capabilities(v gardenapi.StatusView, refreshed bool) gardenapi.Capabilities {
	c := gardenapi.Capabilities{Refresh: refreshed}
	switch v.State {
	case gardenapi.StateNone:
		c.Setup = v.Access.CanSetup() && v.Target.RepoID != 0 && !v.Ambiguous && refreshed
		c.Skip = true
	case gardenapi.StateSkipped:
		c.Unskip = true
	case gardenapi.StateReadOnly:
		c.Read = v.Access >= gardenapi.AccessRead
	case gardenapi.StateConnected:
		c.Read = true
	case gardenapi.StatePartialSetup:
		c.Read = true
		c.Resume = v.Access.CanSetup() && refreshed && !v.RemoteChanged
		c.Skip = true
	case gardenapi.StateAwaitingManual:
		c.Read = true
		c.Skip = true
	case gardenapi.StateOperationInProgress:
		c.Read = true
	}
	return c
}

func targetView(t connect.Target) gardenapi.Target {
	kind := gardenapi.KindRepository
	parent := ""
	if t.Repository.Fork {
		kind, parent = gardenapi.KindFork, t.Repository.ParentFullName
	}
	return gardenapi.Target{RepoID: uint64(t.Repository.ID), FullName: t.FullName, Aliases: append([]string(nil), t.RemoteAliases...), Access: accessOf(t.Repository.Access), Kind: kind, ParentFullName: parent}
}

func candidateView(c connect.Candidate) gardenapi.Target {
	t := gardenapi.Target{RepoID: uint64(c.Repository.ID), FullName: c.Repository.FullName, Aliases: append([]string(nil), c.Aliases...), Access: accessOf(c.Repository.Access), Kind: gardenapi.KindRepository}
	switch {
	case c.Upstream:
		t.Kind = gardenapi.KindUpstream
	case c.Repository.Fork:
		t.Kind, t.ParentFullName = gardenapi.KindFork, c.Repository.ParentFullName
	}
	return t
}

// remoteGarden reports whether the managed label already exists, which is how
// a read-only user learns that a collaborator set the Garden up.
func (b *Backend) remoteGarden(ctx context.Context, repo garden.RepoID) gardenapi.Presence {
	if b.deps.Ports.Labels == nil {
		return gardenapi.PresenceUnknown
	}
	labels, err := b.deps.Ports.Labels.ListLabels(ctx, repo)
	if err != nil {
		return gardenapi.PresenceUnknown
	}
	for _, l := range labels {
		if strings.EqualFold(l.Name, garden.ManagedLabel) {
			return gardenapi.PresencePresent
		}
	}
	return gardenapi.PresenceAbsent
}

// Candidates implements gardenapi.Backend.
func (b *Backend) Candidates(ctx context.Context) (gardenapi.CandidatesView, error) {
	gen := b.gen.Add(1)
	target, notes, err := b.resolve(ctx, gardenapi.TargetRef{})
	var amb *connect.AmbiguousError
	switch {
	case err == nil:
		b.observe(target.GardenID)
		return gardenapi.CandidatesView{Stamp: b.stamp(gen, target.Repository.ID), Candidates: []gardenapi.Target{targetView(target)}, Notes: notes}, nil
	case errors.As(err, &amb):
		out := make([]gardenapi.Target, 0, len(amb.Candidates))
		for _, c := range amb.Candidates {
			out = append(out, candidateView(c))
		}
		// Stable order for the UI: by full name, then ID.
		sort.SliceStable(out, func(i, j int) bool {
			return out[i].FullName < out[j].FullName || out[i].FullName == out[j].FullName && out[i].RepoID < out[j].RepoID
		})
		return gardenapi.CandidatesView{Stamp: b.stamp(gen, 0), Ambiguous: true, Candidates: out, Notes: notes}, nil
	}
	return gardenapi.CandidatesView{}, apiError(err)
}

// Projects implements gardenapi.Backend.
func (b *Backend) Projects(ctx context.Context, ref gardenapi.TargetRef) (gardenapi.ProjectsView, error) {
	gen := b.gen.Add(1)
	target, _, err := b.resolve(ctx, ref)
	if err != nil {
		return gardenapi.ProjectsView{}, apiError(err)
	}
	id, err := b.identity(ctx)
	if err != nil {
		return gardenapi.ProjectsView{}, apiError(err)
	}
	reader, err := b.connection(target, id).Reader()
	if err != nil {
		return gardenapi.ProjectsView{}, apiError(err)
	}
	if reader == nil {
		return gardenapi.ProjectsView{}, apiError(connect.ErrProjectNotFound)
	}
	projects, err := reader.ListProjects(ctx, target.Repository.ID)
	if err != nil {
		return gardenapi.ProjectsView{}, apiError(err)
	}
	b.observe(target.GardenID)
	out := gardenapi.ProjectsView{Stamp: b.stamp(gen, target.Repository.ID), Owner: ownerOf(target.FullName)}
	for _, p := range projects {
		item := gardenapi.Project{ID: p.ID, Number: p.Number, Title: p.Name, Conflict: strings.EqualFold(p.Name, DefaultProjectName)}
		if hasProjectField(p, StageField) {
			item.Stage = gardenapi.FieldPresent
		}
		if hasProjectField(p, TypeField) {
			item.Type = gardenapi.FieldPresent
		}
		out.Projects = append(out.Projects, item)
	}
	return out, nil
}
