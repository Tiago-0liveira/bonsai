package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/core/agents"
	"github.com/Tiago-0liveira/bonsai/internal/core/agents/usagefmt"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/accounts"
	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
)

const (
	// pgWideMin is the width from which the list gets a detail pane beside it;
	// narrower terminals show the list alone with a two-line detail strip.
	pgWideMin = 100
	// pgHeadRows are the title row and the feedback row above the list.
	pgHeadRows = 2
	// pgStripRows are the compact layout's detail rows under the list.
	pgStripRows = 2
)

// pgUnit is one entry of the displayed list: a group header or a session row.
type pgUnit struct {
	header bool
	group  string
	id     string
}

func (m Model) pgWide() bool { return m.width >= pgWideMin }

// pgRowH is the rows one session takes: title, provider line and branch, then a
// gap in the wide layout; title and provider line only in the compact one.
func (m Model) pgRowH() int {
	if m.pgWide() {
		return 4
	}
	return 2
}

// pgBodyRows is how many rows the list itself may use.
func (m Model) pgBodyRows() int {
	n := m.height - shellHeaderRows - shellFooterRows - settingsSubmenuRows - pgHeadRows
	if !m.pgWide() {
		n -= pgStripRows
	}
	return max(n, 1)
}

func (m Model) pgUnits(rows []pgRow) []pgUnit {
	var out []pgUnit
	last, started := "", false
	for _, r := range rows {
		if m.pg.group && (!started || r.Group != last) {
			out = append(out, pgUnit{header: true, group: r.Group})
			last, started = r.Group, true
		}
		out = append(out, pgUnit{id: r.ID})
	}
	return out
}

// pgTop keeps the selected session inside the window of avail rows while moving
// the window as little as possible.
func pgTop(units []pgUnit, rowH, top int, sel string, avail int) int {
	si := -1
	for i, u := range units {
		if !u.header && u.id == sel {
			si = i
		}
	}
	if si < 0 {
		return 0
	}
	h := func(u pgUnit) int {
		if u.header {
			return 1
		}
		return rowH
	}
	top = max(0, min(top, si))
	if top == si && si > 0 && units[si-1].header && h(units[si-1])+h(units[si]) <= avail {
		top--
	}
	for top < si {
		used := 0
		for _, u := range units[top : si+1] {
			used += h(u)
		}
		if used <= avail {
			break
		}
		top++
	}
	return top
}

func (m Model) pgScroll(rows []pgRow) int {
	return pgTop(m.pgUnits(rows), m.pgRowH(), m.pg.top, m.pg.sel, m.pgBodyRows())
}

func pgFactColor(key string) lipgloss.Color {
	p := theme.Current
	switch key {
	case "waiting", "stalled":
		return p.Warning
	case "working":
		return p.Success
	case "failed":
		return p.Danger
	}
	return p.Muted
}

func (m Model) pgIcon() string {
	if m.agentGlyphsASCII() {
		return "#"
	}
	return "▣"
}

// playgroundSubmenu is the breadcrumb, the submenu and a rule.
func (m Model) playgroundSubmenu(width int) []string {
	p := theme.Current
	sep := seg(p.Muted, p.Surface, false).Render(" › ")
	crumb := seg(p.Muted, p.Surface, false).Render(" Playground") + sep +
		seg(p.Muted, p.Surface, false).Render(pgSubNames[m.pg.sub]) + sep
	if m.pg.rev.open {
		crumb += seg(p.Text, p.Surface, true).Render("Human review")
	} else if m.pg.chg.open {
		crumb += seg(p.Muted, p.Surface, false).Render(m.pgChgCrumb()) + sep + seg(p.Text, p.Surface, true).Render("Changed files")
	} else {
		crumb += seg(p.Text, p.Surface, true).Render(m.pgThird())
	}
	attn := pgAttentionCount(m.pg.recs, m.agents.ack)
	var tabs strings.Builder
	tabs.WriteString(seg("", p.Surface, false).Render(" "))
	for s := pgSub(0); s < pgSubCount; s++ {
		label := " " + pgSubNames[s] + " "
		switch s {
		case pgSubSessions:
			label = " " + m.pgIcon() + label
		case pgSubAttention:
			label = " ! " + pgSubNames[s] + " "
			if attn > 0 {
				label = " ! " + pgSubNames[s] + fmt.Sprintf(" %d ", attn)
			}
		}
		switch {
		case s == m.pg.sub:
			tabs.WriteString(seg(p.Background, p.Accent, true).Render(label))
		case s == pgSubAttention && attn > 0:
			tabs.WriteString(seg(p.Warning, p.Surface, true).Render(label))
		default:
			tabs.WriteString(seg(p.Muted, p.Surface, false).Render(label))
		}
		tabs.WriteString(seg("", p.Surface, false).Render(" "))
	}
	rule := seg(p.Border, p.Background, false).Render(strings.Repeat("─", width))
	return m.settingsASCII([]string{
		shellRow(width, p.Surface, crumb, ""),
		shellRow(width, p.Surface, tabs.String(), ""),
		rule,
	})
}

