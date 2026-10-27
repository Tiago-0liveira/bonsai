package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden/drafts"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/spec"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/work"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
)

// privCell clips s to exactly w cells and paints it.
func privCell(s string, w int, fg, bg lipgloss.Color, bold bool) string {
	s = ansi.Truncate(s, w, "…")
	return seg(fg, bg, bold).Render(s + strings.Repeat(" ", max(0, w-ansi.StringWidth(s))))
}

// privButton is a button; the focus marker is a glyph, never color alone. primary is the filled one.
func privButton(label string, focused, primary bool) string {
	p := theme.Current
	mark := " "
	if focused {
		mark = "▸"
	}
	if primary {
		return seg(p.Background, p.Accent, true).Render(mark + label + " ")
	}
	fg := p.Muted
	if focused {
		fg = p.Warning
	}
	return seg(fg, p.Raised, focused).Render(mark + label + " ")
}

// Layout -------------------------------------------------------------------------

// privCaptureLayout sizes the capture screen: the options and spacing give way before the text box.
func privCaptureLayout(rows int) (box int, spacers, opts bool) {
	spacers, opts = rows >= 14, rows >= 24
	fixed := 4 // heading, hint, buttons, status
	if spacers {
		fixed += 2
	}
	if opts {
		fixed += 5
	}
	return max(3, min(12, rows-fixed)), spacers, opts
}

func privCaptureBox(width, rows int) (w, h int) {
	h, _, _ = privCaptureLayout(rows)
	return max(10, min(width-8, 110)), h
}

// privEditorLayout is the text height and the rows spent on read-only previews of the other sections.
func (m Model) privEditorLayout() (th, pv int) {
	rows := m.gardenBoardRows()
	if m.privCompact() {
		return max(3, rows-3-1-1), 0
	}
	avail := max(3, rows-3-2-1)
	if m.privSideAssist(m.garden.drafts.private) {
		avail = max(3, rows-5-2) // the tabs and the section title take two rows of the column
	}
	if p := m.garden.drafts.private; p != nil && avail >= 14 {
		others := 0
		for i, e := range p.secs {
			if i != p.cur && strings.TrimSpace(e.text) != "" {
				others++
			}
		}
		pv = min(2*others, avail/3) &^ 1
		if pv > 0 {
			return avail - pv - 1, pv
		}
	}
	return avail, 0
}

func (m Model) privEditorText() (w, h int) {
	h, _ = m.privEditorLayout()
	if m.privCompact() {
		return max(10, m.width-4), h
	}
	if m.privSideAssist(m.garden.drafts.private) {
		return max(10, m.width-privPaneWidth(m.width)-5), h
	}
	return max(10, min(100, m.width-gardenEditorRail(m.width)-4)), h
}

// Dispatch -------------------------------------------------------------------------

func (m Model) gsPrivate(width, rows int) []string {
	p := m.garden.drafts.private
	var out []string
	switch {
	case p == nil:
		out = []string{gText(width, theme.Current.Muted, false, "No draft is open.")}
	case p.stage == privCapture:
		out = m.privCaptureView(p, width, rows)
	case p.stage == privReview:
		out = m.privReviewView(p, width, rows)
	case p.stage == privPublish:
		out = m.privPublishView(p, width, rows)
	case p.pa != nil && p.pa.stage == paInvite:
		out = m.privInviteView(p, width, rows)
	default:
		out = m.privEditorView(p, width, rows)
	}
	for len(out) < rows {
		out = append(out, blankRow(width, theme.Current.Background))
	}
	return m.settingsASCII(out[:rows])
}

func (p *privateDraft) statusColor() lipgloss.Color {
	c := theme.Current
	switch p.state {
	case saveFailed, saveConflict:
		return c.Danger
	case saveDirty, saveSaving:
		return c.Warning
	}
	return c.Accent
}

