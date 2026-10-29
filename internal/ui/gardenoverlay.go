package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/planrev"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/work"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/overlay"
	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
)

const (
	// overlayRailMin is the content width from which the side summary fits next to a readable body.
	overlayRailMin = 96
	overlayRailW   = 26
	// overlayChrome is the rows around the body: border, identity, state, tabs, actions, reason, border.
	overlayChrome = 7
	// overlayMinW and overlayMinH are the panel sizes below which only the close action is drawn.
	overlayMinW, overlayMinH = 20, overlayChrome + 2
	overlayWheelLines        = 3
)

// gardenOverlayOpen reports whether the opened Issue shows as the overlay. The planner, plan review and launch
// screens of the same Issue are bodies of their own and take over from it.
func (m Model) gardenOverlayOpen() bool {
	b := m.garden.board
	return m.layoutKind == LayoutRedesign && m.workspace == wsGarden && m.garden.api != nil && m.modal == nil &&
		m.gardenScreen() == scrBoard && b.opened != 0 && !b.searching &&
		(!m.planActive() || m.planDecideInOverlay()) && !m.launchActive() && !m.plannerActive()
}

// planDecideInOverlay reports the approval confirmation the Plan tab draws inside the overlay.
func (m Model) planDecideInOverlay() bool {
	return m.garden.planrev.fromOverlay && m.garden.planrev.stage == planDecide
}

// overlayGeometry is the single place that sizes the panel's inner regions, so what scrolls is what is drawn.
func overlayGeometry(screenW, screenH int) (x, y, w, h, cw, bodyW, bodyH int, rail bool) {
	x, y, w, h = overlay.Center(screenW, screenH)
	cw = max(w-4, 0)
	rail = cw >= overlayRailMin
	bodyW = cw
	if rail {
		bodyW -= overlayRailW + 3
	}
	return x, y, w, h, cw, bodyW, max(h-overlayChrome, 0), rail
}

// gardenOverlayBody is the document's wrap width and visible rows inside the panel.
func (m Model) gardenOverlayBody() (width, rows int) {
	_, _, _, _, _, width, rows, _ = overlayGeometry(m.width, m.height)
	return width, rows
}

// issueControl is one pinned action. Keyboard shortcuts, Tab and clicks all resolve to its id.
type issueControl struct {
	id, key, long, short string
	enabled              bool
	reason               string
}

// issueControls lists the actions in focus order. An action whose backend is absent is not offered; one whose
// approval or freshness is not met stays visible, disabled, with the reason.
func (m Model) issueControls() []issueControl {
	g := m.garden
	idle := g.api != nil && !g.busy.writes()
	if sub, ok := m.planTabModeControls(); ok {
		return sub
	}
	if sub, ok := m.handoffControls(); ok {
		return sub
	}
	out := []issueControl{{id: "close", key: "esc", long: "Back to " + m.garden.board.layoutName(), short: m.garden.board.layoutName(), enabled: true}}
	if a, ok := m.launchOverviewAttempt(); ok && g.board.section == 0 {
		out = append(out, issueControl{id: "launch-session", key: "o", long: "Open session", short: "Session", enabled: idle})
		if a.State.Active() {
			out = append(out, issueControl{id: "launch-stop", key: "x", long: "Stop run…", short: "Stop…", enabled: idle})
		}
		return append(out, issueControl{id: "attempts", key: "i", long: "Attempts", short: "Attempts", enabled: idle})
	}
	if g.board.section == planTabSection {
		out = append(out, m.planTabActions(idle)...)
	} else {
		out = append(out, issueControl{id: "plan", key: "p", long: "Request plan", short: "Plan", enabled: idle},
			issueControl{id: "review", key: "r", long: "Review plans", short: "Review", enabled: idle})
	}
	if m.launchSupported() {
		start := issueControl{id: "start", key: "s", long: "Start implementation…", short: "Start…", enabled: idle && m.launchOffered()}
		if !m.launchOffered() {
			start.reason = launchNotReadyText
		}
		if g.board.section == 0 && m.launchOffered() {
			out = []issueControl{out[0], start, {id: "run-settings", key: "e", long: "Change run settings", short: "Settings", enabled: idle}, {id: "attempts", key: "i", long: "Attempts", short: "Attempts", enabled: idle}}
		} else {
			out = append(out, start, issueControl{id: "attempts", key: "i", long: "Attempts", short: "Attempts", enabled: idle})
		}
	}
	return out
}

