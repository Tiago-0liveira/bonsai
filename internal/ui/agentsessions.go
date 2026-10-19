package ui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/accounts"
	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
)

const (
	sessionsStatusW  = 10 // "● Starting" is the widest status label
	sessionsMinList  = 34 // narrowest list column that keeps name and status on one line
	sessionsMinInsp  = 26 // narrowest inspector column worth drawing beside the list
	sessionsDivider  = 3  // " │ "
	sessionsIDCells  = 8
	sessionsMaxStack = 7 // separator plus the detail lines under a stacked list
)

// sessionsPickMsg asks the Agents tab to show a session; sessionsNewMsg asks
// for the launcher. The picker closes in both cases.
type (
	sessionsPickMsg struct{ path, id string } // path: the picker's own worktree
	sessionsNewMsg  struct{}
)

// sessionsPicker is the "all sessions" overlay of one worktree: a searchable
// list beside an inspector that follows the selection. It only draws and
// navigates; opening a session is the parent's job.
type sessionsPicker struct {
	w, h     int
	ascii    bool
	branch   string
	path     string              // worktree whose sessions are listed
	sessions []agentsapi.Session // ordered: running newest first, then ended newest first
	dupes    map[string]bool     // names shared by more than one session
	query    textinput.Model
	search   bool
	cursor   int
	top      int // first visible item
	selID    string
}

func newSessionsPicker(branch string, ascii bool) sessionsPicker {
	ti := textinput.New()
	ti.Prompt = ""
	ti.CharLimit = 100
	return sessionsPicker{branch: branch, ascii: ascii, query: ti}
}

func (p *sessionsPicker) SetSize(w, h int) {
	p.w, p.h = w, h
	p.reveal()
}

// setSessions replaces the list, keeping the selected session when it is still
// listed.
func (p *sessionsPicker) setSessions(list []agentsapi.Session) {
	p.sessions = list
	p.dupes = map[string]bool{}
	seen := map[string]bool{}
	for _, s := range list {
		n := sessionName(s)
		if seen[n] {
			p.dupes[n] = true
		}
		seen[n] = true
	}
	vis := p.visible()
	p.cursor = 0
	for i, s := range vis {
		if s.ID == p.selID {
			p.cursor = i
			break
		}
	}
	p.sync()
}

// sync records the selected session and scrolls it into view.
func (p *sessionsPicker) sync() {
	vis := p.visible()
	p.cursor = max(min(p.cursor, len(vis)-1), 0)
	p.selID = ""
	if len(vis) > 0 {
		p.selID = vis[p.cursor].ID
	}
	p.reveal()
}

// orderedSessions is a worktree's sessions as the picker lists them: running
// ones first, then ended ones, each newest first.
func (m Model) orderedSessions(path string) []agentsapi.Session {
	var running, ended []agentsapi.Session
	for _, i := range m.agents.byTree[path] {
		if s := m.agents.sessions[i]; s.Active() {
			running = append(running, s)
		} else {
			ended = append(ended, s)
		}
	}
	sort.SliceStable(running, func(a, b int) bool { return running[a].CreatedAt.After(running[b].CreatedAt) })
	sort.SliceStable(ended, func(a, b int) bool { return ended[a].CreatedAt.After(ended[b].CreatedAt) })
	return append(running, ended...)
}

// visible is the sessions matching the search, in list order.
func (p sessionsPicker) visible() []agentsapi.Session {
	q := strings.ToLower(strings.TrimSpace(p.query.Value()))
	if q == "" {
		return p.sessions
	}
	var out []agentsapi.Session
	for _, s := range p.sessions {
		plain := sessionName(s) + " " + providerName(s.Provider) + " " + s.ProfileName
		if strings.Contains(strings.ToLower(plain), q) {
			out = append(out, s)
		}
	}
	return out
}

