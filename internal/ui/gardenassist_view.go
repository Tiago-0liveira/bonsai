package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden/assist"
	"github.com/Tiago-0liveira/bonsai/internal/ui/agentsapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/components/accounts"
	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
)

// assistGlyph is the rail mark of an answer: chose an option (+ with notes), wrote own, don't know, skipped, open.
func (m Model) assistGlyph(a assistAnswer) string {
	ascii := m.agentGlyphsASCII()
	pick := func(uni, asc string) string {
		if ascii {
			return asc
		}
		return uni
	}
	switch a.sel {
	case selChoice:
		if strings.TrimSpace(a.text) != "" {
			return pick("●+", "o+")
		}
		return pick("●", "o")
	case selOwn:
		if a.state() == assist.Answered {
			return pick("✎", "w")
		}
	case selUnknown:
		return "?"
	case selSkip:
		return "-"
	}
	return pick("·", ".")
}

// --- chrome ---

func (s *assistSession) label() string {
	if s.isNew {
		return "New idea"
	}
	return fmt.Sprintf("#%d", s.number)
}

func (m Model) gardenAssistCrumbs() []string {
	s := m.garden.assist.open
	if s == nil {
		return []string{"Garden", "Drafts", "Elaborate with AI"}
	}
	return []string{"Garden", "Drafts", s.label(), "Elaborate with AI"}
}

func (m Model) gardenAssistIdentity() string {
	s := m.garden.assist.open
	switch {
	case s == nil:
		return "Elaborate with AI"
	case s.isNew:
		return "New idea with AI"
	}
	return fmt.Sprintf("#%d  %s", s.number, boardTitle(s.title))
}

func (m Model) gardenAssistFocusLabel() string {
	s := m.garden.assist.open
	switch {
	case s == nil:
		return "Draft / AI"
	case s.confirm:
		return "Draft / AI / Cancel request"
	case s.optsOpen:
		return "Draft / AI / Options"
	case s.stage == assistReview && s.refine:
		return "Draft / AI / Refine"
	case s.stage == assistReview:
		return "Draft / AI / Proposal"
	case s.stage == assistQuestions && s.zone == zoneOptions:
		return "Draft / AI / Options"
	case s.stage == assistQuestions && s.zone == zoneText:
		return "Draft / AI / Answer"
	case s.stage == assistQuestions:
		return "Draft / AI / Questions"
	}
	switch s.focus {
	case sfDesc:
		return "Draft / AI / Description"
	case sfOptions:
		return "Draft / AI / Options"
	}
	return "Draft / AI / Ask"
}

func (m Model) gardenAssistHints() []shellHint {
	g := m.garden
	s := g.assist.open
	if s == nil {
		return []shellHint{{"esc", "back"}}
	}
	esc := shellHint{"esc", "back"}
	if g.assist.running(s) {
		esc = shellHint{"esc", "cancel"}
	}
	switch {
	case s.confirm:
		return []shellHint{{"enter", "cancel request"}, {"l", "leave it running"}, {"esc", "keep waiting"}}
	case s.optsOpen:
		return []shellHint{{"tab", "next"}, {"j/k", "account"}, {"←/→", "questions"}, {"ctrl+s", "ask"}, {"esc", "done"}}
	case s.stage == assistReview && s.refine:
		return []shellHint{{"enter", "send feedback"}, {"esc", "cancel"}}
	case s.stage == assistReview:
		h := []shellHint{{"↑/↓", "section"}, {"a", "accept"}, {"k", "keep"}, {"A", "accept all"}, {"e", "editor"}, {"r", "refine"}, {"m", "more"}, {"o", "answer open"}, esc}
		if s.review != nil && s.review.onSuggestions() {
			h = append([]shellHint{{"space", "confirm"}}, h...)
		}
		if s.err != nil {
			h = append([]shellHint{{"R", "retry"}}, h...)
		}
		return h
	case s.stage == assistQuestions && s.zone == zoneText:
		return []shellHint{{"tab", "next"}, {"pgdn/pgup", "question"}, {"ctrl+s", "draft"}, {"esc", "options"}}
	case s.stage == assistQuestions && s.zone == zoneOptions:
		return []shellHint{{"↑/↓", "move"}, {"space", "select"}, {"1-9", "pick"}, {"tab", "next"}, {"pgdn/pgup", "question"}, {"ctrl+s", "draft"}, {"esc", "rail"}}
	case s.stage == assistQuestions:
		h := []shellHint{{"↑/↓", "question"}, {"enter", "answer"}, {"tab", "next"}, {"ctrl+s", "draft"}, esc, {"m", "more"}}
		if s.err != nil {
			h = append([]shellHint{{"R", "retry"}}, h...)
		}
		return h
	}
	h := []shellHint{{"tab", "next"}, {"ctrl+s", "ask"}, {"ctrl+d", "draft now"}}
	if s.proposal != nil {
		h = append(h, shellHint{"p", "proposal"})
	}
	if s.focus == sfOptions {
		h = append([]shellHint{{"enter", "options"}}, h...)
	}
	if s.err != nil {
		h = append(h, shellHint{"R", "retry"})
	}
	return append(h, esc)
}