// pgThird is the last breadcrumb: the selected session's state, else the filter.
func (m Model) pgThird() string {
	if m.pg.open && m.pg.sub != pgSubUsage {
		if m.pgTerminalMode() {
			return "Control"
		}
		return "Observe"
	}
	if m.pg.sub != pgSubUsage {
		if r, ok := m.playgroundSelected(); ok {
			return r.Fact.Label
		}
	}
	switch m.pg.sub {
	case pgSubAttention:
		return "Needs attention"
	case pgSubHistory:
		return "Ended and failed"
	case pgSubUsage:
		return "Account capacity"
	}
	if m.pg.all {
		return "All sessions"
	}
	return "Active"
}

// playgroundBody draws exactly rows lines of exactly width cells.
func (m Model) playgroundBody(width, rows int) []string {
	lines := m.pgLines(width, rows)
	bg := blankRow(width, theme.Current.Background)
	for i := range lines {
		if ansi.StringWidth(lines[i]) > width {
			lines[i] = ansi.Truncate(lines[i], width, "")
		}
	}
	if len(lines) > rows {
		lines = lines[:rows]
	}
	for len(lines) < rows {
		lines = append(lines, bg)
	}
	return m.settingsASCII(lines)
}

func (m Model) pgLines(width, rows int) []string {
	p := theme.Current
	if !m.agents.active() {
		return []string{
			gHead(width, m.pgIcon()+" SESSIONS", ""),
			gText(width, p.Muted, false, "Agent sessions are not available on this host."),
		}
	}
	if !m.pg.loaded {
		return []string{gHead(width, m.pgIcon()+" SESSIONS", ""), gText(width, p.Muted, false, "Reading sessions…")}
	}
	if m.pg.sub == pgSubUsage {
		return m.pgUsageLines(width, rows)
	}
	if m.pg.rev.open {
		return m.pgReviewLines(width, rows)
	}
	if m.pg.chg.open {
		return m.pgChangesLines(width, rows)
	}
	if m.pg.open {
		return m.pgTerminalLines(width, rows)
	}
	list := m.playgroundRows()
	head := []string{m.pgHead(width, len(list)), m.pgFeedback(width)}
	avail := rows - pgHeadRows
	if !m.pgWide() {
		avail -= pgStripRows
	}
	if avail < 1 {
		return head
	}
	left := width
	if m.pgWide() {
		left = pgListWidth(width)
	}
	body := m.pgListLines(left, avail, list)
	if !m.pgWide() {
		return append(append(head, body...), m.pgStrip(width)...)
	}
	right := m.pgDetail(width-left-1, avail)
	div := seg(p.Border, p.Background, false).Render("│")
	out := head
	for i := 0; i < avail; i++ {
		out = append(out, body[i]+div+right[i])
	}
	return out
}

func pgListWidth(width int) int { return max(44, min(width*2/5, 64)) }

