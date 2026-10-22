package ui

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/core/agentterminal"
	"github.com/Tiago-0liveira/bonsai/internal/core/config"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi/fake"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/modals"
)

// pgTermEnv opens the Playground on three running sessions with the debounce shortened, so a settled selection
// attaches inside the drain of the key that moved it.
func pgTermEnv(t *testing.T, w, h int) *pgEnv {
	t.Helper()
	e := newPgEnv(t, w, h)
	e.m.pg.debounce = time.Millisecond
	e.fb.Sessions.Add(e.sess("s1", "claude", "work", "running", 1), e.sess("s2", "codex", "personal", "running", 2), e.sess("s3", "claude", "work", "running", 3))
	e.open(t)
	return e
}

// stream is the newest fake stream the backend made for a session.
func (e *pgEnv) stream(t *testing.T, id string) *fake.Stream {
	t.Helper()
	ss := e.fb.Sessions.Streams(id)
	if len(ss) == 0 {
		t.Fatalf("no stream for %s; attaches: %+v", id, e.fb.Sessions.Attaches())
	}
	return ss[len(ss)-1]
}

func (e *pgEnv) frames(t *testing.T, st *fake.Stream, fs ...agentterminal.Frame) {
	t.Helper()
	e.m = plannerFrames(t, e.m, st, fs...)
}

// pump moves what the focused terminal queued for the agent into Update, as the wait command would.
func (e *pgEnv) pump(t *testing.T) { e.m = plannerPump(t, e.m) }

// press sends terminal-style keys (named keys, runes, alt+digit, ctrl+x) without the drain's blocking waits.
func (e *pgEnv) press(t *testing.T, k tea.KeyMsg) tea.Cmd {
	t.Helper()
	var cmd tea.Cmd
	e.m, cmd = send(t, e.m, k)
	e.m = plannerRun(t, e.m, cmd)
	return cmd
}

// control enters Control on the selected session as its own writer, and ends the replay.
func (e *pgEnv) control(t *testing.T) *fake.Stream {
	t.Helper()
	e.key(t, "enter")
	st := e.stream(t, e.m.pg.sel)
	if !st.Writer() || !e.m.pgTerminalMode() {
		t.Fatalf("enter did not take control: writer=%v mode=%v\n%s", st.Writer(), e.m.pgTerminalMode(), view(e.m))
	}
	e.frames(t, st, status("running"))
	return st
}

func lastAttach(e *pgEnv) fake.AttachCall {
	calls := e.fb.Sessions.Attaches()
	return calls[len(calls)-1]
}

func TestPlaygroundSelectionObservesReadOnlyAndNeverTouchesThePTY(t *testing.T) {
	e := pgTermEnv(t, 120, 35)
	calls := e.fb.Sessions.Attaches()
	if len(calls) != 1 || calls[0].ID != e.m.pg.sel || !calls[0].Opts.ReadOnly || calls[0].Opts.Take {
		t.Fatalf("selection must attach one read-only observer: %+v", calls)
	}
	st := e.stream(t, calls[0].ID)
	e.frames(t, st, agentterminal.Frame{Type: "output", Data: []byte("hello"), Offset: 0}, status("running"))
	e.key(t, "v")
	if !e.m.pg.open || e.m.pgTerminalMode() {
		t.Fatal("v must open the terminal for observing only")
	}
	mustContain(t, "observe", view(e.m), "OBSERVE / read-only", "hello")
	// Window resizes, ticks, a list refresh and key presses are all observer-safe.
	var cmd tea.Cmd
	e.m, cmd = send(t, e.m, tea.WindowSizeMsg{Width: 100, Height: 30})
	if cmd != nil {
		e.m = pgDrain(t, e.m, cmd)
	}
	e.relist(t)
	e.press(t, runeKey("x"))
	if len(st.Inputs()) != 0 || len(st.Resizes()) != 0 || st.Takes() != 0 || st.Writer() {
		t.Fatalf("observer touched the PTY: inputs=%d resizes=%v takes=%d", len(st.Inputs()), st.Resizes(), st.Takes())
	}
}

