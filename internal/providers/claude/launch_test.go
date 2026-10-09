package claude

import (
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

func TestPrepareScrubsInheritedEnvironment(t *testing.T) {
	e := newTestEnv(t)
	account := e.addLogin("work")
	for k, v := range map[string]string{
		"ANTHROPIC_API_KEY":       "sk-parent",
		"ANTHROPIC_AUTH_TOKEN":    "parent-token",
		"ANTHROPIC_BASE_URL":      "https://gateway.invalid",
		"CLAUDECODE":              "1",
		"CLAUDE_CODE_OAUTH_TOKEN": "parent-oauth",
		"CLAUDE_CODE_USE_BEDROCK": "1",
		"CLAUDE_CONFIG_DIR":       "/elsewhere",
		"GH_TOKEN":                "keep-me",
	} {
		t.Setenv(k, v)
	}
	prepared, _ := e.prepare(account, agents.LaunchOptions{})
	got := envMap(prepared.Environment(os.Environ()))
	for k := range got {
		if k != "CLAUDE_CONFIG_DIR" && (strings.HasPrefix(k, "CLAUDE") || strings.HasPrefix(k, "ANTHROPIC_")) {
			t.Errorf("inherited %s survived", k)
		}
	}
	if got["CLAUDE_CONFIG_DIR"] != configDir(e.accounts, account) {
		t.Fatalf("CLAUDE_CONFIG_DIR = %q", got["CLAUDE_CONFIG_DIR"])
	}
	if got["GH_TOKEN"] != "keep-me" || got["PATH"] == "" {
		t.Fatal("unrelated variables must be kept")
	}
	if got["HOME"] != e.home {
		t.Fatalf("HOME = %q, want it unchanged (%q)", got["HOME"], e.home)
	}
	if _, set := prepared.EnvSet["HOME"]; set {
		t.Fatal("provider must not override HOME")
	}
	if got["BONSAI_AGENT_PROVIDER"] != "claude" || got["BONSAI_AGENT_ACCOUNT_ID"] != string(account.ID) {
		t.Fatalf("bonsai variables missing: %v", got)
	}
}

func TestTokenModeEnvironment(t *testing.T) {
	e := newTestEnv(t)
	account := e.addToken("ci")
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "parent-oauth")
	prepared, _ := e.prepare(account, agents.LaunchOptions{})
	got := envMap(prepared.Environment(os.Environ()))
	if got["CLAUDE_CODE_OAUTH_TOKEN"] != testToken {
		t.Fatal("stored token not injected")
	}
	if got["CLAUDE_CODE_SUBPROCESS_ENV_SCRUB"] != "1" {
		t.Fatal("subprocess scrub flag missing")
	}
	login := e.addLogin("work")
	prepared, _ = e.prepare(login, agents.LaunchOptions{})
	got = envMap(prepared.Environment(os.Environ()))
	if _, ok := got["CLAUDE_CODE_OAUTH_TOKEN"]; ok {
		t.Fatal("login mode must not carry a token")
	}
}

func TestTokenModeMissingTokenIsNotAuthenticated(t *testing.T) {
	e := newTestEnv(t)
	account := e.addToken("ci")
	if err := os.Remove(tokenPath(e.accounts, account)); err != nil {
		t.Fatal(err)
	}
	session, _ := e.sessions.Create(account, t.TempDir())
	_, err := e.provider.PrepareSession(t.Context(), agents.PrepareSessionRequest{Account: account, Session: session})
	if !errors.Is(err, agents.ErrNotAuthenticated) {
		t.Fatalf("err = %v, want ErrNotAuthenticated", err)
	}
}

func TestLoginModeStartsWithoutLogin(t *testing.T) {
	e := newTestEnv(t)
	account := e.addLogin("work")
	if err := os.Remove(configDir(e.accounts, account) + "/.credentials.json"); err != nil {
		t.Fatal(err)
	}
	e.prepare(account, agents.LaunchOptions{}) // Claude shows its own login screen
}