func (m Model) pgHead(width, n int) string {
	p := theme.Current
	title := "ACTIVE SESSIONS"
	switch m.pg.sub {
	case pgSubSessions:
		if m.pg.all {
			title = "ALL SESSIONS"
		}
	case pgSubAttention:
		title = "NEEDS ATTENTION"
	case pgSubHistory:
		title = "HISTORY"
	}
	left := seg(p.Text, p.Background, true).Render(" " + m.pgIcon() + " " + title + fmt.Sprintf(" %d", n))
	right := ""
	if a := pgAttentionCount(m.pg.recs, m.agents.ack); a > 0 {
		right = seg(p.Warning, p.Raised, false).Render(fmt.Sprintf(" ! Needs attention %d ", a)) + seg("", p.Background, false).Render(" ")
	}
	return shellRow(width, p.Background, left, right)
}

func (m Model) pgFeedback(width int) string {
	p := theme.Current
	pg := m.pg
	switch {
	case pg.searching:
		s := pg.search
		s.Width = max(1, width-30)
		return boardLine(width, p.Background, p.Text, false, " Search: "+s.View()+"  Enter apply / Esc cancel")
	case pg.stale:
		return boardLine(width, p.Background, p.Warning, false, " Session list stale ("+pg.errMsg+"); showing the last good list.")
	case pg.query != "":
		return boardLine(width, p.Background, p.Muted, false, " Search: "+pgClean(pg.query, 80))
	case pg.group:
		return boardLine(width, p.Background, p.Muted, false, " Grouped by Issue; every session keeps its own state.")
	}
	return boardLine(width, p.Background, p.Muted, false, " This repository, on this machine.")
}

// pgListLines draws the visible window of the list, padded to avail rows.
func (m Model) pgListLines(w, avail int, rows []pgRow) []string {
	p := theme.Current
	gap := blankRow(w, p.Background)
	var out []string
	if len(rows) == 0 {
		msg := "No sessions for this repository yet."
		switch {
		case m.pg.query != "":
			msg = "No session matches the search."
		case len(m.pg.recs) > 0 && m.pg.sub == pgSubAttention:
			msg = "Nothing needs attention."
		case len(m.pg.recs) > 0 && m.pg.sub == pgSubHistory:
			msg = "No ended sessions."
		case len(m.pg.recs) > 0:
			msg = "No active sessions; press a to include ended ones."
		}
		out = append(out, gText(w, p.Muted, false, msg))
	}
	units := m.pgUnits(rows)
	index := map[string]int{}
	for i, r := range rows {
		index[r.ID] = i
	}
	top := pgTop(units, m.pgRowH(), m.pg.top, m.pg.sel, avail)
	for _, u := range units[min(top, len(units)):] {
		if len(out) >= avail {
			break
		}
		if u.header {
			title, n := pgGroupTitle(rows, u.group)
			if u.group == "" {
				title = "Unlinked sessions"
			}
			mark := "▾ "
			if m.agentGlyphsASCII() {
				mark = "v "
			}
			out = append(out, gText(w, p.Muted, true, fmt.Sprintf("%s%s · %d", mark, title, n)))
			continue
		}
		r := rows[index[u.id]]
		lines := m.pgRowLines(w, r, r.ID == m.pg.sel)
		if len(out)+len(lines) > avail {
			break // never draw half a session
		}
		out = append(out, lines...)
		if m.pgWide() {
			out = append(out, gap)
		}
	}
	for len(out) < avail {
		out = append(out, gap)
	}
	return out[:avail]
}

func pgBranchLine(r pgRow) string {
	switch {
	case !r.Rec.InTree:
		return "no worktree"
	case r.Rec.Branch == "":
		return "detached HEAD"
	}
	return pgClean(r.Rec.Branch, 120)
}

func (m Model) pgRowLines(w int, r pgRow, selected bool) []string {
	p := theme.Current
	bg, bar := p.Surface, " "
	if selected {
		bg, bar = p.Raised, "▌"
	}
	mark := " "
	if r.Fact.Attention {
		mark = "!"
	}
	gutter := func(first, second string, fg lipgloss.Color) string {
		return seg(p.Accent, bg, true).Render(first) + seg(fg, bg, true).Render(second)
	}
	line := func(g, text string, fg lipgloss.Color, bold bool) string {
		return shellRow(w, bg, g+seg(fg, bg, bold).Render(ansi.Truncate(text, max(w-3, 1), "…")), "")
	}
	blank := gutter(" ", " ", p.Muted)
	detail := providerName(r.Rec.Session.Provider)
	if r.Account != "" {
		detail += " / " + r.Account
	}
	detail += " / " + r.Fact.Label
	out := []string{
		line(gutter(bar, mark, p.Warning)+seg("", bg, false).Render(" "), r.Title, p.Text, selected),
		line(blank+seg("", bg, false).Render(" "), detail, pgFactColor(r.Fact.Key), false),
	}
	if m.pgWide() {
		out = append(out, line(blank+seg("", bg, false).Render(" "), pgBranchLine(r), p.Dim, false))
	}
	return out
}