// label is the session's name, with a short ID suffix when another session
// shares it. The suffix survives truncation so the rows stay distinguishable.
func (p sessionsPicker) label(s agentsapi.Session, width int) string {
	name := sessionName(s)
	suffix := ""
	if p.dupes[name] {
		id := s.ID
		if len(id) > sessionsIDCells {
			id = id[len(id)-sessionsIDCells:]
		}
		suffix = " · …" + id
	}
	if width <= 0 {
		return ""
	}
	room := width - ansi.StringWidth(suffix)
	if room < 4 {
		return ansi.Truncate(name+suffix, width, "…")
	}
	return ansi.Truncate(name, room, "…") + suffix
}

// --- keys ---

func (p sessionsPicker) Update(msg tea.Msg) (sessionsPicker, tea.Cmd) {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return p, nil
	}
	closeCmd := func() tea.Msg { return accounts.CloseMsg{} }
	switch k.String() {
	case "ctrl+c":
		return p, closeCmd
	case "esc":
		if !p.search {
			return p, closeCmd
		}
		p.search = false
		p.query.Blur()
		p.query.SetValue("")
		p.cursor = 0
		p.sync()
		return p, nil
	case "up", "ctrl+k":
		p.cursor--
		p.sync()
		return p, nil
	case "down", "ctrl+j":
		p.cursor++
		p.sync()
		return p, nil
	case "pgup":
		p.cursor -= max(p.geom().visible, 1)
		p.sync()
		return p, nil
	case "pgdown":
		p.cursor += max(p.geom().visible, 1)
		p.sync()
		return p, nil
	case "enter":
		if vis := p.visible(); p.cursor >= 0 && p.cursor < len(vis) {
			id := vis[p.cursor].ID
			path := p.path
			return p, func() tea.Msg { return sessionsPickMsg{path: path, id: id} }
		}
		return p, nil
	}
	if p.search {
		var cmd tea.Cmd
		p.query, cmd = p.query.Update(k)
		p.cursor = 0
		p.sync()
		return p, cmd
	}
	switch k.String() {
	case "/":
		p.search = true
		return p, p.query.Focus()
	case "n":
		return p, func() tea.Msg { return sessionsNewMsg{} }
	}
	return p, nil
}

// --- layout ---

// sessionsGeom is how the pane's rows and columns are spent.
type sessionsGeom struct {
	iw, h int
	// chrome lines, dropped in this order when the pane is short
	gap, botRule, colHdr, rule, subtitle, title bool
	body                                        int // rows between the search line and the bottom rule
	wide                                        bool
	lw, rw                                      int
	listRows, detailRows                        int // stacked layout; wide uses body for both columns
	spacers                                     bool
	visible                                     int // items shown without scrolling
}

func (p sessionsPicker) geom() sessionsGeom {
	w, h := accounts.FrameSize(p.w, p.h)
	g := sessionsGeom{iw: accounts.ContentWidth(w), h: h,
		gap: true, botRule: true, colHdr: true, rule: true, subtitle: true, title: true}
	chrome := func() int {
		n := 2 // search and footer always stay
		for _, on := range []bool{g.gap, g.botRule, g.colHdr, g.rule, g.subtitle, g.title} {
			if on {
				n++
			}
		}
		return n
	}
	for _, off := range []*bool{&g.gap, &g.botRule, &g.colHdr, &g.rule, &g.subtitle, &g.title} {
		if chrome() <= h-2 {
			break
		}
		*off = false
	}
	g.body = max(h-chrome(), 0)
	n := len(p.visible())
	hdr := 0
	if g.colHdr {
		hdr = 1
	}

	g.wide = g.iw >= sessionsMinList+sessionsDivider+sessionsMinInsp
	itemRows := 0
	switch {
	case g.wide:
		g.lw = max(min(g.iw*11/20, g.iw-sessionsDivider-sessionsMinInsp), sessionsMinList)
		g.rw = g.iw - g.lw - sessionsDivider
		g.listRows = g.body
		itemRows = g.body - hdr
	default:
		g.lw = g.iw
		d := min(sessionsMaxStack, max(g.body-hdr-4, 0))
		if d < 2 {
			d = 0
		}
		g.listRows = g.body - d
		if n > 0 && d > 0 {
			// A short list leaves the rest to the details, which sit right below.
			natural := hdr + 3*n - 1
			if g.listRows-hdr < 3*n-1 {
				natural = hdr + 2*n
			}
			g.listRows = min(g.listRows, natural)
		}
		g.detailRows = min(d, g.body-g.listRows)
		itemRows = g.listRows - hdr
	}
	g.spacers = n > 0 && itemRows >= 3*n-1
	if g.spacers {
		g.visible = (itemRows + 1) / 3
	} else {
		g.visible = itemRows / 2
	}
	g.visible = min(max(g.visible, 1), max(n, 1))
	return g
}

