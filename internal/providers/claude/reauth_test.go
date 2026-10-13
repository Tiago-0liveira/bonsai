package claude

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

// strictIO is a per-request SetupIO with captured output. The service it runs
// under has no usable launcher or writer of its own.
type strictIO struct {
	out     bytes.Buffer
	prompts []string
	answer  string
	err     error
}

func (s *strictIO) io() agents.SetupIO {
	return agents.SetupIO{
		Launcher: agents.NewForegroundLauncher(strings.NewReader(""), &s.out, &s.out),
		Out:      &s.out,
		Prompt: promptFunc(func(label string) (string, error) {
			s.prompts = append(s.prompts, label)
			return s.answer, s.err
		}),
	}
}

type failWriter struct{ t *testing.T }

func (w failWriter) Write(p []byte) (int, error) {
	w.t.Errorf("construction-time writer used: %q", p)
	return len(p), nil
}

type launcherFunc func(context.Context, agents.PreparedSession) error

func (f launcherFunc) RunForeground(ctx context.Context, p agents.PreparedSession) error {
	return f(ctx, p)
}

func (e *testEnv) strictService() *agents.AccountService {
	failing := launcherFunc(func(context.Context, agents.PreparedSession) error {
		e.t.Error("service launcher used instead of the request's")
		return errors.New("wrong launcher")
	})
	return &agents.AccountService{Store: e.accounts, Sessions: e.sessions, Registry: e.registry, Launcher: failing, Out: failWriter{e.t}}
}