// pgCapacity is the account's stored remaining capacity. It is capacity, never the progress of a task, and says so.
func (m Model) pgCapacity(id agents.AccountID) (string, lipgloss.Color) {
	p := theme.Current
	e, ok := m.agents.usage[id]
	if !ok || !e.Have {
		if ok && e.Err != "" {
			return "Account capacity: unavailable (" + e.Err + ")", p.Muted
		}
		return "Account capacity: no stored snapshot", p.Muted
	}
	h := e.Usage.Headline
	limit := h.Primary
	if _, has := agents.RemainingFraction(limit); !has {
		limit = h.Secondary
	}
	frac, has := agents.RemainingFraction(limit)
	if !has {
		return "Account capacity: not reported", p.Muted
	}
	text := fmt.Sprintf("Account capacity %s %s", usagefmt.WindowName(limit, "usage"), accounts.RemainingCapacity(frac, 10, m.agentGlyphsASCII()))
	fg := p.Muted
	if e.Stale() {
		text += " (stale)"
	}
	return text, fg
}

// pgStrip is the compact layout's detail: the branch and the account capacity
// of the selected session.
func (m Model) pgStrip(width int) []string {
	p := theme.Current
	r, ok := m.playgroundSelected()
	if !ok {
		return []string{blankRow(width, p.Background), blankRow(width, p.Background)}
	}
	text, fg := m.pgCapacity(r.Rec.Session.AccountID)
	return []string{
		gText(width, p.Dim, false, pgBranchLine(r)),
		gText(width, fg, false, text),
	}
}

// pgAge is how long ago a time was, for "started/ended" facts.
func pgAge(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return usagefmt.Duration(max(agentNow().Sub(t), 0), false) + " ago"
}

// pgDetail is the wide layout's detail pane for the selected session: its own
// facts, its account's capacity and the places it links to.
func (m Model) pgDetail(w, rows int) []string {
	p := theme.Current
	blank := blankRow(w, p.Background)
	r, ok := m.playgroundSelected()
	var out []string
	if !ok {
		out = []string{gText(w, p.Muted, false, "No session selected.")}
	} else {
		s := r.Rec.Session
		out = append(out, gText(w, p.Text, true, r.Title), blank)
		out = append(out, gWrap(w, pgFactColor(r.Fact.Key), true, r.Fact.Label)...)
		out = append(out, blank)
		pfg, isColor := theme.ProviderStyle(s.Provider).GetForeground().(lipgloss.Color)
		if !isColor {
			pfg = p.Muted
		}
		out = append(out, gText(w, pfg, true, providerName(s.Provider)+" / "+r.Account))
		text, fg := m.pgCapacity(s.AccountID)
		out = append(out, gText(w, fg, false, text), gText(w, p.Dim, false, "Capacity of the account, not progress of this task."), blank)
		out = append(out, gText(w, p.Text, false, "Branch: "+pgBranchLine(r)))
		when := "Created " + pgAge(s.CreatedAt)
		if s.EndedAt != nil {
			when = "Ended " + pgAge(*s.EndedAt)
		} else if s.StartedAt != nil {
			when = "Started " + pgAge(*s.StartedAt)
		}
		out = append(out, gText(w, p.Muted, false, when), blank)
		out = append(out, m.pgLinks(w, r)...)
	}
	for len(out) < rows {
		out = append(out, blank)
	}
	return out[:rows]
}

