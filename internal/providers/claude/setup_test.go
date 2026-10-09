package claude

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

func TestLoginSetupRecordsIdentityAndSettings(t *testing.T) {
	e := newTestEnv(t)
	t.Setenv("FAKE_CLAUDE_EMAIL", "work@example.com")
	account := e.addLogin("work")

	settings, err := ParseSettings(account)
	if err != nil || settings.AuthMode != AuthLogin {
		t.Fatalf("settings = %+v, %v", settings, err)
	}
	if got := storedIdentity(configDir(e.accounts, account)); got != "work@example.com" {
		t.Fatalf("identity = %q", got)
	}
	info := e.provider.DescribeAccount(t.Context(), account)
	if info.AuthMode != AuthLogin || info.Identity != "work@example.com" || info.Options["auth_status"] != "logged_in" || len(info.Warnings) != 0 {
		t.Fatalf("info = %+v", info)
	}
	if !strings.Contains(e.calls(), "login config="+configDir(e.accounts, account)) {
		t.Fatalf("login did not run in the profile dir: %q", e.calls())
	}
	if info, err := os.Stat(configDir(e.accounts, account)); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("config dir = %v, %v", info, err)
	}
}

func TestLoginFailureRemovesAccountDir(t *testing.T) {
	e := newTestEnv(t)
	t.Setenv("FAKE_CLAUDE_LOGIN_FAIL", "1")
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

func TestDuplicateIdentityWarns(t *testing.T) {
	e := newTestEnv(t)
	t.Setenv("FAKE_CLAUDE_EMAIL", "same@example.com")
	e.addLogin("one")
	if strings.Contains(e.out.String(), "warning") {
		t.Fatalf("first profile warned: %q", e.out.String())
	}
	e.addLogin("two") // still added
	if !strings.Contains(e.out.String(), `warning: profile "one" is already signed in as same@example.com`) {
		t.Fatalf("no duplicate warning: %q", e.out.String())
	}
	if accounts, _ := e.accounts.List(); len(accounts) != 2 {
		t.Fatalf("accounts = %d", len(accounts))
	}
}

func TestTokenStdinStoresPrivateFileAndNeverEchoes(t *testing.T) {
	e := newTestEnv(t)
	account := e.addToken("ci")

	path := tokenPath(e.accounts, account)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("token file mode = %v", info.Mode().Perm())
	}
	file, err := readToken(path)
	if err != nil || file.Token != testToken {
		t.Fatalf("token file = %+v, %v", file, err)
	}
	if d := file.ExpiresAt.Sub(file.CreatedAt); d < 364*24*time.Hour || d > 366*24*time.Hour {
		t.Fatalf("expiry = %v", d)
	}
	if strings.Contains(e.out.String(), testToken) {
		t.Fatalf("token echoed: %q", e.out.String())
	}
	if strings.Contains(string(account.Settings), testToken) {
		t.Fatal("token leaked into account settings")
	}
	settings, _ := ParseSettings(account)
	if settings.AuthMode != AuthToken {
		t.Fatalf("auth mode = %q", settings.AuthMode)
	}
	if strings.Contains(e.calls(), "login") {
		t.Fatal("token mode must not run login")
	}
	info2 := e.provider.DescribeAccount(t.Context(), account)
	if info2.AuthMode != AuthToken || info2.Options["auth_status"] != "token" || info2.Identity != "" || len(info2.Warnings) != 0 {
		t.Fatalf("info = %+v", info2)
	}
}

func TestTokenSetupWithoutStdinRunsSetupTokenThenPrompts(t *testing.T) {
	e := newTestEnv(t)
	e.provider.promptSecret = func() (string, error) { return "  " + testToken + " ", nil }
	account, err := e.service.Setup(t.Context(), ProviderID, "ci", agents.SetupOptions{AuthMode: AuthToken})
	if err != nil {
		t.Fatal(err)
	}
	if file, err := readToken(tokenPath(e.accounts, account)); err != nil || file.Token != testToken {
		t.Fatalf("token = %+v, %v", file, err)
	}
	if !strings.Contains(e.out.String(), "sk-ant-oat01-FAKE") {
		t.Fatalf("setup-token output should reach the terminal: %q", e.out.String())
	}
}

func TestBadTokensAreRejected(t *testing.T) {
	for _, bad := range []string{"", "sk-ant-api03-AAAAAAAAAAAAAAAAAAAAAAAA", "sk-ant-oat01-short", "sk-ant-oat01-has space AAAAAAAAAAAAAAAA", strings.Repeat("x", 9000)} {
		e := newTestEnv(t)
		_, err := e.service.Setup(t.Context(), ProviderID, "ci", agents.SetupOptions{Secret: strings.NewReader(bad)})
		if err == nil {
			t.Fatalf("accepted %q", bad)
		}
		if bad != "" && strings.Contains(err.Error(), bad) {
			t.Fatalf("error echoes the token: %v", err)
		}
		if accounts, _ := e.accounts.List(); len(accounts) != 0 {
			t.Fatal("rejected token left an account")
		}
	}
}

