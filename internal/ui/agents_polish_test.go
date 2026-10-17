package ui

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/agentterminal"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi/fake"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/modals"
	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
)

// --- "!" acknowledgement ---

func TestAgentsPickerOpenAcknowledgesFailures(t *testing.T) {
	e := newAgentsEnv(t)
	e.fb.Sessions.Add(
		e.agent("r1", "claude", "running", e.wtA, "Fix", 3),
		e.agent("f1", "claude", "failed", e.wtA, "Broken", 2),
	)
	e.relist(t)
	e.openTab(t)
	if row := e.rowOf("feat-a"); !strings.HasSuffix(strings.TrimRight(row, " "), "!") {
		t.Fatalf("setup: failed marker missing: %q", row)
	}
	e.key(t, "a")
	if !e.m.agents.ack["f1"] {
		t.Error("opening the picker shows the failed session, which acknowledges it")
	}
	if row := e.rowOf("feat-a"); strings.Contains(row, "!") {
		t.Errorf("! should clear when the picker opens: %q", row)
	}
}

func TestAgentsPickerOpenOnlyAcknowledgesItsWorktree(t *testing.T) {
	e := newAgentsEnv(t)
	e.fb.Sessions.Add(
		e.agent("fa", "claude", "failed", e.wtA, "A", 1),
		e.agent("fb", "claude", "failed", e.wtB, "B", 1),
	)
	e.relist(t)
	e.openTab(t)
	e.key(t, "a")
	if e.m.agents.ack["fb"] {
		t.Error("a failure in another worktree was never shown")
	}
}

// --- help ---

func TestAgentKeysHaveTheirOwnHelpSection(t *testing.T) {
	var agentsSection []string
	for _, sec := range keymapSections {
		if sec.name == "Agents tab" {
			agentsSection = sec.actions
		}
	}
	want := []string{"agents_tab", "agent_all", "agent_new", "agent_accounts", "agent_usage", "agent_attach", "agent_stop"}
	if strings.Join(agentsSection, ",") != strings.Join(want, ",") {
		t.Fatalf("Agents tab section = %v, want %v", agentsSection, want)
	}
	for _, name := range want {
		for _, sec := range keymapSections {
			if sec.name == "Agents tab" {
				continue
			}
			for _, a := range sec.actions {
				if a == name {
					t.Errorf("%s also listed under %q", name, sec.name)
				}
			}
		}
	}

	e := newAgentsEnv(t)
	e.m.width, e.m.height = 120, 40
	nm, _ := e.m.openKeymap()
	e.m = nm.(Model)
	view := ansi.Strip(e.m.modal.View())
	for _, s := range []string{"Agents tab (7)", agentPrefixSection + " (8)"} {
		if !strings.Contains(view, s) {
			t.Errorf("help lacks %q:\n%s", s, view)
		}
	}
}

