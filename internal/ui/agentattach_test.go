package ui

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/core/agentterminal"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi/fake"
)

// liveEnv is an agents env with one running session "a" on wtA and the tab open.
func liveEnv(t *testing.T) *agentsEnv {
	t.Helper()
	e := newAgentsEnv(t)
	e.fb.Sessions.Add(e.agent("a", "claude", "running", e.wtA, "Auth fix", 1))
	e.relist(t)
	e.openTab(t)
	return e
}

// attached delivers the result of the attach the last update asked for and
// returns the fake stream it produced.
func (e *agentsEnv) attached(t *testing.T) *fake.Stream {
	t.Helper()
	for _, msg := range flatten(e.lastCmd) {
		if am, ok := msg.(agentAttachedMsg); ok {
			nm, cmd := e.m.Update(am)
			e.m, e.lastCmd = nm.(Model), cmd
			streams := e.fb.Sessions.Streams(e.m.agents.view.sessionID)
			return streams[len(streams)-1]
		}
	}
	t.Fatal("the last update issued no attach")
	return nil
}

func (e *agentsEnv) send(t *testing.T, msg tea.Msg) {
	t.Helper()
	nm, cmd := e.m.Update(msg)
	e.m, e.lastCmd = nm.(Model), cmd
}

func (e *agentsEnv) frames(t *testing.T, fs ...agentterminal.Frame) {
	t.Helper()
	e.send(t, agentFramesMsg{token: e.m.agents.view.token, frames: fs})
}

func out(s string) agentterminal.Frame { return agentterminal.Frame{Type: "output", Data: []byte(s)} }

func status(state string) agentterminal.Frame {
	return agentterminal.Frame{Type: "status", Status: &agentterminal.Summary{State: state}}
}

func TestAgentsAttachesReadOnlyOnTabOpen(t *testing.T) {
	e := liveEnv(t)
	st := e.attached(t)
	calls := e.fb.Sessions.Attaches()
	if len(calls) != 1 || calls[0].ID != "a" || !calls[0].Opts.ReadOnly || calls[0].Opts.Take {
		t.Fatalf("attaches = %+v, want one read-only attach of a", calls)
	}
	if st.Writer() {
		t.Error("the view must never write")
	}
	if e.m.agents.view.state != attachObserving {
		t.Errorf("state = %v", e.m.agents.view.state)
	}
}

func TestAgentsNoAttachOffTab(t *testing.T) {
	e := newAgentsEnv(t)
	e.fb.Sessions.Add(e.agent("a", "claude", "running", e.wtA, "Auth fix", 1))
	e.relist(t)
	e.selectWT(e.wtA)
	e.send(t, tea.WindowSizeMsg{Width: 100, Height: 30})
	if e.m.agents.view != nil || len(e.fb.Sessions.Attaches()) != 0 {
		t.Error("attached while the Agents tab is not showing")
	}
}

func TestAgentsLiveOutputRenders(t *testing.T) {
	for _, size := range [][2]int{{120, 40}, {80, 24}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			e := liveEnv(t)
			e.m.width, e.m.height = size[0], size[1]
			e.attached(t)
			e.frames(t, out("hello from the agent"), status("running"))
			view := ansi.Strip(e.m.View())
			for _, want := range []string{"hello from the agent"} {
				if !strings.Contains(view, want) {
					t.Errorf("View lacks %q:\n%s", want, view)
				}
			}
			lines := strings.Split(e.m.View(), "\n")
			if len(lines) > size[1] {
				t.Errorf("%d rows > %d", len(lines), size[1])
			}
			for _, l := range lines {
				if ansi.StringWidth(l) > size[0] {
					t.Errorf("line wider than %d: %q", size[0], ansi.Strip(l))
				}
			}
		})
	}
}

func TestAgentsReplayEndsAtFirstStatus(t *testing.T) {
	e := liveEnv(t)
	e.attached(t)
	v := e.m.agents.view
	if !v.replaying {
		t.Fatal("a new attach starts in replay")
	}
	e.frames(t, out("history"))
	if !v.replaying {
		t.Error("output alone must not end the replay")
	}
	e.frames(t, status("running"))
	if v.replaying {
		t.Error("the first status frame ends the replay")
	}
	if v.offset != uint64(len("history")) {
		t.Errorf("offset = %d", v.offset)
	}
}

