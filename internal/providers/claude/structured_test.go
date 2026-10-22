package claude

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

const (
	testSchema = `{"type":"object","properties":{"a":{"type":"string"}}}`
	secretText = "SECRET-INPUT-7f3a"
)

type structuredFixture struct {
	e       *testEnv
	account agents.Account
	capture string
	reply   string
}

func newStructuredFixture(t *testing.T) *structuredFixture {
	t.Helper()
	e := newTestEnv(t)
	f := &structuredFixture{e: e, account: e.addLogin("assist"), capture: t.TempDir()}
	t.Setenv("FAKE_CLAUDE_CAPTURE", f.capture)
	f.reply = filepath.Join(t.TempDir(), "reply.json")
	t.Setenv("FAKE_CLAUDE_REPLY", f.reply)
	return f
}

func (f *structuredFixture) setReply(t *testing.T, body string) {
	t.Helper()
	if err := os.WriteFile(f.reply, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func (f *structuredFixture) request() agents.StructuredRequest {
	return agents.StructuredRequest{Account: f.account, System: "be brief", Input: []byte(secretText), Schema: json.RawMessage(testSchema)}
}

func (f *structuredFixture) read(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(f.capture, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func (f *structuredFixture) runDirs(t *testing.T) []string {
	t.Helper()
	dir := filepath.Join(f.e.accounts.AccountDir(f.account.ID), "structured")
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

const okReply = `{"type":"result","subtype":"success","is_error":false,"result":"done","structured_output":{"a":"b"}}`

func TestStructuredArgvExact(t *testing.T) {
	f := newStructuredFixture(t)
	f.setReply(t, okReply)
	req := f.request()
	req.Model, req.Effort = "opus", "high"
	got, err := f.e.provider.RunStructured(t.Context(), req)
	if err != nil || string(got) != `{"a":"b"}` {
		t.Fatalf("got %s, %v", got, err)
	}
	want := []string{"-p", "--output-format", "json", "--json-schema", testSchema, "--tools", "", "--restricted",
		"--strict-mcp-config", "--safe-mode", "--no-session-persistence", "--permission-prompts", "none",
		"--model", "opus", "--effort", "high", "--system-prompt", "be brief"}
	argv := strings.Split(strings.TrimSuffix(f.read(t, "argv"), "\n"), "\n")
	if !slices.Equal(argv, want) {
		t.Fatalf("argv = %q\nwant   %q", argv, want)
	}
}

func TestStructuredProfileSettingsFillModelAndEffort(t *testing.T) {
	f := newStructuredFixture(t)
	f.setReply(t, okReply)
	f.account.Settings = []byte(`{"model":"sonnet","effort":"low"}`)
	req := f.request()
	req.Effort = "max"
	if _, err := f.e.provider.RunStructured(t.Context(), req); err != nil {
		t.Fatal(err)
	}
	argv := strings.Split(f.read(t, "argv"), "\n")
	if !containsRun(argv, []string{"--model", "sonnet"}) || !containsRun(argv, []string{"--effort", "max"}) {
		t.Fatalf("argv = %q", argv)
	}
}

func TestStructuredInputOnlyOnStdin(t *testing.T) {
	f := newStructuredFixture(t)
	f.setReply(t, okReply)
	if _, err := f.e.provider.RunStructured(t.Context(), f.request()); err != nil {
		t.Fatal(err)
	}
	if got := f.read(t, "stdin"); got != secretText {
		t.Fatalf("stdin = %q", got)
	}
	if strings.Contains(f.read(t, "argv"), secretText) || strings.Contains(f.read(t, "env"), secretText) {
		t.Fatal("input leaked into argv or env")
	}
}

func TestStructuredEnvScrubbedAndConfigDirSet(t *testing.T) {
	f := newStructuredFixture(t)
	f.setReply(t, okReply)
	t.Setenv("ANTHROPIC_API_KEY", "inherited")
	t.Setenv("CLAUDECODE", "1")
	t.Setenv("CLAUDE_CODE_OAUTH_TOKEN", "inherited")
	if _, err := f.e.provider.RunStructured(t.Context(), f.request()); err != nil {
		t.Fatal(err)
	}
	env := envMap(strings.Split(f.read(t, "env"), "\n"))
	for _, k := range []string{"ANTHROPIC_API_KEY", "CLAUDECODE", "CLAUDE_CODE_OAUTH_TOKEN"} {
		if _, ok := env[k]; ok {
			t.Errorf("%s leaked", k)
		}
	}
	if env["CLAUDE_CONFIG_DIR"] != configDir(f.e.accounts, f.account) {
		t.Errorf("CLAUDE_CONFIG_DIR = %q", env["CLAUDE_CONFIG_DIR"])
	}
	if env["HOME"] != f.e.home {
		t.Errorf("HOME = %q, want unchanged %q", env["HOME"], f.e.home)
	}
}

func TestStructuredTokenProfileGetsToken(t *testing.T) {
	f := newStructuredFixture(t)
	f.account = f.e.addToken("tok")
	f.setReply(t, okReply)
	if _, err := f.e.provider.RunStructured(t.Context(), f.request()); err != nil {
		t.Fatal(err)
	}
	if env := envMap(strings.Split(f.read(t, "env"), "\n")); env["CLAUDE_CODE_OAUTH_TOKEN"] != testToken {
		t.Fatal("token not passed in env")
	}
	if strings.Contains(f.read(t, "argv"), testToken) {
		t.Fatal("token in argv")
	}
}

func TestStructuredWorkDirRemoved(t *testing.T) {
	cases := map[string]struct {
		reply string
		exit  string
		fail  bool
	}{
		"success": {reply: okReply},
		"failure": {reply: okReply, exit: "3", fail: true},
		"garbage": {reply: "nope", fail: true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := newStructuredFixture(t)
			f.setReply(t, tc.reply)
			if tc.exit != "" {
				t.Setenv("FAKE_CLAUDE_EXIT", tc.exit)
			}
			_, err := f.e.provider.RunStructured(t.Context(), f.request())
			if (err != nil) != tc.fail {
				t.Fatalf("err = %v", err)
			}
			accountDir, err := filepath.EvalSymlinks(f.e.accounts.AccountDir(f.account.ID))
			if err != nil {
				t.Fatal(err)
			}
			if cwd := strings.TrimSpace(f.read(t, "cwd")); !strings.HasPrefix(cwd, accountDir+string(filepath.Separator)) {
				t.Errorf("cwd %q outside the profile data", cwd)
			}
			if dirs := f.runDirs(t); len(dirs) != 0 {
				t.Errorf("work dirs left: %v", dirs)
			}
		})
	}
}

func TestStructuredEnvelopes(t *testing.T) {
	cases := []struct {
		name, reply, want string
	}{
		{"structured_output", okReply, `{"a":"b"}`},
		{"result fallback", `{"subtype":"success","is_error":false,"result":" {\"a\": 1} "}`, `{"a":1}`},
		{"structured wins over result", `{"subtype":"success","is_error":false,"result":"{\"x\":1}","structured_output":{"y":2}}`, `{"y":2}`},
		{"is_error true", `{"subtype":"success","is_error":true,"structured_output":{"a":1}}`, ""},
		{"is_error missing", `{"subtype":"success","structured_output":{"a":1}}`, ""},
		{"error subtype", `{"subtype":"error_max_turns","is_error":false,"structured_output":{"a":1}}`, ""},
		{"array answer", `{"subtype":"success","is_error":false,"structured_output":[1]}`, ""},
		{"result prose", `{"subtype":"success","is_error":false,"result":"here you go"}`, ""},
		{"result array", `{"subtype":"success","is_error":false,"result":"[1]"}`, ""},
		{"null answer", `{"subtype":"success","is_error":false,"structured_output":null}`, ""},
		{"two values", okReply + okReply, ""},
		{"not an object", `[1,2]`, ""},
		{"garbage", "total garbage " + secretText, ""},
		{"empty", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newStructuredFixture(t)
			f.setReply(t, tc.reply)
			got, err := f.e.provider.RunStructured(t.Context(), f.request())
			if tc.want == "" {
				if !errors.Is(err, agents.ErrStructuredOutput) {
					t.Fatalf("got %s, %v", got, err)
				}
				if strings.Contains(err.Error(), secretText) || strings.Contains(err.Error(), "garbage") {
					t.Fatalf("error quotes the reply: %v", err)
				}
				return
			}
			if err != nil || string(got) != tc.want {
				t.Fatalf("got %s, %v", got, err)
			}
		})
	}
}

func TestStructuredOversizedStdout(t *testing.T) {
	f := newStructuredFixture(t)
	f.setReply(t, `{"subtype":"success","is_error":false,"structured_output":{"a":"`+strings.Repeat("x", 4096)+`"}}`)
	req := f.request()
	req.MaxOutput = 1024
	_, err := f.e.provider.RunStructured(t.Context(), req)
	if !errors.Is(err, agents.ErrStructuredOutput) {
		t.Fatalf("err = %v", err)
	}
	if dirs := f.runDirs(t); len(dirs) != 0 {
		t.Fatalf("work dirs left: %v", dirs)
	}
}

func TestStructuredNonZeroExit(t *testing.T) {
	f := newStructuredFixture(t)
	f.setReply(t, okReply)
	t.Setenv("FAKE_CLAUDE_EXIT", "1")
	_, err := f.e.provider.RunStructured(t.Context(), f.request())
	if !errors.Is(err, agents.ErrStructuredOutput) {
		t.Fatalf("err = %v", err)
	}
}

func TestStructuredRejectsBadOptionsBeforeRunning(t *testing.T) {
	f := newStructuredFixture(t)
	f.setReply(t, okReply)
	for name, mutate := range map[string]func(*agents.StructuredRequest){
		"model dash":   func(r *agents.StructuredRequest) { r.Model = "--dangerous" },
		"bad effort":   func(r *agents.StructuredRequest) { r.Effort = "extreme" },
		"no schema":    func(r *agents.StructuredRequest) { r.Schema = nil },
		"array schema": func(r *agents.StructuredRequest) { r.Schema = json.RawMessage(`[]`) },
		"no system":    func(r *agents.StructuredRequest) { r.System = "" },
	} {
		req := f.request()
		mutate(&req)
		if _, err := f.e.provider.RunStructured(t.Context(), req); !errors.Is(err, agents.ErrInvalidLaunch) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(f.capture, "argv")); err == nil {
		t.Fatal("the CLI ran")
	}
}

func TestStructuredTokenMissing(t *testing.T) {
	f := newStructuredFixture(t)
	f.account = f.e.addToken("tok")
	if err := removeFile(tokenPath(f.e.accounts, f.account)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.e.provider.RunStructured(t.Context(), f.request()); !errors.Is(err, agents.ErrNotAuthenticated) {
		t.Fatalf("err = %v", err)
	}
}

func TestStructuredVerdict(t *testing.T) {
	e := newTestEnv(t)
	if s := agents.StructuredOf(e.provider); !s.Supported || s.Mechanism == "" || s.Reason != "" {
		t.Fatalf("verdict = %+v", s)
	}
}
