package gardenbackend

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/launch"
)

type localLaunchStore struct {
	launch.Store
	attempts []launch.Attempt
	err      error
	local    garden.LocalRepoID
}

func (s *localLaunchStore) List(_ context.Context, local garden.LocalRepoID, issue garden.IssueID) ([]launch.Attempt, error) {
	if local != s.local || issue != 0 {
		return nil, errors.New("wrong launch scope")
	}
	return s.attempts, s.err
}
func TestLocalWorkImplementationStates(t *testing.T) {
	for _, state := range []launch.State{launch.StatePreparing, launch.StateStarting, launch.StateStartUnknown, launch.StateRecoverable, launch.StateRunning, launch.StateAwaitingReview, launch.StateFailed, launch.StateStopped} {
		t.Run(string(state), func(t *testing.T) {
			h, _, id, _, _ := plannerHarness(t)
			now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
			h.b.deps.LaunchStore = &localLaunchStore{local: h.b.local, attempts: []launch.Attempt{
				{ID: "old", Issue: garden.IssueID(id), State: launch.StateFailed, RequestedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Minute)},
				{ID: "new", Issue: garden.IssueID(id), State: state, RequestedAt: now, UpdatedAt: now},
				{ID: "cleaned", Issue: garden.IssueID(id), State: launch.StateCleaned, RequestedAt: now.Add(time.Minute)},
				{ID: "other", Issue: garden.IssueID(id + 1), State: launch.StateStopped, RequestedAt: now.Add(2 * time.Minute), UpdatedAt: now},
			}}
			v, err := h.b.LocalWork(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			got := v.Issues[id].Implementation
			live := state == launch.StatePreparing || state == launch.StateStarting || state == launch.StateStartUnknown || state == launch.StateRecoverable || state == launch.StateRunning
			if got == nil || got.AttemptID != "new" || got.State != string(state) || got.Live != live || !got.Updated.Equal(now) || got.Activity != nil {
				t.Fatalf("summary %+v", got)
			}
			if v.Issues[id+1].Implementation == nil {
				t.Fatal("other Issue lost")
			}
		})
	}
}
func TestLocalWorkOptionalLaunchAndListFailure(t *testing.T) {
	h, _, id, _, _ := plannerHarness(t)
	v, err := h.b.LocalWork(context.Background())
	if err != nil || v.Issues[id].Implementation != nil {
		t.Fatalf("optional launch: %+v %v", v, err)
	}
	h.b.deps.LaunchStore = &localLaunchStore{local: h.b.local, err: errors.New("launch list failed")}
	if _, err = h.b.LocalWork(context.Background()); err == nil {
		t.Fatal("launch failure cleared last good data")
	}
}

func TestLocalWorkImplementationWithoutPlanner(t *testing.T) {
	h, _, id, _, _ := plannerHarness(t)
	h.b.deps.Plans, h.b.deps.Checkout, h.b.deps.Agents = nil, nil, nil
	h.b.deps.LaunchStore = &localLaunchStore{local: h.b.local, attempts: []launch.Attempt{{ID: "impl", Issue: garden.IssueID(id), State: launch.StateRunning}}}
	v, err := h.b.LocalWork(context.Background())
	if err != nil || v.Issues[id].Implementation == nil || v.Issues[id].Planner != nil {
		t.Fatalf("implementation without planner: %+v %v", v, err)
	}
}

func TestLocalWorkUnavailableWithoutAnySource(t *testing.T) {
	h, _, _, _, _ := plannerHarness(t)
	h.b.deps.Plans, h.b.deps.Checkout, h.b.deps.Agents, h.b.deps.LaunchStore = nil, nil, nil, nil
	if _, err := h.b.LocalWork(context.Background()); err != unavailablePlanner {
		t.Fatalf("no sources: err = %v, want unavailablePlanner", err)
	}
}