// message is the one line that explains the draft's state: a conflict, a failed save or a notice.
func (p *privateDraft) message() (string, lipgloss.Color) {
	c := theme.Current
	switch {
	case p.state == saveConflict && p.conflict != nil:
		return "! Changed in another Bonsai window. Your text was kept as " + drafts.DraftLabel(p.conflict.Incoming.ID) + ".", c.Danger
	case p.err != nil:
		return "! " + p.err.Message + " Edit or press Ctrl+S to retry.", c.Danger
	case p.notice != "":
		return p.notice, c.Warning
	}
	return "", c.Muted
}

// Capture --------------------------------------------------------------------------

func (m Model) privCaptureView(p *privateDraft, width, rows int) []string {
	c := theme.Current
	bw, bh := privCaptureBox(width, rows)
	_, spacers, opts := privCaptureLayout(rows)
	blank := blankRow(width, c.Background)
	out := []string{gText(width, c.Text, true, "WHAT'S ON YOUR MIND?"), gText(width, c.Muted, false, "Describe a problem, an idea or something you want to change.")}
	if spacers {
		out = append(out, blank)
	}
	focus := p.foc == capDesc
	lines := strings.Split(p.desc.View(), "\n")
	edge := c.Muted
	if focus {
		edge = c.Warning
	}
	for i := 0; i < bh; i++ {
		text := ""
		if i < len(lines) {
			text = lines[i]
		}
		marker := " "
		if focus && i == 0 {
			marker = "›"
		}
		left := seg(c.Warning, c.Background, true).Render(marker) + seg(edge, c.Surface, false).Render(" ") + privCell(text, bw, c.Text, c.Surface, false) + seg(edge, c.Surface, false).Render(" ")
		out = append(out, shellRow(width, c.Background, left, ""))
	}
	if spacers {
		out = append(out, blank)
	}
	if opts {
		out = append(out,
			gText(width, c.Accent, true, "○ Write it yourself"), gText(width, c.Muted, false, "  Shape a clear ticket using a few simple sections."), blank,
			gText(width, c.Warning, true, "○ Invite an assistant"), gText(width, c.Muted, false, "  Choose a model to help clarify the idea and ask useful questions."))
	}
	out = append(out, gButtons(width, "  ", privButton("Continue writing →", p.foc == capContinue, true), privButton("Discuss with AI…", p.foc == capDiscuss, false)))
	status := seg(p.statusColor(), c.Background, false).Render(" " + p.status())
	rest := seg(c.Muted, c.Background, false).Render("  ·  No GitHub issue yet")
	if msg, fg := p.message(); msg != "" {
		rest = seg(fg, c.Background, false).Render("  ·  " + msg)
	}
	out = append(out, shellRow(width, c.Background, status+rest, ""))
	return out
}

// Editor ---------------------------------------------------------------------------

// chipText names where the draft lives: private until publication starts.
func (p *privateDraft) chipText() string {
	if r := p.pub; r != nil && r.result != nil {
		switch r.result.State {
		case gardenapi.PublishIncomplete:
			return "Publishing incomplete"
		case gardenapi.PublishUnknown:
			return "Publishing"
		}
	}
	switch p.draft.Publication.State {
	case drafts.Publishing:
		if p.draft.Publication.Issue != nil {
			return "Publishing incomplete"
		}
		return "Publishing"
	case drafts.Published:
		return "Published"
	}
	return "Private draft"
}

func (m Model) privHeader(p *privateDraft, width int) []string {
	c := theme.Current
	title := strings.TrimSpace(p.title.Value())
	if title == "" {
		title = "Untitled draft"
	}
	marker := " "
	if p.foc == p.focTitle() && p.stage == privEditor && !p.text {
		marker = "›"
	}
	titleView := seg(c.Text, c.Background, true).Render(title)
	if p.titleEditing {
		p.title.Width = max(8, width-8)
		titleView = p.title.View()
		marker = "›"
	}
	row0 := shellRow(width, c.Background, seg(c.Warning, c.Background, true).Render(marker+"✎ ")+titleView, "")
	typ := titleCase(string(captureTypes[p.typ]))
	tmark := " "
	if p.foc == p.focType() && p.stage == privEditor && !p.text {
		tmark = "›"
	}
	chips := seg(c.Warning, c.Background, true).Render(" ") + seg(c.Accent, c.Raised, false).Render(tmark+typ+" ") +
		seg("", c.Background, false).Render(" ") + seg(c.Warning, c.Raised, false).Render(" "+p.chipText()+" ")
	status := seg(p.statusColor(), c.Background, false).Render(p.status() + " ")
	row1 := shellRow(width, c.Background, chips, status)
	msg, fg := p.message()
	row2 := gText(width, fg, false, msg)
	return []string{row0, row1, row2}
}

