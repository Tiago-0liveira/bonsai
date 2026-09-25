package agym

import (
	"context"
	"io"
	"os"
	"testing"
)

func TestParseInfoResponse(t *testing.T) {
	data, err := os.ReadFile("testdata/v1/info.json")
	if err != nil {
		t.Fatal(err)
	}

	client := NewClient("").WithRunner(func(ctx context.Context, stdin io.Reader, args ...string) ([]byte, []byte, error) {
		return data, nil, nil
	})

	info, err := client.Info(context.Background())
	if err != nil {
		t.Fatalf("Info() error: %v", err)
	}
	if info.AGYMVersion != "0.1.0" {
		t.Errorf("version = %q, want 0.1.0", info.AGYMVersion)
	}
	if !info.HasCapability(CapRunsDurable) {
		t.Errorf("expected capability %s", CapRunsDurable)
	}
	if !info.SupportsMajor(1) {
		t.Errorf("expected support for major 1")
	}
	if info.SupportsMajor(2) {
		t.Errorf("did not expect support for major 2")
	}
}

func TestParseProfilesResponse(t *testing.T) {
	data, err := os.ReadFile("testdata/v1/profiles.json")
	if err != nil {
		t.Fatal(err)
	}

	client := NewClient("").WithRunner(func(ctx context.Context, stdin io.Reader, args ...string) ([]byte, []byte, error) {
		return data, nil, nil
	})

	profiles, err := client.Profiles(context.Background())
	if err != nil {
		t.Fatalf("Profiles() error: %v", err)
	}
	if len(profiles) != 2 {
		t.Fatalf("expected 2 profiles, got %d", len(profiles))
	}
	if profiles[0].Name != "personal" || profiles[0].Readiness != "ready" {
		t.Errorf("unexpected profile[0]: %+v", profiles[0])
	}
	if profiles[1].Name != "work" || profiles[1].Readiness != "busy" {
		t.Errorf("unexpected profile[1]: %+v", profiles[1])
	}
}

func TestParseUsageResponse(t *testing.T) {
	data, err := os.ReadFile("testdata/v1/usage.json")
	if err != nil {
		t.Fatal(err)
	}

	client := NewClient("").WithRunner(func(ctx context.Context, stdin io.Reader, args ...string) ([]byte, []byte, error) {
		return data, nil, nil
	})

	usages, err := client.Usage(context.Background(), "personal", false)
	if err != nil {
		t.Fatalf("Usage() error: %v", err)
	}
	if len(usages) != 1 {
		t.Fatalf("expected 1 usage, got %d", len(usages))
	}
	if usages[0].ProfileID != "prof-personal-123" {
		t.Errorf("profile_id = %q, want prof-personal-123", usages[0].ProfileID)
	}
	if len(usages[0].Windows) != 2 {
		t.Fatalf("expected 2 windows, got %d", len(usages[0].Windows))
	}
	if *usages[0].Windows[0].Remaining != 0.64 {
		t.Errorf("remaining = %v, want 0.64", *usages[0].Windows[0].Remaining)
	}
}

func TestParseRunStartAndGet(t *testing.T) {
	startData, err := os.ReadFile("testdata/v1/run_start.json")
	if err != nil {
		t.Fatal(err)
	}
	getData, err := os.ReadFile("testdata/v1/run_get.json")
	if err != nil {
		t.Fatal(err)
	}

	client := NewClient("").WithRunner(func(ctx context.Context, stdin io.Reader, args ...string) ([]byte, []byte, error) {
		if len(args) >= 3 && args[2] == "start" {
			return startData, nil, nil
		}
		return getData, nil, nil
	})

	req := &StartRequest{
		RequestID: "req-001",
		Client:    "bonsai",
		ClientID:  "client-uuid",
		Workspace: WorkspaceIdentityPayload{
			Key: "ws-key-1",
			Cwd: "/path/to/worktree",
		},
		Profile:          "personal",
		Task:             "Fix the parser bug",
		Execution:        "headless",
		PermissionPolicy: "profile-default",
	}

	run, err := client.StartRun(context.Background(), req)
	if err != nil {
		t.Fatalf("StartRun() error: %v", err)
	}
	if run.RunID != "run-xyz-789" {
		t.Errorf("run_id = %q, want run-xyz-789", run.RunID)
	}
	if run.Status != RunStateStarting {
		t.Errorf("status = %q, want starting", run.Status)
	}

	getRun, err := client.GetRun(context.Background(), "run-xyz-789")
	if err != nil {
		t.Fatalf("GetRun() error: %v", err)
	}
	if getRun.Status != RunStateRunning {
		t.Errorf("status = %q, want running", getRun.Status)
	}
	if getRun.ElapsedSeconds != 24.5 {
		t.Errorf("elapsed = %v, want 24.5", getRun.ElapsedSeconds)
	}
}

func TestErrorPayloadHandling(t *testing.T) {
	errJSON := []byte(`{
		"protocol": {"major": 1, "minor": 0},
		"ok": false,
		"error": {
			"code": "PROFILE_UNAVAILABLE",
			"message": "profile 'personal' requires re-authentication",
			"retryable": false
		}
	}`)

	client := NewClient("").WithRunner(func(ctx context.Context, stdin io.Reader, args ...string) ([]byte, []byte, error) {
		return errJSON, nil, nil
	})

	_, err := client.Profiles(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	pe, ok := AsProtocolError(err)
	if !ok {
		t.Fatalf("expected ProtocolError, got %T: %v", err, err)
	}
	if pe.Code != ErrCodeProfileUnavailable {
		t.Errorf("code = %q, want %s", pe.Code, ErrCodeProfileUnavailable)
	}
}

func TestResolveBinary(t *testing.T) {
	// Relative path with separator should fail
	_, err := ResolveBinary("./relative/agym")
	if err == nil {
		t.Errorf("expected error for relative path with separator, got nil")
	}
}
