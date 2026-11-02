package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden/spec"
	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
)

// Layout -------------------------------------------------------------------------------

// privPaneWidth is the discussion column beside the text: about a third of a wide terminal.
func privPaneWidth(width int) int { return max(34, min(46, width*3/10)) }

// privSideAssist says the discussion sits beside the text; privPageAssist says it replaces the text on a compact screen.
func (m Model) privSideAssist(p *privateDraft) bool {
	return p != nil && p.paActive() && !m.privCompact()
}
func (m Model) privPageAssist(p *privateDraft) bool {
	return p != nil && p.paActive() && m.privCompact() && p.foc == p.focInvite()
}

// paDims is the pane's width and height in cells.
func (m Model) paDims() (pw, ph int) {
	ph = max(3, m.gardenBoardRows()-5)
	if m.privCompact() {
		return max(20, m.width-2), ph
	}
	return privPaneWidth(m.width), ph
}

// paLayout is how the pane spends its rows in the questions stage. The answer panel takes what the fixed
// parts leave, and always keeps its options and field (panelGeometry shrinks the field first).
type paLayout struct {
	iw        int
	blankTop  bool
	under     []string
	actRows   [][]paAction
	notice    []string
	panelRows int
}

func (m Model) paLayoutOf(p *privateDraft, iw, ph int) paLayout {
	pa := p.pa
	l := paLayout{iw: iw, blankTop: ph >= 22}
	if pa.stage == paQuestions && pa.s.understanding != "" {
		capLines := 1
		switch {
		case ph >= 26:
			capLines = 3
		case ph >= 20:
			capLines = 2
		}
		l.under = wrapLines(boardTitle(pa.s.understanding), iw)
		if len(l.under) > capLines {
			l.under = l.under[:capLines]
		}
	}
	l.actRows = m.paActionRows(p, iw)
	l.notice = m.paNotice(p, iw)
	fixed := 2 + b2i(l.blankTop) + len(l.under) + b2i(len(l.under) > 0) + 1 + len(l.notice) + len(l.actRows)
	l.panelRows = max(3, ph-fixed)
	return l
}

// paNotice is the status lines under the body: a running request, a failure or a note.
func (m Model) paNotice(p *privateDraft, iw int) []string {
	pa := p.pa
	var text string
	switch {
	case pa.flight != nil:
		f := pa.flight
		who := "the assistant"
		if pa.s.provider != "" {
			who = pa.s.provider
		}
		text = fmt.Sprintf("Asking %s… %ds · Esc cancels", who, f.secs)
	case pa.err != nil:
		text = "! " + pa.err.Message
		if pa.err.Next != "" {
			text += " " + pa.err.Next
		}
	case pa.notice != "":
		text = pa.notice
	case pa.s.notice != "":
		text = pa.s.notice
	}
	if text == "" {
		return nil
	}
	lines := wrapLines(boardTitle(text), iw)
	return lines[:min(len(lines), 3)]
}

var paLabels = map[paAction]string{
	actAsk: "Ask a question", actRetry: "Retry", actClose: "Close", actApplyAnswers: "Apply answers →",
	actDefer: "Defer", actAssume: "Assume", actWhy: "Why?", actApplyProposal: "Apply →", actDiscard: "Discard",
}

// paActionRows wraps the stage's buttons to the pane width.
func (m Model) paActionRows(p *privateDraft, iw int) [][]paAction {
	var rows [][]paAction
	var cur []paAction
	used := 0
	for _, a := range m.paActions(p) {
		w := ansi.StringWidth(paLabels[a]) + 3
		if len(cur) > 0 && used+w > iw {
			rows, cur, used = append(rows, cur), nil, 0
		}
		cur, used = append(cur, a), used+w
	}
	return append(rows, cur)
}

// Pane ---------------------------------------------------------------------------------

