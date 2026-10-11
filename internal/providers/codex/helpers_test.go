package codex

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

const testAPIKey = "sk-proj-TESTTESTTESTTESTTESTTESTTEST"

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

// newTestEnv puts the fake codex first on PATH, points HOME at an empty temp
// dir and builds a provider over temp stores. No real login is ever involved.
func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake codex is a POSIX script")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("CODEX_HOME", "")
	if err := os.Unsetenv("CODEX_HOME"); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	fake, err := filepath.Abs(filepath.Join("testdata", "fakecodex.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(fake, filepath.Join(bin, "codex")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	log := filepath.Join(t.TempDir(), "calls.log")
	t.Setenv("FAKE_CODEX_LOG", log)

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
	scratch := t.TempDir()
	provider.scratchRoot = func() (string, error) { return scratch, nil }
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

func (e *testEnv) add(name string, options agents.SetupOptions) agents.Account {
	e.t.Helper()
	account, err := e.service.Setup(e.t.Context(), ProviderID, name, options)
	if err != nil {
		e.t.Fatalf("setup %s: %v", name, err)
	}
	return account
}

func (e *testEnv) addLogin(name string) agents.Account {
	e.t.Helper()
	return e.add(name, agents.SetupOptions{})
}

func (e *testEnv) addAPIKey(name string) agents.Account {
	e.t.Helper()
	return e.add(name, agents.SetupOptions{Secret: strings.NewReader(testAPIKey + "\n")})
}

func (e *testEnv) profileHome(account agents.Account) string { return homeDir(e.accounts, account) }

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

// treeHash fingerprints every path, mode and content under dir.
func treeHash(t *testing.T, dir string) string {
	t.Helper()
	h := sha256.New()
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		h.Write([]byte(rel + "\x00" + info.Mode().String() + "\x00"))
		if info.Mode().IsRegular() {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			h.Write(data)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func writeFile(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}