// gardenOpenIssue is the one open action behind Enter and a click on a card or row.
func (m *Model) gardenOpenIssue(id garden.IssueID) tea.Cmd {
	if id != 0 {
		if m.garden.board.opened != id {
			prev := m.garden.launch
			m.garden.launch = gardenLaunchState{inflight: prev.inflight, previewSeq: prev.previewSeq + 1, readSeq: prev.readSeq + 1, stopGen: prev.stopGen + 1, overviewGen: prev.overviewGen + 1, run: -1}
		}
		m.garden.board.open(id, m.garden.status.Target.RepoID)
		m.handoffReset(uint64(id))
		next, sync := m.handoffSync()
		*m = next
		if !m.launchSupported() {
			return sync
		}
		next, read := m.launchLoad(uint64(id))
		*m = next
		return tea.Batch(sync, read, m.launchReadyLoad(), m.launchSubscribe())
	}
	return nil
}

// issueSetSection switches the tab. Leaving the Plan tab keeps the feedback typed there; entering it reads the
// revise preview, which starts nothing.
func (m *Model) issueSetSection(n int) tea.Cmd {
	b := &m.garden.board
	prev := b.section
	m.garden.launch.overviewGen++
	m.garden.launch.overviewPending = false
	b.section, b.docScroll = max(0, min(n, len(gardenDocumentSections)-1)), 0
	if prev == planTabSection && b.section != planTabSection {
		m.planFormClose()
		m.garden.plantab.confirmStop = false
		b.overlayFocus = ""
	}
	if b.section == planTabSection && prev != planTabSection {
		return m.planTabLoad()
	}
	next, sync := m.handoffSync()
	*m = next
	return tea.Batch(m.launchOverviewNext(), sync)
}

func (m *Model) issueScroll(delta int) {
	m.garden.board.docScroll = max(0, m.garden.board.docScroll+delta)
	m.gardenClampDocument()
}

// issueActivate runs an action through the same entry points as its shortcut. A shortcut that cannot run says
// why in the notice instead of staying silent.
func (m Model) issueActivate(id string) (tea.Model, tea.Cmd) {
	if strings.HasPrefix(id, "ho-") {
		return m.handoffActivate(id)
	}
	switch id {
	case "close":
		m.handoffReset(0)
		m.garden.launch.overviewGen++
		m.garden.launch.overviewPending = false
		m.garden.launch.stopGen++
		m.garden.launch.stopAttempt = ""
		m.garden.launchReady.gen++
		m.planFormClose()
		m.garden.plantab.confirmStop = false
		m.garden.board.close()
		return m, nil
	case "plan":
		return m.gardenOpenPlanner()
	case "review":
		return m.gardenOpenPlanReview()
	case "start":
		return m.gardenLaunchDefault()
	case "run-settings":
		return m.gardenLaunchOpen()
	case "launch-session", "launch-stop":
		if a, ok := m.launchOverviewAttempt(); ok {
			m.garden.launch.attempt, m.garden.launch.run, m.garden.launch.stage = a.ID, -1, launchStageAttempt
			if id == "launch-stop" {
				return m.launchOpenConfirm(launchActStop)
			}
			return m.launchOpenTerminal()
		}
	case "attempts":
		return m.gardenLaunchHub()
	case "open-planner":
		return m.planTabOpenPlanner()
	case "publish":
		return m.planTabPublish()
	case "stop":
		return m.planTabStop()
	case "stop-keep":
		return m.planTabKeyStop("n")
	case "stop-yes":
		return m.planTabKeyStop("y")
	case "approve":
		return m.planTabReview("approve")
	case "changes":
		return m.planTabReview("changes")
	case "retry":
		return m.planTabRetry()
	case "decide-cancel":
		m = m.planClose()
		return m, nil
	case "decide-approve":
		return m.gardenRunPlanDecision()
	case "form-back":
		m.planFormClose()
		return m, nil
	case "form-dirty":
		if f := m.planForm(); f != nil && f.base.Dirty {
			f.includeDirty = !f.includeDirty
		}
		return m, nil
	case "form-account":
		return m.planFormAccount()
	case "form-send":
		return m.planFormSend()
	}
	return m, nil
}

