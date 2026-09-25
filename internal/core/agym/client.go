package agym

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	MaxControlResponseBytes = 4 * 1024 * 1024 // 4 MiB
	MaxStderrBytes          = 64 * 1024       // 64 KiB

	DefaultInfoTimeout    = 3 * time.Second
	DefaultRequestTimeout = 10 * time.Second
	DefaultRefreshTimeout = 60 * time.Second
)

// Client defines the interface for interacting with the AGYM integration CLI.
type Client interface {
	Info(ctx context.Context) (*InfoData, error)
	Profiles(ctx context.Context) ([]Profile, error)
	Usage(ctx context.Context, profile string, refresh bool) ([]Usage, error)
	StartRun(ctx context.Context, req *StartRequest) (*Run, error)
	GetRun(ctx context.Context, runID string) (*Run, error)
	ListRunsByWorkspace(ctx context.Context, workspaceKey string) ([]Run, error)
	ListRunsByRequest(ctx context.Context, clientID, requestID string) ([]Run, error)
	StopRun(ctx context.Context, runID string) error
	GetEvents(ctx context.Context, runID string, after uint64, limit int) (*EventPage, error)
	GetLease(ctx context.Context, leaseID string) (*Lease, error)
	StreamEvents(ctx context.Context, runID string, after uint64) (<-chan Event, <-chan error)
	GenerateBranchName(ctx context.Context, profile, task string) (string, error)
}

// Runner abstracts running the agym command for tests.
type Runner func(ctx context.Context, stdin io.Reader, args ...string) ([]byte, []byte, error)

// ExecClient executes commands via the installed agym binary.
type ExecClient struct {
	binaryPath string
	runner     Runner
}

// NewClient creates an ExecClient, resolving the installed agym executable.
func NewClient(binaryPath string) *ExecClient {
	return &ExecClient{
		binaryPath: binaryPath,
	}
}

// ResolveBinary locates the agym executable on the host PATH.
// Rejects relative or implicit current-directory resolutions.
func ResolveBinary(customPath string) (string, error) {
	if customPath != "" {
		if !filepath.IsAbs(customPath) && strings.ContainsRune(customPath, filepath.Separator) {
			return "", fmt.Errorf("custom agym path must be absolute: %q", customPath)
		}
		path, err := exec.LookPath(customPath)
		if err != nil {
			return "", fmt.Errorf("%w: %v", ErrNotInstalled, err)
		}
		if !filepath.IsAbs(path) {
			abs, err := filepath.Abs(path)
			if err != nil {
				return "", err
			}
			path = abs
		}
		return path, nil
	}

	path, err := exec.LookPath("agym")
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrNotInstalled, err)
	}
	if !filepath.IsAbs(path) {
		abs, err := filepath.Abs(path)
		if err != nil {
			return "", err
		}
		path = abs
	}
	return path, nil
}

// WithRunner sets a custom runner function for testing.
func (c *ExecClient) WithRunner(r Runner) *ExecClient {
	c.runner = r
	return c
}

func (c *ExecClient) runCommand(ctx context.Context, stdin io.Reader, args ...string) ([]byte, error) {
	if c.runner != nil {
		stdout, stderr, err := c.runner(ctx, stdin, args...)
		if err != nil && len(stdout) == 0 {
			if len(stderr) > 0 {
				return nil, fmt.Errorf("%w: %v (stderr: %s)", ErrTransport, err, string(stderr))
			}
			return nil, fmt.Errorf("%w: %v", ErrTransport, err)
		}
		return stdout, nil
	}

	bin := c.binaryPath
	if bin == "" {
		var err error
		bin, err = ResolveBinary("")
		if err != nil {
			return nil, err
		}
	}

	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = append(os.Environ(), "NO_COLOR=1")
	if stdin != nil {
		cmd.Stdin = stdin
	}

	var stdoutBuf bytes.Buffer
	var stderrBuf bytes.Buffer

	// Bound stdout and stderr reads
	stdoutLimit := io.LimitReader(&stdoutBuf, MaxControlResponseBytes)
	stderrLimit := io.LimitReader(&stderrBuf, MaxStderrBytes)
	_ = stdoutLimit
	_ = stderrLimit

	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	err := cmd.Run()
	stdout := stdoutBuf.Bytes()
	stderr := stderrBuf.Bytes()

	if err != nil && len(stdout) == 0 {
		if len(stderr) > 0 {
			return nil, fmt.Errorf("%w: %v (stderr: %s)", ErrTransport, err, string(stderr))
		}
		return nil, fmt.Errorf("%w: %v", ErrTransport, err)
	}
	return stdout, nil
}