// paLine is one pane row of exactly pw cells: the edge, a space and the content.
func paLine(edge, content string, iw int) string {
	c := theme.Current
	pad := max(0, iw-lipgloss.Width(content))
	return edge + seg("", c.Background, false).Render(" ") + content + seg("", c.Background, false).Render(strings.Repeat(" ", pad))
}

func paText(fg lipgloss.Color, bold bool, text string, iw int) string {
	return seg(fg, theme.Current.Background, bold).Render(fitCell(text, iw))
}

// privPane draws the discussion as exactly ph lines of pw cells.
func (m Model) privPane(p *privateDraft, pw, ph int) []string {
	c := theme.Current
	pa := p.pa
	iw := pw - 2
	edgeFG := c.Dim
	if p.pane {
		edgeFG = c.Warning
	}
	edge := seg(edgeFG, c.Background, false).Render("│")
	l := m.paLayoutOf(p, iw, ph)
	var body []string
	add := func(fg lipgloss.Color, bold bool, text string) { body = append(body, paText(fg, bold, text, iw)) }
	head := "DISCUSSION"
	if p.pane {
		head = "▸ DISCUSSION" // a glyph, never colour alone, says the pane has the keyboard
	}
	if b := p.paBadge(); b != "" {
		head += " " + b + " reply ready"
	}
	add(c.Muted, true, head)
	who := pa.s.provider
	if who == "" {
		who = "assistant"
	}
	if pa.s.accountName != "" {
		who += " / " + boardTitle(pa.s.accountName)
	}
	add(c.Accent, false, "● "+who)
	if l.blankTop {
		add(c.Muted, false, "")
	}
	switch pa.stage {
	case paQuestions:
		for _, t := range l.under {
			add(c.Text, false, t)
		}
		if len(l.under) > 0 {
			add(c.Muted, false, "")
		}
		if pa.flight != nil && pa.flight.draft {
			add(c.Muted, false, "Your answers are being turned into a proposal.")
		}
		body = append(body, m.assistPanel(pa.s, iw, l.panelRows)...)
		pos := fmt.Sprintf("%d of %d useful question%s", pa.s.sel+1, len(pa.s.questions), plural(len(pa.s.questions)))
		if pa.decided > 0 {
			pos = fmt.Sprintf("%d of %d · %d already decided", pa.s.sel+1, len(pa.s.questions), pa.decided)
		}
		for len(body) < ph-len(l.notice)-len(l.actRows)-1 {
			body = append(body, paText(c.Muted, false, "", iw))
		}
		add(c.Muted, false, pos)
	case paProposal:
		body = append(body, m.paProposalLines(p, iw, ph-len(l.notice)-len(l.actRows)-len(body))...)
	default:
		body = append(body, m.paIdleLines(p, iw)...)
	}
	for len(body) < ph-len(l.notice)-len(l.actRows) {
		body = append(body, paText(c.Muted, false, "", iw))
	}
	body = body[:max(0, ph-len(l.notice)-len(l.actRows))]
	for _, t := range l.notice {
		fg := c.Warning
		if pa.err != nil && pa.flight == nil {
			fg = c.Danger
		}
		if pa.flight != nil {
			fg = c.Accent
		}
		body = append(body, paText(fg, false, t, iw))
	}
	for _, row := range l.actRows {
		var runs []string
		for _, a := range row {
			focused := p.pane && (pa.onAct || pa.stage == paIdle) && m.paActions(p)[min(pa.act, len(m.paActions(p))-1)] == a
			runs = append(runs, privButton(paLabels[a], focused, a == actApplyAnswers || a == actApplyProposal || a == actAsk))
		}
		body = append(body, strings.Join(runs, seg("", c.Background, false).Render(" ")))
	}
	out := make([]string, 0, ph)
	for _, b := range body[:min(len(body), ph)] {
		out = append(out, paLine(edge, b, iw))
	}
	for len(out) < ph {
		out = append(out, paLine(edge, "", iw))
	}
	return out
}

