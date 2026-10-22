package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/Tiago-0liveira/bonsai/internal/core/garden"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/approve"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/assist"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/planrev"
	"github.com/Tiago-0liveira/bonsai/internal/core/garden/spec"
	"github.com/Tiago-0liveira/bonsai/internal/ui/gardenapi"
	"github.com/Tiago-0liveira/bonsai/internal/ui/theme"
)

// The review of a proposal. Per fixed section: I is the text the request sent, C the working draft now (the
// editor's text) and P the proposal. The editor changes only when the user applies decisions (e, r or m); Extra
// sections are never part of a decision.

type assistAfter int

const (
	afterNone assistAfter = iota
	afterEdit
	afterRefine
	afterMore
	afterOpen
)

// maxOpenRound is how many open questions one local answer round takes, so the rail stays usable.
const maxOpenRound = 10

// reviewSuggestion is a proposed item nobody stated or answered, in the fixed section it would join.
type reviewSuggestion struct {
	section int
	item    assist.Item
}

type reviewState struct {
	input, proposed []string
	choice          []int // per section: 0 default, 1 accept, 2 keep
	sel             int   // a fixed section, or spec.FixedCount for the Suggestions row
	sugg            []reviewSuggestion
	confirmed       map[assist.Item]bool // suggestions that join their section; the rest are dropped on apply
	sugCur          int
	openNew         int // open questions the assistant added that the draft did not have
}

func newAssistReview(draft string, p assist.Proposal) *reviewState {
	in := spec.Parse(draft)
	r := &reviewState{choice: make([]int, spec.FixedCount), confirmed: map[assist.Item]bool{}}
	for i, section := range [][]assist.Item{p.CurrentBehavior, p.DesiredBehavior, p.Requirements, p.AcceptanceCriteria, p.Constraints} {
		for _, it := range section {
			if it.Basis == "suggested" {
				r.sugg = append(r.sugg, reviewSuggestion{section: i + 1, item: it})
			}
		}
	}
	for i := 0; i < spec.FixedCount; i++ {
		r.input = append(r.input, in.Text(i))
	}
	r.rebuild(p)
	had := map[string]bool{}
	for _, q := range approve.OpenQuestions(in.OpenQuestions) {
		had[q] = true
	}
	for _, q := range approve.OpenQuestions(r.proposed[spec.FixedCount-1]) {
		if !had[q] {
			r.openNew++
		}
	}
	// Carried lines are the person's own skipped or unknown questions, not the assistant's.
	r.openNew = max(0, r.openNew-p.Carried)
	return r
}

// rebuild renders the proposal with the confirmed suggestions.
func (r *reviewState) rebuild(p assist.Proposal) {
	pr := p.Spec(r.confirmed)
	r.proposed = r.proposed[:0]
	for i := 0; i < spec.FixedCount; i++ {
		r.proposed = append(r.proposed, pr.Text(i))
	}
}

// rows is the rail length: the fixed sections and, when there are suggestions, their row.
func (r *reviewState) rows() int {
	if len(r.sugg) > 0 {
		return spec.FixedCount + 1
	}
	return spec.FixedCount
}

func (r *reviewState) onSuggestions() bool { return r.sel == spec.FixedCount && len(r.sugg) > 0 }

type reviewSection struct {
	cur    string
	state  byte // '=' unchanged, '~' proposed, '!' you edited
	accept bool
	edited bool // the working draft differs from what the request sent
	clears bool // the proposal would empty a section that has text
}

// assistSections derives every fixed section's state and decision from the editor as it is now.
func (m Model) assistSections(s *assistSession) []reviewSection {
	r := s.review
	e := m.garden.drafts.editors[s.key]
	if e != nil {
		e.flush()
	}
	out := make([]reviewSection, spec.FixedCount)
	for i := range out {
		cur := r.input[i]
		if e != nil && i < len(e.secs) {
			cur = e.secs[i].text
		}
		same := garden.NormalizeContent(cur) == garden.NormalizeContent(r.proposed[i])
		edited := garden.NormalizeContent(cur) != garden.NormalizeContent(r.input[i])
		sec := reviewSection{cur: cur, edited: edited}
		switch {
		case same:
			sec.state = '='
		case edited:
			sec.state = '!'
		default:
			sec.state = '~'
		}
		// A proposal that empties a section the person wrote is never the default:
		// accepting it would delete their text, so only an explicit a does that.
		clears := strings.TrimSpace(r.proposed[i]) == "" && strings.TrimSpace(cur) != ""
		sec.clears = clears
		switch r.choice[i] {
		case 1:
			sec.accept = !same
		case 2:
		default:
			sec.accept = !same && !edited && !clears
		}
		out[i] = sec
	}
	return out
}