func parseResponse(data []byte) (*Response, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("%w: empty response", ErrInvalidResponse)
	}
	var resp Response
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidResponse, err)
	}
	if !resp.Ok {
		if resp.Error != nil {
			return nil, &ProtocolError{
				Code:              resp.Error.Code,
				Message:           resp.Error.Message,
				Retryable:         resp.Error.Retryable,
				RetryAfterSeconds: resp.Error.RetryAfterSeconds,
				RunID:             resp.Error.RunID,
			}
		}
		return nil, fmt.Errorf("%w: unknown error payload", ErrInvalidResponse)
	}
	return &resp, nil
}

// Info retrieves integration capabilities and metadata.
func (c *ExecClient) Info(ctx context.Context) (*InfoData, error) {
	ctx, cancel := context.WithTimeout(ctx, DefaultInfoTimeout)
	defer cancel()

	raw, err := c.runCommand(ctx, nil, "integration", "info", "--json")
	if err != nil {
		return nil, err
	}
	resp, err := parseResponse(raw)
	if err != nil {
		return nil, err
	}
	var info InfoData
	if err := json.Unmarshal(resp.Data, &info); err != nil {
		return nil, fmt.Errorf("%w: decoding info data: %v", ErrInvalidResponse, err)
	}
	return &info, nil
}

// Profiles lists available profiles.
func (c *ExecClient) Profiles(ctx context.Context) ([]Profile, error) {
	ctx, cancel := context.WithTimeout(ctx, DefaultRequestTimeout)
	defer cancel()

	raw, err := c.runCommand(ctx, nil, "integration", "profiles", "--protocol", strconv.Itoa(SupportedProtocolMajor), "--json")
	if err != nil {
		return nil, err
	}
	resp, err := parseResponse(raw)
	if err != nil {
		return nil, err
	}
	var profiles []Profile
	if err := json.Unmarshal(resp.Data, &profiles); err != nil {
		return nil, fmt.Errorf("%w: decoding profiles: %v", ErrInvalidResponse, err)
	}
	return profiles, nil
}

// Usage retrieves quota status for all profiles or a specific profile.
func (c *ExecClient) Usage(ctx context.Context, profile string, refresh bool) ([]Usage, error) {
	timeout := DefaultRequestTimeout
	if refresh {
		timeout = DefaultRefreshTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	args := []string{"integration", "usage", "--protocol", strconv.Itoa(SupportedProtocolMajor), "--json"}
	if profile != "" {
		args = append(args, "--profile", profile)
	}
	if refresh {
		args = append(args, "--refresh")
	}

	raw, err := c.runCommand(ctx, nil, args...)
	if err != nil {
		return nil, err
	}
	resp, err := parseResponse(raw)
	if err != nil {
		return nil, err
	}

	// Data could be a single Usage or a slice of Usage
	var usages []Usage
	if err := json.Unmarshal(resp.Data, &usages); err == nil {
		return usages, nil
	}
	var single Usage
	if err := json.Unmarshal(resp.Data, &single); err == nil {
		return []Usage{single}, nil
	}
	return nil, fmt.Errorf("%w: decoding usage", ErrInvalidResponse)
}

// StartRun submits a headless task execution request.
func (c *ExecClient) StartRun(ctx context.Context, req *StartRequest) (*Run, error) {
	ctx, cancel := context.WithTimeout(ctx, DefaultRequestTimeout)
	defer cancel()

	reqBytes, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal start request: %w", err)
	}

	raw, err := c.runCommand(ctx, bytes.NewReader(reqBytes), "integration", "run", "start", "--request-json", "-", "--protocol", strconv.Itoa(SupportedProtocolMajor), "--json")
	if err != nil {
		return nil, err
	}
	resp, err := parseResponse(raw)
	if err != nil {
		return nil, err
	}
	var run Run
	if err := json.Unmarshal(resp.Data, &run); err != nil {
		return nil, fmt.Errorf("%w: decoding run: %v", ErrInvalidResponse, err)
	}
	return &run, nil
}

