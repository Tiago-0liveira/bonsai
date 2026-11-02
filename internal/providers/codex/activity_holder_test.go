//go:build linux || darwin

package codex

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Tiago-0liveira/bonsai/internal/core/agentactivity"
	"github.com/Tiago-0liveira/bonsai/internal/core/agenthook"
	"github.com/Tiago-0liveira/bonsai/internal/core/agenthost"
	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
)

// hookRootEnv makes the test binary act as `bonsai __agent-hook` (the holder's
// hook command here is this binary), with the sessions root the test uses.
const hookRootEnv = "BONSAI_TEST_HOOK_SESSIONS_ROOT"

// hookClockEnv names a file that orders the hooks' timestamps across processes.
const hookClockEnv = "BONSAI_TEST_HOOK_CLOCK"

// hookClock stamps each hook later than the previous one. The reducer drops a
// hook older than one it already saw, and the wall clock can step back between
// two hook processes (WSL2 loses about a second every half minute), which made
// a real next event look stale. The stamps also run a minute ahead so the
// holder's own earlier stamps never outrank them. Hooks here run one at a time.
func hookClock(path string) func() time.Time {
	return func() time.Time {
		at := time.Now().Add(time.Minute)
		if raw, err := os.ReadFile(path); err == nil {
			if last, err := strconv.ParseInt(string(raw), 10, 64); err == nil && last >= at.UnixNano() {
				at = time.Unix(0, last+1)
			}
		}
		_ = os.WriteFile(path, []byte(strconv.FormatInt(at.UnixNano(), 10)), 0o600)
		return at
	}
}

func TestMain(m *testing.M) {
	if root := os.Getenv(hookRootEnv); root != "" && len(os.Args) > 1 && os.Args[1] == "__agent-hook" {
		os.Exit(agenthook.Run(agenthook.Options{
			Args: os.Args[2:], Stdin: os.Stdin, Stdout: os.Stdout, SessionsRoot: root, Now: hookClock(os.Getenv(hookClockEnv)),
			Lookup: func(id string) (agents.ActivityReporter, bool) { return New(nil, nil), id == string(ProviderID) },
		}))
	}
	os.Exit(m.Run())
}

// statusTrail follows a holder's status connection and keeps the activity
// states it reports, consecutive repeats collapsed.
type statusTrail struct {
	t       *testing.T
	conn    *agenthost.Conn
	trail   []string
	current *agentactivity.Activity
}

func (s *statusTrail) add(a *agentactivity.Activity) {
	if a == nil {
		return
	}
	s.current = a
	if a.State == agentactivity.Unknown {
		return
	}
	label := string(a.State)
	if a.Reason != "" {
		label += "/" + string(a.Reason)
	}
	if n := len(s.trail); n == 0 || s.trail[n-1] != label {
		s.trail = append(s.trail, label)
	}
}

func (s *statusTrail) until(state agentactivity.State) {
	s.t.Helper()
	_ = s.conn.SetDeadline(time.Now().Add(10 * time.Second))
	for s.current == nil || s.current.State != state {
		f, err := s.conn.Recv()
		if err != nil {
			s.t.Fatalf("status connection ended waiting for %s: trail %v (%v)", state, s.trail, err)
		}
		if f.Status != nil {
			s.add(f.Status.Activity)
		}
	}
}

func followStatus(t *testing.T, paths agenthost.Paths, id string) *statusTrail {
	t.Helper()
	record, err := agenthost.Read(paths, id)
	if err != nil {
		t.Fatal(err)
	}
	conn, ready, err := agenthost.Dial(record, agenthost.Request{Mode: agenthost.ModeStatus})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	trail := &statusTrail{t: t, conn: conn}
	trail.add(ready.Status.Activity)
	return trail
}

