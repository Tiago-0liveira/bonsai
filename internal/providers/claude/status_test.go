package claude

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

func TestParseAuthStatusFixtures(t *testing.T) {
	for file, want := range map[string]authStatus{
		"auth-status-claudeai.json":    {LoggedIn: true, AuthMethod: "claude.ai", Email: "<redacted>"},
		"auth-status-logged-out.json":  {LoggedIn: false, AuthMethod: "none"},
		"auth-status-oauth-token.json": {LoggedIn: true, AuthMethod: "oauth_token"},
	} {
		data, err := os.ReadFile(filepath.Join("testdata", file))
		if err != nil {
			t.Fatal(err)
		}
		got, ok := parseAuthStatus(data)
		if !ok || got != want {
			t.Errorf("%s = %+v, %v; want %+v", file, got, ok, want)
		}
	}
	if _, ok := parseAuthStatus([]byte("not json")); ok {
		t.Fatal("garbage parsed")
	}
}

func TestDescribeLoggedOutProfile(t *testing.T) {
	e := newTestEnv(t)
	account := e.addLogin("work")
	if err := os.Remove(filepath.Join(configDir(e.accounts, account), ".credentials.json")); err != nil {
		t.Fatal(err)
	}
	info := e.provider.DescribeAccount(t.Context(), account)
	if info.Options["auth_status"] != "logged_out" || len(info.Warnings) != 1 || !strings.Contains(info.Warnings[0], "Not logged in") {
		t.Fatalf("info = %+v", info)
	}
}

type blockingRunner struct{ calls atomic.Int32 }