// setSection replaces a section's text in the editor.
func (e *editorDoc) setSection(i int, text string) {
	e.flush()
	e.secs[i].text = text
	if i == e.cur {
		e.ta.SetValue(text)
		textareaTop(&e.ta)
	}
}

// newIdeaEditor is the editor of an idea that has no Issue yet: key Issue 0, no base, always unsaved.
func newIdeaEditor(key editKey, title string) *editorDoc {
	e := newEditorDoc(key, documentOf(title))
	e.origHash = ""
	for i := range e.orig {
		e.orig[i] = ""
	}
	return e
}

func documentOf(title string) gardenapi.DocumentView { return gardenapi.DocumentView{Title: title} }

func (m Model) assistBusy(s *assistSession) bool {
	a := m.garden.assist
	if a.flight == nil {
		return false
	}
	if a.flight.session != s.id {
		s.notice = "Another AI request is running."
	}
	return true
}

// assistApplyThen applies the decisions to the editor, creating it when needed, and then does act. An existing
// draft whose clean editor was closed is read again first.
func (m Model) assistApplyThen(s *assistSession, act assistAfter, feedback string) (Model, tea.Cmd) {
	d := &m.garden.drafts
	e := d.editors[s.key]
	if e == nil {
		if !s.isNew {
			s.after, s.afterFeedback = act, feedback
			return m.gardenLoadDocument(s.key.issue, docApply)
		}
		e = newIdeaEditor(s.key, "")
		if d.editors == nil {
			d.editors = map[editKey]*editorDoc{}
		}
		d.editors[s.key] = e
	}
	return m.assistContinue(s, e, act, feedback)
}

func (m Model) assistContinue(s *assistSession, e *editorDoc, act assistAfter, feedback string) (Model, tea.Cmd) {
	g := &m.garden
	d := &g.drafts
	s.after, s.afterFeedback = afterNone, ""
	if s.review == nil {
		return m, nil
	}
	secs := m.assistSections(s)
	for i, sec := range secs {
		if sec.accept {
			e.setSection(i, s.review.proposed[i])
		}
	}
	// Once the capture form is open the title is the person's; a later proposal must not replace it.
	if s.isNew && s.proposal != nil && s.proposal.Title != "" && !d.idea.active {
		e.title = s.proposal.Title
	}
	switch act {
	case afterEdit:
		d.edit, d.stage = e, draftsEditor
		g.assist.open = nil
		e.setText(false)
		m.shellFocus = shellInput
		m.gardenSizeEditor()
		return m, nil
	case afterRefine:
		return m.gardenAssistDraft(s, feedback)
	case afterOpen:
		return m.assistOpenRound(s, e), nil
	}
	return m.gardenAssistMore(s)
}

func (m Model) gardenAssistReviewKey(s *assistSession, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	r := s.review
	if r == nil || s.proposal == nil {
		s.stage = assistSetup
		return m, nil
	}
	k := msg.String()
	if s.refine {
		switch k {
		case "esc":
			s.refine = false
		case "enter":
			fb := assist.SanitizeFeedback(s.feedback.Value())
			if fb == "" {
				s.notice = "Say what to change first."
				return m, nil
			}
			if m.assistBusy(s) {
				return m, nil
			}
			s.refine = false
			return m.assistApplyThen(s, afterRefine, fb)
		default:
			var cmd tea.Cmd
			s.feedback, cmd = s.feedback.Update(msg)
			return m, cmd
		}
		return m, nil
	}
	switch k {
	case "esc":
		s.stage = assistSetup
		if len(s.questions) > 0 {
			s.stage = assistQuestions
		}
		s.notice = ""
	case "up":
		if r.onSuggestions() && r.sugCur > 0 {
			r.sugCur--
		} else {
			r.sel = max(0, r.sel-1)
		}
	case "down":
		if r.onSuggestions() {
			r.sugCur = min(len(r.sugg)-1, r.sugCur+1)
		} else {
			r.sel = min(r.rows()-1, r.sel+1)
			r.sugCur = 0
		}
	case " ":
		if r.onSuggestions() {
			it := r.sugg[r.sugCur].item
			r.confirmed[it] = !r.confirmed[it]
			r.rebuild(*s.proposal)
		}
	case "o":
		if len(s.questions) >= assist.MaxSessionQuestions {
			s.notice = fmt.Sprintf("The session already asked %d questions, the most it allows. Refine the draft instead.", assist.MaxSessionQuestions)
			return m, nil
		}
		if m.assistBusy(s) {
			return m, nil
		}
		return m.assistApplyThen(s, afterOpen, "")
	case "a":
		if !r.onSuggestions() {
			r.choice[r.sel] = 1
		}
	case "k":
		if !r.onSuggestions() {
			r.choice[r.sel] = 2
		}
	case "A":
		for i, sec := range m.assistSections(s) {
			if !sec.edited && !sec.clears {
				r.choice[i] = 1
			}
		}
	case "e":
		return m.assistApplyThen(s, afterEdit, "")
	case "r":
		if !m.assistBusy(s) {
			s.refine = true
			s.feedback.SetValue("")
			s.feedback.Focus()
			s.notice = ""
		}
	case "m":
		if len(s.questions) >= assist.MaxSessionQuestions {
			s.notice = fmt.Sprintf("The session already asked %d questions, the most it allows. Refine the draft instead.", assist.MaxSessionQuestions)
			return m, nil
		}
		if m.assistBusy(s) {
			return m, nil
		}
		return m.assistApplyThen(s, afterMore, "")
	case "R":
		return m.gardenAssistRetry(s)
	}
	return m, nil
}

