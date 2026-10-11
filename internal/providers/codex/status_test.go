package codex

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

// countingRunner counts `login status` runs on top of the real runner.
type countingRunner struct {
	inner  CommandRunner
	status atomic.Int32
}

func (r *countingRunner) Output(ctx context.Context, p agents.PreparedSession, stdin io.Reader) ([]byte, error) {
	if strings.Contains(strings.Join(p.Args, " "), "login status") {
		r.status.Add(1)
	}
	return r.inner.Output(ctx, p, stdin)
}

func TestStatusIsCached(t *testing.T) {
	e := newTestEnv(t)
	account := e.addLogin("work")
	runner := &countingRunner{inner: e.provider.runner}
	e.provider.runner = runner
	now := time.Now()
	e.provider.now = func() time.Time { return now }
	e.provider.forgetStatus(account.ID)

	for range 3 {
		e.provider.DescribeAccount(t.Context(), account)
	}
	if n := runner.status.Load(); n != 1 {
		t.Fatalf("status ran %d times", n)
	}
	now = now.Add(statusTTL + time.Second)
	e.provider.DescribeAccount(t.Context(), account)
	if n := runner.status.Load(); n != 2 {
		t.Fatalf("status ran %d times after the TTL", n)
	}
}

func TestLoggedOutProfileWarns(t *testing.T) {
	e := newTestEnv(t)
	account := e.addLogin("work")
	if err := os.Remove(authPath(e.profileHome(account))); err != nil {
		t.Fatal(err)
	}
	e.provider.forgetStatus(account.ID)
	info := e.provider.DescribeAccount(t.Context(), account)
	if info.Options["auth_status"] != "logged_out" || info.Identity != "" || len(info.Warnings) != 1 || !strings.Contains(info.Warnings[0], "bonsai agent run work -- login") {
		t.Fatalf("info = %+v", info)
	}
	e.prepare(account, agents.LaunchOptions{}) // Codex shows its own sign-in screen
}

func TestHungStatusIsUnknownAndRememberedBriefly(t *testing.T) {
	e := newTestEnv(t)
	account := e.addLogin("work")
	old := statusTimeout
	statusTimeout = 100 * time.Millisecond
	t.Cleanup(func() { statusTimeout = old })
	t.Setenv("FAKE_CODEX_STATUS_SLEEP", "2")
	runner := &countingRunner{inner: e.provider.runner}
	e.provider.runner = runner
	now := time.Now()
	e.provider.now = func() time.Time { return now }
	e.provider.forgetStatus(account.ID)

	start := time.Now()
	info := e.provider.DescribeAccount(t.Context(), account)
	if info.Options["auth_status"] != "unknown" || len(info.Warnings) != 0 {
		t.Fatalf("info = %+v", info)
	}
	if time.Since(start) > 1500*time.Millisecond {
		t.Fatal("status timeout not honoured")
	}
	e.provider.DescribeAccount(t.Context(), account)
	if n := runner.status.Load(); n != 1 {
		t.Fatalf("unknown status re-ran %d times", n)
	}
	now = now.Add(unknownStatusTTL + time.Second)
	e.provider.DescribeAccount(t.Context(), account)
	if n := runner.status.Load(); n != 2 {
		t.Fatalf("status ran %d times after the unknown TTL", n)
	}
}

func TestStatusFromExit(t *testing.T) {
	exit := func(code int) error {
		return exec.Command("sh", "-c", "exit "+string(rune('0'+code))).Run()
	}
	if statusFromExit(nil) != statusLoggedIn || statusFromExit(exit(1)) != statusLoggedOut || statusFromExit(exit(2)) != statusUnknown || statusFromExit(errors.New("boom")) != statusUnknown {
		t.Fatal("exit code mapping")
	}
}

