package codex

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pelletier/go-toml/v2"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

// activityRequest is a usable request whose hook command is a fixed path.
func (e *testEnv) activityRequest(bonsai string) *agents.ActivityRequest {
	req := testRequest(bonsai)
	req.SpoolPath = filepath.Join(e.t.TempDir(), agents.ActivitySpoolName)
	return req
}

func (e *testEnv) prepareActivity(account agents.Account, request *agents.ActivityRequest, args ...string) agents.PreparedSession {
	e.t.Helper()
	session, err := e.sessions.Create(account, e.t.TempDir())
	if err != nil {
		e.t.Fatal(err)
	}
	e.t.Cleanup(func() { _ = e.sessions.Cleanup(session) })
	prepared, err := e.provider.PrepareSession(e.t.Context(), agents.PrepareSessionRequest{Account: account, Session: session, Args: args, Activity: request})
	if err != nil {
		e.t.Fatalf("prepare: %v", err)
	}
	return prepared
}

// trustedHashes reads hooks.state out of the profile's config.toml.
func (e *testEnv) trustedHashes(account agents.Account) map[string]string {
	e.t.Helper()
	data, err := os.ReadFile(configPath(e.profileHome(account)))
	if err != nil {
		e.t.Fatal(err)
	}
	var config struct {
		Hooks struct {
			State map[string]struct {
				TrustedHash string `toml:"trusted_hash"`
			} `toml:"state"`
		} `toml:"hooks"`
	}
	if err := toml.Unmarshal(data, &config); err != nil {
		e.t.Fatalf("config.toml: %v\n%s", err, data)
	}
	out := map[string]string{}
	for key, v := range config.Hooks.State {
		out[key] = v.TrustedHash
	}
	return out
}

func (e *testEnv) ourKeys(account agents.Account) []string {
	var keys []string
	for _, event := range hookEvents {
		snake := strings.ToLower(strings.TrimPrefix(strings.Join(splitCamel(event.name), "_"), "_"))
		keys = append(keys, hooksPath(e.profileHome(account))+":"+snake+":0:0")
	}
	return keys
}

func splitCamel(s string) []string {
	var words []string
	start := 0
	for i := 1; i < len(s); i++ {
		if s[i] >= 'A' && s[i] <= 'Z' {
			words = append(words, s[start:i])
			start = i
		}
	}
	return append(words, s[start:])
}

func (e *testEnv) configWrites() []string {
	var out []string
	for _, line := range strings.Split(e.calls(), "\n") {
		if file, ok := strings.CutPrefix(line, "config-write "); ok {
			out = append(out, file)
		}
	}
	return out
}

