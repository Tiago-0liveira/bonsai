package gym

import (
	"context"
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
