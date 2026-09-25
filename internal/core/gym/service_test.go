package gym

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agym"
)

func TestServiceStartAndGetAgentView(t *testing.T) {
	repoDir := initTestGitRepo(t)
	mock := &mockClient{
		startResp: &agym.Run{
			RunID:           "run-new",
			SelectedProfile: "personal",
			Status:          agym.RunStateStarting,
			CreatedAt:       time.Now(),
		},
		runResp: &agym.Run{
			RunID:           "run-new",
			SelectedProfile: "personal",
			Status:          agym.RunStateRunning,
			CreatedAt:       time.Now(),
		},
	}

	svc, err := NewService(repoDir, mock)
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}

	run, err := svc.Start(context.Background(), repoDir, "personal", "My test task")
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	if run.RunID != "run-new" {
		t.Errorf("runID = %q, want run-new", run.RunID)
	}

	view, err := svc.GetAgentView(context.Background(), repoDir)
	if err != nil {
		t.Fatalf("GetAgentView failed: %v", err)
	}
	if view == nil || view.Run == nil || view.Run.RunID != "run-new" {
		t.Fatalf("unexpected view: %+v", view)
	}
}

func TestStartTransportFailureKeepsPendingRequest(t *testing.T) {
	repoDir := initTestGitRepo(t)
	svc, err := NewService(repoDir, &mockClient{startErr: errors.New("lost acknowledgement")})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Start(context.Background(), repoDir, "personal", "task"); err == nil {
		t.Fatal("expected transport failure")
	}
	ws, err := svc.store.ResolveWorkspaceIdentity(repoDir)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := svc.store.GetBinding(ws.WorktreeID)
	if err != nil || binding == nil || binding.Submission != SubmissionPending || binding.RequestID == "" {
		t.Fatalf("pending request not retained: %+v, %v", binding, err)
	}
}

func TestGetAgentViewRecoversMatchingRemoteRun(t *testing.T) {
	repoDir := initTestGitRepo(t)
	client := &mockClient{}
	svc, err := NewService(repoDir, client)
	if err != nil {
		t.Fatal(err)
	}
	ws, err := svc.store.ResolveWorkspaceIdentity(repoDir)
	if err != nil {
		t.Fatal(err)
	}
	candidate := agym.Run{RunID: "remote-1", RequestID: "req-1", Client: "bonsai",
		ClientID: svc.clientID, Workspace: agym.WorkspaceIdentityPayload{
			Key: ws.WorktreeID, Cwd: ws.Path, RepositoryKey: ws.RepositoryID},
		Status: agym.RunStateRunning, CreatedAt: time.Now()}
	client.runsResp = []agym.Run{candidate}
	client.runResp = &candidate
	view, err := svc.GetAgentView(context.Background(), repoDir)
	if err != nil || view == nil || view.Run == nil || view.Run.RunID != candidate.RunID {
		t.Fatalf("remote run not recovered: view=%+v err=%v", view, err)
	}
	binding, err := svc.store.GetBinding(ws.WorktreeID)
	if err != nil || binding == nil || binding.RunID != candidate.RunID {
		t.Fatalf("remote binding not saved: binding=%+v err=%v", binding, err)
	}
}

func TestGetAgentViewKeepsArchivedRunVisible(t *testing.T) {
	repoDir := initTestGitRepo(t)
	client := &mockClient{}
	svc, err := NewService(repoDir, client)
	if err != nil {
		t.Fatal(err)
	}
	ws, err := svc.store.ResolveWorkspaceIdentity(repoDir)
	if err != nil {
		t.Fatal(err)
	}
	binding := &RunBinding{SchemaVersion: CurrentSchemaVersion, Workspace: *ws,
		RequestID: "req-1", RunID: "run-1", CreatedAt: time.Now(), Submission: SubmissionAcknowledged}
	if err := svc.store.SaveBinding(binding); err != nil {
		t.Fatal(err)
	}
	client.runResp = &agym.Run{RunID: "run-1", RequestID: "req-1", Status: agym.RunStateSucceeded}
	for i := 0; i < 2; i++ {
		view, err := svc.GetAgentView(context.Background(), repoDir)
		if err != nil || view == nil || view.Run == nil || view.Run.Status != agym.RunStateSucceeded {
			t.Fatalf("archived run disappeared on view %d: view=%+v err=%v", i, view, err)
		}
	}
}

func TestServiceStop(t *testing.T) {
	repoDir := initTestGitRepo(t)
	mock := &mockClient{
		startResp: &agym.Run{
			RunID:  "run-stop",
			Status: agym.RunStateRunning,
		},
	}

	svc, err := NewService(repoDir, mock)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	_, err = svc.Start(context.Background(), repoDir, "personal", "task")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	if err := svc.Stop(context.Background(), repoDir, ""); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}
}

func TestStartAutoWorktree(t *testing.T) {
	repoDir := initTestGitRepo(t)
	mock := &mockClient{
		startResp: &agym.Run{
			RunID:           "run-auto-1",
			SelectedProfile: "personal",
			Status:          agym.RunStateStarting,
		},
	}

	svc, err := NewService(repoDir, mock)
	if err != nil {
		t.Fatalf("NewService failed: %v", err)
	}

	res, err := svc.StartAutoWorktree(context.Background(), "personal", "Fix parser error", "", nil)
	if err != nil {
		t.Fatalf("StartAutoWorktree failed: %v", err)
	}

	if res.Branch != "feat/mock-branch" {
		t.Errorf("Branch = %q, want feat/mock-branch", res.Branch)
	}
	if res.Run == nil || res.Run.RunID != "run-auto-1" {
		t.Errorf("Run = %+v, want run-auto-1", res.Run)
	}
	if res.WorktreePath == "" {
		t.Errorf("expected non-empty WorktreePath")
	}
}