// reveal scrolls the list so the selected item is visible.
func (p *sessionsPicker) reveal() {
	n := len(p.visible())
	v := p.geom().visible
	if p.cursor < p.top {
		p.top = p.cursor
	}
	if p.cursor >= p.top+v {
		p.top = p.cursor - v + 1
	}
	p.top = max(min(p.top, n-v), 0)
}

// --- view ---

// View fills the pane with the picker. The pane draws the one outer boundary;
// the picker only supplies lines.
func (p sessionsPicker) View() string {
	w, h := accounts.FrameSize(p.w, p.h)
	if w == 0 || h == 0 {
		return ""
	}
	p.reveal()
	lines := p.compose()
	iw := accounts.ContentWidth(w)
	for i := range lines {
		lines[i] = fit(" "+fit(lines[i], iw), w)
	}
	return strings.Join(lines, "\n")
}

func (p sessionsPicker) compose() []string {
	th := theme.Current
	dim := lipgloss.NewStyle().Foreground(th.Dim)
	g := p.geom()
	iw := g.iw
	rule := dim.Render(strings.Repeat("─", iw))
	vis := p.visible()

	var out []string
	if g.title {
		title := lipgloss.NewStyle().Bold(true).Foreground(th.Text).Render("Sessions")
		out = append(out, leftRight(title, dim.Render(p.branch), iw))
	}
	if g.subtitle {
		count := fmt.Sprintf("%d sessions", len(p.sessions))
		switch {
		case p.query.Value() != "":
			count = fmt.Sprintf("%d of %d sessions", len(vis), len(p.sessions))
		case len(p.sessions) == 1:
			count = "1 session"
		}
		out = append(out, leftRight(dim.Render("All sessions in this worktree"), dim.Render(count), iw))
	}
	if g.rule {
		out = append(out, rule)
	}
	out = append(out, p.searchRow(iw))
	if g.gap {
		out = append(out, "")
	}

	var body []string
	if g.wide {
		left := p.listLines(g, g.lw, g.body)
		right := p.inspector(g.rw, g.body)
		div := lipgloss.NewStyle().Foreground(th.Border).Render(" │ ")
		for i := range g.body {
			body = append(body, fit(left[i], g.lw)+div+right[i])
		}
	} else {
		body = p.listLines(g, g.lw, g.listRows)
		if g.detailRows > 0 {
			body = append(body, p.stackedDetails(g.iw, g.detailRows)...)
		}
		for len(body) < g.body {
			body = append(body, "")
		}
	}
	out = append(out, body[:min(len(body), g.body)]...)

	if g.botRule {
		out = append(out, p.bottomRule(g, len(vis), dim))
	}
	out = append(out, dim.Render(p.footerFit(iw)))
	return out
}

func (p sessionsPicker) searchRow(iw int) string {
	th := theme.Current
	if !p.search {
		return lipgloss.NewStyle().Foreground(th.Dim).Render("/ Search sessions…")
	}
	q := p.query
	q.Width = max(iw-4, 1)
	return lipgloss.NewStyle().Foreground(th.Accent).Bold(true).Render("/ ") + q.View()
}

// bottomRule closes the list; when the list scrolls it says which items show.
func (p sessionsPicker) bottomRule(g sessionsGeom, n int, dim lipgloss.Style) string {
	if n <= g.visible {
		return dim.Render(strings.Repeat("─", g.iw))
	}
	label := fmt.Sprintf(" %d-%d of %d ", p.top+1, min(p.top+g.visible, n), n)
	return dim.Render(strings.Repeat("─", max(g.iw-2-len(label), 0)) + label + "──")
}

