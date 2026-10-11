package codex

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
		"OPENAI_API_KEY":       "sk-parent",
		"OPENAI_BASE_URL":      "https://gateway.invalid",
		"CODEX_API_KEY":        "parent-key",
		"CODEX_ACCESS_TOKEN":   "parent-token",
		"CODEX_HOME":           "/elsewhere",
		"CODEX_THREAD_ID":      "thread",
		"CODEX_SANDBOX":        "seatbelt",
		"CODEX_CA_CERTIFICATE": "/etc/corp-ca.pem",
		"SSL_CERT_FILE":        "/etc/ssl/corp.pem",
		"GH_TOKEN":             "keep-me",
	} {
		t.Setenv(k, v)
	}
	prepared, _ := e.prepare(account, agents.LaunchOptions{})
	got := envMap(prepared.Environment(os.Environ()))
	for k := range got {
		if k != "CODEX_HOME" && k != "CODEX_CA_CERTIFICATE" && (strings.HasPrefix(k, "CODEX_") || strings.HasPrefix(k, "OPENAI_")) {
			t.Errorf("inherited %s survived", k)
		}
	}
	if got["CODEX_HOME"] != e.profileHome(account) {
		t.Fatalf("CODEX_HOME = %q", got["CODEX_HOME"])
	}
	if got["CODEX_CA_CERTIFICATE"] != "/etc/corp-ca.pem" || got["SSL_CERT_FILE"] != "/etc/ssl/corp.pem" {
		t.Fatal("CA certificate settings must be kept")
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
	if got["BONSAI_AGENT_PROVIDER"] != "codex" || got["BONSAI_AGENT_ACCOUNT_ID"] != string(account.ID) {
		t.Fatalf("bonsai variables missing: %v", got)
	}
}

func TestSessionsShareProfileHomeButNotIDs(t *testing.T) {
	e := newTestEnv(t)
	work, other := e.addLogin("work"), e.addLogin("other")
	a, sa := e.prepare(work, agents.LaunchOptions{})
	b, sb := e.prepare(work, agents.LaunchOptions{})
	c, _ := e.prepare(other, agents.LaunchOptions{})
	if a.EnvSet["CODEX_HOME"] != b.EnvSet["CODEX_HOME"] {
		t.Fatal("sessions of one profile must share CODEX_HOME")
	}
	if a.EnvSet["CODEX_HOME"] == c.EnvSet["CODEX_HOME"] {
		t.Fatal("profiles must not share CODEX_HOME")
	}
	if a.EnvSet["BONSAI_AGENT_SESSION_ID"] != string(sa.ID) || b.EnvSet["BONSAI_AGENT_SESSION_ID"] != string(sb.ID) || sa.ID == sb.ID {
		t.Fatal("session IDs must differ")
	}
	if a.ProviderSessionID != "" {
		t.Fatalf("provider session id = %q, want none", a.ProviderSessionID)
	}
	if a.Dir != sa.WorkDir {
		t.Fatalf("dir = %q, want the worktree %q", a.Dir, sa.WorkDir)
	}
}

func TestMissingHomeFails(t *testing.T) {
	e := newTestEnv(t)
	account := e.addLogin("work")
	if err := os.RemoveAll(e.profileHome(account)); err != nil {
		t.Fatal(err)
	}
	session, _ := e.sessions.Create(account, t.TempDir())
	t.Cleanup(func() { _ = e.sessions.Cleanup(session) })
	_, err := e.provider.PrepareSession(t.Context(), agents.PrepareSessionRequest{Account: account, Session: session})
	if err == nil || !strings.Contains(err.Error(), "remove and add") {
		t.Fatalf("err = %v", err)
	}
}