// assistOpenRound starts an answer round from the working draft's unresolved open questions. No AI call is made:
// the lines become questions without suggestions, and the next Draft resolves them through its answers.
func (m Model) assistOpenRound(s *assistSession, e *editorDoc) Model {
	lines := approve.OpenQuestions(spec.Parse(e.content()).OpenQuestions)
	if len(lines) == 0 {
		s.notice = "The draft has no open questions."
		return m
	}
	room := assist.MaxSessionQuestions - len(s.questions)
	if room <= 0 {
		s.notice = fmt.Sprintf("The session already asked %d questions, the most it allows. Refine the draft instead.", assist.MaxSessionQuestions)
		return m
	}
	n := min(len(lines), maxOpenRound, room)
	round := s.round + 1
	first := len(s.questions)
	for i, l := range lines[:n] {
		s.questions = append(s.questions, assist.Question{ID: fmt.Sprintf("o%dq%d", round, i+1), Round: round, Text: l, Why: "An open question in the draft."})
	}
	s.round = round
	s.stage = assistQuestions
	s.sel = first
	s.resetPanel()
	s.notice = ""
	if n < len(lines) {
		s.notice = fmt.Sprintf("%d more open questions stay in the draft: ask again after this round.", len(lines)-n)
	}
	return m
}

// --- view ---

func (m Model) gsAssistReview(s *assistSession, width, rows int) []string {
	p := theme.Current
	r := s.review
	if r == nil || s.proposal == nil {
		return gCompose(width, rows, []gblock{{lines: []string{gText(width, p.Muted, false, "No proposal yet.")}}}, nil)
	}
	railW := gardenEditorRail(width)
	tw, th := m.gardenEditorText()
	blank := blankRow(width, p.Background)
	secs := m.assistSections(s)
	pr := s.proposal

	out := []string{gText(width, p.Warning, true, fmt.Sprintf("‹ %s · AI proposal (Esc)", boardTitle(s.label()+" "+s.title)))}
	status := "Nothing is applied until you press e."
	if s.isNew && pr.Title != "" {
		status = "Suggested title: " + boardTitle(pr.Title) + " (" + string(pr.Type) + ") · " + status
	}
	var counts []string
	if pr.Carried > 0 {
		counts = append(counts, fmt.Sprintf("%d open question%s added for unanswered questions", pr.Carried, plural(pr.Carried)))
	}
	if pr.Dropped > 0 {
		counts = append(counts, fmt.Sprintf("%d item%s dropped", pr.Dropped, plural(pr.Dropped)))
	}
	if len(counts) > 0 {
		status = strings.Join(counts, " · ") + " · " + status
	}
	out = append(out, gText(width, p.Muted, false, status))
	switch {
	case s.refine:
		out = append(out, shellRow(width, p.Background, seg(p.Warning, p.Background, true).Render(" Refine: ")+m.assistFeedbackInput(s, width-12), ""))
	default:
		fallback := "a accept · k keep · A accept all not edited · e open in the editor"
		switch n := r.openNew; {
		case n == 1:
			fallback = "AI added 1 open question; it blocks validation. o answers it here."
		case n > 1:
			fallback = fmt.Sprintf("AI added %d open questions; they block validation. o answers them here.", n)
		}
		out = append(out, m.assistFeedbackRow(s, width, fallback))
	}

	// The pane of the selected section.
	var pane []assistInfoLine
	if r.onSuggestions() {
		pane = m.assistSuggestionPane(r, th)
	} else {
		pane = m.assistSectionPane(s, secs, pr, tw)
	}
	avail := width - railW - 2
	pad := max(0, (avail-tw)/2)
	for i := 0; i < th; i++ {
		rail := fitCell("", railW)
		bg, fg, bold := p.Surface, p.Muted, false
		if i < r.rows() {
			mark := " "
			if i == r.sel {
				bg, fg, bold = p.Raised, p.Warning, true
				mark = ">"
			}
			if i == spec.FixedCount {
				rail = fitCell(fmt.Sprintf("%s? Suggestions %d", mark, len(r.sugg)), railW)
			} else {
				rail = fitCell(fmt.Sprintf("%s%c %s", mark, secs[i].state, spec.Names[i]), railW)
			}
		}
		text := strings.Repeat(" ", tw)
		if i < len(pane) {
			il := pane[i]
			text = seg(il.fg, p.Background, il.bold).Render(fitCell(ansi.Truncate(il.text, tw, "…"), tw))
		}
		line := seg(fg, bg, bold).Render(rail) + seg("", p.Background, false).Render(strings.Repeat(" ", 2+pad)) + text
		out = append(out, shellRow(width, p.Background, line, ""))
	}
	out = append(out, gText(width, p.Muted, false, "= unchanged · ~ proposed · ! you edited · "+m.assistDisclosureShort(s)))
	for len(out) < rows {
		out = append(out, blank)
	}
	return m.settingsASCII(out[:rows])
}