type privCheck struct {
	ok   bool
	text string
}

// privChecks lists the readiness checks. pending counts assistant questions about scope or acceptance that are
// still waiting in the discussion: they are not stored, but they hold the draft back until applied or left open.
func privChecks(r drafts.Readiness, pending int) []privCheck {
	missing := map[string]bool{}
	for _, name := range r.Missing {
		missing[name] = true
	}
	var out []privCheck
	if missing["Title"] {
		out = append(out, privCheck{false, "Title missing"})
	}
	for _, it := range []struct{ key, ok string }{{"Problem", "Problem stated"}, {"Outcome", "Outcome stated"}, {"Criteria", "Criteria written"}} {
		if missing[it.key] {
			out = append(out, privCheck{false, it.key + " missing"})
		} else {
			out = append(out, privCheck{true, it.ok})
		}
	}
	if r.Unresolved > 0 {
		out = append(out, privCheck{false, fmt.Sprintf("%d open question%s", r.Unresolved, plural(r.Unresolved))})
	} else {
		text := "Questions resolved"
		if r.Deferred > 0 {
			text += fmt.Sprintf(" · %d deferred", r.Deferred)
		}
		out = append(out, privCheck{true, text})
	}
	if pending > 0 {
		out = append(out, privCheck{false, paPendingText(pending)})
	}
	return out
}

// privReasons are the concrete things that stop the draft from being ready.
func privReasons(r drafts.Readiness, pending int) []string {
	var out []string
	for _, c := range privChecks(r, pending) {
		if !c.ok {
			out = append(out, c.text)
		}
	}
	return out
}

func (m Model) privReadinessRow(p *privateDraft, width int) string {
	c := theme.Current
	r := p.assess()
	if p.state == saveConflict {
		return gRuns(width, c.Background, grun{"k Keep mine   o Open theirs   Esc inbox", c.Warning, true})
	}
	if m.privCompact() {
		done := 3
		for _, name := range r.Missing {
			if name != "Title" {
				done--
			}
		}
		text, fg := fmt.Sprintf("%d/3 ready", done), c.Warning
		if reasons := privReasons(r, p.paPending()); len(reasons) > 0 {
			text += " · " + reasons[0]
		} else {
			fg = c.Accent
		}
		return seg(fg, c.Background, false).Render(" " + text)
	}
	var runs []grun
	for _, ck := range privChecks(r, p.paPending()) {
		if ck.ok {
			runs = append(runs, grun{"✓ " + ck.text + "   ", c.Accent, false})
		} else {
			runs = append(runs, grun{"○ " + ck.text + "   ", c.Warning, false})
		}
	}
	return gRuns(width, c.Background, runs...)
}