func TestLaunchArgsMatrix(t *testing.T) {
	base := []string{"-c", storeOverride, "--no-daemon"}
	with := func(rest ...string) []string { return append(append([]string(nil), base...), rest...) }
	for _, tc := range []struct {
		name     string
		settings Settings
		explicit []string
		launch   agents.LaunchOptions
		want     []string
	}{
		{name: "empty", want: base},
		{name: "profile defaults", settings: Settings{Model: "gpt-6.1-sol", Effort: "high", Sandbox: "read-only", Approval: "untrusted"},
			want: with("-m", "gpt-6.1-sol", "-c", `model_reasoning_effort="high"`, "-s", "read-only", "-a", "untrusted")},
		{name: "launch beats profile", settings: Settings{Model: "gpt-5.5", Effort: "low", Sandbox: "read-only", Approval: "untrusted"},
			launch: agents.LaunchOptions{Model: "gpt-6-luna", Effort: "max", Sandbox: "workspace-write", Approval: "on-request"},
			want:   with("-m", "gpt-6-luna", "-c", `model_reasoning_effort="max"`, "-s", "workspace-write", "-a", "on-request")},
		{name: "launch preset replaces the profile pair", settings: Settings{Sandbox: "read-only", Approval: "untrusted"},
			launch: agents.LaunchOptions{Sandbox: SandboxBypass},
			want:   with("--dangerously-bypass-approvals-and-sandbox")},
		{name: "auto preset", launch: agents.LaunchOptions{Sandbox: "workspace-write", Approval: "on-request"},
			want: with("-s", "workspace-write", "-a", "on-request")},
		{name: "full access preset", launch: agents.LaunchOptions{Sandbox: "danger-full-access", Approval: "never"},
			want: with("-s", "danger-full-access", "-a", "never")},
		{name: "bypass", launch: agents.LaunchOptions{Sandbox: SandboxBypass}, want: with("--dangerously-bypass-approvals-and-sandbox")},
		{name: "explicit beats launch", explicit: []string{"--model", "o3", "-c", "model_reasoning_effort=low", "-s", "read-only"},
			launch: agents.LaunchOptions{Model: "gpt-6-luna", Effort: "max", Sandbox: "workspace-write", Approval: "on-request"},
			want:   []string{"--model", "o3", "-c", "model_reasoning_effort=low", "-s", "read-only", "-c", storeOverride, "--no-daemon", "-a", "on-request"}},
		{name: "explicit attached short flags", explicit: []string{"-mo3", "--config=model_reasoning_effort=low", "--sandbox=read-only", "-anever"},
			launch: agents.LaunchOptions{Model: "x", Effort: "max", Sandbox: "workspace-write", Approval: "on-request"},
			want:   []string{"-mo3", "--config=model_reasoning_effort=low", "--sandbox=read-only", "-anever", "-c", storeOverride, "--no-daemon"}},
		{name: "explicit bypass blocks sandbox and approval", explicit: []string{"--dangerously-bypass-approvals-and-sandbox"},
			launch: agents.LaunchOptions{Sandbox: "read-only", Approval: "on-request"},
			want:   []string{"--dangerously-bypass-approvals-and-sandbox", "-c", storeOverride, "--no-daemon"}},
		{name: "explicit sandbox blocks bypass", explicit: []string{"-s", "read-only"}, launch: agents.LaunchOptions{Sandbox: SandboxBypass},
			want: []string{"-s", "read-only", "-c", storeOverride, "--no-daemon"}},
		{name: "explicit no-daemon not repeated", explicit: []string{"--no-daemon"}, want: []string{"--no-daemon", "-c", storeOverride}},
		{name: "prompt last after --", launch: agents.LaunchOptions{Model: "o3", Prompt: "--version: reply PARSED-OK"},
			want: with("-m", "o3", "--", "--version: reply PARSED-OK")},
		{name: "empty prompt has no --", launch: agents.LaunchOptions{Prompt: ""}, want: base},
		{name: "display name is bonsai-only", launch: agents.LaunchOptions{DisplayName: "fix bug"}, want: base},
		{name: "explicit -- keeps its tail last", explicit: []string{"--search", "--", "-x prompt"}, launch: agents.LaunchOptions{Model: "o3"},
			want: []string{"--search", "-c", storeOverride, "--no-daemon", "-m", "o3", "--", "-x prompt"}},
		{name: "subcommand passthrough", explicit: []string{"mcp", "list"}, launch: agents.LaunchOptions{Model: "o3", Sandbox: SandboxBypass},
			want: []string{"-c", storeOverride, "--no-daemon", "mcp", "list"}},
		{name: "login passthrough", explicit: []string{"login", "--device-auth"}, want: []string{"-c", storeOverride, "--no-daemon", "login", "--device-auth"}},
		{name: "resume keeps the session out of the daemon", explicit: []string{"resume", "--last"},
			want: []string{"-c", storeOverride, "--no-daemon", "resume", "--last"}},
		{name: "subcommand no-daemon not repeated", explicit: []string{"resume", "--no-daemon"},
			want: []string{"-c", storeOverride, "resume", "--no-daemon"}},
		{name: "positional prompt", explicit: []string{"fix the bug"}, want: []string{"-c", storeOverride, "--no-daemon", "fix the bug"}},
		{name: "help", explicit: []string{"--help"}, want: []string{"--help", "-c", storeOverride, "--no-daemon"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := launchArgs(tc.settings, tc.explicit, tc.launch)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("args = %q\nwant   %q", got, tc.want)
			}
		})
	}
}

