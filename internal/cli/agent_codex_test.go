package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

const cliTestAPIKey = "sk-proj-CLITESTCLITESTCLITESTCLITEST"

// codexCLIEnv isolates HOME and the Bonsai data dirs and puts the fake codex
// from the provider's testdata first on PATH.
func codexCLIEnv(t *testing.T) (calls string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake codex is a POSIX script")
	}
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("USERPROFILE", root)
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	t.Setenv("CODEX_HOME", "")
	_ = os.Unsetenv("CODEX_HOME")
	t.Setenv("OPENAI_API_KEY", "sk-parent-should-not-leak")
	fake, err := filepath.Abs(filepath.Join("..", "providers", "codex", "testdata", "fakecodex.sh"))
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	if err := os.Symlink(fake, filepath.Join(bin, "codex")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	calls = filepath.Join(t.TempDir(), "calls.log")
	t.Setenv("FAKE_CODEX_LOG", calls)
	return calls
}

func TestCodexAccountLifecycleThroughCLI(t *testing.T) {
	calls := codexCLIEnv(t)

	out, errOut, err := runAgent(t, cliTestAPIKey+"\n", "account", "add", "codex", "work", "--auth", "api-key", "--token-stdin")
	if err != nil {
		t.Fatalf("add: %v (stderr=%q)", err, errOut)
	}
	if !strings.Contains(out, `added codex account "work"`) {
		t.Fatalf("add output = %q", out)
	}
	if strings.Contains(out+errOut, cliTestAPIKey) {
		t.Fatalf("key echoed: stdout=%q stderr=%q", out, errOut)
	}

	out, _, err = runAgent(t, "", "account", "list")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "AUTH") || !strings.Contains(out, "IDENTITY") || !containsAll(strings.Fields(out), "work", "codex", "api-key", "API", "key") {
		t.Fatalf("list output = %q", out)
	}

	// Resolve the profile home through the store: macOS uses Library/Application
	// Support rather than XDG_DATA_HOME.
	store, err := agents.NewFileAccountStore("")
	if err != nil {
		t.Fatal(err)
	}
	account, err := store.GetByName("codex", "work")
	if err != nil {
		t.Fatal(err)
	}
	profileHome := filepath.Join(store.AccountDir(account.ID), "home")

	out, errOut, err = runAgent(t, "", "run", "work", "--", "--help")
	if err != nil {
		t.Fatalf("run: %v (stderr=%q)", err, errOut)
	}
	for _, want := range []string{"ARG:--help", "ARG:-c", `ARG:cli_auth_credentials_store="file"`, "ARG:--no-daemon", "HAS_OPENAI_KEY=0", "HAS_CODEX_KEY=0", "CODEX_HOME=" + profileHome + "\n"} {
		if !strings.Contains(out, want) {
			t.Errorf("run output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out+errOut, cliTestAPIKey) {
		t.Fatal("key echoed by run")
	}

	out, _, err = runAgent(t, "", "account", "remove", "work")
	if err != nil || !strings.Contains(out, `removed codex account "work"`) {
		t.Fatalf("remove: %q %v", out, err)
	}
	if data, _ := os.ReadFile(calls); !strings.Contains(string(data), "logout") {
		t.Fatalf("remove should log the profile out: %q", data)
	}
	out, _, _ = runAgent(t, "", "account", "list")
	if strings.Contains(out, "work") {
		t.Fatalf("profile still listed: %q", out)
	}
}

func TestCodexLoginAccountThroughCLI(t *testing.T) {
	codexCLIEnv(t)
	t.Setenv("FAKE_CODEX_EMAIL", "me@example.com")
	if _, errOut, err := runAgent(t, "", "account", "add", "codex", "personal", "--auth", "device", "--no-seed"); err != nil {
		t.Fatalf("add: %v (stderr=%q)", err, errOut)
	}
	out, _, err := runAgent(t, "", "account", "list")
	if err != nil || !containsAll(strings.Fields(out), "personal", "device", "me@example.com", "plus") {
		t.Fatalf("list = %q, %v", out, err)
	}
}

func TestCodexAccountAddRejectsBadInput(t *testing.T) {
	codexCLIEnv(t)
	for name, tc := range map[string]struct {
		stdin string
		args  []string
	}{
		"chatgpt with key stdin": {cliTestAPIKey, []string{"codex", "w", "--auth", "chatgpt", "--token-stdin"}},
		"unknown auth":           {"", []string{"codex", "w", "--auth", "token"}},
		"bad key":                {"not-a-key", []string{"codex", "w", "--token-stdin"}},
	} {
		t.Run(name, func(t *testing.T) {
			_, errOut, err := runAgent(t, tc.stdin, append([]string{"account", "add"}, tc.args...)...)
			if err == nil {
				t.Fatal("expected error")
			}
			if strings.Contains(err.Error()+errOut, cliTestAPIKey) || strings.Contains(err.Error(), "not-a-key") {
				t.Fatalf("error echoes the key: %v / %q", err, errOut)
			}
		})
	}
}