func (m Model) privEditorView(p *privateDraft, width, rows int) []string {
	c := theme.Current
	if m.privSideAssist(p) {
		return m.privAssistView(p, width)
	}
	out := m.privHeader(p, width)
	th, pv := m.privEditorLayout()
	tw, _ := m.privEditorText()
	review := privButton("Review & create issue →", p.foc == p.focReview() && !p.text, true)
	if m.privCompact() {
		out = append(out, m.privTabs(p, width, true))
		if m.privPageAssist(p) {
			out = append(out, m.privAssistPage(p, width)...)
		} else {
			out = append(out, m.privTextRows(p, width, tw, th, 0)...)
		}
		left := m.privReadinessRow(p, width)
		out = append(out, shellRow(width, c.Background, ansi.Truncate(left, max(0, width-lipgloss.Width(review)-2), ""), review+" "))
		return out
	}
	railW := gardenEditorRail(width)
	rail := m.privRail(p, railW, m.gardenBoardRows()-5)
	pane := []string{}
	title := strings.ToUpper(p.secs[p.cur].title)
	hint := ""
	if p.text {
		hint = "  writing"
	}
	pane = append(pane, privCell(" "+title+hint+" ", tw, c.Warning, c.Raised, true))
	pane = append(pane, m.privTextLines(p, tw, th)...)
	if pv > 0 {
		pane = append(pane, privCell("", tw, c.Muted, c.Background, false))
		pane = append(pane, m.privPreviews(p, tw, len(rail)-1-len(pane))...)
	}
	for len(pane) < len(rail) {
		pane = append(pane, privCell("", tw, c.Muted, c.Background, false))
	}
	gap := seg("", c.Background, false).Render("  ")
	for i := range rail {
		out = append(out, shellRow(width, c.Background, " "+rail[i]+gap+pane[i], ""))
	}
	out = append(out, m.privReadinessRow(p, width))
	out = append(out, shellRow(width, c.Background, "", review+" "))
	return out
}

// privPreviews is a title and a first line for every other written section, up to limit lines.
func (m Model) privPreviews(p *privateDraft, tw, limit int) []string {
	c := theme.Current
	var out []string
	for i, e := range p.secs {
		if i == p.cur || strings.TrimSpace(e.text) == "" || len(out)+2 > limit {
			continue
		}
		first := strings.TrimSpace(strings.SplitN(strings.TrimSpace(e.text), "\n", 2)[0])
		out = append(out, privCell(strings.ToUpper(e.title), tw, c.Muted, c.Background, true), privCell(first, tw, c.Muted, c.Background, false))
	}
	return out
}

// privTextLines draws the textarea at exactly th lines of tw cells.
func (m Model) privTextLines(p *privateDraft, tw, th int) []string {
	c := theme.Current
	lines := strings.Split(p.ta.View(), "\n")
	out := make([]string, th)
	for i := range out {
		text := ""
		if i < len(lines) {
			text = lines[i]
		}
		if i >= len(lines) {
			out[i] = privCell("", tw, c.Text, c.Background, false)
			continue
		}
		out[i] = padCells(ansi.Truncate(text, tw, ""), tw)
	}
	return out
}

func padCells(s string, w int) string {
	return s + strings.Repeat(" ", max(0, w-ansi.StringWidth(s)))
}

// privTextRows is the compact text region, one-cell margins.
func (m Model) privTextRows(p *privateDraft, width, tw, th, _ int) []string {
	c := theme.Current
	var out []string
	for _, l := range m.privTextLines(p, tw, th) {
		out = append(out, shellRow(width, c.Background, seg("", c.Background, false).Render("  ")+l, ""))
	}
	return out
}

// privTabs is the compact section row: five tabs, plus the open one when it is a More section.
func (m Model) privTabs(p *privateDraft, width int, discuss bool) string {
	c := theme.Current
	short := []string{"Problem", "Outcome", "Req", "Criteria", "Questions"}
	var b strings.Builder
	b.WriteString(seg("", c.Background, false).Render(" "))
	add := func(i int, label string) {
		if p.changed[i] {
			label += " ●" // an apply just rewrote it
		}
		if i == p.cur {
			style := seg(c.Warning, c.Raised, true)
			if p.text {
				label = "▌" + label
			} else {
				label = "▸" + label
			}
			b.WriteString(style.Render(" " + label + " "))
			return
		}
		fg := c.Muted
		if strings.TrimSpace(p.secs[i].text) != "" {
			fg = c.Text
		}
		b.WriteString(seg(fg, c.Background, false).Render(" " + label + " "))
	}
	for i, label := range short {
		add(i, label)
	}
	if p.cur >= len(short) {
		add(p.cur, p.secs[p.cur].title)
	}
	if discuss {
		label := paTabLabel(p)
		switch {
		case p.foc == p.focInvite() && p.pane:
			b.WriteString(seg(c.Warning, c.Raised, true).Render(" ▌" + label + " "))
		case p.foc == p.focInvite():
			b.WriteString(seg(c.Warning, c.Raised, true).Render(" ▸" + label + " "))
		default:
			b.WriteString(seg(c.Accent, c.Background, false).Render(" " + label + " "))
		}
	}
	return shellRow(width, c.Background, ansi.Truncate(b.String(), width, ""), "")
}

