package codex

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/Tiago-0liveira/bonsai/internal/core/agentactivity"
	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

func testRequest(bonsai string) *agents.ActivityRequest {
	return &agents.ActivityRequest{
		HookCommand: []string{bonsai, "__agent-hook", "codex"}, SpoolEnv: agents.ActivityEnv, SpoolPath: "/spool/s1/activity.jsonl",
	}
}

// hooksDoc decodes a hooks.json into event -> groups -> handler commands.
func hooksDoc(t *testing.T, data []byte) map[string][][]string {
	t.Helper()
	var doc struct {
		Hooks map[string][]struct {
			Matcher string `json:"matcher"`
			Hooks   []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("hooks.json is not valid JSON: %v\n%s", err, data)
	}
	out := map[string][][]string{}
	for event, groups := range doc.Hooks {
		for _, g := range groups {
			var commands []string
			for _, h := range g.Hooks {
				commands = append(commands, h.Command)
			}
			out[event] = append(out[event], commands)
		}
	}
	return out
}

func TestUpsertHooksCreatesEveryEventOnce(t *testing.T) {
	out, err := upsertHooks(nil, testRequest("/opt/bonsai"))
	if err != nil {
		t.Fatal(err)
	}
	doc := hooksDoc(t, out)
	if len(doc) != len(hookEvents) {
		t.Fatalf("events %v", doc)
	}
	var raw struct {
		Hooks map[string][]struct {
			Hooks []commandHook `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		t.Fatal(err)
	}
	for _, event := range hookEvents {
		groups := raw.Hooks[event.name]
		if len(groups) != 1 || len(groups[0].Hooks) != 1 {
			t.Fatalf("%s: %+v", event.name, groups)
		}
		h := groups[0].Hooks[0]
		want := commandHook{Type: "command", Command: "'/opt/bonsai' '__agent-hook' 'codex' " + event.name, Async: !event.sync, Timeout: 5}
		if h != want {
			t.Fatalf("%s: %+v, want %+v", event.name, h, want)
		}
	}
	if !strings.Contains(string(out), `"async": true`) || strings.Count(string(out), `"async"`) != len(hookEvents)-2 {
		t.Fatalf("async flags:\n%s", out)
	}
}

func TestUpsertHooksKeepsForeignEntries(t *testing.T) {
	existing := `{
  "version": 3,
  "hooks": {
    "PreToolUse": [
      {"matcher": "Bash", "hooks": [{"type": "command", "command": "/usr/bin/audit", "timeout": 9, "statusMessage": "auditing"}]}
    ],
    "PostCompact": [{"hooks": [{"type": "command", "command": "/usr/bin/after-compact"}]}],
    "Stop": [
      {"hooks": [{"type": "command", "command": "'/old/bonsai' '__agent-hook' 'codex' Stop"}, {"type": "command", "command": "/usr/bin/notify"}]}
    ]
  }
}`
	out, err := upsertHooks([]byte(existing), testRequest("/new/bonsai"))
	if err != nil {
		t.Fatal(err)
	}
	doc := hooksDoc(t, out)
	if want := [][]string{{"/usr/bin/audit"}, {"'/new/bonsai' '__agent-hook' 'codex' PreToolUse"}}; !reflect.DeepEqual(doc["PreToolUse"], want) {
		t.Fatalf("PreToolUse %v, want %v", doc["PreToolUse"], want)
	}
	if want := [][]string{{"/usr/bin/after-compact"}}; !reflect.DeepEqual(doc["PostCompact"], want) {
		t.Fatalf("PostCompact %v", doc["PostCompact"])
	}
	// The foreign handler stays, in a group of its own; ours follows it.
	if want := [][]string{{"/usr/bin/notify"}, {"'/new/bonsai' '__agent-hook' 'codex' Stop"}}; !reflect.DeepEqual(doc["Stop"], want) {
		t.Fatalf("Stop %v, want %v", doc["Stop"], want)
	}
	for _, kept := range []string{`"version": 3`, `"matcher": "Bash"`, `"statusMessage": "auditing"`, `"timeout": 9`} {
		if !strings.Contains(string(out), kept) {
			t.Fatalf("lost %s:\n%s", kept, out)
		}
	}
	if strings.Contains(string(out), "/old/bonsai") {
		t.Fatalf("old Bonsai entry kept:\n%s", out)
	}
}

func TestUpsertHooksIsIdempotentAndKeepsPosition(t *testing.T) {
	first, err := upsertHooks(nil, testRequest("/opt/bonsai"))
	if err != nil {
		t.Fatal(err)
	}
	// A foreign group added after ours must not move ours on the next launch:
	// Codex derives its hook keys, and the trust stored under them, from positions.
	var doc map[string]json.RawMessage
	_ = json.Unmarshal(first, &doc)
	var events map[string][]json.RawMessage
	_ = json.Unmarshal(doc["hooks"], &events)
	events["Stop"] = append(events["Stop"], json.RawMessage(`{"hooks":[{"type":"command","command":"/usr/bin/late"}]}`))
	doc["hooks"], _ = json.Marshal(events)
	withForeign, _ := json.MarshalIndent(doc, "", "  ")

	again, err := upsertHooks(withForeign, testRequest("/opt/bonsai"))
	if err != nil {
		t.Fatal(err)
	}
	if want := [][]string{{"'/opt/bonsai' '__agent-hook' 'codex' Stop"}, {"/usr/bin/late"}}; !reflect.DeepEqual(hooksDoc(t, again)["Stop"], want) {
		t.Fatalf("Stop %v, want %v", hooksDoc(t, again)["Stop"], want)
	}
	third, err := upsertHooks(again, testRequest("/opt/bonsai"))
	if err != nil {
		t.Fatal(err)
	}
	if string(third) != string(again) {
		t.Fatalf("not idempotent:\n%s\nvs\n%s", again, third)
	}
	if again2, _ := upsertHooks(first, testRequest("/opt/bonsai")); string(again2) != string(first) {
		t.Fatalf("a file Bonsai wrote changed:\n%s\nvs\n%s", first, again2)
	}
}

func TestUpsertHooksReplacesBonsaiEntriesOfAnotherPath(t *testing.T) {
	first, err := upsertHooks(nil, testRequest("/old path/it's bonsai"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(first), `'/old path/it'\\''s bonsai' '__agent-hook' 'codex' Stop`) {
		t.Fatalf("path not quoted as one word:\n%s", first)
	}
	next, err := upsertHooks(first, testRequest("/new/bonsai"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(next), "old path") {
		t.Fatalf("old path kept:\n%s", next)
	}
	for event, groups := range hooksDoc(t, next) {
		if len(groups) != 1 || len(groups[0]) != 1 {
			t.Fatalf("%s: %v", event, groups)
		}
	}
}

func TestUpsertHooksDropsBonsaiEntriesOfEventsItNoLongerObserves(t *testing.T) {
	existing := `{"hooks":{"SubagentStop":[{"hooks":[{"type":"command","command":"'/b' '__agent-hook' 'codex' SubagentStop"}]}]}}`
	out, err := upsertHooks([]byte(existing), testRequest("/b"))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := hooksDoc(t, out)["SubagentStop"]; ok {
		t.Fatalf("stale entry kept:\n%s", out)
	}
}

func TestUpsertHooksLeavesAnotherProvidersCommandsAlone(t *testing.T) {
	existing := `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"'/b' '__agent-hook' 'claude' Stop"}]}]}}`
	out, err := upsertHooks([]byte(existing), testRequest("/b"))
	if err != nil {
		t.Fatal(err)
	}
	if want := [][]string{{"'/b' '__agent-hook' 'claude' Stop"}, {"'/b' '__agent-hook' 'codex' Stop"}}; !reflect.DeepEqual(hooksDoc(t, out)["Stop"], want) {
		t.Fatalf("Stop %v", hooksDoc(t, out)["Stop"])
	}
}

func TestUpsertHooksRefusesUnreadableFiles(t *testing.T) {
	for _, existing := range []string{`{not json`, `{"hooks": []}`, `{"hooks": {"Stop": "x"}}`, `[]`} {
		if out, err := upsertHooks([]byte(existing), testRequest("/b")); err == nil {
			t.Errorf("%q accepted: %s", existing, out)
		}
	}
	for _, existing := range []string{``, "  \n", `{}`, `{"hooks": null}`} {
		if _, err := upsertHooks([]byte(existing), testRequest("/b")); err != nil {
			t.Errorf("%q: %v", existing, err)
		}
	}
}

// A profile hooks.json is the user's file: whatever shape it has, installing
// hooks must not panic, and a shape that cannot hold hooks is an error that
// leaves the file untouched.
func TestUpsertHooksOddShapes(t *testing.T) {
	refused := []string{`null`, " null\n", `[]`, `"x"`, `1`, `{"hooks":[]}`, `{"hooks":"x"}`, `{"hooks":{"Stop":{}}}`, `{"hooks":{"Stop":"x"}}`}
	for _, existing := range refused {
		t.Run("refuse "+strings.TrimSpace(existing), func(t *testing.T) {
			out, err := upsertHooks([]byte(existing), testRequest("/b"))
			if err == nil {
				t.Fatalf("accepted: %s", out)
			}
		})
	}
	accepted := []string{
		`{"hooks":null}`, `{"hooks":{"SessionStart":null}}`, `{"hooks":{"Stop":[]}}`,
		`{"hooks":{"Stop":[null,1,"x",[]]}}`, `{"hooks":{"Stop":[{"hooks":null}]}}`, `{"hooks":{"Stop":[{"hooks":[1,null,"x"]}]}}`,
		`{"hooks":{"Stop":[{"hooks":{}}]}}`,
	}
	for _, existing := range accepted {
		t.Run("accept "+existing, func(t *testing.T) {
			out, err := upsertHooks([]byte(existing), testRequest("/b"))
			if err != nil {
				t.Fatal(err)
			}
			// Odd foreign entries stay as they were, so the output is checked
			// loosely: valid JSON with exactly one Bonsai handler per event.
			if !json.Valid(out) {
				t.Fatalf("not JSON: %s", out)
			}
			for _, event := range hookEvents {
				if n := strings.Count(string(out), "'__agent-hook' 'codex' "+event.name+`"`); n != 1 {
					t.Errorf("%s: %d Bonsai handlers in %s", event.name, n, out)
				}
			}
		})
	}
}

func TestWriteActivityHooksLeavesRefusedFileAlone(t *testing.T) {
	for _, existing := range []string{`null`, `[]`, `{"hooks":[]}`} {
		home := t.TempDir()
		if err := os.WriteFile(hooksPath(home), []byte(existing), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := writeActivityHooks(home, testRequest("/b")); err == nil {
			t.Errorf("%q: no error", existing)
		}
		if data, _ := os.ReadFile(hooksPath(home)); string(data) != existing {
			t.Errorf("%q rewritten as %q", existing, data)
		}
	}
}

func TestIsBonsaiCommand(t *testing.T) {
	for command, want := range map[string]bool{
		"'/x/bonsai' '__agent-hook' 'codex' Stop":     true,
		"'/x y/it'\\''s' '__agent-hook' 'codex' Stop": true,
		"'/x/bonsai' '__agent-hook' 'claude' Stop":    false,
		"/x/bonsai __agent-hook codex Stop":           false,
		"echo '__agent-hook' 'codex' Stop; rm -rf /":  false,
		"/usr/bin/audit": false,
		"":               false,
	} {
		if got := isBonsaiCommand(command); got != want {
			t.Errorf("isBonsaiCommand(%q) = %v", command, got)
		}
	}
}

func TestLaunchArgsActivityNotifications(t *testing.T) {
	request := testRequest("/b")
	notify := []string{
		"-c", `tui.notifications=["approval-requested","async-question"]`,
		"-c", `tui.notification_method="osc9"`,
		"-c", `tui.notification_condition="always"`,
	}
	cat := func(parts ...[]string) []string {
		var out []string
		for _, p := range parts {
			out = append(out, p...)
		}
		return out
	}
	base := []string{"-c", storeOverride, "--no-daemon"}
	for _, tc := range []struct {
		name     string
		activity *agents.ActivityRequest
		explicit []string
		launch   agents.LaunchOptions
		want     []string
	}{
		{name: "no activity adds nothing", want: base},
		{name: "unusable request adds nothing", activity: &agents.ActivityRequest{HookCommand: []string{"/b"}}, want: base},
		{name: "usable request", activity: request, want: cat(base, notify)},
		{name: "before model and prompt", activity: request, launch: agents.LaunchOptions{Model: "o3", Prompt: "hi"},
			want: cat(base, notify, []string{"-m", "o3", "--", "hi"})},
		{name: "explicit notifications win", activity: request, explicit: []string{"-c", `tui.notifications=["agent-turn-complete"]`},
			want: cat([]string{"-c", `tui.notifications=["agent-turn-complete"]`}, base, notify[2:])},
		{name: "explicit method and condition win", activity: request, explicit: []string{"--config=tui.notification_method=\"bel\"", "-ctui.notification_condition=\"unfocused\""},
			want: cat([]string{"--config=tui.notification_method=\"bel\"", "-ctui.notification_condition=\"unfocused\""}, base, notify[:2])},
		{name: "after an explicit -- the tail stays last", activity: request, explicit: []string{"--search", "--", "-x prompt"},
			want: cat([]string{"--search"}, base, notify, []string{"--", "-x prompt"})},
		{name: "subcommand gets the global flags too", activity: request, explicit: []string{"resume", "--last"},
			want: cat(base, notify, []string{"resume", "--last"})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := launchArgs(Settings{}, tc.explicit, tc.launch, tc.activity)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("args = %q\nwant   %q", got, tc.want)
			}
		})
	}
}

func TestTranslateHook(t *testing.T) {
	p := New(nil, nil)
	sig := func(kind agentactivity.Kind, reason agentactivity.Reason, detail string) []agentactivity.Signal {
		return []agentactivity.Signal{{Kind: kind, Reason: reason, Detail: detail}}
	}
	for _, tc := range []struct {
		event, payload string
		want           []agentactivity.Signal
	}{
		{"SessionStart", `{"source":"startup"}`, sig(agentactivity.SessionStarted, "", "")},
		{"SessionStart", `{"source":"resume"}`, sig(agentactivity.SessionStarted, "", "")},
		{"SessionStart", `{"source":"clear"}`, sig(agentactivity.SessionStarted, "", "")},
		{"SessionStart", `{"source":"fork"}`, sig(agentactivity.SessionStarted, "", "")},
		{"SessionStart", `{}`, sig(agentactivity.SessionStarted, "", "")},
		{"SessionStart", `{"source":"compact"}`, nil},
		{"UserPromptSubmit", `{"prompt":"SECRET"}`, sig(agentactivity.PromptSubmitted, "", "")},
		{"PreToolUse", `{"tool_name":"Bash","tool_input":{"command":"SECRET"}}`, sig(agentactivity.ToolStarted, "", "Bash")},
		{"PreToolUse", `{"tool_name":"request_user_input","tool_input":{"questions":[{"question":"SECRET?"}]}}`, sig(agentactivity.InputRequested, agentactivity.ReasonQuestion, "")},
		{"PreToolUse", `{"tool_name":"request_user_input_async","tool_input":{"questions":[{"question":"SECRET?"}]}}`, sig(agentactivity.AsyncInputRequested, agentactivity.ReasonQuestion, "")},
		{"PreToolUse", `{}`, sig(agentactivity.ToolStarted, "", "")},
		{"PostToolUse", `{"tool_name":"apply_patch","tool_response":"SECRET"}`, sig(agentactivity.ToolFinished, "", "apply_patch")},
		{"PostToolUse", `{"tool_name":"request_user_input","tool_response":"SECRET"}`, sig(agentactivity.InputResolved, "", "")},
		{"PostToolUse", `{"tool_name":"request_user_input_async","tool_response":"SECRET"}`, nil},
		{"PermissionRequest", `{"tool_name":"Bash","tool_input":{"command":"SECRET"}}`, sig(agentactivity.PermissionRequested, "", "Bash")},
		{"Stop", `{"last_assistant_message":"SECRET","stop_hook_active":false}`, sig(agentactivity.TurnCompleted, "", "")},
		{"Interrupt", `{"turn_id":"t"}`, sig(agentactivity.Interrupted, "", "")},
		{"SessionEnd", `{"reason":"exit"}`, sig(agentactivity.SessionEnded, "", "")},
		{"PreCompact", `{"trigger":"auto"}`, nil},
		{"Stop", `not json`, nil},
		{"Stop", ``, nil},
		{"Unknown", `{}`, nil},
	} {
		got := p.TranslateHook(tc.event, []byte(tc.payload))
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s %s: %+v, want %+v", tc.event, tc.payload, got, tc.want)
		}
		for _, s := range got {
			if s.At.IsZero() == false || s.Source != "" {
				t.Errorf("%s: At/Source must be left to the hook command: %+v", tc.event, s)
			}
			if strings.Contains(s.Detail, "SECRET") {
				t.Errorf("%s leaks payload content: %+v", tc.event, s)
			}
		}
	}
	for _, event := range append([]string{"Unknown"}, eventNames()...) {
		if out := p.HookOutput(event); len(out) != 0 {
			t.Errorf("HookOutput(%s) = %q, want none", event, out)
		}
	}
}

func eventNames() []string {
	var names []string
	for _, e := range hookEvents {
		names = append(names, e.name)
	}
	return names
}

func TestClassifyTerminalNotification(t *testing.T) {
	p := New(nil, nil)
	for _, tc := range []struct {
		title, body string
		want        agentactivity.Signal
		ok          bool
	}{
		{body: "Approval requested: git push origin main", want: agentactivity.Signal{Kind: agentactivity.PermissionRequested}, ok: true},
		{body: "Approval requested by github", want: agentactivity.Signal{Kind: agentactivity.PermissionRequested}, ok: true},
		{body: "Codex wants to edit main.go", want: agentactivity.Signal{Kind: agentactivity.PermissionRequested}, ok: true},
		{body: "  Approval requested: ls", want: agentactivity.Signal{Kind: agentactivity.PermissionRequested}, ok: true},
		{title: "Approval requested: rm", want: agentactivity.Signal{Kind: agentactivity.PermissionRequested}, ok: true},
		{body: "Question: which branch?", want: agentactivity.Signal{Kind: agentactivity.InputRequested, Reason: agentactivity.ReasonQuestion}, ok: true},
		{body: "Plan mode prompt: Implement this plan?", want: agentactivity.Signal{Kind: agentactivity.InputRequested, Reason: agentactivity.ReasonPlan}, ok: true},
		{body: "Agent turn complete: done"},
		{body: "an Approval requested: in the middle"},
		{body: "Reset your current usage limits"},
		{},
	} {
		got, ok := p.ClassifyTerminalNotification(tc.title, tc.body)
		if ok != tc.ok || got != tc.want {
			t.Errorf("(%q, %q) = %+v, %v; want %+v, %v", tc.title, tc.body, got, ok, tc.want, tc.ok)
		}
		if strings.Contains(got.Detail, "git") {
			t.Errorf("classification leaks the notification text: %+v", got)
		}
	}
}

func TestActivityProfile(t *testing.T) {
	got := New(nil, nil).ActivityProfile()
	if want := []agentactivity.State{agentactivity.Working, agentactivity.Waiting, agentactivity.Idle}; !reflect.DeepEqual(got.Native, want) || len(got.Inferred) != 0 || got.ProgressMeansTurn {
		t.Fatalf("profile %+v", got)
	}
}