// issueFocusStep moves the focus through the enabled actions, wrapping, from the body into the first.
func (m Model) issueFocusStep(dir int) string {
	var ids []string
	for _, c := range m.issueControls() {
		if c.enabled {
			ids = append(ids, c.id)
		}
	}
	cur := -1
	for i, id := range ids {
		if id == m.garden.board.overlayFocus {
			cur = i
		}
	}
	switch {
	case len(ids) == 0:
		return ""
	case cur < 0 && dir < 0:
		return ids[len(ids)-1]
	}
	return ids[(cur+dir+len(ids))%len(ids)]
}

// gardenIssueKey owns every key while the Issue overlay is open: nothing reaches the board or the globals.
func (m Model) gardenIssueKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	b := &m.garden.board
	if m.planForm() != nil {
		return m.planFormKey(msg)
	}
	m.garden.notice = "" // a notice explains the last action; the next key moves on
	if m.handoffModal() {
		return m.handoffKey(msg)
	}
	m.garden.handoff.note = ""
	_, rows := m.gardenOverlayBody()
	page := max(1, rows-1)
	k := msg.String()
	if m.planStopAsked() {
		return m.planTabKeyStop(k)
	}
	switch {
	case k == "esc" && b.overlayFocus != "" && m.handoffShown():
		b.overlayFocus = "" // Esc first restores the selection, a second one closes
	case m.handoffShown() && handoffKeyIDs[k] != "":
		return m.issueActivate(handoffKeyIDs[k])
	case k == "esc", k == "backspace":
		return m.issueActivate("close")
	case key.Matches(msg, m.keys.Tab):
		b.overlayFocus = m.issueFocusStep(1)
	case key.Matches(msg, m.keys.ShiftTab):
		b.overlayFocus = m.issueFocusStep(-1)
	case k == "enter":
		if b.overlayFocus != "" {
			return m.issueActivate(b.overlayFocus)
		}
	case k == "p", k == "r":
		if b.section == planTabSection {
			return m.planTabShortcut(k)
		}
		return m.issueActivate(map[string]string{"p": "plan", "r": "review"}[k])
	case k == "s":
		return m.issueActivate("start")
	case k == "e":
		return m.issueActivate("run-settings")
	case k == "o", k == "x":
		if _, ok := m.launchOverviewAttempt(); ok {
			return m.issueActivate(map[string]string{"o": "launch-session", "x": "launch-stop"}[k])
		}
	case k == "i":
		return m.issueActivate("attempts")
	case k == "left", k == "h":
		return m, m.issueSetSection(b.section - 1)
	case k == "right", k == "l":
		return m, m.issueSetSection(b.section + 1)
	case b.section == planTabSection && (k == "a" || k == "b" || k == "c" || k == "u"):
		return m.planTabShortcut(k)
	case k == "down", k == "j":
		m.issueScroll(1)
	case k == "up", k == "k":
		m.issueScroll(-1)
	case k == "pgdown":
		m.issueScroll(page)
	case k == "pgup":
		m.issueScroll(-page)
	case k == "home":
		b.docScroll = 0
	case k == "end":
		b.docScroll = 1 << 20
		m.gardenClampDocument()
	}
	return m, nil
}

// --- mouse ---

