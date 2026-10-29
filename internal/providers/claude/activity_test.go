package claude

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/agentactivity"
	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

func TestTranslateHook(t *testing.T) {
	type sig = agentactivity.Signal
	const (
		toolInput = `"tool_input":{"command":"cat ~/.ssh/id_rsa","note":"SECRET-PROMPT"}`
		toolReply = `"tool_response":{"stdout":"SECRET-OUTPUT"}`
	)
	for _, tc := range []struct {
		name, event, payload string
		want                 []sig
	}{
		{"session start", "SessionStart", `{"source":"startup"}`, []sig{{Kind: agentactivity.SessionStarted}}},
		{"session resume", "SessionStart", `{"source":"resume"}`, []sig{{Kind: agentactivity.SessionStarted}}},
		{"compaction restart is not readiness", "SessionStart", `{"source":"compact"}`, nil},
		{"prompt", "UserPromptSubmit", `{"prompt":"SECRET-PROMPT"}`, []sig{{Kind: agentactivity.PromptSubmitted}}},
		{"tool start", "PreToolUse", `{"tool_name":"Bash",` + toolInput + `}`, []sig{{Kind: agentactivity.ToolStarted, Detail: "Bash"}}},
		{"mcp tool", "PreToolUse", `{"tool_name":"mcp__git__status"}`, []sig{{Kind: agentactivity.ToolStarted, Detail: "mcp__git__status"}}},
		{"question", "PreToolUse", `{"tool_name":"AskUserQuestion",` + toolInput + `}`, []sig{{Kind: agentactivity.InputRequested, Reason: agentactivity.ReasonQuestion}}},
		{"plan", "PreToolUse", `{"tool_name":"ExitPlanMode"}`, []sig{{Kind: agentactivity.InputRequested, Reason: agentactivity.ReasonPlan}}},
		{"tool end", "PostToolUse", `{"tool_name":"Edit",` + toolInput + `,` + toolReply + `}`, []sig{{Kind: agentactivity.ToolFinished, Detail: "Edit"}}},
		{"tool failure", "PostToolUseFailure", `{"tool_name":"Bash"}`, []sig{{Kind: agentactivity.ToolFinished, Detail: "Bash"}}},
		{"question answered", "PostToolUse", `{"tool_name":"AskUserQuestion"}`, []sig{{Kind: agentactivity.InputResolved}}},
		{"plan answered", "PostToolUseFailure", `{"tool_name":"ExitPlanMode"}`, []sig{{Kind: agentactivity.InputResolved}}},
		{"permission", "PermissionRequest", `{"tool_name":"Bash",` + toolInput + `}`, []sig{{Kind: agentactivity.PermissionRequested, Detail: "Bash"}}},
		{"question permission hook", "PermissionRequest", `{"tool_name":"AskUserQuestion",` + toolInput + `}`, []sig{{Kind: agentactivity.InputRequested, Reason: agentactivity.ReasonQuestion}}},
		{"plan permission hook", "PermissionRequest", `{"tool_name":"ExitPlanMode",` + toolInput + `}`, []sig{{Kind: agentactivity.InputRequested, Reason: agentactivity.ReasonPlan}}},
		{"notification permission", "Notification", `{"notification_type":"permission_prompt","message":"SECRET-MSG"}`, []sig{{Kind: agentactivity.PermissionRequested}}},
		{"notification idle", "Notification", `{"notification_type":"idle_prompt"}`, []sig{{Kind: agentactivity.TurnCompleted}}},
		{"notification elicitation", "Notification", `{"notification_type":"elicitation_dialog"}`, []sig{{Kind: agentactivity.InputRequested, Reason: agentactivity.ReasonElicitation}}},
		{"notification needs input", "Notification", `{"notification_type":"agent_needs_input"}`, []sig{{Kind: agentactivity.InputRequested, Reason: agentactivity.ReasonQuestion}}},
		{"notification agent completed", "Notification", `{"notification_type":"agent_completed"}`, nil},
		{"notification unknown", "Notification", `{"notification_type":"auth_success"}`, nil},
		{"notification without type", "Notification", `{}`, nil},
		{"elicitation", "Elicitation", `{"mcp_server_name":"s","message":"SECRET-MSG"}`, []sig{{Kind: agentactivity.InputRequested, Reason: agentactivity.ReasonElicitation}}},
		{"elicitation result", "ElicitationResult", `{"action":"accept","content":{"k":"SECRET"}}`, []sig{{Kind: agentactivity.InputResolved}}},
		{"stop", "Stop", `{"last_assistant_message":"SECRET-REPLY"}`, []sig{{Kind: agentactivity.TurnCompleted}}},
		{"stop failure", "StopFailure", `{"error":"rate_limit","error_details":"SECRET-DETAIL"}`, []sig{{Kind: agentactivity.TurnFailed, Detail: "rate_limit"}}},
		{"session end", "SessionEnd", `{"reason":"logout"}`, []sig{{Kind: agentactivity.SessionEnded}}},
		{"unknown event", "PreCompact", `{}`, nil},
		{"malformed json", "Stop", `{"tool_name":`, nil},
		{"not json", "PreToolUse", `hello`, nil},
		{"empty", "PreToolUse", ``, nil},
		{"wrong shape", "PreToolUse", `[1,2]`, nil},
		{"wrong field type", "PreToolUse", `{"tool_name":7}`, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := (&Provider{}).TranslateHook(tc.event, []byte(tc.payload))
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("signals = %+v, want %+v", got, tc.want)
			}
			encoded, _ := json.Marshal(got)
			if strings.Contains(string(encoded), "SECRET") || strings.Contains(string(encoded), "id_rsa") {
				t.Fatalf("signal leaks payload content: %s", encoded)
			}
		})
	}
}