// privRail is the SECTIONS column, exactly h rows of w cells.
func (m Model) privRail(p *privateDraft, w, h int) []string {
	c := theme.Current
	blank := privCell("", w, c.Muted, c.Surface, false)
	rail := []string{privCell(" SECTIONS", w, c.Muted, c.Surface, true), blank}
	for i, e := range p.secs {
		if i >= len(privPrimary) && strings.TrimSpace(e.text) == "" && i != p.cur {
			continue
		}
		mark := " "
		if strings.TrimSpace(e.text) != "" {
			mark = "✓"
		}
		label := "   " + e.title
		bg, fg, bold := c.Surface, c.Muted, false
		if strings.TrimSpace(e.text) != "" {
			fg = c.Text
		}
		if i == p.cur {
			bg, fg, bold = c.Raised, c.Warning, true
			if p.text {
				label = " ▌ " + e.title
			} else if p.foc == i {
				label = " ▸ " + e.title
			}
		}
		rail = append(rail, privCell(padCells(label, w-2)+mark, w, fg, bg, bold))
	}
	rail = append(rail, blank, privCell(" ○ Assistant off", w, c.Muted, c.Surface, false), blank)
	invite := privButton("Invite assistant…", p.foc == p.focInvite() && !p.text, false)
	rail = append(rail, seg("", c.Surface, false).Render(" ")+invite+seg("", c.Surface, false).Render(strings.Repeat(" ", max(0, w-1-lipgloss.Width(invite)))), blank)
	for _, l := range strings.Split(ansi.Wrap("Write and approve without AI.", max(1, w-2), ""), "\n") {
		rail = append(rail, privCell(" "+l, w, c.Muted, c.Surface, false))
	}
	for len(rail) < h {
		rail = append(rail, blank)
	}
	return rail[:h]
}

// Review ---------------------------------------------------------------------------

func (m Model) privReviewView(p *privateDraft, width, rows int) []string {
	c := theme.Current
	out := m.privHeader(p, width)
	d := p.build()
	r := drafts.Assess(d)
	out = append(out, gHeadChip(width, "REVIEW & CREATE ISSUE", c.Warning))
	if reasons := privReasons(r, p.paPending()); len(reasons) > 0 {
		out = append(out, gText(width, c.Warning, false, "Not ready: "+strings.Join(reasons, " · ")))
	} else {
		out = append(out, gText(width, c.Accent, false, "✓ Every required section is written."))
	}
	title := strings.TrimSpace(d.Title)
	if title == "" {
		title = "(no title yet)"
	}
	out = append(out, gField(width, 8, "Title", grun{boardTitle(title), c.Text, true}), gField(width, 8, "Type", grun{typeWord(d.Type), c.Warning, false}))
	body := gWrap(width, c.Text, false, spec.Render(d.Spec)+" ")
	if strings.TrimSpace(spec.Render(d.Spec)) == "" {
		body = []string{gText(width, c.Muted, false, "(nothing written yet)")}
	}
	avail := max(1, rows-len(out)-3)
	start := max(0, min(p.scroll, len(body)-avail))
	p.scroll = start
	for i := 0; i < avail; i++ {
		if start+i < len(body) {
			out = append(out, body[start+i])
		} else {
			out = append(out, blankRow(width, c.Background))
		}
	}
	out = append(out, gText(width, c.Muted, false, fmt.Sprintf("Lines %d–%d of %d", min(start+1, len(body)), min(start+avail, len(body)), len(body))))
	create := privButton("Review & create issue (Enter)", true, true)
	if _, ok := m.privPublisher(); !ok {
		create = seg(c.Dim, c.Raised, false).Render(" Create GitHub issue (unavailable) ")
		if p.notice == "" {
			out = append(out, gText(width, c.Warning, false, privPublishUnavailable))
		}
	} else if p.previewing {
		create = seg(c.Dim, c.Raised, false).Render(" Checking GitHub… ")
	}
	out = append(out, shellRow(width, c.Background, seg("", c.Background, false).Render(" ")+privButton("Back to editor (Esc)", true, false)+seg("", c.Background, false).Render("  ")+create, ""))
	return out
}

