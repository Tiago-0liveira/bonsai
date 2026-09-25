package gym

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agym"
)

func TestReconcileBinding(t *testing.T) {
	repoDir := initTestGitRepo(t)
	store, err := NewStore(repoDir)
	if err != nil {
		t.Fatal(err)
	}

	ws, err := store.ResolveWorkspaceIdentity(repoDir)
	if err != nil {
		t.Fatal(err)
	}

	mock := &mockClient{
		runResp: &agym.Run{
			RunID: "run-reconciled", RequestID: "req-pending-1", Status: agym.RunStateSucceeded,
		},
	}

	binding := &RunBinding{
		SchemaVersion: CurrentSchemaVersion,
		Workspace:     *ws,
		RequestID:     "req-pending-1",
		CreatedAt:     time.Now(),
		Submission:    SubmissionPending,
	}
	_ = store.SaveBinding(binding)
	mock.runsResp = []agym.Run{{
		RunID: "run-reconciled", LeaseID: "lease-reconciled",
		RequestID: "req-pending-1", ClientID: "client-1",
		Workspace: agym.WorkspaceIdentityPayload{Key: ws.WorktreeID}, Status: agym.RunStateRunning,
	}}

	// Step 1: Reconcile pending submission to acknowledged run
	updated, run, err := ReconcileBinding(context.Background(), store, mock, "client-1", binding)
	if err != nil {
		t.Fatalf("ReconcileBinding error: %v", err)
	}
	if updated.Submission != SubmissionAcknowledged {
		t.Errorf("submission = %s, want acknowledged", updated.Submission)
	}
	if updated.RunID != "run-reconciled" {
		t.Errorf("run_id = %s, want run-reconciled", updated.RunID)
	}
	if run == nil || run.RunID != "run-reconciled" {
		t.Fatalf("unexpected run: %+v", run)
	}

	// Step 2: Reconcile acknowledged terminal run -> archives binding
	updated2, run2, err := ReconcileBinding(context.Background(), store, mock, "client-1", updated)
	if err != nil {
		t.Fatalf("ReconcileBinding second run error: %v", err)
	}
	if run2.Status != agym.RunStateSucceeded {
		t.Errorf("run status = %s, want succeeded", run2.Status)
	}
	_ = updated2
}

func TestPendingReconciliationKeepsRequestOnLookupFailure(t *testing.T) {
	repoDir := initTestGitRepo(t)
	store, err := NewStore(repoDir)
	if err != nil {
		t.Fatal(err)
	}
	ws, err := store.ResolveWorkspaceIdentity(repoDir)
	if err != nil {
		t.Fatal(err)
	}
	binding := &RunBinding{SchemaVersion: CurrentSchemaVersion, Workspace: *ws,
		RequestID: "req-1", CreatedAt: time.Now(), Submission: SubmissionPending}
	if err := store.SaveBinding(binding); err != nil {
		t.Fatal(err)
	}
	client := &mockClient{runsErr: errors.New("timeout")}
	_, _, err = ReconcileBinding(context.Background(), store, client, "client-1", binding)
	if err == nil {
		t.Fatal("expected lookup error")
	}
	got, err := store.GetBinding(ws.WorktreeID)
	if err != nil || got == nil || got.Submission != SubmissionPending || got.RequestID != "req-1" {
		t.Fatalf("pending request lost: binding=%+v err=%v", got, err)
	}
}