// pgLinks lists where the session leads. The Issue and the account open through
// existing navigation; the plan and the attempt are shown as information only.
func (m Model) pgLinks(w int, r pgRow) []string {
	p := theme.Current
	s := r.Rec.Session
	var out []string
	switch {
	case r.Task && m.garden.board.loaded:
		out = append(out, gText(w, p.Accent, false, fmt.Sprintf("i  Issue #%d (open in Garden)", r.Number)))
	case r.Linked:
		out = append(out, gText(w, p.Muted, false, "Issue: not in the loaded Garden board"))
	default:
		out = append(out, gText(w, p.Muted, false, "No Garden task (a neutral fact, not a failure)"))
	}
	switch {
	case r.Manual:
		// A manual link is display only: it never carries a plan, an approval or an attempt.
		out = append(out, gText(w, p.Muted, false, "Linked manually: no approved plan"))
	case r.Linked:
		out = append(out, gText(w, p.Muted, false, "Plan: approved plan is on the Issue"))
	}
	if r.Linked && !r.Manual && s.Task != nil && s.Task.AttemptID != "" {
		out = append(out, gText(w, p.Muted, false, "Attempt: "+pgClean(s.Task.AttemptID, 12)+" (see the Issue in Garden)"))
	}
	if m.pg.api != nil {
		out = append(out, gText(w, p.Accent, false, "c  Changes "+m.pgG("›", ">")+" (files, PR, CI)"))
	}
	if m.pg.links.stale() {
		out = append(out, gText(w, p.Warning, false, "stale: manual links could not be read ("+pgReasonText(m.pg.links.err.Code)+")"))
	}
	if m.acct.present {
		out = append(out, gText(w, p.Accent, false, "o  Account: "+providerName(s.Provider)+" / "+r.Account))
	}
	return out
}

// pgUsageLines is the Usage submenu: the stored capacity of every account that
// has a session here. Nothing on this screen reads a provider.
func (m Model) pgUsageLines(width, rows int) []string {
	p := theme.Current
	out := []string{
		gHead(width, m.pgIcon()+" ACCOUNT CAPACITY", "capacity, not task progress"),
		boardLine(width, p.Background, p.Muted, false, " Stored snapshots only; refresh from Accounts."),
	}
	var ids []agents.AccountID
	active, ended := map[agents.AccountID]int{}, map[agents.AccountID]int{}
	first := map[agents.AccountID]pgRec{}
	for _, r := range m.pg.recs {
		id := r.Session.AccountID
		if _, ok := first[id]; !ok {
			ids = append(ids, id)
			first[id] = r
		}
		if r.Session.Active() {
			active[id]++
		} else {
			ended[id]++
		}
	}
	if len(ids) == 0 {
		return append(out, gText(width, p.Muted, false, "No accounts have sessions in this repository."))
	}
	for n, id := range ids {
		r := first[id].Session
		block := m.pgUsageBlock(width, r, active[id], ended[id])
		if len(out)+len(block) > rows {
			if len(out) < rows {
				out = append(out, gText(width, p.Muted, false, fmt.Sprintf("… %d more accounts", len(ids)-n)))
			}
			break
		}
		out = append(out, block...)
	}
	return out
}

func (m Model) pgUsageBlock(width int, s agentsapi.Session, active, ended int) []string {
	p := theme.Current
	name := pgAccount(s)
	title := providerName(s.Provider) + " / " + name
	out := []string{shellRow(width, p.Background,
		seg(p.Text, p.Background, true).Render(" "+ansi.Truncate(title, max(width/2, 10), "…"))+
			seg(p.Muted, p.Background, false).Render(fmt.Sprintf("  %d active · %d ended", active, ended)), "")}
	e, ok := m.agents.usage[s.AccountID]
	switch {
	case !ok || !e.Have:
		msg := "no stored snapshot yet"
		if ok && e.Err != "" {
			msg = "unavailable: " + e.Err
		}
		return append(out, gText(width, p.Muted, false, "  "+msg), blankRow(width, p.Background))
	}
	h := e.Usage.Headline
	now := agentNow()
	for _, limit := range []*agents.UsageLimit{h.Primary, h.Secondary} {
		frac, has := agents.RemainingFraction(limit)
		if !has {
			continue
		}
		line := seg(p.Dim, p.Background, false).Render(fmt.Sprintf("  %-5s ", usagefmt.WindowName(limit, "usage"))) + accounts.RemainingCapacity(frac, 10, m.agentGlyphsASCII())
		if limit.ResetsAt != nil {
			line += seg(p.Dim, p.Background, false).Render(" · Reset ") + seg(p.Accent, p.Background, true).Render(usagefmt.ResetsIn(now, limit.ResetsAt, true))
		}
		out = append(out, shellRow(width, p.Background, line, ""))
	}
	if len(out) == 1 {
		out = append(out, gText(width, p.Muted, false, "  capacity not reported"))
	}
	if e.Stale() {
		out = append(out, gText(width, p.Warning, false, "  last refresh failed; showing the last good snapshot"))
	}
	if !e.Usage.FetchedAt.IsZero() {
		out = append(out, gText(width, p.Dim, false, "  read "+pgAge(e.Usage.FetchedAt)))
	}
	return append(out, blankRow(width, p.Background))
}

