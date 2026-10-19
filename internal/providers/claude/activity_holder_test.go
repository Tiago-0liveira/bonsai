//go:build linux || darwin

package claude

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

func TestClaudeHooksDriveActivityThroughHolder(t *testing.T) {
	e := newTestEnv(t)
	work := e.addLogin("work")
	t.Setenv(hookRootEnv, e.sessions.Root())
	t.Setenv(hookClockEnv, filepath.Join(t.TempDir(), "clock"))
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	hook := []string{exe, "__agent-hook"}
	m, paths := newManagerWithHook(t, e.accounts, e.sessions, e.registry, hook)

	s, err := m.Start("project", "tree", work.ID, "", t.TempDir(), 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	awaitRunning(t, m, s.ID)
	term := attach(t, m, s.ID)
	term.waitFor("HOOKS=1")
	term.waitFor("READY")
	term.waitFor("ARG:--settings")
	spool := filepath.Join(e.sessions.Root(), s.ID, agents.ActivitySpoolName)

	record, err := agenthost.Read(paths, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	conn, ready, err := agenthost.Dial(record, agenthost.Request{Mode: agenthost.ModeStatus})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	trail := &statusTrail{t: t, conn: conn}
	trail.add(ready.Status.Activity)

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
	// The manager follows the holder over its own status connection.
	deadline := time.Now().Add(10 * time.Second)
	for {
		got, _ := m.Get("project", s.ID)
		if got.Activity != nil && got.Activity.State == agentactivity.Waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("manager summary activity %+v, want waiting", got.Activity)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if r, err := agenthost.Read(paths, s.ID); err != nil || r.Summary.Activity == nil || r.Summary.Activity.State != agentactivity.Waiting {
		t.Fatalf("record while waiting: %+v %v", r.Summary.Activity, err)
	}
	hookLine("PostToolUse", `{"tool_name":"Bash","tool_response":{"stdout":"SECRET-OUTPUT"}}`, agentactivity.Working)
	// Claude emits permission hooks and generic permission notifications for
	// these input dialogs too; neither should replace the question or plan.
	for _, tool := range []string{"AskUserQuestion", "ExitPlanMode"} {
		hookLine("PreToolUse", `{"tool_name":"`+tool+`","tool_input":{"prompt":"SECRET-QUESTION"}}`, agentactivity.Waiting)
		hookLine("PermissionRequest", `{"tool_name":"`+tool+`","tool_input":{"prompt":"SECRET-QUESTION"}}`, agentactivity.Waiting)
		hookLine("Notification", `{"notification_type":"permission_prompt","message":"SECRET-NOTIFICATION"}`, agentactivity.Waiting)
		hookLine("PostToolUse", `{"tool_name":"`+tool+`","tool_response":"SECRET-ANSWER"}`, agentactivity.Working)
	}
	// A real tool permission prompt still reports permission after a question.
	hookLine("PermissionRequest", `{"tool_name":"Bash","tool_input":{"command":"SECRET-COMMAND"}}`, agentactivity.Waiting)
	hookLine("Notification", `{"notification_type":"permission_prompt","message":"SECRET-NOTIFICATION"}`, agentactivity.Waiting)
	hookLine("PostToolUse", `{"tool_name":"Bash","tool_response":"SECRET-OUTPUT"}`, agentactivity.Working)
	hookLine("Stop", `{"last_assistant_message":"SECRET-REPLY"}`, agentactivity.Idle)

	if want := []string{"idle/ready", "working", "waiting/permission", "working", "waiting/question", "working", "waiting/plan", "working", "waiting/permission", "working", "idle/turn_complete"}; !reflect.DeepEqual(trail.trail, want) {
		t.Fatalf("activity trail %v, want %v", trail.trail, want)
	}
	rec, err := agenthost.Read(paths, s.ID)
	if err != nil || rec.Summary.Activity == nil || rec.Summary.Activity.State != agentactivity.Idle || rec.Summary.Activity.Reason != agentactivity.ReasonTurnComplete {
		t.Fatalf("record %+v %v", rec.Summary.Activity, err)
	}
	raw, err := os.ReadFile(filepath.Join(paths.Records, s.ID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"SECRET", "tool_input", "cat "} {
		if bytes.Contains(raw, []byte(secret)) {
			t.Fatalf("record leaks %q:\n%s", secret, raw)
		}
	}
	spoolText, err := os.ReadFile(spool)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(spoolText, []byte("SECRET")) {
		t.Fatalf("spool leaks hook payload content:\n%s", spoolText)
	}

	// Bonsai's hooks live in the session's argv, never in the profile.
	if _, err := os.Stat(filepath.Join(configDir(e.accounts, work), "settings.json")); err == nil {
		t.Fatal("the profile's settings.json was written")
	}

	if err := m.Stop("project", s.ID); err != nil {
		t.Fatal(err)
	}
	awaitState(t, m, s.ID, "exited", "stopped")
	deadline = time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(spool); os.IsNotExist(err) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("spool outlived the session")
		}
		time.Sleep(10 * time.Millisecond)
	}

	// A hook still running when the session was cleaned up finds no spool,
	// creates nothing and reports success.
	late := exec.Command(exe, "__agent-hook", "claude", "Stop")
	late.Env = append(os.Environ(), agents.ActivityEnv+"="+spool)
	late.Stdin = strings.NewReader(`{}`)
	out, err := late.CombinedOutput()
	if err != nil || len(out) != 0 {
		t.Fatalf("late hook: err=%v output=%q", err, out)
	}
	if _, err := os.Stat(filepath.Dir(spool)); !os.IsNotExist(err) {
		t.Fatalf("late hook recreated the session directory: %v", err)
	}
}

func TestClaudeSessionWithoutHookCommandHasNoHooks(t *testing.T) {
	// Holders that cannot name a hook command (this test binary is not bonsai)
	// start Claude without hooks: activity stays unknown, nothing fails.
	e := newTestEnv(t)
	work := e.addLogin("work")
	m := newManager(t, e.accounts, e.sessions, e.registry)
	s, err := m.Start("project", "tree", work.ID, "", t.TempDir(), 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	awaitRunning(t, m, s.ID)
	term := attach(t, m, s.ID)
	term.waitFor("HOOKS=0")
	term.waitFor("READY")
	if got, _ := m.Get("project", s.ID); got.Activity != nil && got.Activity.State != agentactivity.Unknown {
		t.Fatalf("activity %+v, want unknown", got.Activity)
	}
}
