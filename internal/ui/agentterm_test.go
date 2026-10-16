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
	if name := runtime.FuncForPC(reflect.ValueOf(cmd).Pointer()).Name(); strings.Contains(name, "waitInputCmd") || strings.Contains(name, "waitFramesCmd") {
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
	if !strings.Contains(ansi.Strip(e.m.statusBar()), "Ctrl+] Bonsai command prefix") {
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
	for _, want := range []string{"Controlled by another client", "Ctrl+] t Take control", "Ctrl+] d Detach"} {
		if !strings.Contains(view, want) {
			t.Errorf("View lacks %q:\n%s", want, view)
		}
	}
	if bar := ansi.Strip(e.m.statusBar()); !strings.Contains(bar, "Ctrl+] t take control") {
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

func TestTerminalPrefixDetachReattachesReadOnlyAtOffset(t *testing.T) {
	e, st := terminalEnv(t)
	e.frames(t, agentterminal.Frame{Type: "output", Offset: 3, Data: []byte("more")})
	e.send(t, ctrlBracket)
	e.send(t, runes("d"))
	e.run(t, e.lastCmd)
	if !st.Closed() {
		t.Error("the writer stream stays open after detach")
	}
	ro := e.attached(t)
	calls := e.fb.Sessions.Attaches()
	last := calls[len(calls)-1].Opts
	if !last.ReadOnly || last.Offset != 7 || last.Generation != "fake-gen" {
		t.Errorf("detach attach = %+v, want read-only at offset 7", last)
	}
	if e.m.agentTerminalMode() || ro.Writer() {
		t.Error("detach must leave terminal mode and the write role")
	}
	if !strings.Contains(ansi.Strip(e.m.View()), "hello") {
		t.Error("the screen must stay while re-attaching")
	}
}

func TestTerminalPrefixCannotBeBypassed(t *testing.T) {
	e, st := terminalEnv(t)
	for _, k := range []tea.KeyMsg{runes("x"), runes("q"), {Type: tea.KeyCtrlC}, {Type: tea.KeyTab}, {Type: tea.KeyEnter}} {
		e.typeKeys(t, ctrlBracket, k)
		if e.m.agents.view.prefix {
			t.Errorf("strip still open after %v", k)
		}
	}
	if len(st.Inputs()) != 0 || e.m.rightTab != tabAgents || e.m.modal != nil {
		t.Errorf("inputs %q: a key after the prefix must be swallowed", inputsOf(st))
	}
	e.typeKeys(t, runes("q"))
	if got := inputsOf(st); !reflect.DeepEqual(got, []string{"q"}) {
		t.Errorf("after the strip closes keys flow again: %q", got)
	}
}

func TestTerminalPrefixEscReturnsToAgent(t *testing.T) {
	e, st := terminalEnv(t)
	e.typeKeys(t, ctrlBracket, tea.KeyMsg{Type: tea.KeyEsc})
	if e.m.agents.view.prefix || !e.m.agentTerminalMode() || len(st.Inputs()) != 0 {
		t.Error("esc after the prefix only closes the strip")
	}
}

func TestTerminalPrefixPrefixSendsLiteral(t *testing.T) {
	e, st := terminalEnv(t)
	e.typeKeys(t, ctrlBracket, ctrlBracket)
	if got := st.Inputs(); len(got) != 1 || string(got[0]) != "\x1d" {
		t.Errorf("inputs = %q, want 0x1d", got)
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
	if !strings.Contains(ansi.Strip(e.m.View()), "[t] Take control") {
		t.Error("the strip offers take control to an observer")
	}
	e.send(t, runes("t"))
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
	if !strings.Contains(view, "Control taken by another client  ·  Ctrl+] t Take back") {
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

func TestTerminalControlStripOverlaysWithoutResize(t *testing.T) {
	for _, size := range [][2]int{{120, 40}, {80, 24}, {50, 24}} {
		e, st := terminalEnv(t)
		e.send(t, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		before := len(st.Resizes())
		lines := strings.Split(ansi.Strip(e.m.View()), "\n")
		e.send(t, ctrlBracket)
		view := ansi.Strip(e.m.View())
		wide := size[0] >= 80
		for _, want := range []string{"BONSAI CONTROL", "[d] Detach", "[Esc] Return to agent", "[s] Stop"} {
			if wide && !strings.Contains(view, want) {
				t.Errorf("%v: View lacks %q:\n%s", size, want, view)
			}
		}
		if !strings.Contains(view, "BONSAI CONTROL") || (wide && !strings.Contains(view, "[Esc]")) {
			t.Errorf("%v: strip missing:\n%s", size, view)
		}
		if strings.Contains(view, "[t]") {
			t.Errorf("%v: the writer is offered take control", size)
		}
		after := strings.Split(view, "\n")
		if len(after) != len(lines) || len(after) > size[1] {
			t.Errorf("%v: rows %d -> %d", size, len(lines), len(after))
		}
		for _, l := range after {
			if ansi.StringWidth(l) > size[0] {
				t.Errorf("%v: line wider than the screen: %q", size, l)
			}
		}
		if len(st.Resizes()) != before {
			t.Errorf("%v: the strip resized the PTY", size)
		}
	}
}

func TestTerminalLayoutFitsNarrowAndWide(t *testing.T) {
	for _, size := range [][2]int{{120, 40}, {80, 24}, {50, 24}} {
		e, _ := terminalEnv(t)
		e.send(t, tea.WindowSizeMsg{Width: size[0], Height: size[1]})
		view := ansi.Strip(e.m.View())
		if !strings.Contains(view, "Ctrl+]  →  Bonsai command prefix") || !strings.Contains(view, "Ctrl+] u: Usage") {
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
	if e.m.modal == nil || e.m.agentTerminalMode() {
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

func TestTerminalPrefixComingSoonKeepsMode(t *testing.T) {
	for _, k := range []string{"u", "m", "s"} {
		e, _ := terminalEnv(t)
		e.typeKeys(t, ctrlBracket, runes(k))
		if e.m.status != "agents: coming soon" || !e.m.agentTerminalMode() {
			t.Errorf("%s: status %q mode %v", k, e.m.status, e.m.agentTerminalMode())
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
