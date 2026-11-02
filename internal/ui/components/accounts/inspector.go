package accounts

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/agents/usagefmt"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
)

// inspector is the body under the tab row. Only the body changes with the tab;
// the table, divider and tabs around it stay put.
type inspector struct {
	body []string
	// tail holds the actions: they stay visible when the body must scroll.
	tail []string
	// from and to are the body lines that must stay visible when it scrolls
	// (the focused field); zero for bodies that scroll from the top.
	from, to int
	// scroll marks a body that scrolls from offset when it overflows, with a
	// line saying how much is hidden, instead of being cut.
	scroll bool
	offset int
}

// scrollMax is how far a scrolling body can move in rows lines.
func (in inspector) scrollMax(rows int) int {
	if !in.scroll || len(in.body) <= rows || squeezes(in.body, rows) {
		return 0
	}
	return len(in.body) - (rows - 1)
}

// squeezes reports that dropping blank lines alone makes lines fit.
func squeezes(lines []string, rows int) bool {
	n := 0
	for _, l := range lines {
		if l != "" {
			n++
		}
	}
	return n <= rows
}

func (in inspector) height() int { return len(in.body) + len(in.tail) }

// render fits the inspector in rows lines: blank lines go first, then the
// body scrolls around its focus, and the actions are never cut before it.
func (in inspector) render(rows int) []string {
	if rows <= 0 {
		return nil
	}
	tail := in.tail
	if len(tail) > rows {
		tail = tail[len(tail)-rows:]
	}
	room := rows - len(tail)
	body, from, to := in.body, in.from, in.to
	if in.scrollMax(room) > 0 {
		window := max(room-1, 0)
		start := min(max(in.offset, 0), len(body)-window)
		dim := lipgloss.NewStyle().Foreground(theme.Current.Dim)
		var note string
		switch below := len(body) - start - window; {
		case start > 0 && below > 0:
			note = fmt.Sprintf("%d above · %d more below · up/down scrolls", start, below)
		case start > 0:
			note = fmt.Sprintf("%d above · up/down scrolls", start)
		default:
			note = fmt.Sprintf("%d more below · up/down scrolls", below)
		}
		out := append([]string{}, body[start:start+window]...)
		return append(append(out, dim.Render(note)), tail...)
	}
	if len(body) > room {
		if from == to {
			body = squeeze(body, room)
		} else {
			for len(body) > room && len(body) > to && body[len(body)-1] == "" {
				body = body[:len(body)-1]
			}
			top := 0
			if to > room {
				top = to - room
			}
			top = min(top, from)
			body = body[top:min(top+room, len(body))]
		}
	}
	return append(append([]string{}, body...), tail...)
}

// inspector builds the body of the active tab. showStatus adds the sign-in
// state to Overview when the table, which normally shows it, is not drawn.
func (m Model) inspector(a agentsapi.Account, w int, showStatus bool) inspector {
	switch m.tab {
	case TabUsage:
		return m.usageInspector(a, w)
	case TabDefaults:
		return m.defaultsInspector(a, w)
	}
	return m.overviewInspector(a, w, showStatus)
}