// --- feedback ---

// assistFeedback is the lines that say what the screen is doing or what went wrong.
func (m Model) assistFeedback(s *assistSession, width int) []string {
	p := theme.Current
	a := m.garden.assist
	var out []string
	switch {
	case s.confirm:
		return []string{gText(width, p.Warning, true, "Cancel the AI request? Enter cancels it · l leaves it running · esc keeps waiting")}
	case a.running(s):
		out = append(out, gText(width, p.Warning, true, m.assistRunning(a.flight)))
	case a.flight != nil:
		out = append(out, gText(width, p.Warning, false, "Another AI request is running."))
	}
	if s.err != nil && !a.running(s) {
		out = append(out, gWrap(width, p.Danger, true, "! "+boardSafe(s.err.Message))...)
		out = append(out, gWrap(width, p.Muted, false, strings.TrimSpace(boardSafe(s.err.Next)+" R retries with the same inputs; your text is unchanged."))...)
	}
	if s.notice != "" {
		out = append(out, gText(width, p.Warning, false, s.notice))
	}
	if n := m.garden.notice; n != "" {
		out = append(out, gText(width, p.Warning, false, n))
	}
	return out
}

// assistRunning is the status line of a flight.
func (m Model) assistRunning(f *assistFlight) string {
	var opts []string
	for _, s := range []string{f.model, f.effort} {
		if s != "" {
			opts = append(opts, s)
		}
	}
	detail := "default model"
	if len(opts) > 0 {
		detail = strings.Join(opts, ", ")
	}
	who := f.provider
	if who == "" {
		who = "the assistant"
	}
	return fmt.Sprintf("Asking %s (%s)… %ds · esc cancel", who, boardSafe(detail), f.secs)
}

// oneLine squeezes the feedback into the single row a screen can spare.
func (m Model) assistFeedbackRow(s *assistSession, width int, fallback string) string {
	p := theme.Current
	a := m.garden.assist
	switch {
	case s.confirm:
		return gText(width, p.Warning, true, "Cancel the AI request? Enter cancels · l leaves it running · esc keeps waiting")
	case a.running(s):
		return gText(width, p.Warning, true, m.assistRunning(a.flight))
	case a.flight != nil && s.notice == "":
		return gText(width, p.Warning, false, "Another AI request is running.")
	case s.err != nil:
		text := "! " + boardSafe(s.err.Message)
		if s.err.Next != "" {
			text += " " + boardSafe(s.err.Next)
		}
		return gText(width, p.Danger, true, text+" R retry")
	case s.notice != "":
		return gText(width, p.Warning, false, s.notice)
	}
	return gText(width, p.Muted, false, fallback)
}