func TestValidateLaunch(t *testing.T) {
	yes := true
	account := agents.Account{ID: "acct_x", Provider: ProviderID, Name: "work"}
	p := &Provider{}
	for _, tc := range []struct {
		name   string
		launch agents.LaunchOptions
		ok     bool
	}{
		{name: "empty", ok: true},
		{name: "all valid", launch: agents.LaunchOptions{Model: "gpt-6.1-sol", Effort: "ultra", Sandbox: "workspace-write", Approval: "on-request", Prompt: "-x", DisplayName: "n"}, ok: true},
		{name: "bypass alone", launch: agents.LaunchOptions{Sandbox: SandboxBypass}, ok: true},
		{name: "approval alone", launch: agents.LaunchOptions{Approval: "never"}, ok: true},
		{name: "bypass with approval", launch: agents.LaunchOptions{Sandbox: SandboxBypass, Approval: "never"}},
		{name: "bad sandbox", launch: agents.LaunchOptions{Sandbox: "yolo"}},
		{name: "bad approval", launch: agents.LaunchOptions{Approval: "on-failure"}},
		{name: "bad effort", launch: agents.LaunchOptions{Effort: "extreme"}},
		{name: "effort injection", launch: agents.LaunchOptions{Effort: `high" sandbox_mode="danger-full-access`}},
		{name: "dash model", launch: agents.LaunchOptions{Model: "--evil"}},
		{name: "control model", launch: agents.LaunchOptions{Model: "a\nb"}},
		{name: "full access", launch: agents.LaunchOptions{FullAccess: &yes}},
		{name: "permission mode", launch: agents.LaunchOptions{PermissionMode: "plan"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := p.ValidateLaunch(account, tc.launch)
			if (err == nil) != tc.ok {
				t.Fatalf("err = %v, want ok=%v", err, tc.ok)
			}
		})
	}
	bad := account
	bad.Settings = []byte(`{"auth_mode":"magic"}`)
	if err := p.ValidateLaunch(bad, agents.LaunchOptions{}); err == nil {
		t.Fatal("invalid profile settings accepted")
	}
}

// Every value the launch dialog can send is one ValidateLaunch accepts.
func TestLaunchFieldsMatchValidation(t *testing.T) {
	account := agents.Account{ID: "acct_x", Provider: ProviderID, Name: "work"}
	p := &Provider{}
	fields := p.LaunchFields()
	if len(fields) != 2 || fields[0].ID != "permissions" || fields[1].ID != "effort" {
		t.Fatalf("fields = %+v", fields)
	}
	permissions, effort := fields[0], fields[1]
	if permissions.Kind != agents.LaunchFieldSelect || permissions.Default != "auto" {
		t.Fatalf("permissions = %+v", permissions)
	}
	var danger []string
	defaultFound := false
	for _, option := range permissions.Options {
		if len(option.Set) == 0 {
			t.Fatalf("preset %q sets nothing", option.Value)
		}
		for key := range option.Set {
			if key != "sandbox" && key != "approval" {
				t.Fatalf("preset %q sets %q", option.Value, key)
			}
		}
		launch := agents.LaunchOptions{Sandbox: option.Set["sandbox"], Approval: option.Set["approval"]}
		if err := p.ValidateLaunch(account, launch); err != nil {
			t.Fatalf("preset %q rejected: %v", option.Value, err)
		}
		if option.Tone == agents.ToneDanger {
			if option.Help == "" {
				t.Fatalf("danger option %q has no help", option.Value)
			}
			danger = append(danger, option.Value)
		}
		defaultFound = defaultFound || option.Value == permissions.Default
	}
	if !defaultFound || !reflect.DeepEqual(danger, []string{"full-access", "bypass"}) {
		t.Fatalf("default found=%v, danger=%v", defaultFound, danger)
	}
	if !effort.PerModel || effort.Options[0].Value != "" || len(effort.Options) != len(reasoningLevel)+1 {
		t.Fatalf("effort = %+v", effort)
	}
	for _, option := range effort.Options {
		if err := p.ValidateLaunch(account, agents.LaunchOptions{Effort: option.Value}); err != nil {
			t.Fatalf("effort %q rejected: %v", option.Value, err)
		}
	}
}

func TestPrepareRejectsInvalidLaunch(t *testing.T) {
	e := newTestEnv(t)
	account := e.addLogin("work")
	session, _ := e.sessions.Create(account, t.TempDir())
	t.Cleanup(func() { _ = e.sessions.Cleanup(session) })
	_, err := e.provider.PrepareSession(t.Context(), agents.PrepareSessionRequest{Account: account, Session: session, Launch: agents.LaunchOptions{Sandbox: "yolo"}})
	if !errors.Is(err, agents.ErrInvalidLaunch) {
		t.Fatalf("err = %v", err)
	}
}

func TestFinalizeIsNoop(t *testing.T) {
	if err := (&Provider{}).FinalizeSession(t.Context(), agents.FinalizeSessionRequest{}); err != nil {
		t.Fatal(err)
	}
}

func TestSettingsValidation(t *testing.T) {
	for raw, ok := range map[string]bool{
		``:                       true,
		`{}`:                     true,
		`{"auth_mode":"device"}`: true,
		`{"auth_mode":"api-key","model":"o3","effort":"high","sandbox":"read-only","approval":"never"}`: true,
		`{"auth_mode":"token"}`:                   false,
		`{"sandbox":"bypass","approval":"never"}`: false,
		`{"effort":"huge"}`:                       false,
		`not json`:                                false,
	} {
		_, err := ParseSettings(agents.Account{Settings: []byte(raw)})
		if (err == nil) != ok {
			t.Errorf("%s: err = %v, want ok=%v", raw, err, ok)
		}
	}
	settings, _ := ParseSettings(agents.Account{})
	if settings.AuthMode != AuthChatGPT {
		t.Fatalf("default auth mode = %q", settings.AuthMode)
	}
}
