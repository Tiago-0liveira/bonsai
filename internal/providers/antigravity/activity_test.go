package antigravity

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agentactivity"
	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

func activityRequest(t *testing.T, session agents.Session) *agents.ActivityRequest {
	t.Helper()
	return &agents.ActivityRequest{
		HookCommand: []string{"/opt/bonsai bin/bonsai", "__agent-hook", "antigravity"},
		SpoolEnv:    agents.ActivityEnv,
		SpoolPath:   filepath.Join(filepath.Dir(session.RuntimeDir), filepath.Base(session.RuntimeDir), agents.ActivitySpoolName),
	}
}

func prepareWithActivity(t *testing.T, provider *Provider, session agents.Session, activity *agents.ActivityRequest) agents.PreparedSession {
	t.Helper()
	prepared, err := provider.PrepareSession(context.Background(), agents.PrepareSessionRequest{
		Account: agents.Account{ID: "acct_a", Provider: ProviderID}, Session: session, Activity: activity,
	})
	if err != nil {
		t.Fatal(err)
	}
	return prepared
}

func testProvider() *Provider {
	return &Provider{binaryResolver: &providerTestBinaryResolver{path: "agy-test"}, credentialMgr: &providerTestCredentialManager{}}
}

func testSession(t *testing.T) agents.Session {
	t.Helper()
	runtimeDir := filepath.Join(t.TempDir(), "sess_a")
	return agents.Session{ID: "sess_a", RuntimeDir: runtimeDir, HomeDir: filepath.Join(runtimeDir, "home"), WorkDir: t.TempDir()}
}

func readJSONFile(t *testing.T, path string) map[string]json.RawMessage {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]json.RawMessage
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("%s: %v\n%s", path, err, data)
	}
	return out
}

func TestPrepareSessionInstallsActivityHooks(t *testing.T) {
	session := testSession(t)
	activity := activityRequest(t, session)
	prepared := prepareWithActivity(t, testProvider(), session, activity)

	if prepared.EnvSet[agents.ActivityEnv] != activity.SpoolPath || prepared.EnvSet["HOME"] != session.HomeDir {
		t.Fatalf("env = %v", prepared.EnvSet)
	}
	path := filepath.Join(session.HomeDir, ".gemini", "config", "hooks.json")
	if runtime.GOOS != "windows" {
		if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("hooks.json: %v %v", info, err)
		}
	}
	groups := readJSONFile(t, path)
	if len(groups) != 1 {
		t.Fatalf("groups = %v, want only %q", groups, hooksGroup)
	}
	var events map[string]json.RawMessage
	if err := json.Unmarshal(groups[hooksGroup], &events); err != nil {
		t.Fatal(err)
	}
	var names []string
	for name := range events {
		names = append(names, name)
	}
	sort.Strings(names)
	// PreToolUse would have to answer with a permission decision.
	if want := []string{"PostInvocation", "PostToolUse", "PreInvocation", "SessionStart", "Stop"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("events = %v, want %v", names, want)
	}

	sessionsRoot := filepath.Dir(session.RuntimeDir)
	wantCommand := func(event string) string {
		return "'/opt/bonsai bin/bonsai' '__agent-hook' 'antigravity' " + event + " '--sessions-root=" + sessionsRoot + "'"
	}
	if runtime.GOOS == "windows" {
		t.Skip("POSIX quoting")
	}
	for _, event := range []string{"SessionStart", "PreInvocation", "PostInvocation", "Stop"} {
		var handlers []hookHandler // flat: handlers directly
		if err := json.Unmarshal(events[event], &handlers); err != nil || len(handlers) != 1 {
			t.Fatalf("%s: %v %s", event, err, events[event])
		}
		if h := handlers[0]; h.Type != "command" || h.Command != wantCommand(event) || h.Timeout != hookTimeoutSeconds {
			t.Errorf("%s handler = %+v", event, h)
		}
	}
	var tool []hookMatcherGroup // grouped: a matcher around the handlers
	if err := json.Unmarshal(events["PostToolUse"], &tool); err != nil || len(tool) != 1 || tool[0].Matcher != "*" || len(tool[0].Hooks) != 1 {
		t.Fatalf("PostToolUse: %v %s", err, events["PostToolUse"])
	}
	if h := tool[0].Hooks[0]; h.Command != wantCommand("PostToolUse") || h.Type != "command" {
		t.Errorf("PostToolUse handler = %+v", h)
	}

	settings := readJSONFile(t, providerSettingsPath(session.HomeDir))
	if string(settings["notifications"]) != "true" {
		t.Fatalf("settings = %v", settings)
	}
	if !notificationsEnabledBy(session) {
		t.Fatal("no marker for Reconcile")
	}
}