func TestAgentsGapRecreatesEmulator(t *testing.T) {
	e := liveEnv(t)
	e.attached(t)
	e.frames(t, out("stale screen"))
	old := e.m.agents.view.term
	e.frames(t, agentterminal.Frame{Type: "gap", Offset: 100}, out("fresh"))
	if e.m.agents.view.term == old {
		t.Fatal("gap must replace the emulator")
	}
	view := ansi.Strip(e.m.View())
	if strings.Contains(view, "stale screen") || !strings.Contains(view, "fresh") {
		t.Errorf("screen after gap:\n%s", view)
	}
}

func TestAgentsEndedBanner(t *testing.T) {
	code := 3
	tests := []struct {
		name string
		st   agentterminal.Summary
		want string
	}{
		{"exited", agentterminal.Summary{State: "exited", ExitCode: &code}, "exited (code 3) · n: new · a: all"},
		{"failed", agentterminal.Summary{State: "failed", Error: "spawn failed"}, "failed: spawn failed"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := liveEnv(t)
			e.attached(t)
			st := tc.st
			e.frames(t, out("last words"), agentterminal.Frame{Type: "status", Status: &st})
			if e.m.agents.view.state != attachEnded {
				t.Fatalf("state = %v", e.m.agents.view.state)
			}
			pane := strings.Split(e.pane(120, 20), "\n")
			if !strings.Contains(pane[len(pane)-1], tc.want) {
				t.Errorf("banner row = %q, want %q", pane[len(pane)-1], tc.want)
			}
			if !strings.Contains(strings.Join(pane, "\n"), "last words") {
				t.Error("the final screen should stay visible")
			}
		})
	}
}

func TestAgentsAttachErrorRetries(t *testing.T) {
	e := liveEnv(t)
	e.fb.Sessions.AttachErr = errors.New("dial unix: refused")
	// The attach issued when the tab opened fails once: a retry is scheduled.
	msgs := flatten(e.lastCmd)
	var failed agentAttachedMsg
	for _, m := range msgs {
		if am, ok := m.(agentAttachedMsg); ok {
			failed = am
		}
	}
	if failed.err == nil {
		t.Fatal("expected a failed attach")
	}
	e.send(t, failed)
	if e.m.agents.view.err != "" {
		t.Fatal("the first failure retries silently")
	}
	if e.lastCmd == nil {
		t.Fatal("no retry scheduled")
	}

	e.send(t, agentRetryMsg{token: e.m.agents.view.token})
	var second agentAttachedMsg
	for _, m := range flatten(e.lastCmd) {
		if am, ok := m.(agentAttachedMsg); ok {
			second = am
		}
	}
	if second.err == nil {
		t.Fatal("retry should dial again")
	}
	e.send(t, second)
	pane := e.pane(120, 20)
	if !strings.Contains(pane, "Could not attach") || !strings.Contains(pane, "enter: retry") {
		t.Errorf("error row missing:\n%s", pane)
	}
	if strings.Contains(pane, "refused") {
		t.Error("transport details must not reach the screen")
	}

	// enter attaches again, with a new token.
	e.fb.Sessions.AttachErr = nil
	oldToken := e.m.agents.view.token
	e.send(t, tea.KeyMsg{Type: tea.KeyEnter})
	if e.m.agents.view.token == oldToken {
		t.Fatal("enter should start a new attach")
	}
	e.attached(t)
	if e.m.agents.view.err != "" || e.m.agents.view.term == nil {
		t.Error("retry did not recover")
	}
}

func TestAgentsVersionMismatchShowsHint(t *testing.T) {
	e := liveEnv(t)
	e.send(t, agentAttachedMsg{token: e.m.agents.view.token, err: fmt.Errorf("attach: %w", agentsapi.ErrVersion)})
	pane := e.pane(120, 20)
	if !strings.Contains(pane, "Started by another bonsai version; run bonsai agents attach Auth fix") {
		t.Errorf("pane:\n%s", pane)
	}
	if strings.Contains(pane, "enter: retry") {
		t.Error("retrying cannot fix a version mismatch")
	}
	if e.lastCmd != nil {
		t.Error("a version mismatch must not retry")
	}
}