func TestCodexHooksDriveActivityThroughHolder(t *testing.T) {
	e := newTestEnv(t)
	work := e.addLogin("work")
	t.Setenv(hookRootEnv, e.sessions.Root())
	t.Setenv(hookClockEnv, filepath.Join(t.TempDir(), "clock"))
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	m, paths := newManagerWithHook(t, e.accounts, e.sessions, e.registry, []string{exe, "__agent-hook"})

	s, err := m.Start("project", "tree", work.ID, "", t.TempDir(), 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	awaitState(t, m, s.ID, "running")
	term := attach(t, m, s.ID)
	term.waitFor("HOOKS=1")
	term.waitFor("READY")
	term.waitFor("ARG:tui.notification_method=\"osc9\"")
	spool := filepath.Join(e.sessions.Root(), s.ID, agents.ActivitySpoolName)
	trail := followStatus(t, paths, s.ID)

	hookLine := func(event, payload string, want agentactivity.State) {
		t.Helper()
		term.send("HOOK " + event + " " + payload)
		term.waitFor("HOOKED:" + event)
		trail.until(want)
	}
	trail.until(agentactivity.Idle) // SessionStart, fired by the fake agent on startup
	hookLine("UserPromptSubmit", `{"prompt":"SECRET-PROMPT"}`, agentactivity.Working)
	hookLine("PreToolUse", `{"tool_name":"Bash","tool_input":{"command":"cat SECRET-INPUT"}}`, agentactivity.Working)
	hookLine("PermissionRequest", `{"tool_name":"Bash","tool_input":{"command":"cat SECRET-INPUT"}}`, agentactivity.Waiting)
	if a := trail.current; a.Reason != agentactivity.ReasonPermission || a.Detail != "Bash" || a.Source != agentactivity.SourceHook || a.Inferred {
		t.Fatalf("waiting activity %+v", a)
	}
	hookLine("PostToolUse", `{"tool_name":"Bash","tool_response":"SECRET-OUTPUT"}`, agentactivity.Working)
	hookLine("PreToolUse", `{"tool_name":"request_user_input","tool_input":{"questions":[{"question":"SECRET-QUESTION"}]}}`, agentactivity.Waiting)
	if a := trail.current; a.Reason != agentactivity.ReasonQuestion || a.Detail != "" {
		t.Fatalf("question activity %+v", a)
	}
	hookLine("PostToolUse", `{"tool_name":"request_user_input","tool_response":"SECRET-ANSWER"}`, agentactivity.Working)
	hookLine("PreToolUse", `{"tool_name":"request_user_input_async","tool_input":{"questions":[{"question":"SECRET-ASYNC-QUESTION"}]}}`, agentactivity.Waiting)
	hookLine("PostToolUse", `{"tool_name":"request_user_input_async","tool_response":"SECRET-ASYNC-RETURN"}`, agentactivity.Waiting)
	hookLine("PreToolUse", `{"tool_name":"Bash","tool_input":{"command":"SECRET-WORK"}}`, agentactivity.Waiting)
	hookLine("PostToolUse", `{"tool_name":"Bash","tool_response":"SECRET-OUTPUT"}`, agentactivity.Waiting)
	hookLine("Stop", `{"last_assistant_message":"SECRET-STOP"}`, agentactivity.Waiting)
	hookLine("UserPromptSubmit", `{"prompt":"SECRET-ASYNC-ANSWER"}`, agentactivity.Working)
	hookLine("Interrupt", `{"turn_id":"t1"}`, agentactivity.Idle)
	hookLine("UserPromptSubmit", `{"prompt":"again"}`, agentactivity.Working)
	hookLine("Stop", `{"last_assistant_message":"SECRET-REPLY"}`, agentactivity.Idle)

	want := []string{"idle/ready", "working", "waiting/permission", "working", "waiting/question", "working", "waiting/question", "working", "idle/interrupted", "working", "idle/turn_complete"}
	if !reflect.DeepEqual(trail.trail, want) {
		t.Fatalf("activity trail %v, want %v", trail.trail, want)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		if got, _ := m.Get("project", s.ID); got.Activity != nil && got.Activity.State == agentactivity.Idle && got.Activity.Reason == agentactivity.ReasonTurnComplete {
			break
		}
		if time.Now().After(deadline) {
			got, _ := m.Get("project", s.ID)
			t.Fatalf("manager summary activity %+v, want idle", got.Activity)
		}
		time.Sleep(10 * time.Millisecond)
	}
	raw, err := os.ReadFile(filepath.Join(paths.Records, s.ID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	spoolText, err := os.ReadFile(spool)
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{"record": raw, "spool": spoolText} {
		for _, secret := range []string{"SECRET", "tool_input", "cat "} {
			if bytes.Contains(data, []byte(secret)) {
				t.Fatalf("%s leaks %q:\n%s", name, secret, data)
			}
		}
	}

	// Bonsai's hooks are in the profile's own CODEX_HOME, never in the user's.
	if _, err := os.Stat(hooksPath(e.profileHome(work))); err != nil {
		t.Fatalf("profile hooks.json: %v", err)
	}
	for _, path := range []string{filepath.Join(e.home, ".codex"), filepath.Join(e.home, ".claude")} {
		if _, err := os.Stat(path); err == nil {
			t.Fatalf("%s was created", path)
		}
	}

	if err := m.Stop("project", s.ID); err != nil {
		t.Fatal(err)
	}
	awaitState(t, m, s.ID, "exited", "stopped")

	// A hook still running when the session was cleaned up finds no spool,
	// creates nothing and reports success.
	late := exec.Command(exe, "__agent-hook", "codex", "Stop")
	late.Env = append(os.Environ(), agents.ActivityEnv+"="+spool)
	late.Stdin = strings.NewReader(`{}`)
	if out, err := late.CombinedOutput(); err != nil || len(out) != 0 {
		t.Fatalf("late hook: err=%v output=%q", err, out)
	}
}

// When the hooks cannot be trusted the session still starts, and Codex's OSC 9
// notifications (asked for with -c) keep needs-input visible, marked inferred.
func TestCodexFallsBackToTerminalNotificationsWhenTrustFails(t *testing.T) {
	old := appServerTimeout
	appServerTimeout = 2 * time.Second
	t.Cleanup(func() { appServerTimeout = old })
	e := newTestEnv(t)
	work := e.addLogin("work")
	t.Setenv(hookRootEnv, e.sessions.Root())
	t.Setenv("FAKE_CODEX_APPSERVER", "error")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	m, paths := newManagerWithHook(t, e.accounts, e.sessions, e.registry, []string{exe, "__agent-hook"})

	s, err := m.Start("project", "tree", work.ID, "", t.TempDir(), 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	awaitState(t, m, s.ID, "running")
	term := attach(t, m, s.ID)
	term.waitFor("READY")
	trail := followStatus(t, paths, s.ID)

	// Untrusted hooks do not run: nothing arrives from this one.
	term.send(`HOOK PermissionRequest {"tool_name":"Bash"}`)
	term.waitFor("HOOKED:PermissionRequest")
	term.send("NOTIFY Approval requested: git push origin main")
	term.waitFor("NOTIFIED")
	trail.until(agentactivity.Waiting)
	a := trail.current
	if a.Reason != agentactivity.ReasonPermission || a.Source != agentactivity.SourceTerminal || !a.Inferred || a.Detail != "" {
		t.Fatalf("activity %+v", a)
	}
	if !reflect.DeepEqual(trail.trail, []string{"waiting/permission"}) {
		t.Fatalf("trail %v: the untrusted hook must not have reported", trail.trail)
	}
	if raw, _ := os.ReadFile(filepath.Join(paths.Records, s.ID+".json")); bytes.Contains(raw, []byte("git push")) {
		t.Fatalf("record leaks the notification text:\n%s", raw)
	}
}