func TestPlaygroundRapidSelectionIsDebounced(t *testing.T) {
	e := pgTermEnv(t, 120, 35)
	e.m.pg.debounce = time.Hour // ticks are delivered by hand
	e.key(t, "down")
	g1 := e.m.pg.gen
	e.key(t, "down")
	g2 := e.m.pg.gen
	if g1 == g2 || e.m.pg.sel != "s3" {
		t.Fatalf("gens %d/%d sel %s", g1, g2, e.m.pg.sel)
	}
	before := len(e.fb.Sessions.Attaches())
	e.m, _ = send(t, e.m, pgAttachTickMsg{gen: g1})
	if len(e.fb.Sessions.Attaches()) != before {
		t.Fatal("a stale tick dialed a session")
	}
	var cmd tea.Cmd
	e.m, cmd = send(t, e.m, pgAttachTickMsg{gen: g2})
	e.m = pgDrain(t, e.m, cmd)
	calls := e.fb.Sessions.Attaches()
	if len(calls) != before+1 || calls[before].ID != "s3" || !calls[before].Opts.ReadOnly {
		t.Fatalf("only the row the cursor rests on is observed: %+v", calls)
	}
	for _, c := range calls[1:] {
		if c.ID == "s2" {
			t.Fatal("a row scrolled past was dialed")
		}
	}
}

func TestPlaygroundSwitchingSessionsObservesNextAndNeverCarriesControl(t *testing.T) {
	e := pgTermEnv(t, 120, 35)
	first := e.m.pg.sel
	st := e.control(t)
	e.press(t, tea.KeyMsg{Type: tea.KeyCtrlCloseBracket})
	e.key(t, "esc", "down")
	if e.m.pg.sel == first {
		t.Fatal("selection did not move")
	}
	if !st.Closed() {
		t.Error("the previous view was not released")
	}
	c := lastAttach(e)
	if c.ID != e.m.pg.sel || !c.Opts.ReadOnly || c.Opts.Take {
		t.Fatalf("next session must be observed read-only: %+v", c)
	}
	if v := e.m.agents.view; v == nil || v.control || e.m.pg.open || e.m.pgTerminalMode() {
		t.Fatal("control carried over to the next session")
	}
}

func TestPlaygroundEnterTakesFreeWriterAndEntersControl(t *testing.T) {
	e := pgTermEnv(t, 120, 35)
	id := e.m.pg.sel
	st := e.control(t)
	c := lastAttach(e)
	if c.ID != id || c.Opts.ReadOnly || c.Opts.Take {
		t.Fatalf("control is a plain write attach, not a take: %+v", c)
	}
	if st.Takes() != 0 || len(e.m.pgTermHints()) == 0 {
		t.Fatal("a free session needs no take")
	}
	out := view(e.m)
	mustContain(t, "control", out, "CONTROL / keys go to agent", "FOCUS: Agent input", "Ctrl+]")
	mustNotContain(t, "control", out, "Ctrl+K commands", "? help")
	if len(st.Resizes()) == 0 {
		t.Error("the writer sizes the agent to its area")
	}
	w, rows := e.m.pgAreaSize()
	want := [2]int{w, rows}
	if r := st.Resizes(); r[len(r)-1] != want {
		t.Errorf("resize = %v, want the Playground area %v", r[len(r)-1], want)
	}
}

func TestPlaygroundTakeoverAsksFirst(t *testing.T) {
	e := pgTermEnv(t, 120, 35)
	id := e.m.pg.sel
	other, err := e.fb.Sessions.Attach(context.Background(), id, agentsapi.AttachOptions{}) // another client writes
	if err != nil || !other.Writer() {
		t.Fatalf("setup: %v", err)
	}
	e.key(t, "enter")
	st := e.stream(t, id)
	if e.m.modal == nil || e.m.modal.Kind() != modals.KindPlaygroundTakeover {
		t.Fatalf("no takeover confirmation:\n%s", view(e.m))
	}
	if st.Takes() != 0 || e.m.pgTerminalMode() {
		t.Fatal("control was taken before the user approved")
	}
	// Cancel changes nothing.
	e.m, _ = send(t, e.m, modals.CancelMsg{Kind: modals.KindPlaygroundTakeover})
	if e.m.modal != nil || st.Takes() != 0 || st.Writer() || e.m.agents.view.control {
		t.Fatal("cancel changed something")
	}
	mustContain(t, "read-only", view(e.m), "READ-ONLY, controlled by another client")
	// Enter asks again; confirming takes exactly once.
	e.key(t, "enter")
	if e.m.modal == nil {
		t.Fatal("enter did not ask again")
	}
	var cmd tea.Cmd
	e.m, cmd = send(t, e.m, modals.SubmitMsg{Kind: modals.KindPlaygroundTakeover, Value: "yes"})
	e.m = plannerRun(t, e.m, cmd)
	if st.Takes() != 1 || !st.Writer() || !e.m.pgTerminalMode() {
		t.Fatalf("takes=%d writer=%v mode=%v", st.Takes(), st.Writer(), e.m.pgTerminalMode())
	}
	_ = other // the fake's Take does not demote other streams; the real holder does
}

