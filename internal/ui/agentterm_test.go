package ui

import (
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/core/agentterminal"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi/fake"
)

// runCmds runs a command's non-blocking parts and returns their messages. The
// frame and input waits are skipped by name: they block on a channel, and one
// left running would swallow the next chunk a test feeds in.
func runCmds(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	if name := runtime.FuncForPC(reflect.ValueOf(cmd).Pointer()).Name(); strings.Contains(name, "tickProc") || strings.Contains(name, "tickPRs") || strings.Contains(name, "waitInputCmd") || strings.Contains(name, "waitFramesCmd") || strings.Contains(name, "assistTickCmd") || strings.Contains(name, "gardenLocalTickCmd") || strings.Contains(name, "waitAgentsCmd") {
		return nil
	}
	switch msg := cmd().(type) {
	case nil:
		return nil
	case tea.BatchMsg:
		var out []tea.Msg
		for _, c := range msg {
			out = append(out, runCmds(c)...)
		}
		return out
	default:
		return []tea.Msg{msg}
	}
}

// run feeds a command's messages back through Update until nothing is left.
func (e *agentsEnv) run(t *testing.T, cmd tea.Cmd) {
	t.Helper()
	for i := 0; i < 20; i++ {
		msgs := runCmds(cmd)
		if len(msgs) == 0 {
			return
		}
		var next []tea.Cmd
		for _, msg := range msgs {
			nm, c := e.m.Update(msg)
			e.m = nm.(Model)
			next = append(next, c)
		}
		cmd = tea.Batch(next...)
	}
}

// pump moves what the terminal queued for the agent into Update, as the wait
// command would, and runs the writes that follow.
func (e *agentsEnv) pump(t *testing.T) {
	t.Helper()
	for {
		term := e.m.agents.view.term
		select {
		case d := <-term.Input():
			nm, cmd := e.m.Update(agentInputMsg{term: term, data: d})
			e.m, e.lastCmd = nm.(Model), cmd
			e.run(t, cmd)
		case <-time.After(40 * time.Millisecond):
			return
		}
	}
}

func (e *agentsEnv) typeKeys(t *testing.T, keys ...tea.KeyMsg) {
	t.Helper()
	for _, k := range keys {
		e.send(t, k)
		e.run(t, e.lastCmd)
	}
	e.pump(t)
}

func runes(s string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)} }

var ctrlBracket = tea.KeyMsg{Type: tea.KeyCtrlCloseBracket}

// terminalEnv is liveEnv with the view in terminal mode as the writer.
func terminalEnv(t *testing.T) (*agentsEnv, *fake.Stream) {
	t.Helper()
	e := liveEnv(t)
	e.attached(t)
	e.frames(t, out("hello"), status("running"))
	e.send(t, tea.KeyMsg{Type: tea.KeyEnter})
	st := e.attached(t)
	e.run(t, e.lastCmd)
	e.frames(t, status("running"))
	if e.m.agents.view.state != attachControlling {
		t.Fatalf("state = %v, want controlling", e.m.agents.view.state)
	}
	return e, st
}

func inputsOf(st *fake.Stream) []string {
	var got []string
	for _, p := range st.Inputs() {
		got = append(got, string(p))
	}
	return got
}

func TestTerminalEnterReattachesAtOffsetAsWriter(t *testing.T) {
	e := liveEnv(t)
	ro := e.attached(t)
	e.frames(t, agentterminal.Frame{Type: "output", Offset: 10, Data: []byte("hello")}, status("running"))
	e.send(t, tea.KeyMsg{Type: tea.KeyEnter})
	st := e.attached(t)
	e.run(t, e.lastCmd)

	calls := e.fb.Sessions.Attaches()
	if len(calls) != 2 {
		t.Fatalf("attaches = %+v", calls)
	}
	want := agentsapi.AttachOptions{Offset: 15, Generation: "fake-gen"}
	if calls[1].Opts != want {
		t.Errorf("control attach = %+v, want %+v", calls[1].Opts, want)
	}
	if !ro.Closed() {
		t.Error("the read-only stream stays open next to the writer")
	}
	cols, rows := e.m.agentAreaSize()
	if got, want := st.Resizes(), [][2]int{{cols, rows - 1}, {cols, rows}}; !reflect.DeepEqual(got, want) {
		t.Errorf("resizes = %v, want the nudge %v", got, want)
	}
	if e.m.agents.view.state != attachControlling || !e.m.agentTerminalMode() {
		t.Errorf("state = %v", e.m.agents.view.state)
	}
	if !strings.Contains(ansi.Strip(e.m.statusBar()), "FOCUSED · Input goes to agent · Ctrl+] unfocus") {
		t.Errorf("status bar = %q", ansi.Strip(e.m.statusBar()))
	}
}

