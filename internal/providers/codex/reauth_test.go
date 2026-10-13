package codex

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
// under has no launcher or writer of its own, so anything that still used
// construction-time stdio would fail the test.
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

func (e *testEnv) strictService() *agents.AccountService {
	failing := launcherFunc(func(context.Context, agents.PreparedSession) error {
		e.t.Error("service launcher used instead of the request's")
		return errors.New("wrong launcher")
	})
	return &agents.AccountService{Store: e.accounts, Sessions: e.sessions, Registry: e.registry, Launcher: failing, Out: failWriter{e.t}}
}

// assertNoSecret searches every Bonsai file except Codex's own auth.json, the
// captured output and the given texts for secret.
func assertNoSecret(t *testing.T, root, authJSON, secret string, texts ...string) {
	t.Helper()
	for _, text := range texts {
		if strings.Contains(text, secret) {
			t.Fatalf("secret leaked into %q", text)
		}
	}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || path == authJSON {
			return err
		}
		if data, err := os.ReadFile(path); err == nil && strings.Contains(string(data), secret) {
			t.Errorf("secret stored in %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestSetupRunsWithPerRequestIOOnly(t *testing.T) {
	e := newTestEnv(t)
	writeFile(t, filepath.Join(e.home, ".codex", "AGENTS.md"), "rules\n", 0o600)
	service := e.strictService()

	login := &strictIO{}
	result, err := service.SetupWithIO(t.Context(), ProviderID, "work", agents.SetupOptions{AuthMode: AuthDevice}, login.io())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(login.out.String(), "seeded profile from") || !strings.Contains(e.calls(), "login device") {
		t.Fatalf("out = %q, calls = %q", login.out.String(), e.calls())
	}

	key := &strictIO{answer: testAPIKey}
	result, err = service.SetupWithIO(t.Context(), ProviderID, "ci", agents.SetupOptions{AuthMode: AuthAPIKey, Seed: new(bool)}, key.io())
	if err != nil {
		t.Fatal(err)
	}
	home := e.profileHome(result.Account)
	if data, _ := os.ReadFile(authPath(home)); !strings.Contains(string(data), testAPIKey) {
		t.Fatalf("key did not reach codex stdin: %q", data)
	}
	if len(key.prompts) != 1 || key.prompts[0] != "OpenAI API key" {
		t.Fatalf("prompts = %v", key.prompts)
	}
	assertNoSecret(t, e.accounts.Root(), authPath(home), testAPIKey, key.out.String(), e.calls(), string(result.Account.Settings))
}

func TestSetupWithoutTerminalExplainsTokenStdin(t *testing.T) {
	e := newTestEnv(t)
	noTTY := &strictIO{err: agents.ErrNoTerminal}
	_, err := e.strictService().SetupWithIO(t.Context(), ProviderID, "ci", agents.SetupOptions{AuthMode: AuthAPIKey}, noTTY.io())
	if err == nil || err.Error() != "no terminal to read the API key from; pipe it with --token-stdin" {
		t.Fatalf("err = %v", err)
	}
	if accounts, _ := e.accounts.List(); len(accounts) != 0 {
		t.Fatal("failed setup left a profile")
	}
}

func TestSetupPromptErrorHidesAnswer(t *testing.T) {
	e := newTestEnv(t)
	bad := &strictIO{err: errors.New("broken pipe " + testAPIKey)}
	_, err := e.strictService().SetupWithIO(t.Context(), ProviderID, "ci", agents.SetupOptions{AuthMode: AuthAPIKey}, bad.io())
	if err == nil || strings.Contains(err.Error(), testAPIKey) {
		t.Fatalf("err = %v", err)
	}
}

func TestReauthenticateSignsInAgainWithTheProfileMode(t *testing.T) {
	e := newTestEnv(t)
	account := e.add("work", agents.SetupOptions{AuthMode: AuthDevice, Seed: new(bool)})
	home := e.profileHome(account)
	if err := os.Remove(authPath(home)); err != nil {
		t.Fatal(err)
	}
	e.provider.forgetStatus(account.ID)
	info := e.provider.DescribeAccount(t.Context(), account)
	if info.Options["auth_status"] != "logged_out" || len(info.Warnings) != 1 || info.Warnings[0] != "Not logged in; run `bonsai agent account login work`" {
		t.Fatalf("info = %+v", info)
	}
	// An active session does not block signing in again.
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
	if strings.Count(e.calls(), "login device home="+home) != 2 {
		t.Fatalf("calls = %q", e.calls())
	}
	if settings, _ := ParseSettings(result.Account); settings.AuthMode != AuthDevice {
		t.Fatalf("mode changed: %+v", settings)
	}
	if info := e.provider.DescribeAccount(t.Context(), result.Account); info.Options["auth_status"] != "logged_in" || len(info.Warnings) != 0 {
		t.Fatalf("stale status after sign-in: %+v", info)
	}
	if !result.Account.CreatedAt.Equal(account.CreatedAt) || result.Account.Name != "work" {
		t.Fatalf("account = %+v", result.Account)
	}
}

func TestReauthenticateSwitchesToAPIKeyFromPrompt(t *testing.T) {
	e := newTestEnv(t)
	account := e.add("work", agents.SetupOptions{Seed: new(bool)})
	key := &strictIO{answer: " " + testAPIKey + "\n"}
	result, err := e.strictService().Reauthenticate(t.Context(), account.ID, agents.SetupOptions{AuthMode: AuthAPIKey}, key.io())
	if err != nil {
		t.Fatal(err)
	}
	home := e.profileHome(account)
	if data, _ := os.ReadFile(authPath(home)); !strings.Contains(string(data), `"`+testAPIKey+`"`) {
		t.Fatalf("auth.json = %q", data)
	}
	stored, _ := e.accounts.Get(account.ID)
	if settings, _ := ParseSettings(stored); settings.AuthMode != AuthAPIKey {
		t.Fatalf("stored mode = %q", settings.AuthMode)
	}
	assertNoSecret(t, e.accounts.Root(), authPath(home), testAPIKey, key.out.String(), e.calls(), string(result.Account.Settings))
}

func TestReauthenticateWithKeyOnStdinImpliesAPIKey(t *testing.T) {
	e := newTestEnv(t)
	account := e.add("work", agents.SetupOptions{Seed: new(bool)})
	result, err := e.strictService().Reauthenticate(t.Context(), account.ID, agents.SetupOptions{Secret: strings.NewReader(testAPIKey)}, (&strictIO{}).io())
	if err != nil {
		t.Fatal(err)
	}
	if settings, _ := ParseSettings(result.Account); settings.AuthMode != AuthAPIKey {
		t.Fatalf("mode = %q", settings.AuthMode)
	}
}

func TestReauthenticateFailureKeepsTheProfile(t *testing.T) {
	e := newTestEnv(t)
	account := e.add("work", agents.SetupOptions{Seed: new(bool)})
	t.Setenv("FAKE_CODEX_LOGIN_FAIL", "1")
	if _, err := e.strictService().Reauthenticate(t.Context(), account.ID, agents.SetupOptions{AuthMode: AuthDevice}, (&strictIO{}).io()); err == nil {
		t.Fatal("expected error")
	}
	stored, err := e.accounts.Get(account.ID)
	if err != nil {
		t.Fatal(err)
	}
	if settings, _ := ParseSettings(stored); settings.AuthMode != AuthChatGPT {
		t.Fatalf("failed sign-in changed the mode: %q", settings.AuthMode)
	}
	if _, err := os.Stat(e.profileHome(account)); err != nil {
		t.Fatal("failed sign-in removed the profile home")
	}
}

func TestReauthenticateRejectsSeeding(t *testing.T) {
	e := newTestEnv(t)
	account := e.addLogin("work")
	for _, options := range []agents.SetupOptions{{Seed: new(bool)}, {SeedFrom: e.home}} {
		if _, err := e.strictService().Reauthenticate(t.Context(), account.ID, options, (&strictIO{}).io()); err == nil || !strings.Contains(err.Error(), "new profiles") {
			t.Fatalf("err = %v", err)
		}
	}
}

func TestReauthenticateWarnsAboutASharedLogin(t *testing.T) {
	e := newTestEnv(t)
	t.Setenv("FAKE_CODEX_EMAIL", "same@example.com")
	e.addLogin("one")
	t.Setenv("FAKE_CODEX_EMAIL", "other@example.com")
	two := e.addLogin("two")
	t.Setenv("FAKE_CODEX_EMAIL", "same@example.com")
	result, err := e.strictService().Reauthenticate(t.Context(), two.ID, agents.SetupOptions{}, (&strictIO{}).io())
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Warnings) != 1 || !strings.Contains(result.Warnings[0], `profile "one" is already signed in as same@example.com`) {
		t.Fatalf("warnings = %v", result.Warnings)
	}
}

func TestSetupSpec(t *testing.T) {
	e := newTestEnv(t)
	t.Setenv("WSL_DISTRO_NAME", "")
	t.Setenv("SSH_CONNECTION", "")
	spec := e.provider.SetupSpec(t.Context())
	if len(spec.AuthModes) != 3 || spec.Seed == nil || spec.Seed.Source != filepath.Join("~", ".codex") {
		t.Fatalf("spec = %+v", spec)
	}
	modes := map[string]agents.AuthModeOption{}
	for _, mode := range spec.AuthModes {
		modes[mode.Value] = mode
	}
	if !modes[AuthChatGPT].Recommended || modes[AuthDevice].Recommended {
		t.Fatalf("local host should recommend the browser: %+v", spec.AuthModes)
	}
	if s := modes[AuthAPIKey].Secret; s == nil || s.Label != "OpenAI API key" || s.Optional {
		t.Fatalf("api-key secret = %+v", s)
	}
	for _, env := range []string{"WSL_DISTRO_NAME", "SSH_CONNECTION"} {
		t.Run(env, func(t *testing.T) {
			t.Setenv(env, "x")
			for _, mode := range e.provider.SetupSpec(t.Context()).AuthModes {
				if mode.Recommended != (mode.Value == AuthDevice) {
					t.Fatalf("%s: %+v", env, mode)
				}
			}
		})
	}

	t.Setenv("CODEX_HOME", filepath.Join(e.home, "codex-alt"))
	if got := e.provider.SetupSpec(t.Context()).Seed.Source; got != filepath.Join("~", "codex-alt") {
		t.Fatalf("seed source = %q", got)
	}
	elsewhere := t.TempDir()
	t.Setenv("CODEX_HOME", elsewhere)
	if got := e.provider.SetupSpec(t.Context()).Seed.Source; got != elsewhere {
		t.Fatalf("seed source = %q", got)
	}
}
