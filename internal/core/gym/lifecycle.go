package gym

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agym"
)

var (
	ErrWorktreeHasActiveAgent = errors.New("cannot prune worktree: an AI agent is currently active")
	ErrAgentUncertain         = errors.New("cannot prune worktree: agent status uncertain (failing closed)")
)

// CheckPruneAllowed verifies that no active or uncertain agent run is associated
// with worktreePath before it is deleted or merged.
func CheckPruneAllowed(ctx context.Context, store *Store, client agym.Client, worktreePath string) error {
	ws, err := store.ResolveWorkspaceIdentity(worktreePath)
	if err != nil {
		// Not a tracked worktree or git error; allow deletion logic to handle git status
		return nil
	}

	binding, err := store.GetBinding(ws.WorktreeID)
	if err != nil {
		return fmt.Errorf("%w: failed to read local binding: %v", ErrAgentUncertain, err)
	}

	// If no binding exists locally, check remote workspace runs if client is available
	if binding == nil {
		if client == nil {
			return nil
		}
		probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		runs, err := client.ListRunsByWorkspace(probeCtx, ws.WorktreeID)
		if err != nil {
			return fmt.Errorf("%w: failed to check remote workspace runs: %v", ErrAgentUncertain, err)
		}
		for _, r := range runs {
			if !agym.IsTerminal(r.Status) {
				return fmt.Errorf("%w: active run %s (status: %s)", ErrWorktreeHasActiveAgent, r.RunID, r.Status)
			}
		}
		return nil
	}

	// Pending submission must block prune
	if binding.Submission == SubmissionPending || (binding.RunID == "" && binding.RequestID != "") {
		return fmt.Errorf("%w: run submission is pending (request %s)", ErrWorktreeHasActiveAgent, binding.RequestID)
	}

	// If we have a run ID, query its live status
	if binding.RunID != "" {
		if client == nil {
			return fmt.Errorf("%w: agym client unavailable to verify run %s", ErrAgentUncertain, binding.RunID)
		}

		probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if binding.AGYMInstanceID != "" {
			info, err := client.Info(probeCtx)
			if err != nil || info == nil || info.InstanceID != binding.AGYMInstanceID {
				return fmt.Errorf("%w: AGYM installation identity changed", ErrAgentUncertain)
			}
		}

		run, err := client.GetRun(probeCtx, binding.RunID)
		if err != nil {
			return fmt.Errorf("%w: failed to query run %s: %v", ErrAgentUncertain, binding.RunID, err)
		}
		if run == nil || run.RunID != binding.RunID || (run.RequestID != "" && run.RequestID != binding.RequestID) {
			return fmt.Errorf("%w: run identity mismatch for %s", ErrAgentUncertain, binding.RunID)
		}

		if !agym.IsTerminal(run.Status) {
			return fmt.Errorf("%w: run %s is %s", ErrWorktreeHasActiveAgent, run.RunID, run.Status)
		}

		// Terminal run: archive binding
		if err := store.ArchiveBinding(binding); err != nil {
			return fmt.Errorf("%w: failed to archive terminal run: %v", ErrAgentUncertain, err)
		}
	}

	return nil
}