func TestPrepareSessionInstallsAndTrustsOnlyBonsaiHooks(t *testing.T) {
	e := newTestEnv(t)
	work := e.addLogin("work")
	home := e.profileHome(work)
	foreign := `{"hooks":{"PreToolUse":[{"matcher":"Bash","hooks":[{"type":"command","command":"/usr/bin/audit"}]}]}}`
	writeFile(t, hooksPath(home), foreign, 0o644)
	before := treeHash(t, e.home)

	request := e.activityRequest("/opt/bonsai")
	prepared := e.prepareActivity(work, request)

	if prepared.EnvSet[agents.ActivityEnv] != request.SpoolPath {
		t.Fatalf("spool env %q", prepared.EnvSet[agents.ActivityEnv])
	}
	if prepared.EnvSet["CODEX_HOME"] != home {
		t.Fatalf("CODEX_HOME %q", prepared.EnvSet["CODEX_HOME"])
	}
	for _, flag := range []string{`tui.notifications=["approval-requested","async-question"]`, `tui.notification_method="osc9"`, `tui.notification_condition="always"`} {
		if !containsArg(prepared.Args, flag) {
			t.Fatalf("missing -c %s in %q", flag, prepared.Args)
		}
	}

	data, err := os.ReadFile(hooksPath(home))
	if err != nil {
		t.Fatal(err)
	}
	doc := hooksDoc(t, data)
	if want := [][]string{{"/usr/bin/audit"}, {"'/opt/bonsai' '__agent-hook' 'codex' PreToolUse"}}; !reflect.DeepEqual(doc["PreToolUse"], want) {
		t.Fatalf("PreToolUse %v", doc["PreToolUse"])
	}
	if runtime.GOOS != "windows" {
		if info, err := os.Stat(hooksPath(home)); err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("hooks.json mode %v %v", info, err)
		}
	}

	// Exactly Bonsai's handlers are trusted; the foreign one is not, and
	// everything stays in the profile.
	trusted := e.trustedHashes(work)
	foreignKey := hooksPath(home) + ":pre_tool_use:0:0"
	if _, ok := trusted[foreignKey]; ok {
		t.Fatalf("a foreign hook was trusted: %v", trusted)
	}
	for _, key := range e.ourKeys(work) {
		if key == hooksPath(home)+":pre_tool_use:0:0" {
			key = hooksPath(home) + ":pre_tool_use:1:0" // after the foreign group
		}
		if !strings.HasPrefix(trusted[key], "sha256:") {
			t.Fatalf("%s not trusted: %v", key, trusted)
		}
	}
	if len(trusted) != len(hookEvents) {
		t.Fatalf("trusted %d hooks, want %d: %v", len(trusted), len(hookEvents), trusted)
	}
	if writes := e.configWrites(); len(writes) != 1 || writes[0] != configPath(home) {
		t.Fatalf("config writes %v, want only %s", writes, configPath(home))
	}
	if want := []string{"initialize", "initialized", "hooks/list", "config/batchWrite"}; !reflect.DeepEqual(e.rpcs(), want) {
		t.Fatalf("rpcs %v", e.rpcs())
	}
	if e.spawns() != 1 {
		t.Fatalf("app-servers %d", e.spawns())
	}
	if after := treeHash(t, e.home); after != before {
		t.Fatal("the user's home directory changed")
	}
}

func TestPrepareSessionSkipsTheAppServerWhenNothingChanged(t *testing.T) {
	e := newTestEnv(t)
	work := e.addLogin("work")
	request := e.activityRequest("/opt/bonsai")
	e.prepareActivity(work, request)
	hooksBefore, _ := os.ReadFile(hooksPath(e.profileHome(work)))
	statBefore, _ := os.Stat(hooksPath(e.profileHome(work)))

	e.prepareActivity(work, request)
	e.prepareActivity(work, e.activityRequest("/opt/bonsai"))

	if e.spawns() != 1 {
		t.Fatalf("app-servers %d, want 1", e.spawns())
	}
	hooksAfter, _ := os.ReadFile(hooksPath(e.profileHome(work)))
	statAfter, _ := os.Stat(hooksPath(e.profileHome(work)))
	if string(hooksAfter) != string(hooksBefore) || !os.SameFile(statBefore, statAfter) {
		t.Fatal("an unchanged hooks.json was rewritten")
	}
}

func TestPrepareSessionRetrustsAfterTheBonsaiPathOrTheConfigChanges(t *testing.T) {
	e := newTestEnv(t)
	work := e.addLogin("work")
	home := e.profileHome(work)
	e.prepareActivity(work, e.activityRequest("/opt/bonsai"))
	oldTrusted := e.trustedHashes(work)

	e.prepareActivity(work, e.activityRequest("/opt/bonsai-2"))
	if e.spawns() != 2 {
		t.Fatalf("a new Bonsai path must be trusted again: %d app-servers", e.spawns())
	}
	data, _ := os.ReadFile(hooksPath(home))
	if strings.Contains(string(data), "/opt/bonsai'") || !strings.Contains(string(data), "/opt/bonsai-2") {
		t.Fatalf("hooks.json:\n%s", data)
	}
	newTrusted := e.trustedHashes(work)
	if len(newTrusted) != len(hookEvents) {
		t.Fatalf("trusted %v", newTrusted)
	}
	for key, hash := range newTrusted {
		if oldTrusted[key] == hash {
			t.Fatalf("%s kept the old hash", key)
		}
	}

	// A config.toml that lost the trust (reseeded, edited) is repaired too.
	writeFile(t, configPath(home), "cli_auth_credentials_store = \"file\"\n", 0o600)
	e.prepareActivity(work, e.activityRequest("/opt/bonsai-2"))
	if e.spawns() != 3 || len(e.trustedHashes(work)) != len(hookEvents) {
		t.Fatalf("app-servers %d, trusted %v", e.spawns(), e.trustedHashes(work))
	}
	data, _ = os.ReadFile(configPath(home))
	if !strings.Contains(string(data), "cli_auth_credentials_store") {
		t.Fatalf("config.toml lost its own keys:\n%s", data)
	}
}