func TestAgentsSwitchReleasesOldStreamAndIgnoresStaleMsgs(t *testing.T) {
	e := newAgentsEnv(t)
	e.fb.Sessions.Add(
		e.agent("a", "claude", "running", e.wtA, "One", 1),
		e.agent("b", "codex", "running", e.wtA, "Two", 2),
	)
	e.relist(t)
	e.openTab(t) // b is selected
	old := e.attached(t)
	oldToken := e.m.agents.view.token
	e.frames(t, out("from b"))

	e.key(t, "1") // switch to a
	flatten(e.lastCmd)
	if !old.Closed() {
		t.Fatal("switching must close the previous stream")
	}
	newView := e.m.agents.view
	if newView.sessionID != "a" || newView.token == oldToken {
		t.Fatalf("view = %+v", newView)
	}

	// Late messages of the old stream change nothing.
	e.send(t, agentFramesMsg{token: oldToken, frames: []agentterminal.Frame{out("late b output")}})
	e.send(t, agentFramesMsg{token: oldToken, closed: true})
	if newView.err != "" || newView.term != nil {
		t.Error("stale message leaked into the new view")
	}

	// A dial that finishes after the user moved on must not leak its stream.
	stale, err := e.fb.Sessions.Attach(t.Context(), "b", agentsapi.AttachOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	e.send(t, agentAttachedMsg{token: oldToken, stream: stale})
	flatten(e.lastCmd)
	if !stale.(*fake.Stream).Closed() {
		t.Error("stale attach result must be closed")
	}
	if newView.term != nil {
		t.Error("stale attach installed a stream")
	}
}

func TestAgentsLeavingTabClosesStream(t *testing.T) {
	e := liveEnv(t)
	st := e.attached(t)
	e.m.rightTab = tabLog
	e.send(t, tea.WindowSizeMsg{Width: 100, Height: 30})
	flatten(e.lastCmd)
	if !st.Closed() || e.m.agents.view != nil {
		t.Errorf("closed=%v view=%v", st.Closed(), e.m.agents.view)
	}
}

func TestAgentsWorktreeChangeClosesStream(t *testing.T) {
	e := liveEnv(t)
	e.fb.Sessions.Add(e.agent("b", "codex", "running", e.wtB, "Bee", 1))
	e.relist(t)
	st := e.attached(t)
	e.selectWT(e.wtB)
	e.send(t, tea.WindowSizeMsg{Width: 100, Height: 30})
	flatten(e.lastCmd)
	if !st.Closed() {
		t.Error("moving to another worktree must close the stream")
	}
	if v := e.m.agents.view; v == nil || v.sessionID != "b" {
		t.Errorf("view = %+v, want an attach of b", v)
	}
}

func TestAgentsQuitReleasesView(t *testing.T) {
	e := liveEnv(t)
	st := e.attached(t)
	cmd := e.m.agents.quit()
	// tea.Sequence hides its commands; run them the way the runtime would.
	seq := reflect.ValueOf(cmd())
	if seq.Kind() != reflect.Slice {
		t.Fatalf("quit returned %T, want a sequence", seq.Interface())
	}
	for i := 0; i < seq.Len(); i++ {
		if c, ok := seq.Index(i).Interface().(tea.Cmd); ok && c != nil {
			c()
		}
	}
	if !st.Closed() {
		t.Error("quit must close the live stream")
	}
	if e.m.agents.ctx.Err() == nil {
		t.Error("quit must cancel the agent context")
	}
}

func TestWaitFramesBatchesAndCloses(t *testing.T) {
	st := fake.NewStream(80, 24)
	for i := 0; i < 256; i++ {
		if !st.Push(out("x")) {
			t.Fatal("push failed")
		}
	}
	msg := waitFramesCmd(7, st)().(agentFramesMsg)
	if msg.token != 7 || len(msg.frames) != maxFrameBatch || msg.closed {
		t.Fatalf("first batch: token=%d frames=%d closed=%v", msg.token, len(msg.frames), msg.closed)
	}
	st.Push(out("y"))
	msg = waitFramesCmd(7, st)().(agentFramesMsg)
	if len(msg.frames) != 1 {
		t.Errorf("second batch = %d frames", len(msg.frames))
	}
	_ = st.Close()
	if msg = waitFramesCmd(7, st)().(agentFramesMsg); !msg.closed {
		t.Error("a closed stream must report closed")
	}
}

func TestAgentsDisconnectOffersRetry(t *testing.T) {
	e := liveEnv(t)
	e.attached(t)
	e.send(t, agentFramesMsg{token: e.m.agents.view.token, closed: true})
	pane := e.pane(120, 20)
	if !strings.Contains(pane, "disconnected · enter: retry") {
		t.Errorf("pane:\n%s", pane)
	}
}

func TestAgentsReadOnlyHeaderSuffix(t *testing.T) {
	e := liveEnv(t)
	e.attached(t)
	if strings.Contains(e.pane(120, 30), "read-only") {
		t.Error("read-only suffix only applies when another client controls")
	}
	e.m.agents.view.state = attachObservingOther
	if !strings.Contains(e.pane(120, 30), "● Running · read-only") {
		t.Errorf("header lacks the suffix:\n%s", e.pane(120, 30))
	}
}
