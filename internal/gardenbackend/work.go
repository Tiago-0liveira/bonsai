package gardenbackend

import (
	"context"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/connect"
	gardensync "github.com/Tiago-0liveira/bonsai/internal/core/garden/sync"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
)

// Work shares the backend generation and association epoch with Setup. Only
// sync.Store decides whether a partial or late collection may replace data.
func (b *Backend) Work(ctx context.Context) (gardenapi.WorkView, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	gen := b.gen.Add(1)
	b.invalidateAuth()
	_, a, err := b.association(ctx)
	if err != nil {
		return gardenapi.WorkView{}, apiError(err)
	}
	if a != nil && a.Mode == connect.Skip {
		return gardenapi.WorkView{}, &gardenapi.Error{Code: gardenapi.CodeSkipped, Message: "Garden is off for this clone."}
	}
	ref := gardenapi.TargetRef{}
	if a != nil {
		ref.RepoID = uint64(a.GardenID.Repo)
	}
	target, _, err := b.resolve(ctx, ref)
	repo := target.GardenID
	if a != nil {
		repo = a.GardenID
	}
	if repo.Repo == 0 {
		if err != nil {
			return gardenapi.WorkView{}, apiError(err)
		}
		return gardenapi.WorkView{}, &gardenapi.Error{Code: gardenapi.CodeFailed, Message: "Garden repository identity is unavailable."}
	}
	b.observe(repo)
	store := b.Reads()
	stamp := b.stamp(gen, repo.Repo)
	request := gardensync.Request{GardenID: repo, LocalRepoID: b.local, Generation: gen, Epoch: stamp.Epoch, Issues: b.deps.Issues, Projects: b.deps.Ports.Projects, Limits: gardensync.Limits{PageCap: 20, ItemCap: 1000, CallTimeout: 10 * time.Second}, StageField: StageField, TypeField: TypeField}
	if a != nil {
		request.ProjectID = a.Project.ID
	}
	if err == nil {
		var id connect.GitHubIdentity
		id, err = b.identity(ctx)
		if err == nil && id.ID == "" {
			err = connect.ErrAuthRequired
		}
		if err == nil {
			request.ApprovalPolicy = garden.ApprovalPolicy{AllowedActorIDs: []string{id.ID}}
		}
	}
	var snap gardensync.Snapshot
	if err != nil || target.GardenID != repo || target.Repository.Access < connect.AccessRead || b.deps.Issues == nil {
		code := "read-failed"
		if target.Repository.Access < connect.AccessRead && err == nil {
			code = "permission-denied"
		}
		if err != nil {
			if e, ok := apiError(err).(*gardenapi.Error); ok {
				code = string(e.Code)
			}
		}
		snap = gardensync.Snapshot{GardenID: repo, LocalRepoID: b.local, Generation: gen, Epoch: stamp.Epoch, FetchedAt: time.Now().UTC(), Problems: []gardensync.Problem{{Code: code, Message: "Garden could not refresh; retained last good data."}}}
	} else {
		snap = gardensync.Load(ctx, request)
	}
	if err := store.Apply(snap); err != nil {
		return gardenapi.WorkView{}, apiError(err)
	}
	return gardenapi.WorkView{Stamp: stamp, Snapshot: store.Read(), PendingCaptures: b.pendingCaptures(ctx, gen, repo.Repo)}, nil
}