// Hints ----------------------------------------------------------------------------

func (m Model) privHints() []shellHint {
	p := m.garden.drafts.private
	switch {
	case p == nil:
		return []shellHint{{"esc", "drafts"}}
	case p.state == saveConflict:
		return []shellHint{{"k", "keep mine"}, {"o", "open theirs"}, {"esc", "drafts"}}
	case p.stage == privCapture:
		return []shellHint{{"Ctrl+S", "save draft"}, {"Tab", "action"}, {"Enter", "activate"}, {"Esc", "drafts"}}
	case p.stage == privReview:
		return []shellHint{{"↑/↓", "scroll"}, {"Enter", "review & create issue"}, {"Esc", "editor"}}
	case p.stage == privPublish:
		switch {
		case p.pub == nil || p.pub.running:
			return []shellHint{{"Esc", "wait"}}
		case p.pub.view != nil && p.pub.result == nil:
			return []shellHint{{"Enter", "approve & create issue"}, {"↑/↓", "scroll"}, {"Esc", "back to draft"}}
		}
		return []shellHint{{"Enter", "resume"}, {"Esc", "drafts"}}
	}
	if pa := p.pa; pa != nil {
		switch {
		case pa.stage == paInvite:
			return []shellHint{{"Tab", "next"}, {"Enter", "choose"}, {"Esc", "keep writing"}, {"Ctrl+S", "save draft"}}
		case p.pane && pa.flight != nil:
			return []shellHint{{"Esc", "cancel request"}, {"Ctrl+G", "back to text"}}
		case p.pane && pa.stage == paQuestions:
			return []shellHint{{"Tab", "zone"}, {"PgUp/PgDn", "question"}, {"Enter", "select"}, {"Esc", "back"}, {"Ctrl+G", "text"}}
		case p.pane && pa.stage == paProposal:
			return []shellHint{{"↑/↓", "section"}, {"Enter", "take/keep"}, {"Tab", "buttons"}, {"Esc", "back"}}
		case p.pane && (pa.err != nil || pa.cancelled):
			return []shellHint{{"R", "retry"}, {"←/→", "button"}, {"Enter", "choose"}, {"Esc", "back"}}
		case p.pane:
			return []shellHint{{"←/→", "button"}, {"Enter", "choose"}, {"Esc", "back"}, {"Ctrl+G", "text"}}
		}
		return []shellHint{{"Tab", "section"}, {"Ctrl+G", "discussion"}, {"Ctrl+S", "save draft"}, {"Ctrl+Enter", "review"}, {"Esc", "drafts"}}
	}
	return []shellHint{{"Tab", "section"}, {"Ctrl+S", "save draft"}, {"Ctrl+Enter", "review"}, {"Esc", "drafts"}}
}

// Inbox rows -------------------------------------------------------------------------

// gardenInboxRows is the flat list the inbox draws: the local drafts, then the GitHub ones, each group
// under a heading once there is more than one kind to tell apart.
func (m Model) gardenInboxRows() []inboxRow {
	d := m.garden.drafts
	locals := d.locals()
	var rows []inboxRow
	if len(locals) > 0 {
		rows = append(rows, inboxRow{head: fmt.Sprintf("LOCAL DRAFTS  %d  ·  private to this machine", len(locals))})
		for i := range locals {
			rows = append(rows, inboxRow{local: &locals[i]})
		}
		if len(d.collection.Records) > 0 {
			rows = append(rows, inboxRow{head: fmt.Sprintf("GITHUB DRAFT ISSUES  %d", len(d.collection.Records))})
		}
	}
	for i := range d.collection.Records {
		rows = append(rows, inboxRow{gh: &d.collection.Records[i]})
	}
	return rows
}