func (m Model) paIdleLines(p *privateDraft, iw int) []string {
	c := theme.Current
	pa := p.pa
	var out []string
	add := func(fg lipgloss.Color, bold bool, text string) {
		for _, l := range wrapLines(text, iw) {
			out = append(out, paText(fg, bold, l, iw))
		}
	}
	if pa.flight != nil {
		add(c.Text, false, "Working on it. Keep writing: your text is yours, and a reply never overwrites what you type meanwhile.")
		return out
	}
	add(c.Text, false, "The assistant can ask up to three short questions about this draft, then propose wording for you to review.")
	out = append(out, paText(c.Muted, false, "", iw))
	add(c.Muted, false, "Nothing changes until you apply it. You can still edit every section directly.")
	if n := len(p.confirmed); n > 0 {
		out = append(out, paText(c.Muted, false, "", iw))
		add(c.Accent, false, fmt.Sprintf("✓ %d confirmed answer%s", n, plural(n)))
	}
	if n := len(p.decisions); n > 0 {
		add(c.Accent, false, fmt.Sprintf("✓ %d recorded decision%s", n, plural(n)))
	}
	return out
}

// paProposalLines lists the proposal's sections and what the selected one would change. The proposal is drawn as
// a proposal: its additions stay in this pane until the person applies them.
func (m Model) paProposalLines(p *privateDraft, iw, rows int) []string {
	c := theme.Current
	pa := p.pa
	r := pa.review
	var out []string
	add := func(fg lipgloss.Color, bold bool, text string) { out = append(out, paText(fg, bold, text, iw)) }
	add(c.Warning, true, "PROPOSAL — not in your draft yet")
	if rows >= 14 {
		add(c.Muted, false, "Enter takes or leaves a section.")
	}
	head := len(out)
	secs := reviewSectionsOf(pa.review, p.fixedText)
	var list []string
	at := r.sel
	push := out
	out = nil
	for i := 0; i < spec.FixedCount; i++ {
		sec := secs[i]
		mark, box := "  ", "[ ]"
		if r.sel == i && !r.onSuggestions() && p.pane && !pa.onAct {
			mark = "› "
		}
		if sec.accept {
			box = "[x]"
		}
		word, fg := "no change", c.Dim
		switch sec.state {
		case '~':
			word, fg = "proposed", c.Text
			if sec.accept {
				fg = c.Success
			}
		case '!':
			word, fg = "you edited it", c.Warning
			if sec.accept {
				word = "replaces yours"
			} else {
				word = "kept yours"
			}
		}
		if sec.state == '=' {
			box = "   "
		}
		name := privFieldLabel(i)
		add(fg, r.sel == i && !r.onSuggestions(), fmt.Sprintf("%s%s %-13s %s", mark, box, name, word))
	}
	if len(r.sugg) > 0 {
		fg := c.Text
		mark := "  "
		if r.onSuggestions() && p.pane && !pa.onAct {
			mark, fg = "› ", c.Warning
		}
		add(fg, r.onSuggestions(), fmt.Sprintf("%s? %d suggestion%s (not yours)", mark, len(r.sugg), plural(len(r.sugg))))
	}
	// A short pane scrolls the list to keep the selected row in view and still leaves a line or two for detail.
	list, out = out, push
	if limit := max(3, rows-head-3); len(list) > limit {
		list, _ = scrollWindow(list, at, at+1, limit)
	}
	out = append(out, list...)
	room := max(0, rows-len(out))
	var detail []assistInfoLine
	if r.onSuggestions() {
		detail = m.assistSuggestionPane(r, room, privFieldLabel)
	} else {
		detail = m.assistSectionPane(pa.s, secs, pa.proposal, iw)
	}
	// The shared panes write one long line per fact; the narrow column wraps them.
	var wrapped []assistInfoLine
	for _, l := range detail {
		for _, w := range wrapLines(l.text, iw) {
			wrapped = append(wrapped, assistInfoLine{w, l.fg, l.bold})
		}
	}
	for _, l := range wrapped[:min(len(wrapped), room)] {
		add(l.fg, l.bold, l.text)
	}
	return out
}

