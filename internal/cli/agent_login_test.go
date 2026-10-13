package cli

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

func TestCodexAccountLoginThroughCLI(t *testing.T) {
	calls := codexCLIEnv(t)
	t.Setenv("FAKE_CODEX_EMAIL", "me@example.com")
	if _, errOut, err := runAgent(t, "", "account", "add", "codex", "work", "--auth", "device", "--no-seed"); err != nil {
		t.Fatalf("add: %v (stderr=%q)", err, errOut)
	}
	store, err := agents.NewFileAccountStore("")
	if err != nil {
		t.Fatal(err)
	}
	account, err := store.GetByName("codex", "work")
	if err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(store.AccountDir(account.ID), "home")
	if err := os.Remove(filepath.Join(home, "auth.json")); err != nil {
		t.Fatal(err)
	}
	out, _, err := runAgent(t, "", "account", "show", "work")
	if err != nil || !strings.Contains(out, "Not logged in; run `bonsai agent account login work`") {
		t.Fatalf("show = %q, %v", out, err)
	}

	out, errOut, err := runAgent(t, "", "account", "login", "work")
	if err != nil {
		t.Fatalf("login: %v (stderr=%q)", err, errOut)
	}
	if out != "signed in codex account \"work\"\n" {
		t.Fatalf("login output = %q", out)
	}
	if data, _ := os.ReadFile(calls); strings.Count(string(data), "login device home="+home) != 2 {
		t.Fatalf("calls = %q", data)
	}

	out, errOut, err = runAgent(t, cliTestAPIKey+"\n", "account", "login", "codex/work", "--auth=api-key", "--token-stdin")
	if err != nil {
		t.Fatalf("login api-key: %v (stderr=%q)", err, errOut)
	}
	if strings.Contains(out+errOut, cliTestAPIKey) {
		t.Fatalf("key echoed: %q %q", out, errOut)
	}
	if data, _ := os.ReadFile(filepath.Join(home, "auth.json")); !strings.Contains(string(data), cliTestAPIKey) {
		t.Fatalf("auth.json = %q", data)
	}
	out, _, err = runAgent(t, "", "account", "list")
	if err != nil || !containsAll(strings.Fields(out), "work", "api-key") {
		t.Fatalf("list = %q, %v", out, err)
	}
}

func TestClaudeAccountLoginThroughCLI(t *testing.T) {
	calls := claudeCLIEnv(t)
	t.Setenv("FAKE_CLAUDE_EMAIL", "me@example.com")
	if _, errOut, err := runAgent(t, cliTestToken+"\n", "account", "add", "claude", "work", "--token-stdin", "--no-seed"); err != nil {
		t.Fatalf("add: %v (stderr=%q)", err, errOut)
	}
	const newToken = "sk-ant-oat01-NEWCLINEWCLINEWCLINEWCLI"
	out, errOut, err := runAgent(t, newToken, "account", "login", "work", "--token-stdin")
	if err != nil || out != "signed in claude account \"work\"\n" || strings.Contains(out+errOut, newToken) {
		t.Fatalf("login token: %q %q %v", out, errOut, err)
	}

	out, errOut, err = runAgent(t, "", "account", "login", "work", "--auth", "login")
	if err != nil {
		t.Fatalf("login: %v (stderr=%q)", err, errOut)
	}
	if data, _ := os.ReadFile(calls); !strings.Contains(string(data), "login config=") {
		t.Fatalf("calls = %q", data)
	}
	out, _, err = runAgent(t, "", "account", "list")
	if err != nil || !containsAll(strings.Fields(out), "work", "login", "me@example.com") {
		t.Fatalf("list = %q, %v", out, err)
	}
}

func TestAccountLoginWarnsAboutASharedLogin(t *testing.T) {
	claudeCLIEnv(t)
	t.Setenv("FAKE_CLAUDE_EMAIL", "same@example.com")
	for _, name := range []string{"one", "two"} {
		if _, errOut, err := runAgent(t, "", "account", "add", "claude", name, "--no-seed"); err != nil {
			t.Fatalf("add: %v (stderr=%q)", err, errOut)
		}
	}
	out, _, err := runAgent(t, "", "account", "login", "two")
	if err != nil {
		t.Fatal(err)
	}
	want := "warning: profile \"one\" is already signed in as same@example.com; two profiles on one login share its usage limits\nsigned in claude account \"two\"\n"
	if out != want {
		t.Fatalf("output = %q", out)
	}
}

func TestAccountLoginRejectsBadInput(t *testing.T) {
	codexCLIEnv(t)
	if _, errOut, err := runAgent(t, "", "account", "add", "codex", "work", "--no-seed"); err != nil {
		t.Fatalf("add: %v (stderr=%q)", err, errOut)
	}
	for name, tc := range map[string]struct {
		stdin string
		args  []string
		want  string
	}{
		"no account":      {"", nil, accountLoginUsage},
		"two accounts":    {"", []string{"a", "b"}, accountLoginUsage},
		"unknown flag":    {"", []string{"work", "--no-seed"}, "unknown flag"},
		"auth no value":   {"", []string{"work", "--auth"}, "--auth requires a value"},
		"unknown account": {"", []string{"nope"}, "not found"},
		"unknown mode":    {"", []string{"work", "--auth", "token"}, "unknown auth mode"},
		"empty stdin":     {"", []string{"work", "--token-stdin"}, "no token on standard input"},
		"bad key":         {"not-a-key", []string{"work", "--token-stdin"}, "not a valid OpenAI API key"},
	} {
		t.Run(name, func(t *testing.T) {
			_, _, err := runAgent(t, tc.stdin, append([]string{"account", "login"}, tc.args...)...)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
			if strings.Contains(err.Error(), "not-a-key") {
				t.Fatalf("error echoes the key: %v", err)
			}
		})
	}
}

func TestParseAccountLoginArgs(t *testing.T) {
	selector, options, err := parseAccountLoginArgs([]string{"--auth", "device", "codex/work"}, nil)
	if err != nil || selector != "codex/work" || options.AuthMode != "device" || options.Secret != nil {
		t.Fatalf("got %q %+v %v", selector, options, err)
	}
	selector, options, err = parseAccountLoginArgs([]string{"work", "--token-stdin"}, strings.NewReader("  "+cliTestToken+"\n"))
	if err != nil || selector != "work" || options.Secret == nil {
		t.Fatalf("got %q %+v %v", selector, options, err)
	}
	if data, _ := io.ReadAll(options.Secret); string(data) != cliTestToken {
		t.Fatalf("secret = %q", data)
	}
	if _, _, err := parseAccountLoginArgs([]string{"work", "--token-stdin=x"}, nil); err == nil {
		t.Fatal("--token-stdin=x accepted")
	}
}