func (r *blockingRunner) Output(ctx context.Context, _ agents.PreparedSession) ([]byte, error) {
	r.calls.Add(1)
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestStatusTimeoutIsUnknownNotAnError(t *testing.T) {
	e := newTestEnv(t)
	account := e.addLogin("work")
	old := statusTimeout
	statusTimeout = 50 * time.Millisecond
	t.Cleanup(func() { statusTimeout = old })
	e.provider.runner = &blockingRunner{}

	start := time.Now()
	info := e.provider.DescribeAccount(t.Context(), account)
	if time.Since(start) > 2*time.Second {
		t.Fatal("status did not time out")
	}
	if info.Options["auth_status"] != "unknown" || len(info.Warnings) != 0 || info.AuthMode != AuthLogin {
		t.Fatalf("info = %+v", info)
	}
	if info.Identity != "user@example.com" {
		t.Fatalf("identity should still come from .claude.json, got %q", info.Identity)
	}
}

type countingRunner struct {
	inner CommandRunner
	calls atomic.Int32
}

func (r *countingRunner) Output(ctx context.Context, p agents.PreparedSession) ([]byte, error) {
	r.calls.Add(1)
	return r.inner.Output(ctx, p)
}

func TestStatusIsCached(t *testing.T) {
	e := newTestEnv(t)
	account := e.addLogin("work")
	runner := &countingRunner{inner: execRunner{}}
	e.provider.runner = runner
	now := time.Now()
	e.provider.now = func() time.Time { return now }

	e.provider.DescribeAccount(t.Context(), account)
	e.provider.DescribeAccount(t.Context(), account)
	if runner.calls.Load() != 1 {
		t.Fatalf("auth status ran %d times within the TTL", runner.calls.Load())
	}
	now = now.Add(statusTTL + time.Second)
	e.provider.DescribeAccount(t.Context(), account)
	if runner.calls.Load() != 2 {
		t.Fatalf("auth status ran %d times after the TTL", runner.calls.Load())
	}
}

func TestAvailability(t *testing.T) {
	e := newTestEnv(t)
	if a := e.provider.Availability(t.Context()); !a.Available || a.Version != "2.1.295" {
		t.Fatalf("availability = %+v", a)
	}

	old := newTestEnv(t)
	t.Setenv("FAKE_CLAUDE_VERSION", "2.0.9")
	a := old.provider.Availability(t.Context())
	if a.Available || a.Reason != "Update Claude Code (found 2.0.9, need "+MinVersion+")" {
		t.Fatalf("availability = %+v", a)
	}

	missing := newTestEnv(t)
	t.Setenv("PATH", t.TempDir())
	a = missing.provider.Availability(t.Context())
	if a.Available || a.Reason != "Install Claude Code and restart Bonsai" {
		t.Fatalf("availability = %+v", a)
	}
}

func TestAvailabilityRecoversAfterInstall(t *testing.T) {
	e := newTestEnv(t)
	realPath := os.Getenv("PATH")
	t.Setenv("PATH", t.TempDir())
	if e.provider.Availability(t.Context()).Available {
		t.Fatal("should be unavailable")
	}
	t.Setenv("PATH", realPath)
	if !e.provider.Availability(t.Context()).Available {
		t.Fatal("failure must not be cached")
	}
}

func TestParseVersion(t *testing.T) {
	for in, want := range map[string]string{"2.1.295 (Claude Code)": "2.1.295", "claude 10.0.3": "10.0.3"} {
		if _, got, ok := parseVersion(in); !ok || got != want {
			t.Errorf("%q -> %q, %v", in, got, ok)
		}
	}
	if _, _, ok := parseVersion("dev"); ok {
		t.Fatal("parsed a non-version")
	}
	a, _, _ := parseVersion("2.1.9")
	b, _, _ := parseVersion("2.1.10")
	if !a.less(b) || b.less(a) {
		t.Fatal("versions must compare numerically")
	}
}

func TestInvalidSettingsDescribeAsWarning(t *testing.T) {
	e := newTestEnv(t)
	bad := agents.Account{ID: "acct_bad", Provider: ProviderID, Name: "bad", Settings: []byte(`{"auth_mode":"magic"}`)}
	info := e.provider.DescribeAccount(t.Context(), bad)
	if len(info.Warnings) != 1 || info.AuthMode != "" {
		t.Fatalf("info = %+v", info)
	}
	if _, err := ParseSettings(bad); err == nil {
		t.Fatal("bad auth mode accepted")
	}
	for _, raw := range []string{`{"permission_mode":"x"}`, `{"effort":"x"}`, `{"model":"-m"}`, `not json`} {
		if _, err := ParseSettings(agents.Account{Settings: []byte(raw)}); err == nil {
			t.Errorf("%s accepted", raw)
		}
	}
}

func TestUnknownStatusIsNotRetriedImmediately(t *testing.T) {
	e := newTestEnv(t)
	account := e.addLogin("work")
	old := statusTimeout
	statusTimeout = 20 * time.Millisecond
	t.Cleanup(func() { statusTimeout = old })
	runner := &blockingRunner{}
	e.provider.runner = runner
	now := time.Now()
	e.provider.now = func() time.Time { return now }
	for i := 0; i < 3; i++ {
		e.provider.DescribeAccount(t.Context(), account)
	}
	if runner.calls.Load() != 1 {
		t.Fatalf("hung binary ran %d times within the unknown TTL", runner.calls.Load())
	}
	now = now.Add(unknownStatusTTL + time.Second)
	e.provider.DescribeAccount(t.Context(), account)
	if runner.calls.Load() != 2 {
		t.Fatalf("auth status ran %d times after the unknown TTL", runner.calls.Load())
	}
}

type recordingRunner struct {
	inner CommandRunner
	seen  []agents.PreparedSession
}

func (r *recordingRunner) Output(ctx context.Context, p agents.PreparedSession) ([]byte, error) {
	r.seen = append(r.seen, p)
	return r.inner.Output(ctx, p)
}

func TestVersionCheckUsesThrowawayConfigDir(t *testing.T) {
	e := newTestEnv(t)
	runner := &recordingRunner{inner: execRunner{}}
	e.provider.runner = runner
	if a := e.provider.Availability(t.Context()); !a.Available {
		t.Fatalf("availability = %+v", a)
	}
	if len(runner.seen) != 1 {
		t.Fatalf("runs = %d", len(runner.seen))
	}
	dir := runner.seen[0].EnvSet["CLAUDE_CONFIG_DIR"]
	if dir == "" || strings.HasPrefix(dir, e.home) {
		t.Fatalf("version ran with CLAUDE_CONFIG_DIR=%q", dir)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("scratch dir was not removed: %v", err)
	}
	if entries, _ := os.ReadDir(e.home); len(entries) != 0 {
		t.Fatalf("version check wrote into HOME: %v", entries)
	}
}