// assistSuggestionPane lists the suggestions with their confirmation; only confirmed ones are ever saved.
func (m Model) assistSuggestionPane(r *reviewState, th int) []assistInfoLine {
	p := theme.Current
	pane := []assistInfoLine{
		{"? Suggestions · the assistant's ideas, nobody asked for them", p.Warning, true},
		{"space confirms one: it joins its section. The rest are dropped when you apply.", p.Muted, false},
	}
	h := max(th-len(pane), 1)
	start := 0
	if r.sugCur >= h {
		start = r.sugCur - h + 1
	}
	for i := start; i < len(r.sugg) && i < start+h; i++ {
		sg := r.sugg[i]
		box, fg := "[ ]", p.Text
		if r.confirmed[sg.item] {
			box, fg = "[x]", p.Success
		}
		cur := "  "
		if i == r.sugCur {
			cur = "› "
		}
		pane = append(pane, assistInfoLine{cur + box + " " + spec.Names[sg.section] + ": " + boardTitle(sg.item.Text), fg, i == r.sugCur})
	}
	return pane
}

// assistSectionPane describes one fixed section: its state, the decision and the diff.
func (m Model) assistSectionPane(s *assistSession, secs []reviewSection, pr *assist.Proposal, tw int) []assistInfoLine {
	p := theme.Current
	r := s.review
	sec := secs[r.sel]
	title := spec.Names[r.sel]
	var pane []assistInfoLine
	word := map[byte]string{'=': "no change proposed", '~': "proposed change", '!': "you edited this section"}[sec.state]
	pane = append(pane, assistInfoLine{"§ " + title + " · " + word, p.Warning, true})
	decision := "Decision: keep your text"
	if sec.accept {
		decision = "Decision: accept the proposal"
	}
	pane = append(pane, assistInfoLine{decision, p.Text, false})
	if sec.edited && sec.state != '=' {
		pane = append(pane, assistInfoLine{"you edited this section while the assistant worked: kept unless you press a", p.Muted, false})
	}
	if sec.clears {
		pane = append(pane, assistInfoLine{"the proposal leaves this section empty: your text is kept unless you press a", p.Warning, false})
	}
	for i, n := range pr.Notes {
		if i == 2 {
			break
		}
		pane = append(pane, assistInfoLine{"Note: " + boardTitle(n), p.Muted, false})
	}
	diff := planrev.Collapse(planrev.Diff(sec.cur, r.proposed[r.sel]), 1)
	if len(diff) == 0 {
		pane = append(pane, assistInfoLine{"(identical)", p.Muted, false})
	}
	for _, l := range diff {
		switch {
		case l.Skipped > 0:
			pane = append(pane, assistInfoLine{fmt.Sprintf("  … %d unchanged line%s", l.Skipped, plural(l.Skipped)), p.Dim, false})
		case l.Op == planrev.OpAdd:
			pane = append(pane, assistInfoLine{"+ " + boardSafe(l.Text), p.Success, false})
		case l.Op == planrev.OpDel:
			pane = append(pane, assistInfoLine{"- " + boardSafe(l.Text), p.Danger, false})
		default:
			pane = append(pane, assistInfoLine{"  " + boardSafe(l.Text), p.Muted, false})
		}
	}
	return pane
}

func (m Model) assistFeedbackInput(s *assistSession, w int) string {
	s.feedback.Width = max(8, w)
	return s.feedback.View()
}