func (p sessionsPicker) footer() string {
	arrows := "↑↓"
	if p.ascii {
		arrows = "Up/Down"
	}
	if p.search {
		return arrows + " Select  Enter Open  Esc Clear search"
	}
	return arrows + " Select  / Search  n New session  Esc Back"
}

// footerFit shortens words, not keys, until the hints fit w.
func (p sessionsPicker) footerFit(w int) string {
	s := p.footer()
	for _, r := range [][2]string{{"New session", "New"}, {"Clear search", "Clear"}, {"  ", " "}} {
		if ansi.StringWidth(s) <= w {
			break
		}
		s = strings.ReplaceAll(s, r[0], r[1])
	}
	return ansi.Truncate(s, w, "…")
}

// listLines draws exactly rows lines: the column heading and a window of items.
func (p sessionsPicker) listLines(g sessionsGeom, width, rows int) []string {
	th := theme.Current
	dim := lipgloss.NewStyle().Foreground(th.Dim)
	var out []string
	if g.colHdr {
		nameW := max(width-2-2-sessionsStatusW, 0)
		hdr := "  " + fit("SESSION", nameW) + "  " + "STATUS"
		if nameW < 8 {
			hdr = "  SESSION"
		}
		out = append(out, dim.Render(fit(hdr, width)))
	}
	vis := p.visible()
	if len(vis) == 0 {
		msg := []string{"No sessions in this worktree yet.", "Press n to start one."}
		if len(p.sessions) > 0 {
			msg = []string{`No sessions match "` + ansi.Truncate(p.query.Value(), max(width-18, 4), "…") + `".`}
		}
		for _, l := range msg {
			out = append(out, "  "+dim.Render(l))
		}
	}
	end := min(p.top+g.visible, len(vis))
	for i := p.top; i < end; i++ {
		out = append(out, p.item(vis[i], i == p.cursor, width)...)
		if g.spacers && i < end-1 {
			out = append(out, "")
		}
	}
	for len(out) < rows {
		out = append(out, "")
	}
	return out[:rows]
}

// item is one session's two lines: name and status, then provider and account.
func (p sessionsPicker) item(s agentsapi.Session, selected bool, width int) []string {
	th := theme.Current
	accent := lipgloss.NewStyle().Foreground(th.Accent).Bold(true)
	marker := "  "
	nameSt := lipgloss.NewStyle().Foreground(th.Text)
	if selected {
		marker = lipgloss.NewStyle().Foreground(th.Accent).Render("▌") + " "
		nameSt = accent
	}
	status := p.status(s, true)
	sub := dimText(providerName(s.Provider) + " · " + s.ProfileName)
	nameW := width - 2 - 2 - sessionsStatusW
	if nameW >= 8 {
		l1 := marker + nameSt.Render(fit(p.label(s, nameW), nameW)) + "  " + status
		return []string{l1, "  " + sub}
	}
	l1 := marker + nameSt.Render(p.label(s, max(width-2, 1)))
	return []string{l1, "  " + leftRight(sub, status, max(width-2, 1))}
}

// status is the status mark in its color followed by the status label. The
// list column is ten cells wide, so a waiting session reads "Waiting" there;
// the inspector keeps the full Display label with its reason.
func (p sessionsPicker) status(s agentsapi.Session, short bool) string {
	key, label := sessionStatus(s)
	if short && key == "waiting" {
		label = "Waiting"
	}
	return stateMarkStyled(key) + " " + lipgloss.NewStyle().Foreground(theme.Current.Text).Render(label)
}

// --- inspector ---

type sessionField struct{ label, value string }

