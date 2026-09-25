package gym

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agym"
)

type mockClient struct {
	agym.Client
	runResp   *agym.Run
	runErr    error
	runsResp  []agym.Run
	runsErr   error
	stopErr   error
	startResp *agym.Run
	startErr  error
	infoResp  *agym.InfoData
	infoErr   error
}

func (m *mockClient) Info(ctx context.Context) (*agym.InfoData, error) {
	return m.infoResp, m.infoErr
}

func (m *mockClient) GetRun(ctx context.Context, runID string) (*agym.Run, error) {
	return m.runResp, m.runErr
}

func (m *mockClient) ListRunsByWorkspace(ctx context.Context, wsKey string) ([]agym.Run, error) {
	return m.runsResp, m.runsErr
}

func (m *mockClient) ListRunsByRequest(ctx context.Context, clientID, requestID string) ([]agym.Run, error) {
	return m.runsResp, m.runsErr
}

func (m *mockClient) StartRun(ctx context.Context, req *agym.StartRequest) (*agym.Run, error) {
	return m.startResp, m.startErr
}

func (m *mockClient) StopRun(ctx context.Context, runID string) error {
	return m.stopErr
}

func (m *mockClient) GenerateBranchName(ctx context.Context, profile, task string) (string, error) {
	return "feat/mock-branch", nil
}

func TestCheckPruneAllowed(t *testing.T) {
	repoDir := initTestGitRepo(t)
	store, err := NewStore(repoDir)
	if err != nil {
		t.Fatal(err)
	}

	ws, err := store.ResolveWorkspaceIdentity(repoDir)
	if err != nil {
		t.Fatal(err)
	}

	// 1. No binding, no client: allowed
	if err := CheckPruneAllowed(context.Background(), store, nil, repoDir); err != nil {
		t.Errorf("expected nil for no binding, got %v", err)
	}

	// 2. Pending binding: refuses prune
	pending := &RunBinding{
		SchemaVersion: CurrentSchemaVersion,
		Workspace:     *ws,
		RequestID:     "req-pending",
		CreatedAt:     time.Now(),
		Submission:    SubmissionPending,
	}
	_ = store.SaveBinding(pending)
	err = CheckPruneAllowed(context.Background(), store, nil, repoDir)
	if !errors.Is(err, ErrWorktreeHasActiveAgent) {
		t.Errorf("expected ErrWorktreeHasActiveAgent for pending run, got %v", err)
	}

	// 3. Active run (running): refuses prune
	mock := &mockClient{
		runResp: &agym.Run{
			RunID:  "run-123",
			Status: agym.RunStateRunning,
		},
	}
	active := &RunBinding{
		SchemaVersion: CurrentSchemaVersion,
		Workspace:     *ws,
		RequestID:     "req-active",
		RunID:         "run-123",
		CreatedAt:     time.Now(),
		Submission:    SubmissionAcknowledged,
	}
	_ = store.SaveBinding(active)
	err = CheckPruneAllowed(context.Background(), store, mock, repoDir)
	if !errors.Is(err, ErrWorktreeHasActiveAgent) {
		t.Errorf("expected ErrWorktreeHasActiveAgent for active run, got %v", err)
	}

	// 4. Uncertainty (client fails): fails closed
	mockError := &mockClient{
		runErr: errors.New("connection failed"),
	}
	err = CheckPruneAllowed(context.Background(), store, mockError, repoDir)
	if !errors.Is(err, ErrAgentUncertain) {
		t.Errorf("expected ErrAgentUncertain on client failure, got %v", err)
	}

	// 5. Terminal run (succeeded): allowed
	mockTerminal := &mockClient{
		runResp: &agym.Run{
			RunID:  "run-123",
			Status: agym.RunStateSucceeded,
		},
	}
	err = CheckPruneAllowed(context.Background(), store, mockTerminal, repoDir)
	if err != nil {
		t.Errorf("expected nil for terminal run, got %v", err)
	}
}