// footer and chrome hooks ------------------------------------------------------

func (m Model) playgroundWhere() string {
	if m.pg.sub != pgSubUsage {
		if r, ok := m.playgroundSelected(); ok {
			if m.pg.chg.open {
				return r.Title + " / CHANGES"
			}
			if m.pg.open {
				return r.Title + " / " + strings.ToUpper(m.pgThird())
			}
			return r.Title + " / " + r.Fact.Label
		}
	}
	return "Playground / " + pgSubNames[m.pg.sub]
}

func (m Model) playgroundFocusLabel() string {
	switch {
	case m.pg.rev.open:
		return "Human review"
	case m.pg.chg.open && m.pg.chg.diffOpen:
		return "File diff"
	case m.pg.chg.open:
		return "Changed files"
	case m.pgTerminalMode():
		return "Agent input"
	case m.pg.open:
		return "Terminal / Observe"
	case m.pg.searching:
		return "Session search"
	case m.pg.sub == pgSubUsage:
		return "Account capacity"
	}
	return "Session list"
}

func (m Model) playgroundStatusLine() string {
	switch {
	case !m.agents.active():
		return "Sessions unavailable"
	case !m.pg.loaded:
		return "Reading sessions…"
	case m.pg.rev.open:
		return "Human review / local machine"
	case m.pg.chg.open:
		return m.pgChangesStatus()
	case m.pg.stale:
		return "Sessions stale / local machine"
	}
	return "Sessions live / local machine"
}

func (m Model) playgroundHints() []shellHint {
	p := m.pg
	if p.searching {
		return []shellHint{{"enter", "apply search"}, {"esc", "cancel"}}
	}
	if p.rev.open {
		return m.pgReviewHints()
	}
	if p.chg.open {
		return m.pgChangesHints()
	}
	if p.open {
		return m.pgTermHints()
	}
	var h []shellHint
	if p.sub != pgSubUsage {
		h = append(h, shellHint{"↑/↓", "session"})
		if r, ok := m.playgroundSelected(); ok && m.agents.active() {
			switch s := r.Rec.Session; {
			case pgEnded(s):
				h = append(h, shellHint{"v", "retained output"})
			case pgAttachable(s):
				h = append(h, shellHint{"enter", "control"}, shellHint{"v", "observe"})
			}
		}
		h = append(h, shellHint{"r", "review"}, shellHint{"c", "changes"}, shellHint{"/", "search"})
	}
	if p.sub == pgSubSessions {
		if p.all {
			h = append(h, shellHint{"a", "active only"})
		} else {
			h = append(h, shellHint{"a", "all sessions"})
		}
	}
	if p.sub != pgSubUsage {
		if p.group {
			h = append(h, shellHint{"g", "ungroup"})
		} else {
			h = append(h, shellHint{"g", "group by Issue"})
		}
	}
	h = append(h, shellHint{m.keys.Tab.Help().Key, "submenu"})
	if r, ok := m.playgroundSelected(); ok && p.sub != pgSubUsage {
		if r.Task && m.garden.board.loaded {
			h = append(h, shellHint{"i", "issue"})
		}
		if r.Rec.Session.State == "failed" && !m.agents.ack[r.ID] {
			h = append(h, shellHint{"x", "mark seen"})
		}
	}
	if m.acct.present {
		h = append(h, shellHint{"o", "account"})
	}
	return append(h, shellHint{"esc", "workspaces"})
}