// GetRun fetches the snapshot of a specific run.
func (c *ExecClient) GetRun(ctx context.Context, runID string) (*Run, error) {
	ctx, cancel := context.WithTimeout(ctx, DefaultRequestTimeout)
	defer cancel()

	raw, err := c.runCommand(ctx, nil, "integration", "run", "get", "--id", runID, "--protocol", strconv.Itoa(SupportedProtocolMajor), "--json")
	if err != nil {
		return nil, err
	}
	resp, err := parseResponse(raw)
	if err != nil {
		return nil, err
	}
	var run Run
	if err := json.Unmarshal(resp.Data, &run); err != nil {
		return nil, fmt.Errorf("%w: decoding run: %v", ErrInvalidResponse, err)
	}
	return &run, nil
}

// ListRunsByWorkspace retrieves runs for a specific workspace key.
func (c *ExecClient) ListRunsByWorkspace(ctx context.Context, workspaceKey string) ([]Run, error) {
	ctx, cancel := context.WithTimeout(ctx, DefaultRequestTimeout)
	defer cancel()

	raw, err := c.runCommand(ctx, nil, "integration", "run", "list", "--client", "bonsai", "--workspace-key", workspaceKey, "--protocol", strconv.Itoa(SupportedProtocolMajor), "--json")
	if err != nil {
		return nil, err
	}
	resp, err := parseResponse(raw)
	if err != nil {
		return nil, err
	}
	var runs []Run
	if err := json.Unmarshal(resp.Data, &runs); err != nil {
		return nil, fmt.Errorf("%w: decoding runs: %v", ErrInvalidResponse, err)
	}
	return runs, nil
}

// ListRunsByRequest retrieves runs by client ID and request ID.
func (c *ExecClient) ListRunsByRequest(ctx context.Context, clientID, requestID string) ([]Run, error) {
	ctx, cancel := context.WithTimeout(ctx, DefaultRequestTimeout)
	defer cancel()

	raw, err := c.runCommand(ctx, nil, "integration", "run", "list", "--request-id", requestID, "--client-id", clientID, "--protocol", strconv.Itoa(SupportedProtocolMajor), "--json")
	if err != nil {
		return nil, err
	}
	resp, err := parseResponse(raw)
	if err != nil {
		return nil, err
	}
	var runs []Run
	if err := json.Unmarshal(resp.Data, &runs); err != nil {
		return nil, fmt.Errorf("%w: decoding runs: %v", ErrInvalidResponse, err)
	}
	return runs, nil
}

// StopRun requests termination of an active run.
func (c *ExecClient) StopRun(ctx context.Context, runID string) error {
	ctx, cancel := context.WithTimeout(ctx, DefaultRequestTimeout)
	defer cancel()

	raw, err := c.runCommand(ctx, nil, "integration", "run", "stop", "--id", runID, "--protocol", strconv.Itoa(SupportedProtocolMajor), "--json")
	if err != nil {
		return err
	}
	_, err = parseResponse(raw)
	return err
}

// GetEvents retrieves a page of historical events.
func (c *ExecClient) GetEvents(ctx context.Context, runID string, after uint64, limit int) (*EventPage, error) {
	ctx, cancel := context.WithTimeout(ctx, DefaultRequestTimeout)
	defer cancel()

	if limit <= 0 {
		limit = 200
	}
	raw, err := c.runCommand(ctx, nil, "integration", "run", "events", "--id", runID, "--after", strconv.FormatUint(after, 10), "--limit", strconv.Itoa(limit), "--protocol", strconv.Itoa(SupportedProtocolMajor), "--json")
	if err != nil {
		return nil, err
	}
	resp, err := parseResponse(raw)
	if err != nil {
		return nil, err
	}
	var page EventPage
	if err := json.Unmarshal(resp.Data, &page); err != nil {
		return nil, fmt.Errorf("%w: decoding event page: %v", ErrInvalidResponse, err)
	}
	return &page, nil
}

// GetLease retrieves lease details.
func (c *ExecClient) GetLease(ctx context.Context, leaseID string) (*Lease, error) {
	ctx, cancel := context.WithTimeout(ctx, DefaultRequestTimeout)
	defer cancel()

	raw, err := c.runCommand(ctx, nil, "integration", "lease", "get", "--id", leaseID, "--protocol", strconv.Itoa(SupportedProtocolMajor), "--json")
	if err != nil {
		return nil, err
	}
	resp, err := parseResponse(raw)
	if err != nil {
		return nil, err
	}
	var lease Lease
	if err := json.Unmarshal(resp.Data, &lease); err != nil {
		return nil, fmt.Errorf("%w: decoding lease: %v", ErrInvalidResponse, err)
	}
	return &lease, nil
}