func TestTerminalKeysReachTheAgentAndNothingElse(t *testing.T) {
	e, st := terminalEnv(t)
	e.typeKeys(t,
		runes("q"), tea.KeyMsg{Type: tea.KeyCtrlC}, tea.KeyMsg{Type: tea.KeyEsc},
		tea.KeyMsg{Type: tea.KeyTab}, tea.KeyMsg{Type: tea.KeyUp}, runes("?"), runes("g"),
	)
	want := []string{"q", "\x03", "\x1b", "\t", "\x1b[A", "?", "g"}
	if got := inputsOf(st); !reflect.DeepEqual(got, want) {
		t.Errorf("inputs = %q, want %q", got, want)
	}
	if e.m.rightTab != tabAgents || e.m.modal != nil || e.m.prefs != nil {
		t.Error("a key leaked to the TUI")
	}
	for _, msg := range runCmds(e.lastCmd) {
		if _, quit := msg.(tea.QuitMsg); quit {
			t.Error("quit")
		}
	}
}

func TestTerminalInputKeepsOrder(t *testing.T) {
	e, st := terminalEnv(t)
	var keys []tea.KeyMsg
	var want []string
	for _, r := range "abcdefghijklmnopqrstuvwxyz" {
		keys = append(keys, runes(string(r)))
		want = append(want, string(r))
	}
	e.typeKeys(t, keys...)
	if got := inputsOf(st); !reflect.DeepEqual(got, want) {
		t.Errorf("inputs = %q", got)
	}
}

func TestNavigationModeSendsNothing(t *testing.T) {
	e := liveEnv(t)
	st := e.attached(t)
	e.frames(t, status("running"))
	for _, k := range []tea.KeyMsg{runes("x"), {Type: tea.KeyUp}, runes("j")} {
		e.send(t, k)
	}
	e.pump(t)
	if len(st.Inputs()) != 0 || e.m.agentTerminalMode() {
		t.Errorf("inputs = %q in navigation mode", inputsOf(st))
	}
}

func TestTerminalObservingOtherSwallowsKeysWithOneHint(t *testing.T) {
	e := liveEnv(t)
	e.attached(t)
	e.frames(t, status("running"))
	other, err := e.fb.Sessions.Attach(t.Context(), "a", agentsapi.AttachOptions{}) // someone else writes
	if err != nil || !other.Writer() {
		t.Fatalf("setup: %v", err)
	}
	e.send(t, tea.WindowSizeMsg{Width: 140, Height: 30})
	e.send(t, tea.KeyMsg{Type: tea.KeyEnter})
	st := e.attached(t)
	e.frames(t, status("running"))
	v := e.m.agents.view
	if v.state != attachObservingOther || st.Writer() {
		t.Fatalf("state = %v", v.state)
	}
	view := ansi.Strip(e.m.View())
	for _, want := range []string{"Controlled by another client", "Ctrl+] Unfocus, then Enter", "Ctrl+] Unfocus"} {
		if !strings.Contains(view, want) {
			t.Errorf("View lacks %q:\n%s", want, view)
		}
	}
	if bar := ansi.Strip(e.m.statusBar()); !strings.Contains(bar, "READ-ONLY · Ctrl+] unfocus") {
		t.Errorf("status bar = %q", bar)
	}
	e.typeKeys(t, runes("q"))
	if e.m.status != observingOtherNote {
		t.Errorf("status = %q, want the one-time note", e.m.status)
	}
	e.m.status = ""
	e.typeKeys(t, runes("q"), tea.KeyMsg{Type: tea.KeyCtrlC})
	if e.m.status != "" || len(st.Inputs()) != 0 || len(other.(*fake.Stream).Inputs()) != 0 {
		t.Errorf("status %q, inputs %q: the hint is shown once and nothing is sent", e.m.status, inputsOf(st))
	}
}

func TestTerminalEscapeKeepsStreamAndReturnsKeyboard(t *testing.T) {
	e, st := terminalEnv(t)
	attaches := len(e.fb.Sessions.Attaches())
	e.typeKeys(t, ctrlBracket, runes("z"), ctrlBracket)
	if e.m.agentTerminalMode() || e.m.modal != nil || e.m.agents.overlay != nil {
		t.Fatal("escape should leave focus immediately")
	}
	if st.Closed() || len(e.fb.Sessions.Attaches()) != attaches || len(st.Inputs()) != 0 {
		t.Fatal("leaving focus must keep stream and swallow no further agent input")
	}
	e.frames(t, out("still running"))
	if !strings.Contains(ansi.Strip(e.m.agents.view.term.View()), "still running") {
		t.Fatal("output stopped")
	}
	e.typeKeys(t, tea.KeyMsg{Type: tea.KeyEnter}, runes("hello"))
	if !e.m.agentTerminalMode() || !reflect.DeepEqual(inputsOf(st), []string{"hello"}) {
		t.Fatalf("enter did not restore agent input: %q", inputsOf(st))
	}
}