func TestAgentPrefixKeysDriveTheRouter(t *testing.T) {
	// Every key the help lists does what it says after ctrl+]; anything else is swallowed.
	type effect func(t *testing.T, e *agentsEnv, st *fake.Stream)
	tests := []struct {
		key  tea.KeyMsg
		want effect
	}{
		{runes("d"), func(t *testing.T, e *agentsEnv, st *fake.Stream) {
			if e.m.agentTerminalMode() {
				t.Error("d should leave terminal mode")
			}
		}},
		{runes("a"), func(t *testing.T, e *agentsEnv, st *fake.Stream) {
			if e.m.modal == nil || e.m.agentTerminalMode() {
				t.Error("a should open the picker and leave terminal mode")
			}
		}},
		{runes("u"), func(t *testing.T, e *agentsEnv, st *fake.Stream) {
			if e.m.agents.overlay == nil || e.m.agents.overlay.quick == nil {
				t.Error("u should open quick usage")
			}
		}},
		{runes("m"), func(t *testing.T, e *agentsEnv, st *fake.Stream) {
			if e.m.agents.overlay == nil || e.m.agents.overlay.accounts == nil {
				t.Error("m should open accounts")
			}
		}},
		{runes("s"), func(t *testing.T, e *agentsEnv, st *fake.Stream) {
			if e.m.modal == nil || !strings.Contains(ansi.Strip(e.m.modal.View()), "Stop") {
				t.Error("s should ask to stop")
			}
		}},
		{runes("t"), func(t *testing.T, e *agentsEnv, st *fake.Stream) {
			if st.Takes() != 0 || len(st.Inputs()) != 0 {
				t.Error("t as the writer has nothing to take and sends nothing")
			}
		}},
		{tea.KeyMsg{Type: tea.KeyEsc}, func(t *testing.T, e *agentsEnv, st *fake.Stream) {
			if !e.m.agentTerminalMode() || len(st.Inputs()) != 0 || e.m.agents.view.prefix {
				t.Error("esc closes the strip without reaching the agent")
			}
		}},
		{ctrlBracket, func(t *testing.T, e *agentsEnv, st *fake.Stream) {
			if got := inputsOf(st); len(got) != 1 || got[0] != "\x1d" {
				t.Errorf("ctrl+] ctrl+] sends 0x1d, got %q", got)
			}
		}},
		{runes("x"), func(t *testing.T, e *agentsEnv, st *fake.Stream) {
			if !e.m.agentTerminalMode() || len(st.Inputs()) != 0 || e.m.modal != nil || e.m.agents.overlay != nil {
				t.Error("an unknown key is swallowed")
			}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.key.String(), func(t *testing.T) {
			e, st := terminalEnv(t)
			e.typeKeys(t, ctrlBracket, tc.key)
			tc.want(t, e, st)
		})
	}
	// The help lists exactly these keys.
	if len(agentPrefixKeys) != 8 {
		t.Errorf("help lists %d prefix keys, want 8", len(agentPrefixKeys))
	}
}

func TestKeymapPrefixRowsAreInert(t *testing.T) {
	e := newAgentsEnv(t)
	nm, _ := e.m.openKeymap()
	e.m = nm.(Model)
	row := "ctrl+] d  detach  · " + agentPrefixSection
	nm, _ = e.m.Update(modals.SubmitMsg{Kind: modals.KindKeymap, Value: row})
	e.m = nm.(Model)
	if e.m.prefs != nil || e.m.modal == nil {
		t.Errorf("a fixed key must not open preferences: prefs=%v modal=%v", e.m.prefs != nil, e.m.modal != nil)
	}
	nm, _ = e.m.Update(modals.SubmitMsg{Kind: modals.KindKeymap, Value: "q  quit  · Navigation"})
	e.m = nm.(Model)
	if e.m.prefs == nil {
		t.Error("a bindable row still opens the key editor")
	}
}

// --- status bar hint per state ---

func TestAgentsContextHintPerState(t *testing.T) {
	plain := func(e *agentsEnv) string { return ansi.Strip(e.m.contextHelp()) }

	t.Run("navigation never advertises ctrl+]", func(t *testing.T) {
		e := liveEnv(t)
		e.attached(t)
		e.frames(t, status("running"))
		if got := plain(e); got != "tab focus · g agents · ? keys" {
			t.Errorf("navigation = %q", got)
		}
	})
	t.Run("connecting", func(t *testing.T) {
		e := liveEnv(t) // the attach has not landed
		if got := plain(e); !strings.HasPrefix(got, "connecting…") {
			t.Errorf("connecting = %q", got)
		}
	})
	t.Run("ended", func(t *testing.T) {
		e := liveEnv(t)
		e.attached(t)
		e.frames(t, status("exited"))
		if got := plain(e); got != "n new · a all · tab focus · g agents · ? keys" {
			t.Errorf("ended = %q", got)
		}
	})
	t.Run("terminal", func(t *testing.T) {
		e, _ := terminalEnv(t)
		if got := plain(e); got != "Ctrl+] Bonsai command prefix" {
			t.Errorf("terminal = %q", got)
		}
	})
	t.Run("observing other", func(t *testing.T) {
		e := goldenLive(t, 120, 30, (*agentsEnv).twoSessions)
		if _, err := e.fb.Sessions.Attach(t.Context(), "b", agentsapi.AttachOptions{}); err != nil {
			t.Fatal(err)
		}
		e.send(t, tea.KeyMsg{Type: tea.KeyEnter})
		e.attached(t)
		e.frames(t, status("running"))
		if got := plain(e); got != "Ctrl+] t take control · Ctrl+] d detach" {
			t.Errorf("observing other = %q", got)
		}
	})
	t.Run("terminal while reconnecting", func(t *testing.T) {
		e, _ := terminalEnv(t)
		e.m.agents.view.stream = nil
		if got := plain(e); got != "connecting… · Ctrl+] d detach" {
			t.Errorf("reconnecting = %q", got)
		}
	})
}

// --- ASCII preference ---

func nonASCII(s string) []rune {
	var out []rune
	for _, r := range ansi.Strip(s) {
		if r >= 0x80 {
			out = append(out, r)
		}
	}
	return out
}

const asciiScreen = "\x1b[2J\x1b[Hagent output\x1b[24;1H> "

// agentDrawing is the agent's own unicode, which the preference never touches.
const agentDrawing = "\x1b[2J\x1b[H\u2500\u2502\u25cf\u2026"

func TestAgentsASCIIPreferenceEndToEnd(t *testing.T) {
	setup := func(t *testing.T, w, h int) *agentsEnv {
		e := newAgentsEnv(t)
		e.m.state.Prefs.AgentGlyphs = "ascii"
		withUsage(t, e)
		e.fb.Sessions.Add(e.twoSessions()...)
		e.fb.Sessions.Add(
			e.agent("x", "codex", "exited", e.wtA, "Old", 0),
			e.agent("f", "claude", "failed", e.wtA, "Broke", 0),
		)
		e.relist(t)
		e.sizeTo(t, w, h)
		e.openTab(t)
		e.apply(t, fetchAgentUsage(e.fb.Accounts, e.m.agents.usageSeq, e.m.agents.usageAccount))
		e.attached(t)
		e.frames(t, agentterminal.Frame{Type: "output", Data: []byte(asciiScreen)}, status("running"))
		return e
	}
	// The terminal body is the agent's own drawing, so rows are checked
	// without it: fixed rows above and the hint/strip rows below.
	check := func(t *testing.T, label, s string) {
		t.Helper()
		if bad := nonASCII(s); len(bad) > 0 {
			t.Errorf("%s has non-ASCII %q:\n%s", label, string(bad), ansi.Strip(s))
		}
	}
	for _, size := range [][2]int{{160, 40}, {80, 24}, {58, 20}} {
		w, h := size[0], size[1]
		t.Run(fmt.Sprintf("navigation %dx%d", w, h), func(t *testing.T) {
			e := setup(t, w, h)
			check(t, "pane", e.m.renderAgentsPane(w, h))
		})
		t.Run(fmt.Sprintf("terminal mode %dx%d", w, h), func(t *testing.T) {
			e := setup(t, w, h)
			e.enterTerminalWith(t, asciiScreen)
			e.send(t, tea.KeyMsg{Type: tea.KeyCtrlCloseBracket})
			check(t, "terminal pane with the control strip", e.m.renderAgentsPane(w, h))
		})
	}
	t.Run("agent drawing survives", func(t *testing.T) {
		e := setup(t, 120, 30)
		e.frames(t, agentterminal.Frame{Type: "output", Data: []byte(agentDrawing)})
		out := ansi.Strip(e.m.renderAgentsPane(120, 30))
		for _, r := range "\u2500\u2502\u25cf\u2026" {
			if !strings.ContainsRune(out, r) {
				t.Errorf("the agent's %q was rewritten", r)
			}
		}
	})
	t.Run("states", func(t *testing.T) {
		e := setup(t, 120, 30)
		for _, id := range []string{"x", "f"} { // ended sessions in the header
			e.m.agents.selected[cleanPath(e.wtA)] = id
			check(t, "header of "+id, e.m.renderAgentsPane(120, 30))
		}
		e.m.agents.err = "boom"
		check(t, "stale marker", e.m.renderAgentsPane(120, 30))
	})
	t.Run("empty", func(t *testing.T) {
		e := newAgentsEnv(t)
		e.m.state.Prefs.AgentGlyphs = "ascii"
		e.relist(t)
		e.openTab(t)
		check(t, "empty pane", e.m.renderAgentsPane(100, 24))
	})
	t.Run("picker marks", func(t *testing.T) {
		e := setup(t, 120, 30)
		e.m.agents.pickIDs = map[string]string{}
		items := e.m.agentPickerItems(cleanPath(e.wtA), "")
		if len(items) != 4 {
			t.Fatalf("items = %q", items)
		}
		check(t, "picker items", strings.Join(items, "\n"))
		marks := map[string]string{}
		for _, it := range items {
			plain := ansi.Strip(it)
			marks[plain[:1]] += plain
		}
		if !strings.Contains(marks["+"], "Old") || !strings.Contains(marks["x"], "Broke") || !strings.Contains(marks["*"], "Auth fix") {
			t.Errorf("marks = %v, want * running, + exited, x failed", marks)
		}
	})
	t.Run("launch dialog", func(t *testing.T) {
		e := accountsEnv(t)
		e.m.state.Prefs.AgentGlyphs = "ascii"
		e.m.width, e.m.height = 100, 30
		e.openLaunch(t)
		check(t, "launch dialog", e.m.agentOverlayView(e.m.agentPaneSize()))
	})
}

// --- themes ---

var sgrColor = regexp.MustCompile(`\x1b\[([0-9;]*)m`)

// sgrColors returns the foreground and background specs every SGR sets.
func sgrColors(s string) (fg, bg map[string]bool) {
	fg, bg = map[string]bool{}, map[string]bool{}
	for _, m := range sgrColor.FindAllStringSubmatch(s, -1) {
		parts := strings.Split(m[1], ";")
		for i := 0; i < len(parts); i++ {
			switch p := parts[i]; {
			case (p == "38" || p == "48") && i+1 < len(parts):
				n := 2 // 38;5;n
				if parts[i+1] == "2" {
					n = 4 // 38;2;r;g;b
				}
				end := min(i+1+n, len(parts))
				spec := strings.Join(parts[i+1:end], ";")
				if p == "38" {
					fg[spec] = true
				} else {
					bg[spec] = true
				}
				i = end - 1
			case len(p) == 2 && (p[0] == '3' || p[0] == '9') && p[1] != '8':
				fg[p] = true // basic 16-color foreground
			case len(p) == 2 && p[0] == '4' && p[1] != '8', len(p) == 3 && strings.HasPrefix(p, "10"):
				bg[p] = true
			}
		}
	}
	return fg, bg
}

// agentSurfaces renders every agents-tab surface that carries color.
func agentSurfaces(t *testing.T, p theme.Palette) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, size := range [][2]int{{160, 40}, {80, 24}, {58, 20}} {
		w, h := size[0], size[1]
		name := fmt.Sprintf("%dx%d", w, h)
		e := newAgentsEnv(t)
		useTheme(t, p) // after New, which installs the configured theme
		withUsage(t, e)
		e.fb.Sessions.Add(e.twoSessions()...)
		e.fb.Sessions.Add(
			e.agent("x", "antigravity", "exited", e.wtA, "Old", 0),
			e.agent("f", "claude", "failed", e.wtA, "Broke", 0),
			e.agent("s", "codex", "starting", e.wtA, "Boot", 0),
		)
		e.relist(t)
		e.sizeTo(t, w, h)
		e.openTab(t)
		e.apply(t, fetchAgentUsage(e.fb.Accounts, e.m.agents.usageSeq, e.m.agents.usageAccount))
		e.attached(t)
		e.frames(t, status("running"))
		out["navigation "+name] = e.m.renderAgentsPane(w, h)
		e.m.agents.pickIDs = map[string]string{}
		out["picker "+name] = strings.Join(e.m.agentPickerItems(cleanPath(e.wtA), ""), "\n")
		for _, id := range []string{"x", "f", "s"} { // every state dot
			e.m.agents.selected[cleanPath(e.wtA)] = id
			out["header "+id+" "+name] = e.m.renderAgentsPane(w, h)
		}
		e.m.agents.selected[cleanPath(e.wtA)] = "b"
		e.m.agents.err = "stale"
		out["stale "+name] = e.m.renderAgentsPane(w, h)
		e.m.agents.err = ""
		e.enterTerminal(t)
		out["terminal "+name] = e.m.renderAgentsPane(w, h)
		e.send(t, tea.KeyMsg{Type: tea.KeyCtrlCloseBracket})
		out["control strip "+name] = e.m.renderAgentsPane(w, h)
		out["status bar "+name] = e.m.contextHelp()
	}
	return out
}