func TestActivityProfile(t *testing.T) {
	p := &Provider{}
	profile := p.ActivityProfile()
	want := []agentactivity.State{agentactivity.Working, agentactivity.Waiting, agentactivity.Idle}
	if !reflect.DeepEqual(profile.Native, want) || len(profile.Inferred) != 0 || profile.ProgressMeansTurn {
		t.Fatalf("profile = %+v", profile)
	}
	if out := p.HookOutput("Stop"); len(out) != 0 {
		t.Fatalf("HookOutput = %q, want empty", out)
	}
}

func testActivity(command ...string) *agents.ActivityRequest {
	return &agents.ActivityRequest{HookCommand: command, SpoolEnv: agents.ActivityEnv, SpoolPath: "/spool/activity.jsonl"}
}

// settingsArg returns the value of Bonsai's --settings in args and its
// position, or -1 when args carry none of Bonsai's.
func settingsArg(t *testing.T, args []string) (string, int) {
	t.Helper()
	for i, a := range args {
		if a == "--settings" && i+1 < len(args) && strings.Contains(args[i+1], "__agent-hook") {
			return args[i+1], i
		}
	}
	return "", -1
}

func TestActivitySettingsJSON(t *testing.T) {
	args, _, err := launchArgs(Settings{}, nil, agents.LaunchOptions{}, testActivity("/opt/bonsai", "__agent-hook", "claude"))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := settingsArg(t, args)
	var parsed map[string]map[string][]struct {
		Matcher string `json:"matcher"`
		Hooks   []struct {
			Type    string `json:"type"`
			Command string `json:"command"`
			Async   bool   `json:"async"`
			Timeout int    `json:"timeout"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		t.Fatalf("--settings is not valid JSON: %v\n%s", err, raw)
	}
	// Only hooks: no key that could change permissions, models or the user's own hooks settings.
	if len(parsed) != 1 || parsed["hooks"] == nil {
		t.Fatalf("settings keys = %v, want only hooks", keys(parsed))
	}
	events := []string{"SessionStart", "UserPromptSubmit", "PreToolUse", "PermissionRequest", "PostToolUse", "PostToolUseFailure", "Notification", "Elicitation", "ElicitationResult", "Stop", "StopFailure", "SessionEnd"}
	if len(parsed["hooks"]) != len(events) {
		t.Fatalf("events = %v", keys(parsed["hooks"]))
	}
	for _, event := range events {
		groups := parsed["hooks"][event]
		if len(groups) != 1 || len(groups[0].Hooks) != 1 {
			t.Fatalf("%s groups = %+v", event, groups)
		}
		hook := groups[0].Hooks[0]
		if hook.Type != "command" || !hook.Async || hook.Timeout != 5 {
			t.Errorf("%s hook = %+v", event, hook)
		}
		if want := "'/opt/bonsai' '__agent-hook' 'claude' " + event; hook.Command != want {
			t.Errorf("%s command = %q, want %q", event, hook.Command, want)
		}
		wantMatcher := ""
		if event == "PreToolUse" {
			wantMatcher = "*"
		}
		if groups[0].Matcher != wantMatcher {
			t.Errorf("%s matcher = %q", event, groups[0].Matcher)
		}
	}
}

func keys[V any](m map[string]V) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestHookCommandQuoting(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("hook commands run through a POSIX shell")
	}
	// A directory with a space, a quote, a dollar and a backtick must still be one word.
	dir := filepath.Join(t.TempDir(), "my bin'dir $HOME `x`")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(dir, "it's here")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nfor a in \"$@\"; do printf '[%s]' \"$a\"; done\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	line := hookCommandLine([]string{script, "__agent-hook", "claude"}, "PreToolUse")
	out, err := exec.Command("sh", "-c", line).Output()
	if err != nil {
		t.Fatalf("sh -c %q: %v", line, err)
	}
	if got, want := string(out), "[__agent-hook][claude][PreToolUse]"; got != want {
		t.Fatalf("hook args = %q, want %q", got, want)
	}
}

func TestLaunchArgsActivity(t *testing.T) {
	activity := testActivity("/opt/bonsai", "__agent-hook", "claude")
	for _, tc := range []struct {
		name     string
		explicit []string
		launch   agents.LaunchOptions
		activity *agents.ActivityRequest
		hooks    bool
	}{
		{name: "installed", activity: activity, hooks: true},
		{name: "nil request", activity: nil},
		{name: "incomplete request", activity: &agents.ActivityRequest{SpoolEnv: agents.ActivityEnv, SpoolPath: "/x"}},
		{name: "explicit settings", explicit: []string{"--settings", "{}"}, activity: activity},
		{name: "explicit settings equals", explicit: []string{"--settings={}"}, activity: activity},
		{name: "bare", explicit: []string{"--bare"}, activity: activity},
		{name: "subcommand", explicit: []string{"mcp", "list"}, activity: activity},
		{name: "other explicit flags keep hooks", explicit: []string{"--model", "haiku"}, activity: activity, hooks: true},
		{name: "prompt stays last", launch: agents.LaunchOptions{Prompt: "-hello"}, activity: activity, hooks: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args, _, err := launchArgs(Settings{}, tc.explicit, tc.launch, tc.activity)
			if err != nil {
				t.Fatal(err)
			}
			value, at := settingsArg(t, args)
			if (at >= 0) != tc.hooks {
				t.Fatalf("hooks installed = %v, want %v: %q", at >= 0, tc.hooks, args)
			}
			if !tc.hooks {
				return
			}
			if !json.Valid([]byte(value)) {
				t.Fatalf("invalid settings JSON: %s", value)
			}
			if tc.launch.Prompt != "" {
				if n := len(args); args[n-2] != "--" || args[n-1] != tc.launch.Prompt || at > n-3 {
					t.Fatalf("prompt must stay last after --: %q", args)
				}
			}
			for _, explicit := range tc.explicit {
				if !contains(args, explicit) {
					t.Fatalf("explicit arg %q lost: %q", explicit, args)
				}
			}
		})
	}
}

func TestPrepareSessionActivityEnvironment(t *testing.T) {
	e := newTestEnv(t)
	account := e.addLogin("work")
	session, err := e.sessions.Create(account, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.sessions.Cleanup(session) })
	spool := filepath.Join(session.RuntimeDir, agents.ActivitySpoolName)
	t.Setenv(agents.ActivityEnv, "/inherited/elsewhere")

	prepared, err := e.provider.PrepareSession(t.Context(), agents.PrepareSessionRequest{
		Account: account, Session: session,
		Activity: &agents.ActivityRequest{HookCommand: []string{"/opt/bonsai", "__agent-hook", "claude"}, SpoolEnv: agents.ActivityEnv, SpoolPath: spool},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := envMap(prepared.Environment(os.Environ()))[agents.ActivityEnv]; got != spool {
		t.Fatalf("%s = %q, want %q", agents.ActivityEnv, got, spool)
	}
	if _, at := settingsArg(t, prepared.Args); at < 0 {
		t.Fatalf("hooks missing: %q", prepared.Args)
	}
	// The profile's own settings are never written.
	if _, err := os.Stat(filepath.Join(configDir(e.accounts, account), "settings.json")); err == nil {
		t.Fatal("PrepareSession wrote the profile settings.json")
	}

	prepared, _ = e.prepare(account, agents.LaunchOptions{})
	if _, set := prepared.EnvSet[agents.ActivityEnv]; set {
		t.Fatal("spool env set without an activity request")
	}
	if _, at := settingsArg(t, prepared.Args); at >= 0 {
		t.Fatalf("hooks installed without an activity request: %q", prepared.Args)
	}
}