func TestTerminalPrefixTakeControl(t *testing.T) {
	e := liveEnv(t)
	e.attached(t)
	e.frames(t, status("running"))
	other, _ := e.fb.Sessions.Attach(t.Context(), "a", agentsapi.AttachOptions{})
	e.send(t, tea.KeyMsg{Type: tea.KeyEnter})
	st := e.attached(t)
	e.frames(t, status("running"))
	if e.m.agents.view.state != attachObservingOther {
		t.Fatal("setup")
	}
	e.send(t, ctrlBracket)
	if e.m.agentTerminalMode() {
		t.Fatal("Ctrl+] must leave focus")
	}
	e.send(t, tea.KeyMsg{Type: tea.KeyEnter})
	e.run(t, e.lastCmd)
	if st.Takes() != 1 {
		t.Fatalf("takes = %d", st.Takes())
	}
	// The fake grants the role at once, as a writer frame would announce it.
	other.(*fake.Stream).SetWriter(false)
	yes := true
	e.frames(t, agentterminal.Frame{Type: "writer", Writer: &yes})
	e.run(t, e.lastCmd)
	if e.m.agents.view.state != attachControlling {
		t.Fatalf("state = %v after taking", e.m.agents.view.state)
	}
	cols, rows := e.m.agentAreaSize()
	if got, want := st.Resizes(), [][2]int{{cols, rows - 1}, {cols, rows}}; !reflect.DeepEqual(got, want) {
		t.Errorf("resizes = %v, want the nudge %v", got, want)
	}
	if strings.Contains(ansi.Strip(e.m.View()), "Controlled by another") {
		t.Error("the banner outlived the takeover")
	}
}

func TestTerminalWriterLost(t *testing.T) {
	e, st := terminalEnv(t)
	st.SetWriter(false)
	no := false
	e.frames(t, agentterminal.Frame{Type: "writer", Writer: &no})
	if e.m.agents.view.state != attachObservingOther {
		t.Fatalf("state = %v", e.m.agents.view.state)
	}
	view := ansi.Strip(e.m.View())
	if !strings.Contains(view, "Control taken by another client  ·  Ctrl+] then Enter Take back") {
		t.Errorf("View lacks the lost-control hint:\n%s", view)
	}
	e.typeKeys(t, runes("q"))
	if len(st.Inputs()) != 0 {
		t.Error("input after losing control")
	}
}

func TestTerminalReplayRepliesAndKeysNeverReachTheAgent(t *testing.T) {
	e := liveEnv(t)
	e.attached(t)
	e.frames(t, status("running"))
	e.send(t, tea.KeyMsg{Type: tea.KeyEnter})
	st := e.attached(t)
	// History that asks questions: the answers are for requests long gone.
	e.frames(t, out("\x1b[6n\x1b[c\x1b[?u"))
	e.typeKeys(t, runes("x"))
	e.frames(t, status("running"))
	e.pump(t)
	if got := inputsOf(st); len(got) != 0 {
		t.Errorf("inputs during replay = %q", got)
	}
	// Live questions are answered.
	e.frames(t, out("\x1b[6n"))
	e.pump(t)
	if got := inputsOf(st); len(got) == 0 {
		t.Error("a live cursor query must be answered")
	}
}