// gardenMouse routes a cell click or wheel step. The open overlay takes all of it; otherwise a click on a card or
// row is the same open action as Enter.
func (m Model) gardenMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.mouseOff || m.modal != nil || m.garden.api == nil || m.garden.busy.writes() || msg.Action != tea.MouseActionPress {
		return m, nil
	}
	if m.gardenOverlayOpen() {
		return m.issueMouse(msg)
	}
	b := &m.garden.board
	if msg.Button != tea.MouseButtonLeft || m.gardenScreen() != scrBoard || b.opened != 0 || b.searching ||
		m.currentShellFocus() == shellInput || m.planActive() || m.launchActive() || m.plannerActive() {
		return m, nil
	}
	if b.layout != layoutBoard {
		if i, ok := m.gardenTableHit(msg.X, msg.Y); ok && b.tableView().lines[i].header {
			m.shellFocus = shellBody
			b.tableView().line = i
			b.toggleGroup()
			m.gardenBoardVisible()
			return m, nil
		}
	}
	id, ok := m.gardenListHit(msg.X, msg.Y)
	if !ok {
		return m, nil
	}
	m.shellFocus = shellBody
	b.selection.Focus(b.collection, id)
	if b.layout != layoutBoard {
		b.tableFocus()
	}
	m.gardenBoardVisible()
	cmd := m.gardenOpenIssue(id)
	return m, cmd
}

func (m Model) issueMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	x, y, w, h, _, _, _, _ := overlayGeometry(m.width, m.height)
	px, py := msg.X-x, msg.Y-y
	if px < 0 || py < 0 || px >= w || py >= h {
		return m, nil // outside the panel the board is dimmed and inert
	}
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		m.issueScroll(-overlayWheelLines)
	case tea.MouseButtonWheelDown:
		m.issueScroll(overlayWheelLines)
	case tea.MouseButtonLeft:
		_, hits := m.issueOverlayRows(w, h)
		for _, hit := range hits {
			if hit.y == py && px >= hit.x0 && px < hit.x1 {
				if hit.tab >= 0 {
					return m, m.issueSetSection(hit.tab)
				}
				m.garden.notice = ""
				return m.issueActivate(hit.action)
			}
		}
	}
	return m, nil
}

// gardenTableHit shares the renderer's body offset and line capacity, including groups.
func (m Model) gardenTableHit(x, y int) (int, bool) {
	b := m.garden.board
	v := b.tableView()
	line := y - shellHeaderRows - settingsSubmenuRows - 3
	capacity := gardenTableCapacity(m.gardenBoardRows())
	scroll := tableScrollFor(v.scroll, v.line, capacity, len(v.lines))
	i := scroll + line
	return i, x >= 1 && x < m.width && line >= 0 && line < capacity && i < len(v.lines)
}

// gardenListHit maps a cell to the card or row drawn there, mirroring the layout of gsBoard and gsTable.
func (m Model) gardenListHit(x, y int) (garden.IssueID, bool) {
	b := m.garden.board
	if b.layout != layoutBoard {
		i, ok := m.gardenTableHit(x, y)
		if !ok || b.tableView().lines[i].header {
			return 0, false
		}
		return b.tableView().lines[i].rec.Item.Issue.ID, true
	}
	rows := m.gardenBoardRows()
	line := y - shellHeaderRows - settingsSubmenuRows - 2 // under the two rows of the shared work head
	if x < 1 || line < 0 || line >= max(0, rows-3) {
		return 0, false
	}
	columns := min(gardenBoardColumns(m.width, b.selection.Mode), len(b.collection.Lanes(b.selection.Mode)))
	s := b.selection
	s.EnsureVisible(b.collection, gardenBoardCapacity(rows), columns)
	left := 1
	for j, w := range boardColumnWidths(m.width, columns) {
		if x < left || x >= left+w {
			left += w + 1
			continue
		}
		col := work.Column(s.Window + j)
		items := b.collection.Lanes(s.Mode)[col]
		first := min(s.Scroll[col], max(0, len(items)-1))
		top := 3 // lane title, count and upward scroll indicator
		for n := first; n < len(items) && n < first+gardenBoardCapacity(rows); n++ {
			card := m.boardCardRows(items[n].Item.Issue.ID)
			// The trailing gap row belongs to no card.
			if line >= top && line < top+card-1 {
				return items[n].Item.Issue.ID, true
			}
			top += card
		}
		return 0, false
	}
	return 0, false
}

// --- drawing ---

// overlayHit is a clickable span of a panel row, in panel-relative cells.
type overlayHit struct {
	y, x0, x1 int
	tab       int // section index, or -1 for an action
	action    string
}