func (m Model) overviewInspector(a agentsapi.Account, w int, showStatus bool) inspector {
	th := theme.Current
	dim := lipgloss.NewStyle().Foreground(th.Dim)

	loading := dim.Render("Loading…")
	info := func(value string) string {
		switch {
		case m.describing:
			return loading
		case m.describeErr != "":
			return dim.Render("Unavailable")
		case value == "":
			return dim.Render("-")
		}
		return value
	}
	identity := info(a.Info.Identity)
	if m.describeErr != "" && !m.describing {
		identity = dim.Render("Unavailable · " + m.describeErr)
	}

	var left, right []string
	if showStatus {
		left = append(left, kv("Status", connectionLabel(a), 10))
	}
	left = append(left,
		kv("Identity", identity, 10),
		kv("Auth", info(authLabel(a.Info.AuthMode)), 10),
		kv("Sessions", fmt.Sprintf("%d running", a.Running), 10),
	)
	right = []string{
		kv("Model", m.defaultValue("model"), 8),
		kv("Effort", m.defaultValue("effort"), 8),
	}

	var body []string
	if w >= 64 {
		colW := w / 2
		for i := range max(len(left), len(right)) {
			l := ansi.Truncate(at(left, i), colW-2, "…")
			body = append(body, fit(l, colW)+at(right, i))
		}
	} else {
		body = append(left, right...)
	}

	if m.embedded {
		body = append(body, m.overviewReasons(a, w)...)
	}

	focused := m.focus == focusContent
	login := button("Sign in again", focused && m.ov.focus == btnLogin, false)
	remove := plainAction("Remove account", focused && m.ov.focus == btnRemove, a.Running > 0)
	actions := twoCol(login, remove, w)
	if actions == login {
		actions = login + "  " + remove
	}
	tail := []string{""}
	if m.ov.msg != "" {
		tail = append(tail, lipgloss.NewStyle().Foreground(th.Warning).Render(m.ov.msg))
	}
	return inspector{body: body, tail: append(tail, actions)}
}

// overviewReasons says why an account is not fully usable, from what the
// provider reported: nothing is guessed beyond "unknown" when it stayed silent.
func (m Model) overviewReasons(a agentsapi.Account, w int) []string {
	th := theme.Current
	dim := lipgloss.NewStyle().Foreground(th.Dim)
	if m.describing || m.describeErr != "" {
		return nil
	}
	var lines []string
	switch authOf(a.Info) {
	case authNeedsSignIn:
		lines = append(lines, lipgloss.NewStyle().Foreground(th.Warning).Render("Sign-in required: choose Sign in again."))
	case authUnknown:
		lines = append(lines, dim.Render("Sign-in state unknown: the provider CLI reported none (missing or not responding)."))
	}
	for _, warn := range a.Info.Warnings {
		lines = append(lines, dim.Render(warn))
	}
	var out []string
	for _, l := range lines {
		for _, wrapped := range strings.Split(ansi.Wrap(l, w, ""), "\n") {
			out = append(out, wrapped)
		}
	}
	if len(out) > 0 {
		out = append([]string{""}, out...)
	}
	return out
}

// plainAction is a secondary action: unbracketed until it has focus.
func plainAction(label string, focused, disabled bool) string {
	if focused {
		return button(label, true, disabled)
	}
	s := lipgloss.NewStyle()
	if disabled {
		s = s.Faint(true)
	}
	return s.Render(label)
}

