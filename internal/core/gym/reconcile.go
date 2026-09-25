package gym

import (
	"context"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agym"
)

// ReconcileBinding checks pending submissions and refreshes the binding against AGYM.
func ReconcileBinding(ctx context.Context, store *Store, client agym.Client, clientID string, binding *RunBinding) (*RunBinding, *agym.Run, error) {
	if binding == nil || client == nil {
		return binding, nil, nil
	}

	// Case 1: Pending submission
	if binding.Submission == SubmissionPending && binding.RunID == "" {
		runs, err := client.ListRunsByRequest(ctx, clientID, binding.RequestID)
		if err == nil && len(runs) > 0 {
			matching := runs[0]
			binding.RunID = matching.RunID
			binding.LeaseID = matching.LeaseID
			binding.Submission = SubmissionAcknowledged
			_ = store.SaveBinding(binding)
			return binding, &matching, nil
		}
		// If older than 30s and still not found, leave as pending or mark rejected
		if time.Since(binding.CreatedAt) > 30*time.Second {
			// Leave as pending so user can retry or cancel
			return binding, nil, nil
		}
		return binding, nil, nil
	}

	// Case 2: Acknowledged run
	if binding.RunID != "" {
		run, err := client.GetRun(ctx, binding.RunID)
		if err != nil {
			return binding, nil, err
		}
		if agym.IsTerminal(run.Status) {
			_ = store.ArchiveBinding(binding)
		}
		return binding, run, nil
	}

	return binding, nil, nil
}