// --- sizing ---

// gardenAssistSize gives the description and the answer editor the sizes the views draw them at.
func (m *Model) gardenAssistSize() {
	s := m.garden.assist.open
	if s == nil {
		return
	}
	fw := captureFieldWidth(m.width)
	s.desc.SetWidth(fw - 2)
	s.desc.SetHeight(m.assistDescHeight())
	tw, _ := m.gardenEditorText()
	g := s.panelGeometry(tw, m.assistPanelRows())
	s.qScroll = g.scroll
	s.ta.SetWidth(tw - 2)
	s.ta.SetHeight(g.taH)
	s.ta.Placeholder = "Your answer"
	if q, ok := s.current(); ok {
		if sel := s.answer(q).sel; sel == selChoice || sel == selUnknown {
			s.ta.Placeholder = "Add notes"
		}
	}
}

// assistDescHeight is the rows of the description box: what Setup leaves after its fixed lines.
func (m Model) assistDescHeight() int {
	return max(3, min(12, m.gardenBoardRows()-9))
}

// --- the screens ---

func (m Model) gsAssist(width, rows int) []string {
	p := theme.Current
	s := m.garden.assist.open
	if s == nil {
		return gCompose(width, rows, []gblock{{lines: []string{gText(width, p.Muted, false, "No AI session is open.")}}}, nil)
	}
	m.gardenAssistSize()
	switch {
	case s.optsOpen:
		return m.gsAssistOptions(s, width, rows)
	case s.stage == assistQuestions:
		return m.gsAssistQuestions(s, width, rows)
	case s.stage == assistReview:
		return m.gsAssistReview(s, width, rows)
	}
	return m.gsAssistSetup(s, width, rows)
}

// assistAccountSummary is the account and options as one line.
func (m Model) assistAccountSummary(s *assistSession) string {
	o := s.opts
	a, ok := o.Current()
	if !ok {
		switch {
		case o.ListErr() != "":
			return "accounts unavailable"
		case !o.Listed():
			return "loading accounts…"
		}
		return "no account"
	}
	set := o.settings()
	parts := []string{assistProviderName(a.Provider), boardTitle(a.Name)}
	for _, v := range []string{set.Model, set.Effort} {
		if v != "" {
			parts = append(parts, boardTitle(v))
		}
	}
	parts = append(parts, fmt.Sprintf("%d questions", set.Questions))
	return strings.Join(parts, " · ")
}

// assistDisclosure is said before the first call: what leaves the machine, and where to.
func (m Model) assistDisclosure(s *assistSession) string {
	if s.private != nil {
		return m.privDisclosure(s.private)
	}
	who := "the chosen account"
	if a, ok := s.opts.Current(); ok {
		who = fmt.Sprintf("%s (account %s)", assistProviderName(a.Provider), boardTitle(a.Name))
	}
	return "Sends your description, answers and draft to " + who + ". No repository files are read."
}