// fields are the safe, already-recorded facts about a session; nothing here
// is a command, a prompt, a token or terminal output.
func (p sessionsPicker) fields(s agentsapi.Session) []sessionField {
	acct := s.ProfileName
	if acct == "" {
		acct = string(s.AccountID)
	}
	out := []sessionField{{"Account", acct}, {"Worktree", p.branch}}
	if s.Model != "" {
		out = append(out, sessionField{"Model", s.Model})
	}
	if s.ExitCode != nil {
		out = append(out, sessionField{"Exit code", fmt.Sprint(*s.ExitCode)})
	}
	if s.Error != "" {
		out = append(out, sessionField{"Error", sanitizeAgentText(s.Error)})
	}
	id := s.ID
	if len(id) > sessionsIDCells {
		id = "…" + id[len(id)-sessionsIDCells:]
	}
	return append(out, sessionField{"ID", id})
}

func (p sessionsPicker) fieldLine(f sessionField, width int) string {
	const labelW = 9
	return dimText(fit(f.label, labelW)) + ansi.Truncate(f.value, max(width-labelW, 1), "…")
}

func (p sessionsPicker) button(width int) string {
	b := lipgloss.NewStyle().Foreground(theme.Current.Accent).Bold(true).Render("[ Enter Open session ]")
	return ansi.Truncate(b, width, "…")
}

func (p sessionsPicker) selected() (agentsapi.Session, bool) {
	vis := p.visible()
	if p.cursor < 0 || p.cursor >= len(vis) {
		return agentsapi.Session{}, false
	}
	return vis[p.cursor], true
}

// inspector is the right column: exactly rows lines of width cells.
func (p sessionsPicker) inspector(width, rows int) []string {
	out := make([]string, rows)
	s, ok := p.selected()
	if !ok {
		return out
	}
	title := lipgloss.NewStyle().Bold(true).Foreground(theme.Current.Text).Render(p.label(s, width))
	heading := theme.ProviderStyle(s.Provider).Render(theme.ProviderGlyph(s.Provider, p.ascii) + " " + strings.ToUpper(providerName(s.Provider)))
	fields := p.fields(s)

	// Blank separators and the heading are the first things to go in a short pane.
	build := func(blanks, head bool) []string {
		var l []string
		if head {
			l = append(l, heading)
		}
		l = append(l, title)
		if blanks {
			l = append(l, "")
		}
		l = append(l, p.status(s, false))
		if blanks {
			l = append(l, "")
		}
		for _, f := range fields {
			l = append(l, p.fieldLine(f, width))
		}
		return l
	}
	var lines []string
	for _, opt := range [][2]bool{{true, true}, {false, true}, {false, false}} {
		lines = build(opt[0], opt[1])
		if len(lines)+2 <= rows { // content, a blank, the button
			break
		}
	}
	copy(out, lines[:min(len(lines), max(rows-1, 0))])
	if rows >= 1 {
		out[rows-1] = p.button(width)
	}
	return out
}

// stackedDetails is the narrow layout's details: a separator naming the
// provider, then as many lines as fit, most useful first.
func (p sessionsPicker) stackedDetails(width, rows int) []string {
	s, ok := p.selected()
	if !ok || rows < 1 {
		return nil
	}
	th := theme.Current
	dim := lipgloss.NewStyle().Foreground(th.Dim)
	name := theme.ProviderGlyph(s.Provider, p.ascii) + " " + providerName(s.Provider)
	label := " " + name + " "
	sep := dim.Render("──") + theme.ProviderStyle(s.Provider).Render(label) +
		dim.Render(strings.Repeat("─", max(width-2-ansi.StringWidth(label), 0)))

	fields := p.fields(s)
	type entry struct {
		prio int
		text string
	}
	list := []entry{
		{0, lipgloss.NewStyle().Bold(true).Foreground(th.Text).Render(p.label(s, width))},
		{1, p.status(s, false)},
		{2, p.fieldLine(fields[0], width)}, // Account
		{3, p.fieldLine(fields[1], width)}, // Worktree
	}
	for i, f := range fields[2:] {
		list = append(list, entry{5 + i, p.fieldLine(f, width)})
	}
	list = append(list, entry{4, p.button(width)})
	keep := max(rows-1, 0)
	out := []string{sep}
	for _, e := range list {
		if e.prio < keep {
			out = append(out, e.text)
		}
	}
	return out
}
