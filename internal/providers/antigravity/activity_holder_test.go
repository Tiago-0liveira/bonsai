//go:build linux || darwin

package antigravity

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

// TestMain makes the test binary act as `bonsai __agent-hook`, which the fake
// agy's hook command runs. It finds the default sessions root the way
// agentruntime.RunHook does: from the environment it was started with, where
// agy's HOME override applies.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "__agent-hook" {
		root, _ := agents.DefaultSessionsRoot()
		os.Exit(agenthook.Run(agenthook.Options{
			Args: os.Args[2:], Stdin: os.Stdin, Stdout: os.Stdout, SessionsRoot: root, Now: hookClock(os.Getenv(hookClockEnv)),
			Lookup: func(id string) (agents.ActivityReporter, bool) { return New(nil, nil), id == string(ProviderID) },
		}))
	}
	os.Exit(m.Run())
}

type holderTerminal struct {
	t      *testing.T
	a      *agenthost.Attachment
	output strings.Builder
}

func (term *holderTerminal) waitFor(want string) {
	term.t.Helper()
	deadline := time.After(10 * time.Second)
	for !strings.Contains(term.output.String(), want) {
		select {
		case f, ok := <-term.a.Frames:
			if !ok {
				term.t.Fatalf("terminal closed before %q; output:\n%s", want, term.output.String())
			}
			term.output.Write(f.Data)
		case <-deadline:
			term.t.Fatalf("timeout waiting for %q; output:\n%s", want, term.output.String())
		}
	}
}

func (term *holderTerminal) send(line string) {
	term.t.Helper()
	if err := term.a.Input([]byte(line + "\n")); err != nil {
		term.t.Fatal(err)
	}
}

