package claude

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

const testToken = "sk-ant-oat01-TESTTESTTESTTESTTESTTESTTEST"

type testEnv struct {
	t        *testing.T
	home     string
	log      string
	out      *bytes.Buffer
	accounts *agents.FileAccountStore
	sessions *agents.FileSessionStore
	registry *agents.Registry
	provider *Provider
	service  *agents.AccountService
}

// newTestEnv puts the fake claude first on PATH, points HOME at an empty temp
// dir and builds a provider over temp stores. No real login is ever involved.
func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake claude is a POSIX script")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	if err := os.Unsetenv("CLAUDE_CONFIG_DIR"); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	fixtures, err := filepath.Abs("testdata")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(fixtures, "fakeclaude.sh"), filepath.Join(bin, "claude")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_CLAUDE_FIXTURES", fixtures)
	log := filepath.Join(t.TempDir(), "calls.log")
	t.Setenv("FAKE_CLAUDE_LOG", log)

	accounts, err := agents.NewFileAccountStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sessions, err := agents.NewFileSessionStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	out := &bytes.Buffer{}
	launcher := agents.NewForegroundLauncher(strings.NewReader(""), out, out)
	provider := New(accounts, sessions, launcher, out)
	registry := agents.NewRegistry()
	if err := registry.Register(provider); err != nil {
		t.Fatal(err)
	}
	return &testEnv{
		t: t, home: home, log: log, out: out, accounts: accounts, sessions: sessions, registry: registry, provider: provider,
		service: &agents.AccountService{Store: accounts, Sessions: sessions, Registry: registry, Launcher: launcher},
	}
}

func (e *testEnv) calls() string {
	data, _ := os.ReadFile(e.log)
	return string(data)
}

func (e *testEnv) addLogin(name string) agents.Account {
	e.t.Helper()
	account, err := e.service.Setup(e.t.Context(), ProviderID, name, agents.SetupOptions{})
	if err != nil {
		e.t.Fatalf("setup %s: %v", name, err)
	}
	return account
}

func (e *testEnv) addToken(name string) agents.Account {
	e.t.Helper()
	account, err := e.service.Setup(e.t.Context(), ProviderID, name, agents.SetupOptions{Secret: strings.NewReader(testToken + "\n")})
	if err != nil {
		e.t.Fatalf("setup %s: %v", name, err)
	}
	return account
}

func (e *testEnv) prepare(account agents.Account, launch agents.LaunchOptions, args ...string) (agents.PreparedSession, agents.Session) {
	e.t.Helper()
	session, err := e.sessions.Create(account, e.t.TempDir())
	if err != nil {
		e.t.Fatal(err)
	}
	e.t.Cleanup(func() { _ = e.sessions.Cleanup(session) })
	prepared, err := e.provider.PrepareSession(e.t.Context(), agents.PrepareSessionRequest{Account: account, Session: session, Args: args, Launch: launch})
	if err != nil {
		e.t.Fatalf("prepare: %v", err)
	}
	return prepared, session
}

func envMap(env []string) map[string]string {
	m := map[string]string{}
	for _, entry := range env {
		if k, v, ok := strings.Cut(entry, "="); ok {
			m[k] = v
		}
	}
	return m
}

func dirEntries(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}
