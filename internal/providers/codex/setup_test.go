package codex

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

func TestLoginSetupRecordsIdentityAndForcesFileStore(t *testing.T) {
	e := newTestEnv(t)
	t.Setenv("FAKE_CODEX_EMAIL", "work@example.com")
	t.Setenv("FAKE_CODEX_PLAN", "pro")
	account := e.addLogin("work")
	home := e.profileHome(account)

	settings, err := ParseSettings(account)
	if err != nil || settings.AuthMode != AuthChatGPT {
		t.Fatalf("settings = %+v, %v", settings, err)
	}
	if !strings.Contains(e.calls(), `login browser home=`+home+` store="file"`) {
		t.Fatalf("login did not run in the profile home with the file store: %q", e.calls())
	}
	config, err := os.ReadFile(configPath(home))
	if err != nil || !strings.Contains(string(config), "cli_auth_credentials_store = 'file'") {
		t.Fatalf("config.toml = %q, %v", config, err)
	}
	if info, err := os.Stat(home); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("home = %v, %v", info, err)
	}
	info := e.provider.DescribeAccount(t.Context(), account)
	if info.AuthMode != AuthChatGPT || info.Identity != "work@example.com" || info.Plan != "Pro" || info.Options["auth_status"] != "logged_in" || len(info.Warnings) != 0 {
		t.Fatalf("info = %+v", info)
	}
	for _, leak := range []string{"fake-access", "fake-refresh", "acct-fake", home} {
		if strings.Contains(info.Identity+string(account.Settings), leak) {
			t.Fatalf("description or settings leak %q", leak)
		}
	}
}

func TestDeviceSetup(t *testing.T) {
	e := newTestEnv(t)
	account := e.add("headless", agents.SetupOptions{AuthMode: AuthDevice})
	if !strings.Contains(e.calls(), "login device home="+e.profileHome(account)) {
		t.Fatalf("calls = %q", e.calls())
	}
	if settings, _ := ParseSettings(account); settings.AuthMode != AuthDevice {
		t.Fatalf("auth mode = %q", settings.AuthMode)
	}
}

func TestLoginFailureRemovesAccountDir(t *testing.T) {
	e := newTestEnv(t)
	t.Setenv("FAKE_CODEX_LOGIN_FAIL", "1")
	_, err := e.service.Setup(t.Context(), ProviderID, "work", agents.SetupOptions{})
	if err == nil || !strings.Contains(err.Error(), "login") {
		t.Fatalf("err = %v", err)
	}
	if accounts, _ := e.accounts.List(); len(accounts) != 0 {
		t.Fatalf("accounts = %v", accounts)
	}
	entries, _ := os.ReadDir(filepath.Join(e.accounts.Root(), "accounts"))
	if len(entries) != 0 {
		t.Fatalf("account dir left behind: %v", entries)
	}
}

// A login that exits 0 without signing in (closed browser tab) still fails.
func TestLoginWithoutCredentialsFails(t *testing.T) {
	e := newTestEnv(t)
	e.service.Launcher = launcherFunc(func(context.Context, agents.PreparedSession) error { return nil })
	if _, err := e.service.Setup(t.Context(), ProviderID, "work", agents.SetupOptions{}); err == nil || !strings.Contains(err.Error(), "did not complete") {
		t.Fatalf("err = %v", err)
	}
}

type launcherFunc func(context.Context, agents.PreparedSession) error

func (f launcherFunc) RunForeground(ctx context.Context, p agents.PreparedSession) error {
	return f(ctx, p)
}

func TestDuplicateIdentityWarns(t *testing.T) {
	e := newTestEnv(t)
	t.Setenv("FAKE_CODEX_EMAIL", "same@example.com")
	e.addLogin("one")
	if strings.Contains(e.out.String(), "warning") {
		t.Fatalf("first profile warned: %q", e.out.String())
	}
	t.Setenv("FAKE_CODEX_ACCOUNT", "acct-team")
	team := e.addLogin("team") // same person, another workspace: separate limits
	if strings.Contains(e.out.String(), "warning") {
		t.Fatalf("another workspace warned: %q", e.out.String())
	}
	t.Setenv("FAKE_CODEX_ACCOUNT", "")
	_ = os.Unsetenv("FAKE_CODEX_ACCOUNT")
	two := e.addLogin("two") // still added
	if !strings.Contains(e.out.String(), `warning: profile "one" is already signed in as same@example.com`) {
		t.Fatalf("no duplicate warning: %q", e.out.String())
	}
	if accounts, _ := e.accounts.List(); len(accounts) != 3 {
		t.Fatalf("accounts = %d", len(accounts))
	}
	info := e.provider.DescribeAccount(t.Context(), two)
	if len(info.Warnings) != 1 || !strings.Contains(info.Warnings[0], `"one"`) {
		t.Fatalf("warnings = %v", info.Warnings)
	}
	if info := e.provider.DescribeAccount(t.Context(), team); len(info.Warnings) != 0 {
		t.Fatalf("team warnings = %v", info.Warnings)
	}
}