func TestSetupOptionValidation(t *testing.T) {
	no := false
	for name, options := range map[string]agents.SetupOptions{
		"unknown auth":          {AuthMode: "api-key"},
		"login with token":      {AuthMode: AuthLogin, Secret: strings.NewReader(testToken)},
		"no-seed and seed-from": {Seed: &no, SeedFrom: "/x"},
	} {
		t.Run(name, func(t *testing.T) {
			e := newTestEnv(t)
			if _, err := e.service.Setup(t.Context(), ProviderID, "x", options); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestSetupFailsWhenClaudeIsMissingOrOld(t *testing.T) {
	e := newTestEnv(t)
	t.Setenv("FAKE_CLAUDE_VERSION", "2.0.1")
	_, err := e.service.Setup(t.Context(), ProviderID, "work", agents.SetupOptions{})
	if err == nil || !strings.Contains(err.Error(), "Update Claude Code (found 2.0.1, need "+MinVersion+")") {
		t.Fatalf("err = %v", err)
	}
	e2 := newTestEnv(t)
	t.Setenv("PATH", t.TempDir())
	_, err = e2.service.Setup(t.Context(), ProviderID, "work", agents.SetupOptions{})
	if err == nil || !strings.Contains(err.Error(), "Install Claude Code") {
		t.Fatalf("err = %v", err)
	}
}

func TestRemoveLogsOutWithProfileEnvironment(t *testing.T) {
	e := newTestEnv(t)
	account := e.addLogin("work")
	warnings, err := e.service.Remove(t.Context(), account.ID)
	if err != nil || len(warnings) != 0 {
		t.Fatalf("remove: %v %v", err, warnings)
	}
	want := "logout config=" + configDir(e.accounts, account) + " home=" + e.home
	if !strings.Contains(e.calls(), want) {
		t.Fatalf("calls = %q, want %q", e.calls(), want)
	}
	if _, err := os.Stat(e.accounts.AccountDir(account.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("account dir still exists: %v", err)
	}
}

func TestRemoveStillDeletesWhenLogoutFails(t *testing.T) {
	e := newTestEnv(t)
	account := e.addLogin("work")
	t.Setenv("FAKE_CLAUDE_LOGOUT_FAIL", "1")
	warnings, err := e.service.Remove(t.Context(), account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "Claude Code provider cleanup failed") {
		t.Fatalf("warnings = %v", warnings)
	}
	if _, err := os.Stat(e.accounts.AccountDir(account.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("account dir still exists")
	}
}

func TestRemoveTokenProfileSkipsLogout(t *testing.T) {
	e := newTestEnv(t)
	account := e.addToken("ci")
	if _, err := e.service.Remove(t.Context(), account.ID); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(e.calls(), "logout") {
		t.Fatalf("token profile ran logout: %q", e.calls())
	}
}

func TestDescribeTokenExpiry(t *testing.T) {
	e := newTestEnv(t)
	account := e.addToken("ci")
	for _, tc := range []struct {
		in    time.Duration
		state string
		warn  string
	}{
		{in: 10 * 24 * time.Hour, state: "token", warn: "Token expires in"},
		{in: -time.Hour, state: "expired", warn: "Token expired"},
		{in: 100 * 24 * time.Hour, state: "token"},
	} {
		if err := writeToken(tokenPath(e.accounts, account), testToken, time.Now().Add(tc.in-tokenLifetime)); err != nil {
			t.Fatal(err)
		}
		info := e.provider.DescribeAccount(t.Context(), account)
		if info.Options["auth_status"] != tc.state {
			t.Fatalf("state = %v, want %s", info.Options["auth_status"], tc.state)
		}
		if tc.warn == "" && len(info.Warnings) != 0 || tc.warn != "" && (len(info.Warnings) != 1 || !strings.Contains(info.Warnings[0], tc.warn)) {
			t.Fatalf("warnings = %v, want %q", info.Warnings, tc.warn)
		}
		if strings.Contains(strings.Join(info.Warnings, " "), testToken) {
			t.Fatal("warning leaks the token")
		}
	}
	os.Remove(tokenPath(e.accounts, account))
	if info := e.provider.DescribeAccount(t.Context(), account); info.Options["auth_status"] != "missing" || len(info.Warnings) != 1 {
		t.Fatalf("info = %+v", info)
	}
}