func TestSessionsShareProfileConfigButNotIDs(t *testing.T) {
	e := newTestEnv(t)
	work, other := e.addLogin("work"), e.addLogin("other")
	a, sa := e.prepare(work, agents.LaunchOptions{})
	b, sb := e.prepare(work, agents.LaunchOptions{})
	c, _ := e.prepare(other, agents.LaunchOptions{})
	if a.EnvSet["CLAUDE_CONFIG_DIR"] != b.EnvSet["CLAUDE_CONFIG_DIR"] {
		t.Fatal("sessions of one profile must share the config dir")
	}
	if a.EnvSet["CLAUDE_CONFIG_DIR"] == c.EnvSet["CLAUDE_CONFIG_DIR"] {
		t.Fatal("profiles must not share a config dir")
	}
	if a.EnvSet["BONSAI_AGENT_SESSION_ID"] != string(sa.ID) || b.EnvSet["BONSAI_AGENT_SESSION_ID"] != string(sb.ID) || sa.ID == sb.ID {
		t.Fatal("session IDs must differ")
	}
	if a.ProviderSessionID == "" || a.ProviderSessionID == b.ProviderSessionID {
		t.Fatalf("provider session IDs = %q, %q", a.ProviderSessionID, b.ProviderSessionID)
	}
	if !hasArgPair(a.Args, "--session-id", a.ProviderSessionID) {
		t.Fatalf("args %q lack --session-id %s", a.Args, a.ProviderSessionID)
	}
}

func hasArgPair(args []string, flag, value string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag && args[i+1] == value {
			return true
		}
	}
	return false
}

func TestRealHomeIsNeverWritten(t *testing.T) {
	e := newTestEnv(t)
	account := e.addLogin("work")
	token := e.addToken("ci")
	e.prepare(account, agents.LaunchOptions{Model: "opus"})
	e.prepare(token, agents.LaunchOptions{})
	if _, err := e.provider.Usage(t.Context(), account, agents.UsageOptions{}); err == nil {
		t.Fatal("usage should be unsupported for now")
	}
	e.provider.DescribeAccount(t.Context(), account)
	for _, a := range []agents.Account{account, token} {
		if _, err := e.service.Remove(t.Context(), a.ID); err != nil {
			t.Fatal(err)
		}
	}
	if names := dirEntries(t, e.home); len(names) != 0 {
		t.Fatalf("HOME was written: %v", names)
	}
}