func TestPlaygroundRefreshesAndUsageNeverStealTheWriter(t *testing.T) {
	e := pgTermEnv(t, 120, 35)
	id := e.m.pg.sel
	if _, err := e.fb.Sessions.Attach(context.Background(), id, agentsapi.AttachOptions{}); err != nil {
		t.Fatal(err)
	}
	e.key(t, "down", "up") // selection moves away and back
	e.relist(t)
	e.m, _ = send(t, e.m, pgChangedMsg{ch: make(chan struct{})})
	for _, st := range e.fb.Sessions.Streams(id) {
		if st.Takes() != 0 || st.Writer() && len(st.Resizes()) > 0 {
			t.Fatal("navigation or a refresh took the writer")
		}
	}
	for _, c := range e.fb.Sessions.Attaches() {
		if c.Opts.Take {
			t.Fatalf("a take attach was issued: %+v", c)
		}
	}
}

func TestPlaygroundControlForwardsEveryKeyButCtrlBracket(t *testing.T) {
	e := pgTermEnv(t, 120, 35)
	st := e.control(t)
	keys := []tea.KeyMsg{runeKey("q"), {Type: tea.KeyEsc}, {Type: tea.KeyTab}, {Type: tea.KeyCtrlC}, {Type: tea.KeyCtrlK}, runeKey("?"), {Type: tea.KeyRunes, Runes: []rune("1"), Alt: true}}
	for _, k := range keys {
		var cmd tea.Cmd
		e.m, cmd = send(t, e.m, k)
		for _, msg := range runCmds(cmd) {
			if _, quit := msg.(tea.QuitMsg); quit {
				t.Fatalf("%v quit the app", k)
			}
		}
		e.pump(t)
		if e.m.workspace != wsPlayground || e.m.modal != nil || !e.m.pgTerminalMode() {
			t.Fatalf("%v left Control: ws=%v modal=%v", k, e.m.workspace, e.m.modal != nil)
		}
	}
	got := st.Inputs()
	if len(got) != len(keys) {
		t.Fatalf("agent got %d of %d keys: %q", len(got), len(keys), got)
	}
	if string(got[0]) != "q" || string(got[3]) != "\x03" || string(got[4]) != "\v" || string(got[5]) != "?" {
		t.Errorf("keys arrived mangled: %q", got)
	}
}

func TestPlaygroundCtrlBracketKeepsOutputAttachedAndStopsInput(t *testing.T) {
	e := pgTermEnv(t, 120, 35)
	st := e.control(t)
	e.press(t, tea.KeyMsg{Type: tea.KeyCtrlCloseBracket})
	v := e.m.agents.view
	if v == nil || v.control || e.m.pgTerminalMode() || st.Closed() {
		t.Fatal("Ctrl+] must only return the keyboard")
	}
	before := len(st.Inputs())
	e.press(t, runeKey("x"))
	e.pump(t)
	if len(st.Inputs()) != before {
		t.Fatal("a key reached the agent after Ctrl+]")
	}
	e.frames(t, st, agentterminal.Frame{Type: "output", Data: []byte("still live"), Offset: 0})
	mustContain(t, "observe", view(e.m), "still live", "OBSERVE / input stays with Bonsai")
	if e.m.currentShellFocus() != shellBody {
		t.Errorf("focus = %v", e.m.currentShellFocus())
	}
	e.key(t, "esc") // back to the list; still attached
	if e.m.pg.open || e.m.agents.view == nil {
		t.Error("esc must only close the terminal body")
	}
}

func TestPlaygroundOwnershipLossDemotesAndStopsInput(t *testing.T) {
	e := pgTermEnv(t, 120, 35)
	st := e.control(t)
	st.SetWriter(false)
	e.frames(t, st, agentterminal.Frame{Type: "output", Data: []byte("x"), Offset: 0})
	v := e.m.agents.view
	if v.state != attachObservingOther || !v.lost {
		t.Fatalf("state = %v lost=%v", v.state, v.lost)
	}
	before := len(st.Inputs())
	e.press(t, runeKey("a"))
	e.pump(t)
	if len(st.Inputs()) != before {
		t.Fatal("input went out after control was lost")
	}
	mustContain(t, "lost", view(e.m), hintControlLost, "READ-ONLY, controlled by another client")
	if h := e.m.shellHints(); len(h) != 1 || h[0].key != "Ctrl+]" {
		t.Errorf("hints = %+v", h)
	}
}

