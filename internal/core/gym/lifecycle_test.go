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
	if m.runResp != nil && m.runResp.RequestID == "" && m.startResp != nil {
		m.runResp.RequestID = m.startResp.RequestID
	}
	return m.runResp, m.runErr
}

func (m *mockClient) ListRunsByWorkspace(ctx context.Context, wsKey string) ([]agym.Run, error) {
	return m.runsResp, m.runsErr
}

func (m *mockClient) ListRunsByRequest(ctx context.Context, clientID, requestID string) ([]agym.Run, error) {
	return m.runsResp, m.runsErr
}

func (m *mockClient) StartRun(ctx context.Context, req *agym.StartRequest) (*agym.Run, error) {
	if m.startResp != nil {
		m.startResp.RequestID = req.RequestID
		m.startResp.ClientID = req.ClientID
		m.startResp.Workspace = req.Workspace
	}
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

func TestPruneFailsClosedWithoutLocalBinding(t *testing.T) {
	repoDir := initTestGitRepo(t)
	store, err := NewStore(repoDir)
	if err != nil {
		t.Fatal(err)
	}
	client := &mockClient{runsErr: errors.New("agym unavailable")}
	if err := CheckPruneAllowed(context.Background(), store, client, repoDir); !errors.Is(err, ErrAgentUncertain) {
		t.Fatalf("prune with failed remote lookup = %v, want uncertainty", err)
	}
}

func TestPruneAllowedWhenAgymNotInstalledWithoutBinding(t *testing.T) {
	repoDir := initTestGitRepo(t)
	store, err := NewStore(repoDir)
	if err != nil {
		t.Fatal(err)
	}
	client := &mockClient{runsErr: agym.ErrNotInstalled}
	if err := CheckPruneAllowed(context.Background(), store, client, repoDir); err != nil {
		t.Fatalf("prune should be allowed when agym is not installed and no local binding exists: %v", err)
	}
}