func useTrueColor(t *testing.T) {
	t.Helper()
	old := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(old) })
}

func useTheme(t *testing.T, p theme.Palette) {
	t.Helper()
	old := theme.Current
	theme.Current = p
	applyTheme()
	t.Cleanup(func() { theme.Current = old; applyTheme() })
}

// A palette of unique truecolor values: any color in the output that is not one
// of them is hard-coded, and would ignore the user's theme (a hard-coded light
// or dark color is unreadable on the opposite theme).
func TestAgentsColorsComeOnlyFromThePalette(t *testing.T) {
	useTrueColor(t)
	roles := []string{"#010203", "#040506", "#070809", "#0a0b0c", "#0d0e0f", "#101112", "#131415", "#161718", "#191a1b"}
	p := theme.Palette{
		Accent: lipgloss.Color(roles[0]), BorderFocus: lipgloss.Color(roles[1]), Border: lipgloss.Color(roles[2]),
		Dim: lipgloss.Color(roles[3]), Text: lipgloss.Color(roles[4]), Success: lipgloss.Color(roles[5]),
		Danger: lipgloss.Color(roles[6]), Warning: lipgloss.Color(roles[7]), PRBadge: lipgloss.Color(roles[8]),
	}
	allowed := map[string]bool{}
	for _, r := range roles {
		n := func(s string) int { v, _ := strconv.ParseInt(s, 16, 0); return int(v) }
		allowed[fmt.Sprintf("2;%d;%d;%d", n(r[1:3]), n(r[3:5]), n(r[5:7]))] = true
	}
	for name, out := range agentSurfaces(t, p) {
		fg, bg := sgrColors(out)
		for spec := range fg {
			if !allowed[spec] {
				t.Errorf("%s uses a color outside the palette: %q", name, spec)
			}
		}
		// Only foregrounds are set, so none can equal its background.
		for spec := range bg {
			t.Errorf("%s sets a background %q; text would depend on the terminal's", name, spec)
		}
	}
}