func TestPlaygroundReplayGuardAndLateTokens(t *testing.T) {
	e := pgTermEnv(t, 120, 35)
	e.key(t, "enter")
	st := e.stream(t, e.m.pg.sel)
	e.press(t, runeKey("a")) // still replaying: the holder's history is not over
	e.pump(t)
	if len(st.Inputs()) != 0 {
		t.Fatal("input went out during the replay")
	}
	e.frames(t, st, status("running"))
	old := e.m.agents.view.token
	e.m, _ = send(t, e.m, agentFramesMsg{token: old - 1, frames: []agentterminal.Frame{{Type: "output", Data: []byte("ghost"), Offset: 0}}})
	if strings.Contains(view(e.m), "ghost") {
		t.Fatal("a frame of an older stream was applied")
	}
	late := fake.NewStream(80, 24)
	var cmd tea.Cmd
	e.m, cmd = send(t, e.m, agentAttachedMsg{token: old - 1, stream: late})
	e.m = plannerRun(t, e.m, cmd)
	if !late.Closed() || e.m.agents.view.stream == agentsapi.Stream(late) {
		t.Fatal("a late attach result was kept")
	}
}

func TestPlaygroundReconnectsAfterClose(t *testing.T) {
	e := pgTermEnv(t, 120, 35)
	st := e.control(t)
	e.m, _ = send(t, e.m, agentFramesMsg{token: e.m.agents.view.token, closed: true})
	v := e.m.agents.view
	if !v.retry || v.control || e.m.pgTerminalMode() {
		t.Fatalf("a closed stream must stop control and offer a retry: %+v", v)
	}
	mustContain(t, "closed", view(e.m), "enter: retry")
	if h := e.m.shellHints(); h[0].key != "enter" || h[0].desc != "retry" {
		t.Errorf("hints = %+v", h)
	}
	n := len(e.fb.Sessions.Attaches())
	e.key(t, "enter")
	if len(e.fb.Sessions.Attaches()) != n+1 || lastAttach(e).Opts.ReadOnly || st == e.stream(t, e.m.pg.sel) {
		t.Fatal("enter did not reconnect for control")
	}
}

func TestPlaygroundEndedSessionsAcceptNoInputAndSayRetainedOutputIsBounded(t *testing.T) {
	e := newPgEnv(t, 120, 35)
	e.m.pg.debounce = time.Millisecond
	e.fb.Sessions.Add(e.sess("done", "claude", "work", "exited", 1))
	e.open(t)
	e.key(t, "a") // include ended sessions
	if e.m.pg.sel != "done" {
		t.Fatalf("sel = %q", e.m.pg.sel)
	}
	st := e.stream(t, "done")
	e.key(t, "enter")
	if e.m.pg.open || !strings.Contains(e.m.status, "ended") {
		t.Fatalf("enter on an ended session: open=%v status=%q", e.m.pg.open, e.m.status)
	}
	for _, h := range e.m.shellHints() {
		if h.key == "enter" {
			t.Errorf("an ended session advertises %+v", h)
		}
	}
	e.key(t, "v")
	exit := 0
	e.frames(t, st, agentterminal.Frame{Type: "output", Data: []byte("last words"), Offset: 0}, agentterminal.Frame{Type: "status", Status: &agentterminal.Summary{State: "exited", ExitCode: &exit}})
	out := view(e.m)
	mustContain(t, "ended", out, "ENDED / read-only, no input", "last words", "retained output is bounded")
	mustNotContain(t, "ended", strings.ToLower(out), "transcript", "complete output", "full output")
	e.press(t, runeKey("x"))
	e.pump(t)
	e.key(t, "enter")
	if e.m.pgTerminalMode() || len(st.Inputs()) != 0 {
		t.Fatal("an ended session took input")
	}
}

