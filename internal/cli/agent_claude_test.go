package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const cliTestToken = "sk-ant-oat01-CLITESTCLITESTCLITESTCLITEST"

// claudeCLIEnv isolates HOME and the Bonsai data dirs and puts the fake claude
// from the provider's testdata first on PATH.
func claudeCLIEnv(t *testing.T) (calls string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake claude is a POSIX script")
	}
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("USERPROFILE", root)
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(root, "cache"))
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	_ = os.Unsetenv("CLAUDE_CONFIG_DIR")
	fixtures, err := filepath.Abs(filepath.Join("..", "providers", "claude", "testdata"))
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	if err := os.Symlink(filepath.Join(fixtures, "fakeclaude.sh"), filepath.Join(bin, "claude")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKE_CLAUDE_FIXTURES", fixtures)
	calls = filepath.Join(t.TempDir(), "calls.log")
	t.Setenv("FAKE_CLAUDE_LOG", calls)
	return calls
}

func runAgent(t *testing.T, stdin string, args ...string) (stdout, stderr string, err error) {
	t.Helper()
	var out, errOut bytes.Buffer
	err = RunWithIO(append([]string{"agent"}, args...), strings.NewReader(stdin), &out, &errOut)
	return out.String(), errOut.String(), err
}

func TestClaudeAccountLifecycleThroughCLI(t *testing.T) {
	calls := claudeCLIEnv(t)

	out, errOut, err := runAgent(t, cliTestToken+"\n", "account", "add", "claude", "work", "--token-stdin")
	if err != nil {
		t.Fatalf("add: %v (stderr=%q)", err, errOut)
	}
	if !strings.Contains(out, `added claude account "work"`) {
		t.Fatalf("add output = %q", out)
	}
	if strings.Contains(out+errOut, cliTestToken) {
		t.Fatalf("token echoed: stdout=%q stderr=%q", out, errOut)
	}

	out, _, err = runAgent(t, "", "account", "list")
	if err != nil {
		t.Fatal(err)
	}
	fields := strings.Fields(out)
	if !strings.Contains(out, "AUTH") || !containsAll(fields, "work", "claude", "token") {
		t.Fatalf("list output = %q", out)
	}

	out, errOut, err = runAgent(t, "", "run", "work", "--", "--help")
	if err != nil {
		t.Fatalf("run: %v (stderr=%q)", err, errOut)
	}
	for _, want := range []string{"ARG:--help", "ARG:--session-id", "HAS_OAUTH_TOKEN=1", "HAS_API_KEY=0"} {
		if !strings.Contains(out, want) {
			t.Errorf("run output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out+errOut, cliTestToken) {
		t.Fatal("token echoed by run")
	}

	out, _, err = runAgent(t, "", "account", "remove", "work")
	if err != nil || !strings.Contains(out, `removed claude account "work"`) {
		t.Fatalf("remove: %q %v", out, err)
	}
	if data, _ := os.ReadFile(calls); strings.Contains(string(data), "logout") {
		t.Fatalf("token profile must not log out: %q", data)
	}
	out, _, _ = runAgent(t, "", "account", "list")
	if strings.Contains(out, "work") {
		t.Fatalf("profile still listed: %q", out)
	}
}

func TestClaudeLoginAccountThroughCLI(t *testing.T) {
	calls := claudeCLIEnv(t)
	t.Setenv("FAKE_CLAUDE_EMAIL", "me@example.com")
	if _, errOut, err := runAgent(t, "", "account", "add", "claude", "personal", "--no-seed"); err != nil {
		t.Fatalf("add: %v (stderr=%q)", err, errOut)
	}
	out, _, err := runAgent(t, "", "account", "list")
	if err != nil || !containsAll(strings.Fields(out), "personal", "login", "me@example.com") {
		t.Fatalf("list = %q, %v", out, err)
	}
	if _, errOut, err := runAgent(t, "", "account", "remove", "personal"); err != nil {
		t.Fatalf("remove: %v (stderr=%q)", err, errOut)
	}
	if data, _ := os.ReadFile(calls); !strings.Contains(string(data), "logout") {
		t.Fatalf("login profile should log out on removal: %q", data)
	}
}

func TestClaudeAccountAddRejectsBadInput(t *testing.T) {
	claudeCLIEnv(t)
	for name, tc := range map[string]struct {
		stdin string
		args  []string
	}{
		"login with token stdin": {cliTestToken, []string{"claude", "w", "--auth", "login", "--token-stdin"}},
		"unknown auth":           {"", []string{"claude", "w", "--auth", "key"}},
		"bad token":              {"not-a-token", []string{"claude", "w", "--token-stdin"}},
		"antigravity token":      {cliTestToken, []string{"antigravity", "w", "--token-stdin"}},
	} {
		t.Run(name, func(t *testing.T) {
			_, errOut, err := runAgent(t, tc.stdin, append([]string{"account", "add"}, tc.args...)...)
			if err == nil {
				t.Fatal("expected error")
			}
			if strings.Contains(err.Error()+errOut, cliTestToken) || strings.Contains(err.Error(), "not-a-token") {
				t.Fatalf("error echoes the token: %v / %q", err, errOut)
			}
		})
	}
}

func TestUsageIgnoresProvidersWithoutUsage(t *testing.T) {
	claudeCLIEnv(t)
	if _, _, err := runAgent(t, cliTestToken, "account", "add", "claude", "work", "--token-stdin"); err != nil {
		t.Fatal(err)
	}
	if _, errOut, err := runAgent(t, "", "usage"); err != nil {
		t.Fatalf("usage failed for a provider without usage: %v (%q)", err, errOut)
	}
}

func containsAll(fields []string, wants ...string) bool {
	for _, want := range wants {
		found := false
		for _, f := range fields {
			if f == want {
				found = true
			}
		}
		if !found {
			return false
		}
	}
	return true
}