func TestAPIKeyReachesCodexStdinOnly(t *testing.T) {
	e := newTestEnv(t)
	account := e.addAPIKey("ci")
	home := e.profileHome(account)

	data, err := os.ReadFile(authPath(home))
	if err != nil || !strings.Contains(string(data), testAPIKey) {
		t.Fatalf("key did not reach codex login --with-api-key: %q, %v", data, err)
	}
	if !strings.Contains(e.calls(), `login api-key home=`+home+` store="file"`) {
		t.Fatalf("calls = %q", e.calls())
	}
	if strings.Contains(e.out.String()+e.calls(), testAPIKey) {
		t.Fatalf("key echoed: %q", e.out.String())
	}
	// Only Codex's own auth.json holds the key; Bonsai's files never do.
	err = filepath.WalkDir(e.accounts.Root(), func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || path == authPath(home) {
			return err
		}
		content, err := os.ReadFile(path)
		if err == nil && strings.Contains(string(content), testAPIKey) {
			t.Errorf("key stored in %s", path)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	settings, _ := ParseSettings(account)
	if settings.AuthMode != AuthAPIKey {
		t.Fatalf("auth mode = %q", settings.AuthMode)
	}
	info := e.provider.DescribeAccount(t.Context(), account)
	if info.AuthMode != AuthAPIKey || info.Identity != "API key" || info.Options["auth_status"] != "logged_in" || len(info.Warnings) != 0 {
		t.Fatalf("info = %+v", info)
	}
}

func TestAPIKeyPrompt(t *testing.T) {
	e := newTestEnv(t)
	result, err := e.service.SetupWithIO(t.Context(), ProviderID, "ci", agents.SetupOptions{AuthMode: AuthAPIKey}, agents.SetupIO{Prompt: promptFunc(func(label string) (string, error) {
		if label != "OpenAI API key" {
			t.Errorf("label = %q", label)
		}
		return "  " + testAPIKey + " ", nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	account := result.Account
	if data, _ := os.ReadFile(authPath(e.profileHome(account))); !strings.Contains(string(data), `"`+testAPIKey+`"`) {
		t.Fatalf("auth.json = %q", data)
	}
}

func TestBadAPIKeysAreRejected(t *testing.T) {
	for _, bad := range []string{"", "pk-AAAAAAAAAAAAAAAAAAAAAAAAAAAA", "sk-short", "sk-has space AAAAAAAAAAAAAAAAAAAAA", "sk-ant-oat01-\"quote\"AAAAAAAAAAAAA", strings.Repeat("x", 9000)} {
		e := newTestEnv(t)
		_, err := e.service.Setup(t.Context(), ProviderID, "ci", agents.SetupOptions{Secret: strings.NewReader(bad)})
		if err == nil {
			t.Fatalf("accepted %q", bad)
		}
		if bad != "" && strings.Contains(err.Error(), bad) {
			t.Fatalf("error echoes the key: %v", err)
		}
		if accounts, _ := e.accounts.List(); len(accounts) != 0 {
			t.Fatal("rejected key left an account")
		}
		if strings.Contains(e.calls(), "login") {
			t.Fatal("a rejected key must not reach codex")
		}
	}
}

// A failing `codex login --with-api-key` reports neither the key nor Codex's output.
func TestAPIKeyLoginFailureHidesKey(t *testing.T) {
	e := newTestEnv(t)
	piped := false
	e.provider.runner = runnerFunc(func(_ context.Context, p agents.PreparedSession, stdin io.Reader) ([]byte, error) {
		if p.Args[0] == "--version" {
			return []byte("codex-cli " + MinVersion), nil
		}
		if stdin != nil {
			data, _ := io.ReadAll(stdin)
			piped = strings.TrimSpace(string(data)) == testAPIKey
		}
		return []byte("error: " + testAPIKey), errors.New("exit status 1: " + testAPIKey)
	})
	_, err := e.service.Setup(t.Context(), ProviderID, "ci", agents.SetupOptions{Secret: strings.NewReader(testAPIKey)})
	if !piped || err == nil || !strings.Contains(err.Error(), "--with-api-key failed") || strings.Contains(err.Error(), testAPIKey) {
		t.Fatalf("piped=%v err = %v", piped, err)
	}
}

type runnerFunc func(context.Context, agents.PreparedSession, io.Reader) ([]byte, error)

func (f runnerFunc) Output(ctx context.Context, p agents.PreparedSession, stdin io.Reader) ([]byte, error) {
	return f(ctx, p, stdin)
}

func TestSetupOptionValidation(t *testing.T) {
	no := false
	for _, tc := range []struct {
		name    string
		options agents.SetupOptions
		want    string
	}{
		{name: "unknown mode", options: agents.SetupOptions{AuthMode: "token"}, want: "unknown auth mode"},
		{name: "stdin with chatgpt", options: agents.SetupOptions{AuthMode: AuthChatGPT, Secret: strings.NewReader(testAPIKey)}, want: "--auth api-key"},
		{name: "stdin with device", options: agents.SetupOptions{AuthMode: AuthDevice, Secret: strings.NewReader(testAPIKey)}, want: "--auth api-key"},
		{name: "no seed and seed from", options: agents.SetupOptions{Seed: &no, SeedFrom: "/x"}, want: "cannot be combined"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newTestEnv(t)
			_, err := e.service.Setup(t.Context(), ProviderID, "x", tc.options)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
			if e.calls() != "" {
				t.Fatalf("codex ran: %q", e.calls())
			}
		})
	}
}

func TestAvailability(t *testing.T) {
	e := newTestEnv(t)
	if a := e.provider.Availability(t.Context()); !a.Available || a.Version != MinVersion || a.Hint != "" {
		t.Fatalf("availability = %+v", a)
	}

	old := newTestEnv(t)
	t.Setenv("FAKE_CODEX_VERSION", "0.98.0")
	a := old.provider.Availability(t.Context())
	if a.Available || a.Reason != "Update Codex (found 0.98.0, need "+MinVersion+")" || a.Hint != installHint {
		t.Fatalf("availability = %+v", a)
	}
	if _, err := old.service.Setup(t.Context(), ProviderID, "x", agents.SetupOptions{}); err == nil || !strings.Contains(err.Error(), "Update Codex") {
		t.Fatalf("setup err = %v", err)
	}

	missing := newTestEnv(t)
	missing.provider.binaryResolver = resolverFunc(func() (string, error) { return "", errors.New("not found") })
	if a := missing.provider.Availability(t.Context()); a.Available || a.Reason != "Install Codex CLI and restart Bonsai" || a.Hint != "npm install -g @openai/codex@latest" {
		t.Fatalf("availability = %+v", a)
	}
}

type resolverFunc func() (string, error)

func (f resolverFunc) Resolve() (string, error) { return f() }

// The version probe runs in a throwaway CODEX_HOME under Bonsai's cache, never
// the user's home or the system temp dir, and removes it.
func TestVersionProbeUsesScratchHome(t *testing.T) {
	e := newTestEnv(t)
	scratch := t.TempDir()
	e.provider.scratchRoot = func() (string, error) { return scratch, nil }
	var home string
	e.provider.runner = runnerFunc(func(_ context.Context, p agents.PreparedSession, _ io.Reader) ([]byte, error) {
		home = p.EnvSet["CODEX_HOME"]
		return []byte("codex-cli 0.162.1\n"), nil
	})
	if a := e.provider.Availability(t.Context()); !a.Available {
		t.Fatalf("availability = %+v", a)
	}
	if filepath.Dir(home) != scratch {
		t.Fatalf("version ran with CODEX_HOME=%q", home)
	}
	if _, err := os.Stat(home); !os.IsNotExist(err) {
		t.Fatalf("scratch home was not removed: %v", err)
	}
	if entries, _ := os.ReadDir(e.home); len(entries) != 0 {
		t.Fatalf("version check wrote into HOME: %v", entries)
	}
}
