package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agym"
	"github.com/Tiago-0liveira/bonsai/internal/core/gym"
)

type cliMockClient struct {
	agym.Client
	infoResp     *agym.InfoData
	profilesResp []agym.Profile
	usageResp    []agym.Usage
	startResp    *agym.Run
	runResp      *agym.Run
	stopErr      error
}

func (m *cliMockClient) Info(ctx context.Context) (*agym.InfoData, error) {
	if m.infoResp != nil {
		return m.infoResp, nil
	}
	return &agym.InfoData{
		AGYMVersion:             "0.1.0",
		SupportedProtocolMajors: []int{1},
		Capabilities:            []string{agym.CapRunsDurable, agym.CapProfilesRead, agym.CapUsageRead},
	}, nil
}

func (m *cliMockClient) Profiles(ctx context.Context) ([]agym.Profile, error) {
	return m.profilesResp, nil
}

func (m *cliMockClient) Usage(ctx context.Context, profile string, refresh bool) ([]agym.Usage, error) {
	return m.usageResp, nil
}

func (m *cliMockClient) StartRun(ctx context.Context, req *agym.StartRequest) (*agym.Run, error) {
	return m.startResp, nil
}

func (m *cliMockClient) GetRun(ctx context.Context, runID string) (*agym.Run, error) {
	return m.runResp, nil
}

func (m *cliMockClient) StopRun(ctx context.Context, runID string) error {
	return m.stopErr
}

func (m *cliMockClient) GenerateBranchName(ctx context.Context, profile, task string) (string, error) {
	return "feat/cli-test-branch", nil
}

func TestGymStatus(t *testing.T) {
	mock := &cliMockClient{
		runResp: &agym.Run{
			RunID:           "run-123",
			Status:          agym.RunStateRunning,
			SelectedProfile: "personal",
			Task:            "test task",
		},
	}
	svc, err := gym.NewService("", mock)
	if err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	var errOut bytes.Buffer
	err = RunGym([]string{"status", "--run", "run-123", "--json"}, svc, &out, &errOut)
	if err != nil {
		t.Fatalf("RunGym status failed: %v", err)
	}

	var parsed struct {
		Available bool      `json:"available"`
		Run       *agym.Run `json:"run"`
	}
	if err := json.Unmarshal(out.Bytes(), &parsed); err != nil {
		t.Fatalf("failed to parse json: %v", err)
	}
	if !parsed.Available || parsed.Run == nil || parsed.Run.RunID != "run-123" {
		t.Fatalf("unexpected status output: %+v", parsed)
	}
}

func TestGymProfiles(t *testing.T) {
	mock := &cliMockClient{
		profilesResp: []agym.Profile{
			{ProfileID: "p1", Name: "personal", Readiness: "ready"},
			{ProfileID: "p2", Name: "work", Readiness: "busy", Reason: "in use"},
		},
	}
	svc, err := gym.NewService("", mock)
	if err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	var errOut bytes.Buffer
	err = RunGym([]string{"profiles"}, svc, &out, &errOut)
	if err != nil {
		t.Fatalf("RunGym profiles failed: %v", err)
	}
	if !strings.Contains(out.String(), "personal") || !strings.Contains(out.String(), "work") {
		t.Fatalf("unexpected profiles output: %s", out.String())
	}
}

func TestGymUsage(t *testing.T) {
	rem := 0.75
	mock := &cliMockClient{
		usageResp: []agym.Usage{
			{
				ProfileID: "personal",
				Windows: []agym.UsageWindow{
					{Name: "5h", Remaining: &rem},
				},
				ObservedAt: time.Now(),
			},
		},
	}
	svc, err := gym.NewService("", mock)
	if err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	var errOut bytes.Buffer
	err = RunGym([]string{"usage", "--json"}, svc, &out, &errOut)
	if err != nil {
		t.Fatalf("RunGym usage failed: %v", err)
	}
	if !strings.Contains(out.String(), "personal") {
		t.Fatalf("unexpected usage output: %s", out.String())
	}
}

func TestGymRunValidation(t *testing.T) {
	svc, _ := gym.NewService("", &cliMockClient{})
	var out bytes.Buffer
	var errOut bytes.Buffer

	// Missing task text
	err := RunGym([]string{"run"}, svc, &out, &errOut)
	if err == nil {
		t.Fatal("expected error when neither --task nor --task-file specified")
	}
}

func TestGymRunAutoWorktree(t *testing.T) {
	repo := initRepo(t)
	mock := &cliMockClient{
		startResp: &agym.Run{
			RunID:           "run-cli-auto",
			SelectedProfile: "personal",
			Status:          agym.RunStateStarting,
		},
	}
	svc, err := gym.NewService(repo, mock)
	if err != nil {
		t.Fatal(err)
	}

	oldWd, _ := os.Getwd()
	defer os.Chdir(oldWd)
	_ = os.Chdir(repo)

	var out, errOut bytes.Buffer
	err = RunGym([]string{"run", "--task", "Implement feature X", "--json"}, svc, &out, &errOut)
	if err != nil {
		t.Fatalf("RunGym run failed: %v", err)
	}

	var res struct {
		Branch   string    `json:"branch"`
		Worktree string    `json:"worktree"`
		Run      *agym.Run `json:"run"`
	}
	if err := json.Unmarshal(out.Bytes(), &res); err != nil {
		t.Fatalf("unmarshal error: %v (raw: %s)", err, out.String())
	}
	if res.Branch != "feat/cli-test-branch" {
		t.Errorf("Branch = %q, want feat/cli-test-branch", res.Branch)
	}
	if res.Run == nil || res.Run.RunID != "run-cli-auto" {
		t.Errorf("Run = %+v, want run-cli-auto", res.Run)
	}
}