// issueOverlayRows draws the panel as w x h rows of exact width, with the hit map of its tabs and actions.
func (m Model) issueOverlayRows(w, h int) ([]string, []overlayHit) {
	p := theme.Current
	_, _, _, _, cw, bodyW, bodyH, rail := overlayGeometry(m.width, m.height)
	blank := shellRow(w, p.Surface, "", "")
	if w < overlayMinW || h < overlayMinH {
		out := make([]string, h)
		for i := range out {
			out[i] = blank
		}
		if h > 0 {
			out[0] = shellRow(w, p.Surface, seg(p.Text, p.Surface, true).Render(ansi.Truncate("esc close", w, "")), "")
		}
		return out, nil
	}
	b := m.garden.board
	r, _, ok := b.collection.Find(b.opened)
	border := func(s string) string { return seg(p.BorderFocus, p.Surface, false).Render(m.chrome(s)) }
	pad := seg("", p.Surface, false).Render(" ")
	row := func(content string) string { return border("│") + pad + content + pad + border("│") }
	line := func(fg lipgloss.Color, bold bool, left, right string) string {
		if right != "" {
			right = " " + right
		}
		left = ansi.Truncate(left, max(cw-ansi.StringWidth(right), 0), "…")
		return row(shellRow(cw, p.Surface, seg(fg, p.Surface, bold).Render(left), seg(p.Muted, p.Surface, false).Render(right)))
	}

	title := " Issue "
	if ok {
		title = fmt.Sprintf(" Issue #%d ", r.Item.Issue.Number)
	}
	out := []string{border("╭─" + title + strings.Repeat("─", max(w-4-ansi.StringWidth(title), 0)) + "─╮")}
	var hits []overlayHit

	head, state, stateFg := "", "", p.Muted
	column := ""
	if ok {
		head = fmt.Sprintf("#%d  %s", r.Item.Issue.Number, boardTitle(r.Item.Issue.Title))
		state = fmt.Sprintf("%s · %s · %s", r.Column, r.Item.Classification.Type, r.Priority)
		status, fg := gardenApprovalIndicator(r)
		state += " · " + status
		stateFg = fg
		column = r.Column.String()
		if m.handoffDone() {
			state, stateFg = "✓ Done · "+state, p.Success
		}
	}
	out = append(out, line(p.Text, true, head, column))
	doc := m.gardenDocument(bodyW)
	scroll := max(0, min(b.docScroll, max(0, len(doc)-bodyH)))
	pos := ""
	if len(doc) > bodyH {
		pos = fmt.Sprintf("%d–%d / %d", scroll+1, min(scroll+bodyH, len(doc)), len(doc))
	}
	out = append(out, line(stateFg, false, state, pos))

	// Tabs
	var tabs strings.Builder
	x := 2
	for i, name := range gardenDocumentSections {
		label := " " + name + " "
		style := seg(p.Muted, p.Raised, false)
		if i == b.section {
			style = seg(p.Background, p.Accent, true)
		}
		tabs.WriteString(style.Render(label) + seg("", p.Surface, false).Render(" "))
		w := ansi.StringWidth(label)
		hits = append(hits, overlayHit{y: len(out), x0: x, x1: min(x+w, cw+2), tab: i})
		x += w + 1
	}
	out = append(out, row(shellRow(cw, p.Surface, tabs.String(), "")))

	// Body, with the summary rail when it fits.
	var summary []string
	if rail {
		summary = m.issueRail(overlayRailW)
	}
	for i := 0; i < bodyH; i++ {
		left := blankRow(bodyW, p.Surface)
		if scroll+i < len(doc) {
			left = doc[scroll+i]
		}
		if rail {
			side := blankRow(overlayRailW, p.Surface)
			if i < len(summary) {
				side = summary[i]
			}
			left += seg(p.Border, p.Surface, false).Render(m.chrome(" │ ")) + side
		}
		out = append(out, row(left))
	}

	// Pinned actions, then the reason a disabled or just-refused one gives.
	controls := m.issueControls()
	total := 0
	for _, c := range controls {
		total += ansi.StringWidth(c.key+" "+c.long) + 3
	}
	short := total > cw
	var bar strings.Builder
	x = 2
	for _, c := range controls {
		label := " " + c.key + " " + c.label(short) + " "
		style := seg(p.Text, p.Raised, false)
		switch {
		case !c.enabled:
			style = seg(p.Dim, p.Raised, false)
		case c.id == b.overlayFocus || b.overlayFocus == "" && (c.id == "start" || c.id == "launch-session"):
			style = seg(p.Background, p.Accent, true)
		}
		bar.WriteString(style.Render(label) + seg("", p.Surface, false).Render(" "))
		w := ansi.StringWidth(label)
		hits = append(hits, overlayHit{y: len(out), x0: x, x1: min(x+w, cw+2), tab: -1, action: c.id})
		x += w + 1
	}
	out = append(out, row(shellRow(cw, p.Surface, bar.String(), "")))
	reason, fg := "←/→ tab · ↑/↓ scroll · tab actions", p.Muted
	if a, ok := m.launchOverviewAttempt(); ok && b.section == 0 {
		reason = m.launchNext(a)
	}
	if r, ok := m.handoffReason(); ok {
		reason = r
	}
	for _, c := range controls {
		if c.reason != "" {
			reason = c.key + " unavailable: " + c.reason
		}
	}
	if !m.launchSupported() {
		reason = "s, i unavailable: this Garden runtime has no local launch"
	}
	if n := m.garden.notice; n != "" {
		reason, fg = n, p.Warning
	}
	out = append(out, line(fg, false, reason, ""))
	out = append(out, border("╰─"+strings.Repeat("─", max(w-4, 0))+"─╯"))
	return m.settingsASCII(out), hits
}