func TestPlaygroundVersionMismatchAndUnsupportedAreClear(t *testing.T) {
	e := newPgEnv(t, 120, 35)
	e.m.pg.debounce = time.Millisecond
	e.fb.Sessions.AttachErr = agentsapi.ErrVersion
	e.fb.Sessions.Add(e.sess("s1", "claude", "work", "running", 1))
	e.open(t)
	e.key(t, "enter")
	mustContain(t, "version", view(e.m), "Started by another bonsai version")

	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	st, _ := config.LoadState()
	m := sized(t, New(t.TempDir(), &config.Config{}, st, WithLayout(LayoutRedesign)), 120, 35)
	m, _ = send(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2"), Alt: true})
	m, _ = send(t, m, keyOf("enter"))
	m, cmd := send(t, m, keyOf("enter"))
	if cmd != nil && len(runCmds(cmd)) != 0 {
		t.Error("an unsupported host dialed something")
	}
	mustContain(t, "unsupported", view(m), "not available on this host")
}

func TestPlaygroundResizeInControlDebouncesOnceAndObserveSendsNone(t *testing.T) {
	e := pgTermEnv(t, 120, 35)
	st := e.control(t)
	base := len(st.Resizes())
	var cmds []tea.Cmd
	for _, sz := range [][2]int{{110, 33}, {100, 30}} {
		var cmd tea.Cmd
		e.m, cmd = send(t, e.m, tea.WindowSizeMsg{Width: sz[0], Height: sz[1]})
		cmds = append(cmds, cmd)
	}
	e.m = pgDrain(t, e.m, tea.Batch(cmds...))
	r := st.Resizes()
	if len(r) != base+1 || r[len(r)-1] != ([2]int{100, 30 - shellHeaderRows - shellFooterRows - settingsSubmenuRows - pgTermHeadRows}) {
		t.Fatalf("resizes after the drag = %v (base %d)", r, base)
	}
	// Observe: the same drag sends none.
	e.press(t, tea.KeyMsg{Type: tea.KeyCtrlCloseBracket})
	n := len(st.Resizes())
	e.m, _ = send(t, e.m, tea.WindowSizeMsg{Width: 90, Height: 28})
	if cmd := e.m.agentResizeDebounce(); cmd != nil || len(st.Resizes()) != n {
		t.Fatal("observing resized the PTY")
	}
}

func TestPlaygroundOwnsTheOneAttachmentWithGarden(t *testing.T) {
	e := pgTermEnv(t, 120, 35)
	st := e.stream(t, e.m.pg.sel)
	var cmd tea.Cmd
	e.m, cmd = e.m.selectWorkspace(wsGarden)
	e.m = pgDrain(t, e.m, cmd)
	if e.m.agents.view != nil || e.m.pg.own || !st.Closed() {
		t.Fatal("leaving the Playground kept its attachment")
	}
	// A Garden-owned attachment is released on entering the Playground.
	garden := fake.NewStream(80, 24)
	e.m.agents.view = &agentView{sessionID: "g", stream: garden}
	e.m, cmd = e.m.selectWorkspace(wsPlayground)
	e.m = pgDrain(t, e.m, cmd)
	if !garden.Closed() {
		t.Fatal("the Garden attachment was kept")
	}
	if v := e.m.agents.view; v != nil && v.sessionID == "g" {
		t.Fatal("two workspaces own the view")
	}
}

func TestPlaygroundRemovedSessionReleasesAndObservesTheNeighbor(t *testing.T) {
	e := pgTermEnv(t, 120, 35)
	gone := e.m.pg.sel
	st := e.stream(t, gone)
	s := e.sess(gone, "claude", "work", "running", 1)
	s.ProjectID, s.WorkDir = "someone-else", e.other
	e.fb.Sessions.Set(s)
	e.relist(t)
	if !st.Closed() || e.m.pg.sel == gone {
		t.Fatalf("closed=%v sel=%q", st.Closed(), e.m.pg.sel)
	}
	if c := lastAttach(e); c.ID != e.m.pg.sel || !c.Opts.ReadOnly {
		t.Fatalf("the new selection is not observed read-only: %+v", c)
	}
}

func TestPlaygroundTerminalRenderBounds(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {120, 35}, {160, 48}} {
		w, h := size[0], size[1]
		t.Run(fmt.Sprintf("%dx%d", w, h), func(t *testing.T) {
			e := pgTermEnv(t, w, h)
			check := func(label string) {
				t.Helper()
				lines := strings.Split(e.m.View(), "\n")
				if len(lines) != h {
					t.Errorf("%s: %d lines, want %d", label, len(lines), h)
				}
				for i, l := range lines {
					if got := ansi.StringWidth(l); got > w {
						t.Errorf("%s: line %d is %d cells > %d: %q", label, i, got, w, ansi.Strip(l))
					}
				}
			}
			check("list")
			e.key(t, "v")
			st := e.stream(t, e.m.pg.sel)
			long := strings.Repeat("0123456789 界 ", 30)
			rows := ""
			for i := 0; i < h; i++ {
				rows += long + "\r\n"
			}
			e.frames(t, st, agentterminal.Frame{Type: "output", Data: []byte(rows), Offset: 0}, status("running"))
			check("observe")
			mustContain(t, "observe", view(e.m), "OBSERVE / read-only")
			e.key(t, "enter")
			st = e.stream(t, e.m.pg.sel)
			e.frames(t, st, agentterminal.Frame{Type: "output", Data: []byte(rows), Offset: 0}, status("running"))
			check("control")
			mustContain(t, "control", view(e.m), "CONTROL / keys go to agent", "FOCUS: Agent input")
		})
	}
}