func TestPrepareSessionWithoutActivityInstallsNothing(t *testing.T) {
	for name, activity := range map[string]*agents.ActivityRequest{
		"nil":      nil,
		"unusable": {HookCommand: []string{"bonsai"}, SpoolEnv: agents.ActivityEnv}, // no spool
	} {
		t.Run(name, func(t *testing.T) {
			session := testSession(t)
			prepared := prepareWithActivity(t, testProvider(), session, activity)
			if _, ok := prepared.EnvSet[agents.ActivityEnv]; ok {
				t.Fatal("spool env set")
			}
			for _, path := range []string{hooksPath(session.HomeDir), providerSettingsPath(session.HomeDir), notificationsMarkerPath(session)} {
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Errorf("%s exists: %v", path, err)
				}
			}
		})
	}
}

func TestActivityHooksKeepOtherGroupsAndSettings(t *testing.T) {
	session := testSession(t)
	if err := os.MkdirAll(filepath.Dir(hooksPath(session.HomeDir)), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(antigravityDir(session.HomeDir), 0o700); err != nil {
		t.Fatal(err)
	}
	other := `{"mine":{"Stop":[{"command":"true"}]},"bonsai-activity":{"Stop":[{"command":"old"}]}}`
	if err := os.WriteFile(hooksPath(session.HomeDir), []byte(other), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(providerSettingsPath(session.HomeDir), []byte(`{"trustedWorkspaces":["/w"],"notifications":false}`), 0o600); err != nil {
		t.Fatal(err)
	}
	prepareWithActivity(t, testProvider(), session, activityRequest(t, session))

	groups := readJSONFile(t, hooksPath(session.HomeDir))
	if len(groups) != 2 || !strings.Contains(string(groups["mine"]), `"true"`) || strings.Contains(string(groups[hooksGroup]), `"old"`) {
		t.Fatalf("groups = %s", groups)
	}
	settings := readJSONFile(t, providerSettingsPath(session.HomeDir))
	if string(settings["notifications"]) != "true" || !sameSettings(t, settings["trustedWorkspaces"], json.RawMessage(`["/w"]`)) {
		t.Fatalf("settings = %s", settings)
	}
}

func TestActivityDegradesWithoutFailingTheLaunch(t *testing.T) {
	t.Run("unreadable settings keep hooks and are left alone", func(t *testing.T) {
		session := testSession(t)
		if err := os.MkdirAll(antigravityDir(session.HomeDir), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(providerSettingsPath(session.HomeDir), []byte(`{broken`), 0o600); err != nil {
			t.Fatal(err)
		}
		prepared := prepareWithActivity(t, testProvider(), session, activityRequest(t, session))
		if _, ok := prepared.EnvSet[agents.ActivityEnv]; !ok {
			t.Fatal("hooks work without notifications: spool env expected")
		}
		if data, _ := os.ReadFile(providerSettingsPath(session.HomeDir)); string(data) != `{broken` {
			t.Fatalf("settings rewritten: %q", data)
		}
		if notificationsEnabledBy(session) {
			t.Fatal("marker without a change")
		}
	})
	t.Run("hooks that cannot be written", func(t *testing.T) {
		session := testSession(t)
		// .gemini is a file, so neither hooks.json nor settings can exist.
		if err := os.MkdirAll(session.HomeDir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(session.HomeDir, ".gemini"), nil, 0o600); err != nil {
			t.Fatal(err)
		}
		prepared := prepareWithActivity(t, testProvider(), session, activityRequest(t, session))
		if _, ok := prepared.EnvSet[agents.ActivityEnv]; ok {
			t.Fatal("spool env set although no hooks were installed")
		}
		if prepared.Executable != "agy-test" {
			t.Fatalf("prepared = %+v", prepared)
		}
	})
}

// hooks.json and settings.json in the session home come from the profile, so
// they can hold any JSON. None of it may panic the launch, and what is not an
// object is left exactly as it was.
func TestActivityOddFiles(t *testing.T) {
	for _, content := range []string{`null`, " null\n", `[]`, `"x"`, `1`, `{broken`} {
		t.Run("hooks.json "+strings.TrimSpace(content), func(t *testing.T) {
			session := testSession(t)
			if err := os.MkdirAll(filepath.Dir(hooksPath(session.HomeDir)), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(hooksPath(session.HomeDir), []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			prepared := prepareWithActivity(t, testProvider(), session, activityRequest(t, session))
			if _, ok := prepared.EnvSet[agents.ActivityEnv]; ok {
				t.Fatal("spool env set although no hooks were installed")
			}
			if data, _ := os.ReadFile(hooksPath(session.HomeDir)); string(data) != content {
				t.Fatalf("hooks.json rewritten: %q", data)
			}
		})
		t.Run("settings.json "+strings.TrimSpace(content), func(t *testing.T) {
			session := testSession(t)
			if err := os.MkdirAll(antigravityDir(session.HomeDir), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(providerSettingsPath(session.HomeDir), []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
			prepared := prepareWithActivity(t, testProvider(), session, activityRequest(t, session))
			if _, ok := prepared.EnvSet[agents.ActivityEnv]; !ok {
				t.Fatal("hooks work without notifications: spool env expected")
			}
			if data, _ := os.ReadFile(providerSettingsPath(session.HomeDir)); string(data) != content {
				t.Fatalf("settings rewritten: %q", data)
			}
			if notificationsEnabledBy(session) {
				t.Fatal("marker without a change")
			}
		})
	}
	t.Run("groups of other shapes are kept", func(t *testing.T) {
		session := testSession(t)
		if err := os.MkdirAll(filepath.Dir(hooksPath(session.HomeDir)), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(hooksPath(session.HomeDir), []byte(`{"a":null,"b":[1],"c":"x","bonsai-activity":null}`), 0o600); err != nil {
			t.Fatal(err)
		}
		prepareWithActivity(t, testProvider(), session, activityRequest(t, session))
		groups := readJSONFile(t, hooksPath(session.HomeDir))
		if len(groups) != 4 || string(groups["a"]) != "null" || !sameSettings(t, groups["b"], json.RawMessage("[1]")) || string(groups["c"]) != `"x"` || !strings.Contains(string(groups[hooksGroup]), "PostToolUse") {
			t.Fatalf("groups = %s", groups)
		}
	})
}

func TestReconcileLeavesActivityOutOfTheProfile(t *testing.T) {
	for name, vaultSettings := range map[string]string{
		"profile without the key": `{"gcp":{"project":"p"}}`,
		"profile with it off":     `{"notifications":false,"gcp":{"project":"p"}}`,
		"profile with it on":      `{"notifications":true,"gcp":{"project":"p"}}`,
	} {
		t.Run(name, func(t *testing.T) {
			accounts, err := agents.NewFileAccountStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			sessions, err := agents.NewFileSessionStore(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			account := agents.Account{ID: "acct_a", Provider: ProviderID, Name: "a", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
			if err := accounts.Create(account); err != nil {
				t.Fatal(err)
			}
			manager := NewCredentialManager(accounts).(*fileCredentialManager)
			setup, err := sessions.Create(account, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			writeSessionAuth(t, setup.HomeDir, "access", "refresh", "user@example.com", time.Now().Add(time.Hour))
			if err := os.WriteFile(providerSettingsPath(setup.HomeDir), []byte(vaultSettings), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := manager.CaptureSetup(context.Background(), account, setup); err != nil {
				t.Fatal(err)
			}
			before, err := manager.readVault(account)
			if err != nil {
				t.Fatal(err)
			}

			session, err := sessions.Create(account, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			provider := &Provider{accounts: accounts, sessions: sessions, binaryResolver: &providerTestBinaryResolver{path: "agy-test"}, credentialMgr: manager}
			prepareWithActivity(t, provider, session, activityRequest(t, session))
			if string(readJSONFile(t, providerSettingsPath(session.HomeDir))["notifications"]) != "true" {
				t.Fatal("notifications not enabled in the session")
			}
			if err := provider.FinalizeSession(context.Background(), agents.FinalizeSessionRequest{Account: account, Session: session}); err != nil {
				t.Fatal(err)
			}
			after, err := manager.readVault(account)
			if err != nil {
				t.Fatal(err)
			}
			// (Reconcile rewrites the vault's formatting either way, so compare meaning.)
			if !sameSettings(t, before.ProviderSettings, after.ProviderSettings) {
				t.Fatalf("an untouched session changed the profile settings:\nbefore %s\nafter  %s", before.ProviderSettings, after.ProviderSettings)
			}

			// What agy itself changed in a later session still goes back.
			session, err = sessions.Create(account, t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			prepareWithActivity(t, provider, session, activityRequest(t, session))
			settings := readJSONFile(t, providerSettingsPath(session.HomeDir))
			settings["trustedWorkspaces"] = json.RawMessage(`["/new"]`)
			data, _ := json.Marshal(settings)
			if err := os.WriteFile(providerSettingsPath(session.HomeDir), data, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := provider.FinalizeSession(context.Background(), agents.FinalizeSessionRequest{Account: account, Session: session}); err != nil {
				t.Fatal(err)
			}
			after, _ = manager.readVault(account)
			var got map[string]json.RawMessage
			if err := json.Unmarshal(after.ProviderSettings, &got); err != nil {
				t.Fatal(err)
			}
			var original map[string]json.RawMessage
			_ = json.Unmarshal(before.ProviderSettings, &original)
			if !sameSettings(t, got["trustedWorkspaces"], json.RawMessage(`["/new"]`)) || string(got["notifications"]) != string(original["notifications"]) {
				t.Fatalf("profile settings = %s, want notifications as before and the new workspace", after.ProviderSettings)
			}
			// Hooks stay in the session home: nothing of them reaches the vault, and
			// the sign-in/plan probe home never has any.
			vault, _ := os.ReadFile(vaultPath(accounts, account))
			if strings.Contains(string(vault), hooksGroup) || strings.Contains(string(vault), "__agent-hook") {
				t.Fatalf("vault holds hooks: %s", vault)
			}
			if _, err := os.Stat(hooksPath(probeHomeDir(accounts, account))); !os.IsNotExist(err) {
				t.Fatalf("probe home has hooks: %v", err)
			}
		})
	}
}

func sameSettings(t *testing.T, a, b json.RawMessage) bool {
	t.Helper()
	var x, y any
	if err := json.Unmarshal(a, &x); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &y); err != nil {
		t.Fatal(err)
	}
	return reflect.DeepEqual(x, y)
}

func TestReconcileWithoutProfileSettingsDropsOnlyOurKey(t *testing.T) {
	got := restoreNotifications(json.RawMessage(`{"notifications":true}`), nil)
	if len(got) != 0 {
		t.Fatalf("settings = %s, want none: the profile had none", got)
	}
	got = restoreNotifications(json.RawMessage(`{"notifications":true,"x":1}`), nil)
	if string(got) != `{"x":1}` {
		t.Fatalf("settings = %s", got)
	}
}

func TestTranslateHook(t *testing.T) {
	p := New(nil, nil)
	secret := `"args":{"CommandLine":"cat SECRET"},"error":"SECRET failure","conversationId":"c"`
	for _, tc := range []struct {
		name, event, payload string
		want                 []agentactivity.Signal
	}{
		{"session start", "SessionStart", `{"conversationId":"c"}`, []agentactivity.Signal{{Kind: agentactivity.SessionStarted}}},
		{"pre invocation", "PreInvocation", `{"invocationNum":3}`, []agentactivity.Signal{{Kind: agentactivity.PromptSubmitted}}},
		{"post invocation", "PostInvocation", `{"invocationNum":3}`, []agentactivity.Signal{{Kind: agentactivity.ToolFinished}}},
		{"post tool use", "PostToolUse", `{"toolCall":{"name":"run_command",` + secret + `}}`, []agentactivity.Signal{{Kind: agentactivity.ToolFinished, Detail: "run_command"}}},
		{"post tool use without a name", "PostToolUse", `{}`, []agentactivity.Signal{{Kind: agentactivity.ToolFinished}}},
		{"stop: normal", "Stop", `{"terminationReason":"NO_TOOL_CALL","fullyIdle":true}`, []agentactivity.Signal{{Kind: agentactivity.TurnCompleted}}},
		{"stop: limit", "Stop", `{"terminationReason":"MAX_INVOCATIONS"}`, []agentactivity.Signal{{Kind: agentactivity.TurnCompleted}}},
		{"stop: token budget", "Stop", `{"terminationReason":"MAX_TOKEN_BUDGET_EXCEEDED"}`, []agentactivity.Signal{{Kind: agentactivity.TurnCompleted}}},
		{"stop: hook", "Stop", `{"terminationReason":"TERMINAL_CUSTOM_HOOK"}`, []agentactivity.Signal{{Kind: agentactivity.TurnCompleted}}},
		{"stop: no reason", "Stop", `{}`, []agentactivity.Signal{{Kind: agentactivity.TurnCompleted}}},
		{"stop: error keeps only the kind", "Stop", `{"terminationReason":"ERROR","error":"SECRET failure"}`, []agentactivity.Signal{{Kind: agentactivity.TurnFailed, Detail: "error"}}},
		{"stop: canceled", "Stop", `{"terminationReason":"USER_CANCELED"}`, []agentactivity.Signal{{Kind: agentactivity.Interrupted}}},
		{"stop: prefixed enum name", "Stop", `{"terminationReason":"EXECUTOR_TERMINATION_REASON_USER_CANCELED"}`, []agentactivity.Signal{{Kind: agentactivity.Interrupted}}},
		{"pre tool use is never ours", "PreToolUse", `{"toolCall":{"name":"run_command"}}`, nil},
		{"unknown event", "Whatever", `{}`, nil},
		{"malformed", "Stop", `not json`, nil},
		{"empty", "Stop", ``, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := p.TranslateHook(tc.event, []byte(tc.payload))
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("signals = %+v, want %+v", got, tc.want)
			}
			for _, s := range got {
				if !s.At.IsZero() || s.Source != "" || strings.Contains(s.Detail, "SECRET") {
					t.Fatalf("signal %+v: the hook command stamps At and Source; details carry no content", s)
				}
			}
		})
	}
}

func TestHookOutputIsAnEmptyJSONObject(t *testing.T) {
	p := New(nil, nil)
	for _, event := range []string{"SessionStart", "PreInvocation", "PostInvocation", "PostToolUse", "Stop", "anything"} {
		if got := string(p.HookOutput(event)); got != "{}" {
			t.Errorf("%s: %q", event, got)
		}
	}
}

func TestActivityProfile(t *testing.T) {
	got := New(nil, nil).ActivityProfile()
	want := agents.ActivityProfile{
		Native:            []agentactivity.State{agentactivity.Working, agentactivity.Idle},
		Inferred:          []agentactivity.State{agentactivity.Waiting},
		ProgressMeansTurn: true,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("profile = %+v", got)
	}
}

func TestClassifyTerminalNotification(t *testing.T) {
	p := New(nil, nil)
	for _, tc := range []struct {
		name, title, body string
		want              agentactivity.Signal
		ok                bool
	}{
		{"question", "", "The agent has a question for you: which branch?", agentactivity.Signal{Kind: agentactivity.InputRequested, Reason: agentactivity.ReasonQuestion}, true},
		{"subagent question", "Antigravity", "researcher has a question", agentactivity.Signal{Kind: agentactivity.InputRequested, Reason: agentactivity.ReasonQuestion}, true},
		{"permission", "Antigravity", "Permission required to run a command", agentactivity.Signal{Kind: agentactivity.PermissionRequested}, true},
		{"tool confirmation", "", "Antigravity CLI needs tool confirmation", agentactivity.Signal{Kind: agentactivity.PermissionRequested}, true},
		{"tool confirmation title", "Antigravity CLI needs tool confirmation", "", agentactivity.Signal{Kind: agentactivity.PermissionRequested}, true},
		{"attention", "", "Action requires your attention", agentactivity.Signal{Kind: agentactivity.PermissionRequested}, true},
		{"approval", "", "Approve file edit?", agentactivity.Signal{Kind: agentactivity.PermissionRequested}, true},
		{"completion is not waiting", "Antigravity", "Task complete", agentactivity.Signal{}, false},
		{"empty", "", "", agentactivity.Signal{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := p.ClassifyTerminalNotification(tc.title, tc.body)
			if ok != tc.ok || got != tc.want {
				t.Fatalf("got %+v %v, want %+v %v", got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestClassifyTerminalScreen(t *testing.T) {
	p := New(nil, nil)
	want := agentactivity.Signal{Kind: agentactivity.InputRequested, Reason: agentactivity.ReasonQuestion}
	for _, tc := range []struct {
		name  string
		lines []string
		want  agentactivity.Signal
		ok    bool
	}{
		{"question dialog", []string{"Question", "1. Red", "2. Blue", "Question 1/1: Please choose a color:", "↑/↓ Navigate · enter Select · esc Skip"}, want, true},
		{"answer content is not copied", []string{"Question 1/1: Should I print SECRET?", "↑/↓ Navigate · enter Select · esc Skip"}, want, true},
		{"quoted cues in prose", []string{"The UI says Question 1/1: choose and ↑/↓ Navigate · enter Select · esc Skip"}, agentactivity.Signal{}, false},
		{"missing footer", []string{"Question 1/1: Please choose a color:"}, agentactivity.Signal{}, false},
		{"partial footer", []string{"Question 1/1: Please choose a color:", "enter Select · esc Skip"}, agentactivity.Signal{}, false},
		{"missing header", []string{"↑/↓ Navigate · enter Select · esc Skip"}, agentactivity.Signal{}, false},
		{"not numbered header", []string{"Question: Please choose a color:", "↑/↓ Navigate · enter Select · esc Skip"}, agentactivity.Signal{}, false},
		{"permission dialog", []string{"Requesting permission for:", "rm -rf SECRET", "Run this command?", "1. Yes", "2. No", "↑/↓ Navigate · tab Amend · ctrl+g edit/expand command"}, agentactivity.Signal{Kind: agentactivity.PermissionRequested}, true},
		{"permission command is not copied", []string{"Requesting permission for:", "print SECRET", "Run this command?", "↑/↓ Navigate · tab Amend · ctrl+g edit/expand command"}, agentactivity.Signal{Kind: agentactivity.PermissionRequested}, true},
		{"permission missing footer", []string{"Requesting permission for:", "rm command", "Run this command?"}, agentactivity.Signal{}, false},
		{"permission partial footer", []string{"Requesting permission for:", "rm command", "Run this command?", "tab Amend · ctrl+g edit/expand command"}, agentactivity.Signal{}, false},
		{"permission prose quoting cues", []string{"It says Requesting permission for: Run this command? ↑/↓ Navigate · tab Amend · ctrl+g edit/expand command"}, agentactivity.Signal{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := p.ClassifyTerminalScreen(tc.lines)
			if ok != tc.ok || got != tc.want {
				t.Fatalf("got %+v %v, want %+v %v", got, ok, tc.want, tc.ok)
			}
		})
	}
}