func TestTerminalResizeIsDebounced(t *testing.T) {
	e, st := terminalEnv(t)
	base := len(st.Resizes())
	var gens []uint64
	for _, size := range [][2]int{{110, 34}, {120, 36}, {130, 40}} {
		e.send(t, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		gens = append(gens, e.m.agents.view.resizeGen)
	}
	if len(st.Resizes()) != base {
		t.Fatal("resized before the debounce elapsed")
	}
	token := e.m.agents.view.token
	for _, g := range gens {
		e.send(t, agentResizeTickMsg{token: token, gen: g})
		e.run(t, e.lastCmd)
	}
	cols, rows := e.m.agentAreaSize()
	got := st.Resizes()[base:]
	if len(got) != 1 || got[0] != [2]int{cols, rows} {
		t.Errorf("resizes after settling = %v, want one to %dx%d", got, cols, rows)
	}
	e.send(t, agentResizeTickMsg{token: token, gen: gens[len(gens)-1]})
	e.run(t, e.lastCmd)
	if len(st.Resizes()) != base+1 {
		t.Error("an unchanged size must not resize again")
	}
}

func TestTerminalEscapeDoesNotShowControlMenu(t *testing.T) {
	for _, size := range [][2]int{{120, 40}, {80, 24}, {50, 24}} {
		e, _ := terminalEnv(t)
		e.send(t, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		e.send(t, ctrlBracket)
		view := ansi.Strip(e.m.View())
		if strings.Contains(view, "BONSAI CONTROL") || e.m.agentTerminalMode() {
			t.Fatal("escape opened a menu or kept focus")
		}
		if len(strings.Split(view, "\n")) > size[1] {
			t.Fatal("view exceeds screen height")
		}
		for _, line := range strings.Split(view, "\n") {
			if ansi.StringWidth(line) > size[0] {
				t.Fatal("view exceeds screen width")
			}
		}
	}
}

func TestTerminalLayoutFitsNarrowAndWide(t *testing.T) {
	for _, size := range [][2]int{{120, 40}, {80, 24}, {50, 24}} {
		e, _ := terminalEnv(t)
		e.send(t, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		view := ansi.Strip(e.m.View())
		if !strings.Contains(view, "FOCUSED") || !strings.Contains(view, "Ctrl+] Unfocus") {
			if size[0] >= 80 {
				t.Errorf("%v: View lacks the hint row or usage hint:\n%s", size, view)
			}
		}
		lines := strings.Split(view, "\n")
		if len(lines) > size[1] {
			t.Errorf("%v: %d rows", size, len(lines))
		}
		for _, l := range lines {
			if ansi.StringWidth(l) > size[0] {
				t.Errorf("%v: line wider: %q", size, l)
			}
		}
	}
}

func TestTerminalPrefixAgentsLeavesModeKeepsStream(t *testing.T) {
	e, st := terminalEnv(t)
	attaches := len(e.fb.Sessions.Attaches())
	e.typeKeys(t, ctrlBracket, runes("a"))
	if e.m.agents.overlay == nil || e.m.agents.overlay.sessions == nil || e.m.agentTerminalMode() {
		t.Fatal("ctrl+] a opens the picker and leaves terminal mode")
	}
	if st.Closed() || e.m.agents.view.stream != agentsapi.Stream(st) {
		t.Error("the stream must stay attached under the overlay")
	}
	e.frames(t, out("still running"))
	if !strings.Contains(ansi.Strip(e.m.agents.view.term.View()), "still running") {
		t.Error("output stops rendering under the overlay")
	}
	e.send(t, tea.KeyMsg{Type: tea.KeyEsc})
	e.run(t, e.lastCmd)
	e.send(t, tea.KeyMsg{Type: tea.KeyEnter})
	e.run(t, e.lastCmd)
	if !e.m.agentTerminalMode() || len(e.fb.Sessions.Attaches()) != attaches {
		t.Error("enter returns to the kept writer stream without a re-attach")
	}
	cols, rows := e.m.agentAreaSize()
	if got := st.Resizes(); len(got) < 4 || got[len(got)-1] != [2]int{cols, rows} {
		t.Errorf("re-entering nudges again: %v", got)
	}
}

func TestTerminalPrefixOverlaysLeaveModeKeepStream(t *testing.T) {
	for _, k := range []string{"u", "m", "s"} {
		e, st := terminalEnv(t)
		e.typeKeys(t, ctrlBracket, runes(k))
		if e.m.agentTerminalMode() {
			t.Errorf("%s: terminal mode must end under the overlay", k)
		}
		switch k {
		case "u":
			if e.m.agents.overlay == nil || e.m.agents.overlay.quick == nil {
				t.Errorf("%s: no usage overlay", k)
			}
		case "m":
			if e.m.agents.overlay == nil || e.m.agents.overlay.accounts == nil {
				t.Errorf("%s: no accounts overlay", k)
			}
		case "s":
			if e.m.modal == nil || e.m.pendingConfirm == nil {
				t.Errorf("%s: no stop confirmation", k)
			}
		}
		if st.Closed() || e.m.agents.view.stream != agentsapi.Stream(st) {
			t.Errorf("%s: the stream must stay attached under the overlay", k)
		}
		e.frames(t, out("still going"))
		if !strings.Contains(ansi.Strip(e.m.agents.view.term.View()), "still going") {
			t.Errorf("%s: output stops rendering under the overlay", k)
		}
	}
}

func TestTerminalModeEndsWhenTheAgentExits(t *testing.T) {
	e, _ := terminalEnv(t)
	e.frames(t, status("exited"))
	if e.m.agentTerminalMode() {
		t.Error("an ended agent cannot be typed into; keys must return to the TUI")
	}
}

func TestAgentAttachBindingIsRebindable(t *testing.T) {
	b := binding("agent_attach", map[string]string{"agent_attach": "o"})
	if !key.Matches(runes("o"), b) || key.Matches(tea.KeyMsg{Type: tea.KeyEnter}, b) {
		t.Error("agent_attach must follow the user's override")
	}
}