// assertNoToken searches every Bonsai file except the token file, plus the
// given texts, for token.
func assertNoToken(t *testing.T, root, tokenFile, token string, texts ...string) {
	t.Helper()
	for _, text := range texts {
		if strings.Contains(text, token) {
			t.Fatalf("token leaked into %q", text)
		}
	}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || path == tokenFile {
			return err
		}
		if data, err := os.ReadFile(path); err == nil && strings.Contains(string(data), token) {
			t.Errorf("token stored in %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestSetupRunsWithPerRequestIOOnly(t *testing.T) {
	e := newTestEnv(t)
	if err := os.MkdirAll(filepath.Join(e.home, ".claude"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(e.home, ".claude", "CLAUDE.md"), []byte("rules\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	service := e.strictService()

	login := &strictIO{}
	if _, err := service.SetupWithIO(t.Context(), ProviderID, "work", agents.SetupOptions{}, login.io()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(login.out.String(), "seeded profile from") || !strings.Contains(e.calls(), "login config=") {
		t.Fatalf("out = %q, calls = %q", login.out.String(), e.calls())
	}

	token := &strictIO{answer: testToken}
	result, err := service.SetupWithIO(t.Context(), ProviderID, "ci", agents.SetupOptions{AuthMode: AuthToken, Seed: new(bool)}, token.io())
	if err != nil {
		t.Fatal(err)
	}
	if len(token.prompts) != 1 || token.prompts[0] != "token" {
		t.Fatalf("prompts = %v", token.prompts)
	}
	// setup-token ran in the request's terminal before the prompt.
	if !strings.Contains(token.out.String(), "sk-ant-oat01-FAKE") {
		t.Fatalf("setup-token output = %q", token.out.String())
	}
	file := tokenPath(e.accounts, result.Account)
	if stored, err := readToken(file); err != nil || stored.Token != testToken {
		t.Fatalf("token = %+v, %v", stored, err)
	}
	assertNoToken(t, e.accounts.Root(), file, testToken, token.out.String(), e.calls(), string(result.Account.Settings))
}

func TestSetupWithoutTerminalExplainsTokenStdin(t *testing.T) {
	e := newTestEnv(t)
	noTTY := &strictIO{err: agents.ErrNoTerminal}
	_, err := e.strictService().SetupWithIO(t.Context(), ProviderID, "ci", agents.SetupOptions{AuthMode: AuthToken}, noTTY.io())
	if err == nil || err.Error() != "no terminal to read the token from; pipe it with --token-stdin" {
		t.Fatalf("err = %v", err)
	}
}

func TestReauthenticateLoginProfile(t *testing.T) {
	e := newTestEnv(t)
	account := e.addLogin("work")
	if err := os.Remove(filepath.Join(configDir(e.accounts, account), ".credentials.json")); err != nil {
		t.Fatal(err)
	}
	e.provider.forgetStatus(account.ID)
	info := e.provider.DescribeAccount(t.Context(), account)
	if info.Options["auth_status"] != "logged_out" || len(info.Warnings) != 1 || info.Warnings[0] != "Not logged in; run `bonsai agent account login work`" {
		t.Fatalf("info = %+v", info)
	}
	running, err := e.sessions.Create(account, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer e.sessions.Cleanup(running)

	login := &strictIO{}
	result, err := e.strictService().Reauthenticate(t.Context(), account.ID, agents.SetupOptions{}, login.io())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(e.calls(), "login config="+configDir(e.accounts, account)) != 2 {
		t.Fatalf("calls = %q", e.calls())
	}
	if info := e.provider.DescribeAccount(t.Context(), result.Account); info.Options["auth_status"] != "logged_in" || len(info.Warnings) != 0 {
		t.Fatalf("stale status after sign-in: %+v", info)
	}
	if settings, _ := ParseSettings(result.Account); settings.AuthMode != AuthLogin {
		t.Fatalf("mode = %q", settings.AuthMode)
	}
}

func TestReauthenticateTokenProfileRunsSetupTokenThenPrompts(t *testing.T) {
	e := newTestEnv(t)
	account := e.addToken("ci")
	const newToken = "sk-ant-oat01-NEWNEWNEWNEWNEWNEWNEWNEWNEW"
	token := &strictIO{answer: newToken + "\n"}
	if _, err := e.strictService().Reauthenticate(t.Context(), account.ID, agents.SetupOptions{}, token.io()); err != nil {
		t.Fatal(err)
	}
	file := tokenPath(e.accounts, account)
	if stored, err := readToken(file); err != nil || stored.Token != newToken {
		t.Fatalf("token = %+v, %v", stored, err)
	}
	if !strings.Contains(token.out.String(), "sk-ant-oat01-FAKE") || len(token.prompts) != 1 {
		t.Fatalf("out = %q, prompts = %v", token.out.String(), token.prompts)
	}
	assertNoToken(t, e.accounts.Root(), file, newToken, token.out.String(), e.calls())
}

func TestReauthenticateSwitchesModes(t *testing.T) {
	e := newTestEnv(t)
	account := e.addToken("ci")
	file := tokenPath(e.accounts, account)

	result, err := e.strictService().Reauthenticate(t.Context(), account.ID, agents.SetupOptions{AuthMode: AuthLogin}, (&strictIO{}).io())
	if err != nil {
		t.Fatal(err)
	}
	if settings, _ := ParseSettings(result.Account); settings.AuthMode != AuthLogin {
		t.Fatalf("mode = %q", settings.AuthMode)
	}
	if _, err := os.Stat(file); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("token file kept after switching to login: %v", err)
	}

	result, err = e.strictService().Reauthenticate(t.Context(), account.ID, agents.SetupOptions{Secret: strings.NewReader(testToken)}, (&strictIO{}).io())
	if err != nil {
		t.Fatal(err)
	}
	if settings, _ := ParseSettings(result.Account); settings.AuthMode != AuthToken {
		t.Fatalf("mode = %q", settings.AuthMode)
	}
	if stored, err := readToken(file); err != nil || stored.Token != testToken {
		t.Fatalf("token = %+v, %v", stored, err)
	}
}

func TestReauthenticateFailureKeepsTheToken(t *testing.T) {
	e := newTestEnv(t)
	account := e.addToken("ci")
	t.Setenv("FAKE_CLAUDE_LOGIN_FAIL", "1")
	if _, err := e.strictService().Reauthenticate(t.Context(), account.ID, agents.SetupOptions{AuthMode: AuthLogin}, (&strictIO{}).io()); err == nil {
		t.Fatal("expected error")
	}
	if stored, err := readToken(tokenPath(e.accounts, account)); err != nil || stored.Token != testToken {
		t.Fatalf("failed sign-in lost the token: %+v, %v", stored, err)
	}
	stored, _ := e.accounts.Get(account.ID)
	if settings, _ := ParseSettings(stored); settings.AuthMode != AuthToken {
		t.Fatalf("failed sign-in changed the mode: %q", settings.AuthMode)
	}
}

func TestReauthenticateWarnsAboutASharedLogin(t *testing.T) {
	e := newTestEnv(t)
	t.Setenv("FAKE_CLAUDE_EMAIL", "same@example.com")
	e.addLogin("one")
	t.Setenv("FAKE_CLAUDE_EMAIL", "other@example.com")
	two := e.addLogin("two")
	t.Setenv("FAKE_CLAUDE_EMAIL", "same@example.com")
	result, err := e.strictService().Reauthenticate(t.Context(), two.ID, agents.SetupOptions{}, (&strictIO{}).io())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], `profile "one" is already signed in as same@example.com`) {
		t.Fatalf("warnings = %v", result.Warnings)
	}
}

func TestDescribeTokenWarningsPointAtLogin(t *testing.T) {
	e := newTestEnv(t)
	account := e.addToken("ci")
	if err := os.Remove(tokenPath(e.accounts, account)); err != nil {
		t.Fatal(err)
	}
	info := e.provider.DescribeAccount(t.Context(), account)
	if len(info.Warnings) != 1 || info.Warnings[0] != "Token missing; run `bonsai agent account login ci`" {
		t.Fatalf("warnings = %v", info.Warnings)
	}
}

func TestSetupSpec(t *testing.T) {
	e := newTestEnv(t)
	spec := e.provider.SetupSpec(t.Context())
	if len(spec.AuthModes) != 2 || spec.Seed == nil || spec.Seed.Source != filepath.Join("~", ".claude") {
		t.Fatalf("spec = %+v", spec)
	}
	if token := spec.AuthModes[1]; token.Value != AuthToken || token.Secret == nil || !token.Secret.Optional {
		t.Fatalf("token mode = %+v", token)
	}
	if !spec.AuthModes[0].Recommended || spec.AuthModes[0].Secret != nil {
		t.Fatalf("login mode = %+v", spec.AuthModes[0])
	}
	elsewhere := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", elsewhere)
	if got := e.provider.SetupSpec(t.Context()).Seed.Source; got != elsewhere {
		t.Fatalf("seed source = %q", got)
	}
}