// With an administrator-enforced keyring the login is not in auth.json.
func TestLoginOutsideTheProfileWarns(t *testing.T) {
	e := newTestEnv(t)
	account := e.addLogin("work")
	if err := os.Remove(authPath(e.profileHome(account))); err != nil {
		t.Fatal(err)
	}
	e.provider.runner = runnerFunc(func(context.Context, agents.PreparedSession, io.Reader) ([]byte, error) { return nil, nil })
	e.provider.forgetStatus(account.ID)
	info := e.provider.DescribeAccount(t.Context(), account)
	if info.Options["auth_status"] != "logged_in" || len(info.Warnings) != 1 || !strings.Contains(info.Warnings[0], "keyring") {
		t.Fatalf("info = %+v", info)
	}
}

func TestDescribeInvalidSettings(t *testing.T) {
	info := (&Provider{}).DescribeAccount(t.Context(), agents.Account{Settings: []byte(`{"auth_mode":"x"}`)})
	if len(info.Warnings) != 1 || info.AuthMode != "" {
		t.Fatalf("info = %+v", info)
	}
}

func TestRemoveLogsOutWithProfileEnvironment(t *testing.T) {
	e := newTestEnv(t)
	account := e.addLogin("work")
	t.Setenv("CODEX_HOME", "/elsewhere")
	warnings, err := e.service.Remove(t.Context(), account.ID)
	if err != nil || len(warnings) != 0 {
		t.Fatalf("remove: %v, %v", warnings, err)
	}
	if !strings.Contains(e.calls(), `logout home=`+e.profileHome(account)+` store="file"`) {
		t.Fatalf("calls = %q", e.calls())
	}
	if _, err := os.Stat(e.accounts.AccountDir(account.ID)); !os.IsNotExist(err) {
		t.Fatalf("account dir still exists: %v", err)
	}
}

func TestRemoveFailureStillRemoves(t *testing.T) {
	e := newTestEnv(t)
	account := e.addLogin("work")
	t.Setenv("FAKE_CODEX_LOGOUT_FAIL", "1")
	warnings, err := e.service.Remove(t.Context(), account.ID)
	if err != nil || len(warnings) != 1 || !strings.Contains(warnings[0], "Codex provider cleanup failed") {
		t.Fatalf("remove: %v, %v", warnings, err)
	}
	if accounts, _ := e.accounts.List(); len(accounts) != 0 {
		t.Fatalf("accounts = %v", accounts)
	}
}

func TestRemoveLoggedOutProfileSkipsLogout(t *testing.T) {
	e := newTestEnv(t)
	account := e.addLogin("work")
	if err := os.Remove(authPath(e.profileHome(account))); err != nil {
		t.Fatal(err)
	}
	e.provider.forgetStatus(account.ID)
	if warnings, err := e.service.Remove(t.Context(), account.ID); err != nil || len(warnings) != 0 {
		t.Fatalf("remove: %v, %v", warnings, err)
	}
	if strings.Contains(e.calls(), "logout") {
		t.Fatalf("calls = %q", e.calls())
	}
}

// Setup, launch, describe and remove never touch the user's real ~/.codex.
func TestRealCodexHomeIsNeverWritten(t *testing.T) {
	e := newTestEnv(t)
	hostCodexHome(t, e.home+"/.codex")
	before := treeHash(t, e.home)
	work := e.addLogin("work")
	key := e.addAPIKey("ci")
	e.prepare(work, agents.LaunchOptions{Model: "o3", Sandbox: SandboxBypass})
	e.prepare(key, agents.LaunchOptions{})
	e.provider.DescribeAccount(t.Context(), work)
	e.provider.DescribeAccount(t.Context(), key)
	if _, err := e.provider.Usage(t.Context(), work, agents.UsageOptions{}); !errors.Is(err, agents.ErrUsageUnsupported) {
		t.Fatalf("usage err = %v", err)
	}
	for _, a := range []agents.Account{work, key} {
		if _, err := e.service.Remove(t.Context(), a.ID); err != nil {
			t.Fatal(err)
		}
	}
	if treeHash(t, e.home) != before {
		t.Fatal("HOME changed")
	}
}