// Editor beside the pane -----------------------------------------------------------------

// privAssistView is the editor with the discussion beside the text: the specification keeps its tabs and
// its cursor, the pane keeps its own.
func (m Model) privAssistView(p *privateDraft, width int) []string {
	c := theme.Current
	out := m.privHeader(p, width)
	pw, ph := m.paDims()
	tw, th := m.privEditorText()
	pane := m.privPane(p, pw, ph)
	right := []string{m.privTabs(p, tw, false)}
	title := strings.ToUpper(p.secs[p.cur].title)
	hint := ""
	switch {
	case p.text:
		hint = "  writing"
	case p.changed[p.cur]:
		hint = "  ● updated by the assistant"
	}
	right = append(right, privCell(" "+title+hint+" ", tw, c.Warning, c.Raised, true))
	right = append(right, m.privTextLines(p, tw, th)...)
	if _, pv := m.privEditorLayout(); pv > 0 {
		right = append(right, privCell("", tw, c.Muted, c.Background, false))
		right = append(right, m.privPreviews(p, tw, ph-len(right))...)
	}
	for len(right) < ph {
		right = append(right, privCell("", tw, c.Muted, c.Background, false))
	}
	for i := 0; i < ph; i++ {
		out = append(out, shellRow(width, c.Background, " "+pane[i]+seg("", c.Background, false).Render("  ")+right[i], ""))
	}
	review := privButton("Review & create issue →", p.foc == p.focReview() && !p.text && !p.pane, true)
	out = append(out, m.privReadinessRow(p, width))
	out = append(out, shellRow(width, c.Background, "", review+" "))
	return out
}

// Invitation -----------------------------------------------------------------------------

// privInviteView is the invitation: the shared options dialog under this draft's own wording, and the two
// ways forward. Nothing is sent until Start discussion.
func (m Model) privInviteView(p *privateDraft, width, rows int) []string {
	c := theme.Current
	pa := p.pa
	out := m.gsAssistOptions(pa.s, width, max(3, rows-3))
	// The buttons sit under the form, not at the foot of a mostly blank screen.
	blank := blankRow(width, c.Background)
	for len(out) > 1 && out[len(out)-1] == blank {
		out = out[:len(out)-1]
	}
	out = append(out[:min(len(out), max(1, rows-3))], blank)
	keep := privButton("Keep writing myself", pa.invBtn == 0, false)
	start := privButton("Start discussion", pa.invBtn == 1, true)
	gap := seg("", c.Background, false).Render("  ")
	out = append(out, shellRow(width, c.Background, seg("", c.Background, false).Render(" ")+keep+gap+start, ""))
	out = append(out, gText(width, c.Muted, false, "Tab moves on · Esc keeps writing without an assistant · Ctrl+S still saves the draft"))
	for len(out) < rows {
		out = append(out, blank)
	}
	return out[:rows]
}

// Compact tabs and pages --------------------------------------------------------------------

// paTabLabel is the extra tab of the compact layout: where the assistant is invited or resumed.
func paTabLabel(p *privateDraft) string {
	switch {
	case p.pa == nil || p.pa.stage == paInvite:
		return "Assistant…"
	case p.paBadge() != "":
		return "Discussion ●"
	}
	return "Discussion"
}

// privAssistPage is the compact discussion: the pane where the text would be.
func (m Model) privAssistPage(p *privateDraft, width int) []string {
	c := theme.Current
	pw, ph := m.paDims()
	var out []string
	for _, l := range m.privPane(p, pw, ph) {
		out = append(out, shellRow(width, c.Background, " "+l, ""))
	}
	return out
}

// paPendingRow is the readiness text for decisions still waiting in the pane.
func paPendingText(n int) string {
	return fmt.Sprintf("%d assistant decision%s open", n, plural(n))
}