func TestConcurrentSessionsShareOneConsistentHooksFile(t *testing.T) {
	e := newTestEnv(t)
	work := e.addLogin("work")
	home := e.profileHome(work)
	writeFile(t, hooksPath(home), `{"hooks":{"Stop":[{"hooks":[{"type":"command","command":"/usr/bin/notify"}]}]}}`, 0o600)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			session, err := e.sessions.Create(work, t.TempDir())
			if err != nil {
				t.Error(err)
				return
			}
			defer e.sessions.Cleanup(session)
			request := e.activityRequest("/opt/bonsai")
			if _, err := e.provider.PrepareSession(t.Context(), agents.PrepareSessionRequest{Account: work, Session: session, Activity: request}); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()

	data, err := os.ReadFile(hooksPath(home))
	if err != nil {
		t.Fatal(err)
	}
	doc := hooksDoc(t, data)
	if want := [][]string{{"/usr/bin/notify"}, {"'/opt/bonsai' '__agent-hook' 'codex' Stop"}}; !reflect.DeepEqual(doc["Stop"], want) {
		t.Fatalf("Stop %v", doc["Stop"])
	}
	for _, event := range hookEvents {
		if event.name != "Stop" && len(doc[event.name]) != 1 {
			t.Fatalf("%s: %v", event.name, doc[event.name])
		}
	}
	if e.spawns() != 1 {
		t.Fatalf("app-servers %d: concurrent launches must share one trust step", e.spawns())
	}
}

func TestPrepareSessionWithoutUsableActivityTouchesNoHooks(t *testing.T) {
	e := newTestEnv(t)
	work := e.addLogin("work")
	for name, request := range map[string]*agents.ActivityRequest{
		"nil":          nil,
		"no command":   {SpoolEnv: agents.ActivityEnv, SpoolPath: "/x/activity.jsonl"},
		"no spool env": {HookCommand: []string{"/b", "__agent-hook", "codex"}, SpoolPath: "/x/activity.jsonl"},
		"no spool":     {HookCommand: []string{"/b", "__agent-hook", "codex"}, SpoolEnv: agents.ActivityEnv},
	} {
		prepared := e.prepareActivity(work, request)
		if _, ok := prepared.EnvSet[agents.ActivityEnv]; ok {
			t.Errorf("%s: spool env set", name)
		}
		if containsArg(prepared.Args, `tui.notification_method="osc9"`) {
			t.Errorf("%s: notification flags added", name)
		}
	}
	if _, err := os.Stat(hooksPath(e.profileHome(work))); !os.IsNotExist(err) {
		t.Fatalf("hooks.json written: %v", err)
	}
	if e.spawns() != 0 {
		t.Fatalf("app-servers %d", e.spawns())
	}
}