func (m Model) gsAssistSetup(s *assistSession, width, rows int) []string {
	p := theme.Current
	blank := blankRow(width, p.Background)

	title := "✎ ELABORATE WITH AI / A NEW IDEA"
	intro := "Describe it in your own words. Bonsai asks a few questions, then proposes a draft you review before anything is saved."
	if !s.isNew {
		title = fmt.Sprintf("✎ ELABORATE #%d WITH AI / %s", s.number, boardTitle(s.title))
		intro = "The assistant works from the editor's current text, including unsaved edits. Nothing changes until you accept a proposal."
	}
	top := []string{gText(width, p.Warning, true, title)}
	top = append(top, gWrap(width, p.Muted, false, intro)...)

	optsFocused := s.focus == sfOptions
	marker := func(on bool) string {
		if on {
			return "› "
		}
		return "  "
	}
	optFg := p.Text
	if optsFocused {
		optFg = p.Warning
	}
	bottom := []string{
		gRuns(width, p.Background, grun{marker(optsFocused) + "Options  ", optFg, optsFocused}, grun{m.assistAccountSummary(s) + "  (enter)", p.Muted, false}),
	}
	bottom = append(bottom, gWrap(width, p.Muted, false, m.assistDisclosure(s))...)
	bottom = append(bottom, gButtons(width, "  ",
		gButton("Ask questions (ctrl+s)", s.focus == sfAsk), gButton("Draft without questions (ctrl+d)", s.focus == sfDraft)))
	fb := m.assistFeedback(s, width)

	var mid []string
	if s.isNew {
		mid = m.assistDescBox(s, width)
	} else {
		mid = gWrap(width, p.Muted, false, m.assistDraftSummary(s))
	}

	// The description gives rows up before any control does.
	for len(top)+len(mid)+len(bottom)+len(fb) > rows && len(fb) > 2 {
		fb = fb[:len(fb)-1]
	}
	for len(top)+len(mid)+len(bottom)+len(fb) > rows && len(top) > 1 {
		top = top[:len(top)-1]
	}
	if over := len(top) + len(mid) + len(bottom) + len(fb) - rows; over > 0 && s.isNew && len(mid) > 4 {
		cut := min(over, len(mid)-4)
		mid = mid[:len(mid)-cut]
	}
	out := append([]string(nil), top...)
	out = append(out, mid...)
	out = append(out, bottom...)
	out = append(out, fb...)
	for len(out) < rows {
		out = append(out, blank)
	}
	return m.settingsASCII(out[:rows])
}

// assistDraftSummary says what the assistant would be given for an existing draft.
func (m Model) assistDraftSummary(s *assistSession) string {
	text := m.assistDraftText(s)
	lines := len(strings.Split(strings.TrimSpace(text), "\n"))
	if strings.TrimSpace(text) == "" {
		lines = 0
	}
	return fmt.Sprintf("Working draft: %d line%s from the specification editor.", lines, plural(lines))
}

// assistDescBox draws the description textarea with its label.
func (m Model) assistDescBox(s *assistSession, width int) []string {
	p := theme.Current
	fw := captureFieldWidth(width)
	focused := s.focus == sfDesc
	fg := p.Text
	if focused {
		fg = p.Warning
	}
	edge := seg(fg, p.Background, false)
	out := []string{gText(width, fg, focused, marker(focused)+"Description")}
	for _, l := range strings.Split(s.desc.View(), "\n") {
		l = ansi.Truncate(l, fw-2, "")
		l += strings.Repeat(" ", max(0, fw-2-ansi.StringWidth(l)))
		out = append(out, shellRow(width, p.Background, seg("", p.Background, false).Render("  ")+edge.Render("[")+l+edge.Render("]"), ""))
	}
	return out
}

func marker(on bool) string {
	if on {
		return "› "
	}
	return "  "
}

// --- options dialog ---