func (c issueControl) label(short bool) string {
	if short {
		return c.short
	}
	return c.long
}

// issueRail is the wide summary beside the document: facts only, no actions.
func (m Model) issueRail(w int) []string {
	p := theme.Current
	b := m.garden.board
	r, _, ok := b.collection.Find(b.opened)
	if !ok {
		return nil
	}
	var out []string
	add := func(fg lipgloss.Color, bold bool, s string) {
		for _, l := range strings.Split(ansi.Wrap(s, w, ""), "\n") {
			out = append(out, shellRow(w, p.Surface, seg(fg, p.Surface, bold).Render(ansi.Truncate(l, w, "…")), ""))
		}
	}
	status, fg := gardenApprovalIndicator(r)
	add(p.Muted, true, "SUMMARY")
	add(boardPhaseColor(r.Column.Phase()), true, r.Column.String())
	add(p.Text, false, fmt.Sprintf("%s / %s", r.Item.Classification.Type, r.Priority))
	add(fg, false, status)
	if lb, ok := m.localBadgeFor(r.Item.Issue.ID); ok {
		add(lb.fg, false, "local: "+lb.long)
	}
	rv := planrev.FromVerification(r.Item.Issue, r.Item.Verification)
	add(p.Muted, false, fmt.Sprintf("Plan revisions: %d", len(rv.Revisions)))
	add(p.Muted, false, "Next gate: "+work.NextGate(r.Column))
	if b.section == planTabSection {
		m.planTabRail(add)
	}
	m.handoffRail(add)
	return out
}

// overlayFocusID is the focused action. The approval confirmation keeps its own focus (the two buttons), and the
// text box of the request-changes form is the empty focus.
func (m Model) overlayFocusID() string {
	if m.planDecideInOverlay() {
		if m.garden.planrev.btn == 1 {
			return "decide-approve"
		}
		return "decide-cancel"
	}
	return m.garden.board.overlayFocus
}

// planTabShortcut runs a Plan tab key (a, b, c, u) through the action it names. A key whose action is not offered
// in this state does nothing; one that is offered but disabled says why.
func (m Model) planTabShortcut(k string) (tea.Model, tea.Cmd) {
	for _, c := range m.issueControls() {
		if c.key != k || c.id == "close" {
			continue
		}
		if !c.enabled {
			m.garden.notice = c.reason
			return m, nil
		}
		return m.issueActivate(c.id)
	}
	// The tab offers no action on this key in this state; p and r keep their usual meaning.
	if id := map[string]string{"p": "plan", "r": "review"}[k]; id != "" {
		return m.issueActivate(id)
	}
	return m, nil
}