func (m Model) gardenInboxFlatIndex() int {
	d := m.garden.drafts
	for i, r := range m.gardenInboxRows() {
		switch {
		case r.local != nil && d.selectedLocal != "" && r.local.ID == d.selectedLocal:
			return i
		case r.gh != nil && d.selectedLocal == "" && d.selected != 0 && r.gh.Item.Issue.ID == d.selected:
			return i
		}
	}
	return -1
}

// localTitle is what a local row shows for the idea.
func localTitle(l drafts.Draft) string {
	t := strings.TrimSpace(l.Title)
	if t == "" {
		t = strings.TrimSpace(strings.SplitN(strings.TrimSpace(l.Description), "\n", 2)[0])
	}
	if t == "" {
		t = "(untitled)"
	}
	t = boardTitle(t)
	if l.ConflictOf != "" {
		t = "recovery of " + drafts.DraftLabel(l.ConflictOf) + " · " + t
	}
	return t
}

// localState is the readiness word of a row.
func localState(l drafts.Draft) (string, lipgloss.Color) {
	c := theme.Current
	if !l.ArchivedAt.IsZero() {
		return "archived", c.Muted
	}
	switch {
	case l.Publication.State == drafts.Publishing && l.Publication.Issue != nil:
		return "Publishing incomplete", c.Warning
	case l.Publication.State == drafts.Publishing:
		return "Publishing", c.Warning
	}
	r := drafts.Assess(l)
	switch {
	case r.Ready():
		return "Ready", c.Accent
	case len(r.Missing) > 0:
		return fmt.Sprintf("%d missing", len(r.Missing)), c.Warning
	}
	return fmt.Sprintf("%d question%s", r.Unresolved, plural(r.Unresolved)), c.Warning
}

func (m Model) gardenLocalInspector(w, n int, l drafts.Draft) []string {
	c := theme.Current
	var lines []string
	line := func(fg lipgloss.Color, bold bool, s string) {
		lines = append(lines, boardLine(w, c.Background, fg, bold, " "+s))
	}
	blank := func() { line(c.Muted, false, "") }
	line(c.Text, true, drafts.DraftLabel(l.ID)+" / "+typeWord(l.Type))
	blank()
	for _, t := range strings.Split(ansi.Wrap(localTitle(l), max(1, w-2), ""), "\n")[:1] {
		line(c.Text, false, t)
	}
	blank()
	state, fg := localState(l)
	line(fg, true, state)
	if l.Publication.State == drafts.Unpublished {
		for _, reason := range privReasons(drafts.Assess(l), 0) {
			line(c.Muted, false, "○ "+reason)
		}
	}
	blank()
	switch {
	case l.Publication.State == drafts.Publishing && l.Publication.Issue != nil:
		line(c.Muted, false, fmt.Sprintf("Issue #%d exists, not approved", l.Publication.Issue.Number))
		line(c.Muted, false, "Open it to resume")
	case l.Publication.State == drafts.Publishing:
		line(c.Muted, false, "GitHub may hold the issue")
		line(c.Muted, false, "Open it to resume")
	default:
		line(c.Muted, false, "Private to this machine")
		line(c.Muted, false, "No GitHub issue yet")
	}
	line(c.Muted, false, "Updated "+ago(l.UpdatedAt))
	line(c.Dim, false, string(l.ID))
	blank()
	line(c.Warning, true, "Enter open  ·  a archive")
	for len(lines) < n {
		blank()
	}
	return lines[:n]
}

// inboxRow is one line of the inbox: a group heading, a local draft or a GitHub Issue.
type inboxRow struct {
	head  string
	local *drafts.Draft
	gh    *work.Draft
}