func (m Model) gsAssistOptions(s *assistSession, width, rows int) []string {
	p := theme.Current
	o := s.opts
	blank := blankRow(width, p.Background)
	iw := max(width-4, 10)
	row := func(fg lipgloss.Color, bold bool, text string) string { return gText(width, fg, bold, text) }

	title := "✎ AI OPTIONS / THIS SESSION ONLY"
	if s.private != nil {
		title = "✎ INVITE AN ASSISTANT / THIS DRAFT"
	}
	if o.defaultsMode {
		title = "✎ AI DRAFTING / DEFAULTS"
	}
	head := []string{gText(width, p.Warning, true, title)}

	var accts []string
	selAt := 0
	switch {
	case !o.Listed():
		accts = []string{row(p.Muted, false, "Loading accounts…")}
	case o.ListErr() != "":
		accts = []string{row(p.Danger, true, "Could not load accounts"), row(p.Muted, false, o.ListErr())}
	case len(o.Ordered()) == 0:
		accts = []string{row(p.Muted, false, "No agent accounts yet. Add one in Accounts.")}
	default:
		for n, a := range o.Ordered() {
			on := a.ID == o.Selected()
			if on {
				selAt = n
			}
			glyph := theme.ProviderGlyph(a.Provider, m.agentGlyphsASCII())
			status, fg := "ready", p.Text
			switch {
			case accounts.NeedsSignIn(a.Info):
				status, fg = "signed out: choose it and press ctrl+s to sign in", p.Warning
				if o.defaultsMode {
					status = "signed out: sign in from Accounts"
				}
			case !a.Structured.Supported:
				status, fg = "cannot draft: "+oneReason(a), p.Dim
			}
			if on {
				fg = p.Warning
			}
			text := fmt.Sprintf("%s%s %s · %s   %s", marker(on && o.focus == optAccount), glyph, providerName(a.Provider), boardTitle(a.Name), status)
			accts = append(accts, row(fg, on, text))
		}
	}

	var form []string
	switch {
	case o.formErr != "":
		form = []string{row(p.Danger, false, "Options unavailable: "+boardSafe(o.formErr))}
	case o.Selected() == "":
		form = []string{row(p.Muted, false, "Choose an account to set model and effort.")}
	case !o.formLoaded:
		form = []string{row(p.Muted, false, "Loading model and effort…")}
	default:
		raw, _, _ := o.form.CompactLines(iw)
		for _, l := range raw {
			form = append(form, shellRow(width, p.Background, "  "+l, ""))
		}
	}

	qFocus := o.focus == optQuestions
	qfg := p.Text
	if qFocus {
		qfg = p.Warning
	}
	gFocus := o.focus == optGuidance
	gfg := p.Text
	if gFocus {
		gfg = p.Warning
	}
	o.guidance.Width = max(8, min(iw-16, 60))
	tail := []string{
		gRuns(width, p.Background, grun{marker(qFocus) + "Questions  ", qfg, qFocus}, grun{fmt.Sprintf("‹ %d ›  per round, 1 to %d", o.questions, o.maxQuestions), p.Text, false}),
		shellRow(width, p.Background, seg(gfg, p.Background, gFocus).Render(" "+marker(gFocus)+"Guidance   ")+seg(p.Text, p.Background, false).Render("[ ")+o.guidance.View()+seg(p.Text, p.Background, false).Render(" ]"), ""),
	}
	if o.defaultsMode {
		cancelFG, saveFG := p.Text, p.Text
		if o.focus == optCancel {
			cancelFG = p.Warning
		}
		if o.focus == optSave {
			saveFG = p.Warning
		}
		tail = []string{gRuns(width, p.Background,
			grun{marker(o.focus == optCancel) + "[ Cancel ]   ", cancelFG, o.focus == optCancel},
			grun{marker(o.focus == optSave) + "[ Save defaults ]", saveFG, o.focus == optSave})}
	} else {
		tail = append(tail, gWrap(width, p.Muted, false, m.assistDisclosure(s))...)
	}
	if o.notice != "" {
		tail = append(tail, gWrap(width, p.Warning, false, o.notice)...)
	}
	if s.notice != "" {
		tail = append(tail, row(p.Warning, false, s.notice))
	}

	// The account list is the part that scrolls: the choices below it always stay.
	room := rows - len(head) - len(form) - len(tail) - 1
	if len(accts) > max(room, 1) {
		accts, _ = scrollWindow(accts, selAt, selAt+1, max(room, 1))
	}
	out := append([]string(nil), head...)
	out = append(out, accts...)
	if len(out)+len(form)+len(tail) < rows {
		out = append(out, blank)
	}
	out = append(out, form...)
	out = append(out, tail...)
	for len(out) < rows {
		out = append(out, blank)
	}
	return m.settingsASCII(out[:rows])
}