func TestLaunchArgsMatrix(t *testing.T) {
	for _, tc := range []struct {
		name     string
		settings Settings
		explicit []string
		launch   agents.LaunchOptions
		want     []string // without the generated --session-id pair
		noID     bool
	}{
		{name: "empty", want: nil},
		{name: "profile defaults", settings: Settings{Model: "sonnet", PermissionMode: "plan", Effort: "high"},
			want: []string{"--model", "sonnet", "--permission-mode", "plan", "--effort", "high"}},
		{name: "launch beats profile", settings: Settings{Model: "sonnet", PermissionMode: "plan", Effort: "high"},
			launch: agents.LaunchOptions{Model: "opus", PermissionMode: "acceptEdits", Effort: "max"},
			want:   []string{"--model", "opus", "--permission-mode", "acceptEdits", "--effort", "max"}},
		{name: "explicit beats launch", explicit: []string{"--model", "haiku", "--effort=low"},
			launch: agents.LaunchOptions{Model: "opus", Effort: "max", PermissionMode: "plan"},
			want:   []string{"--model", "haiku", "--effort=low", "--permission-mode", "plan"}},
		{name: "explicit skip permissions blocks mode", explicit: []string{"--dangerously-skip-permissions"},
			launch: agents.LaunchOptions{PermissionMode: "plan"}, want: []string{"--dangerously-skip-permissions"}},
		{name: "name", launch: agents.LaunchOptions{DisplayName: "fix bug"}, want: []string{"--name", "fix bug"}},
		{name: "explicit name", explicit: []string{"-n", "mine"}, launch: agents.LaunchOptions{DisplayName: "fix bug"}, want: []string{"-n", "mine"}},
		{name: "prompt last after --", launch: agents.LaunchOptions{Model: "opus", Prompt: "--version: reply"},
			want: []string{"--model", "opus", "<id>", "--", "--version: reply"}},
		{name: "empty prompt has no --", launch: agents.LaunchOptions{Prompt: ""}, want: nil},
		{name: "resume keeps its own id", explicit: []string{"--resume", "abc"}, want: []string{"--resume", "abc"}, noID: true},
		{name: "continue short", explicit: []string{"-c"}, want: []string{"-c"}, noID: true},
		{name: "explicit session id", explicit: []string{"--session-id", "x"}, want: []string{"--session-id", "x"}, noID: true},
		{name: "subcommand passthrough", explicit: []string{"mcp", "list"}, launch: agents.LaunchOptions{Model: "opus"}, want: []string{"mcp", "list"}, noID: true},
		{name: "help", explicit: []string{"--help"}, want: []string{"--help"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args, id, err := launchArgs(tc.settings, tc.explicit, tc.launch)
			if err != nil {
				t.Fatal(err)
			}
			if tc.noID != (id == "") {
				t.Fatalf("provider session id = %q, noID=%v", id, tc.noID)
			}
			var got []string
			for i := 0; i < len(args); i++ {
				if args[i] == "--session-id" && id != "" && args[i+1] == id {
					got = append(got, "<id>")
					i++
					continue
				}
				got = append(got, args[i])
			}
			want := append([]string(nil), tc.want...)
			if !tc.noID && !contains(want, "<id>") {
				// the id pair sits before an optional "--" prompt, after other flags
				if i := indexOf(want, "--"); i >= 0 {
					want = append(want[:i], append([]string{"<id>"}, want[i:]...)...)
				} else {
					want = append(want, "<id>")
				}
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("args = %q, want %q", got, want)
			}
		})
	}
}

func contains(s []string, v string) bool { return indexOf(s, v) >= 0 }

func indexOf(s []string, v string) int {
	for i := range s {
		if s[i] == v {
			return i
		}
	}
	return -1
}

func TestValidateLaunch(t *testing.T) {
	e := newTestEnv(t)
	account := e.addLogin("work")
	yes := true
	for _, tc := range []struct {
		name   string
		launch agents.LaunchOptions
		ok     bool
	}{
		{name: "empty", ok: true},
		{name: "all valid", launch: agents.LaunchOptions{Model: "claude-opus-5-5", PermissionMode: "bypassPermissions", Effort: "xhigh"}, ok: true},
		{name: "bad mode", launch: agents.LaunchOptions{PermissionMode: "yolo"}},
		{name: "bad effort", launch: agents.LaunchOptions{Effort: "extreme"}},
		{name: "dash model", launch: agents.LaunchOptions{Model: "--evil"}},
		{name: "full access", launch: agents.LaunchOptions{FullAccess: &yes}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := e.provider.ValidateLaunch(account, tc.launch)
			if (err == nil) != tc.ok {
				t.Fatalf("err = %v, want ok=%v", err, tc.ok)
			}
		})
	}
}

func TestPrepareRejectsInvalidLaunch(t *testing.T) {
	e := newTestEnv(t)
	account := e.addLogin("work")
	session, _ := e.sessions.Create(account, t.TempDir())
	_, err := e.provider.PrepareSession(t.Context(), agents.PrepareSessionRequest{Account: account, Session: session, Launch: agents.LaunchOptions{Effort: "extreme"}})
	if !errors.Is(err, agents.ErrInvalidLaunch) {
		t.Fatalf("err = %v", err)
	}
}

func TestFinalizeIsNoop(t *testing.T) {
	e := newTestEnv(t)
	if err := e.provider.FinalizeSession(t.Context(), agents.FinalizeSessionRequest{}); err != nil {
		t.Fatal(err)
	}
}