func awaitHolderState(t *testing.T, m *agenthost.Manager, id string, states ...string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		s, _ := m.Get("project", id)
		for _, want := range states {
			if s.State == want {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	s, _ := m.Get("project", id)
	t.Fatalf("session %s: want %v, got %+v", id, states, s)
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

func (s *statusTrail) until(state agentactivity.State, reason agentactivity.Reason) {
	s.t.Helper()
	_ = s.conn.SetDeadline(time.Now().Add(10 * time.Second))
	for s.current == nil || s.current.State != state || s.current.Reason != reason {
		f, err := s.conn.Recv()
		if err != nil {
			s.t.Fatalf("status connection ended waiting for %s/%s: trail %v (%v)", state, reason, s.trail, err)
		}
		if f.Status != nil {
			s.add(f.Status.Activity)
		}
	}
}

func TestAntigravityHooksDriveActivityThroughHolder(t *testing.T) {
	// agy's session HOME must not be what makes the hook find its spool.
	t.Setenv("HOME", t.TempDir())
	t.Setenv(hookClockEnv, filepath.Join(t.TempDir(), "clock"))
	t.Setenv("XDG_CACHE_HOME", "")
	if err := os.Unsetenv("XDG_CACHE_HOME"); err != nil {
		t.Fatal(err)
	}
	fake, err := filepath.Abs("testdata/fakeagy.sh")
	if err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	accounts, err := agents.NewFileAccountStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	sessions, err := agents.NewFileSessionStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	account := agents.Account{ID: "acct_agy", Provider: ProviderID, Name: "agy", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
	if err := accounts.Create(account); err != nil {
		t.Fatal(err)
	}
	provider := &Provider{accounts: accounts, sessions: sessions, binaryResolver: &providerTestBinaryResolver{path: fake}, credentialMgr: &providerTestCredentialManager{}}
	registry := agents.NewRegistry()
	if err := registry.Register(provider); err != nil {
		t.Fatal(err)
	}
	// macOS's default temp directory can exceed the Unix socket path limit.
	sockets, err := os.MkdirTemp("/tmp", "bah")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(sockets) })
	paths := agenthost.Paths{Records: filepath.Join(t.TempDir(), "agents"), Sockets: sockets}
	m := agenthost.NewManager(accounts, sessions, registry, paths, agenthost.InProcess(agenthost.Deps{
		Accounts: accounts, Sessions: sessions, Registry: registry, Linger: 200 * time.Millisecond,
		HookCommand: []string{exe, "__agent-hook"},
	}), nil)
	t.Cleanup(func() { m.Close(false, 10*time.Second) })

	s, err := m.Start("project", "tree", account.ID, "", t.TempDir(), 80, 24)
	if err != nil {
		t.Fatal(err)
	}
	awaitHolderState(t, m, s.ID, "running")
	a, err := m.Attach("project", s.ID, 0, "", "web")
	if err != nil || !a.Writer() {
		t.Fatalf("attach: %v", err)
	}
	t.Cleanup(a.Close)
	term := &holderTerminal{t: t, a: a}
	for _, f := range a.Replay {
		term.output.Write(f.Data)
	}
	sessionHome := filepath.Join(sessions.Root(), s.ID, "home")
	term.waitFor("HOME=" + sessionHome)
	term.waitFor("HOOKS=1")
	term.waitFor("SPOOLENV=1")
	term.waitFor("READY")
	spool := filepath.Join(sessions.Root(), s.ID, agents.ActivitySpoolName)

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

	hook := func(event, payload string, state agentactivity.State, reason agentactivity.Reason) {
		t.Helper()
		term.send("HOOK " + event + " " + payload)
		term.waitFor("HOOKED:" + event)
		trail.until(state, reason)
	}
	trail.until(agentactivity.Idle, agentactivity.ReasonReady) // SessionStart, fired on startup
	hook("PreInvocation", `{"invocationNum":1,"conversationId":"c"}`, agentactivity.Working, "")
	term.send("NOTIFY The agent has a question for you: SECRET-QUESTION")
	term.waitFor("NOTIFIED")
	trail.until(agentactivity.Waiting, agentactivity.ReasonQuestion)
	if a := trail.current; a.Source != agentactivity.SourceTerminal || !a.Inferred {
		t.Fatalf("waiting must come from the terminal and be inferred: %+v", a)
	}
	hook("PostToolUse", `{"toolCall":{"name":"run_command","args":{"CommandLine":"cat SECRET-INPUT"}},"error":"SECRET-ERROR"}`, agentactivity.Working, "")
	if a := trail.current; a.Detail != "run_command" || a.Source != agentactivity.SourceHook || a.Inferred {
		t.Fatalf("working activity %+v", a)
	}
	hook("PostInvocation", `{"invocationNum":1}`, agentactivity.Working, "")
	hook("Stop", `{"terminationReason":"NO_TOOL_CALL","fullyIdle":true}`, agentactivity.Idle, agentactivity.ReasonTurnComplete)
	hook("PreInvocation", `{"invocationNum":2}`, agentactivity.Working, "")
	hook("Stop", `{"terminationReason":"ERROR","error":"SECRET-FAILURE"}`, agentactivity.Idle, agentactivity.ReasonError)
	if a := trail.current; a.Detail != "error" {
		t.Fatalf("error activity %+v: only the kind, never the text", a)
	}
	hook("PreInvocation", `{"invocationNum":3}`, agentactivity.Working, "")
	hook("Stop", `{"terminationReason":"USER_CANCELED"}`, agentactivity.Idle, agentactivity.ReasonInterrupted)

	want := []string{
		"idle/ready", "working", "waiting/question", "working", "idle/turn_complete",
		"working", "idle/error", "working", "idle/interrupted",
	}
	if !reflect.DeepEqual(trail.trail, want) {
		t.Fatalf("activity trail %v, want %v", trail.trail, want)
	}
	// agy needs JSON on every hook's stdout.
	term.send("done")
	term.waitFor("REPLY:done")
	if n := strings.Count(term.output.String(), "HOOKOUT:{}"); n != 9 {
		t.Fatalf("%d hooks printed {}, want 9 (SessionStart + 8 events):\n%s", n, term.output.String())
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
		for _, secret := range []string{"SECRET", "CommandLine", "cat "} {
			if bytes.Contains(data, []byte(secret)) {
				t.Fatalf("%s leaks %q:\n%s", name, secret, data)
			}
		}
	}

	// The control for the point of the sessions-root argument: the same hook,
	// run without it under agy's HOME, derives another root and is rejected.
	bare := exec.Command(exe, "__agent-hook", string(ProviderID), "Stop")
	bare.Env = append(os.Environ(), "HOME="+sessionHome, agents.ActivityEnv+"="+spool)
	bare.Stdin = strings.NewReader(`{"terminationReason":"NO_TOOL_CALL"}`)
	if out, err := bare.CombinedOutput(); err != nil || string(out) != "{}" {
		t.Fatalf("bare hook: err=%v out=%q", err, out)
	}
	if after, _ := os.ReadFile(spool); !bytes.Equal(after, spoolText) {
		t.Fatalf("a hook without the sessions root appended to the spool:\n%s", after)
	}

	// Nothing of it reaches the profile's stores.
	if _, err := os.Stat(hooksPath(probeHomeDir(accounts, account))); !os.IsNotExist(err) {
		t.Fatalf("probe home has hooks: %v", err)
	}
	if err := m.Stop("project", s.ID); err != nil {
		t.Fatal(err)
	}
	awaitHolderState(t, m, s.ID, "exited", "stopped")
}