func oneReason(a agentsapi.Account) string {
	if a.Structured.Reason != "" {
		return boardTitle(a.Structured.Reason)
	}
	return "no tool-free mode"
}

// --- questions ---

type assistInfoLine struct {
	text string
	fg   lipgloss.Color
	bold bool
}

func wrapLines(text string, w int) []string {
	return strings.Split(ansi.Wrap(text, max(w, 1), ""), "\n")
}

// assistGeometry is how the answer panel spends its rows. The panel is a head line, the question text, the
// "Choose one" label, the options and the text field. The options and the field always stay: when rows run short the
// field shrinks first, then the separator, the head line and the label go, and the question text scrolls.
type assistGeometry struct {
	head, rule, choose bool
	text               []assistInfoLine // the visible part of the question and its reason
	clipped            bool             // some of the question text is out of view
	scroll             int              // first visible line, after clamping
	optRoom            int              // option lines shown, separator included
	taH                int              // rows of the text field; 0 when the selection has no field
}

const (
	assistFieldMax = 6
	// assistChrome is the lines of the Questions stage around the panel: title, status, feedback and position.
	assistChrome = 4
)

// assistPanelRows is the rows the rail and the panel share.
func (m Model) assistPanelRows() int { return max(3, m.gardenBoardRows()-assistChrome) }