func luminance(c lipgloss.Color) float64 {
	rgb := termenv.ConvertToRGB(termenv.ANSI256.Color(string(c)))
	lin := func(v float64) float64 {
		if v <= 0.03928 {
			return v / 12.92
		}
		return math.Pow((v+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(rgb.R) + 0.7152*lin(rgb.G) + 0.0722*lin(rgb.B)
}

// Every built-in theme is dark: the text roles must stay readable against a
// dark terminal background, and distinct roles must stay distinguishable.
func TestAgentsThemeContrast(t *testing.T) {
	const bg = 0.013 // luminance of #1e1e1e, a typical dark terminal
	for _, name := range theme.Presets() {
		t.Run(name, func(t *testing.T) {
			p := theme.Resolve(name, nil)
			roles := map[string]lipgloss.Color{
				"accent": p.Accent, "dim": p.Dim, "text": p.Text,
				"success": p.Success, "danger": p.Danger, "warning": p.Warning,
			}
			for role, c := range roles {
				if ratio := (luminance(c) + 0.05) / (bg + 0.05); ratio < 2 {
					t.Errorf("%s %s (%s) contrast %.2f < 2 on a dark terminal", name, role, c, ratio)
				}
			}
			// Provider glyphs must not vanish into the dim secondary text.
			useTheme(t, p)
			for _, prov := range []agents.ProviderID{"claude", "codex", "antigravity"} {
				if theme.ProviderStyle(prov).GetForeground() == lipgloss.TerminalColor(p.Dim) {
					t.Errorf("%s: %s glyph has the dim color", name, prov)
				}
			}
		})
	}
}