func (m Model) usageInspector(a agentsapi.Account, w int) inspector {
	dim := lipgloss.NewStyle().Foreground(theme.Current.Dim)
	e := m.usage[a.ID]
	u := e.Usage

	updated := ""
	switch {
	case e.Refreshing:
		updated = "Refreshing…"
	case e.Have && !u.FetchedAt.IsZero():
		updated = updatedText(m.now(), u.FetchedAt)
	}
	head := twoCol(dim.Bold(true).Render("REMAINING CAPACITY"), dim.Render(updated), w)
	body := []string{head}

	if !e.Have {
		if e.Refreshing {
			return inspector{body: append(body, dim.Render("Loading usage…"))}
		}
		body = append(body, dim.Render("Usage unavailable"))
		for _, l := range wrapUsageFields([]string{e.Err}, w) {
			body = append(body, dim.Render(l))
		}
		if m.embedded && NeedsSignIn(a.Info) {
			body = append(body, dim.Render("Sign in again from Overview, then refresh."))
		}
		return inspector{body: body}
	}

	// The strip helpers are shared with the quick usage view, so a window,
	// pool or credit reads the same in both.
	q := Quick{usage: m.usage, now: m.now, ascii: m.ascii}
	windows, extra := stripWindows(a, u)
	labelW := 0
	for _, row := range windows {
		for i, l := range row {
			if l != nil {
				labelW = max(labelW, ansi.StringWidth(stripWindowName(l, i)))
			}
		}
	}
	extraLabel := func(l agents.UsageLimit) string {
		label := limitLabel(l)
		if l.Group != "" {
			label = l.Group + " · " + usagefmt.WindowName(&l, label)
		}
		return label
	}
	for _, l := range extra {
		labelW = max(labelW, ansi.StringWidth(extraLabel(l)))
	}

	var lines []string
	for _, row := range windows {
		for i, l := range row {
			if l != nil {
				lines = append(lines, q.stackedWindow(l, i, labelW, w)...)
			}
		}
	}
	for _, l := range extra {
		label := extraLabel(l)
		l.Window, l.Label = "", label
		lines = append(lines, q.stackedWindow(&l, 0, labelW, w)...)
	}
	var meta []string
	for _, fields := range usageMetadata(u.Snapshot, m.now()) {
		meta = append(meta, wrapUsageFields(fields, w)...)
	}
	if len(lines) == 0 && len(meta) == 0 {
		lines = []string{dim.Render("No usage reported")}
	}

	if u.Stale {
		// Last good values stay readable but lose their severity colors.
		for i, l := range lines {
			lines[i] = dim.Render(ansi.Strip(l))
		}
		for i, l := range meta {
			meta[i] = dim.Render(ansi.Strip(l))
		}
		note := "Stale · Showing last good data"
		if u.Err != "" {
			note += " · " + u.Err
		}
		body = append(body, dim.Render(ansi.Truncate(note, w, "…")))
	}
	body = append(body, lines...)
	if len(meta) > 0 {
		body = append(body, "")
		body = append(body, meta...)
	}
	return inspector{body: body, scroll: true, offset: m.usageOff}
}

func (m Model) defaultsInspector(a agentsapi.Account, w int) inspector {
	th := theme.Current
	dim := lipgloss.NewStyle().Foreground(th.Dim)
	switch {
	case m.df.err != "" && !m.df.loaded:
		return inspector{body: []string{dim.Render("Defaults unavailable"), dim.Render(m.df.err)}}
	case !m.df.loaded:
		return inspector{body: []string{dim.Render("Loading defaults…")}}
	}

	if m.embedded && len(m.defaults.Fields) == 0 {
		return inspector{body: []string{dim.Render("No editable defaults"), dim.Render(providerName(a.Provider) + " declares no launch defaults to edit here.")}}
	}
	fields, from, to := m.df.form.CompactLines(w)
	noteText := "Applies to new sessions for " + providerName(a.Provider) + " / " + a.Name + "."
	if m.embedded {
		noteText = "Provider default = inherited; a value applies to new sessions only."
	}
	note := dim.Render(ansi.Truncate(noteText, w, "…"))
	switch {
	case m.df.saveErr != "":
		note = lipgloss.NewStyle().Foreground(th.Danger).Render(ansi.Truncate("Couldn't save: "+m.df.saveErr, w, "…"))
	case m.df.saving:
		note = dim.Render("Saving…")
	}
	focused := m.focus == focusContent
	reset := plainAction("Reset", focused && m.df.zone == zoneReset, false)
	save := saveButton(focused && m.df.zone == zoneSave)
	buttons := strings.Repeat(" ", max(w-ansi.StringWidth(reset)-ansi.StringWidth(save)-3, 0)) + reset + "   " + save
	return inspector{body: append(fields, ""), tail: []string{note, buttons}, from: from, to: to}
}

// saveButton is the primary action: accent-bold until it has focus.
func saveButton(focused bool) string {
	if focused {
		return button("Save defaults", true, false)
	}
	return lipgloss.NewStyle().Bold(true).Foreground(theme.Current.Accent).Render("[Save defaults]")
}