func (s *assistSession) panelGeometry(tw, th int) assistGeometry {
	p := theme.Current
	q, ok := s.current()
	if !ok {
		return assistGeometry{taH: 2}
	}
	var block []assistInfoLine
	for _, l := range wrapLines(boardTitle(q.Text), tw) {
		block = append(block, assistInfoLine{l, p.Text, true})
	}
	if t := boardTitle(q.Why); t != "" && !s.hideWhy {
		for _, l := range wrapLines("Why: "+t, tw) {
			block = append(block, assistInfoLine{l, p.Muted, false})
		}
	}
	g := assistGeometry{head: true, choose: true, rule: len(q.Suggestions) > 0}
	field := s.answer(q).sel != selSkip
	if field {
		g.taH = 2
	}
	optLines := func() int { return optionRows(q) + b2i(g.rule) }
	used := func() int {
		n := b2i(g.head) + b2i(g.choose) + optLines()
		if field {
			n += 1 + g.taH
		}
		return n
	}
	for used()+1 > th {
		switch {
		case g.taH > 1:
			g.taH = 1
		case g.rule:
			g.rule = false
		case g.head:
			g.head = false
		case g.choose:
			g.choose = false
		default:
			g.optRoom = max(th-1-b2i(field)*(1+g.taH), 1)
			return g.withText(block, 1, s.qScroll)
		}
	}
	g.optRoom = optLines()
	room := max(th-used(), 1)
	if field && len(block) < room {
		g.taH = min(g.taH+room-len(block), assistFieldMax)
	}
	return g.withText(block, room, s.qScroll)
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// withText fits the question text into room lines, scrolled to scroll when it is longer.
func (g assistGeometry) withText(block []assistInfoLine, room, scroll int) assistGeometry {
	if len(block) <= room {
		g.text = block
		return g
	}
	g.clipped = true
	g.scroll = max(0, min(scroll, len(block)-room))
	g.text = block[g.scroll : g.scroll+room]
	return g
}

// optionLines lists the options as the list draws them: the suggestions, a separator when there are any and room for
// it, then Write my own, I don't know and Skip.
func optionLines(q assist.Question, rule bool, ascii bool) []string {
	var out []string
	for _, sg := range q.Suggestions {
		out = append(out, boardTitle(sg))
	}
	if rule {
		sep := "─────────────"
		if ascii {
			sep = "-------------"
		}
		out = append(out, sep)
	}
	return append(out, "Write my own answer", "I don't know", "Skip for now")
}

type railEntry struct {
	header bool
	round  int
	q      int
}

// rail lists the headers and questions in view: only the round of the selected question is open.
func (s *assistSession) rail() []railEntry {
	var out []railEntry
	open := 0
	if q, ok := s.current(); ok {
		open = q.Round
	}
	last := -1
	for i, q := range s.questions {
		if q.Round != last {
			out = append(out, railEntry{header: true, round: q.Round})
			last = q.Round
		}
		if q.Round == open {
			out = append(out, railEntry{round: q.Round, q: i})
		}
	}
	return out
}

func (s *assistSession) roundCounts(round int) (done, total int) {
	for _, q := range s.questions {
		if q.Round != round {
			continue
		}
		total++
		if s.answer(q).state() != assist.Open {
			done++
		}
	}
	return
}

func (m Model) gsAssistQuestions(s *assistSession, width, rows int) []string {
	p := theme.Current
	railW := gardenEditorRail(width)
	tw, _ := m.gardenEditorText()
	th := m.assistPanelRows()
	blank := blankRow(width, p.Background)

	out := make([]string, 0, rows)
	out = append(out, gText(width, p.Warning, true, fmt.Sprintf("‹ %s · AI questions (Esc)", boardTitle(s.label()+" "+s.title))))
	answered, skipped, unknown := s.counts()
	status := fmt.Sprintf("Round %d · %d question%s · %d answered · %d skipped · %d don't know", s.round, len(s.questions), plural(len(s.questions)), answered, skipped, unknown)
	out = append(out, gText(width, p.Muted, false, status))
	out = append(out, m.assistFeedbackRow(s, width, "Skipped and don't-know questions stay in the draft as open questions."))

	var textLines []string
	if len(s.questions) == 0 || s.sel >= len(s.questions) {
		msg := "No further questions — draft now."
		if len(s.questions) > 0 {
			msg = "Choose a question."
		}
		textLines = append(textLines, seg(p.Text, p.Background, true).Render(fitCell(msg, tw)))
		if s.understanding != "" {
			for _, l := range wrapLines(boardTitle(s.understanding), tw) {
				textLines = append(textLines, seg(p.Muted, p.Background, false).Render(fitCell(l, tw)))
			}
		}
		textLines = append(textLines, seg(p.Muted, p.Background, false).Render(fitCell("ctrl+s asks for the draft.", tw)))
	} else {
		textLines = m.assistPanel(s, tw, th)
	}

	entries := s.rail()
	selAt := 0
	for i, e := range entries {
		if !e.header && e.q == s.sel {
			selAt = i
		}
	}
	first := 0
	if len(entries) > th {
		first = max(0, min(selAt-th/2, len(entries)-th))
	}
	avail := width - railW - 2
	pad := max(0, (avail-tw)/2)
	for i := 0; i < th; i++ {
		rail := fitCell("", railW)
		bg, fg, bold := p.Surface, p.Muted, false
		if n := first + i; n < len(entries) {
			e := entries[n]
			if e.header {
				done, total := s.roundCounts(e.round)
				fold := "▸"
				if q, ok := s.current(); ok && q.Round == e.round {
					fold = "▼"
				}
				rail = fitCell(fmt.Sprintf(" %s Round %d  %d/%d", fold, e.round, done, total), railW)
			} else {
				q := s.questions[e.q]
				mark := " "
				if e.q == s.sel {
					bg, fg, bold = p.Raised, p.Warning, true
					if s.zone == zoneRail {
						mark = ">"
					}
				}
				glyph := m.assistGlyph(s.answer(q))
				rail = fitCell(fmt.Sprintf("%s%s%s %s", mark, glyph, strings.Repeat(" ", 2-ansi.StringWidth(glyph)), boardTitle(q.Text)), railW)
			}
		}
		text := strings.Repeat(" ", tw)
		if i < len(textLines) {
			text = textLines[i]
		}
		line := seg(fg, bg, bold).Render(rail) + seg("", p.Background, false).Render(strings.Repeat(" ", 2+pad)) + text
		out = append(out, shellRow(width, p.Background, line, ""))
	}
	pos := "No questions"
	if len(s.questions) > 0 {
		pos = fmt.Sprintf("Question %d of %d", s.sel+1, len(s.questions))
	}
	out = append(out, gText(width, p.Muted, false, pos+" · "+m.assistDisclosureShort(s)))
	for len(out) < rows {
		out = append(out, blank)
	}
	return m.settingsASCII(out[:rows])
}

// assistPanel draws the answer panel of the selected question as lines of the text pane's width.
func (m Model) assistPanel(s *assistSession, tw, th int) []string {
	p := theme.Current
	q, _ := s.current()
	ascii := m.agentGlyphsASCII()
	g := s.panelGeometry(tw, th)
	a := s.answer(q)
	line := func(fg lipgloss.Color, bold bool, text string) string {
		return seg(fg, p.Background, bold).Render(fitCell(text, tw))
	}
	focusFG := func(on bool) lipgloss.Color {
		if on {
			return p.Warning
		}
		return p.Muted
	}

	scrollHint := "ctrl+↑/↓ scrolls the question"
	var out []string
	if g.head {
		head := fmt.Sprintf("Q%d of %d", s.sel+1, len(s.questions))
		if area := boardTitle(q.Area); area != "" {
			head += " · " + area
		}
		if g.clipped {
			head += "  (" + scrollHint + ")"
		}
		out = append(out, line(p.Muted, false, head))
	}
	for i, il := range g.text {
		if g.clipped && !g.head && i == len(g.text)-1 {
			il.text = ansi.Truncate(il.text, max(tw-len(scrollHint)-3, 1), "…") + " (ctrl+↑/↓)"
		}
		out = append(out, line(il.fg, il.bold, il.text))
	}

	if g.choose {
		out = append(out, line(focusFG(s.zone == zoneOptions), s.zone == zoneOptions, "Choose one"))
	}
	rows := optionLines(q, g.rule, ascii)
	cur := s.optCur
	if g.rule && cur >= len(q.Suggestions) {
		cur++ // the separator takes a line
	}
	var optLines []string
	for i, text := range rows {
		row := i
		if g.rule && i == len(q.Suggestions) {
			optLines = append(optLines, line(p.Dim, false, "  "+text))
			continue
		}
		if g.rule && i > len(q.Suggestions) {
			row = i - 1
		}
		on := a.sel != selNone && selectedRow(q, a) == row
		radio := "( )"
		switch {
		case on && ascii:
			radio = "(*)"
		case on:
			radio = "(•)"
		}
		mark := "  "
		if s.zone == zoneOptions && row == s.optCur {
			mark = "› "
		}
		fg := p.Text
		if row < len(q.Suggestions) {
			fg = p.Accent
		}
		if on {
			fg = p.Warning
		}
		optLines = append(optLines, line(fg, on, mark+radio+" "+text))
	}
	optLines, _ = scrollWindow(optLines, cur, cur+1, g.optRoom)
	out = append(out, optLines...)

	if g.taH > 0 {
		label := "Your answer"
		if a.sel == selChoice || a.sel == selUnknown {
			label = "Notes (optional)"
		}
		on := s.zone == zoneText
		out = append(out, line(focusFG(on), on, label))
		edge := seg(focusFG(on), p.Background, false)
		ta := strings.Split(s.ta.View(), "\n")
		for j := 0; j < g.taH; j++ {
			l := ""
			if j < len(ta) {
				l = ansi.Truncate(ta[j], tw-2, "")
			}
			l += strings.Repeat(" ", max(0, tw-2-ansi.StringWidth(l)))
			out = append(out, edge.Render("[")+l+edge.Render("]"))
		}
	}
	return out
}

func (m Model) assistDisclosureShort(s *assistSession) string {
	if s.provider == "" {
		return "answers go to the chosen account"
	}
	return "answers go to " + s.provider
}