// Hooks are an upgrade: whatever goes wrong while installing or trusting them,
// the session starts, with the terminal fallback, and nothing is recorded as
// trusted.
func TestPrepareSessionStartsWhenHookTrustFails(t *testing.T) {
	for _, mode := range []string{"error", "exit", "exit-after-init", "hang"} {
		t.Run(mode, func(t *testing.T) {
			old := appServerTimeout
			appServerTimeout = 2 * time.Second
			t.Cleanup(func() { appServerTimeout = old })
			e := newTestEnv(t)
			work := e.addLogin("work")
			t.Setenv("FAKE_CODEX_APPSERVER", mode)

			prepared := e.prepareActivity(work, e.activityRequest("/opt/bonsai"))

			if !containsArg(prepared.Args, `tui.notification_method="osc9"`) || prepared.EnvSet[agents.ActivityEnv] == "" {
				t.Fatalf("terminal fallback missing: %q", prepared.Args)
			}
			if _, err := os.Stat(trustRecordPath(e.accounts, work)); !os.IsNotExist(err) {
				t.Fatalf("trust record written after a failure: %v", err)
			}
			if len(e.configWrites()) != 0 {
				t.Fatalf("config written: %v", e.configWrites())
			}

			// The next launch tries again.
			t.Setenv("FAKE_CODEX_APPSERVER", "")
			e.prepareActivity(work, e.activityRequest("/opt/bonsai"))
			if len(e.trustedHashes(work)) != len(hookEvents) {
				t.Fatalf("not trusted on retry: %v", e.trustedHashes(work))
			}
		})
	}
}

func TestPrepareSessionLeavesAnUnreadableHooksFileAlone(t *testing.T) {
	e := newTestEnv(t)
	work := e.addLogin("work")
	path := hooksPath(e.profileHome(work))
	writeFile(t, path, `{ this is the user's own notes`, 0o600)

	prepared := e.prepareActivity(work, e.activityRequest("/opt/bonsai"))

	if data, _ := os.ReadFile(path); string(data) != `{ this is the user's own notes` {
		t.Fatalf("hooks.json overwritten:\n%s", data)
	}
	if prepared.Executable == "" || e.spawns() != 0 {
		t.Fatalf("prepared %+v, app-servers %d", prepared, e.spawns())
	}
}

func TestBonsaiHooksIgnoresEveryoneElses(t *testing.T) {
	hooksFile := "/profile/hooks.json"
	reply := `{"data":[{"hooks":[
	 {"key":"k1","handlerType":"command","command":"'/b' '__agent-hook' 'codex' Stop","sourcePath":"/profile/hooks.json","currentHash":"sha256:1","trustStatus":"untrusted"},
	 {"key":"k1","handlerType":"command","command":"'/b' '__agent-hook' 'codex' Stop","sourcePath":"/profile/hooks.json","currentHash":"sha256:1","trustStatus":"untrusted"},
	 {"key":"k2","handlerType":"command","command":"/usr/bin/audit","sourcePath":"/profile/hooks.json","currentHash":"sha256:2"},
	 {"key":"k3","handlerType":"command","command":"'/b' '__agent-hook' 'codex' Stop","sourcePath":"/repo/.codex/hooks.json","currentHash":"sha256:3"},
	 {"key":"k4","handlerType":"mcpTool","command":"'/b' '__agent-hook' 'codex' Stop","sourcePath":"/profile/hooks.json","currentHash":"sha256:4"},
	 {"key":"k5","handlerType":"command","command":"'/b' '__agent-hook' 'codex' Stop","sourcePath":"/profile/hooks.json","currentHash":"sha256:5","isManaged":true},
	 {"key":"","handlerType":"command","command":"'/b' '__agent-hook' 'codex' Stop","sourcePath":"/profile/hooks.json","currentHash":"sha256:6"},
	 {"key":"k7","handlerType":"command","command":"'/b' '__agent-hook' 'codex' Stop","sourcePath":"/profile/hooks.json","currentHash":""}
	]}]}`
	got, err := bonsaiHooks([]byte(reply), hooksFile)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Key != "k1" {
		t.Fatalf("hooks %+v", got)
	}
	if _, err := bonsaiHooks([]byte(`"nope"`), hooksFile); err == nil {
		t.Fatal("unreadable reply accepted")
	}
}

func containsArg(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}
